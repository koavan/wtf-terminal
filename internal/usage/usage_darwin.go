package usage

/*
#include <dlfcn.h>
#include <libproc.h>
#include <mach/mach_time.h>
#include <sys/resource.h>

// responsibility_get_pid_responsible_for_pid is how Activity Monitor ties
// WebKit's helpers to the app they work for. Not in a public header, so it's
// looked up at run time; without it, only wtf's own processes are counted.
static pid_t (*wtf_responsible_fn)(pid_t);
static int wtf_responsible_loaded;

static int wtf_has_responsible(void) {
	if (!wtf_responsible_loaded) {
		wtf_responsible_fn = (pid_t (*)(pid_t))dlsym(RTLD_DEFAULT, "responsibility_get_pid_responsible_for_pid");
		wtf_responsible_loaded = 1;
	}
	return wtf_responsible_fn != NULL;
}

static pid_t wtf_responsible(pid_t pid) {
	return wtf_has_responsible() ? wtf_responsible_fn(pid) : -1;
}

static int wtf_stat(pid_t pid, uint64_t *cpu, uint64_t *mem) {
	struct rusage_info_v2 ri;
	if (proc_pid_rusage(pid, RUSAGE_INFO_V2, (rusage_info_t *)&ri) != 0) return -1;
	*cpu = ri.ri_user_time + ri.ri_system_time;
	*mem = ri.ri_phys_footprint;
	return 0;
}
*/
import "C"

import (
	"time"
	"unsafe"
)

const (
	memoryKind       = "footprint"
	supportsDetached = true
)

// rusage reports CPU time in mach ticks: nanoseconds on Intel, 125/3 ns on Apple silicon.
var tick = func() float64 {
	var tb C.mach_timebase_info_data_t
	C.mach_timebase_info(&tb)
	if tb.denom == 0 {
		return 1
	}
	return float64(tb.numer) / float64(tb.denom)
}()

func readStat(pid int) (stat, bool) {
	var cpu, mem C.uint64_t
	if C.wtf_stat(C.pid_t(pid), &cpu, &mem) != 0 {
		return stat{}, false
	}
	var buf [256]C.char
	C.proc_name(C.int(pid), unsafe.Pointer(&buf[0]), C.uint32_t(len(buf)))
	return stat{
		cpu:  time.Duration(float64(cpu) * tick),
		mem:  uint64(mem),
		name: C.GoString(&buf[0]),
	}, true
}

func children(pid int) []int {
	buf := make([]C.pid_t, 64)
	for {
		n := int(C.proc_listchildpids(C.pid_t(pid), unsafe.Pointer(&buf[0]), C.int(len(buf)*int(unsafe.Sizeof(buf[0])))))
		if n <= 0 {
			return nil
		}
		if n < len(buf) {
			out := make([]int, n)
			for i := range out {
				out[i] = int(buf[i])
			}
			return out
		}
		buf = make([]C.pid_t, len(buf)*4)
	}
}

// scanDetached finds the processes wtf is responsible for (as Activity
// Monitor groups them): WebKit's helpers, and programs that left the
// shell they were started from. Descendants are sorted out by the caller.
func scanDetached(self int) ([]detachedProc, string) {
	if C.wtf_has_responsible() == 0 {
		return nil, "This macOS doesn't say which processes belong to wtf, so the window's WebKit processes aren't counted."
	}
	n := int(C.proc_listallpids(nil, 0))
	if n <= 0 {
		return nil, ""
	}
	buf := make([]C.pid_t, n+64)
	n = int(C.proc_listallpids(unsafe.Pointer(&buf[0]), C.int(len(buf)*int(unsafe.Sizeof(buf[0])))))
	var out []detachedProc
	for _, p := range buf[:max(n, 0)] {
		pid := int(p)
		if pid == self || pid <= 1 || int(C.wtf_responsible(p)) != self {
			continue
		}
		st, ok := readStat(pid)
		if !ok {
			continue
		}
		out = append(out, detachedProc{pid: pid, webkit: isWebKit(st.name)})
	}
	return out, ""
}

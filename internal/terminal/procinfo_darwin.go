package terminal

/*
#include <libproc.h>
#include <string.h>
#include <sys/proc_info.h>

static int wtf_proc_cwd(int pid, char *out, int n) {
	struct proc_vnodepathinfo vpi;
	int r = proc_pidinfo(pid, PROC_PIDVNODEPATHINFO, 0, &vpi, sizeof(vpi));
	if (r <= 0) return -1;
	strlcpy(out, vpi.pvi_cdir.vip_path, n);
	return 0;
}
*/
import "C"

import (
	"os/exec"
	"strings"
	"unsafe"
)

func processName(pid int) string {
	buf := make([]byte, 256)
	n := C.proc_name(C.int(pid), unsafe.Pointer(&buf[0]), C.uint32_t(len(buf)))
	if n <= 0 {
		return ""
	}
	return string(buf[:n])
}

func processCwd(pid int) string {
	buf := make([]byte, 1024) // MAXPATHLEN
	if C.wtf_proc_cwd(C.int(pid), (*C.char)(unsafe.Pointer(&buf[0])), C.int(len(buf))) != 0 {
		return ""
	}
	return C.GoString((*C.char)(unsafe.Pointer(&buf[0])))
}

// loginShell asks Directory Services, because apps launched from Finder
// don't reliably inherit $SHELL.
func loginShell(user string) string {
	out, err := exec.Command("/usr/bin/dscl", ".", "-read", "/Users/"+user, "UserShell").Output()
	if err != nil {
		return ""
	}
	// Output looks like "UserShell: /bin/zsh"
	_, shell, ok := strings.Cut(strings.TrimSpace(string(out)), ":")
	if !ok {
		return ""
	}
	return strings.TrimSpace(shell)
}

const fallbackShell = "/bin/zsh"

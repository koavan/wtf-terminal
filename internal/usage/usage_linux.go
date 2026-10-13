package usage

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	memoryKind = "rss"
	// WebKitGTK's helpers are wtf's children, so walking the tree finds them.
	supportsDetached = false
)

// userHZ is the unit of /proc's CPU times: fixed at 100 for user space.
const userHZ = 100

var pageSize = uint64(os.Getpagesize())

func readStat(pid int) (stat, bool) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return stat{}, false
	}
	name, fields, ok := parseStat(data)
	if !ok {
		return stat{}, false
	}
	st := stat{name: name, cpu: time.Duration(fields.utime+fields.stime) * time.Second / userHZ}
	if m, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/statm"); err == nil {
		if f := strings.Fields(string(m)); len(f) > 1 {
			if pages, err := strconv.ParseUint(f[1], 10, 64); err == nil {
				st.mem = pages * pageSize
			}
		}
	}
	return st, true
}

// children reads /proc/<pid>/task/*/children, falling back to every
// process's parent when the kernel doesn't offer those files.
func children(pid int) []int {
	tasks, _ := filepath.Glob("/proc/" + strconv.Itoa(pid) + "/task/*/children")
	if len(tasks) == 0 {
		return childrenByScan(pid)
	}
	var out []int
	for _, t := range tasks {
		data, err := os.ReadFile(t)
		if err != nil {
			continue
		}
		for _, f := range strings.Fields(string(data)) {
			if c, err := strconv.Atoi(f); err == nil {
				out = append(out, c)
			}
		}
	}
	return out
}

func childrenByScan(pid int) []int {
	entries, _ := os.ReadDir("/proc")
	var out []int
	for _, e := range entries {
		c, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		data, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue
		}
		if _, f, ok := parseStat(data); ok && f.ppid == pid {
			out = append(out, c)
		}
	}
	return out
}

func scanDetached(int) ([]detachedProc, string) { return nil, "" }

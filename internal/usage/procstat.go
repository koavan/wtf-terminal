package usage

import (
	"bytes"
	"strconv"
	"strings"
)

type statFields struct {
	ppid         int
	utime, stime uint64
}

// parseStat reads /proc/<pid>/stat: "pid (comm) state ppid … utime stime …".
// The name can hold spaces and parentheses, so it ends at the last ')'.
func parseStat(data []byte) (string, statFields, bool) {
	open, end := bytes.IndexByte(data, '('), bytes.LastIndexByte(data, ')')
	if open < 0 || end < open {
		return "", statFields{}, false
	}
	f := strings.Fields(string(data[end+1:]))
	// After the name: state(0) ppid(1) … utime(11) stime(12)
	if len(f) < 13 {
		return "", statFields{}, false
	}
	var s statFields
	s.ppid, _ = strconv.Atoi(f[1])
	s.utime, _ = strconv.ParseUint(f[11], 10, 64)
	s.stime, _ = strconv.ParseUint(f[12], 10, 64)
	return string(data[open+1 : end]), s, true
}

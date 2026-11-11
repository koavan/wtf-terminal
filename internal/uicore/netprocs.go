package uicore

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/dayanraj/wtf/internal/netconn"
	"github.com/dayanraj/wtf/internal/procs"
	"github.com/dayanraj/wtf/internal/usage"
)

// The Network, Processes and Resource Usage rules: addresses, which
// connections show, how processes sort and nest, and number formats. A port
// of macos/Sources/WtfCore/NetProcs.swift.

// NetSplitAddress splits "host:port" ("[v6]:port" for IPv6) into the host
// without brackets, and the port.
func NetSplitAddress(a string) (host, port string) {
	i := strings.LastIndexByte(a, ':')
	if i < 0 {
		return a, ""
	}
	host = a[:i]
	if len(host) >= 2 && strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
	}
	return host, a[i+1:]
}

// NetIsExposed is whether other machines can reach a listener on this host.
func NetIsExposed(host string) bool {
	return !(host == "localhost" || strings.HasPrefix(host, "127.") || host == "::1")
}

// NetMergeListeners keeps one row per program, protocol, port and
// exposure: 127.0.0.1:3000 and [::1]:3000 show once.
func NetMergeListeners(rows []netconn.Conn) []netconn.Conn {
	seen := map[string]bool{}
	var out []netconn.Conn
	for _, c := range rows {
		host, port := NetSplitAddress(c.Local)
		key := fmt.Sprintf("%d|%s|%s|%t", c.PID, c.Protocol, port, NetIsExposed(host))
		if !seen[key] {
			seen[key] = true
			out = append(out, c)
		}
	}
	return out
}

var netServices = map[string]string{"22": "ssh", "53": "dns", "80": "http", "443": "https", "993": "imaps", "587": "smtp", "3306": "mysql",
	"5432": "postgres", "6379": "redis", "27017": "mongodb", "9200": "elasticsearch", "5672": "amqp",
	"9092": "kafka", "11211": "memcached", "8080": "http-alt", "5353": "mdns"}

// NetServiceName is what usually listens on a port; "" when nothing well
// known does.
func NetServiceName(port string) string { return netServices[port] }

var pidFilter = regexp.MustCompile(`^pid:\s*(\d+)$`)

// NetVisible is the rows to show: a "pid:N" filter picks one process;
// Terminals hides what wtf's shells didn't start (except listeners of
// active forwards).
func NetVisible(conns []netconn.Conn, terminalsOnly bool, filter string, forwardPorts map[string]bool) []netconn.Conn {
	q := strings.ToLower(trimBlanks(filter))
	var out []netconn.Conn
	if m := pidFilter.FindStringSubmatch(q); m != nil {
		if pid, err := strconv.Atoi(m[1]); err == nil {
			for _, c := range conns {
				if c.PID == pid {
					out = append(out, c)
				}
			}
			return out
		}
	}
	for _, c := range conns {
		if terminalsOnly && c.Session == "" {
			_, port := NetSplitAddress(c.Local)
			if !(c.State == "LISTEN" && forwardPorts[port]) {
				continue
			}
		}
		if q == "" || strings.Contains(strings.ToLower(fmt.Sprintf("%s %s %s %d", c.Process, c.Local, c.Remote, c.PID)), q) {
			out = append(out, c)
		}
	}
	return out
}

// NetStateLabel is "close wait" for CLOSE_WAIT; "" for ESTABLISHED.
func NetStateLabel(s string) string {
	if s == "" || s == "ESTABLISHED" {
		return ""
	}
	return strings.ReplaceAll(strings.ToLower(s), "_", " ")
}

// ProcSort is how the Processes tab orders its list.
type ProcSort string

const (
	ProcSortCPU    ProcSort = "cpu"
	ProcSortMemory ProcSort = "memory"
	ProcSortName   ProcSort = "name"
)

// ProcCPU is "0.4%" or "23%".
func ProcCPU(c float64) string {
	if c < 10 {
		return fmt.Sprintf("%.1f%%", c)
	}
	return fmt.Sprintf("%.0f%%", c)
}

// ProcBytes is "1 KB", "50 MB" or "1.5 GB".
func ProcBytes(n int64) string {
	const mib, gib = 1024.0 * 1024, 1024.0 * 1024 * 1024
	d := float64(n)
	if d < mib {
		return fmt.Sprintf("%d KB", max(1, int(math.Round(d/1024))))
	}
	if d < gib {
		return fmt.Sprintf("%d MB", int(math.Round(d/mib)))
	}
	return fmt.Sprintf("%.1f GB", d/gib)
}

// ProcElapsed is "45s", "12m", "3h 5m" or "2d 4h".
func ProcElapsed(s int64) string {
	switch {
	case s < 60:
		return fmt.Sprintf("%ds", s)
	case s < 3600:
		return fmt.Sprintf("%dm", s/60)
	case s < 86400:
		return fmt.Sprintf("%dh %dm", s/3600, s%3600/60)
	}
	return fmt.Sprintf("%dd %dh", s/86400, s%86400/3600)
}

func ProcMatches(p procs.Proc, filter string) bool {
	q := strings.ToLower(trimBlanks(filter))
	return q == "" || strings.Contains(strings.ToLower(fmt.Sprintf("%s %s %d", p.Name, p.Command, p.PID)), q)
}

// ProcSorted is a sorted copy: busiest first, or by name then pid.
func ProcSorted(list []procs.Proc, by ProcSort) []procs.Proc {
	out := append([]procs.Proc(nil), list...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		switch by {
		case ProcSortCPU:
			if a.CPU != b.CPU {
				return a.CPU > b.CPU
			}
			return a.Memory > b.Memory
		case ProcSortMemory:
			if a.Memory != b.Memory {
				return a.Memory > b.Memory
			}
			return a.CPU > b.CPU
		}
		if an, bn := strings.ToLower(a.Name), strings.ToLower(b.Name); an != bn {
			return an < bn
		}
		return a.PID < b.PID
	})
	return out
}

// ProcNode is a process in a tree, with how deep it sits.
type ProcNode struct {
	Proc  procs.Proc
	Depth int
}

// ProcTree is one session's processes as a tree: the shell first,
// children by pid.
func ProcTree(list []procs.Proc) []ProcNode {
	pids := map[int]bool{}
	children := map[int][]procs.Proc{}
	for _, p := range list {
		pids[p.PID] = true
		children[p.PPID] = append(children[p.PPID], p)
	}
	for _, c := range children {
		sort.Slice(c, func(i, j int) bool { return c[i].PID < c[j].PID })
	}
	var roots []procs.Proc
	for _, p := range list {
		if p.Shell || !pids[p.PPID] {
			roots = append(roots, p)
		}
	}
	sort.Slice(roots, func(i, j int) bool {
		if roots[i].Shell != roots[j].Shell {
			return roots[i].Shell
		}
		return roots[i].PID < roots[j].PID
	})
	var out []ProcNode
	seen := map[int]bool{}
	var walk func(p procs.Proc, d int)
	walk = func(p procs.Proc, d int) {
		if seen[p.PID] {
			return
		}
		seen[p.PID] = true
		out = append(out, ProcNode{p, d})
		for _, c := range children[p.PID] {
			walk(c, d+1)
		}
	}
	for _, r := range roots {
		walk(r, 0)
	}
	return out
}

// UsageTotal is the app's own CPU (-1 before a second sample) and memory;
// your programs aren't counted.
func UsageTotal(u usage.Usage) (cpu float64, memory uint64) {
	cpu = -1
	if u.Interval > 0 {
		cpu = u.App.CPU + u.Window.CPU
	}
	return cpu, u.App.Memory + u.Window.Memory
}

// UsageCPU is "…" (no reading yet), "0.4%" or "12%".
func UsageCPU(c float64) string {
	if c < 0 {
		return "…"
	}
	if c < 10 {
		return fmt.Sprintf("%.1f%%", c)
	}
	return fmt.Sprintf("%d%%", int(math.Round(c)))
}

// UsageBytes is "512 B", "840 KB", "412 MB" or "1.2 GB".
func UsageBytes(n uint64) string {
	v := float64(n)
	units := []string{"B", "KB", "MB", "GB", "TB"}
	unit := 0
	for v >= 1024 && unit < len(units)-1 {
		v /= 1024
		unit++
	}
	if unit >= 3 {
		return fmt.Sprintf("%.1f %s", v, units[unit])
	}
	return fmt.Sprintf("%d %s", int(math.Round(v)), units[unit])
}

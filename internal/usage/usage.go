// Package usage measures wtf's own CPU and memory, for the address bar's
// resource readout: the app process, the WebKit processes that draw its
// windows, and — counted separately — the shells and programs run in it.
//
// It samples only when asked (the readout polls while it's on screen), with
// direct system calls: libproc on macOS, /proc on Linux. No ps, no top.
package usage

import (
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// Proc is one process.
type Proc struct {
	PID  int    `json:"pid"`
	Name string `json:"name"`
	// Percent of one CPU core since the previous sample; -1 on the first.
	CPU    float64 `json:"cpu"`
	Memory uint64  `json:"memory"`
}

// Group adds up some processes.
type Group struct {
	CPU    float64 `json:"cpu"`
	Memory uint64  `json:"memory"`
	Procs  []Proc  `json:"procs"`
}

type Usage struct {
	// wtf itself, and what it runs on its own (tunnels, git and kubectl checks…).
	App Group `json:"app"`
	// The WebKit processes that draw the windows.
	Window Group `json:"window"`
	// The shells and everything run in them.
	Programs Group `json:"programs"`
	// Seconds since the previous sample: CPU is over this time. 0 on the first.
	Interval float64 `json:"interval"`
	Cores    int     `json:"cores"`
	// What Memory measures: "footprint" (macOS, as Activity Monitor shows) or "rss" (Linux).
	MemoryKind string `json:"memoryKind"`
	// Why the window's processes couldn't be found, if they couldn't.
	Note string `json:"note"`
}

// maxProcs keeps a busy shell's process list short; the totals count every one.
const maxProcs = 40

// stat is one reading of a process.
type stat struct {
	cpu  time.Duration // user + system time so far
	mem  uint64
	name string
}

type Service struct {
	shells func() map[int]string
	self   int

	mu   sync.Mutex
	last time.Time
	prev map[int]time.Duration
	// macOS: processes wtf is responsible for that aren't its descendants,
	// found by a scan of every process, which is repeated only now and then.
	detached     []detachedProc
	detachedAt   time.Time
	detachedNote string
}

type detachedProc struct {
	pid    int
	webkit bool
}

// rescanEvery is how often the scan for detached processes is repeated.
const rescanEvery = 10 * time.Second

func NewService(shells func() map[int]string) *Service {
	return &Service{shells: shells, self: os.Getpid(), prev: map[int]time.Duration{}}
}

// Sample measures wtf's processes now.
func (s *Service) Sample() Usage {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	u := Usage{Cores: runtime.NumCPU(), MemoryKind: memoryKind}
	if !s.last.IsZero() {
		u.Interval = now.Sub(s.last).Seconds()
	}
	shells := map[int]string{}
	if s.shells != nil {
		shells = s.shells()
	}

	// Each process is read once per sample.
	stats := map[int]stat{}
	read := func(pid int) (stat, bool) {
		if st, ok := stats[pid]; ok {
			return st, true
		}
		st, ok := readStat(pid)
		if ok {
			stats[pid] = st
		}
		return st, ok
	}

	app, window, programs := []int{s.self}, []int{}, []int{}
	descendants := map[int]bool{s.self: true}
	for _, kid := range children(s.self) {
		tree := subtree(kid)
		for _, p := range tree {
			descendants[p] = true
		}
		switch {
		case shells[kid] != "":
			programs = append(programs, tree...)
		case anyWebKit(tree, read):
			window = append(window, tree...)
		default:
			app = append(app, tree...)
		}
	}
	if supportsDetached {
		if now.Sub(s.detachedAt) > rescanEvery {
			s.detached, s.detachedNote = scanDetached(s.self)
			s.detachedAt = now
		}
		u.Note = s.detachedNote
		for _, d := range s.detached {
			if descendants[d.pid] {
				continue
			}
			if d.webkit {
				window = append(window, d.pid)
			} else {
				programs = append(programs, d.pid)
			}
		}
	}

	seen := map[int]time.Duration{}
	measure := func(pids []int) Group {
		g := Group{Procs: []Proc{}}
		for _, pid := range pids {
			st, ok := read(pid)
			if !ok {
				continue
			}
			seen[pid] = st.cpu
			p := Proc{PID: pid, Name: st.name, CPU: -1, Memory: st.mem}
			if before, ok := s.prev[pid]; ok && u.Interval > 0 && st.cpu >= before {
				p.CPU = float64(st.cpu-before) / float64(now.Sub(s.last)) * 100
				g.CPU += p.CPU
			}
			g.Memory += p.Memory
			g.Procs = append(g.Procs, p)
		}
		sort.Slice(g.Procs, func(i, j int) bool { return g.Procs[i].Memory > g.Procs[j].Memory })
		if len(g.Procs) > maxProcs {
			g.Procs = g.Procs[:maxProcs]
		}
		return g
	}
	u.App, u.Window, u.Programs = measure(app), measure(window), measure(programs)
	s.prev, s.last = seen, now
	return u
}

// subtree is pid and every process under it.
func subtree(pid int) []int {
	out := []int{pid}
	for i := 0; i < len(out) && len(out) < 4096; i++ {
		out = append(out, children(out[i])...)
	}
	return out
}

// anyWebKit says whether a process tree belongs to WebKit: WebKitGTK's
// helpers (WebKitWebProcess…, perhaps under its bwrap sandbox).
func anyWebKit(pids []int, read func(int) (stat, bool)) bool {
	for _, p := range pids {
		if st, ok := read(p); ok && isWebKit(st.name) {
			return true
		}
	}
	return false
}

// isWebKit is a WebKit helper's process name: com.apple.WebKit.WebContent
// (macOS) or WebKitWebProcess (WebKitGTK).
func isWebKit(name string) bool {
	return strings.HasPrefix(name, "com.apple.WebKit.") || strings.HasPrefix(name, "WebKit")
}

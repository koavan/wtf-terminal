// Package procs lists the user's processes for the side pane, and works
// out which terminal session each one belongs to.
package procs

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Proc struct {
	PID  int `json:"pid"`
	PPID int `json:"ppid"`
	// Short name, e.g. "node"; Command is the full command line.
	Name    string  `json:"name"`
	Command string  `json:"command"`
	CPU     float64 `json:"cpu"`     // percent of one core
	Memory  int64   `json:"memory"`  // resident, in bytes
	Elapsed int64   `json:"elapsed"` // seconds since it started
	Session string  `json:"session"` // wtf terminal session it runs in, or ""
	Shell   bool    `json:"shell"`   // it is that session's shell
}

// Sessions reports the shell PID of each terminal session.
type Sessions func() map[int]string

type Service struct {
	sessions Sessions

	mu   sync.Mutex
	last map[int]bool // PIDs in the latest listing, which Kill may end
}

func NewService(sessions Sessions) *Service {
	return &Service{sessions: sessions}
}

// List returns the current user's processes.
func (s *Service) List() ([]Proc, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	nameField := "ucomm" // macOS: name without its path, spaces allowed
	if runtime.GOOS == "linux" {
		nameField = "comm"
	}
	uid := strconv.Itoa(os.Getuid())
	stats, err := exec.CommandContext(ctx, "ps", "-U", uid, "-o", "pid=,ppid=,pcpu=,rss=,etime=,"+nameField+"=").Output()
	if err != nil {
		return nil, fmt.Errorf("ps: %w", err)
	}
	args, _ := exec.CommandContext(ctx, "ps", "-U", uid, "-o", "pid=,args=").Output()

	procs := parseStats(stats)
	cmdlines := parseArgs(args)
	self := os.Getpid()
	shells := s.sessions()

	out := procs[:0]
	for _, p := range procs {
		// Leave out the ps we just ran.
		if p.PPID == self && p.Name == "ps" {
			continue
		}
		p.Command = cmdlines[p.PID]
		if p.Command == "" {
			p.Command = p.Name
		}
		out = append(out, p)
	}
	attribute(out, shells)

	pids := make(map[int]bool, len(out))
	for _, p := range out {
		pids[p.PID] = true
	}
	s.mu.Lock()
	s.last = pids
	s.mu.Unlock()
	return out, nil
}

// Kill ends a process from the latest listing: politely (SIGTERM), or
// right away when force is set (SIGKILL).
func (s *Service) Kill(pid int, force bool) error {
	s.mu.Lock()
	listed := s.last[pid]
	s.mu.Unlock()
	switch {
	case pid <= 1 || !listed:
		return fmt.Errorf("process %d isn't in the process list", pid)
	case pid == os.Getpid():
		return fmt.Errorf("that's wtf itself")
	}
	sig := syscall.SIGTERM
	if force {
		sig = syscall.SIGKILL
	}
	return syscall.Kill(pid, sig)
}

// parseStats reads `ps -o pid=,ppid=,pcpu=,rss=,etime=,ucomm=` output. The
// name comes last because it may contain spaces.
func parseStats(out []byte) []Proc {
	var procs []Proc
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		f := strings.Fields(line)
		if len(f) < 6 {
			continue
		}
		pid, err1 := strconv.Atoi(f[0])
		ppid, err2 := strconv.Atoi(f[1])
		cpu, err3 := strconv.ParseFloat(f[2], 64)
		rss, err4 := strconv.ParseInt(f[3], 10, 64)
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
			continue
		}
		// Everything after the fifth field is the name.
		rest := line
		for range 5 {
			rest = strings.TrimSpace(rest)
			rest = rest[strings.IndexFunc(rest, isSpace):]
		}
		procs = append(procs, Proc{
			PID:     pid,
			PPID:    ppid,
			CPU:     cpu,
			Memory:  rss * 1024,
			Elapsed: parseElapsed(f[4]),
			Name:    strings.TrimSpace(rest),
		})
	}
	return procs
}

func isSpace(r rune) bool { return r == ' ' || r == '\t' }

// parseArgs reads `ps -o pid=,args=` output into PID → command line.
func parseArgs(out []byte) map[int]string {
	args := map[int]string{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		pid, rest, ok := strings.Cut(strings.TrimSpace(sc.Text()), " ")
		if n, err := strconv.Atoi(pid); ok && err == nil {
			args[n] = strings.TrimSpace(rest)
		}
	}
	return args
}

// parseElapsed turns ps's "[[dd-]hh:]mm:ss" into seconds.
func parseElapsed(s string) int64 {
	var days int64
	if d, rest, ok := strings.Cut(s, "-"); ok {
		days, _ = strconv.ParseInt(d, 10, 64)
		s = rest
	}
	var total int64
	for _, part := range strings.Split(s, ":") {
		n, err := strconv.ParseInt(part, 10, 64)
		if err != nil {
			return 0
		}
		total = total*60 + n
	}
	return days*86400 + total
}

// attribute marks each process with the terminal session whose shell it
// descends from (or is).
func attribute(procs []Proc, shells map[int]string) {
	if len(shells) == 0 {
		return
	}
	parent := make(map[int]int, len(procs))
	for _, p := range procs {
		parent[p.PID] = p.PPID
	}
	for i := range procs {
		p := &procs[i]
		if id, ok := shells[p.PID]; ok {
			p.Session, p.Shell = id, true
			continue
		}
		for pid, hops := p.PPID, 0; pid > 1 && hops < 64; pid, hops = parent[pid], hops+1 {
			if id, ok := shells[pid]; ok {
				p.Session = id
				break
			}
		}
	}
}

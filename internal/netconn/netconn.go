// Package netconn lists network connections and listening ports, and
// works out which terminal session each one came from.
package netconn

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Conn struct {
	PID     int    `json:"pid"`
	Process string `json:"process"`
	// "TCP" or "UDP".
	Protocol string `json:"protocol"`
	// Local and Remote are "host:port" ("[v6]:port" for IPv6); Remote is ""
	// for a listening or unconnected socket.
	Local  string `json:"local"`
	Remote string `json:"remote"`
	// TCP state, e.g. "LISTEN" or "ESTABLISHED"; "" for UDP.
	State string `json:"state"`
	// The wtf terminal session whose shell started this process, or "".
	Session string `json:"session"`

	// For tools like ping that send through an unconnected socket (so the
	// system knows no remote address): the host named on their command
	// line, and its IP. TargetResolved means the IP came from a DNS lookup
	// here, so for hosts with several addresses the tool may use another.
	Target         string `json:"target"`
	TargetIP       string `json:"targetIP"`
	TargetResolved bool   `json:"targetResolved"`
}

// Sessions reports the shell PID of each terminal session.
type Sessions func() map[int]string

type Service struct {
	sessions Sessions
	resolver *resolver

	mu   sync.Mutex
	last map[int]bool // PIDs in the latest listing, which Kill may end
}

func NewService(sessions Sessions) *Service {
	return &Service{sessions: sessions, resolver: newResolver()}
}

// List returns current connections, listening sockets first. Without
// root this covers the user's own processes, which is the point.
func (s *Service) List() ([]Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var conns []Conn
	var err error
	if runtime.GOOS == "linux" {
		if path, lerr := exec.LookPath("ss"); lerr == nil {
			out, rerr := exec.CommandContext(ctx, path, "-H", "-tuanp").Output()
			conns, err = parseSS(out), rerr
		} else {
			conns, err = s.lsof(ctx)
		}
	} else {
		conns, err = s.lsof(ctx)
	}
	if err != nil {
		return nil, err
	}

	conns = dedupe(conns)
	addTargets(ctx, conns, commandLines, s.resolver.lookup)
	owners := owningSessions(ctx, conns, s.sessions())
	pids := map[int]bool{}
	for i := range conns {
		conns[i].Session = owners[conns[i].PID]
		pids[conns[i].PID] = true
	}
	sortConns(conns)

	s.mu.Lock()
	s.last = pids
	s.mu.Unlock()
	return conns, nil
}

func (s *Service) lsof(ctx context.Context) ([]Conn, error) {
	path, err := exec.LookPath("lsof")
	if err != nil {
		return nil, errors.New("lsof isn't installed")
	}
	out, err := exec.CommandContext(ctx, path, "-nP", "-i", "-F", "fpcPnTt", "-w").Output()
	// Exit status 1 means nothing was found, or some processes couldn't be
	// inspected; either way the output is usable.
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		return nil, err
	}
	return parseLsof(out), nil
}

// Kill asks a process from the latest listing to quit (SIGTERM).
func (s *Service) Kill(pid int) error {
	s.mu.Lock()
	listed := s.last[pid]
	s.mu.Unlock()
	if pid <= 1 || !listed {
		return fmt.Errorf("process %d isn't in the connection list", pid)
	}
	return syscall.Kill(pid, syscall.SIGTERM)
}

// parseLsof reads `lsof -F fpcPnTt` output (macOS lsof adds the "f"
// lines unasked, Linux lsof only when asked): a "p<pid>" and "c<command>"
// line per process, then per open file "f<fd>", "t<IPv4|IPv6>",
// "P<TCP|UDP>", "n<addresses>" and "TST=<state>" lines.
func parseLsof(out []byte) []Conn {
	var conns []Conn
	var pid int
	var command string
	var cur *Conn
	flush := func() {
		if cur != nil && cur.Local != "" {
			conns = append(conns, *cur)
		}
		cur = nil
	}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		v := line[1:]
		switch line[0] {
		case 'p':
			flush()
			pid, _ = strconv.Atoi(v)
		case 'c':
			command = v
		case 'f':
			flush()
			cur = &Conn{PID: pid, Process: command}
		case 'P':
			if cur != nil {
				cur.Protocol = v
			}
		case 'n':
			if cur != nil {
				cur.Local, cur.Remote, _ = strings.Cut(v, "->")
			}
		case 'T':
			if cur != nil && strings.HasPrefix(v, "ST=") {
				cur.State = v[3:]
			}
		}
	}
	flush()
	return conns
}

// parseSS reads `ss -H -tuanp` output:
//
//	tcp LISTEN 0 4096 127.0.0.1:5432 0.0.0.0:* users:(("postgres",pid=812,fd=6))
func parseSS(out []byte) []Conn {
	var conns []Conn
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 6 {
			continue
		}
		c := Conn{Protocol: strings.ToUpper(f[0]), Local: f[4], Remote: f[5]}
		if c.Protocol != "TCP" && c.Protocol != "UDP" {
			continue
		}
		state := f[1]
		if c.Protocol == "TCP" {
			c.State = map[string]string{"ESTAB": "ESTABLISHED", "LISTEN": "LISTEN"}[state]
			if c.State == "" {
				c.State = strings.ReplaceAll(state, "-", "_")
			}
		}
		if strings.HasSuffix(c.Remote, ":*") {
			c.Remote = ""
		}
		if len(f) >= 7 {
			// users:(("name",pid=123,fd=4),...) — take the first process.
			p := strings.Join(f[6:], " ")
			if i := strings.Index(p, `(("`); i >= 0 {
				rest := p[i+3:]
				name, after, _ := strings.Cut(rest, `"`)
				c.Process = name
				if j := strings.Index(after, "pid="); j >= 0 {
					num := after[j+4:]
					if k := strings.IndexAny(num, ",)"); k >= 0 {
						num = num[:k]
					}
					c.PID, _ = strconv.Atoi(num)
				}
			}
		}
		if c.PID == 0 {
			continue // another user's socket: nothing we can act on
		}
		conns = append(conns, c)
	}
	return conns
}

// dedupe drops copies of a socket a process holds through several
// descriptors (common after fork, and for dual-stack listeners).
func dedupe(conns []Conn) []Conn {
	seen := map[string]bool{}
	out := conns[:0]
	for _, c := range conns {
		key := fmt.Sprintf("%d|%s|%s|%s|%s", c.PID, c.Protocol, c.Local, c.Remote, c.State)
		if !seen[key] {
			seen[key] = true
			out = append(out, c)
		}
	}
	return out
}

// sortConns: listening first, then by process and address.
func sortConns(conns []Conn) {
	rank := func(c Conn) int {
		switch {
		case c.State == "LISTEN":
			return 0
		case c.Remote != "":
			return 1
		default:
			return 2
		}
	}
	sort.SliceStable(conns, func(i, j int) bool {
		a, b := conns[i], conns[j]
		if rank(a) != rank(b) {
			return rank(a) < rank(b)
		}
		if a.Process != b.Process {
			return strings.ToLower(a.Process) < strings.ToLower(b.Process)
		}
		return a.Local < b.Local
	})
}

// owningSessions maps each connection's PID to the terminal session whose
// shell is its ancestor.
func owningSessions(ctx context.Context, conns []Conn, shells map[int]string) map[int]string {
	owners := map[int]string{}
	if len(shells) == 0 || len(conns) == 0 {
		return owners
	}
	parents := parentMap(ctx)
	for _, c := range conns {
		if _, done := owners[c.PID]; done {
			continue
		}
		owners[c.PID] = ""
		for pid, hops := c.PID, 0; pid > 1 && hops < 64; pid, hops = parents[pid], hops+1 {
			if id, ok := shells[pid]; ok {
				owners[c.PID] = id
				break
			}
		}
	}
	return owners
}

// parentMap returns every process's parent PID, from `ps`.
func parentMap(ctx context.Context) map[int]int {
	out, err := exec.CommandContext(ctx, "ps", "-axo", "pid=,ppid=").Output()
	if err != nil {
		return nil
	}
	return parsePS(out)
}

func parsePS(out []byte) map[int]int {
	parents := map[int]int{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) != 2 {
			continue
		}
		pid, err1 := strconv.Atoi(f[0])
		ppid, err2 := strconv.Atoi(f[1])
		if err1 == nil && err2 == nil {
			parents[pid] = ppid
		}
	}
	return parents
}

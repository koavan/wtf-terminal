package netconn

import (
	"bufio"
	"bytes"
	"context"
	"net"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Tools that probe a host through an unconnected socket.
var probeTools = map[string]bool{
	"ping": true, "ping6": true, "traceroute": true, "traceroute6": true,
	"tracepath": true, "mtr": true, "mtr-packet": true, "fping": true, "nping": true,
}

// Options of those tools that take a value, which is never the target.
var valueFlags = map[string]bool{
	"-c": true, "-i": true, "-s": true, "-t": true, "-W": true, "-w": true, "-S": true,
	"-I": true, "-p": true, "-m": true, "-l": true, "-M": true, "-Q": true, "-g": true,
	"-G": true, "-h": true, "-T": true, "-b": true, "-k": true, "-K": true, "-z": true,
	"-f": true, "-q": true, "-P": true, "-N": true, "-r": true,
}

// probeTarget returns the host a probe tool's command line names, and
// whether it asks for IPv6.
func probeTarget(args string) (host string, v6, ok bool) {
	f := strings.Fields(args)
	if len(f) == 0 || !probeTools[filepath.Base(f[0])] {
		return "", false, false
	}
	tool := filepath.Base(f[0])
	v6 = strings.HasSuffix(tool, "6")
	for i := 1; i < len(f); i++ {
		t := f[i]
		if t == "-6" {
			v6 = true
		}
		if strings.HasPrefix(t, "-") {
			if valueFlags[t] {
				i++ // skip its value
			}
			continue
		}
		if looksLikeHost(t) {
			host = t // the last one wins: the target comes after the options
		}
	}
	return host, v6, host != ""
}

func looksLikeHost(t string) bool {
	if net.ParseIP(t) != nil {
		return true
	}
	// Numbers like "3" or "0.2" are option values, not hosts.
	if strings.Trim(t, "0123456789.") == "" {
		return false
	}
	return !strings.ContainsAny(t, "/=")
}

type commandLineFunc func(ctx context.Context, pids []int) map[int]string
type lookupFunc func(ctx context.Context, host string, v6 bool) string

// addTargets fills in Target for probe tools whose sockets have no remote.
func addTargets(ctx context.Context, conns []Conn, cmdlines commandLineFunc, lookup lookupFunc) {
	var pids []int
	for _, c := range conns {
		if c.Remote == "" && (c.Protocol == "ICMP" || probeTools[c.Process]) {
			pids = append(pids, c.PID)
		}
	}
	if len(pids) == 0 {
		return
	}
	args := cmdlines(ctx, pids)
	for i := range conns {
		c := &conns[i]
		if c.Remote != "" {
			continue
		}
		host, v6, ok := probeTarget(args[c.PID])
		if !ok {
			continue
		}
		c.Target = host
		if ip := net.ParseIP(host); ip != nil {
			c.TargetIP = ip.String()
		} else if ip := lookup(ctx, host, v6); ip != "" {
			c.TargetIP = ip
			c.TargetResolved = true
		}
	}
}

// commandLines returns the full command line of each PID, from `ps`.
func commandLines(ctx context.Context, pids []int) map[int]string {
	list := make([]string, len(pids))
	for i, p := range pids {
		list[i] = strconv.Itoa(p)
	}
	out, err := exec.CommandContext(ctx, "ps", "-o", "pid=,args=", "-p", strings.Join(list, ",")).Output()
	if err != nil && len(out) == 0 {
		return nil
	}
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

// resolver looks hosts up with a short timeout and remembers the answers,
// so a refresh every few seconds doesn't hit DNS each time.
type resolver struct {
	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	ip      string
	expires time.Time
}

const resolveTTL = time.Minute

func newResolver() *resolver { return &resolver{cache: map[string]cached{}} }

func (r *resolver) lookup(ctx context.Context, host string, v6 bool) string {
	key := host + "|" + strconv.FormatBool(v6)
	r.mu.Lock()
	if c, ok := r.cache[key]; ok && time.Now().Before(c.expires) {
		r.mu.Unlock()
		return c.ip
	}
	r.mu.Unlock()

	network := "ip4"
	if v6 {
		network = "ip6"
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	ip := ""
	if ips, err := net.DefaultResolver.LookupIP(ctx, network, host); err == nil && len(ips) > 0 {
		ip = ips[0].String()
	}
	r.mu.Lock()
	r.cache[key] = cached{ip: ip, expires: time.Now().Add(resolveTTL)}
	r.mu.Unlock()
	return ip
}

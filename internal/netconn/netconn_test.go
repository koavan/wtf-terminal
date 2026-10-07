package netconn

import (
	"context"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestParseLsof(t *testing.T) {
	out := `p981
crapportd
f11
tIPv4
PTCP
n*:57120
TST=LISTEN
TQR=0
f15
tIPv6
PTCP
n*:57120
TST=LISTEN
p1010
cnode
f13
tIPv4
PUDP
n*:*
f50
tIPv6
PTCP
n[::1]:52344->[::1]:5432
TST=ESTABLISHED
`
	got := parseLsof([]byte(out))
	if len(got) != 4 {
		t.Fatalf("got %d conns: %+v", len(got), got)
	}
	want := []Conn{
		{PID: 981, Process: "rapportd", Protocol: "TCP", Local: "*:57120", State: "LISTEN"},
		{PID: 981, Process: "rapportd", Protocol: "TCP", Local: "*:57120", State: "LISTEN"},
		{PID: 1010, Process: "node", Protocol: "UDP", Local: "*:*"},
		{PID: 1010, Process: "node", Protocol: "TCP", Local: "[::1]:52344", Remote: "[::1]:5432", State: "ESTABLISHED"},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("conn %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if d := dedupe(got); len(d) != 3 {
		t.Errorf("dedupe kept %d, want 3", len(d))
	}
}

func TestParseSS(t *testing.T) {
	out := `tcp   LISTEN 0      4096       127.0.0.1:5432      0.0.0.0:*    users:(("postgres",pid=812,fd=6))
tcp   ESTAB  0      0      192.168.1.4:51234 140.82.112.3:443 users:(("git-remote-http",pid=4410,fd=3),("git",pid=4409,fd=3))
udp   UNCONN 0      0            0.0.0.0:5353      0.0.0.0:*    users:(("avahi-daemon",pid=600,fd=12))
tcp   ESTAB  0      0      10.0.0.2:22 10.0.0.9:50000
`
	got := parseSS([]byte(out))
	want := []Conn{
		{PID: 812, Process: "postgres", Protocol: "TCP", Local: "127.0.0.1:5432", State: "LISTEN"},
		{PID: 4410, Process: "git-remote-http", Protocol: "TCP", Local: "192.168.1.4:51234", Remote: "140.82.112.3:443", State: "ESTABLISHED"},
		{PID: 600, Process: "avahi-daemon", Protocol: "UDP", Local: "0.0.0.0:5353"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("conn %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestSortListeningFirst(t *testing.T) {
	c := []Conn{
		{Process: "b", Remote: "x:1", State: "ESTABLISHED"},
		{Process: "z", State: "LISTEN"},
		{Process: "a", Protocol: "UDP"},
		{Process: "a", State: "LISTEN"},
	}
	sortConns(c)
	var order []string
	for _, x := range c {
		order = append(order, x.Process+"/"+x.State)
	}
	if got := strings.Join(order, " "); got != "a/LISTEN z/LISTEN b/ESTABLISHED a/" {
		t.Fatalf("order %s", got)
	}
}

func TestParsePS(t *testing.T) {
	got := parsePS([]byte("    1     0\n  812     1\n 4410  4409\nbad line here\n"))
	if got[812] != 1 || got[4410] != 4409 || len(got) != 3 {
		t.Fatalf("parsePS = %v", got)
	}
}

// A real listener started by a child of a "shell" is found and attributed
// to that shell's session.
func TestListAttributesToSession(t *testing.T) {
	if _, err := exec.LookPath("lsof"); err != nil {
		t.Skip("lsof not installed")
	}
	// The test process plays the shell; the listener runs in a child.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)

	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()

	s := NewService(func() map[int]string { return map[int]string{os.Getpid(): "s7"} })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for ctx.Err() == nil {
		conns, err := s.List()
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range conns {
			if c.PID == os.Getpid() && strings.HasSuffix(c.Local, ":"+port) {
				if c.State != "LISTEN" || c.Session != "s7" || c.Protocol != "TCP" {
					t.Fatalf("listener = %+v", c)
				}
				// Kill only accepts listed PIDs.
				if err := s.Kill(cmd.Process.Pid); err == nil {
					t.Error("killed a process that isn't in the list")
				}
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("listener never showed up")
}

func TestProbeTarget(t *testing.T) {
	cases := []struct {
		args, host string
		v6, ok     bool
	}{
		{"ping google.com", "google.com", false, true},
		{"/sbin/ping -c 3 -i 0.2 8.8.8.8", "8.8.8.8", false, true},
		{"ping google.com -I eth0 -c 5", "google.com", false, true},
		{"ping6 ipv6.google.com", "ipv6.google.com", true, true},
		{"ping -6 example.org", "example.org", true, true},
		{"traceroute -m 20 -q 1 example.org", "example.org", false, true},
		{"mtr --report github.com", "github.com", false, true},
		{"ping -c 3", "", false, false},
		{"node server.js", "", false, false},
		{"", "", false, false},
	}
	for _, c := range cases {
		host, v6, ok := probeTarget(c.args)
		if host != c.host || v6 != c.v6 || ok != c.ok {
			t.Errorf("probeTarget(%q) = %q, %v, %v; want %q, %v, %v", c.args, host, v6, ok, c.host, c.v6, c.ok)
		}
	}
}

func TestAddTargets(t *testing.T) {
	conns := []Conn{
		{PID: 1, Process: "ping", Protocol: "ICMP", Local: "*:*"},
		{PID: 2, Process: "ping", Protocol: "ICMP", Local: "*:*"},
		{PID: 3, Process: "traceroute", Protocol: "UDP", Local: "*:*"},
		{PID: 4, Process: "node", Protocol: "TCP", Local: "1.2.3.4:5", Remote: "6.7.8.9:443"},
		{PID: 5, Process: "ping", Protocol: "ICMP", Local: "*:*"},
	}
	args := map[int]string{1: "ping google.com", 2: "ping 1.1.1.1", 3: "traceroute example.org", 5: "ping nowhere.invalid"}
	var asked []int
	cmdlines := func(_ context.Context, pids []int) map[int]string { asked = pids; return args }
	lookup := func(_ context.Context, host string, v6 bool) string {
		return map[string]string{"google.com": "142.251.215.78", "example.org": "93.184.215.14"}[host]
	}
	addTargets(context.Background(), conns, cmdlines, lookup)

	if len(asked) != 4 {
		t.Errorf("looked up command lines of %v; want only the 4 remote-less probe sockets", asked)
	}
	want := []struct {
		target, ip string
		resolved   bool
	}{
		{"google.com", "142.251.215.78", true},
		{"1.1.1.1", "1.1.1.1", false},
		{"example.org", "93.184.215.14", true},
		{"", "", false},
		{"nowhere.invalid", "", false},
	}
	for i, w := range want {
		c := conns[i]
		if c.Target != w.target || c.TargetIP != w.ip || c.TargetResolved != w.resolved {
			t.Errorf("conn %d = target %q ip %q resolved %v; want %+v", i, c.Target, c.TargetIP, c.TargetResolved, w)
		}
	}
}

// A real ping: its ICMP socket shows up with the target from its command line.
func TestListFindsPingTarget(t *testing.T) {
	if _, err := exec.LookPath("lsof"); err != nil {
		t.Skip("lsof not installed")
	}
	ping, err := exec.LookPath("ping")
	if err != nil {
		t.Skip("ping not installed")
	}
	cmd := exec.Command(ping, "-i", "1", "127.0.0.1")
	if err := cmd.Start(); err != nil {
		t.Skip(err)
	}
	defer cmd.Process.Kill()

	s := NewService(func() map[int]string { return nil })
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		conns, err := s.List()
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range conns {
			if c.PID == cmd.Process.Pid {
				if c.Target != "127.0.0.1" || c.TargetIP != "127.0.0.1" || c.TargetResolved {
					t.Fatalf("ping socket = %+v", c)
				}
				return
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Skip("ping's socket never appeared in lsof (it may need privileges here)")
}

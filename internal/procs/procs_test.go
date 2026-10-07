package procs

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestParseStats(t *testing.T) {
	out := `  934     1   0.0   2864 05-06:36:40 distnoted
41000   940  12.5  20480    01:02:03 Google Chrome Helper (Renderer)
41001 41000   0.0   1024       00:07 node
garbage line
`
	got := parseStats([]byte(out))
	if len(got) != 3 {
		t.Fatalf("got %+v", got)
	}
	want := []Proc{
		{PID: 934, PPID: 1, CPU: 0, Memory: 2864 * 1024, Elapsed: 5*86400 + 6*3600 + 36*60 + 40, Name: "distnoted"},
		{PID: 41000, PPID: 940, CPU: 12.5, Memory: 20480 * 1024, Elapsed: 3723, Name: "Google Chrome Helper (Renderer)"},
		{PID: 41001, PPID: 41000, CPU: 0, Memory: 1024 * 1024, Elapsed: 7, Name: "node"},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("proc %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseElapsed(t *testing.T) {
	for in, want := range map[string]int64{"00:05": 5, "01:00": 60, "02:00:00": 7200, "1-00:00:01": 86401, "bad": 0} {
		if got := parseElapsed(in); got != want {
			t.Errorf("parseElapsed(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestParseArgs(t *testing.T) {
	got := parseArgs([]byte("  12 /usr/bin/node server.js --port 3000\n 13 vim  notes.md\n"))
	if got[12] != "/usr/bin/node server.js --port 3000" || got[13] != "vim  notes.md" {
		t.Fatalf("parseArgs = %q", got)
	}
}

func TestAttribute(t *testing.T) {
	procs := []Proc{
		{PID: 100, PPID: 1, Name: "bash"},     // shell of s1
		{PID: 101, PPID: 100, Name: "npm"},    // child
		{PID: 102, PPID: 101, Name: "node"},   // grandchild
		{PID: 200, PPID: 1, Name: "Finder"},   // unrelated
		{PID: 300, PPID: 1, Name: "zsh"},      // shell of s2
		{PID: 301, PPID: 999, Name: "orphan"}, // parent not listed
	}
	attribute(procs, map[int]string{100: "s1", 300: "s2"})
	want := map[int]struct {
		session string
		shell   bool
	}{100: {"s1", true}, 101: {"s1", false}, 102: {"s1", false}, 200: {"", false}, 300: {"s2", true}, 301: {"", false}}
	for _, p := range procs {
		w := want[p.PID]
		if p.Session != w.session || p.Shell != w.shell {
			t.Errorf("%s: session %q shell %v, want %q %v", p.Name, p.Session, p.Shell, w.session, w.shell)
		}
	}
}

// A live run: this test process plays a terminal's shell and starts a child.
func TestListLive(t *testing.T) {
	child := exec.Command("sleep", "30")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer child.Process.Kill()

	s := NewService(func() map[int]string { return map[int]string{os.Getpid(): "s1"} })
	procs, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	var me, kid *Proc
	for i := range procs {
		switch procs[i].PID {
		case os.Getpid():
			me = &procs[i]
		case child.Process.Pid:
			kid = &procs[i]
		}
		if procs[i].Name == "ps" && procs[i].PPID == os.Getpid() {
			t.Error("listed the ps it ran")
		}
	}
	if me == nil || !me.Shell || me.Session != "s1" {
		t.Fatalf("self = %+v", me)
	}
	if kid == nil || kid.Session != "s1" || kid.Shell || kid.Command != "sleep 30" || kid.Memory <= 0 {
		t.Fatalf("child = %+v", kid)
	}

	// Kill refuses unlisted PIDs and wtf itself, then ends the child.
	if err := s.Kill(1, false); err == nil {
		t.Error("killed pid 1")
	}
	if err := s.Kill(os.Getpid(), false); err == nil {
		t.Error("killed itself")
	}
	if err := s.Kill(child.Process.Pid, false); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- child.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("child didn't exit after Kill")
	}
}

package usage

import (
	"os/exec"
	"runtime"
	"testing"
	"time"
)

func TestParseStat(t *testing.T) {
	// The name can hold spaces and parentheses.
	line := "4242 (Web (Content) x) S 17 4242 4242 0 -1 4194560 100 0 0 0 250 75 0 0 20 0 1 0 100 1000 50"
	name, f, ok := parseStat([]byte(line))
	if !ok || name != "Web (Content) x" || f.ppid != 17 || f.utime != 250 || f.stime != 75 {
		t.Fatalf("parseStat = %q %+v %v", name, f, ok)
	}
	if _, _, ok := parseStat([]byte("garbage")); ok {
		t.Error("garbage should not parse")
	}
}

func TestIsWebKit(t *testing.T) {
	for name, want := range map[string]bool{
		"com.apple.WebKit.WebContent": true, "com.apple.WebKit.GPU": true, "WebKitWebProcess": true,
		"WebKitNetworkProcess": true, "zsh": false, "kubectl": false, "webkit-fake": false,
	} {
		if isWebKit(name) != want {
			t.Errorf("isWebKit(%q) = %v", name, !want)
		}
	}
}

// Real processes: wtf's own, a "shell" and what runs in it, and a helper
// wtf runs itself (a tunnel, say).
func TestSample(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("macOS and Linux only")
	}
	shell := exec.Command("sh", "-c", "sleep 30 & wait")
	helper := exec.Command("sleep", "30")
	for _, c := range []*exec.Cmd{shell, helper} {
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		defer c.Process.Kill()
	}
	time.Sleep(200 * time.Millisecond) // for the shell's sleep to start

	s := NewService(func() map[int]string { return map[int]string{shell.Process.Pid: "s1"} })
	first := s.Sample()
	if first.Interval != 0 || first.Cores < 1 || first.MemoryKind == "" {
		t.Fatalf("first sample: %+v", first)
	}
	self := find(first.App, s.self)
	if self == nil || self.Memory == 0 || self.CPU != -1 {
		t.Fatalf("wtf itself: %+v", first.App)
	}
	if find(first.App, helper.Process.Pid) == nil {
		t.Errorf("a helper wtf runs belongs to wtf: %+v", first.App)
	}
	if find(first.Programs, shell.Process.Pid) == nil || len(first.Programs.Procs) < 2 {
		t.Errorf("the shell and its sleep are programs: %+v", first.Programs)
	}

	// Burn some CPU: the next sample shows it.
	deadline := time.Now().Add(300 * time.Millisecond)
	for x := 0; time.Now().Before(deadline); x++ {
		_ = x * x
	}
	second := s.Sample()
	if second.Interval <= 0.25 {
		t.Fatalf("interval %v", second.Interval)
	}
	if me := find(second.App, s.self); me == nil || me.CPU < 30 || me.CPU > 100*float64(runtime.NumCPU()) {
		t.Errorf("CPU of a busy loop: %+v", me)
	}
	if second.App.CPU < find(second.App, s.self).CPU {
		t.Errorf("group CPU %v should add up its processes", second.App.CPU)
	}
}

func find(g Group, pid int) *Proc {
	for i := range g.Procs {
		if g.Procs[i].PID == pid {
			return &g.Procs[i]
		}
	}
	return nil
}

// What one sample costs: the readout takes one every 2 seconds while it's
// on screen; the full scan for detached processes runs every 10 seconds.
func BenchmarkSample(b *testing.B) {
	s := NewService(nil)
	s.Sample()
	b.Run("steady", func(b *testing.B) {
		for range b.N {
			s.Sample()
		}
	})
	b.Run("with full scan", func(b *testing.B) {
		for range b.N {
			s.detachedAt = time.Time{}
			s.Sample()
		}
	})
}

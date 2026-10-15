package terminal

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// wtf-exec starts a program with nothing blocked even when its parent blocks
// signals, as Go's threads do inside the native app. Without it, programs in
// wtf's shells ignore Ctrl-C, hang-ups and resizes.
func TestWtfExecClearsTheSignalMask(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("wtf-exec is for the macOS app")
	}
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not installed")
	}
	src, _ := filepath.Abs("../../macos/exec/wtf-exec.c")
	helper := filepath.Join(t.TempDir(), "wtf-exec")
	if out, err := exec.Command("cc", "-O2", "-o", helper, src).CombinedOutput(); err != nil {
		t.Skipf("can't build wtf-exec: %v\n%s", err, out)
	}
	// A parent that blocks SIGINT and SIGHUP starts a child two ways.
	script := `
import signal, subprocess, sys
signal.pthread_sigmask(signal.SIG_BLOCK, {signal.SIGINT, signal.SIGHUP})
probe = "import signal; print(sorted(s.name for s in signal.pthread_sigmask(signal.SIG_BLOCK, [])))"
direct = subprocess.run([sys.executable, "-c", probe], capture_output=True, text=True).stdout.strip()
helped = subprocess.run([sys.argv[1], sys.executable, "python3", "-c", probe], capture_output=True, text=True).stdout.strip()
print(direct + "|" + helped)
`
	out, err := exec.Command(py, "-c", script, helper).Output()
	if err != nil {
		t.Fatal(err)
	}
	direct, helped, _ := strings.Cut(strings.TrimSpace(string(out)), "|")
	if !strings.Contains(direct, "SIGINT") {
		t.Fatalf("the parent's mask should reach a direct child: %q", direct)
	}
	if helped != "[]" {
		t.Fatalf("wtf-exec left signals blocked: %q", helped)
	}
}

// With the launcher set, a session runs through it.
func TestLauncherWrapsTheShell(t *testing.T) {
	Launcher = func(path string, argv []string) (string, []string) {
		return "/usr/bin/env", append([]string{"env", "WTF_LAUNCHED=1", path}, argv[1:]...)
	}
	defer func() { Launcher = nil }()
	sess, err := startSession("t", spawnOptions{Shell: "/bin/sh", Args: []string{"sh", "-c", "echo launched=$WTF_LAUNCHED"},
		Cwd: t.TempDir(), Cols: 80, Rows: 24, Env: []string{"PATH=/usr/bin:/bin"}})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	var out strings.Builder
	for chunk := range sess.Output {
		out.Write(chunk)
	}
	if !strings.Contains(out.String(), "launched=1") {
		t.Fatalf("the shell didn't start through the launcher: %q", out.String())
	}
}

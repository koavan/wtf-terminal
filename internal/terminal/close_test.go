package terminal

import (
	"errors"
	"syscall"
	"testing"
	"time"
)

// Closing a session (wtf quitting, a tab closed) ends the program running in
// its foreground too, not just the shell: a dev server must not outlive it.
func TestCloseEndsTheForegroundProgram(t *testing.T) {
	_, _, sess := runIntegratedHome(t, "bash", nil, "sleep 300\r", "\x1b]133;C\x07")
	var pgid int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if g, err := sess.foreground(); err == nil && g != sess.cmd.Process.Pid {
			pgid = g
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if pgid == 0 {
		t.Fatal("sleep never became the foreground job")
	}
	sess.Close()
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(-pgid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
	t.Fatalf("the foreground job (group %d) outlived its session", pgid)
}

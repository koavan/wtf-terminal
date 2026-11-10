//go:build linux

package notify

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// NotifySound with stand-in notify-send and canberra-gtk-play that note
// their arguments: the daemon plays the sound when it can, else canberra.
func TestNotifySound(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"notify-send", "canberra-gtk-play"} {
		script := "#!/bin/sh\necho \"$*\" >> " + filepath.Join(dir, name+".log") + "\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	read := func(name string) string {
		b, _ := os.ReadFile(filepath.Join(dir, name+".log"))
		os.Remove(filepath.Join(dir, name+".log"))
		return strings.TrimSpace(string(b))
	}
	defer func(f func() bool) { daemonPlaysSounds = f }(daemonPlaysSounds)
	s := NewService()

	daemonPlaysSounds = func() bool { return true }
	if err := s.NotifySound("Claude Code needs you", "deploy", "Claude needs your permission to use Bash", SoundAttention); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if got := read("notify-send"); got != "--app-name=wtf --hint=string:sound-name:message-new-instant -- Claude Code needs you — deploy Claude needs your permission to use Bash" {
		t.Errorf("by the daemon: notify-send %q", got)
	}
	if got := read("canberra-gtk-play"); got != "" {
		t.Errorf("by the daemon: canberra played %q", got)
	}

	daemonPlaysSounds = func() bool { return false }
	s.NotifySound("Claude Code finished", "deploy", "fix it", SoundComplete)
	deadline := time.Now().Add(2 * time.Second)
	var played string
	for played == "" && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		b, _ := os.ReadFile(filepath.Join(dir, "canberra-gtk-play.log"))
		played = strings.TrimSpace(string(b))
	}
	if played != "--id=complete --description=wtf" {
		t.Errorf("without the daemon: canberra %q", played)
	}
	if got := read("notify-send"); strings.Contains(got, "sound-name") {
		t.Errorf("without the daemon: notify-send %q", got)
	}

	// No sound asked: neither plays one.
	read("canberra-gtk-play")
	s.Notify("t", "", "b")
	time.Sleep(200 * time.Millisecond)
	if got := read("canberra-gtk-play"); got != "" || strings.Contains(read("notify-send"), "sound") {
		t.Errorf("Notify played %q", got)
	}
}

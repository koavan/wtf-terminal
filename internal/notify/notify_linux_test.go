//go:build linux

package notify

import (
	"os"
	"os/exec"
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

// NotifyActions with a stand-in notify-send that prints its id and "clicks"
// the button its test names; Close asks the daemon through gdbus.
func TestNotifyActions(t *testing.T) {
	dir := t.TempDir()
	write := func(name, script string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write("notify-send", `echo "$*" >> `+filepath.Join(dir, "args")+`
echo 42
read k < `+filepath.Join(dir, "click")+`
echo "$k"
`)
	write("gdbus", `echo "$*" >> `+filepath.Join(dir, "gdbus")+"\n")
	t.Setenv("PATH", dir)
	defer func(f, g func() bool) { daemonHasActions, daemonPlaysSounds = f, g }(daemonHasActions, daemonPlaysSounds)
	daemonHasActions = func() bool { return true }
	daemonPlaysSounds = func() bool { return false }
	if err := exec.Command("mkfifo", filepath.Join(dir, "click")).Run(); err != nil {
		t.Skip("no mkfifo")
	}
	s := NewService()
	asked, err := Ask(s, "Claude Code needs you", "deploy", "Bash: make deploy", "", []Action{{"allow", "Allow"}, {"deny", "Deny"}})
	if err != nil || asked == nil || asked.id != "42" {
		t.Fatalf("asked %+v, %v", asked, err)
	}
	os.WriteFile(filepath.Join(dir, "click"), []byte("allow\n"), 0o644)
	select {
	case k := <-asked.Chosen:
		if k != "allow" {
			t.Fatalf("chosen %q", k)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no answer")
	}
	b, _ := os.ReadFile(filepath.Join(dir, "args"))
	if got := strings.TrimSpace(string(b)); got != "--app-name=wtf --print-id --wait --action=allow=Allow --action=deny=Deny -- Claude Code needs you — deploy Bash: make deploy" {
		t.Errorf("notify-send %q", got)
	}
	asked.Close()
	b, _ = os.ReadFile(filepath.Join(dir, "gdbus"))
	if !strings.Contains(string(b), "CloseNotification 42") {
		t.Errorf("gdbus %q", b)
	}

	// Without the daemon's buttons: a plain notification, nothing to wait on.
	daemonHasActions = func() bool { return false }
	write("notify-send", `echo "$*" > `+filepath.Join(dir, "args")+"\n")
	if asked, err := Ask(s, "t", "", "b", "", []Action{{"allow", "Allow"}}); asked != nil || err != nil {
		t.Fatalf("without actions: %+v %v", asked, err)
	}
}

// Post gives the notification's id, replaces the one it names, and falls
// back to a plain one with a notify-send that has no ids; Close closes it.
func TestPostReplaces(t *testing.T) {
	dir := t.TempDir()
	write := func(script string) {
		if err := os.WriteFile(filepath.Join(dir, "notify-send"), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(dir, "gdbus"), []byte("#!/bin/sh\necho \"$*\" >> "+filepath.Join(dir, "gdbus.log")+"\n"), 0o755)
	t.Setenv("PATH", dir)
	defer func(f func() bool) { daemonPlaysSounds = f }(daemonPlaysSounds)
	daemonPlaysSounds = func() bool { return false }
	log := filepath.Join(dir, "args")
	write(`echo "$*" >> ` + log + "\necho 7\n")
	s := NewService()
	if id := Post(s, "", "Claude Code needs you", "deploy", "Bash: make", ""); id != "7" {
		t.Fatalf("first id %q", id)
	}
	if id := Post(s, "7", "Claude Code finished", "deploy", "make", ""); id != "7" {
		t.Fatalf("second id %q", id)
	}
	b, _ := os.ReadFile(log)
	want := "--app-name=wtf --print-id -- Claude Code needs you — deploy Bash: make\n" +
		"--app-name=wtf --print-id --replace-id=7 -- Claude Code finished — deploy make\n"
	if string(b) != want {
		t.Errorf("notify-send:\n%s", b)
	}
	Close("7")
	if b, _ := os.ReadFile(filepath.Join(dir, "gdbus.log")); !strings.Contains(string(b), "CloseNotification 7") {
		t.Errorf("gdbus %q", b)
	}

	// libnotify before 0.7.9: no --print-id, so a plain one.
	os.Remove(log)
	write(`case "$*" in *--print-id*) exit 1;; esac
echo "$*" >> ` + log + "\n")
	if id := Post(s, "", "t", "", "b", ""); id != "" {
		t.Fatalf("old notify-send id %q", id)
	}
	if b, _ := os.ReadFile(log); strings.TrimSpace(string(b)) != "--app-name=wtf -- t b" {
		t.Errorf("fallback %q", b)
	}
}

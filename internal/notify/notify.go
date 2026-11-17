// Package notify shows desktop notifications.
//
// It shells out to the platform's notification tool, which needs no
// bundled .app on macOS.
package notify

import (
	"bufio"
	"context"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const maxLen = 240

type Service struct{}

func NewService() *Service { return &Service{} }

// Sounds from the freedesktop sound theme, for NotifySound on Linux (the
// macOS app plays its own).
const (
	// Something needs you.
	SoundAttention = "message-new-instant"
	// Something finished.
	SoundComplete = "complete"
)

// Notify shows a notification. Text is passed as arguments, never through a
// shell or script source, so it can't inject anything.
func (s *Service) Notify(title, subtitle, body string) error {
	return s.NotifySound(title, subtitle, body, "")
}

// NotifySound shows a notification with a short sound (a sound theme name;
// "" for none). On Linux the notification daemon plays it when it can, so
// the desktop's sound theme and Do Not Disturb apply; otherwise
// canberra-gtk-play does, when it's installed. On macOS the sound is the
// app's to play.
func (s *Service) NotifySound(title, subtitle, body, sound string) error {
	title, subtitle, body = clip(title), clip(subtitle), clip(body)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "/usr/bin/osascript",
			"-e", "on run argv",
			"-e", "display notification (item 3 of argv) with title (item 1 of argv) subtitle (item 2 of argv)",
			"-e", "end run",
			title, subtitle, body)
	default:
		byDaemon := sound != "" && daemonPlaysSounds()
		if sound != "" && !byDaemon {
			go playSound(sound)
		}
		path, err := exec.LookPath("notify-send")
		if err != nil {
			return nil // no notification daemon tooling; stay silent
		}
		summary := title
		if subtitle != "" {
			summary += " — " + subtitle
		}
		args := []string{"--app-name=wtf"}
		if byDaemon {
			args = append(args, "--hint=string:sound-name:"+sound)
		}
		cmd = exec.CommandContext(ctx, path, append(args, "--", summary, body)...)
	}
	return cmd.Run()
}

// Post shows a notification in the place of the one replace names ("" for
// none), so an agent's don't pile up, and gives its id for the next one or
// Close ("" when it can't tell: macOS, or notify-send before libnotify
// 0.7.9, which shows a plain one). Not a Service method: only the Linux
// app calls it, directly.
func Post(s *Service, replace, title, subtitle, body, sound string) string {
	path, err := exec.LookPath("notify-send")
	if runtime.GOOS == "darwin" || err != nil {
		_ = s.NotifySound(title, subtitle, body, sound)
		return ""
	}
	title, subtitle, body = clip(title), clip(subtitle), clip(body)
	byDaemon := sound != "" && daemonPlaysSounds()
	if sound != "" && !byDaemon {
		go playSound(sound)
	}
	summary := title
	if subtitle != "" {
		summary += " — " + subtitle
	}
	args := []string{"--app-name=wtf", "--print-id"}
	if replace != "" {
		args = append(args, "--replace-id="+replace)
	}
	if byDaemon {
		args = append(args, "--hint=string:sound-name:"+sound)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, append(args, "--", summary, body)...).Output()
	if err != nil {
		// An older notify-send: no ids, so no replacing.
		_ = s.NotifySound(title, subtitle, body, "")
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Close takes a notification Post showed away (it's out of date).
func Close(id string) {
	if id != "" && runtime.GOOS != "darwin" {
		closeNotification(id)
	}
}

// Action is a button on a notification: its key, and its label.
type Action struct{ Key, Label string }

// Asked is a notification with buttons, shown by NotifyActions.
type Asked struct {
	// Chosen receives the key of the button clicked, or "" when the
	// notification goes without one (dismissed, expired, or closed).
	Chosen <-chan string
	id     string
	cancel context.CancelFunc
}

// Close takes the notification away (it was answered elsewhere).
func (a *Asked) Close() {
	if a == nil {
		return
	}
	if a.id != "" {
		closeNotification(a.id)
	}
	a.cancel()
}

// Ask shows a notification with buttons, on Linux, when the notification
// daemon has them (its "actions" capability) and notify-send can wait for
// one (libnotify 0.7.9 and later). Otherwise, and on macOS (whose app posts
// its own), it shows a plain notification and returns nil. Not a Service
// method: only the Linux app calls it, directly.
func Ask(s *Service, title, subtitle, body, sound string, actions []Action) (*Asked, error) {
	if runtime.GOOS == "darwin" || !daemonHasActions() {
		return nil, s.NotifySound(title, subtitle, body, sound)
	}
	path, err := exec.LookPath("notify-send")
	if err != nil {
		return nil, nil
	}
	title, subtitle, body = clip(title), clip(subtitle), clip(body)
	byDaemon := sound != "" && daemonPlaysSounds()
	if sound != "" && !byDaemon {
		go playSound(sound)
	}
	summary := title
	if subtitle != "" {
		summary += " — " + subtitle
	}
	args := []string{"--app-name=wtf", "--print-id", "--wait"}
	if byDaemon {
		args = append(args, "--hint=string:sound-name:"+sound)
	}
	for _, a := range actions {
		args = append(args, "--action="+a.Key+"="+a.Label)
	}
	// It waits for a click; a day is longer than any approval waits.
	ctx, cancel := context.WithTimeout(context.Background(), 24*time.Hour)
	cmd := exec.CommandContext(ctx, path, append(args, "--", summary, body)...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, s.NotifySound(title, subtitle, body, "")
	}
	ch := make(chan string, 1)
	asked := &Asked{Chosen: ch, cancel: cancel}
	lines := bufio.NewScanner(out)
	// The first line is the notification's id, the next the button's key.
	if lines.Scan() {
		asked.id = strings.TrimSpace(lines.Text())
	}
	go func() {
		chosen := ""
		if lines.Scan() {
			chosen = strings.TrimSpace(lines.Text())
		}
		cmd.Wait()
		cancel()
		ch <- chosen
	}()
	return asked, nil
}

// closeNotification asks the daemon to take a notification away.
func closeNotification(id string) {
	path, err := exec.LookPath("gdbus")
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = exec.CommandContext(ctx, path, "call", "--session", "--dest", "org.freedesktop.Notifications",
		"--object-path", "/org/freedesktop/Notifications", "--method", "org.freedesktop.Notifications.CloseNotification", id).Run()
}

// daemonHasActions: the notification daemon shows buttons (its "actions"
// capability), asked once.
var daemonHasActions = sync.OnceValue(func() bool { return daemonCapability("actions") })

// PlaySound plays a sound theme sound by itself (Settings' preview), on
// Linux; it does nothing elsewhere.
func (s *Service) PlaySound(sound string) {
	if runtime.GOOS != "darwin" && sound != "" {
		go playSound(sound)
	}
}

func playSound(sound string) {
	path, err := exec.LookPath("canberra-gtk-play")
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = exec.CommandContext(ctx, path, "--id="+sound, "--description=wtf").Run()
}

// daemonPlaysSounds: the notification daemon says it has the "sound"
// capability (asked once, through gdbus).
var daemonPlaysSounds = sync.OnceValue(func() bool { return daemonCapability("sound") })

// daemonCapability asks the notification daemon, through gdbus, whether it
// has a capability.
func daemonCapability(name string) bool {
	path, err := exec.LookPath("gdbus")
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "call", "--session", "--dest", "org.freedesktop.Notifications",
		"--object-path", "/org/freedesktop/Notifications", "--method", "org.freedesktop.Notifications.GetCapabilities").Output()
	return err == nil && hasCapability(string(out), name)
}

// hasCapability reads gdbus's answer: (['actions', 'body', 'sound'],)
func hasCapability(out, name string) bool {
	return strings.Contains(out, "'"+name+"'")
}

func clip(s string) string {
	if len(s) <= maxLen {
		return s
	}
	s = s[:maxLen]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s + "…"
}

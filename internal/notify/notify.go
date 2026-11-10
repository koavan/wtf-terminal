// Package notify shows desktop notifications.
//
// It shells out to the platform's notification tool instead of using the
// Wails notification service, which requires a bundled .app on macOS.
package notify

import (
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
var daemonPlaysSounds = sync.OnceValue(func() bool {
	path, err := exec.LookPath("gdbus")
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "call", "--session", "--dest", "org.freedesktop.Notifications",
		"--object-path", "/org/freedesktop/Notifications", "--method", "org.freedesktop.Notifications.GetCapabilities").Output()
	return err == nil && hasCapability(string(out), "sound")
})

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

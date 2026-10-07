// Package notify shows desktop notifications.
//
// It shells out to the platform's notification tool instead of using the
// Wails notification service, which requires a bundled .app on macOS.
package notify

import (
	"context"
	"os/exec"
	"runtime"
	"time"
	"unicode/utf8"
)

const maxLen = 240

type Service struct{}

func NewService() *Service { return &Service{} }

// Notify shows a notification. Text is passed as arguments, never through a
// shell or script source, so it can't inject anything.
func (s *Service) Notify(title, subtitle, body string) error {
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
		path, err := exec.LookPath("notify-send")
		if err != nil {
			return nil // no notification daemon tooling; stay silent
		}
		summary := title
		if subtitle != "" {
			summary += " — " + subtitle
		}
		cmd = exec.CommandContext(ctx, path, "--app-name=wtf", "--", summary, body)
	}
	return cmd.Run()
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

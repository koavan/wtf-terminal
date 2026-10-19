package uicore

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// How the Share & Record tab exports output: file names, the recording
// clock, and the Markdown transcript. Port of lib/transcript.ts (the
// Export part of macos/Sources/WtfCore/History.swift).

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// ExportName is `wtf-kubectl-get-pods-n-shop-2026-10-09-2305`, in at's
// time zone.
func ExportName(title string, at time.Time) string {
	slug := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if len(slug) > 40 {
		slug = slug[:40]
	}
	if slug == "" {
		slug = "terminal"
	}
	return "wtf-" + slug + at.Format("-2006-01-02-1504")
}

// ExportElapsed is `M:SS`, or `H:MM:SS` from an hour.
func ExportElapsed(seconds int) string {
	s := max(0, seconds)
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// ExportBlock is one command for the transcript.
type ExportBlock struct {
	Text string
	// nil once it has left the scrollback.
	Output *string
	// Zero when unknown.
	StartedAt time.Time
	// nil while it runs.
	ExitCode *int
	Duration *time.Duration
}

func longestTicks(s string) int {
	best, run := 0, 0
	for _, r := range s {
		if r == '`' {
			run++
			best = max(best, run)
		} else {
			run = 0
		}
	}
	return best
}

// ExportFence is a code fence longer than any run of backticks in text.
func ExportFence(text string) string { return strings.Repeat("`", max(3, longestTicks(text)+1)) }

// ExportCode is text as inline code, whatever backticks it holds.
func ExportCode(text string) string {
	ticks := strings.Repeat("`", longestTicks(text)+1)
	pad := ""
	if strings.HasPrefix(text, "`") || strings.HasSuffix(text, "`") {
		pad = " "
	}
	return ticks + pad + text + pad + ticks
}

// ExportMarkdown is the Markdown transcript: each command with how it went
// and its output. Times show in loc (nil for local time).
func ExportMarkdown(blocks []ExportBlock, title, cwd string, exportedAt time.Time, loc *time.Location) string {
	if loc == nil {
		loc = time.Local
	}
	const clock = "15:04:05"
	n := len(blocks)
	head := "Terminal transcript from wtf, saved " + exportedAt.In(loc).Format("Jan 2, 2006 "+clock)
	if cwd != "" {
		head += " · " + cwd
	}
	if n == 1 {
		head += " · 1 command"
	} else {
		head += fmt.Sprintf(" · %d commands", n)
	}
	lines := []string{"# " + title, "", head}
	for i, b := range blocks {
		text := b.Text
		if text == "" {
			text = "(no command text)"
		}
		lines = append(lines, "", fmt.Sprintf("## %d. %s", i+1, ExportCode(text)), "")
		var facts []string
		switch {
		case b.ExitCode == nil:
			facts = append(facts, "still running")
		case *b.ExitCode == 0:
			facts = append(facts, "✓ succeeded")
		default:
			facts = append(facts, fmt.Sprintf("✗ exit %d", *b.ExitCode))
		}
		if !b.StartedAt.IsZero() {
			facts = append(facts, "started "+b.StartedAt.In(loc).Format(clock))
		}
		if b.Duration != nil {
			facts = append(facts, "took "+Duration(*b.Duration))
		}
		lines = append(lines, strings.Join(facts, " · "))
		switch {
		case b.Output == nil:
			lines = append(lines, "", "_Output no longer in the scrollback._")
		case *b.Output == "":
			lines = append(lines, "", "_No output._")
		default:
			f := ExportFence(*b.Output)
			lines = append(lines, "", f+"text", *b.Output, f)
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

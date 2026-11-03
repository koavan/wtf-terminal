package logsum

import (
	"strings"
	"testing"
	"time"

	"github.com/dayanraj/wtf/internal/uicore"
)

const sample = `2026-11-03T10:00:01Z INFO request id=1234 took 12ms path="/api/a"
2026-11-03T10:00:02Z INFO request id=1235 took 9ms path="/api/b"
2026-11-03T10:00:03Z ERROR db: connection refused to 10.0.0.5:5432 (attempt 1)
2026-11-03T10:00:04Z ERROR db: connection refused to 10.0.0.6:5432 (attempt 2)
2026-11-03T10:07:09Z WARN pod checkout-api-7f9c8d5b4-x2k4z restarted 3 times
2026-11-03T10:07:30Z ERROR db: connection refused to 10.0.0.5:5432 (attempt 3)
2026-11-03T10:08:00Z INFO request id=99 took 1.5s path="/api/c"
`

func TestSummarize(t *testing.T) {
	s := Summarize(sample, time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC), time.UTC)
	if s.Lines != 7 || len(s.Patterns) != 3 {
		t.Fatalf("%d lines, patterns %+v", s.Lines, s.Patterns)
	}
	db, warn, req := s.Patterns[0], s.Patterns[1], s.Patterns[2]
	if db.Level != uicore.LogError || db.Count != 3 || !strings.Contains(db.Template, "connection refused to <ip> (attempt <n>)") {
		t.Errorf("first = %+v", db)
	}
	if db.First.Format("15:04:05") != "10:00:03" || db.Last.Format("15:04:05") != "10:07:30" {
		t.Errorf("db seen %v – %v", db.First, db.Last)
	}
	if warn.Level != uicore.LogWarn || !strings.Contains(warn.Template, "pod checkout-api-<pod> restarted <n> times") {
		t.Errorf("second = %+v", warn)
	}
	if req.Count != 3 || !strings.Contains(req.Template, "request id=<n> took <dur> path=<str>") {
		t.Errorf("third = %+v", req)
	}
	if s.Step != time.Minute || len(s.Timeline) != 8 || s.Timeline[0].Errors != 2 || s.Timeline[7].Errors != 1 || s.Timeline[7].Warns != 1 {
		t.Errorf("timeline %v %+v", s.Step, s.Timeline)
	}
	text := s.Text(2)
	for _, want := range []string{"7 lines, 3 patterns", "1. ×3 [error] ", "first 2026-11-03 10:00:03 UTC, last 2026-11-03 10:07:30 UTC",
		"… 1 more patterns, 3 lines", "Errors and warnings per 1 minutes:"} {
		if !strings.Contains(text, want) {
			t.Errorf("text lacks %q:\n%s", want, text)
		}
	}
}

func TestNoTimestamps(t *testing.T) {
	s := Summarize("panic: boom\nat main.go:12\nat main.go:40\n\n", time.Now(), time.UTC)
	if s.Lines != 3 || len(s.Timeline) != 0 || len(s.Patterns) != 2 || s.Patterns[0].Template != "at main.go:<n>" || s.Patterns[0].Count != 2 {
		t.Fatalf("%+v", s)
	}
}

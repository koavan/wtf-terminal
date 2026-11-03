// Package logsum groups a command's log output into patterns for the wtf
// plugin's log_patterns tool (#28): lines that differ only in numbers, ids,
// addresses, durations, timestamps and quoted values are one pattern, with
// its count, log level, first and last time seen, and an example. It's
// deterministic; Claude writes the summary from it.
package logsum

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/dayanraj/wtf/internal/uicore"
)

const (
	// Lines longer than this are cut before grouping.
	maxLine = 2000
	// An example line is cut to this many bytes.
	maxExample = 240
)

// Pattern is one group of similar lines.
type Pattern struct {
	// The line with its variable parts as <n>, <id>, <ip>…
	Template string
	// The first log level seen on its lines ("" for none).
	Level       uicore.LogLevel
	Count       int
	First, Last time.Time
	Example     string
}

// Bucket is a stretch of time on the timeline.
type Bucket struct {
	Start          time.Time
	Errors, Warns int
}

// Summary is what Summarize found.
type Summary struct {
	// Non-empty lines read.
	Lines int
	// Most important first: errors, warnings, the rest; then by count.
	Patterns []Pattern
	// Error and warning lines over time, when lines have timestamps.
	Timeline []Bucket
	Step     time.Duration
}

var (
	uuidRe = regexp.MustCompile(`\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b`)
	ipRe   = regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}(?::\d+)?(?:/\d+)?\b`)
	// Hex ids with at least one letter and one digit (commit hashes, pod
	// suffixes, request ids), 6 or more long.
	hexRe  = regexp.MustCompile(`\b[0-9a-f]*[a-f][0-9a-f]*\d[0-9a-f]*\b|\b[0-9a-f]*\d[0-9a-f]*[a-f][0-9a-f]*\b`)
	durRe  = regexp.MustCompile(`\b\d+(?:\.\d+)?(?:ns|us|µs|ms|s|m|h)\b`)
	quotRe = regexp.MustCompile(`"[^"]*"|'[^']*'`)
	numRe  = regexp.MustCompile(`\d+(?:\.\d+)?`)
	// A pod's generated suffix: name-<hash>-<5 chars>.
	podRe  = regexp.MustCompile(`\b([a-z0-9]+(?:-[a-z0-9]+)*)-[a-z0-9]{8,10}-[a-z0-9]{5}\b`)
	spaces = regexp.MustCompile(`\s+`)
)

// template is a line with its variable parts replaced.
func template(line string, tokens []uicore.LogToken) string {
	// Timestamps first, by the scanner's own spans (they hold digits the
	// number rule would split up).
	for i := len(tokens) - 1; i >= 0; i-- {
		t := tokens[i]
		if t.Kind == uicore.LogKindTimestamp || t.Kind == uicore.LogKindTime {
			line = line[:t.Start] + "<time>" + line[t.End:]
		}
	}
	line = uuidRe.ReplaceAllString(line, "<id>")
	line = ipRe.ReplaceAllString(line, "<ip>")
	line = podRe.ReplaceAllString(line, "$1-<pod>")
	line = hexRe.ReplaceAllStringFunc(line, func(m string) string {
		if len(m) < 6 {
			return m
		}
		return "<id>"
	})
	line = durRe.ReplaceAllString(line, "<dur>")
	line = quotRe.ReplaceAllString(line, "<str>")
	line = numRe.ReplaceAllString(line, "<n>")
	return strings.TrimSpace(spaces.ReplaceAllString(line, " "))
}

func rank(l uicore.LogLevel) int {
	switch l {
	case uicore.LogError:
		return 0
	case uicore.LogWarn:
		return 1
	}
	return 2
}

// Summarize groups output's lines. now and loc read timestamps without a
// year or zone, as the terminal shows them.
func Summarize(output string, now time.Time, loc *time.Location) Summary {
	var s Summary
	byTemplate := map[string]int{}
	var stamps []stamp
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimRight(line, "\r ")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if len(line) > maxLine {
			line = line[:maxLine]
		}
		s.Lines++
		tokens := uicore.LogScan(line)
		var level uicore.LogLevel
		var at time.Time
		for _, t := range tokens {
			switch {
			case t.Kind == uicore.LogKindLevel && level == "":
				level = t.Level
			case t.Kind == uicore.LogKindTimestamp && at.IsZero():
				if v, _, ok := uicore.LogParseTimestamp(t.Text, now, loc); ok {
					at = v
				}
			}
		}
		tpl := template(line, tokens)
		i, ok := byTemplate[tpl]
		if !ok {
			i = len(s.Patterns)
			byTemplate[tpl] = i
			ex := line
			if len(ex) > maxExample {
				ex = ex[:maxExample] + "…"
			}
			s.Patterns = append(s.Patterns, Pattern{Template: tpl, Level: level, Example: ex})
		}
		p := &s.Patterns[i]
		p.Count++
		if p.Level == "" {
			p.Level = level
		}
		if !at.IsZero() {
			if p.First.IsZero() || at.Before(p.First) {
				p.First = at
			}
			if at.After(p.Last) {
				p.Last = at
			}
			if level == uicore.LogError || level == uicore.LogWarn {
				stamps = append(stamps, stamp{at, level})
			}
		}
	}
	slices.SortStableFunc(s.Patterns, func(a, b Pattern) int {
		if r := rank(a.Level) - rank(b.Level); r != 0 {
			return r
		}
		return b.Count - a.Count
	})
	if len(stamps) > 0 {
		s.Timeline, s.Step = timeline(stamps)
	}
	return s
}

// stamp is an error or warning line's time.
type stamp struct {
	at    time.Time
	level uicore.LogLevel
}

// timeline buckets error and warning lines by the smallest step that keeps
// it to 30 rows.
func timeline(list []stamp) ([]Bucket, time.Duration) {
	lo, hi := list[0].at, list[0].at
	for _, t := range list {
		if t.at.Before(lo) {
			lo = t.at
		}
		if t.at.After(hi) {
			hi = t.at
		}
	}
	var step time.Duration
	for _, d := range []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour, 6 * time.Hour, 24 * time.Hour} {
		step = d
		if hi.Truncate(d).Sub(lo.Truncate(d))/d < 30 {
			break
		}
	}
	start := lo.Truncate(step)
	buckets := make([]Bucket, int(hi.Truncate(step).Sub(start)/step)+1)
	for i := range buckets {
		buckets[i].Start = start.Add(time.Duration(i) * step)
	}
	for _, t := range list {
		b := &buckets[int(t.at.Truncate(step).Sub(start)/step)]
		if t.level == uicore.LogError {
			b.Errors++
		} else {
			b.Warns++
		}
	}
	return buckets, step
}

// Text is the summary for the model: at most max patterns, then the
// timeline.
func (s Summary) Text(max int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d lines, %d patterns (numbers, ids, addresses, durations, times and quoted values are placeholders).\n", s.Lines, len(s.Patterns))
	if len(s.Patterns) == 0 {
		return b.String()
	}
	b.WriteString("\nPatterns, errors and warnings first, then by count:\n")
	for i, p := range s.Patterns {
		if i == max {
			fmt.Fprintf(&b, "… %d more patterns, %d lines\n", len(s.Patterns)-max, s.rest(max))
			break
		}
		level := string(p.Level)
		if level == "" {
			level = "-"
		}
		fmt.Fprintf(&b, "%d. ×%d [%s] %s\n", i+1, p.Count, level, p.Template)
		if !p.First.IsZero() {
			if p.First.Equal(p.Last) {
				fmt.Fprintf(&b, "   at %s\n", stampText(p.First))
			} else {
				fmt.Fprintf(&b, "   first %s, last %s\n", stampText(p.First), stampText(p.Last))
			}
		}
		if p.Count > 1 || p.Example != p.Template {
			fmt.Fprintf(&b, "   e.g. %s\n", p.Example)
		}
	}
	if len(s.Timeline) > 1 {
		fmt.Fprintf(&b, "\nErrors and warnings per %s:\n", stepText(s.Step))
		for _, k := range s.Timeline {
			fmt.Fprintf(&b, "%s  errors %d, warnings %d\n", stampText(k.Start), k.Errors, k.Warns)
		}
	}
	return b.String()
}

func (s Summary) rest(from int) int {
	n := 0
	for _, p := range s.Patterns[from:] {
		n += p.Count
	}
	return n
}

func stampText(t time.Time) string { return t.Format("2006-01-02 15:04:05 MST") }

func stepText(d time.Duration) string {
	switch {
	case d >= 24*time.Hour:
		return "day"
	case d >= time.Hour:
		return fmt.Sprintf("%d hours", int(d.Hours()))
	}
	return fmt.Sprintf("%d minutes", int(d.Minutes()))
}

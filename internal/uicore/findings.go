package uicore

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/dayanraj/wtf/internal/findings"
)

// The Findings tab's grouping: by severity, then rule, filtered by words.
// Port of lib/findings.svelte.ts (macos/Sources/WtfCore/Findings.swift).

var FindingSeverities = []string{"critical", "high", "error", "medium", "warning", "low", "note", "none"}

var FindingSeverityLabels = map[string]string{"critical": "Critical", "high": "High", "error": "Error", "medium": "Medium",
	"warning": "Warning", "low": "Low", "note": "Note", "none": "None"}

// FindingTone is how loudly a severity shows.
type FindingTone int

const (
	FindingBad FindingTone = iota
	FindingWarn
	FindingDim
)

func FindingToneOf(severity string) FindingTone {
	switch severity {
	case "critical", "high", "error":
		return FindingBad
	case "medium", "warning":
		return FindingWarn
	}
	return FindingDim
}

// FindingRule is one rule's findings within a severity.
type FindingRule struct {
	// Tool and rule id, for identity.
	Key                 string
	Title, RuleID, Tool string
	Items               []findings.Finding
}

// FindingGroup is one severity's rules.
type FindingGroup struct {
	Severity string
	Rules    []FindingRule
}

func (g FindingGroup) Count() int {
	n := 0
	for _, r := range g.Rules {
		n += len(r.Items)
	}
	return n
}

// FindingMatches: every word of the filter is in the finding's tool, rule,
// message or path.
func FindingMatches(f findings.Finding, filter string) bool {
	hay := strings.ToLower(f.Tool + " " + f.RuleID + " " + f.Rule + " " + f.Message + " " + f.Path)
	for _, w := range strings.Fields(strings.ToLower(filter)) {
		if !strings.Contains(hay, w) {
			return false
		}
	}
	return true
}

// GroupFindings groups the findings that match the filter: severities
// worst first, rules with the most findings first, findings by path and line.
func GroupFindings(list []findings.Finding, filter string) []FindingGroup {
	bySeverity := map[string]map[string]*FindingRule{}
	for _, f := range list {
		if !FindingMatches(f, filter) {
			continue
		}
		rules := bySeverity[f.Severity]
		if rules == nil {
			rules = map[string]*FindingRule{}
			bySeverity[f.Severity] = rules
		}
		key := f.Tool + "\n" + f.RuleID
		r := rules[key]
		if r == nil {
			title := f.Rule
			if title == "" {
				title = f.RuleID
			}
			r = &FindingRule{Key: key, Title: title, RuleID: f.RuleID, Tool: f.Tool}
			rules[key] = r
		}
		r.Items = append(r.Items, f)
	}
	rank := func(s string) int {
		if i := slices.Index(FindingSeverities, s); i >= 0 {
			return i
		}
		return len(FindingSeverities)
	}
	var out []FindingGroup
	for sev, rules := range bySeverity {
		g := FindingGroup{Severity: sev}
		for _, r := range rules {
			sort.SliceStable(r.Items, func(i, j int) bool {
				a, b := r.Items[i], r.Items[j]
				if a.Path != b.Path {
					return a.Path < b.Path
				}
				return a.Line < b.Line
			})
			g.Rules = append(g.Rules, *r)
		}
		sort.Slice(g.Rules, func(i, j int) bool {
			a, b := g.Rules[i], g.Rules[j]
			if len(a.Items) != len(b.Items) {
				return len(a.Items) > len(b.Items)
			}
			if a.Title != b.Title {
				return a.Title < b.Title
			}
			return a.Key < b.Key
		})
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool {
		if ri, rj := rank(out[i].Severity), rank(out[j].Severity); ri != rj {
			return ri < rj
		}
		return out[i].Severity < out[j].Severity
	})
	return out
}

// FindingWhere is `src/a.go:12`, relative to the root when under it.
func FindingWhere(f findings.Finding, root string) string {
	if f.Path == "" {
		return ""
	}
	p := strings.TrimPrefix(f.Path, root+"/")
	if f.Line > 0 {
		return fmt.Sprintf("%s:%d", p, f.Line)
	}
	return p
}

// FindingsStale is whether a folder needs another read: it's outside what
// was loaded. root is the loaded report's root and loaded the folder it
// was read for; "" when there is none.
func FindingsStale(folder, root, loaded string) bool {
	if root == "" {
		return folder != loaded
	}
	return !(folder == root || strings.HasPrefix(folder, root+"/"))
}

var scanCommand = regexp.MustCompile(`(^|[\s(;&|])(trivy|semgrep|checkov|gitleaks|grype)\s`)

// IsScanCommand: a finished command that was one of the scanners, so
// results may have changed.
func IsScanCommand(command string) bool { return scanCommand.MatchString(command) }

// FindingRelative is "just now", "5 min ago", "3 h ago" or "2 d ago" for an
// RFC 3339 time; "" when it isn't one.
func FindingRelative(iso string, now time.Time) string {
	t, err := time.Parse(time.RFC3339Nano, iso)
	if err != nil {
		return ""
	}
	s := now.Sub(t).Seconds()
	switch {
	case s < 45:
		return "just now"
	case s < 3600:
		return fmt.Sprintf("%d min ago", max(1, int(math.Round(s/60))))
	case s < 86400:
		return fmt.Sprintf("%d h ago", int(math.Round(s/3600)))
	}
	return fmt.Sprintf("%d d ago", int(math.Round(s/86400)))
}

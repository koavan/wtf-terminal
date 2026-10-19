package uicore

import (
	"reflect"
	"testing"
	"time"

	"github.com/dayanraj/wtf/internal/findings"
)

func TestFindingGroups(t *testing.T) {
	fs := []findings.Finding{
		{Tool: "trivy", RuleID: "CVE-1", Rule: "lib bad", Severity: "high", Message: "m", Path: "/r/b.go", Line: 3},
		{Tool: "trivy", RuleID: "CVE-1", Rule: "lib bad", Severity: "high", Message: "m", Path: "/r/a.go", Line: 9},
		{Tool: "semgrep", RuleID: "x", Severity: "warning", Message: "eval used", Path: "/r/c.py"},
		{Tool: "gitleaks", RuleID: "aws", Severity: "critical", Message: "key"},
	}
	severities := func(gs []FindingGroup) []string {
		var out []string
		for _, g := range gs {
			out = append(out, g.Severity)
		}
		return out
	}
	g := GroupFindings(fs, "")
	if got := severities(g); !reflect.DeepEqual(got, []string{"critical", "high", "warning"}) {
		t.Fatalf("severities %v", got)
	}
	items := g[1].Rules[0].Items
	if items[0].Path != "/r/a.go" || items[1].Path != "/r/b.go" || g[1].Count() != 2 {
		t.Errorf("high %+v", g[1])
	}
	expectText(t, g[2].Rules[0].Title, "x")
	if got := severities(GroupFindings(fs, "EVAL c.py")); !reflect.DeepEqual(got, []string{"warning"}) {
		t.Errorf("filtered %v", got)
	}
	expectText(t, FindingWhere(fs[0], "/r"), "b.go:3")
	expectText(t, FindingWhere(fs[3], "/r"), "")
	if FindingsStale("/r/sub", "/r", "/r") || !FindingsStale("/x", "/r", "/r") || FindingsStale("/y", "", "/y") {
		t.Error("stale")
	}
	if !IsScanCommand("trivy fs .") || !IsScanCommand("(cd /r && semgrep scan)") || IsScanCommand("trivyx") {
		t.Error("scan commands")
	}
	if FindingToneOf("medium") != FindingWarn || FindingToneOf("note") != FindingDim || FindingToneOf("critical") != FindingBad {
		t.Error("tones")
	}
}

func TestFindingRelative(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	for ago, want := range map[time.Duration]string{10 * time.Second: "just now", 5 * time.Minute: "5 min ago",
		3 * time.Hour: "3 h ago", 50 * time.Hour: "2 d ago"} {
		expectText(t, FindingRelative(now.Add(-ago).Format(time.RFC3339), now), want)
	}
	expectText(t, FindingRelative("junk", now), "")
}

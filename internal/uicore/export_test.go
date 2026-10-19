package uicore

import (
	"strings"
	"testing"
	"time"
)

func TestExports(t *testing.T) {
	at := time.Date(2026, 10, 9, 23, 5, 0, 0, time.UTC)
	expectText(t, ExportName("kubectl get pods -n shop", at), "wtf-kubectl-get-pods-n-shop-2026-10-09-2305")
	expectText(t, ExportName("~/", at), "wtf-terminal-2026-10-09-2305")
	expectText(t, ExportElapsed(65), "1:05")
	expectText(t, ExportElapsed(3725), "1:02:05")
	expectText(t, ExportCode("echo `date`"), "`` echo `date` ``")
	expectText(t, ExportFence("a ``` b"), "````")
	started := time.Date(2026, 10, 9, 23, 45, 1, 0, time.UTC)
	out, empty, y := "NAME READY\nweb-1 1/1", "", "y"
	zero, one := 0, 1
	second, none := time.Second, time.Duration(0)
	md := ExportMarkdown([]ExportBlock{
		{Text: "kubectl get pods", Output: &out, StartedAt: started, ExitCode: &zero, Duration: &second},
		{Text: "false", Output: &empty, ExitCode: &one, Duration: &none},
		{Text: "tail -f log"},
		{Text: "x", Output: &y},
	}, "ops", "~/ops", at, time.UTC)
	for _, want := range []string{
		"## 1. `kubectl get pods`\n\n✓ succeeded · started 23:45:01 · took 1s\n\n```text\nNAME READY\nweb-1 1/1\n```",
		"## 2. `false`\n\n✗ exit 1 · took 0.0s\n\n_No output._",
		"## 3. `tail -f log`\n\nstill running\n\n_Output no longer in the scrollback._",
		"~/ops · 4 commands",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q in\n%s", want, md)
		}
	}
	if !strings.HasPrefix(md, "# ops\n\nTerminal transcript from wtf, saved Oct 9, 2026 23:05:00") {
		t.Errorf("head:\n%s", md)
	}
}

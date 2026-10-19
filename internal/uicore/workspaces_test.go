package uicore

import (
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/dayanraj/wtf/internal/infra"
	"github.com/dayanraj/wtf/internal/snippets"
)

// The cases of macos/Tests/WtfCoreTests/WorkspaceTests.swift, and the
// placeholder ones of SnippetTests.swift.

func names(fs []Field) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Name)
	}
	return out
}

func TestPlaceholderFieldsInOrderOfFirstUse(t *testing.T) {
	f := Fields([]string{"kubectl -n {{ns}} logs {{pod}}", "{{ns=prod}} {{end}} {{ else }}"},
		[]snippets.Param{{Name: "pod", Description: "which pod", Choices: []string{"a", "b"}}})
	if got := names(f); !reflect.DeepEqual(got, []string{"ns", "pod"}) {
		t.Fatalf("names %v", got)
	}
	if f[0].Default != "prod" {
		t.Errorf("default %q", f[0].Default)
	}
	if f[1].Description != "which pod" || !reflect.DeepEqual(f[1].Choices, []string{"a", "b"}) {
		t.Errorf("pod %+v", f[1])
	}
}

func TestFillQuotesUnlessRaw(t *testing.T) {
	fields := []Field{{Name: "q"}, {Name: "r", Raw: true}}
	got, err := Fill("grep {{q}} | {{r}} {{end}}", map[string]string{"q": "a b", "r": "wc -l"}, fields)
	if err != nil || got != "grep 'a b' | wc -l {{end}}" {
		t.Errorf("Fill = %q, %v", got, err)
	}
	var empty EmptyError
	if _, err := Fill("x {{q}}", nil, fields); !errors.As(err, &empty) || empty.Name != "q" || err.Error() != "Fill in q" {
		t.Errorf("empty: %v", err)
	}
	if got, err := FillPlain("~/{{q}}", map[string]string{"q": " src "}); err != nil || got != "~/src" {
		t.Errorf("FillPlain = %q, %v", got, err)
	}
}

func TestDefaultsFromTheShell(t *testing.T) {
	f := ShellFacts{KubeNamespace: "api", Cwd: "/w"}
	for in, want := range map[string]string{"$kube_namespace": "api", " $cwd ": "/w", "$other": "$other", "literal": "literal"} {
		if got := ResolveDefault(in, f); got != want {
			t.Errorf("ResolveDefault(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestShellFactsOf(t *testing.T) {
	if (ShellFactsOf(nil, nil) != ShellFacts{}) {
		t.Error("no pane: no facts")
	}
	p := &PaneState{Cwd: "/w", Env: map[string]string{"AWS_PROFILE": "dev", "AWS_DEFAULT_REGION": "eu-west-1"}}
	got := ShellFactsOf(p, &infra.Context{KubeContext: "kind", AWSProfile: "env credentials"})
	want := ShellFacts{KubeContext: "kind", KubeNamespace: "default", AWSProfile: "dev", AWSRegion: "eu-west-1", Cwd: "/w"}
	if got != want {
		t.Errorf("got %+v", got)
	}
	got = ShellFactsOf(p, &infra.Context{KubeNamespace: "api", AWSProfile: "prod", AWSRegion: "us-east-1"})
	if got.KubeNamespace != "api" || got.AWSProfile != "prod" || got.AWSRegion != "us-east-1" {
		t.Errorf("got %+v", got)
	}
}

func TestSnippetFields(t *testing.T) {
	command := "kubectl -n {{ns=$kube_namespace}} rollout restart deploy/{{name}} {{end}}"
	fields := Fields([]string{command}, []snippets.Param{{Name: "name", Description: "The deployment", Choices: []string{"web", "worker"}}})
	if got := names(fields); !reflect.DeepEqual(got, []string{"ns", "name"}) {
		t.Fatalf("names %v", got)
	}
	if fields[1].Description != "The deployment" || fields[0].Default != "$kube_namespace" {
		t.Errorf("fields %+v", fields)
	}
	facts := ShellFacts{KubeNamespace: "prod-ns"}
	if fields[0].InitialValue(facts) != "prod-ns" || fields[1].InitialValue(facts) != "web" {
		t.Errorf("initial values %q %q", fields[0].InitialValue(facts), fields[1].InitialValue(facts))
	}
	if got, err := Fill(command, map[string]string{"ns": "a b", "name": "web"}, fields); err != nil ||
		got != "kubectl -n 'a b' rollout restart deploy/web {{end}}" {
		t.Errorf("Fill = %q, %v", got, err)
	}
	var empty EmptyError
	if _, err := Fill(command, map[string]string{"ns": "x"}, fields); !errors.As(err, &empty) {
		t.Errorf("empty: %v", err)
	}
}

func TestRunbookFill(t *testing.T) {
	fields := Fields([]string{"kubectl cordon {{node}}", "kubectl drain {{node}} {{flags=--force}}"},
		[]snippets.Param{{Name: "flags", Raw: true}})
	if got := names(fields); !reflect.DeepEqual(got, []string{"node", "flags"}) {
		t.Fatalf("names %v", got)
	}
	got, err := Fill("kubectl drain {{node}} {{flags=--force}}", map[string]string{"node": "my node's", "flags": "--force --grace-period=0"}, fields)
	if want := `kubectl drain 'my node'\''s' --force --grace-period=0`; err != nil || got != want {
		t.Errorf("Fill = %q, %v", got, err)
	}
}

func TestPlansAndOpensAWorkspace(t *testing.T) {
	w := snippets.Workspace{Name: "on call", Tabs: []snippets.WTab{
		{Title: "{{ns}} pods", Group: "oncall", Color: "red", Layout: "row", Panes: []snippets.WPane{
			{Cwd: "~/src", Command: "kubectl -n {{ns}} get pods"}, {Cwd: "", Command: "k9s"}, {Cwd: "/tmp", Command: ""}}},
		{Group: "oncall", Cwd: "~"},
	}}
	plan, err := PlanWorkspace(w, map[string]string{"ns": "shop"}, "/h")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Tabs[0].Title != "shop pods" {
		t.Errorf("title %q", plan.Tabs[0].Title)
	}
	var cwds []string
	for _, p := range plan.Tabs[0].Panes {
		cwds = append(cwds, p.Cwd)
	}
	if !reflect.DeepEqual(cwds, []string{"/h/src", "", "/tmp"}) {
		t.Errorf("cwds %v", cwds)
	}
	if plan.Tabs[0].Panes[0].Command != "kubectl -n shop get pods" {
		t.Errorf("command %q", plan.Tabs[0].Panes[0].Command)
	}
	if !reflect.DeepEqual(plan.Tabs[1].Panes, []PlannedPane{{Cwd: "/h"}}) {
		t.Errorf("tab 2 panes %+v", plan.Tabs[1].Panes)
	}
	var empty EmptyError
	if _, err := PlanWorkspace(w, nil, "/h"); !errors.As(err, &empty) || empty.Name != "ns" {
		t.Errorf("unfilled: %v", err)
	}

	m := NewWindowModel("/h")
	existing := "/existing"
	m.Open(OpenOptions{Cwd: &existing})
	first := m.OpenPlan(plan)
	if len(m.Tabs()) != 3 || m.ActiveID() != first || first == 0 {
		t.Fatalf("tabs %d active %d first %d", len(m.Tabs()), m.ActiveID(), first)
	}
	tab, _ := m.Tab(first)
	leaves := tab.Root.Leaves()
	if len(leaves) != 3 || tab.FocusedPaneID != leaves[0] {
		t.Errorf("leaves %v focused %d", leaves, tab.FocusedPaneID)
	}
	if tab.CustomTitle != "shop pods" {
		t.Errorf("custom title %q", tab.CustomTitle)
	}
	// Three panes side by side, a third each.
	for id, r := range tab.Root.Frames() {
		if math.Abs(r.W-1.0/3) > 1e-9 {
			t.Errorf("pane %d width %v", id, r.W)
		}
	}
	// The empty folder falls back to the tab's first pane's.
	var initial []string
	for _, id := range leaves {
		initial = append(initial, m.Panes[id].InitialCwd)
	}
	if !reflect.DeepEqual(initial, []string{"/h/src", "/h/src", "/tmp"}) {
		t.Errorf("initial cwds %v", initial)
	}
	if m.Panes[leaves[1]].InitialCommand != "k9s" {
		t.Errorf("initial command %q", m.Panes[leaves[1]].InitialCommand)
	}
	gs := m.Groups()
	if len(gs) != 1 || gs[0].Name != "oncall" || gs[0].Color != GroupRed || len(m.TabsIn(gs[0].ID)) != 2 {
		t.Errorf("groups %+v", gs)
	}

	if (NewWindowModel("/h").OpenPlan(Plan{Tabs: []PlannedTab{{Title: "none"}}})) != 0 {
		t.Error("a plan with no panes opens nothing")
	}
}

func TestCapturesTheWindowAsAWorkspace(t *testing.T) {
	m := NewWindowModel("/h")
	src, logs, tmp := "/h/src", "/h/logs", "/tmp"
	a := m.Open(OpenOptions{Cwd: &src})
	m.Split(SplitRow, &logs, "")
	m.ActivePane().Running = true
	m.ActivePane().Command = "tail -f app.log"
	m.Rename(a, "api")
	g := m.CreateGroup(a)
	name, red := "on call", GroupRed
	m.SetGroup(g, &name, &red)
	m.Open(OpenOptions{Cwd: &tmp})
	d := m.CaptureWorkspace("/h")
	if len(d.Tabs) != 2 {
		t.Fatalf("tabs %d", len(d.Tabs))
	}
	t0, t1 := d.Tabs[0], d.Tabs[1]
	if t0.Title != "api" || t0.Group != "on call" || t0.Color != "red" || t0.Layout != "row" {
		t.Errorf("tab 1 %+v", t0)
	}
	if want := []snippets.WPane{{Cwd: "~/src"}, {Cwd: "~/logs", Command: "tail -f app.log"}}; !reflect.DeepEqual(t0.Panes, want) {
		t.Errorf("tab 1 panes %+v", t0.Panes)
	}
	if t1.Title != "" || t1.Layout != "" || !reflect.DeepEqual(t1.Panes, []snippets.WPane{{Cwd: "/tmp"}}) {
		t.Errorf("tab 2 %+v", t1)
	}
}

// Swift's convertsAListedWorkspace: the Go types are used directly, so
// only the one-pane fallback is left to check.
func TestAllPanes(t *testing.T) {
	if got := AllPanes(snippets.WTab{Title: "t", Cwd: "~"}); !reflect.DeepEqual(got, []snippets.WPane{{Cwd: "~"}}) {
		t.Errorf("one-pane tab %+v", got)
	}
	panes := []snippets.WPane{{Cwd: "/a"}, {Cwd: "/b", Command: "top"}}
	if got := AllPanes(snippets.WTab{Cwd: "~", Panes: panes}); !reflect.DeepEqual(got, panes) {
		t.Errorf("pane list %+v", got)
	}
}

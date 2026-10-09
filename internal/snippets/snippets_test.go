package snippets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// A home and config folder in a temp dir.
func service(t *testing.T) (*Service, string) {
	t.Helper()
	home := t.TempDir()
	return &Service{home: home, configDir: filepath.Join(home, ".config", "wtf")}, home
}

func write(t *testing.T, path, data string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func names(l List) []string {
	var out []string
	for _, s := range l.Snippets {
		out = append(out, s.Name)
	}
	return out
}

func TestList(t *testing.T) {
	s, home := service(t)
	write(t, s.userFile(), `
[[snippet]]
name = "Mine"
command = "echo {{who=me}}"
`)
	write(t, filepath.Join(s.configDir, "snippets", "team.toml"), `
[[snippet]]
name = "Team runbook"
[[snippet.steps]]
title = "One"
command = "kubectl cordon {{node}}"
[[snippet.steps]]
command = "kubectl drain {{node}}"
[snippet.params.node]
description = "The node"
choices = ["a", "b"]
`)
	repo := filepath.Join(home, "src", "payments")
	write(t, filepath.Join(repo, ".wtf", "snippets.toml"), `
[[snippet]]
name = "Deploy payments"
command = "make deploy ENV={{env}}"
[snippet.params.env]
choices = ["staging", "prod"]
`)
	write(t, filepath.Join(home, "src", ".wtf", "snippets.toml"), `
[[snippet]]
name = "Broken"

[[snippet]]
name = "Both"
command = "x"
[[snippet.steps]]
command = "y"
`)
	write(t, filepath.Join(home, ".wtf", "snippets.toml"), "[[snippet]]\nname = \"In home\"\ncommand = \"ls\"\n")
	write(t, filepath.Join(filepath.Dir(home), ".wtf", "snippets.toml"), "this is outside home and never read")

	l := s.List(filepath.Join(repo, "cmd"))
	if got := strings.Join(names(l), ","); got != "Deploy payments,In home,Mine,Team runbook" {
		t.Errorf("snippets %s", got)
	}
	if len(l.Problems) != 2 || !strings.Contains(l.Problems[0], "needs a command") || !strings.Contains(l.Problems[1], "not both") {
		t.Errorf("problems %v", l.Problems)
	}
	if l.ProjectFile != filepath.Join(repo, ".wtf", "snippets.toml") || l.UserFile != s.userFile() {
		t.Errorf("files %q %q", l.ProjectFile, l.UserFile)
	}
	dep := l.Snippets[0]
	if !dep.Project || dep.Params[0].Name != "env" || strings.Join(dep.Params[0].Choices, ",") != "staging,prod" {
		t.Errorf("deploy %+v", dep)
	}
	team := l.Snippets[3]
	if len(team.Steps) != 2 || team.Steps[0].Title != "One" || team.Params[0].Description != "The node" || team.Project {
		t.Errorf("team %+v", team)
	}
	if l.Versions[s.userFile()] == "" {
		t.Error("user file version")
	}

	// Somewhere else, only the user's files.
	if got := strings.Join(names(s.List("/")), ","); got != "Mine,Team runbook" {
		t.Errorf("elsewhere %s", got)
	}
}

const handWritten = `# My snippets — keep this comment

[[snippet]]
name = "First"
command = "echo one"

# Runbook for the database — a comment about the next one
[[snippet]]
name = "Second"
command = "echo {{x}}"
[snippet.params.x]
default = "2"

[[snippet]]
name = "Third"
command = "echo three"   # trailing comment
`

func TestEditKeepsTheRestOfTheFile(t *testing.T) {
	s, home := service(t)
	write(t, s.userFile(), handWritten)
	l := s.List(home)
	v := l.Versions[s.userFile()]

	e := Edit{File: s.userFile(), Index: 1, Version: v, Snippet: Def{
		Name: "Second, better", Command: "echo {{x}} {{y}}",
		Params: []Param{{Name: "x", Default: "2"}, {Name: "y", Choices: []string{"a", " b "}, Raw: true}, {Name: "z"}},
	}}
	pv, err := s.PlanEdit(e, home)
	if err != nil {
		t.Fatal(err)
	}
	if pv.NewFile || !strings.HasPrefix(pv.Text, "[[snippet]]\nname = \"Second, better\"") || !strings.Contains(pv.Text, `choices = ["a", "b"]`) || strings.Contains(pv.Text, "params.z") {
		t.Errorf("preview %+v", pv)
	}
	if read(t, s.userFile()) != handWritten {
		t.Fatal("planning must not write")
	}
	if _, err := s.ApplyEdit(e, home); err != nil {
		t.Fatal(err)
	}
	got := read(t, s.userFile())
	for _, keep := range []string{"# My snippets — keep this comment", "# Runbook for the database — a comment about the next one", `name = "First"`, `command = "echo three"   # trailing comment`} {
		if !strings.Contains(got, keep) {
			t.Errorf("lost %q:\n%s", keep, got)
		}
	}
	if strings.Contains(got, `name = "Second"`) || !strings.Contains(got, `name = "Second, better"`) {
		t.Errorf("not replaced:\n%s", got)
	}
	// The comment before Second stays before it, not swallowed by First.
	if strings.Index(got, "# Runbook for the database") > strings.Index(got, "Second, better") {
		t.Error("comment moved")
	}
	l = s.List(home)
	if names(l)[1] != "Second, better" || len(l.Snippets[1].Params) != 2 {
		t.Errorf("after edit %+v", l.Snippets)
	}

	// A stale version is refused; delete works on the block alone.
	if _, err := s.ApplyEdit(e, home); err == nil {
		t.Error("stale version")
	}
	if _, err := s.Delete("snippet", s.userFile(), 0, l.Versions[s.userFile()], home); err != nil {
		t.Fatal(err)
	}
	got = read(t, s.userFile())
	if strings.Contains(got, `name = "First"`) || !strings.HasPrefix(got, "# My snippets") || strings.Contains(got, "\n\n\n") {
		t.Errorf("after delete:\n%s", got)
	}
	if strings.Join(names(s.List(home)), ",") != "Second, better,Third" {
		t.Errorf("names %v", names(s.List(home)))
	}
}

func TestAddAndTargets(t *testing.T) {
	s, home := service(t)
	repo := filepath.Join(home, "src", "app")
	os.MkdirAll(filepath.Join(repo, ".git"), 0o755)
	cwd := filepath.Join(repo, "web")
	os.MkdirAll(cwd, 0o755)

	l := s.List(cwd)
	projectFile := filepath.Join(repo, ".wtf", "snippets.toml")
	if len(l.Targets) != 2 || l.Targets[0] != (Target{Path: projectFile, Kind: "project"}) || l.Targets[1].Kind != "user" || l.Targets[1].Exists {
		t.Fatalf("targets %+v", l.Targets)
	}

	// A new project file is created at the repository's root.
	e := Edit{File: projectFile, Index: -1, Snippet: Def{Name: "Build", Command: "make build"}}
	pv, err := s.PlanEdit(e, cwd)
	if err != nil || !pv.NewFile {
		t.Fatalf("preview %+v %v", pv, err)
	}
	l, err = s.ApplyEdit(e, cwd)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Snippets) != 1 || !l.Snippets[0].Project || !strings.HasPrefix(read(t, projectFile), "# wtf snippets") {
		t.Errorf("after add %+v", l)
	}

	// A runbook, appended to the user's file.
	e = Edit{File: s.userFile(), Index: -1, Snippet: Def{Name: "Drain", Steps: []Step{{Title: "Cordon", Command: "kubectl cordon {{node}}"}, {}, {Command: "kubectl drain {{node}}"}}}}
	if l, err = s.ApplyEdit(e, cwd); err != nil {
		t.Fatal(err)
	}
	if dr := l.Snippets[1]; dr.Name != "Drain" || len(dr.Steps) != 2 || dr.Steps[0].Title != "Cordon" {
		t.Errorf("runbook %+v", dr)
	}

	for _, bad := range []Edit{
		{File: "/etc/profile", Index: -1, Snippet: Def{Name: "x", Command: "y"}},
		{File: s.userFile(), Index: -1, Version: l.Versions[s.userFile()], Snippet: Def{Name: "", Command: "y"}},
		{File: s.userFile(), Index: -1, Version: l.Versions[s.userFile()], Snippet: Def{Name: "x", Command: "a\nb"}},
		{File: s.userFile(), Index: -1, Version: l.Versions[s.userFile()], Snippet: Def{Name: "x", Command: "y", Steps: []Step{{Command: "z"}}}},
		{File: s.userFile(), Index: -1, Version: l.Versions[s.userFile()], Snippet: Def{Name: "x", Command: "y", Params: []Param{{Name: "bad name", Default: "1"}}}},
		{File: s.userFile(), Index: 7, Version: l.Versions[s.userFile()], Snippet: Def{Name: "x", Command: "y"}},
	} {
		if _, err := s.ApplyEdit(bad, cwd); err == nil {
			t.Errorf("%+v should be refused", bad)
		}
	}
	// Outside a repository there's no project target to create.
	if tg := s.List("/").Targets; len(tg) != 1 || tg[0].Kind != "user" {
		t.Errorf("targets outside a repo %+v", tg)
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, _ := os.ReadFile(p)
	return string(b)
}

func TestExamplesParse(t *testing.T) {
	var f file
	if _, err := toml.Decode(header+examples, &f); err != nil {
		t.Fatal(err)
	}
	for i, fs := range f.Snippet {
		if _, err := convert(fs); err != nil {
			t.Errorf("example %d: %v", i, err)
		}
	}
	if len(f.Snippet) != 5 || len(f.Snippet[4].Steps) != 3 {
		t.Errorf("examples %+v", f.Snippet)
	}
	if len(f.Workspace) != 1 {
		t.Fatalf("workspace examples %+v", f.Workspace)
	}
	if _, err := convertWorkspace(f.Workspace[0]); err != nil {
		t.Errorf("workspace example: %v", err)
	}
}

const mixed = `# Team file

[[snippet]]
name = "One"
command = "echo 1"

# The on-call workspace
[[workspace]]
name = "Oncall"
window = "new"
[[workspace.tab]]
title = "api"
group = "pay"
color = "red"
command = "ssh pay-api-1"
[[workspace.tab]]
layout = "column"
[[workspace.tab.pane]]
cwd = "~/src"
command = "kubectl get pods -w -n {{ns}}"
[[workspace.tab.pane]]
command = "kubectl get events -w"
[workspace.params.ns]
default = "$kube_namespace"

[[snippet]]
name = "Two"
command = "echo 2"
`

func TestWorkspaces(t *testing.T) {
	s, home := service(t)
	write(t, s.userFile(), mixed)
	l := s.List(home)
	if strings.Join(names(l), ",") != "One,Two" || len(l.Workspaces) != 1 {
		t.Fatalf("list %+v (problems %v)", l, l.Problems)
	}
	w := l.Workspaces[0]
	if w.Name != "Oncall" || w.Window != "new" || len(w.Tabs) != 2 || w.Tabs[0].Group != "pay" || w.Tabs[1].Layout != "column" ||
		len(w.Tabs[1].Panes) != 2 || w.Tabs[1].Panes[0].Cwd != "~/src" || w.Params[0].Name != "ns" || w.Index != 0 {
		t.Errorf("workspace %+v", w)
	}

	// Editing the workspace keeps the snippets around it — and the comment
	// introducing it, and the snippet after it, which shares no lines.
	e := Edit{Kind: "workspace", File: s.userFile(), Index: 0, Version: l.Versions[s.userFile()], Workspace: WorkspaceDef{
		Name: "Oncall", Window: "current",
		Tabs: []WTab{
			{Title: "api", Group: "pay", Color: "red", Panes: []WPane{{Command: "ssh pay-api-1"}}}, // one pane: a plain tab
			{Layout: "", Panes: []WPane{{Command: "top"}, {}, {Command: "htop"}}},                  // empty pane dropped
		},
	}}
	pv, err := s.PlanEdit(e, home)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(pv.Text, "window") || !strings.Contains(pv.Text, "[[workspace.tab.pane]]") || !strings.Contains(pv.Text, `layout = "row"`) {
		t.Errorf("preview:\n%s", pv.Text)
	}
	if _, err := s.ApplyEdit(e, home); err != nil {
		t.Fatal(err)
	}
	got := read(t, s.userFile())
	for _, keep := range []string{"# Team file", "# The on-call workspace", `name = "One"`, `name = "Two"`} {
		if !strings.Contains(got, keep) {
			t.Errorf("lost %q:\n%s", keep, got)
		}
	}
	l = s.List(home)
	if w := l.Workspaces[0]; w.Window != "" || w.Tabs[0].Command != "ssh pay-api-1" || len(w.Tabs[0].Panes) != 0 || len(w.Tabs[1].Panes) != 2 {
		t.Errorf("after edit %+v", w)
	}
	if strings.Join(names(l), ",") != "One,Two" {
		t.Errorf("snippets %v", names(l))
	}

	// Deleting the workspace leaves both snippets.
	if l, err = s.Delete("workspace", s.userFile(), 0, l.Versions[s.userFile()], home); err != nil {
		t.Fatal(err)
	}
	if len(l.Workspaces) != 0 || strings.Join(names(l), ",") != "One,Two" {
		t.Errorf("after delete %+v", l)
	}

	for _, bad := range []WorkspaceDef{
		{Name: "", Tabs: []WTab{{Command: "x"}}},
		{Name: "x"},
		{Name: "x", Tabs: []WTab{{Color: "magenta", Command: "x"}}},
		{Name: "x", Tabs: []WTab{{Layout: "grid", Panes: []WPane{{Command: "a"}, {Command: "b"}}}}},
		{Name: "x", Window: "elsewhere", Tabs: []WTab{{Command: "x"}}},
		{Name: "x", Tabs: []WTab{{Command: "a\nb"}}},
	} {
		if _, err := s.PlanEdit(Edit{Kind: "workspace", File: s.userFile(), Index: -1, Version: l.Versions[s.userFile()], Workspace: bad}, home); err == nil {
			t.Errorf("%+v should be refused", bad)
		}
	}
	if _, err := s.Delete("table", s.userFile(), 0, "", home); err == nil {
		t.Error("unknown kind")
	}
}

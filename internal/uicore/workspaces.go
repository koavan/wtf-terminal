package uicore

import (
	"os"
	"regexp"
	"strings"
	"unicode"

	"github.com/dayanraj/wtf/internal/infra"
	"github.com/dayanraj/wtf/internal/snippets"
)

// Workspaces: named sets of tabs (folders, split panes, tab groups and
// startup commands) opened in one go, from the snippets files
// (internal/snippets). A workspace becomes a plan once its placeholders are
// filled in; the plan is what opens. Ports of lib/workspaces.svelte.ts and
// the placeholder helpers in lib/snippets.svelte.ts
// (macos/Sources/WtfCore/Workspaces.swift).

// AllPanes is a tab's panes: the pane list, or its own folder and command.
func AllPanes(t snippets.WTab) []snippets.WPane {
	if len(t.Panes) == 0 {
		return []snippets.WPane{{Cwd: t.Cwd, Command: t.Command}}
	}
	return t.Panes
}

// Field is one value a workspace (or snippet) asks for.
type Field struct {
	Name        string
	Description string
	// As written: a literal or a $variable.
	Default string
	Choices []string
	Raw     bool
}

// InitialValue is the value a field starts with in the run dialog: its
// default worked out from the shell, else the first choice.
func (f Field) InitialValue(facts ShellFacts) string {
	if v := ResolveDefault(f.Default, facts); v != "" {
		return v
	}
	if len(f.Choices) > 0 {
		return f.Choices[0]
	}
	return ""
}

// ShellFacts is what the focused shell is pointed at, for $variable defaults.
type ShellFacts struct {
	KubeContext, KubeNamespace, AWSProfile, AWSRegion, Cwd string
}

// ShellFactsOf reads a pane's facts; p and ctx may be nil.
func ShellFactsOf(p *PaneState, ctx *infra.Context) ShellFacts {
	var f ShellFacts
	if p == nil {
		return f
	}
	var c infra.Context
	if ctx != nil {
		c = *ctx
	}
	env := p.Env
	f.KubeContext = c.KubeContext
	switch {
	case c.KubeNamespace != "":
		f.KubeNamespace = c.KubeNamespace
	case f.KubeContext != "":
		f.KubeNamespace = "default"
	}
	if c.AWSProfile != "" && c.AWSProfile != "env credentials" {
		f.AWSProfile = c.AWSProfile
	} else {
		f.AWSProfile = env["AWS_PROFILE"]
	}
	for _, r := range []string{c.AWSRegion, env["AWS_REGION"], env["AWS_DEFAULT_REGION"]} {
		if r != "" {
			f.AWSRegion = r
			break
		}
	}
	f.Cwd = p.Cwd
	return f
}

// Placeholders

var placeholderPattern = regexp.MustCompile(`\{\{([A-Za-z_][A-Za-z0-9_-]*)(?:=([^}]*))?\}\}`)

// Words Go templates use bare: never placeholders.
var templateWords = map[string]bool{"end": true, "else": true, "break": true, "continue": true, "nil": true, "true": true, "false": true}

type placeholderHit struct {
	start, end int
	name       string
	// Its inline default, {{name=default}}; hasInline tells "" from none.
	inline    string
	hasInline bool
}

func placeholderHits(text string) []placeholderHit {
	var out []placeholderHit
	for _, m := range placeholderPattern.FindAllStringSubmatchIndex(text, -1) {
		h := placeholderHit{start: m[0], end: m[1], name: text[m[2]:m[3]]}
		if m[4] >= 0 {
			h.inline, h.hasInline = text[m[4]:m[5]], true
		}
		if templateWords[h.name] && !h.hasInline {
			continue
		}
		out = append(out, h)
	}
	return out
}

// Fields are the placeholders in some texts, in order of first use, with
// their declared settings.
func Fields(texts []string, params []snippets.Param) []Field {
	var out []Field
	for _, text := range texts {
	hits:
		for _, h := range placeholderHits(text) {
			for i := range out {
				if out[i].Name == h.name {
					if out[i].Default == "" && h.hasInline {
						out[i].Default = h.inline
					}
					continue hits
				}
			}
			f := Field{Name: h.name}
			for _, p := range params {
				if p.Name == h.name {
					f = Field{Name: h.name, Description: p.Description, Default: p.Default, Choices: p.Choices, Raw: p.Raw}
					break
				}
			}
			if h.hasInline {
				f.Default = h.inline
			}
			out = append(out, f)
		}
	}
	return out
}

// trimBlanks trims spaces and tabs (Swift's .whitespaces), not newlines.
func trimBlanks(s string) string {
	return strings.TrimFunc(s, func(r rune) bool { return r != '\n' && r != '\r' && unicode.IsSpace(r) })
}

// ResolveDefault is a default with its $variable worked out ("" when the
// shell doesn't say).
func ResolveDefault(def string, f ShellFacts) string {
	t := trimBlanks(def)
	if len(t) < 2 || t[0] != '$' {
		return def
	}
	for _, r := range t[1:] {
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '_' {
			return def
		}
	}
	switch t[1:] {
	case "kube_context":
		return f.KubeContext
	case "kube_namespace":
		return f.KubeNamespace
	case "aws_profile":
		return f.AWSProfile
	case "aws_region":
		return f.AWSRegion
	case "cwd":
		return f.Cwd
	}
	return def
}

// EmptyError names the first placeholder left empty.
type EmptyError struct{ Name string }

func (e EmptyError) Error() string { return "Fill in " + e.Name }

func replacePlaceholders(text string, value func(placeholderHit) (string, error)) (string, error) {
	var b strings.Builder
	last := 0
	for _, h := range placeholderHits(text) {
		v, err := value(h)
		if err != nil {
			return "", err
		}
		b.WriteString(text[last:h.start])
		b.WriteString(v)
		last = h.end
	}
	b.WriteString(text[last:])
	return b.String(), nil
}

// Fill is a command with its placeholders filled: values shell-quoted
// unless raw.
func Fill(command string, values map[string]string, fields []Field) (string, error) {
	return replacePlaceholders(command, func(h placeholderHit) (string, error) {
		v := trimBlanks(values[h.name])
		if v == "" {
			return "", EmptyError{h.name}
		}
		for _, f := range fields {
			if f.Name == h.name {
				if f.Raw {
					return v, nil
				}
				break
			}
		}
		return ShellQuote(v), nil
	})
}

// FillPlain is text (a folder, a title) with its placeholders filled as typed.
func FillPlain(text string, values map[string]string) (string, error) {
	return replacePlaceholders(text, func(h placeholderHit) (string, error) {
		v := trimBlanks(values[h.name])
		if v == "" {
			return "", EmptyError{h.name}
		}
		return v, nil
	})
}

// Plans

type PlannedPane struct {
	Cwd     string `json:"cwd"`
	Command string `json:"command"`
}

type PlannedTab struct {
	Title string `json:"title"`
	Group string `json:"group"`
	Color string `json:"color"`
	// For several panes: side by side (row) or stacked (column).
	Layout string        `json:"layout"`
	Panes  []PlannedPane `json:"panes"`
}

// Plan is a workspace with its placeholders filled: the tabs it opens.
type Plan struct {
	Name string       `json:"name"`
	Tabs []PlannedTab `json:"tabs"`
}

// WorkspaceFields are the values a workspace asks for, from every title,
// group, folder and command.
func WorkspaceFields(w snippets.Workspace) []Field {
	var texts []string
	for _, t := range w.Tabs {
		texts = append(texts, t.Title, t.Group)
		for _, p := range AllPanes(t) {
			texts = append(texts, p.Cwd, p.Command)
		}
	}
	return Fields(texts, w.Params)
}

// PlanWorkspace is the tabs a workspace opens, placeholders filled; the
// error names the first field left empty. home "" is the user's home folder.
func PlanWorkspace(w snippets.Workspace, values map[string]string, home string) (Plan, error) {
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	fields := WorkspaceFields(w)
	expandHome := func(p string) string {
		if p == "~" {
			return home
		}
		if strings.HasPrefix(p, "~/") {
			return home + p[1:]
		}
		return p
	}
	plan := Plan{Name: w.Name, Tabs: []PlannedTab{}}
	for _, t := range w.Tabs {
		title, err := FillPlain(t.Title, values)
		if err != nil {
			return Plan{}, err
		}
		group, err := FillPlain(t.Group, values)
		if err != nil {
			return Plan{}, err
		}
		pt := PlannedTab{Title: title, Group: group, Color: t.Color, Layout: "row", Panes: []PlannedPane{}}
		if t.Layout == "column" {
			pt.Layout = "column"
		}
		for _, p := range AllPanes(t) {
			cwd, err := FillPlain(p.Cwd, values)
			if err != nil {
				return Plan{}, err
			}
			cmd := ""
			if p.Command != "" {
				if cmd, err = Fill(p.Command, values, fields); err != nil {
					return Plan{}, err
				}
			}
			pt.Panes = append(pt.Panes, PlannedPane{Cwd: expandHome(cwd), Command: cmd})
		}
		plan.Tabs = append(plan.Tabs, pt)
	}
	return plan, nil
}

// OpenPlan opens a plan's tabs in this window, after the tabs already open:
// splits chained off the newest pane then evened out, names, groups
// (matched within the plan only). Returns the first opened tab, 0 when
// nothing opened.
func (w *WindowModel) OpenPlan(p Plan) int {
	groups := map[string]int{}
	first := 0
	for _, t := range p.Tabs {
		if len(t.Panes) == 0 {
			continue
		}
		firstPane := t.Panes[0]
		cwd := firstPane.Cwd
		id := w.Open(OpenOptions{Cwd: &cwd, Command: firstPane.Command})
		if first == 0 {
			first = id
		}
		dir := SplitRow
		if t.Layout == "column" {
			dir = SplitColumn
		}
		for _, pane := range t.Panes[1:] {
			c := pane.Cwd
			if c == "" {
				c = firstPane.Cwd
			}
			w.Split(dir, &c, pane.Command)
		}
		w.EvenOut(id)
		if tab, ok := w.Tab(id); ok {
			if leaves := tab.Root.Leaves(); len(leaves) > 0 {
				w.FocusPane(leaves[0])
			}
		}
		if t.Title != "" {
			w.Rename(id, t.Title)
		}
		if t.Group != "" {
			if g, ok := groups[t.Group]; ok {
				w.AddToGroup(id, g)
			} else if g := w.CreateGroup(id); g != 0 {
				name := t.Group
				// An unknown colour keeps the one the group was given.
				var color *GroupColor
				for _, c := range GroupColors {
					if string(c) == t.Color {
						color = &c
						break
					}
				}
				w.SetGroup(g, &name, color)
				groups[t.Group] = g
			}
		}
	}
	if first != 0 {
		w.Activate(first)
	}
	return first
}

// CaptureWorkspace is "Save Window as Workspace": every tab in strip order
// with its custom title, group and colour, its panes' folders (~ for home)
// and what runs in them now (an ssh host, `kubectl … -w`), and the root
// split's direction. Nested splits flatten to one direction.
func (w *WindowModel) CaptureWorkspace(home string) snippets.WorkspaceDef {
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	def := snippets.WorkspaceDef{Tabs: []snippets.WTab{}, Params: []snippets.Param{}}
	for _, t := range w.Tabs() {
		wt := snippets.WTab{Title: t.CustomTitle, Panes: []snippets.WPane{}}
		if g, ok := w.Group(t.GroupID); ok && t.GroupID != 0 {
			wt.Group, wt.Color = g.Name, string(g.Color)
		}
		if t.Root.IsSplit() {
			wt.Layout = string(t.Root.Dir)
		}
		for _, id := range t.Root.Leaves() {
			p := w.Panes[id]
			if p == nil {
				continue
			}
			cmd := ""
			if p.Running {
				cmd = p.Command
			}
			wt.Panes = append(wt.Panes, snippets.WPane{Cwd: PrettyPath(p.Cwd, home), Command: cmd})
		}
		def.Tabs = append(def.Tabs, wt)
	}
	return def
}

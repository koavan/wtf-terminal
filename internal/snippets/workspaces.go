package snippets

import (
	"errors"
	"fmt"
	"strings"
)

// Workspaces: named sets of tabs — with folders, split panes, tab groups
// and startup commands — opened in one go. They live in the same files as
// snippets, as [[workspace]] blocks, so one project file can carry a
// team's runbooks and workspaces. Opening one is done by the frontend
// (lib/workspaces.svelte.ts); this reads, checks and edits them.

// WPane is one pane of a tab with several.
type WPane struct {
	Cwd     string `json:"cwd" toml:"cwd,omitempty"`
	Command string `json:"command" toml:"command,omitempty"`
}

// WTab is one tab of a workspace.
type WTab struct {
	// A name for the tab; empty keeps the automatic one.
	Title string `json:"title" toml:"title,omitempty"`
	// Tabs with the same group name open in one tab group.
	Group string `json:"group" toml:"group,omitempty"`
	Color string `json:"color" toml:"color,omitempty"`
	// For a one-pane tab.
	Cwd     string `json:"cwd" toml:"cwd,omitempty"`
	Command string `json:"command" toml:"command,omitempty"`
	// For several panes: "row" (side by side) or "column" (stacked).
	Layout string  `json:"layout" toml:"layout,omitempty"`
	Panes  []WPane `json:"panes" toml:"pane,omitempty"`
}

type Workspace struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// "new" opens it in a new window; otherwise in the current one.
	Window  string  `json:"window"`
	Tabs    []WTab  `json:"tabs"`
	Params  []Param `json:"params"`
	Source  string  `json:"source"`
	Index   int     `json:"index"`
	Project bool    `json:"project"`
}

// WorkspaceDef is a workspace as the editor fills it in.
type WorkspaceDef struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Window      string  `json:"window"`
	Tabs        []WTab  `json:"tabs"`
	Params      []Param `json:"params"`
}

type fileWorkspace struct {
	Name        string           `toml:"name"`
	Description string           `toml:"description,omitempty"`
	Window      string           `toml:"window,omitempty"`
	Tabs        []WTab           `toml:"tab"`
	Params      map[string]Param `toml:"params,omitempty"`
}

const (
	maxTabs  = 30
	maxPanes = 8
)

// The tab group colors wtf has.
var groupColors = map[string]bool{
	"blue": true, "red": true, "yellow": true, "green": true, "pink": true,
	"purple": true, "cyan": true, "orange": true, "grey": true,
}

func convertWorkspace(fw fileWorkspace) (Workspace, error) {
	w := Workspace{Name: strings.TrimSpace(fw.Name), Description: strings.TrimSpace(fw.Description),
		Window: strings.TrimSpace(fw.Window), Tabs: []WTab{}, Params: []Param{}}
	switch {
	case w.Name == "":
		return w, errors.New("it needs a name")
	case len(fw.Tabs) == 0:
		return w, errors.New("it needs at least one [[workspace.tab]]")
	case len(fw.Tabs) > maxTabs:
		return w, fmt.Errorf("at most %d tabs", maxTabs)
	case w.Window != "" && w.Window != "new" && w.Window != "current":
		return w, fmt.Errorf(`window must be "new" or "current", not %q`, w.Window)
	}
	oneLine := func(what, v string) error {
		if strings.ContainsAny(v, "\r\n") {
			return fmt.Errorf("%s must be one line", what)
		}
		return nil
	}
	for i, t := range fw.Tabs {
		t.Title, t.Group, t.Color = strings.TrimSpace(t.Title), strings.TrimSpace(t.Group), strings.TrimSpace(t.Color)
		t.Cwd, t.Command, t.Layout = strings.TrimSpace(t.Cwd), strings.TrimSpace(t.Command), strings.TrimSpace(t.Layout)
		where := fmt.Sprintf("tab %d", i+1)
		if t.Color != "" && !groupColors[t.Color] {
			return w, fmt.Errorf("%s: color %q isn't a group color (blue, red, yellow, green, pink, purple, cyan, orange, grey)", where, t.Color)
		}
		if t.Layout != "" && t.Layout != "row" && t.Layout != "column" {
			return w, fmt.Errorf(`%s: layout must be "row" (side by side) or "column" (stacked)`, where)
		}
		if len(t.Panes) > maxPanes {
			return w, fmt.Errorf("%s: at most %d panes", where, maxPanes)
		}
		if len(t.Panes) > 0 && (t.Cwd != "" || t.Command != "") {
			return w, fmt.Errorf("%s: with panes, give each pane its folder and command", where)
		}
		for _, v := range []string{t.Title, t.Group, t.Cwd, t.Command} {
			if err := oneLine(where, v); err != nil {
				return w, err
			}
		}
		for j := range t.Panes {
			t.Panes[j].Cwd, t.Panes[j].Command = strings.TrimSpace(t.Panes[j].Cwd), strings.TrimSpace(t.Panes[j].Command)
			if err := oneLine(fmt.Sprintf("%s, pane %d", where, j+1), t.Panes[j].Cwd+t.Panes[j].Command); err != nil {
				return w, err
			}
		}
		if t.Panes == nil {
			t.Panes = []WPane{}
		}
		w.Tabs = append(w.Tabs, t)
	}
	w.Params = paramList(fw.Params)
	return w, nil
}

// fromDef turns an edited workspace into its file form, dropping empty
// panes and parameters with nothing to say.
func fromDef(d WorkspaceDef) (fileWorkspace, error) {
	fw := fileWorkspace{Name: strings.TrimSpace(d.Name), Description: strings.TrimSpace(d.Description), Window: d.Window}
	if fw.Window == "current" {
		fw.Window = ""
	}
	for _, t := range d.Tabs {
		var panes []WPane
		for _, p := range t.Panes {
			if strings.TrimSpace(p.Cwd) != "" || strings.TrimSpace(p.Command) != "" {
				panes = append(panes, p)
			}
		}
		switch len(panes) {
		case 0:
		case 1: // one pane is a plain tab
			t.Cwd, t.Command, t.Layout = panes[0].Cwd, panes[0].Command, ""
			panes = nil
		default:
			t.Cwd, t.Command = "", ""
			if t.Layout == "" {
				t.Layout = "row"
			}
		}
		t.Panes = panes
		fw.Tabs = append(fw.Tabs, t)
	}
	params, err := cleanParams(d.Params)
	if err != nil {
		return fw, err
	}
	fw.Params = params
	if strings.ContainsAny(fw.Name+fw.Description, "\r\n") {
		return fw, errors.New("the name and description must be one line each")
	}
	_, err = convertWorkspace(fw)
	return fw, err
}

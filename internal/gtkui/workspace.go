//go:build linux

package gtkui

import (
	"fmt"
	"strings"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/dayanraj/wtf/internal/snippets"
	"github.com/dayanraj/wtf/internal/uicore"
)

// openWorkspace (Open “<workspace>”) asks for the workspace's placeholders,
// shows what will open, then opens it here or in a new window. Like
// the macOS app's WorkspaceOpen.
func (w *Window) openWorkspace(ws snippets.Workspace) {
	var facts uicore.ShellFacts
	if ps := w.model.ActivePane(); ps != nil {
		facts = uicore.ShellFactsOf(ps, w.infra[ps.ID])
	}
	var sub []string
	if ws.Description != "" {
		sub = append(sub, ws.Description)
	}
	if ws.Source != "" {
		sub = append(sub, "From "+uicore.PrettyPath(ws.Source, w.app.home))
	}
	d := adw.NewMessageDialog(&w.win.Window, fmt.Sprintf("Open “%s”", ws.Name), strings.Join(sub, " · "))

	form := gtk.NewGrid()
	form.SetRowSpacing(8)
	form.SetColumnSpacing(12)
	type input struct {
		field uicore.Field
		value func() string
	}
	var inputs []input
	plan := gtk.NewLabel("")
	plan.SetXAlign(0)
	plan.SetWrap(true)
	plan.AddCSSClass("wtf-plan")

	values := func() map[string]string {
		v := map[string]string{}
		for _, in := range inputs {
			v[in.field.Name] = in.value()
		}
		return v
	}
	// The plan as it stands, redrawn on every edit.
	update := func() {
		p, err := uicore.PlanWorkspace(ws, values(), w.app.home)
		ok := err == nil
		d.SetResponseEnabled("here", ok)
		d.SetResponseEnabled("new", ok)
		if !ok {
			plan.SetMarkup(fmt.Sprintf(`<span foreground="#e5484d">%s</span>`, escapeMarkup(err.Error())))
			return
		}
		plan.SetMarkup(planMarkup(p, w.app.home))
	}

	for i, f := range uicore.WorkspaceFields(ws) {
		label := f.Name
		if f.Description != "" {
			label += " — " + f.Description
		}
		l := gtk.NewLabel(label)
		l.SetXAlign(0)
		form.Attach(l, 0, i, 1, 1)
		initial := uicore.ResolveDefault(f.Default, facts)
		if len(f.Choices) > 0 {
			dd := gtk.NewDropDownFromStrings(f.Choices)
			for j, c := range f.Choices {
				if c == initial {
					dd.SetSelected(uint(j))
				}
			}
			choices := f.Choices
			dd.NotifyProperty("selected", update)
			inputs = append(inputs, input{f, func() string {
				if s := int(dd.Selected()); s >= 0 && s < len(choices) {
					return choices[s]
				}
				return ""
			}})
			form.Attach(dd, 1, i, 1, 1)
			continue
		}
		e := gtk.NewEntry()
		e.SetText(initial)
		e.SetPlaceholderText(f.Default)
		e.SetHExpand(true)
		e.SetActivatesDefault(true)
		e.ConnectChanged(update)
		inputs = append(inputs, input{f, e.Text})
		form.Attach(e, 1, i, 1, 1)
	}
	box := gtk.NewBox(gtk.OrientationVertical, 12)
	box.SetSizeRequest(480, -1)
	if len(inputs) > 0 {
		box.Append(form)
	}
	box.Append(plan)
	d.SetExtraChild(box)

	d.AddResponse("cancel", "Cancel")
	d.AddResponse("new", "Open in New Window")
	if ws.Window == "new" {
		d.SetDefaultResponse("new")
		d.SetResponseAppearance("new", adw.ResponseSuggested)
	} else {
		d.AddResponse("here", "Open Here")
		d.SetDefaultResponse("here")
		d.SetResponseAppearance("here", adw.ResponseSuggested)
	}
	d.SetCloseResponse("cancel")
	d.ConnectResponse(func(r string) {
		if r != "here" && r != "new" {
			return
		}
		p, err := uicore.PlanWorkspace(ws, values(), w.app.home)
		if err != nil {
			w.toast(err.Error())
			return
		}
		if r == "here" {
			w.model.OpenPlan(p)
		} else {
			w.app.openWindowWith(windowStart{plan: &p})
		}
	})
	update()
	w.workspaceDialog = d
	d.Present()
}

// planMarkup lists what a plan opens: its tabs, their panes' folders and
// startup commands.
func planMarkup(p uicore.Plan, home string) string {
	var b strings.Builder
	for i, t := range p.Tabs {
		head := t.Title
		if head == "" {
			head = fmt.Sprintf("Tab %d", i+1)
		}
		if t.Group != "" {
			head += "   [" + t.Group + "]"
		}
		if len(t.Panes) > 1 {
			how := "side by side"
			if t.Layout == "column" {
				how = "stacked"
			}
			head += fmt.Sprintf("   [%d panes, %s]", len(t.Panes), how)
		}
		fmt.Fprintf(&b, "<b>%s</b>\n", escapeMarkup(head))
		for _, pn := range t.Panes {
			where := "current folder"
			if pn.Cwd != "" {
				where = uicore.PrettyPath(pn.Cwd, home)
			}
			fmt.Fprintf(&b, `    <span alpha="65%%">%s</span>`, escapeMarkup(where))
			if pn.Command != "" {
				fmt.Fprintf(&b, "  <tt>%s</tt>", escapeMarkup(pn.Command))
			}
			b.WriteByte('\n')
		}
	}
	n := len(p.Tabs)
	tabs := "Opens 1 tab"
	if n != 1 {
		tabs = fmt.Sprintf("Opens %d tabs", n)
	}
	fmt.Fprintf(&b, `<span size="small" alpha="65%%">%s; each startup command is typed into its new shell and run.</span>`, tabs)
	return b.String()
}

// workspaceEntries are the workspaces in the snippets files, read afresh
// (they're hand-edited), for the palette.
func (w *Window) workspaceEntries() []paletteEntry {
	cwd := ""
	if ps := w.model.ActivePane(); ps != nil {
		cwd = ps.Cwd
	}
	var out []paletteEntry
	for _, ws := range w.app.be.Snippets.List(cwd).Workspaces {
		detail := ws.Description
		if detail == "" {
			detail = fmt.Sprintf("%d tabs", len(ws.Tabs))
			if len(ws.Tabs) == 1 {
				detail = "1 tab"
			}
		}
		var hints []string
		if ws.Window == "new" {
			hints = append(hints, "new window")
		}
		if ws.Project {
			hints = append(hints, "project")
		}
		ws := ws
		out = append(out, paletteEntry{section: "Workspaces", label: ws.Name, detail: detail,
			hint: strings.Join(hints, " · "), run: func() { w.openWorkspace(ws) }})
	}
	return out
}

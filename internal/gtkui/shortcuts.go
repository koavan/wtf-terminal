//go:build linux

package gtkui

import (
	"fmt"
	"html"
	"strings"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/dayanraj/wtf/internal/uicore"
)

// The Keyboard Shortcuts window: every command with a shortcut, from the
// keymap itself, so it can't drift from what the keys do.

var shortcutGroups = []struct {
	title  string
	prefix []string
}{
	{"Tabs and windows", []string{"tab.", "window.", "app."}},
	{"Panes", []string{"pane."}},
	{"Terminal", []string{"term.", "find", "prompt.", "secrets.", "font."}},
	{"Side pane", []string{"sidebar."}},
	{"Other", nil},
}

// shortcutsXML is the GtkBuilder description of the window, with the
// commands works says work here.
func shortcutsXML(works func(id string) bool) string {
	used := map[string]bool{}
	var b strings.Builder
	b.WriteString(`<interface><object class="GtkShortcutsWindow" id="shortcuts"><property name="modal">1</property>` +
		`<child><object class="GtkShortcutsSection"><property name="section-name">wtf</property><property name="max-height">14</property>`)
	for _, g := range shortcutGroups {
		var rows strings.Builder
		for _, c := range uicore.Commands {
			if c.Keys == "" || used[c.ID] || !works(c.ID) {
				continue
			}
			match := g.prefix == nil
			for _, p := range g.prefix {
				match = match || strings.HasPrefix(c.ID, p)
			}
			if !match {
				continue
			}
			used[c.ID] = true
			fmt.Fprintf(&rows, `<child><object class="GtkShortcutsShortcut"><property name="title">%s</property>`+
				`<property name="accelerator">%s</property></object></child>`,
				html.EscapeString(strings.TrimSuffix(c.Title, "…")), html.EscapeString(uicore.Accel(c.Keys)))
		}
		if rows.Len() == 0 {
			continue
		}
		fmt.Fprintf(&b, `<child><object class="GtkShortcutsGroup"><property name="title">%s</property>%s</object></child>`,
			html.EscapeString(g.title), rows.String())
	}
	b.WriteString(`</object></child></object></interface>`)
	return b.String()
}

func (w *Window) showShortcuts() {
	builder := gtk.NewBuilderFromString(shortcutsXML(func(id string) bool { return w.action(id) != "" }))
	win, ok := builder.GetObject("shortcuts").Cast().(*gtk.ShortcutsWindow)
	if !ok {
		return
	}
	win.SetTransientFor(&w.win.Window)
	win.ConnectCloseRequest(func() bool {
		w.shortcuts = nil
		return false
	})
	w.shortcuts = win
	win.Present()
}

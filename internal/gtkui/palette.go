//go:build linux

package gtkui

import (
	"fmt"
	"strings"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"

	"github.com/dayanraj/wtf/internal/config"
	"github.com/dayanraj/wtf/internal/uicore"
)

// The command palette (Ctrl+Shift+P): commands, tabs and groups in one
// fuzzy list; or themes alone, previewed as you move. Like Palette.svelte
// and the macOS app's Palette. One per window.

type paletteMode int

const (
	paletteCommands paletteMode = iota
	paletteThemes
	paletteWorkspaces
	paletteSnippets
	paletteAgents
)

type paletteEntry struct {
	section string // "Commands", "Tabs", "Groups", "Themes"
	label   string
	detail  string
	hint    string // a shortcut
	theme   *config.Theme
	run     func()
}

type palette struct {
	w       *Window
	mode    paletteMode
	panel   *gtk.Box
	entry   *gtk.SearchEntry
	list    *gtk.ListBox
	scroll  *gtk.ScrolledWindow
	entries []paletteEntry
	shown   []paletteEntry
	// Running the chosen entry (a theme is kept, not undone).
	committed bool
}

// togglePalette opens the palette, or closes it (another mode replaces it).
func (w *Window) togglePalette(mode paletteMode) {
	if p := w.palette; p != nil {
		same := p.mode == mode
		p.dismiss()
		if same {
			return
		}
	}
	p := &palette{w: w, mode: mode}
	w.palette = p
	p.build()
	p.load()
	p.refilter()
	w.overlay.AddOverlay(p.panel)
	p.entry.GrabFocus()
}

func (p *palette) build() {
	p.entry = gtk.NewSearchEntry()
	p.entry.SetHExpand(true)
	switch p.mode {
	case paletteThemes:
		p.entry.SetPlaceholderText("Search themes…")
	case paletteWorkspaces:
		p.entry.SetPlaceholderText("Search workspaces…")
	case paletteSnippets:
		p.entry.SetPlaceholderText("Search snippets…")
	case paletteAgents:
		p.entry.SetPlaceholderText("Search agents: needs you, done, working, idle, or a tab, folder or prompt…")
	default:
		p.entry.SetPlaceholderText("Search commands, tabs and workspaces…")
	}
	p.list = gtk.NewListBox()
	p.list.SetSelectionMode(gtk.SelectionBrowse)
	p.list.AddCSSClass("wtf-palette-list")
	p.list.ConnectRowActivated(func(row *gtk.ListBoxRow) { p.run(row.Index()) })
	p.list.ConnectRowSelected(func(row *gtk.ListBoxRow) {
		if row != nil {
			p.previewRow(row.Index())
		}
	})
	p.scroll = gtk.NewScrolledWindow()
	p.scroll.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	p.scroll.SetMaxContentHeight(400)
	p.scroll.SetPropagateNaturalHeight(true)
	p.scroll.SetChild(p.list)

	p.panel = gtk.NewBox(gtk.OrientationVertical, 6)
	p.panel.AddCSSClass("wtf-palette")
	p.panel.SetHAlign(gtk.AlignCenter)
	p.panel.SetVAlign(gtk.AlignStart)
	p.panel.SetMarginTop(48)
	p.panel.SetSizeRequest(560, -1)
	p.panel.Append(p.entry)
	p.panel.Append(p.scroll)

	p.entry.ConnectSearchChanged(func() { p.refilter() })
	p.entry.ConnectActivate(func() {
		if row := p.list.SelectedRow(); row != nil {
			p.run(row.Index())
		}
	})
	p.entry.ConnectStopSearch(p.dismiss)
	keys := gtk.NewEventControllerKey()
	keys.SetPropagationPhase(gtk.PhaseCapture)
	keys.ConnectKeyPressed(func(keyval, _ uint, _ gdk.ModifierType) bool {
		switch keyval {
		case gdk.KEY_Down:
			p.move(1)
		case gdk.KEY_Up:
			p.move(-1)
		case gdk.KEY_Escape:
			p.dismiss()
		default:
			return false
		}
		return true
	})
	p.entry.AddController(keys)
	// Clicking elsewhere closes it.
	focus := gtk.NewEventControllerFocus()
	focus.ConnectLeave(func() {
		onMainAsync(func() {
			if p.w.palette == p && !focus.ContainsFocus() {
				p.dismiss()
			}
		})
	})
	p.panel.AddController(focus)
}

func (p *palette) load() {
	w := p.w
	m := w.model
	switch p.mode {
	case paletteThemes:
		p.loadThemes()
		return
	case paletteWorkspaces:
		p.entries = append(w.workspaceEntries(), paletteEntry{section: "Workspaces", label: "Save Window as Workspace…",
			detail: "Its tabs, groups, splits, folders and running commands", run: w.saveWindowAsWorkspace})
		return
	case paletteSnippets:
		p.entries = w.snippetEntries(true)
		return
	case paletteAgents:
		p.entries = w.agentEntries()
		return
	}
	cmds := w.windowCommands()
	for _, c := range uicore.Commands {
		if c.ID == "palette" {
			continue
		}
		run, ok := cmds[c.ID]
		if !ok {
			if a := w.action(c.ID); strings.HasPrefix(a, "app.") {
				name := strings.TrimPrefix(a, "app.")
				run = func() { w.app.gtk.ActivateAction(name, nil) }
			} else {
				continue
			}
		}
		p.entries = append(p.entries, paletteEntry{section: "Commands", label: strings.TrimSuffix(c.Title, "…"),
			hint: uicore.KeyLabel(c.Keys), run: run})
	}
	for _, t := range m.Tabs() {
		detail := ""
		if ps := m.FocusedPane(t); ps != nil {
			detail = uicore.PrettyPath(ps.Cwd, w.app.home)
		}
		if g, ok := m.Group(t.GroupID); ok && t.GroupID != 0 && g.Name != "" {
			detail = g.Name + " · " + detail
		}
		id := t.ID
		p.entries = append(p.entries, paletteEntry{section: "Tabs", label: w.app.settings.text(m.Title(t)),
			detail: detail, run: func() { m.Activate(id) }})
	}
	for _, g := range m.Groups() {
		name := g.Name
		if name == "" {
			name = "unnamed group"
		}
		verb := "Collapse"
		if g.Collapsed {
			verb = "Expand"
		}
		id := g.ID
		p.entries = append(p.entries, paletteEntry{section: "Groups", label: verb + " " + name,
			detail: fmt.Sprintf("%d tabs", len(m.TabsIn(g.ID))), run: func() { m.ToggleCollapsed(id) }})
	}
	p.entries = append(p.entries, w.workspaceEntries()...)
	p.entries = append(p.entries, w.snippetEntries(false)...)
}

func (p *palette) loadThemes() {
	s := p.w.app.settings
	set := func(spec string) func() {
		return func() {
			go func() {
				if err := p.w.app.be.Config.SetTheme(spec); err != nil {
					onMainAsync(func() {
						s.preview(nil)
						p.w.toast("Couldn't save the theme: " + err.Error())
					})
				}
			}()
		}
	}
	themes := p.w.app.be.Config.Themes()
	var auto *config.Theme
	want := "wtf Light"
	if s.style.Dark() {
		want = "wtf Dark"
	}
	for i := range themes {
		if themes[i].Name == want {
			auto = &themes[i]
		}
	}
	p.entries = append(p.entries, paletteEntry{section: "Themes", label: "Auto",
		detail: "wtf Dark or wtf Light, following the desktop", theme: auto, run: set("auto")})
	for i := range themes {
		t := &themes[i]
		detail := "Light"
		if t.Dark {
			detail = "Dark"
		}
		if !t.Builtin {
			detail += " · from themes folder"
		}
		p.entries = append(p.entries, paletteEntry{section: "Themes", label: t.Name, detail: detail, theme: t, run: set(t.Name)})
	}
}

// refilter shows the entries matching the query, best first; all of them,
// by section, when it's empty.
func (p *palette) refilter() {
	q := strings.TrimSpace(p.entry.Text())
	if q == "" {
		order := map[string]int{"Commands": 0, "Tabs": 1, "Groups": 2, "Themes": 3}
		p.shown = append([]paletteEntry{}, p.entries...)
		if p.mode != paletteThemes {
			// Tabs and groups first: switching is what the palette is for most.
			order = map[string]int{"Tabs": 0, "Groups": 1, "Commands": 2, "Workspaces": 3, "Snippets": 4}
		}
		sortStable(p.shown, func(a, b paletteEntry) bool { return order[a.section] < order[b.section] })
	} else {
		p.shown = nil
		for _, r := range uicore.FuzzyRank(q, p.entries, func(e paletteEntry) string { return e.label }) {
			p.shown = append(p.shown, r.Item)
		}
	}
	for c := p.list.FirstChild(); c != nil; c = p.list.FirstChild() {
		p.list.Remove(c)
	}
	last := ""
	for _, e := range p.shown {
		p.list.Append(p.row(e, q == "" && e.section != last))
		last = e.section
	}
	if first := p.list.RowAtIndex(0); first != nil {
		p.list.SelectRow(first)
	}
}

func sortStable[T any](s []T, less func(a, b T) bool) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && less(s[j], s[j-1]); j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func (p *palette) row(e paletteEntry, heading bool) *gtk.Box {
	box := gtk.NewBox(gtk.OrientationVertical, 0)
	if heading {
		h := gtk.NewLabel(e.section)
		h.AddCSSClass("wtf-palette-heading")
		h.SetXAlign(0)
		box.Append(h)
	}
	line := gtk.NewBox(gtk.OrientationHorizontal, 8)
	line.AddCSSClass("wtf-palette-row")
	label := gtk.NewLabel(e.label)
	label.SetXAlign(0)
	label.SetEllipsize(pango.EllipsizeEnd)
	line.Append(label)
	if e.detail != "" {
		d := gtk.NewLabel(e.detail)
		d.AddCSSClass("dim-label")
		d.SetEllipsize(pango.EllipsizeMiddle)
		d.SetXAlign(0)
		d.SetHExpand(true)
		line.Append(d)
	} else {
		label.SetHExpand(true)
	}
	if e.hint != "" {
		k := gtk.NewLabel(e.hint)
		k.AddCSSClass("dim-label")
		k.AddCSSClass("wtf-palette-key")
		line.Append(k)
	}
	box.Append(line)
	return box
}

func (p *palette) move(d int) {
	n := len(p.shown)
	if n == 0 {
		return
	}
	i := 0
	if row := p.list.SelectedRow(); row != nil {
		i = row.Index()
	}
	i = ((i+d)%n + n) % n
	row := p.list.RowAtIndex(i)
	p.list.SelectRow(row)
	row.GrabFocus()
	p.entry.GrabFocus()
}

// previewRow shows the theme under the selection.
func (p *palette) previewRow(i int) {
	if p.mode == paletteThemes && i >= 0 && i < len(p.shown) {
		p.w.app.settings.preview(p.shown[i].theme)
	}
}

func (p *palette) run(i int) {
	if i < 0 || i >= len(p.shown) {
		return
	}
	e := p.shown[i]
	p.committed = true
	p.dismiss()
	e.run()
}

func (p *palette) dismiss() {
	w := p.w
	if w.palette != p {
		return
	}
	w.palette = nil
	if p.mode == paletteThemes && !p.committed {
		w.app.settings.preview(nil)
	}
	w.overlay.RemoveOverlay(p.panel)
	w.focusActive()
}

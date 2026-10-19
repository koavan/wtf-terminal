//go:build linux

package gtkui

import (
	"fmt"
	"math"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/graphene"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/dayanraj/wtf/internal/uicore"
)

func init() {
	registerSidebarTab(sidebarTabInfo{tab: uicore.SidebarPins, label: "Pinned Output", icon: "view-pin-symbolic",
		cmd: "sidebar.pins", make: func(w *Window) sidePaneTab { return newPinsTab(w) }})
}

// pinsTab is the Pinned tab: output kept in view, newest first; a running
// command's pin follows it (every 1.5s, only while this tab shows). Like
// PinsView.svelte and the macOS app's PinsView.
type pinsTab struct {
	w       *Window
	root    *gtk.Box
	header  *paneHeader
	trash   *gtk.Button
	cards   *gtk.Box
	note    *gtk.Label
	follow  coreglib.SourceHandle
	off     func()
	popover *menuPopover
}

func newPinsTab(w *Window) *pinsTab {
	t := &pinsTab{w: w}
	t.header = newPaneHeader("Pinned output")
	t.trash = t.header.tool("user-trash-symbolic", "Unpin all", t.unpinAll)
	t.cards = gtk.NewBox(gtk.OrientationVertical, 8)
	t.cards.AddCSSClass("wtf-pins")
	t.note = wrapLabel("", "wtf-side-note")
	scroll := gtk.NewScrolledWindow()
	scroll.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	scroll.SetVExpand(true)
	inner := gtk.NewBox(gtk.OrientationVertical, 0)
	inner.Append(t.note)
	inner.Append(t.cards)
	scroll.SetChild(inner)
	t.root = gtk.NewBox(gtk.OrientationVertical, 4)
	t.root.Append(t.header.root)
	t.root.Append(scroll)
	t.off = w.records.observe(t.rebuild)
	t.rebuild()
	return t
}

func (t *pinsTab) widget() gtk.Widgetter { return t.root }
func (t *pinsTab) paneChanged()          {}
func (t *pinsTab) commandFinished()      {}

func (t *pinsTab) close() {
	t.off()
	t.stopFollowing()
}

func (t *pinsTab) stopFollowing() {
	if t.follow != 0 {
		coreglib.SourceRemove(t.follow)
		t.follow = 0
	}
}

func (t *pinsTab) rebuild() {
	r := t.w.records
	pins := r.pins.List()
	t.trash.SetSensitive(len(pins) > 0)
	for c := t.cards.FirstChild(); c != nil; c = t.cards.FirstChild() {
		t.cards.Remove(c)
	}
	for _, p := range pins {
		t.cards.Append(t.card(p))
	}
	t.note.SetVisible(len(pins) == 0)
	t.note.SetText("Nothing pinned. Keep a command's output in view while you work on: click its ✓/✗ mark (or right-click its output) " +
		"and choose Pin Output, or select text and right-click → Pin Selection. A command still running keeps its pin up to date.")
	// Follow running commands only while there are any.
	switch live := len(r.pins.Live()) > 0; {
	case !live:
		t.stopFollowing()
	case t.follow == 0:
		t.follow = coreglib.TimeoutAdd(1500, func() bool {
			t.followLive()
			return true
		})
	}
}

// followLive reads running commands' output into their pins.
func (t *pinsTab) followLive() {
	r := t.w.records
	changed := false
	for _, p := range r.pins.Live() {
		pane := t.w.panes[p.PaneID]
		if p.BlockID == nil || pane == nil {
			continue
		}
		if text, ok := pane.tracker.Output(*p.BlockID); ok && text != p.Text {
			r.pins.Update(p.PaneID, *p.BlockID, text, nil)
			changed = true
		}
	}
	if changed {
		r.changed()
	}
}

// card is one pin: a header (status, title, copy, collapse, unpin) over
// its text.
func (t *pinsTab) card(p uicore.Pin) *gtk.Box {
	w, s := t.w, t.w.app.settings
	card := gtk.NewBox(gtk.OrientationVertical, 4)
	card.AddCSSClass("wtf-pin")
	head := gtk.NewBox(gtk.OrientationHorizontal, 6)
	head.AddCSSClass("wtf-pin-head")
	var mark *gtk.Box
	switch {
	case p.BlockID == nil:
		mark = dot("tone-running")
	case p.Running:
		mark = dot("tone-running")
	case p.ExitCode != nil && *p.ExitCode == 0:
		mark = dot("dot-git-added")
	default:
		mark = dot("dot-git-deleted")
	}
	head.Append(mark)
	texts := gtk.NewBox(gtk.OrientationVertical, 0)
	texts.SetHExpand(true)
	title := sideLabel(s.text(p.Title), "wtf-pin-title")
	if p.BlockID != nil {
		title.AddCSSClass("monospace")
	}
	title.SetTooltipText(s.text(p.Title))
	sub := ""
	switch {
	case p.Running:
		sub = "running · following it"
	case p.Duration != nil:
		sub = uicore.Duration(*p.Duration)
		if p.ExitCode != nil && *p.ExitCode != 0 {
			sub += fmt.Sprintf(" · exit %d", *p.ExitCode)
		}
	default:
		sub = "pinned " + clockTime(p.PinnedAt)
	}
	if tab, ok := w.model.TabOf(p.PaneID); ok {
		sub += " · " + w.model.Title(tab)
	} else {
		sub += " · closed tab"
	}
	subLabel := sideLabel(sub, "dim-label")
	subLabel.AddCSSClass("wtf-side-detail")
	subLabel.SetTooltipText(uicore.PrettyPath(p.Cwd, w.app.home))
	texts.Append(title)
	texts.Append(subLabel)
	head.Append(texts)
	id := p.ID
	tool := func(icon, tip string, run func()) {
		b := gtk.NewButtonFromIconName(icon)
		b.AddCSSClass("flat")
		b.AddCSSClass("wtf-tool")
		b.SetTooltipText(tip)
		b.SetFocusOnClick(false)
		b.SetVAlign(gtk.AlignCenter)
		b.ConnectClicked(run)
		head.Append(b)
	}
	tool("edit-copy-symbolic", "Copy", func() { t.copy(p) })
	if p.Collapsed {
		tool("pan-down-symbolic", "Expand", func() { t.toggle(id) })
	} else {
		tool("pan-up-symbolic", "Collapse", func() { t.toggle(id) })
	}
	tool("window-close-symbolic", "Unpin", func() { t.unpin(id) })
	click := gtk.NewGestureClick()
	click.SetButton(0)
	click.ConnectPressed(func(n int, x, y float64) {
		switch click.CurrentButton() {
		case gdk.BUTTON_PRIMARY:
			if n == 2 {
				t.toggle(id)
			}
		case gdk.BUTTON_SECONDARY:
			if pt, ok := head.ComputePoint(t.root, graphene.NewPointAlloc().Init(float32(x), float32(y))); ok {
				x, y = float64(pt.X()), float64(pt.Y())
			}
			t.menu().show(x, y, t.menuItems(p), nil)
		}
	})
	head.AddController(click)
	card.Append(head)

	if p.Collapsed {
		return card
	}
	if p.Truncated || p.Text == "" {
		note := "No output"
		switch {
		case p.Text == "" && p.Running:
			note = "No output yet"
		case p.Text != "":
			note = fmt.Sprintf("Showing the last %dk characters", int(math.Round(float64(len([]rune(p.Text)))/1000)))
		}
		card.Append(sideLabel(note, "wtf-side-note"))
	}
	if p.Text != "" {
		view := gtk.NewTextView()
		view.SetEditable(false)
		view.SetMonospace(true)
		view.SetCursorVisible(false)
		view.SetWrapMode(gtk.WrapNone)
		view.AddCSSClass("wtf-pin-text")
		view.Buffer().SetText(s.text(p.Text))
		scroll := gtk.NewScrolledWindow()
		scroll.SetChild(view)
		// As tall as the output, up to 180px; longer output scrolls.
		scroll.SetMaxContentHeight(180)
		scroll.SetPropagateNaturalHeight(true)
		card.Append(scroll)
		// Keep the end in view: that's where a running command writes.
		coreglib.IdleAdd(func() {
			if adj := scroll.VAdjustment(); adj != nil {
				adj.SetValue(adj.Upper() - adj.PageSize())
			}
		})
	}
	return card
}

// menu is the tab's popup, parented to the tab (not a list).
func (t *pinsTab) menu() *menuPopover {
	if t.popover == nil {
		t.popover = newMenuPopover(t.root)
	}
	return t.popover
}

func (t *pinsTab) menuItems(p uicore.Pin) []*menuItem {
	w := t.w
	var items []*menuItem
	if w.model.Panes[p.PaneID] != nil {
		label := "Show in Terminal"
		if p.BlockID == nil {
			label = "Show Tab"
		}
		items = append(items, item(label, func() {
			w.showPane(p.PaneID)
			if p.BlockID != nil {
				if pane := w.panes[p.PaneID]; pane == nil || !pane.revealCommand(*p.BlockID) {
					w.toast("That command has scrolled out of the terminal")
				}
			}
		}))
	}
	items = append(items, item("Copy Output", func() { t.copy(p) }))
	if p.BlockID != nil {
		items = append(items, item("Copy Command", func() {
			w.win.Clipboard().SetText(w.app.settings.text(p.Title))
			w.toast("Copied command")
		}))
		items = append(items, item("Save as Snippet…", func() { w.saveAsSnippet(p.Title) }))
	}
	label := "Collapse"
	if p.Collapsed {
		label = "Expand"
	}
	return append(items, menuSection, item(label, func() { t.toggle(p.ID) }), item("Unpin", func() { t.unpin(p.ID) }))
}

func (t *pinsTab) copy(p uicore.Pin) {
	t.w.win.Clipboard().SetText(t.w.app.settings.text(p.Text))
	t.w.toast("Copied pinned output")
}

func (t *pinsTab) toggle(id int) {
	t.w.records.pins.Toggle(id)
	t.w.records.changed()
}

func (t *pinsTab) unpin(id int) {
	t.w.records.pins.Remove(id)
	t.w.records.changed()
}

func (t *pinsTab) unpinAll() {
	n := len(t.w.records.pins.List())
	if n == 0 {
		return
	}
	t.w.app.confirm(&t.w.win.Window, "Unpin everything?",
		fmt.Sprintf("Removes %d pinned %s. The terminals keep their own scrollback.", n, plural(n, "output")), "Unpin All", func() {
			t.w.records.pins.Clear()
			t.w.records.changed()
		})
}

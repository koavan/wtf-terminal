//go:build linux

package gtkui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"

	"github.com/dayanraj/wtf/internal/uicore"
)

// broadcast is input broadcast in a window: typing in one pane also goes to
// the other panes of its tab group, or of its split tab. In memory only (it
// never survives a restart). Like the macOS app's
// Broadcast.
type broadcast struct {
	groups map[int]bool
	tabs   map[int]bool
	bar    *gtk.Box
	label  *gtk.Label
}

func newBroadcast(w *Window) *broadcast {
	b := &broadcast{groups: map[int]bool{}, tabs: map[int]bool{}}
	b.label = gtk.NewLabel("")
	b.label.SetEllipsize(pango.EllipsizeEnd)
	b.label.SetXAlign(0)
	b.label.SetHExpand(true)
	stop := gtk.NewButtonWithLabel("Stop")
	stop.SetTooltipText("Stop broadcasting (Ctrl+Alt+Shift+B)")
	stop.SetFocusOnClick(false)
	stop.ConnectClicked(func() { w.stopBroadcast() })
	b.bar = gtk.NewBox(gtk.OrientationHorizontal, 8)
	b.bar.AddCSSClass("wtf-broadcast")
	b.bar.Append(gtk.NewImageFromIconName("send-to-symbolic"))
	b.bar.Append(b.label)
	b.bar.Append(stop)
	b.bar.SetVisible(false)
	return b
}

func (b *broadcast) on() bool { return len(b.groups) > 0 || len(b.tabs) > 0 }

// scope is the tabs a tab's input goes to: its whole group when the group
// broadcasts, else just itself when its panes do.
func (w *Window) broadcastScope(tab int) []int {
	b, m := w.broadcast, w.model
	t, ok := m.Tab(tab)
	if !ok {
		return nil
	}
	if t.GroupID != 0 && b.groups[t.GroupID] {
		return tabIDs(m.TabsIn(t.GroupID))
	}
	if b.tabs[tab] {
		return []int{tab}
	}
	return nil
}

// broadcastTargets are the panes a pane's typing goes to, besides itself.
func (w *Window) broadcastTargets(pane int) []int {
	if !w.broadcast.on() {
		return nil
	}
	t, ok := w.model.TabOf(pane)
	if !ok {
		return nil
	}
	var out []int
	for _, id := range w.broadcastScope(t.ID) {
		if x, ok := w.model.Tab(id); ok {
			for _, p := range x.Root.Leaves() {
				if p != pane {
					out = append(out, p)
				}
			}
		}
	}
	return out
}

func (w *Window) isBroadcasting(tab int) bool { return len(w.broadcastScope(tab)) > 0 }

// fanOut sends what was typed in a pane to the panes it broadcasts to:
// only typing in the pane with the keyboard, never the terminal's own
// answers to a program (cursor reports and the like).
func (w *Window) fanOut(from *Pane, text string) {
	if !w.broadcast.on() || !from.term.HasFocus() || uicore.IsTerminalReport(text) {
		return
	}
	for _, id := range w.broadcastTargets(from.id) {
		if p := w.panes[id]; p != nil {
			p.write(text)
		}
	}
}

func (w *Window) toggleBroadcastGroup(g int) {
	b := w.broadcast
	b.groups[g] = !b.groups[g]
	if !b.groups[g] {
		delete(b.groups, g)
	}
	w.broadcastChanged()
}

func (w *Window) toggleBroadcastTab(tab int) {
	b := w.broadcast
	b.tabs[tab] = !b.tabs[tab]
	if !b.tabs[tab] {
		delete(b.tabs, tab)
	}
	w.broadcastChanged()
}

func (w *Window) stopBroadcast() {
	w.broadcast.groups, w.broadcast.tabs = map[int]bool{}, map[int]bool{}
	w.broadcastChanged()
}

// toggleBroadcast (Ctrl+Alt+Shift+B) stops every broadcast, or starts one
// for the active tab's group (more than one tab) or its split panes.
func (w *Window) toggleBroadcast() {
	if w.broadcast.on() {
		w.stopBroadcast()
		return
	}
	t, ok := w.model.Active()
	switch {
	case !ok:
	case t.GroupID != 0 && len(w.model.TabsIn(t.GroupID)) > 1:
		w.toggleBroadcastGroup(t.GroupID)
	case t.Root.IsSplit():
		w.toggleBroadcastTab(t.ID)
	default:
		w.toast("Broadcast needs a tab group or split panes")
	}
}

// pruneBroadcast drops broadcasts whose group or tab is gone.
func (w *Window) pruneBroadcast() {
	b, m := w.broadcast, w.model
	changed := false
	for g := range b.groups {
		if len(m.TabsIn(g)) == 0 {
			delete(b.groups, g)
			changed = true
		}
	}
	for t := range b.tabs {
		if _, ok := m.Tab(t); !ok {
			delete(b.tabs, t)
			changed = true
		}
	}
	if changed {
		w.broadcastChanged()
	}
}

// describeBroadcast is "2 tabs in “prod”", "2 panes of a tab".
func (w *Window) describeBroadcast() string {
	b, m := w.broadcast, w.model
	var parts []string
	groups := make([]int, 0, len(b.groups))
	for g := range b.groups {
		groups = append(groups, g)
	}
	slices.Sort(groups)
	for _, g := range groups {
		group, ok := m.Group(g)
		if !ok {
			continue
		}
		n := len(m.TabsIn(g))
		tabs := fmt.Sprintf("%d %s", n, map[bool]string{true: "tab", false: "tabs"}[n == 1])
		if group.Name == "" {
			parts = append(parts, tabs+" in a group")
		} else {
			parts = append(parts, fmt.Sprintf("%s in “%s”", tabs, group.Name))
		}
	}
	tabs := make([]int, 0, len(b.tabs))
	for t := range b.tabs {
		tabs = append(tabs, t)
	}
	slices.Sort(tabs)
	for _, t := range tabs {
		if tab, ok := m.Tab(t); ok {
			parts = append(parts, fmt.Sprintf("%d panes of a tab", len(tab.Root.Leaves())))
		}
	}
	return strings.Join(parts, " and ")
}

// broadcastChanged shows the bar and outlines the panes that receive input.
func (w *Window) broadcastChanged() {
	b := w.broadcast
	on := b.on()
	b.bar.SetVisible(on)
	if on {
		b.label.SetMarkup("<b>Broadcasting input</b> — typing goes to " + escapeMarkup(w.describeBroadcast()))
	}
	for id, p := range w.panes {
		p.outline.SetVisible(on && len(w.broadcastTargets(id)) > 0)
	}
	w.strip.shape = ""
	w.strip.refresh()
}

func escapeMarkup(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

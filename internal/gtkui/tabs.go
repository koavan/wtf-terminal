//go:build linux

package gtkui

import (
	"fmt"
	"strconv"
	"strings"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/graphene"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"

	"github.com/dayanraj/wtf/internal/uicore"
)

// tabStrip is the tabs in the header bar: each tab's title, a dot for
// output in the background, ✓ or ✗ for a command that finished there, and
// a close button; group chips before their tabs. Click to switch,
// double-click to rename, middle-click to close, right-click for the menu;
// drag to reorder, or out of the window to tear the tab off. Drawn by wtf
// rather than AdwTabBar, so groups look as they do on macOS.
type tabStrip struct {
	w      *Window
	root   *gtk.Box
	tabs   *gtk.Box
	scroll *gtk.ScrolledWindow
	menu   *menuPopover
	shape  string
	// The widget of each tab shown, for menus and renaming.
	items map[int]*gtk.Box
}

// What a dragged tab carries: "wtf-tab <window> <tab>".
const tabDragPrefix = "wtf-tab "

func newTabStrip(w *Window) *tabStrip {
	s := &tabStrip{w: w, items: map[int]*gtk.Box{}}
	s.tabs = gtk.NewBox(gtk.OrientationHorizontal, 2)
	s.tabs.AddCSSClass("wtf-tabs")
	s.tabs.SetHAlign(gtk.AlignStart)
	s.scroll = gtk.NewScrolledWindow()
	s.scroll.SetPolicy(gtk.PolicyExternal, gtk.PolicyNever)
	s.scroll.SetChild(s.tabs)
	s.scroll.SetPropagateNaturalWidth(true)
	// Only as wide as the tabs: + follows them.
	s.scroll.SetHExpand(false)
	add := gtk.NewButtonFromIconName("list-add-symbolic")
	add.AddCSSClass("flat")
	add.SetTooltipText("New Tab (Ctrl+Shift+T)")
	add.ConnectClicked(func() { w.model.Open(uicore.OpenOptions{}) })
	s.root = gtk.NewBox(gtk.OrientationHorizontal, 4)
	s.root.SetHExpand(true)
	s.root.SetHAlign(gtk.AlignFill)
	s.root.Append(s.scroll)
	s.root.Append(add)
	// The rest of the bar: room to drag the window by.
	space := gtk.NewBox(gtk.OrientationHorizontal, 0)
	space.SetHExpand(true)
	s.root.Append(space)
	s.menu = newMenuPopover(s.root)

	// Dropped past the last tab: to the end.
	drop := gtk.NewDropTarget(coreglib.TypeString, gdk.ActionMove)
	drop.ConnectDrop(func(v *coreglib.Value, _, _ float64) bool { return s.dropped(v, 0) })
	s.root.AddController(drop)
	return s
}

// refresh redraws the strip when anything it shows changed.
func (s *tabStrip) refresh() {
	m := s.w.model
	dark := s.w.app.settings.theme().Dark
	type entry struct {
		tab   *uicore.Tab
		group *uicore.TabGroup
	}
	var entries []entry
	var shape strings.Builder
	lastGroup := 0
	for _, t := range m.Tabs() {
		if t.GroupID != 0 && t.GroupID != lastGroup {
			if g, ok := m.Group(t.GroupID); ok {
				entries = append(entries, entry{group: &g})
				fmt.Fprintf(&shape, "g%d:%s:%s:%v:%d|", g.ID, g.Name, g.Color, g.Collapsed, len(m.TabsIn(g.ID)))
			}
		}
		lastGroup = t.GroupID
		if m.IsHidden(t) {
			continue
		}
		t := t
		entries = append(entries, entry{tab: &t})
		fmt.Fprintf(&shape, "t%d:%s:%v:%v:%v:%d:%v|", t.ID, s.w.app.settings.text(m.Title(t)), t.ID == m.ActiveID(),
			t.Activity, t.Finished, t.GroupID, s.w.isBroadcasting(t.ID))
	}
	fmt.Fprintf(&shape, "%v", dark)
	if shape.String() == s.shape {
		return
	}
	s.shape = shape.String()

	for c := s.tabs.FirstChild(); c != nil; c = s.tabs.FirstChild() {
		s.tabs.Remove(c)
	}
	s.items = map[int]*gtk.Box{}
	for _, e := range entries {
		if e.group != nil {
			s.tabs.Append(s.chip(*e.group))
			continue
		}
		item := s.item(*e.tab)
		s.items[e.tab.ID] = item
		s.tabs.Append(item)
	}
}

// item is one tab.
func (s *tabStrip) item(t uicore.Tab) *gtk.Box {
	m := s.w.model
	box := gtk.NewBox(gtk.OrientationHorizontal, 4)
	box.AddCSSClass("wtf-tab")
	if t.ID == m.ActiveID() {
		box.AddCSSClass("active")
	}
	if g, ok := m.Group(t.GroupID); ok && t.GroupID != 0 {
		box.AddCSSClass("group-" + string(g.Color))
	}
	title := s.w.app.settings.text(m.Title(t))
	label := gtk.NewLabel(title)
	label.SetEllipsize(pango.EllipsizeEnd)
	label.SetMaxWidthChars(22)
	label.SetWidthChars(4)
	label.SetHExpand(true)
	box.SetTooltipText(title)
	box.Append(label)
	switch {
	case t.Finished != nil && *t.Finished:
		box.Append(statusLabel("✓", "ok"))
	case t.Finished != nil:
		box.Append(statusLabel("✗", "fail"))
	case t.Activity:
		box.Append(statusLabel("●", "activity"))
	}
	if s.w.isBroadcasting(t.ID) {
		icon := gtk.NewImageFromIconName("send-to-symbolic")
		icon.AddCSSClass("wtf-tab-broadcast")
		icon.SetTooltipText("Broadcasting input")
		box.Append(icon)
	}
	closeBtn := gtk.NewButtonFromIconName("window-close-symbolic")
	closeBtn.AddCSSClass("flat")
	closeBtn.AddCSSClass("wtf-tab-close")
	closeBtn.SetTooltipText("Close Tab")
	id := t.ID
	closeBtn.ConnectClicked(func() { s.w.closeTabs([]int{id}) })
	box.Append(closeBtn)

	click := gtk.NewGestureClick()
	click.SetButton(0)
	click.ConnectPressed(func(n int, x, y float64) {
		switch click.CurrentButton() {
		case gdk.BUTTON_PRIMARY:
			if n == 2 {
				s.rename(id)
			} else {
				m.Activate(id)
			}
		case gdk.BUTTON_MIDDLE:
			s.w.closeTabs([]int{id})
		case gdk.BUTTON_SECONDARY:
			px, py := s.toRoot(box, x, y)
			s.menu.show(px, py, s.w.tabMenu(id), nil)
		}
	})
	box.AddController(click)

	// Dragging: reorder here, move to another window, or tear off.
	drag := gtk.NewDragSource()
	drag.SetActions(gdk.ActionMove)
	drag.ConnectPrepare(func(x, y float64) *gdk.ContentProvider {
		return gdk.NewContentProviderForValue(coreglib.NewValue(fmt.Sprintf("%s%s %d", tabDragPrefix, s.w.name, id)))
	})
	drag.ConnectDragCancel(func(_ gdk.Dragger, reason gdk.DragCancelReason) bool {
		// Let go outside every wtf window: the tab gets a window of its own.
		if reason == gdk.DragCancelNoTarget {
			coreglib.IdleAdd(func() { s.w.moveToNewWindow(id) })
			return true
		}
		return false
	})
	box.AddController(drag)
	drop := gtk.NewDropTarget(coreglib.TypeString, gdk.ActionMove)
	drop.ConnectDrop(func(v *coreglib.Value, x, _ float64) bool {
		// The right half of a tab drops after it.
		before := id
		if x > float64(box.Width())/2 {
			before = s.after(id)
		}
		return s.dropped(v, before)
	})
	box.AddController(drop)
	return box
}

func statusLabel(text, class string) *gtk.Label {
	l := gtk.NewLabel(text)
	l.AddCSSClass("wtf-tab-status")
	l.AddCSSClass(class)
	return l
}

// after is the visible tab after id (0 at the end).
func (s *tabStrip) after(id int) int {
	v := s.w.model.Visible()
	for i, t := range v {
		if t.ID == id && i+1 < len(v) {
			return v[i+1].ID
		}
	}
	return 0
}

// dropped moves a dragged tab before another (0: to the end), from this
// window or another.
func (s *tabStrip) dropped(v *coreglib.Value, before int) bool {
	text, _ := v.GoValue().(string)
	rest, ok := strings.CutPrefix(text, tabDragPrefix)
	if !ok {
		return false
	}
	window, idText, _ := strings.Cut(rest, " ")
	id, err := strconv.Atoi(idText)
	if err != nil {
		return false
	}
	if window == s.w.name {
		if id != before {
			s.w.model.MoveBefore(id, before)
		}
		return true
	}
	from := s.w.app.window(window)
	if from == nil {
		return false
	}
	coreglib.IdleAdd(func() { from.moveTab(id, s.w, before) })
	return true
}

// chip is a group's label: click to fold or unfold it, right-click for its
// menu.
func (s *tabStrip) chip(g uicore.TabGroup) *gtk.Button {
	name := g.Name
	if g.Collapsed {
		n := len(s.w.model.TabsIn(g.ID))
		if name == "" {
			name = fmt.Sprintf("%d", n)
		} else {
			name = fmt.Sprintf("%s (%d)", name, n)
		}
	}
	b := gtk.NewButtonWithLabel(name)
	b.AddCSSClass("wtf-chip")
	b.AddCSSClass("group-" + string(g.Color))
	if g.Name == "" && !g.Collapsed {
		b.AddCSSClass("empty")
	}
	b.SetTooltipText("Click to fold or unfold the group; right-click for its menu")
	id := g.ID
	b.ConnectClicked(func() { s.w.model.ToggleCollapsed(id) })
	click := gtk.NewGestureClick()
	click.SetButton(gdk.BUTTON_SECONDARY)
	click.ConnectPressed(func(_ int, x, y float64) {
		px, py := s.toRoot(b, x, y)
		s.menu.show(px, py, s.w.groupMenu(id), nil)
	})
	b.AddController(click)
	return b
}

// toRoot turns a point in a strip widget into the strip's coordinates.
func (s *tabStrip) toRoot(w gtk.Widgetter, x, y float64) (float64, float64) {
	pt, ok := gtk.BaseWidget(w).ComputePoint(s.root, graphene.NewPointAlloc().Init(float32(x), float32(y)))
	if !ok {
		return x, y
	}
	return float64(pt.X()), float64(pt.Y())
}

// rename edits a tab's name in place; empty goes back to the automatic one.
func (s *tabStrip) rename(id int) {
	item := s.items[id]
	t, ok := s.w.model.Tab(id)
	if item == nil || !ok {
		return
	}
	entry := gtk.NewEntry()
	entry.SetText(s.w.model.Title(t))
	entry.SetPlaceholderText("Automatic name")
	pop := gtk.NewPopover()
	pop.SetChild(entry)
	pop.SetParent(item)
	done := false
	entry.ConnectActivate(func() {
		done = true
		s.w.model.Rename(id, entry.Text())
		pop.Popdown()
	})
	pop.ConnectClosed(func() {
		coreglib.IdleAdd(func() {
			pop.Unparent()
			if !done {
				s.w.focusActive()
			}
		})
	})
	pop.Popup()
	entry.GrabFocus()
}

// Menus

func (w *Window) tabMenu(id int) []*menuItem {
	m := w.model
	t, ok := m.Tab(id)
	if !ok {
		return nil
	}
	items := []*menuItem{
		item("New Tab to the Right", func() { m.OpenNextTo(id, false) }),
		item("Duplicate", func() { m.OpenNextTo(id, true) }),
		item("Rename Tab…", func() { coreglib.IdleAdd(func() { w.strip.rename(id) }) }),
		menuSection,
		item("Add Tab to New Group", func() { m.CreateGroup(id) }),
	}
	for _, g := range m.Groups() {
		if g.ID == t.GroupID {
			continue
		}
		name := g.Name
		if name == "" {
			name = "Unnamed " + string(g.Color) + " group"
		}
		gid := g.ID
		items = append(items, item("Add to "+name, func() { m.AddToGroup(id, gid) }))
	}
	if t.GroupID != 0 {
		items = append(items, item("Remove from Group", func() { m.RemoveFromGroup(id) }))
		gid := t.GroupID
		label := "Broadcast Input to Group"
		if w.broadcast.groups[gid] {
			label = "Stop Broadcasting Input"
		}
		items = append(items, item(label, func() { w.toggleBroadcastGroup(gid) }))
	}
	if t.Root.IsSplit() {
		label := "Broadcast Input to Panes"
		if w.broadcast.tabs[id] {
			label = "Stop Broadcasting to Panes"
		}
		items = append(items, item(label, func() { w.toggleBroadcastTab(id) }))
	}
	items = append(items, menuSection)
	if len(m.Tabs()) > 1 {
		items = append(items, item("Move to New Window", func() { w.moveToNewWindow(id) }))
	}
	for _, other := range w.app.windows {
		if other != w {
			o := other
			items = append(items, item("Move to “"+o.win.Title()+"”", func() { w.moveTab(id, o, 0) }))
		}
	}
	others := []int{}
	for _, x := range m.Tabs() {
		if x.ID != id {
			others = append(others, x.ID)
		}
	}
	items = append(items, menuSection)
	if len(others) > 0 {
		items = append(items, item("Close Other Tabs", func() { w.closeTabs(others) }))
	}
	return append(items, item("Close Tab", func() { w.closeTabs([]int{id}) }))
}

func (w *Window) groupMenu(id int) []*menuItem {
	m := w.model
	g, ok := m.Group(id)
	if !ok {
		return nil
	}
	items := []*menuItem{
		item("Name Group…", func() { coreglib.IdleAdd(func() { w.nameGroup(id) }) }),
		item("New Tab in Group", func() { m.NewTabInGroup(id) }),
		menuSection,
	}
	for _, c := range uicore.GroupColors {
		if c == g.Color {
			continue
		}
		color := c
		items = append(items, item(strings.ToUpper(string(c[:1]))+string(c[1:]), func() { m.SetGroup(id, nil, &color) }))
	}
	return append(items, menuSection,
		item("Ungroup", func() { m.Ungroup(id) }),
		item("Close Group", func() { w.closeTabs(tabIDs(m.TabsIn(id))) }))
}

func tabIDs(tabs []uicore.Tab) []int {
	ids := make([]int, len(tabs))
	for i, t := range tabs {
		ids[i] = t.ID
	}
	return ids
}

// nameGroup asks for a group's name.
func (w *Window) nameGroup(id int) {
	g, ok := w.model.Group(id)
	if !ok {
		return
	}
	w.app.ask(&w.win.Window, "Name Group", "", g.Name, "Name", func(name string) {
		name = strings.TrimSpace(name)
		w.model.SetGroup(id, &name, nil)
	})
}

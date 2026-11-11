//go:build linux

package gtkui

import (
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/dayanraj/wtf/internal/uicore"
)

// sidePaneTab is a side-pane tab's view. Only the shown tab exists: it's
// made when its tab is chosen and dropped when another is (or the pane
// closes), so hidden tabs do no work.
type sidePaneTab interface {
	widget() gtk.Widgetter
	// The focused pane, or something about it, changed.
	paneChanged()
	// A command finished in the focused pane.
	commandFinished()
	// It's going away: stop anything it started.
	close()
}

type sidebarTabInfo struct {
	tab   uicore.SidebarTab
	label string
	icon  string
	cmd   string // its command (and shortcut)
	make  func(w *Window) sidePaneTab
}

// sidebarTabs are the tabs the Linux app has so far; the rest come with M5.
var sidebarTabs []sidebarTabInfo

func registerSidebarTab(t sidebarTabInfo) {
	// In the order of uicore.SidebarTabs, whatever the file order.
	sidebarTabs = append(sidebarTabs, t)
	order := map[uicore.SidebarTab]int{}
	for i, x := range uicore.SidebarTabs {
		order[x] = i
	}
	sortStable(sidebarTabs, func(a, b sidebarTabInfo) bool { return order[a.tab] < order[b.tab] })
}

func sidebarTabInfoOf(tab uicore.SidebarTab) (sidebarTabInfo, bool) {
	for _, t := range sidebarTabs {
		if t.tab == tab {
			return t, true
		}
	}
	return sidebarTabInfo{}, false
}

// sidebar is the side pane: an icon rail and the chosen tab, left of the
// terminals, resized by its divider. Like the macOS app's
// Sidebar.
type sidebar struct {
	w       *Window
	root    *gtk.Box
	rail    *gtk.Box
	buttons map[uicore.SidebarTab]*gtk.Button
	content sidePaneTab
	holder  *gtk.Box
	shown   uicore.SidebarTab
}

func newSidebar(w *Window) *sidebar {
	s := &sidebar{w: w, buttons: map[uicore.SidebarTab]*gtk.Button{}}
	s.rail = gtk.NewBox(gtk.OrientationVertical, 2)
	s.rail.AddCSSClass("wtf-rail")
	for _, t := range sidebarTabs {
		b := gtk.NewButtonFromIconName(t.icon)
		b.AddCSSClass("flat")
		b.AddCSSClass("wtf-rail-button")
		tip := t.label
		if c, ok := uicore.CommandByID(t.cmd); ok && c.Keys != "" {
			tip += " (" + uicore.KeyLabel(c.Keys) + ")"
		}
		b.SetTooltipText(tip)
		b.SetFocusOnClick(false)
		tab := t.tab
		b.ConnectClicked(func() {
			w.sidebarState.Tab = tab
			w.layoutSidebar()
		})
		s.buttons[t.tab] = b
		s.rail.Append(b)
	}
	// At the bottom: the tools check, and help for the tab shown.
	spacer := gtk.NewBox(gtk.OrientationVertical, 0)
	spacer.SetVExpand(true)
	s.rail.Append(spacer)
	for _, b := range []struct {
		icon, tip string
		run       func()
	}{
		{"applications-utilities-symbolic", "Command-Line Tools: kubectl, AWS CLI, git, OpenSSH", func() { w.showTools("") }},
		{"help-about-symbolic", "Help for this tab (F1)", w.showHelp},
	} {
		btn := gtk.NewButtonFromIconName(b.icon)
		btn.AddCSSClass("flat")
		btn.AddCSSClass("wtf-rail-button")
		btn.SetTooltipText(b.tip)
		btn.SetFocusOnClick(false)
		btn.ConnectClicked(b.run)
		s.rail.Append(btn)
	}
	s.holder = gtk.NewBox(gtk.OrientationVertical, 0)
	s.holder.SetHExpand(true)
	s.holder.AddCSSClass("wtf-side-content")
	s.root = gtk.NewBox(gtk.OrientationHorizontal, 0)
	s.root.AddCSSClass("wtf-sidebar")
	s.root.Append(s.rail)
	s.root.Append(gtk.NewSeparator(gtk.OrientationVertical))
	s.root.Append(s.holder)
	return s
}

// update shows the window's chosen tab, making its view if it changed.
func (s *sidebar) update() {
	// Investigate (#27) moved to the Claude pane: an old session's tab is the first.
	want := s.w.sidebarState.Tab
	info, ok := sidebarTabInfoOf(want)
	if !ok && len(sidebarTabs) > 0 {
		info = sidebarTabs[0]
	}
	if info.tab != s.shown || s.content == nil {
		s.drop()
		s.shown = info.tab
		if info.make != nil {
			s.content = info.make(s.w)
			s.holder.Append(s.content.widget())
		}
	}
	for t, b := range s.buttons {
		if t == s.shown {
			b.AddCSSClass("active")
		} else {
			b.RemoveCSSClass("active")
		}
	}
}

// drop closes the tab's view: closed means gone, nothing in it keeps running.
func (s *sidebar) drop() {
	if s.content == nil {
		return
	}
	s.content.close()
	s.holder.Remove(s.content.widget())
	s.content = nil
}

// layoutSidebar shows, hides or resizes the side pane to match the state.
func (w *Window) layoutSidebar() {
	st := &w.sidebarState
	if st.Open {
		if w.sidebar == nil {
			w.sidebar = newSidebar(w)
			w.split.SetStartChild(w.sidebar.root)
		}
		w.sidebar.update()
		w.applyingSplit = true
		w.split.SetPosition(int(st.Width))
		w.applyingSplit = false
	} else if w.sidebar != nil {
		w.sidebar.drop()
		w.split.SetStartChild(nil)
		w.sidebar = nil
	}
	w.scheduleSave()
}

// showSidebar is a tab's command: opens the pane on it, or closes the pane
// when it's already showing it (as VS Code does).
func (w *Window) showSidebar(tab uicore.SidebarTab) {
	st := &w.sidebarState
	if st.Open && st.Tab == tab {
		st.Open = false
	} else {
		st.Tab, st.Open = tab, true
	}
	w.layoutSidebar()
}

func (w *Window) toggleSidebar() {
	w.sidebarState.Open = !w.sidebarState.Open
	w.layoutSidebar()
}

// sidePane is the shown side-pane tab, if any.
func (w *Window) sidePane() sidePaneTab {
	if w.sidebar == nil {
		return nil
	}
	return w.sidebar.content
}

// newSidebarSplit is the side pane and the terminals, side by side; the
// divider sets the pane's width.
func (w *Window) newSidebarSplit(content gtk.Widgetter) *gtk.Paned {
	p := gtk.NewPaned(gtk.OrientationHorizontal)
	p.SetEndChild(content)
	p.SetResizeStartChild(false)
	p.SetResizeEndChild(true)
	p.SetShrinkStartChild(false)
	p.SetShrinkEndChild(false)
	p.NotifyProperty("position", func() {
		if w.applyingSplit || w.sidebar == nil || !p.Mapped() {
			return
		}
		width := uicore.ClampSidebarWidth(float64(p.Position()))
		if width != w.sidebarState.Width {
			w.sidebarState.Width = width
			w.scheduleSave()
		}
	})
	return p
}

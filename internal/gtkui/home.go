//go:build linux

package gtkui

import (
	"strings"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"

	"github.com/dayanraj/wtf/internal/uicore"
)

// homePage is what a window shows while it has no tabs: ways to start, the
// tabs closed here, and where the rest is. Nothing in it polls.
type homePage struct {
	w    *Window
	root *gtk.ScrolledWindow
	body *gtk.Box
	// first is the first row, which takes the keyboard.
	first *gtk.Button
	// shape is what the page was last built from.
	shape string
}

// homeClosedMax is how many closed tabs the home page lists.
const homeClosedMax = 8

func newHomePage(w *Window) *homePage {
	h := &homePage{w: w}
	h.body = gtk.NewBox(gtk.OrientationVertical, 2)
	h.body.SetHAlign(gtk.AlignCenter)
	h.body.SetVAlign(gtk.AlignCenter)
	h.body.SetSizeRequest(420, -1)
	h.body.SetMarginTop(20)
	h.body.SetMarginBottom(20)
	h.body.SetMarginStart(24)
	h.body.SetMarginEnd(24)
	h.root = gtk.NewScrolledWindow()
	h.root.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	h.root.AddCSSClass("wtf-home")
	h.root.SetChild(h.body)
	return h
}

// refresh rebuilds the page when the closed tabs changed.
func (h *homePage) refresh() {
	w := h.w
	closed := w.model.RecentlyClosed()
	if len(closed) > homeClosedMax {
		closed = closed[:homeClosedMax]
	}
	var sb strings.Builder
	sb.WriteString("home")
	for _, c := range closed {
		sb.WriteString("\x00" + w.app.settings.text(c.Cwd))
		if c.API {
			sb.WriteString("\x01")
		}
	}
	if shape := sb.String(); shape == h.shape {
		return
	} else {
		h.shape = shape
	}
	for c := h.body.FirstChild(); c != nil; c = h.body.FirstChild() {
		h.body.Remove(c)
	}
	h.first = nil

	title := sideLabel("wtf", "wtf-home-title")
	h.body.Append(title)
	h.body.Append(sideLabel("No tabs are open in this window.", "dim-label"))

	cmds := w.windowCommands()
	command := func(icon, label, id string) {
		c, _ := uicore.CommandByID(id)
		run := cmds[id]
		if id == "window.new" {
			run = func() { w.app.openWindowWith(windowStart{}) }
		}
		if run == nil {
			return
		}
		h.row(icon, label, uicore.KeyLabel(c.Keys), "", run)
	}
	side := func(icon, label string, tab uicore.SidebarTab, id string) {
		c, _ := uicore.CommandByID(id)
		// The command itself would close the pane when it's already there.
		h.row(icon, label, uicore.KeyLabel(c.Keys), "", func() {
			if st := w.sidebarState; !(st.Open && st.Tab == tab) {
				w.showSidebar(tab)
			}
		})
	}

	h.heading("Start")
	command("tab-new-symbolic", "New Tab", "tab.new")
	command("network-transmit-receive-symbolic", "New API Tab", "tab.newApi")
	command("system-run-symbolic", "Start Claude Code…", "agents.start")
	command("window-new-symbolic", "New Window", "window.new")

	if len(closed) > 0 {
		h.heading("Recently closed")
		reopen, _ := uicore.CommandByID("tab.reopen")
		for i, c := range closed {
			icon, label, tip, keys := "folder-symbolic", "Home folder", "", ""
			switch {
			case c.API:
				icon, label = "network-transmit-receive-symbolic", "API tab"
			case c.Cwd != "":
				label = w.app.settings.text(uicore.PrettyPath(c.Cwd, w.app.home))
				tip = label
			}
			if i == 0 {
				keys = uicore.KeyLabel(reopen.Keys)
			}
			h.row(icon, label, keys, tip, func() { w.model.ReopenAt(i) })
		}
	}

	h.heading("More")
	command("edit-find-symbolic", "Command Palette", "palette")
	side("view-grid-symbolic", "Workspaces", uicore.SidebarWorkspaces, "sidebar.workspaces")
	side("avatar-default-symbolic", "Agents", uicore.SidebarAgents, "sidebar.agents")
	command("help-browser-symbolic", "Manual", "help.manual")
	command("emblem-system-symbolic", "Settings…", "config.open")
}

func (h *homePage) heading(text string) {
	h.body.Append(sideLabel(text, "wtf-home-heading"))
}

// row is one thing to click: an icon, what it does, its shortcut.
func (h *homePage) row(icon, label, keys, tip string, run func()) {
	inner := gtk.NewBox(gtk.OrientationHorizontal, 10)
	inner.Append(gtk.NewImageFromIconName(icon))
	l := sideLabel(label, "")
	l.SetEllipsize(pango.EllipsizeMiddle)
	l.SetHExpand(true)
	inner.Append(l)
	if keys != "" {
		inner.Append(sideLabel(keys, "dim-label"))
	}
	b := gtk.NewButton()
	b.AddCSSClass("flat")
	b.AddCSSClass("wtf-home-row")
	b.SetChild(inner)
	if tip != "" {
		b.SetTooltipText(tip)
	}
	b.ConnectClicked(run)
	h.body.Append(b)
	if h.first == nil {
		h.first = b
	}
}

// focus gives the keyboard to the page, unless a text field in the window
// (the palette, a side pane's filter) has it.
func (h *homePage) focus() {
	if f := h.w.win.Focus(); f != nil {
		if _, typing := f.(*gtk.Text); typing {
			return
		}
	}
	if h.first != nil {
		h.first.GrabFocus()
	}
}

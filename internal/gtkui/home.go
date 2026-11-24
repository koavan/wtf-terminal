//go:build linux

package gtkui

import (
	"fmt"
	"strings"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"

	"github.com/dayanraj/wtf/internal/uicore"
)

// homePage is what a window shows at launch, from the strip's Home button,
// and while it has no tabs: ways to start, the open tabs, the tabs closed
// here, and where the rest is. Nothing in it polls.
type homePage struct {
	w    *Window
	root *gtk.ScrolledWindow
	body *gtk.Box
	// first is the first row, which takes the keyboard.
	first *gtk.Button
	// shape is what the page was last built from.
	shape string
	// konami follows the keys toward the green screen.
	konami uicore.Konami
}

// homeClosedMax is how many closed tabs the home page lists; homeOpenMax,
// how many open ones.
const (
	homeClosedMax = 8
	homeOpenMax   = 12
)

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
	// Watched before the rows see them, so the arrows still move between rows.
	keys := gtk.NewEventControllerKey()
	keys.SetPropagationPhase(gtk.PhaseCapture)
	keys.ConnectKeyPressed(func(keyval, _ uint, _ gdk.ModifierType) bool {
		if h.konami.Key(konamiKey(keyval)) {
			if w.app.settings.togglePhosphor() {
				w.toast("▒▒ PHOSPHOR MODE ▒▒  (the code again turns it off)")
			} else {
				w.toast("Back to the present.")
			}
		}
		return false
	})
	h.root.AddController(keys)
	return h
}

// konamiKey names a key as uicore.Konami takes it.
func konamiKey(keyval uint) string {
	switch keyval {
	case gdk.KEY_Up, gdk.KEY_KP_Up:
		return "up"
	case gdk.KEY_Down, gdk.KEY_KP_Down:
		return "down"
	case gdk.KEY_Left, gdk.KEY_KP_Left:
		return "left"
	case gdk.KEY_Right, gdk.KEY_KP_Right:
		return "right"
	case gdk.KEY_a, gdk.KEY_A:
		return "a"
	case gdk.KEY_b, gdk.KEY_B:
		return "b"
	}
	return ""
}

// refresh rebuilds the page when the open or the closed tabs changed.
func (h *homePage) refresh() {
	w := h.w
	closed := w.model.RecentlyClosed()
	if len(closed) > homeClosedMax {
		closed = closed[:homeClosedMax]
	}
	var sb strings.Builder
	sb.WriteString("home")
	open := w.model.Tabs()
	type openTab struct {
		id           int
		title, group string
		api          bool
	}
	tabs := make([]openTab, 0, len(open))
	for _, t := range open {
		o := openTab{id: t.ID, title: w.app.settings.text(w.model.Title(t))}
		if g, ok := w.model.Group(t.GroupID); ok && t.GroupID != 0 {
			o.group = g.Name
		}
		if p := w.model.FocusedPane(t); p != nil {
			o.api = p.Kind == uicore.PaneAPI
		}
		tabs = append(tabs, o)
		fmt.Fprintf(&sb, "\x00%d\x01%s\x01%s\x01%v", o.id, o.title, o.group, o.api)
	}
	sb.WriteString("\x00closed")
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
	switch n := len(tabs); n {
	case 0:
		h.body.Append(sideLabel("No tabs are open in this window.", "dim-label"))
	case 1:
		h.body.Append(sideLabel("1 tab is open in this window.", "dim-label"))
	default:
		h.body.Append(sideLabel(fmt.Sprintf("%d tabs are open in this window.", n), "dim-label"))
	}

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

	if len(tabs) > 0 {
		h.heading("Open tabs")
		for i, o := range tabs {
			if i == homeOpenMax {
				h.body.Append(sideLabel(fmt.Sprintf("and %d more in the tab strip", len(tabs)-homeOpenMax), "dim-label"))
				break
			}
			icon, label := "utilities-terminal-symbolic", o.title
			if o.api {
				icon = "network-transmit-receive-symbolic"
			}
			if o.group != "" {
				label += " — " + o.group
			}
			h.row(icon, label, "", o.title, func() { w.model.Activate(o.id) })
		}
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

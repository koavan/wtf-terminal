//go:build linux

package gtkui

import (
	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/dayanraj/wtf/internal/uicore"
)

// Window is one top-level window. For now it holds a single pane; tabs and
// splits come with the window shell (M3).
type Window struct {
	app  *App
	name string // the backend's name for it, as in the saved session
	win  *adw.ApplicationWindow
	pane *Pane
}

func newWindow(a *App, name string) *Window {
	w := &Window{app: a, name: name}
	w.win = adw.NewApplicationWindow(&a.gtk.Application)
	w.win.SetTitle("wtf")
	w.win.SetDefaultSize(900, 600)

	w.pane = newPane(w)
	header := adw.NewHeaderBar()
	view := adw.NewToolbarView()
	view.AddTopBar(header)
	view.SetContent(w.pane.term)
	w.win.SetContent(view)

	w.installCommands(map[string]func(){
		"term.copy":      w.pane.term.Copy,
		"term.paste":     w.pane.term.Paste,
		"term.selectAll": w.pane.term.SelectAll,
		"pane.close":     w.win.Close,
		"window.close":   w.win.Close,
		"font.bigger":    func() { w.pane.zoom(+1) },
		"font.smaller":   func() { w.pane.zoom(-1) },
		"font.reset":     func() { w.pane.zoom(0) },
	}, "window.new", "app.quit")

	// Polling pauses while no wtf window is in front.
	w.win.NotifyProperty("is-active", func() { a.be.Active.SetFocused(w.name, w.win.IsActive()) })
	w.win.ConnectCloseRequest(func() bool {
		a.windowClosed(w)
		return false
	})
	// The marks self-check feeds synthetic output instead of a shell.
	if a.debug != "marks" {
		if err := w.pane.start(); err != nil {
			w.pane.term.Feed([]byte("\r\nCouldn't start a shell: " + err.Error() + "\r\n"))
		}
	}
	w.pane.term.GrabFocus()
	return w
}

// installCommands adds the window's commands as "win." actions, and a
// shortcut for each of them and of the named app commands. The shortcuts
// run in the capture phase, before VTE turns the keys into input.
func (w *Window) installCommands(run map[string]func(), appCommands ...string) {
	keys := gtk.NewShortcutController()
	keys.SetPropagationPhase(gtk.PhaseCapture)
	keys.SetScope(gtk.ShortcutScopeLocal)
	shortcut := func(id, action string) {
		c, ok := uicore.CommandByID(id)
		if !ok || c.Keys == "" {
			return
		}
		keys.AddShortcut(gtk.NewShortcut(gtk.NewShortcutTriggerParseString(uicore.Accel(c.Keys)), gtk.NewNamedAction(action)))
	}
	for id, f := range run {
		act := gio.NewSimpleAction(id, nil)
		act.ConnectActivate(func(*glib.Variant) { f() })
		w.win.AddAction(act)
		shortcut(id, "win."+id)
	}
	for _, id := range appCommands {
		shortcut(id, "app."+id)
	}
	w.win.AddController(keys)
}

//go:build linux

package gtkui

import "github.com/dayanraj/wtf/internal/uicore"

// movingTab is a tab on its way to another window: the model's payload and
// its panes' terminals, by backend session, so scrollback and running
// programs carry over.
type movingTab struct {
	payload *uicore.TabPayload
	panes   map[string]*Pane
}

// detach takes a tab out of this window, its shells still running.
func (w *Window) detach(id int) *movingTab {
	t, ok := w.model.Tab(id)
	if !ok {
		return nil
	}
	moving := &movingTab{panes: map[string]*Pane{}}
	for _, leaf := range t.Root.Leaves() {
		if p := w.panes[leaf]; p != nil && p.session != "" {
			moving.panes[p.session] = p
			// It's the other window's now: this one doesn't hang it up.
			delete(w.panes, leaf)
			detachWidget(&p.root.Widget)
		}
	}
	payload, ok := w.model.Detach(id, func(*uicore.PaneState) string { return "" })
	if !ok {
		return nil
	}
	moving.payload = payload
	return moving
}

// adopt takes in a tab; its panes reuse the terminals that came with it.
func (w *Window) adopt(tab *movingTab) int {
	before := map[int]bool{}
	for id := range w.model.Panes {
		before[id] = true
	}
	if !w.model.Adopt(tab.payload) {
		return 0
	}
	for id, ps := range w.model.Panes {
		if before[id] {
			continue
		}
		if p := tab.panes[ps.SessionID]; p != nil {
			p.win, p.id = w, id
			w.panes[id] = p
			_ = w.app.be.Terminal.Adopt(ps.SessionID, w.name)
		}
	}
	return w.model.ActiveID()
}

// moveTab moves a tab to another window, before one of its tabs (0: at
// the end).
func (w *Window) moveTab(id int, to *Window, before int) {
	tab := w.detach(id)
	if tab == nil {
		return
	}
	if moved := to.adopt(tab); moved != 0 && before != 0 {
		to.model.MoveBefore(moved, before)
	}
	to.win.Present()
}

// moveToNewWindow gives a tab a window of its own (never the last one).
func (w *Window) moveToNewWindow(id int) {
	if len(w.model.Tabs()) < 2 {
		return
	}
	if tab := w.detach(id); tab != nil {
		w.app.openWindowWith(windowStart{tab: tab})
	}
}

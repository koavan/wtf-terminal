//go:build linux

package gtkui

import (
	"slices"

	"github.com/dayanraj/wtf/internal/bridge"
	"github.com/dayanraj/wtf/internal/uicore"
)

// Ask Claude (#32): a split beside a shell running the user's own Claude
// Code with wtf's plugin, whose tools work on that shell (its target). The
// plugin's hooks report what Claude Code is doing (bridge:status): the tab
// shows it, and wtf notifies when it needs you or finishes while you're
// looking elsewhere. Kept by backend session, so it survives a tab moving
// to another window. As macos/Sources/Wtf/ClaudePane.swift.
type claudePanes struct {
	// Claude pane session → the shell session it works on.
	targets map[string]string
	// Claude pane session → bridge.StateIdle, StateBusy or StateWaiting
	// (none before its first hook, and after it exits).
	states map[string]string
	// Claude panes split but not started yet → their target.
	pending map[*uicore.PaneState]string
}

func newClaudePanes() *claudePanes {
	return &claudePanes{targets: map[string]string{}, states: map[string]string{}, pending: map[*uicore.PaneState]string{}}
}

func (c *claudePanes) isClaude(ps *uicore.PaneState) bool {
	return ps != nil && ps.SessionID != "" && c.targets[ps.SessionID] != ""
}

// state is the tab's badge: what a Claude pane in it is doing ("" for none).
func (w *Window) claudeState(t uicore.Tab) string {
	c, found := w.app.claude, ""
	for _, id := range t.Root.Leaves() {
		ps := w.model.Panes[id]
		if !c.isClaude(ps) {
			continue
		}
		st := c.states[ps.SessionID]
		if st == "" {
			st = bridge.StateIdle
		}
		if st == bridge.StateWaiting {
			return st
		}
		if found == "" || st == bridge.StateBusy {
			found = st
		}
	}
	return found
}

// askClaude opens the Claude pane for the focused shell, or goes to the one
// it has.
func (w *Window) askClaude() {
	c := w.app.claude
	src := w.model.ActivePane()
	if src == nil || src.Kind != uicore.PaneTerminal || src.SessionID == "" {
		w.toast("Ask Claude works on a shell: focus one first")
		return
	}
	if c.isClaude(src) {
		w.toast("This is a Claude pane: its tools work on the shell it was opened from")
		return
	}
	command, err := w.app.be.Bridge.ClaudeCommand()
	if err != nil {
		w.toast("Can't start Claude Code: " + err.Error())
		return
	}
	for cs, target := range c.targets {
		if target != src.SessionID {
			continue
		}
		cw := w.app.windowOf(cs)
		if cw == nil {
			break
		}
		cp := cw.model.PaneBySession(cs)
		// Back to its Claude pane; start claude again if it was quit.
		if t, ok := cw.model.TabOf(cp.ID); ok {
			cw.model.Activate(t.ID)
		}
		cw.model.FocusPane(cp.ID)
		cw.win.Present()
		if p := cw.panes[cp.ID]; p != nil && !cp.Busy() {
			p.write(uicore.ClearLine + command + "\r")
		}
		return
	}
	id, ok := w.model.Split(uicore.SplitRow, &src.Cwd, command)
	if !ok {
		return
	}
	if ps := w.model.Panes[id]; ps != nil {
		if ps.SessionID != "" {
			c.started(w, ps, src.SessionID)
		} else {
			c.pending[ps] = src.SessionID
		}
	}
}

// claudeStarted: a pane's shell started; a Claude pane starts working on
// its target.
func (w *Window) claudeStarted(ps *uicore.PaneState) {
	c := w.app.claude
	if target, ok := c.pending[ps]; ok && ps.SessionID != "" {
		delete(c.pending, ps)
		c.started(w, ps, target)
	}
}

func (c *claudePanes) started(w *Window, ps *uicore.PaneState, target string) {
	c.targets[ps.SessionID] = target
	w.app.be.Bridge.SetTarget(ps.SessionID, target)
	w.strip.refresh()
}

// claudeClosed: a shell closed (not moved to another window).
func (a *App) claudeClosed(session string) {
	c := a.claude
	if c.targets[session] == "" {
		return
	}
	delete(c.targets, session)
	delete(c.states, session)
	a.be.Bridge.SetTarget(session, "")
}

func (a *App) claudeStatus(st bridge.Status) {
	c := a.claude
	if c.targets[st.Session] == "" {
		return
	}
	w := a.windowOf(st.Session)
	if w == nil {
		return
	}
	old := c.states[st.Session]
	if st.State == bridge.StateEnded {
		delete(c.states, st.Session)
	} else {
		c.states[st.Session] = st.State
	}
	w.strip.refresh()
	ps := w.model.PaneBySession(st.Session)
	t, ok := w.model.TabOf(ps.ID)
	if !ok || w.win.IsActive() && t.ID == w.model.ActiveID() && slices.Contains(t.Root.Leaves(), ps.ID) {
		return
	}
	title := w.app.settings.text(w.model.Title(t))
	var head, body string
	switch {
	case st.State == bridge.StateWaiting:
		head, body = "Claude Code needs you", w.app.settings.text(st.Message)
	case st.State == bridge.StateIdle && old == bridge.StateBusy:
		head = "Claude Code finished"
	default:
		return
	}
	notify := a.be.Notify
	go func() { _ = notify.Notify(head, title, body) }()
}

//go:build linux

package gtkui

import (
	"fmt"
	"time"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"

	"github.com/dayanraj/wtf/internal/bridge"
	"github.com/dayanraj/wtf/internal/uicore"
)

// Ask Claude (#32): a split beside a shell running the user's own Claude
// Code with wtf's plugin, whose tools work on that shell (its target). The
// plugin's hooks report what Claude Code is doing (bridge:status), which
// says when it's ready for a prompt; the badges and notifications are the
// agent list's (agents.go, for every Claude Code). Kept by backend session,
// so it survives a tab moving to another window. As
// macos/Sources/Wtf/ClaudePane.swift.
type claudePanes struct {
	// Claude pane session → the shell session it works on.
	targets map[string]string
	// Claude pane session → bridge.StateIdle, StateBusy or StateWaiting
	// (none before its first hook, and after it exits).
	states map[string]string
	// Claude panes split but not started yet → their target.
	pending map[*uicore.PaneState]string
	// What to type in a Claude pane once Claude Code is ready for a
	// prompt, by session; for a pane not started yet, by pane.
	queued          map[string]claudeMessage
	pendingMessages map[*uicore.PaneState]claudeMessage
}

// claudeMessage is a `/wtf:` command for a Claude pane, with Enter or for
// you to finish.
type claudeMessage struct {
	text  string
	enter bool
}

func newClaudePanes() *claudePanes {
	return &claudePanes{targets: map[string]string{}, states: map[string]string{}, pending: map[*uicore.PaneState]string{},
		queued: map[string]claudeMessage{}, pendingMessages: map[*uicore.PaneState]claudeMessage{}}
}

func (c *claudePanes) isClaude(ps *uicore.PaneState) bool {
	return ps != nil && ps.SessionID != "" && c.targets[ps.SessionID] != ""
}

// claudeState is the tab's badge (the agent list's): "" for none.
func (w *Window) claudeState(t uicore.Tab) string { return w.agentTabState(t.Root.Leaves()) }

// askClaude opens the Claude pane for the focused shell, or goes to the one
// it has.
func (w *Window) askClaude() { w.askClaudeTo(claudeMessage{}) }

// askClaudeTo is askClaude, then types m there once Claude Code is ready.
// Nothing is typed while it starts (its folder-trust question comes first)
// or works: it waits for its next prompt.
func (w *Window) askClaudeTo(m claudeMessage) {
	c := w.app.claude
	src := w.model.ActivePane()
	if src == nil || src.Kind != uicore.PaneTerminal || src.SessionID == "" {
		w.toast("Ask Claude works on a shell: focus one first")
		return
	}
	if c.isClaude(src) {
		if m.text != "" {
			w.app.claudeDeliver(m, src.SessionID, w)
		} else {
			w.toast("This is a Claude pane: its tools work on the shell it was opened from")
		}
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
			delete(c.states, cs)
			p.write(uicore.ClearLine + command + "\r")
		}
		if m.text != "" {
			w.app.claudeDeliver(m, cs, cw)
		}
		return
	}
	id, ok := w.model.Split(uicore.SplitRow, &src.Cwd, command)
	if !ok {
		return
	}
	if ps := w.model.Panes[id]; ps != nil {
		if m.text != "" {
			c.pendingMessages[ps] = m
		}
		if ps.SessionID != "" {
			c.started(w, ps, src.SessionID)
		} else {
			c.pending[ps] = src.SessionID
		}
	}
}

// claudeDeliver types m in Claude pane session now if Claude Code waits for
// a prompt, else when it next does (a newer message replaces it).
func (a *App) claudeDeliver(m claudeMessage, session string, w *Window) {
	c := a.claude
	switch c.states[session] {
	case bridge.StateIdle:
		a.claudeType(m, session)
		return
	case bridge.StateBusy, bridge.StateWaiting:
		w.toast("Claude Code is busy: wtf types this when it's done")
	}
	c.queued[session] = m
}

func (a *App) claudeType(m claudeMessage, session string) {
	w := a.windowOf(session)
	if w == nil {
		return
	}
	ps := w.model.PaneBySession(session)
	p := w.panes[ps.ID]
	if p == nil {
		return
	}
	p.write(m.text)
	if m.enter {
		// Apart from the text, so it isn't taken as part of a paste.
		coreglib.TimeoutAdd(uint(200*time.Millisecond/time.Millisecond), func() bool {
			p.write("\r")
			return false
		})
	}
	if t, ok := w.model.TabOf(ps.ID); ok && t.ID == w.model.ActiveID() {
		w.model.FocusPane(ps.ID)
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
	if m, ok := c.pendingMessages[ps]; ok {
		delete(c.pendingMessages, ps)
		c.queued[ps.SessionID] = m
	}
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
	delete(c.queued, session)
	a.be.Bridge.SetTarget(session, "")
}

// claudeStatus types what was queued for a Claude pane once Claude Code
// is ready for a prompt.
func (a *App) claudeStatus(st bridge.Status) {
	c := a.claude
	if c.targets[st.Session] == "" {
		return
	}
	if st.State == bridge.StateEnded {
		delete(c.states, st.Session)
	} else {
		c.states[st.Session] = st.State
	}
	if m, ok := c.queued[st.Session]; ok && st.State == bridge.StateIdle {
		delete(c.queued, st.Session)
		a.claudeType(m, st.Session)
	}
}

// #25–#27 through the Claude pane's slash commands (claude-plugin/commands).

// claudeCommand: a command from plain English, `/wtf:cmd ` for you to finish.
func (w *Window) claudeCommand() { w.askClaudeTo(claudeMessage{text: "/wtf:cmd "}) }

// claudeInvestigate: `/wtf:investigate ` for you to say what's wrong.
func (w *Window) claudeInvestigate() { w.askClaudeTo(claudeMessage{text: "/wtf:investigate "}) }

// claudeLogs: a summary of a command's logs (#28), block id or the latest
// one with enough output (0).
func (w *Window) claudeLogs(block int) {
	text := "/wtf:logs"
	if block > 0 {
		text += fmt.Sprintf(" block %d", block)
	}
	w.askClaudeTo(claudeMessage{text: text, enter: true})
}

// claudeRunbook: a runbook from block id on (#29), or `/wtf:runbook ` for
// you to say which commands (0).
func (w *Window) claudeRunbook(block int) {
	if block > 0 {
		w.askClaudeTo(claudeMessage{text: fmt.Sprintf("/wtf:runbook from block %d", block), enter: true})
		return
	}
	w.askClaudeTo(claudeMessage{text: "/wtf:runbook "})
}

// claudePostmortem: `/wtf:postmortem ` for you to say which commands and
// what happened.
func (w *Window) claudePostmortem() { w.askClaudeTo(claudeMessage{text: "/wtf:postmortem "}) }

// claudeTriage: triage of a finding (#30), its rule and where it is
// (`src/db.go:40`).
func (w *Window) claudeTriage(rule, where string) {
	text := "/wtf:triage"
	if rule != "" {
		text += " " + rule
	}
	if where != "" {
		text += " at " + where
	}
	w.askClaudeTo(claudeMessage{text: text, enter: true})
}

// claudeHistory: ask your history (#31), `/wtf:history ` for you to say
// what you're after.
func (w *Window) claudeHistory() { w.askClaudeTo(claudeMessage{text: "/wtf:history "}) }

// claudeReview: a summary of a change preview, block id or a saved plan file.
func (w *Window) claudeReview(block int, file string) {
	text := "/wtf:review"
	switch {
	case file != "":
		text += " plan file " + uicore.ShellQuote(file)
	case block > 0:
		text += fmt.Sprintf(" block %d", block)
	}
	w.askClaudeTo(claudeMessage{text: text, enter: true})
}

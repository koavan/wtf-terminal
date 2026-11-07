//go:build linux

package gtkui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/dayanraj/wtf/internal/bridge"
	"github.com/dayanraj/wtf/internal/uicore"
)

// Agents (#33): every Claude Code running in wtf's shells, from its hooks
// (bridge:status): Ask Claude's panes through wtf's plugin, a claude you
// start yourself through agent-status's settings. The tab badges, the
// Agents side tab and the picker read it here; wtf notifies when one needs
// you or finishes while you're looking elsewhere. As
// macos/Sources/Wtf/Agents.swift.

// What a tab's or an agent's badge shows: bridge's states, and done (it
// finished while you weren't looking).
const agentDone = "done"

type agentList struct {
	// Session → its Claude Code.
	all map[string]bridge.Status
	// Called after every change (the Agents tab while it shows).
	observers map[int]func()
	nextObs   int
}

func newAgentList() *agentList {
	return &agentList{all: map[string]bridge.Status{}, observers: map[int]func(){}}
}

// observe calls f after every change until the returned stop is called.
func (l *agentList) observe(f func()) (stop func()) {
	id := l.nextObs
	l.nextObs++
	l.observers[id] = f
	return func() { delete(l.observers, id) }
}

func agentState(st bridge.Status) string {
	if st.State == bridge.StateIdle && st.Unseen {
		return agentDone
	}
	return st.State
}

// agentRank: most urgent first.
func agentRank(state string) int {
	switch state {
	case bridge.StateWaiting:
		return 0
	case agentDone:
		return 1
	case bridge.StateBusy:
		return 2
	}
	return 3
}

func agentLabel(state string) string {
	switch state {
	case bridge.StateWaiting:
		return "Needs you"
	case agentDone:
		return "Done"
	case bridge.StateBusy:
		return "Working"
	}
	return "Idle"
}

// sorted: most urgent first, then the longest in that state.
func (l *agentList) sorted() []bridge.Status {
	out := make([]bridge.Status, 0, len(l.all))
	for _, st := range l.all {
		out = append(out, st)
	}
	slices.SortFunc(out, func(a, b bridge.Status) int {
		if ra, rb := agentRank(agentState(a)), agentRank(agentState(b)); ra != rb {
			return ra - rb
		}
		if a.Since != b.Since {
			if a.Since < b.Since {
				return -1
			}
			return 1
		}
		if a.Session < b.Session {
			return -1
		}
		return 1
	})
	return out
}

// agentTabState is a tab's badge: its most urgent agent ("" for none).
func (w *Window) agentTabState(leaves []int) string {
	found := ""
	for _, id := range leaves {
		ps := w.model.Panes[id]
		if ps == nil || ps.SessionID == "" {
			continue
		}
		st, ok := w.app.agents.all[ps.SessionID]
		if !ok {
			continue
		}
		if s := agentState(st); found == "" || agentRank(s) < agentRank(found) {
			found = s
		}
	}
	return found
}

// agentVisible: you're looking at the pane: its tab is in front in the
// active window.
func (w *Window) agentVisible(session string) bool {
	if !w.win.IsActive() {
		return false
	}
	t, ok := w.model.Active()
	if !ok {
		return false
	}
	ps := w.model.PaneBySession(session)
	return ps != nil && slices.Contains(t.Root.Leaves(), ps.ID)
}

// agentsSeen: the finished agents in front of you in w are no longer done.
func (w *Window) agentsSeen() {
	if !w.win.IsActive() {
		return
	}
	t, ok := w.model.Active()
	if !ok {
		return
	}
	for _, id := range t.Root.Leaves() {
		ps := w.model.Panes[id]
		if ps == nil {
			continue
		}
		if st, ok := w.app.agents.all[ps.SessionID]; ok && st.Unseen {
			go w.app.be.Bridge.Seen(st.Session)
		}
	}
}

func (a *App) agentStatus(st bridge.Status) {
	l := a.agents
	old, had := l.all[st.Session]
	if st.State == bridge.StateEnded {
		delete(l.all, st.Session)
	} else {
		l.all[st.Session] = st
	}
	w := a.windowOf(st.Session)
	if w != nil {
		w.strip.refresh()
	}
	for _, f := range l.observers {
		f()
	}
	// A background agent that quit leaves only its shell: close it.
	if w != nil && w.background && st.State == bridge.StateEnded {
		if ps := w.model.PaneBySession(st.Session); ps != nil {
			w.model.ClosePane(ps.ID)
		}
	}
	if w == nil || st.State == bridge.StateEnded {
		return
	}
	if w.agentVisible(st.Session) {
		if st.Unseen {
			go a.be.Bridge.Seen(st.Session)
		}
		return
	}
	now := agentState(st)
	if had && agentState(old) == now {
		return
	}
	ps := w.model.PaneBySession(st.Session)
	if ps == nil {
		return
	}
	t, ok := w.model.TabOf(ps.ID)
	if !ok {
		return
	}
	title := a.settings.text(w.model.Title(t))
	var head, body string
	switch now {
	case bridge.StateWaiting:
		head, body = "Claude Code needs you", a.settings.text(st.Message)
	case agentDone:
		head, body = "Claude Code finished", a.settings.text(st.Prompt)
	default:
		return
	}
	notify := a.be.Notify
	go func() { _ = notify.Notify(head, title, body) }()
}

// goToAgent shows an agent's window, tab and pane; a background one comes
// back into a tab first.
func (a *App) goToAgent(session string) {
	w := a.windowOf(session)
	if w == nil {
		return
	}
	if w.background {
		a.openAgent(session)
		if w = a.windowOf(session); w == nil {
			return
		}
	}
	ps := w.model.PaneBySession(session)
	if ps == nil {
		return
	}
	if t, ok := w.model.TabOf(ps.ID); ok {
		w.model.Activate(t.ID)
	}
	w.model.FocusPane(ps.ID)
	w.win.Present()
	w.agentsSeen()
}

// nextAgent goes to the next agent that needs you (then the next done one),
// after the focused pane's, round and round.
func (w *Window) nextAgent() {
	var list []bridge.Status
	for _, st := range w.app.agents.sorted() {
		if s := agentState(st); s == bridge.StateWaiting || s == agentDone {
			list = append(list, st)
		}
	}
	if len(list) == 0 {
		w.toast("No Claude Code needs you")
		return
	}
	current := ""
	if t, ok := w.model.Active(); ok {
		if ps := w.model.Panes[t.FocusedPaneID]; ps != nil {
			current = ps.SessionID
		}
	}
	i := slices.IndexFunc(list, func(st bridge.Status) bool { return st.Session == current })
	w.app.goToAgent(list[(i+1)%len(list)].Session)
}

// agentPlace is an agent's tab title and folder, as shown.
func (a *App) agentPlace(session string) (title, cwd string) {
	w := a.windowOf(session)
	if w == nil {
		return session, ""
	}
	ps := w.model.PaneBySession(session)
	if ps == nil {
		return session, ""
	}
	title = session
	if t, ok := w.model.TabOf(ps.ID); ok {
		title = a.settings.text(w.model.Title(t))
	}
	return title, uicore.PrettyPath(ps.Cwd, a.home)
}

// agentAge: how long ago, short: "now", "4m", "2h", "3d".
func agentAge(since int64) string {
	if since <= 0 {
		return ""
	}
	d := time.Since(time.UnixMilli(since))
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// agentEntries are the picker's rows (Go to Agent…): every Claude Code,
// most urgent first.
func (w *Window) agentEntries() []paletteEntry {
	a := w.app
	var out []paletteEntry
	for _, st := range a.agents.sorted() {
		state := agentState(st)
		title, cwd := a.agentPlace(st.Session)
		what := st.Prompt
		if state == bridge.StateWaiting && st.Message != "" {
			what = st.Message
		}
		detail := agentLabel(state)
		for _, s := range []string{cwd, a.settings.text(what)} {
			if s != "" {
				detail += " · " + s
			}
		}
		session := st.Session
		out = append(out, paletteEntry{section: "Agents", label: title + " — " + strings.ToLower(agentLabel(state)), detail: detail,
			hint: agentAge(st.Since), run: func() { a.goToAgent(session) }})
	}
	if len(out) == 0 {
		out = append(out, paletteEntry{section: "Agents", label: "No Claude Code running", detail: "Start claude in any tab, or Ask Claude", run: func() {}})
	}
	return out
}

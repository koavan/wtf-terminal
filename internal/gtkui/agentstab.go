//go:build linux

package gtkui

import (
	"fmt"
	"slices"
	"strings"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/dayanraj/wtf/internal/bridge"
	"github.com/dayanraj/wtf/internal/uicore"
)

func init() {
	registerSidebarTab(sidebarTabInfo{tab: uicore.SidebarAgents, label: "Agents", icon: "view-list-bullet-symbolic", cmd: "sidebar.agents",
		make: func(w *Window) sidePaneTab { return newAgentsTab(w) }})
}

// agentsTab is the Agents tab (#33): every Claude Code in wtf's shells,
// across windows, in the order they started (rows don't move), by the name
// you gave them or their tab's title. A click goes to it; a double-click
// renames it. Event-driven (the
// agent list's changes); the only timer redraws "4m" every 30s, and only
// while the tab exists. As macos/Sources/Wtf/AgentsView.swift.
type agentsTab struct {
	w      *Window
	root   *gtk.Box
	header *paneHeader
	list   *sideList
	stop   func()
	timer  coreglib.SourceHandle
	closed bool
}

func newAgentsTab(w *Window) *agentsTab {
	t := &agentsTab{w: w}
	t.header = newPaneHeader("Agents")
	t.header.tool("list-add-symbolic", "Start Claude Code…", w.startAgent)
	t.header.tool("help-about-symbolic", "How the Agents tab works", func() { w.app.showManual("agents") })
	t.list = newSideList(false)
	t.list.onClick = t.activate
	t.list.onActivate = t.activate
	t.list.onDoubleClick = func(id string) {
		if session, toggle := agentRowSession(id); !toggle && id == session {
			w.renameAgent(session)
		}
	}
	t.list.onMenu = func(ids []string, x, y float64) {
		session, _ := agentRowSession(ids[0])
		open := "Go to Claude Code"
		if aw := w.app.windowOf(session); aw != nil && aw.background {
			open = "Open in Tab"
		}
		items := []*menuItem{{label: open, run: func() { w.app.goToAgent(session) }},
			{label: "Peek and Reply…", run: func() { w.app.peekAgent(session, &w.win.Window) }},
			{label: "Rename…", run: func() { w.renameAgent(session) }}}
		if len(w.app.agents.all[session].Subagents) > 0 {
			label := "Show Sub-agents"
			if w.app.agents.subagentsShown[session] {
				label = "Hide Sub-agents"
			}
			items = append(items, &menuItem{label: label, run: func() { w.app.agents.toggleSubagents(session) }})
		}
		if aw := w.app.windowOf(session); aw != nil {
			if ps := aw.model.PaneBySession(session); ps != nil && ps.Cwd != "" {
				cwd := ps.Cwd
				items = append(items, &menuItem{label: "Copy Folder Path", run: func() { w.win.Clipboard().SetText(cwd) }})
			}
		}
		items = append(items, menuSection, &menuItem{label: "End Claude Code…", run: func() { w.app.endAgent(session, &w.win.Window) }})
		t.list.menu.show(x, y, items, nil)
	}
	t.root = gtk.NewBox(gtk.OrientationVertical, 4)
	t.root.Append(t.header.root)
	t.root.Append(t.list.root)
	t.stop = w.app.agents.observe(t.rebuild)
	t.timer = coreglib.TimeoutSecondsAdd(30, func() bool {
		if t.closed {
			return false
		}
		t.rebuild()
		return true
	})
	t.rebuild()
	return t
}

// Row ids: an agent's session; its sub-agents' toggle "subs:<session>";
// a sub-agent "sub:<session>:<id>".
func agentRowSession(id string) (session string, toggle bool) {
	if s, ok := strings.CutPrefix(id, "subs:"); ok {
		return s, true
	}
	if s, ok := strings.CutPrefix(id, "sub:"); ok {
		s, _, _ = strings.Cut(s, ":")
		return s, false
	}
	return id, false
}

// activate: a click on an agent or one of its sub-agents goes to it; on
// the toggle, shows or hides its sub-agents.
func (t *agentsTab) activate(id string) {
	if session, toggle := agentRowSession(id); toggle {
		t.w.app.agents.toggleSubagents(session)
	} else {
		t.w.app.goToAgent(session)
	}
}

// subagentRows: "▸ 3 sub-agents · 1 working", and when it's open each
// sub-agent with what it's doing, or its report's first line once done.
func (t *agentsTab) subagentRows(st bridge.Status) []sideRow {
	n := len(st.Subagents)
	if n == 0 {
		return nil
	}
	a := t.w.app
	open := a.agents.subagentsShown[st.Session]
	running := 0
	for _, sa := range st.Subagents {
		if sa.Running {
			running++
		}
	}
	title := fmt.Sprintf("%d sub-agent", n)
	if n != 1 {
		title += "s"
	}
	if running > 0 {
		title += fmt.Sprintf(" · %d working", running)
	}
	toggle := sideRow{id: "subs:" + st.Session, icon: "pan-end-symbolic", title: title, titleClass: "dim-label", depth: 1,
		tip: "Show the sub-agents it started and what they're doing"}
	if open {
		toggle.icon, toggle.tip = "pan-down-symbolic", "Hide sub-agents"
	}
	rows := []sideRow{toggle}
	if !open {
		return rows
	}
	for _, sa := range st.Subagents {
		kind := sa.Type
		if kind == "" {
			kind = "Sub-agent"
		}
		r := sideRow{id: "sub:" + st.Session + ":" + sa.ID, dot: "agent-idle", title: kind, depth: 2,
			pills: []sidePill{{text: agentAge(sa.Ended), tip: "Done then"}}}
		if sa.Task != "" {
			r.title += " · " + a.settings.text(sa.Task)
		}
		switch {
		case sa.Running:
			r.dot, r.detail = "agent-busy", "Working"
			if sa.Doing != "" {
				r.detail = a.settings.text(sa.Doing)
			}
			r.pills = []sidePill{{text: agentAge(sa.Since), tip: "Started then"}}
		case sa.Result != "":
			r.detail = "Done · " + a.settings.text(sa.Result)
		default:
			r.detail = "Done"
		}
		r.tip = strings.Join(slices.DeleteFunc([]string{kind, a.settings.text(sa.Task), r.detail}, func(s string) bool { return s == "" }), "\n")
		rows = append(rows, r)
	}
	return rows
}

func (t *agentsTab) widget() gtk.Widgetter { return t.root }

func (t *agentsTab) close() {
	t.closed = true
	t.stop()
	coreglib.SourceRemove(t.timer)
}

func (t *agentsTab) paneChanged()     { t.rebuild() }
func (t *agentsTab) commandFinished() {}

func (t *agentsTab) rebuild() {
	if t.closed {
		return
	}
	a := t.w.app
	focused := ""
	if ps := t.w.model.ActivePane(); ps != nil {
		focused = ps.SessionID
	}
	var rows, parked []sideRow
	// Fixed order (when each started), so a row never moves.
	for _, st := range a.agents.listed() {
		state := agentState(st)
		_, cwd := a.agentPlace(st.Session)
		title, tab := a.agentTitle(st)
		detail := agentLabel(state)
		for _, s := range []string{tab, cwd} {
			if s != "" {
				detail += " · " + s
			}
		}
		r := sideRow{id: st.Session, dot: "agent-" + state, title: title, detail: detail,
			pills: []sidePill{{text: agentAge(st.Since), tip: "In this state since then"}}, highlighted: st.Session == focused,
			tip: "Go to this Claude Code; double-click to name it"}
		if state == bridge.StateWaiting && st.Message != "" {
			r.alert, r.alertClass = a.settings.text(st.Message), "agent-alert"
		} else if st.Prompt != "" {
			r.alert, r.alertClass = a.settings.text(st.Prompt), "dim-label"
		}
		session := st.Session
		r.button, r.buttonTip, r.buttonRun = "view-reveal-symbolic", "Peek and reply", func() { a.peekAgent(session, &t.w.win.Window) }
		subs := t.subagentRows(st)
		if aw := a.windowOf(st.Session); aw != nil && aw.background {
			r.tip = "Open it in a tab; double-click to name it"
			parked = append(append(parked, r), subs...)
			continue
		}
		rows = append(append(rows, r), subs...)
	}
	sec := sideSection{rows: rows, always: true}
	if len(a.agents.all) == 0 {
		if !a.settings.config().AgentStatus {
			sec.empty = "No Claude Code running. Only Ask Claude panes are listed: agent-status is off (Settings → General), so a claude you start yourself doesn't report what it's doing."
		} else {
			sec.empty = "No Claude Code running. Start claude in any tab, or Ask Claude (" + keyOf("claude.ask") + "). Each one shows here: needs you, working, done or idle, with its last prompt."
		}
	} else {
		sec.title = strings.TrimSpace("Go to Agent… " + keyOf("agents.pick") + " · Next Needing You " + keyOf("agents.next"))
	}
	sections := []sideSection{sec}
	if len(parked) > 0 {
		sections = append(sections, sideSection{title: "Background", tip: "Running after their tab closed; they end when wtf quits", rows: parked, always: true})
	}
	t.list.set(sections)
}

// keyOf is a command's shortcut as shown ("" without one).
func keyOf(id string) string {
	if c, ok := uicore.CommandByID(id); ok && c.Keys != "" {
		return uicore.KeyLabel(c.Keys)
	}
	return ""
}

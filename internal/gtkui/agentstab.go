//go:build linux

package gtkui

import (
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
// across windows, most urgent first. A click goes to it. Event-driven (the
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
	t.list.onClick = func(id string) { w.app.goToAgent(id) }
	t.list.onActivate = func(id string) { w.app.goToAgent(id) }
	t.list.onMenu = func(ids []string, x, y float64) {
		session := ids[0]
		open := "Go to Claude Code"
		if aw := w.app.windowOf(session); aw != nil && aw.background {
			open = "Open in Tab"
		}
		items := []*menuItem{{label: open, run: func() { w.app.goToAgent(session) }},
			{label: "Peek and Reply…", run: func() { w.app.peekAgent(session, &w.win.Window) }}}
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
	for _, st := range a.agents.sorted() {
		state := agentState(st)
		title, cwd := a.agentPlace(st.Session)
		detail := agentLabel(state)
		if cwd != "" {
			detail += " · " + cwd
		}
		r := sideRow{id: st.Session, dot: "agent-" + state, title: title, detail: detail,
			pills: []sidePill{{text: agentAge(st.Since), tip: "In this state since then"}}, highlighted: st.Session == focused,
			tip: "Go to this Claude Code"}
		if state == bridge.StateWaiting && st.Message != "" {
			r.alert, r.alertClass = a.settings.text(st.Message), "agent-alert"
		} else if st.Prompt != "" {
			r.alert, r.alertClass = a.settings.text(st.Prompt), "dim-label"
		}
		session := st.Session
		r.button, r.buttonTip, r.buttonRun = "view-reveal-symbolic", "Peek and reply", func() { a.peekAgent(session, &t.w.win.Window) }
		if aw := a.windowOf(st.Session); aw != nil && aw.background {
			r.tip = "Open it in a tab"
			parked = append(parked, r)
			continue
		}
		rows = append(rows, r)
	}
	sec := sideSection{rows: rows, always: true}
	if len(rows)+len(parked) == 0 {
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

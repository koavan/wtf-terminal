//go:build linux

package gtkui

import (
	"os"
	"strings"
	"time"

	"github.com/dayanraj/wtf/internal/bridge"
	"github.com/dayanraj/wtf/internal/uicore"
)

// standInBackground plays a claude that keeps working after its tab
// closes: it reports through agent-status's hooks, prints a marker, then
// takes reply lines until "quit".
const standInBackground = `#!/bin/sh
d=%DIR%
echo "$*" >> $d/args
h() { printf '%s' "$1" | "$WTF_MCP" hook; }
h '{"hook_event_name":"SessionStart"}'
h '{"hook_event_name":"UserPromptSubmit","prompt":"background work"}'
h '{"hook_event_name":"Notification","message":"Claude needs your permission to use Bash"}'
echo STANDIN-SCREEN
while IFS= read -r line; do
	echo "$line" >> $d/typed
	[ "$line" = quit ] && break
done
h '{"hook_event_name":"SessionEnd"}'
`

// checkBackground (WTF_NATIVE_DEBUG=background): background agents (#34).
// A never-shown window takes output as fast as a hidden tab; closing an
// agent's tab parks it there, running; Peek shows its screen and a reply
// reaches it; Open brings it back into a tab; End ends it; Start Claude
// Code starts one in the background.
func (w *Window) checkBackground() {
	a := w.app
	dir := "/tmp/bgbin"
	_ = os.RemoveAll(dir)
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(dir+"/claude", []byte(strings.ReplaceAll(standInBackground, "%DIR%", dir)), 0o755)
	read := func(name string) string { b, _ := os.ReadFile(dir + "/" + name); return string(b) }
	// New shells find the stand-in first (there's no real claude here).
	_ = os.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	_ = a.be.Config.SetAgentClose("background")
	defer func() { _ = a.be.Config.SetAgentClose("ask") }()
	check(waitUntil(5*time.Second, func() bool { return a.settings.config().AgentClose == "background" }), "agent-close = background")

	first := w.active()
	check(waitUntil(10*time.Second, func() bool { return first != nil && len(first.tracker.Blocks(0, 1<<30)) > 0 }), "shell integration in the first tab")

	// An agent in a second tab.
	var paneID int
	onMain(func() { w.model.Open(uicore.OpenOptions{}); paneID = w.model.ActivePane().ID })
	var p *Pane
	check(waitUntil(10*time.Second, func() bool { p = w.panes[paneID]; return p != nil && len(p.tracker.Blocks(0, 1<<30)) > 0 }), "a second tab")
	if p == nil {
		w.finish()
		return
	}
	session := p.session
	p.write("claude --resume x\r")
	check(waitUntil(10*time.Second, func() bool {
		st, ok := a.agents.all[session]
		return ok && st.State == bridge.StateWaiting
	}), "claude runs and needs you")
	check(strings.Contains(read("args"), "/claude-plugin/"+bridge.StatusSettingsFile), "with wtf's status hooks: %q", read("args"))

	// Closing its tab (agent-close = background) parks it.
	onMain(func() {
		if t, ok := w.model.TabOf(paneID); ok {
			w.closeTabs([]int{t.ID})
		}
	})
	check(waitUntil(5*time.Second, func() bool {
		return w.model.PaneBySession(session) == nil && a.background != nil && a.background.model.PaneBySession(session) != nil
	}), "closing its tab moves it to the background window")
	var visible int
	var shown bool
	onMain(func() { visible, shown = len(a.visibleWindows()), a.background.win.Mapped() })
	check(visible == 1 && !shown, "which isn't shown (%d visible windows, mapped %v)", visible, shown)
	_, still := a.agents.all[session]
	check(still, "and it keeps running")
	var ids []string
	onMain(func() {
		w.showSidebar(uicore.SidebarAgents)
	})
	onMain(func() {
		if t, ok := w.sidePane().(*agentsTab); ok {
			ids = t.list.ids
		}
	})
	check(strings.Contains(strings.Join(ids, ","), session), "the Agents tab lists it: %q", ids)

	// Peek and reply.
	onMain(func() { a.peekAgent(session, &w.win.Window) })
	var screen string
	check(waitUntil(5*time.Second, func() bool {
		if lastPeek == nil {
			return false
		}
		b := lastPeek.screen.Buffer()
		start, end := b.Bounds()
		screen = b.Text(start, end, false)
		return strings.Contains(screen, "STANDIN-SCREEN")
	}), "Peek shows its screen: %q", screen)
	onMain(func() { lastPeek.reply.SetText("hello agent"); lastPeek.sendReply() })
	check(waitUntil(5*time.Second, func() bool { return strings.Contains(read("typed"), "hello agent\n") }), "a reply reaches it: %q", read("typed"))
	w.shot("background-peek")
	onMain(func() { lastPeek.d.Close() })

	// Open: back in a tab of the shown window, the same shell.
	onMain(func() { a.goToAgent(session) })
	check(waitUntil(5*time.Second, func() bool {
		ps := w.model.ActivePane()
		return ps != nil && ps.SessionID == session && a.background == nil
	}), "Open brings it back into a tab (and the empty background window goes)")
	_, still = a.agents.all[session]
	check(still, "still running")

	// Parked again, then ended from the Agents tab.
	onMain(func() {
		if ps := w.model.PaneBySession(session); ps != nil {
			if t, ok := w.model.TabOf(ps.ID); ok {
				w.closeTabs([]int{t.ID})
			}
		}
	})
	check(waitUntil(5*time.Second, func() bool { return a.background != nil && a.background.model.PaneBySession(session) != nil }), "parked again")
	onMain(func() {
		if ps := a.background.model.PaneBySession(session); ps != nil {
			a.background.model.ClosePane(ps.ID)
		}
	})
	check(waitUntil(5*time.Second, func() bool { _, ok := a.agents.all[session]; return !ok && a.background == nil }), "End ends it")

	// Start Claude Code in the background, with a first prompt.
	onMain(func() { w.startAgentIn(dir, "fix the build", true) })
	check(waitUntil(15*time.Second, func() bool { return strings.Contains(read("args"), "fix the build") }), "Start in Background runs claude there: %q", read("args"))
	var started string
	check(waitUntil(5*time.Second, func() bool {
		if a.background == nil {
			return false
		}
		for _, ps := range a.background.model.Panes {
			if _, ok := a.agents.all[ps.SessionID]; ok {
				started = ps.SessionID
				return true
			}
		}
		return false
	}), "and the Agents tab has it")
	onMain(func() {
		if a.background != nil {
			if ps := a.background.model.PaneBySession(started); ps != nil {
				a.background.model.ClosePane(ps.ID)
			}
		}
	})
	check(waitUntil(5*time.Second, func() bool { return a.background == nil }), "ended")
	w.finish()
}

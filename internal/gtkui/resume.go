//go:build linux

package gtkui

import (
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"

	"github.com/dayanraj/wtf/internal/bridge"
	"github.com/dayanraj/wtf/internal/state"
	"github.com/dayanraj/wtf/internal/uicore"
)

// Resuming agents after a restart (#38). A tab that ran Claude Code saves
// it with its pane; background agents are saved when wtf quits. After a
// relaunch, each such tab shows a bar (Resume / Dismiss) and the Agents
// tab lists the background ones under Resumable. Nothing resumes on its
// own: `claude --resume <id>` runs only on your click. As
// macos/Sources/Wtf/Resume.swift.

// resumeCommand resumes a in a shell in cwd: claude finds a session by the
// folder it started in.
func resumeCommand(a state.Agent, cwd string) string {
	resume := "claude --resume " + uicore.ShellQuote(a.Session)
	if cwd == a.Dir {
		return resume
	}
	return "cd " + uicore.ShellQuote(a.Dir) + " && " + resume
}

// resumeTitle is what an agent is called in the bar and the list.
func resumeTitle(a state.Agent) string {
	if a.Name != "" {
		return a.Name
	}
	return filepath.Base(a.Dir)
}

// loadResumables runs once the saved windows are open: the background
// agents saved when wtf last quit, and a bar on each restored pane whose
// agent can still be resumed (the rest are dropped quietly).
func (a *App) loadResumables() {
	type pending struct {
		w     *Window
		id    int
		agent state.Agent
	}
	var list []pending
	for _, w := range a.windows {
		if w.background {
			continue
		}
		for id, ps := range w.model.Panes {
			if ps.Resumable != nil {
				list = append(list, pending{w, id, *ps.Resumable})
			}
		}
	}
	st := a.be.State
	go func() {
		bg := st.BackgroundAgents()
		agents := make([]state.Agent, len(list))
		for i, p := range list {
			agents[i] = p.agent
		}
		ok := st.StillResumable(agents)
		onMainAsync(func() {
			a.agents.resumable = bg
			a.agents.changed()
			for i, p := range list {
				ps := p.w.model.Panes[p.id]
				if ps == nil || ps.Resumable == nil || *ps.Resumable != p.agent {
					continue
				}
				if i < len(ok) && ok[i] {
					p.w.showResumeBar(p.id)
				} else {
					ps.Resumable = nil
					p.w.scheduleSave()
				}
			}
		})
	}()
}

// noteResumable keeps what a restart would offer to resume in the agent's
// pane (not an Ask Claude pane, which belongs to the shell it works on).
// An agent that starts in a pane still offering one takes its place; a
// resumed one gets its name back.
func (w *Window) noteResumable(st bridge.Status) {
	ps := w.model.PaneBySession(st.Session)
	if ps == nil || w.app.claude.isClaude(ps) {
		return
	}
	var ag *state.Agent
	if st.State != bridge.StateEnded && st.ClaudeSession != "" {
		ag = &state.Agent{Session: st.ClaudeSession, Dir: st.Project, Name: st.Name, Transcript: st.Transcript}
	}
	if (ag == nil) != (ps.Agent == nil) || (ag != nil && *ag != *ps.Agent) {
		ps.Agent = ag
		w.scheduleSave()
	}
	if ag == nil {
		return
	}
	if ps.Resumable != nil {
		w.dismissResume(ps.ID)
	}
	if name, ok := w.app.agents.pendingNames[st.ClaudeSession]; ok && st.Name == "" {
		delete(w.app.agents.pendingNames, st.ClaudeSession)
		session := st.Session
		go w.app.be.Bridge.Rename(session, name)
		w.app.renameAgentTab(session, "", name)
	}
}

// showResumeBar: "Claude Code was running here when wtf quit", with
// Dismiss and Resume, over the top of the pane.
func (w *Window) showResumeBar(id int) {
	ps, p := w.model.Panes[id], w.panes[id]
	if ps == nil || ps.Resumable == nil || p == nil {
		return
	}
	ag := *ps.Resumable
	if p.resumeBar != nil {
		p.root.RemoveOverlay(p.resumeBar)
	}
	text := "Claude Code was running here when wtf quit."
	if ag.Name != "" {
		text = "Claude Code “" + ag.Name + "” was running here when wtf quit."
	}
	bar := gtk.NewBox(gtk.OrientationHorizontal, 8)
	bar.AddCSSClass("find-bar")
	bar.AddCSSClass("wtf-resume-bar")
	bar.SetHAlign(gtk.AlignFill)
	bar.SetVAlign(gtk.AlignStart)
	bar.SetMarginTop(6)
	bar.SetMarginStart(8)
	bar.SetMarginEnd(8)
	icon := gtk.NewImageFromIconName("starred-symbolic")
	label := gtk.NewLabel(text)
	label.SetXAlign(0)
	label.SetHExpand(true)
	label.SetEllipsize(pango.EllipsizeEnd)
	no := gtk.NewButtonWithLabel("Dismiss")
	no.AddCSSClass("flat")
	no.SetTooltipText("Stop offering it")
	no.ConnectClicked(func() { w.dismissResume(id) })
	yes := gtk.NewButtonWithLabel("Resume")
	yes.AddCSSClass("suggested-action")
	yes.SetTooltipText("Runs: " + resumeCommand(ag, ps.Cwd))
	yes.ConnectClicked(func() { w.resumePane(id) })
	for _, c := range []gtk.Widgetter{icon, label, no, yes} {
		bar.Append(c)
	}
	p.resumeBar = bar
	p.root.AddOverlay(bar)
	w.app.debugLog("resume bar: pane %d", id)
}

// resumePane types claude --resume into the pane's shell when it's idle at
// a prompt, else runs it in a new tab in its folder.
func (w *Window) resumePane(id int) {
	ps, p := w.model.Panes[id], w.panes[id]
	if ps == nil || ps.Resumable == nil {
		return
	}
	ag := *ps.Resumable
	if ag.Name != "" {
		w.app.agents.pendingNames[ag.Session] = ag.Name
	}
	cmd := resumeCommand(ag, ps.Cwd)
	w.dismissResume(id)
	if p != nil && ps.IsShell && !ps.Running {
		p.write(uicore.ClearLine + cmd + "\r")
		p.term.GrabFocus()
		return
	}
	cwd := ps.Cwd
	w.model.Open(uicore.OpenOptions{Cwd: &cwd, Command: cmd})
}

// dismissResume stops offering the pane's agent.
func (w *Window) dismissResume(id int) {
	if ps := w.model.Panes[id]; ps != nil {
		ps.Resumable = nil
	}
	if p := w.panes[id]; p != nil && p.resumeBar != nil {
		p.root.RemoveOverlay(p.resumeBar)
		p.resumeBar = nil
	}
	w.scheduleSave()
}

// resumeBackground resumes a background agent from before the restart in
// a new tab of w, or in the background.
func (w *Window) resumeBackground(ag state.Agent, inTab bool) {
	a := w.app
	if ag.Name != "" {
		a.agents.pendingNames[ag.Session] = ag.Name
	}
	dir := ag.Dir
	opts := uicore.OpenOptions{Cwd: &dir, Command: "claude --resume " + uicore.ShellQuote(ag.Session)}
	switch {
	case inTab:
		w.model.Open(opts)
	case a.background != nil:
		a.background.model.Open(opts)
	default:
		a.openBackground(windowStart{open: &opts})
	}
	a.forgetResumable(ag)
}

// forgetResumable drops a background agent from the Resumable list.
func (a *App) forgetResumable(ag state.Agent) {
	a.agents.resumable = slices.DeleteFunc(a.agents.resumable, func(x state.Agent) bool { return x == ag })
	left := slices.Clone(a.agents.resumable)
	st := a.be.State
	go func() { _ = st.SaveBackgroundAgents(left) }()
	a.agents.changed()
}

// resumeAll (Resume All…): lists them, then resumes each in the background.
func (w *Window) resumeAll() {
	list := slices.Clone(w.app.agents.resumable)
	if len(list) == 0 {
		return
	}
	var lines []string
	for _, ag := range list {
		lines = append(lines, "• "+resumeTitle(ag)+" — "+uicore.PrettyPath(ag.Dir, w.app.home))
	}
	w.confirmPlan(&w.win.Window, "Resume "+strconv.Itoa(len(list))+" agents?",
		"Each runs claude --resume in a new shell in the background, in its folder. The Agents tab shows them.\n\n"+strings.Join(lines, "\n"),
		"Resume All", false, func() {
			for _, ag := range list {
				w.resumeBackground(ag, false)
			}
		}, nil)
}

// saveBackgroundAgents, when wtf quits: the agents running in the
// background and the ones still offered from last time, for next launch.
func (a *App) saveBackgroundAgents() {
	if a.be == nil {
		return
	}
	list := slices.Clone(a.agents.resumable)
	if a.background != nil {
		for _, ps := range a.background.model.Panes {
			if ps.Agent != nil && !slices.Contains(list, *ps.Agent) {
				list = append(list, *ps.Agent)
			}
		}
	}
	if err := a.be.State.SaveBackgroundAgents(list); err != nil {
		a.debugLog("saving background agents: %v", err)
	}
}

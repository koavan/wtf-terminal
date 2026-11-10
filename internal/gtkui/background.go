//go:build linux

package gtkui

import (
	"context"
	"fmt"
	"strings"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"

	"github.com/dayanraj/wtf/internal/uicore"
)

// Background agents (#34): a Claude Code whose tab you close can keep
// running in wtf's background window, a window that's never shown. Its
// pane moves there with its shell and scrollback, the way a tab moves
// between windows, so its hooks, badges and notifications carry on; the
// Agents tab opens it again in a tab, peeks at its screen, types a reply
// you write, or ends it. Background agents end when wtf quits. As
// macos/Sources/Wtf/Background.swift.

// backgroundName is the background window's name (and its shells' window
// label in the terminal service).
const backgroundName = "background"

// openBackground makes the background window, never shown, with start.
func (a *App) openBackground(start windowStart) *Window {
	w := newWindow(a, backgroundName)
	w.background = true
	a.windows = append(a.windows, w)
	a.background = w
	w.bootstrap(start)
	return w
}

// visibleWindows are the windows you can see (all but the background one).
func (a *App) visibleWindows() []*Window {
	var out []*Window
	for _, w := range a.windows {
		if !w.background {
			out = append(out, w)
		}
	}
	return out
}

// backgroundCount is how many agents run in the background window.
func (a *App) backgroundCount() int {
	if a.background == nil {
		return 0
	}
	return len(a.background.model.Panes)
}

// agentPanes are the panes among ids running a Claude Code wtf knows of.
func (w *Window) agentPanes(ids []int) []int {
	var out []int
	for _, id := range ids {
		if ps := w.model.Panes[id]; ps != nil && ps.SessionID != "" {
			if _, ok := w.app.agents.all[ps.SessionID]; ok {
				out = append(out, id)
			}
		}
	}
	return out
}

// park moves panes to the background window, their shells still running.
// The last visible window keeps a fresh tab rather than closing.
func (w *Window) park(ids []int) {
	a := w.app
	for _, id := range ids {
		p, ps := w.panes[id], w.model.Panes[id]
		if p == nil || ps == nil || p.session == "" {
			continue
		}
		if t, ok := w.model.TabOf(id); ok && len(w.model.Tabs()) == 1 && len(t.Root.Leaves()) == 1 && len(a.visibleWindows()) == 1 {
			w.model.Open(uicore.OpenOptions{Cwd: new(string)})
		}
		// Its window label first, so this window emptying (CloseWindow)
		// spares it.
		_ = a.be.Terminal.Adopt(p.session, backgroundName)
		delete(w.panes, id)
		w.recordsPaneGone(id, true)
		detachWidget(&p.root.Widget)
		payload, ok := w.model.DetachPane(id, func(*uicore.PaneState) string { return "" })
		if !ok {
			continue
		}
		tab := &movingTab{payload: payload, panes: map[string]*Pane{p.session: p}}
		if a.background != nil {
			a.background.adopt(tab)
		} else {
			a.openBackground(windowStart{tab: tab})
		}
	}
	if len(ids) > 0 {
		w.toast(map[bool]string{true: "Claude Code keeps running in the background: the Agents tab opens it again",
			false: "Claude Code sessions keep running in the background: the Agents tab opens them again"}[len(ids) == 1])
	}
}

// openAgent brings a background agent back into a tab of the window in
// front (a new window when none shows).
func (a *App) openAgent(session string) {
	bg := a.background
	if bg == nil {
		return
	}
	ps := bg.model.PaneBySession(session)
	if ps == nil {
		return
	}
	t, ok := bg.model.TabOf(ps.ID)
	if !ok {
		return
	}
	// agent-group: back in a window's tabs, it joins "agents".
	defer coreglib.IdleAdd(func() { a.autoGroup(session) })
	if front := a.front(); front != nil {
		bg.moveTab(t.ID, front, 0)
		return
	}
	if tab := bg.detach(t.ID); tab != nil {
		a.openWindowWith(windowStart{tab: tab})
	}
}

// endAgent ends an agent (its shell too), after asking.
func (a *App) endAgent(session string, parent *gtk.Window) {
	w := a.windowOf(session)
	if w == nil {
		return
	}
	ps := w.model.PaneBySession(session)
	if ps == nil {
		return
	}
	title, _ := a.agentPlace(session)
	id := ps.ID
	a.confirm(parent, "End this Claude Code?", title+"\n\nIts shell closes too.", "End", func() { w.model.ClosePane(id) })
}

// askAgentClose: some of the panes being closed run Claude Code. Keep them
// running in the background, end them, or cancel (agent-close says which,
// unless it's "ask").
func (w *Window) askAgentClose(agents []int, done func(keep bool)) {
	a := w.app
	switch a.settings.config().AgentClose {
	case "background":
		done(true)
		return
	case "end":
		done(false)
		return
	}
	var lines []string
	for i, id := range agents {
		if i == 6 {
			lines = append(lines, fmt.Sprintf("and %d more", len(agents)-6))
			break
		}
		ps := w.model.Panes[id]
		title, _ := a.agentPlace(ps.SessionID)
		lines = append(lines, title+" — "+strings.ToLower(agentLabel(agentState(a.agents.all[ps.SessionID]))))
	}
	head := "Keep Claude Code running?"
	body := "Claude Code runs in what you're closing. It can keep running in the background: the Agents tab shows it and opens it again. It ends when wtf quits."
	if len(agents) > 1 {
		body = fmt.Sprintf("%d Claude Code sessions run in what you're closing. They can keep running in the background: the Agents tab shows them and opens them again. They end when wtf quits.", len(agents))
	}
	d := adw.NewMessageDialog(&w.win.Window, head, body+"\n\n"+strings.Join(lines, "\n"))
	again := gtk.NewCheckButtonWithLabel("Don't ask again (agent-close)")
	d.SetExtraChild(again)
	d.AddResponse("cancel", "Cancel")
	d.AddResponse("end", "End Claude Code")
	d.AddResponse("keep", "Keep Running in Background")
	d.SetResponseAppearance("end", adw.ResponseDestructive)
	d.SetResponseAppearance("keep", adw.ResponseSuggested)
	d.SetDefaultResponse("keep")
	d.SetCloseResponse("cancel")
	d.ConnectResponse(func(r string) {
		if r == "cancel" {
			return
		}
		if again.Active() {
			mode := map[string]string{"keep": "background", "end": "end"}[r]
			cfg := a.be.Config
			go func() { _ = cfg.SetAgentClose(mode) }()
		}
		done(r == "keep")
	})
	a.debugLog("confirm: %s", head)
	d.Present()
}

// peekAgent shows an agent's screen (Hide Secrets applied), updated every
// second while open, with a reply box: what you type is sent with Enter,
// and Enter and Esc go as keys, for Claude Code's own prompts. Nothing is
// typed unless you click.
func (a *App) peekAgent(session string, parent *gtk.Window) {
	w := a.windowOf(session)
	if w == nil {
		return
	}
	title, cwd := a.agentPlace(session)
	d := adw.NewWindow()
	d.SetTitle("Claude Code — " + title)
	d.SetTransientFor(parent)
	d.SetDefaultSize(760, 520)

	screen := gtk.NewTextView()
	screen.SetEditable(false)
	screen.SetCursorVisible(false)
	screen.SetMonospace(true)
	screen.SetWrapMode(gtk.WrapNone)
	screen.AddCSSClass("wtf-peek")
	scroll := gtk.NewScrolledWindow()
	scroll.SetChild(screen)
	scroll.SetVExpand(true)

	state := gtk.NewLabel("")
	state.SetXAlign(0)
	state.SetEllipsize(pango.EllipsizeEnd)
	state.AddCSSClass("dim-label")

	pane := func() *Pane {
		if w := a.windowOf(session); w != nil {
			if ps := w.model.PaneBySession(session); ps != nil {
				return w.panes[ps.ID]
			}
		}
		return nil
	}
	refresh := func() bool {
		p := pane()
		if p == nil {
			state.SetText("It's gone: Claude Code quit or its shell closed.")
			return false
		}
		screen.Buffer().SetText(a.settings.text(strings.Join(p.lastLines(40), "\n")))
		st, ok := a.agents.all[session]
		what := "Not reporting"
		if ok {
			what = agentLabel(agentState(st))
			if agentState(st) == "waiting" && st.Message != "" {
				what += ": " + a.settings.text(st.Message)
			}
		}
		where := "In a tab"
		if aw := a.windowOf(session); aw != nil && aw.background {
			where = "In the background"
		}
		state.SetText(what + " · " + where + " · " + cwd)
		return true
	}
	refresh()
	timer := coreglib.TimeoutSecondsAdd(1, func() bool { return refresh() })
	d.ConnectCloseRequest(func() bool {
		coreglib.SourceRemove(timer)
		return false
	})

	send := func(text string) {
		if p := pane(); p != nil {
			p.write(text)
		}
	}
	reply := gtk.NewEntry()
	reply.SetHExpand(true)
	reply.SetPlaceholderText("Reply to Claude Code (sent with Enter)")
	sendReply := func() {
		text := reply.Text()
		if strings.TrimSpace(text) == "" {
			return
		}
		send(text)
		// Apart from the text, so it isn't taken as part of a paste.
		coreglib.TimeoutAdd(200, func() bool { send("\r"); return false })
		reply.SetText("")
	}
	reply.ConnectActivate(sendReply)
	button := func(label, tip string, run func()) *gtk.Button {
		b := gtk.NewButtonWithLabel(label)
		b.SetTooltipText(tip)
		b.ConnectClicked(run)
		return b
	}
	row := gtk.NewBox(gtk.OrientationHorizontal, 6)
	row.Append(reply)
	row.Append(button("Send", "Type it in Claude Code, then Enter", sendReply))
	row.Append(button("Enter", "Press Enter in Claude Code (accepts what its prompt has selected)", func() { send("\r") }))
	row.Append(button("Esc", "Press Esc in Claude Code (cancels its prompt or stops it)", func() { send("\x1b") }))
	actions := gtk.NewBox(gtk.OrientationHorizontal, 6)
	actions.Append(button("Open in Tab", "Show it in a tab", func() {
		d.Close()
		a.goToAgent(session)
	}))
	actions.Append(button("End…", "End this Claude Code and its shell", func() { a.endAgent(session, &d.Window) }))

	box := gtk.NewBox(gtk.OrientationVertical, 8)
	box.SetMarginTop(10)
	box.SetMarginBottom(10)
	box.SetMarginStart(10)
	box.SetMarginEnd(10)
	box.Append(state)
	box.Append(scroll)
	box.Append(row)
	box.Append(actions)
	view := adw.NewToolbarView()
	view.AddTopBar(adw.NewHeaderBar())
	view.SetContent(box)
	d.SetContent(view)
	a.debugLog("peek: %s", session)
	d.Present()
	reply.GrabFocus()
	if a.debug != "" {
		lastPeek = &peekDialog{d: d, reply: reply, screen: screen, send: send, sendReply: sendReply}
	}
}

// peekDialog is the Peek window last shown (for the self-checks).
type peekDialog struct {
	d         *adw.Window
	reply     *gtk.Entry
	screen    *gtk.TextView
	send      func(string)
	sendReply func()
}

var lastPeek *peekDialog

// startAgent asks for a folder and an optional first prompt, then starts
// claude there in a new tab or in the background.
func (w *Window) startAgent() {
	a := w.app
	folder := a.home
	if ps := w.model.ActivePane(); ps != nil && ps.Cwd != "" {
		folder = ps.Cwd
	}
	d := adw.NewMessageDialog(&w.win.Window, "Start Claude Code",
		"Runs claude in a new shell in this folder, with a first prompt if you give one.")
	where := gtk.NewEntry()
	where.SetText(folder)
	where.SetHExpand(true)
	choose := gtk.NewButtonWithLabel("Choose…")
	choose.ConnectClicked(func() {
		fd := gtk.NewFileDialog()
		fd.SetTitle("Folder for Claude Code")
		fd.SetAcceptLabel("Choose")
		fd.SetInitialFolder(gio.NewFileForPath(where.Text()))
		fd.SelectFolder(context.Background(), &d.Window, func(res gio.AsyncResulter) {
			if dir, err := fd.SelectFolderFinish(res); err == nil && dir.Path() != "" {
				where.SetText(dir.Path())
			}
		})
	})
	folderRow := gtk.NewBox(gtk.OrientationHorizontal, 6)
	folderRow.Append(where)
	folderRow.Append(choose)
	prompt := gtk.NewEntry()
	prompt.SetPlaceholderText("First prompt (optional)")
	box := gtk.NewBox(gtk.OrientationVertical, 6)
	box.Append(folderRow)
	box.Append(prompt)
	d.SetExtraChild(box)
	d.AddResponse("cancel", "Cancel")
	d.AddResponse("background", "Start in Background")
	d.AddResponse("tab", "Start in a Tab")
	d.SetResponseAppearance("tab", adw.ResponseSuggested)
	d.SetDefaultResponse("tab")
	d.SetCloseResponse("cancel")
	d.ConnectResponse(func(r string) {
		if r == "cancel" {
			return
		}
		w.startAgentIn(strings.TrimSpace(where.Text()), strings.TrimSpace(prompt.Text()), r == "background")
	})
	d.Present()
	prompt.GrabFocus()
}

// startAgentIn runs claude (with a first prompt) in a new shell in folder:
// a tab here, or the background window.
func (w *Window) startAgentIn(folder, prompt string, background bool) {
	a := w.app
	if folder == "" {
		folder = a.home
	}
	command := agentCommand(prompt)
	opts := uicore.OpenOptions{Cwd: &folder, Command: command}
	if !background {
		w.model.Open(opts)
		return
	}
	if a.background != nil {
		a.background.model.Open(opts)
	} else {
		a.openBackground(windowStart{open: &opts})
	}
	w.toast("Claude Code started in the background: the Agents tab shows it")
}

// agentCommand is what a new agent's shell runs: claude, through the
// shell integration's wrapper (agent-status), with a first prompt.
func agentCommand(prompt string) string {
	if prompt == "" {
		return "claude"
	}
	return "claude " + uicore.ShellQuote(prompt)
}

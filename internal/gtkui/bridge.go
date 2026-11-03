//go:build linux

package gtkui

import (
	"strings"
	"time"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"

	"github.com/dayanraj/wtf/internal/bridge"
	"github.com/dayanraj/wtf/internal/uicore"
)

// A run that hasn't finished by then gets Ctrl-C (as an investigation step).
const bridgeRunTimeout = 60 * time.Second

// bridgeRequest answers the bridge's requests about a shell (#32): what it
// is, its recent command blocks, typing at its prompt, and running a line
// as a command block. The tools that use them, and every check on what may
// run, are in internal/bridge; the approval is Claude Code's own prompt.
// Main loop only.
func (a *App) bridgeRequest(req bridge.UIRequest) {
	svc := a.be.Bridge
	reply := func(r bridge.Reply) { go svc.Reply(req.ID, r) }
	fail := func(msg string) { reply(bridge.Reply{Error: msg}) }
	w, ps, p := a.bridgePane(req.Session)
	if p == nil {
		fail("That shell has closed")
		return
	}
	switch req.Kind {
	case bridge.KindPane:
		title := ""
		if t, ok := w.model.TabOf(ps.ID); ok {
			title = a.settings.text(w.model.Title(t))
		}
		process := ""
		if !ps.IsShell {
			process = ps.Process
		}
		reply(bridge.Reply{Pane: &bridge.Pane{Title: title, Cwd: ps.Cwd, Env: ps.Env, Running: ps.Busy(),
			Process: process, Integrated: ps.Integrated}})
	case bridge.KindBlocks:
		var list []bridge.Block
		for _, id := range p.tracker.RecentIDs(max(1, req.Count)) {
			if b, ok := bridgeBlock(p, id, false); ok {
				list = append(list, b)
			}
		}
		reply(bridge.Reply{Blocks: list})
	case bridge.KindType:
		switch p.typeAtPrompt(req.Text) {
		case typed:
			reply(bridge.Reply{})
		case shellBusy:
			fail("The shell is busy: a program is running in it")
		case copiedInstead:
			fail("The command has several lines and this shell would run each as it's typed, so it was copied to the clipboard instead")
		}
	case bridge.KindRun:
		a.bridgeRun(req, p, reply, fail)
	case bridge.KindSnippet:
		// Filled in for the user to review and save; nothing is saved here.
		if req.Snippet == nil {
			fail("No snippet given")
			return
		}
		w.win.Present()
		w.showSnippetDraft(*req.Snippet)
		reply(bridge.Reply{})
	default:
		fail("wtf doesn't know the request " + req.Kind)
	}
}

// bridgePane finds a shell by its backend session, in any window.
func (a *App) bridgePane(session string) (*Window, *uicore.PaneState, *Pane) {
	for _, w := range a.windows {
		for id, ps := range w.model.Panes {
			if ps.SessionID == session {
				if p := w.panes[id]; p != nil {
					return w, ps, p
				}
			}
		}
	}
	return nil, nil, nil
}

func bridgeBlock(p *Pane, id int, timedOut bool) (bridge.Block, bool) {
	b, ok := p.tracker.Info(id)
	if !ok {
		return bridge.Block{}, false
	}
	out, _ := p.tracker.Output(id)
	code := -1
	if b.ExitCode != nil {
		code = *b.ExitCode
	}
	started := ""
	if !b.StartedAt.IsZero() {
		started = b.StartedAt.Format(time.RFC3339)
	}
	return bridge.Block{ID: id, Command: b.Text, ExitCode: code, Output: out,
		DurationMs: float64(b.Duration.Milliseconds()), StartedAt: started, TimedOut: timedOut}, true
}

// bridgeRun types the line with Enter and waits for its block to end.
func (a *App) bridgeRun(req bridge.UIRequest, p *Pane, reply func(bridge.Reply), fail func(string)) {
	if _, ps, _ := a.bridgePane(req.Session); !ps.Integrated {
		fail("This shell has no shell integration, so wtf can't tell when a command ends")
		return
	}
	alive := func() bool { _, _, q := a.bridgePane(req.Session); return q == p }
	idle := func() bool {
		_, ps, q := a.bridgePane(req.Session)
		return q == p && ps.IsShell && !ps.Running
	}
	// The shell reports it's back at its prompt a moment after the last
	// command's block ends: wait for that, briefly.
	settle := time.Now().Add(5 * time.Second)
	coreglib.TimeoutAdd(100, func() bool {
		switch {
		case idle():
		case time.Now().After(settle) || !alive():
			fail("The shell isn't at its prompt (a program is running in it, or it closed)")
			return false
		default:
			return true
		}
		before, _ := p.tracker.LastFinished()
		p.write(uicore.ClearLine + req.Text + "\r")
		deadline := time.Now().Add(bridgeRunTimeout)
		interrupted := false
		coreglib.TimeoutAdd(150, func() bool {
			if !alive() {
				fail("The shell closed")
				return false
			}
			if id, ok := p.tracker.LastFinished(); ok && id != before && !p.feed.busy() {
				if b, ok := bridgeBlock(p, id, interrupted); ok && b.ExitCode >= 0 {
					reply(bridge.Reply{Block: &b})
					return false
				}
			}
			if time.Now().After(deadline) {
				if interrupted {
					fail("The command didn't stop after Ctrl-C")
					return false
				}
				p.write("\x03")
				interrupted = true
				deadline = time.Now().Add(5 * time.Second)
			}
			return true
		})
		return false
	})
}

type typeResult int

const (
	typed typeResult = iota
	shellBusy
	copiedInstead
)

// typeAtPrompt types a suggested command at the pane's prompt without
// Enter: Ctrl-U first, then the text (a multi-line one bracketed). A
// multi-line one is copied instead when the shell would run each line;
// nothing is typed while a program runs.
func (p *Pane) typeAtPrompt(text string) typeResult {
	ps := p.win.model.Panes[p.id]
	if ps == nil || !ps.IsShell || ps.Running {
		return shellBusy
	}
	if strings.Contains(text, "\n") {
		if !p.feed.bracketed {
			p.term.Clipboard().SetText(text)
			return copiedInstead
		}
		text = "\x1b[200~" + text + "\x1b[201~"
	}
	// One write, so nothing lands between clearing the line and the text.
	p.write(uicore.ClearLine + text)
	p.term.GrabFocus()
	return typed
}

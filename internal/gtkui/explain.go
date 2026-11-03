//go:build linux

package gtkui

import "fmt"

// Explain this failure (#24) goes through the Claude pane's /wtf:explain.

// explainLastFailure explains the focused pane's last command, when it failed.
func (w *Window) explainLastFailure() {
	p := w.active()
	if p == nil {
		return
	}
	id, ok := p.tracker.LastFinished()
	if !ok {
		w.toast("No finished command to explain yet")
		return
	}
	if b, _ := p.tracker.Info(id); b.ExitCode != nil && *b.ExitCode == 0 {
		w.toast("The last command succeeded: nothing to explain")
		return
	}
	w.explainBlock(p, id)
}

// explainBlock asks pane p's Claude pane why block id failed.
func (w *Window) explainBlock(p *Pane, id int) {
	w.model.FocusPane(p.id)
	w.askClaudeTo(claudeMessage{text: fmt.Sprintf("/wtf:explain block %d", id), enter: true})
}

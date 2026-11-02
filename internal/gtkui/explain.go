//go:build linux

package gtkui

import (
	"fmt"
	"strings"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/dayanraj/wtf/internal/ai"
	"github.com/dayanraj/wtf/internal/explain"
)

// Explain this failure (#24): a failed command block's facts go to the AI
// (internal/explain builds them), and the reply streams into a dialog. Its
// fix command is typed at the prompt for the user to read and run; nothing
// runs it. Like the macOS app's ExplainSheet.

// explainDialog is one explanation: gathering the facts, then the reply.
// Main loop only.
type explainDialog struct {
	w                   *Window
	p                   *Pane
	s                   *toolsSheet
	text                *gtk.TextView
	fixAt               *gtk.Label
	state               *gtk.Label
	sent, copy, typeFix *gtk.Button

	call *aiCall
	req  ai.Request
	// The facts are in req.
	ready  bool
	reply  string
	fix    string
	done   *ai.Done
	closed bool
}

// explainShown is the open dialog, if any (for the self-check).
var explainShown *explainDialog

// aiEnabled says whether an AI provider is set up.
func (w *Window) aiEnabled() bool { return len(w.app.settings.config().AI.Providers) > 0 }

// explainLastFailure explains the focused pane's last command, when it failed.
func (w *Window) explainLastFailure() {
	p := w.active()
	switch {
	case !w.aiEnabled():
		w.toast("Set up an AI provider in Settings first (Ctrl+,)")
		return
	case p == nil:
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

// explainBlock opens the dialog for block id of pane p.
func (w *Window) explainBlock(p *Pane, id int) {
	b, ok := p.tracker.Info(id)
	if !ok || b.ExitCode == nil {
		return
	}
	f := explain.Failure{Command: b.Text, ExitCode: *b.ExitCode, DurationMs: float64(b.Duration.Milliseconds())}
	f.Output, _ = p.tracker.Output(id)
	if ps := w.model.Panes[p.id]; ps != nil {
		f.Env, f.Cwd = ps.Env, ps.Cwd
	}

	shown := []rune(b.Text)
	if len(shown) > 60 {
		shown = append(shown[:59], '…')
	}
	d := &explainDialog{w: w, p: p, s: newToolsSheet(w, "Explain: "+w.app.settings.text(string(shown)), 640)}
	explainShown = d
	sub := fmt.Sprintf("✗ exit %d", *b.ExitCode)
	if name := w.app.settings.config().AIDefault; name != "" {
		sub += " · asking " + name
	}
	d.s.subtitle(sub)

	d.text = gtk.NewTextView()
	d.text.SetEditable(false)
	d.text.SetCursorVisible(false)
	d.text.SetWrapMode(gtk.WrapWordChar)
	d.text.SetTopMargin(8)
	d.text.SetBottomMargin(8)
	d.text.SetLeftMargin(10)
	d.text.SetRightMargin(10)
	scroll := gtk.NewScrolledWindow()
	scroll.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	scroll.SetMinContentHeight(240)
	scroll.SetMaxContentHeight(240)
	scroll.SetChild(d.text)
	frame := gtk.NewFrame("")
	frame.SetChild(scroll)
	d.s.body.Append(frame)
	d.fixAt = d.s.label("", "wtf-tools-mono")
	d.fixAt.SetSelectable(true)
	d.fixAt.SetVisible(false)
	d.s.body.Append(d.fixAt)
	d.state = d.s.label("Gathering the command's facts…", "dim-label", "wtf-sheet-sub")
	d.s.body.Append(d.state)

	d.sent = d.s.button("What's Sent", func() {
		if d.ready {
			w.previewAI(&d.s.win.Window, d.req)
		}
	})
	d.copy = d.s.button("Copy", d.copyAll)
	d.typeFix = d.s.button("Type Fix at Prompt", d.typeAtPrompt)
	for _, b := range []*gtk.Button{d.sent, d.copy, d.typeFix} {
		b.SetSensitive(false)
	}
	closeBtn := d.s.button("Close", d.s.close)
	d.s.buttons([]*gtk.Button{d.sent, d.copy}, []*gtk.Button{closeBtn, d.typeFix}, nil)
	closeBtn.GrabFocus()
	d.s.win.ConnectCloseRequest(func() bool {
		d.closed = true
		if d.call != nil {
			d.call.cancel()
		}
		if explainShown == d {
			explainShown = nil
		}
		return false
	})
	d.s.win.Present()

	svc := w.app.be.Explain
	go func() {
		r := svc.Request(f)
		onMainAsync(func() { d.ask(r) })
	}()
}

// ask sends the facts.
func (d *explainDialog) ask(r ai.Request) {
	if d.closed {
		return
	}
	d.req, d.ready = r, true
	d.sent.SetSensitive(true)
	d.state.SetText("Waiting for the reply…  Close stops it.")
	d.call = d.w.askAI(&d.s.win.Window, r, func(chunk string) {
		d.reply += chunk
		d.show(d.reply)
	}, d.finish)
}

func (d *explainDialog) finish(done ai.Done) {
	d.call = nil
	d.done = &done
	if d.closed {
		return
	}
	switch {
	case done.Cancelled && d.reply == "":
		// Cancelled in the first-send preview: nothing was asked.
		d.s.close()
		return
	case done.Cancelled:
		d.state.SetText("Stopped.")
		return
	case done.Error != "":
		d.state.SetText("The AI couldn't answer: " + done.Error)
		d.state.AddCSSClass("error")
		return
	}
	d.reply = done.Text
	r := d.w.app.be.Explain.Parse(done.Text)
	d.show(r.Cause)
	d.fix = r.Fix
	d.copy.SetSensitive(d.reply != "")
	note := " · no fix command suggested"
	if d.fix != "" {
		d.fixAt.SetText("$ " + d.fix)
		d.fixAt.SetVisible(true)
		d.typeFix.SetSensitive(true)
		d.typeFix.AddCSSClass("suggested-action")
		d.s.win.SetDefaultWidget(d.typeFix)
		note = " · the fix is typed, not run"
	}
	d.state.SetText(aiSummary(done) + note)
}

func (d *explainDialog) show(text string) {
	buf := d.text.Buffer()
	buf.SetText(d.w.app.settings.text(text))
	d.text.ScrollToIter(buf.EndIter(), 0, false, 0, 1)
}

func (d *explainDialog) shownText() string {
	buf := d.text.Buffer()
	return buf.Text(buf.StartIter(), buf.EndIter(), false)
}

func (d *explainDialog) copyAll() {
	text := strings.TrimSpace(d.shownText())
	if d.fix != "" {
		text += "\n\n" + d.fix
	}
	d.p.term.Clipboard().SetText(d.w.app.settings.text(text))
	d.w.toast("Copied the explanation")
}

// typeAtPrompt puts the fix at the pane's prompt, without Enter.
func (d *explainDialog) typeAtPrompt() {
	if d.fix == "" {
		return
	}
	switch d.p.typeAtPrompt(d.fix) {
	case shellBusy:
		d.w.toast("The shell is busy — finish the running program first")
	case copiedInstead:
		d.s.close()
		d.w.toast("The fix has several lines and this shell would run them as typed: copied instead")
	default:
		d.s.close()
	}
}

// aiSummary is "1.2 s · 12 in, 3 out tokens · $0.0004": what the provider reported.
func aiSummary(d ai.Done) string {
	parts := []string{fmt.Sprintf("%d ms", d.Millis)}
	if d.Millis >= 1000 {
		parts[0] = fmt.Sprintf("%.1f s", float64(d.Millis)/1000)
	}
	if d.Usage.Counted {
		parts = append(parts, fmt.Sprintf("%d in, %d out tokens", d.Usage.InputTokens, d.Usage.OutputTokens))
	}
	if d.Usage.Cost > 0 {
		parts = append(parts, fmt.Sprintf("$%.4f", d.Usage.Cost))
	}
	return strings.Join(parts, " · ")
}

//go:build linux

package gtkui

import (
	"strings"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/dayanraj/wtf/internal/ai"
	"github.com/dayanraj/wtf/internal/review"
)

// Change reviewer (#26): what a terraform plan, kubectl diff or helm diff
// (a command block or a saved plan file) will do, parsed by
// internal/review and listed as facts. With an AI set up, it then writes a
// plain-English summary of those facts. Nothing is applied. Like the macOS
// app's ReviewSheet.

// reviewDialog is one review. Main loop only.
type reviewDialog struct {
	w       *Window
	s       *toolsSheet
	sub     *gtk.Label
	facts   *gtk.TextView
	summary *gtk.TextView // nil without an AI
	status  *gtk.Label
	sent    *gtk.Button
	copy    *gtk.Button

	env    map[string]string
	cwd    string
	review *review.Review
	call   *aiCall
	req    ai.Request
	ready  bool
	reply  string
	done   *ai.Done
	closed bool
}

// reviewShown is the open dialog, if any (for the self-check).
var reviewShown *reviewDialog

// reviewBlock reviews block id of pane p.
func (w *Window) reviewBlock(p *Pane, id int) {
	b, ok := p.tracker.Info(id)
	if !ok {
		return
	}
	out, _ := p.tracker.Output(id)
	shown := []rune(b.Text)
	if len(shown) > 60 {
		shown = append(shown[:59], '…')
	}
	d := w.newReview("Review: " + w.app.settings.text(string(shown)))
	if ps := w.model.Panes[p.id]; ps != nil {
		d.env, d.cwd = ps.Env, ps.Cwd
	}
	svc, text := w.app.be.Review, b.Text
	go func() {
		r := svc.Parse(text, out)
		onMainAsync(func() { d.show(r, nil) })
	}()
}

// reviewFile reviews a saved plan file.
func (w *Window) reviewFile(path string) {
	name := path[strings.LastIndex(path, "/")+1:]
	d := w.newReview("Review: " + name)
	if ps := w.model.ActivePane(); ps != nil {
		d.env, d.cwd = ps.Env, ps.Cwd
	}
	d.status.SetText("Reading the plan…")
	svc, env := w.app.be.Review, d.env
	go func() {
		r, err := svc.ParseFile(path, env)
		onMainAsync(func() { d.show(r, err) })
	}()
}

// reviewLastCommand reviews the focused pane's last finished command.
func (w *Window) reviewLastCommand() {
	p := w.active()
	if p == nil {
		return
	}
	id, ok := p.tracker.LastFinished()
	if !ok {
		w.toast("No finished command to review yet")
		return
	}
	w.reviewBlock(p, id)
}

func (w *Window) newReview(title string) *reviewDialog {
	d := &reviewDialog{w: w, s: newToolsSheet(w, title, 700)}
	reviewShown = d
	d.sub = d.s.label("Reading…", "dim-label", "wtf-sheet-sub")
	d.s.body.Append(d.sub)
	d.facts = d.textBox(200, true)
	if w.aiEnabled() {
		h := d.s.label("Summary", "heading")
		d.s.body.Append(h)
		d.summary = d.textBox(200, false)
	} else {
		d.s.body.Append(d.s.label("Set up an AI provider in Settings (Ctrl+,) for a plain-English summary of these changes.", "dim-label", "wtf-sheet-sub"))
	}
	d.status = d.s.label("", "dim-label", "wtf-sheet-sub")
	d.s.body.Append(d.status)
	d.sent = d.s.button("What's Sent", func() {
		if d.ready {
			w.previewAI(&d.s.win.Window, d.req)
		}
	})
	d.sent.SetSensitive(false)
	d.sent.SetVisible(d.summary != nil)
	d.copy = d.s.button("Copy", d.copyAll)
	d.copy.SetSensitive(false)
	closeBtn := d.s.button("Close", d.s.close)
	d.s.buttons([]*gtk.Button{d.sent, d.copy}, []*gtk.Button{closeBtn}, nil)
	closeBtn.GrabFocus()
	d.s.win.ConnectCloseRequest(func() bool {
		d.closed = true
		if d.call != nil {
			d.call.cancel()
		}
		if reviewShown == d {
			reviewShown = nil
		}
		return false
	})
	d.s.win.Present()
	return d
}

func (d *reviewDialog) textBox(height int, mono bool) *gtk.TextView {
	tv := gtk.NewTextView()
	tv.SetEditable(false)
	tv.SetCursorVisible(false)
	tv.SetMonospace(mono)
	tv.SetWrapMode(gtk.WrapWordChar)
	tv.SetTopMargin(8)
	tv.SetBottomMargin(8)
	tv.SetLeftMargin(10)
	tv.SetRightMargin(10)
	scroll := gtk.NewScrolledWindow()
	scroll.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	scroll.SetMinContentHeight(height)
	scroll.SetMaxContentHeight(height)
	scroll.SetChild(tv)
	frame := gtk.NewFrame("")
	frame.SetChild(scroll)
	d.s.body.Append(frame)
	return tv
}

func (d *reviewDialog) show(r review.Review, err error) {
	if d.closed {
		return
	}
	if err != nil {
		d.sub.SetText("")
		d.status.SetText("Couldn't read it: " + err.Error())
		d.status.AddCSSClass("error")
		return
	}
	d.review = &r
	if r.Tool == "" {
		d.sub.SetText("")
		d.facts.Buffer().SetText("This output isn't terraform plan, kubectl diff or helm diff output, so there's nothing to review.")
		return
	}
	sub := r.Tool + " · " + r.CountText
	if r.Plan != "" {
		sub += "\n" + r.Tool + " says: " + r.Plan
	}
	d.sub.SetText(sub)
	text := strings.Join(r.Lines, "\n")
	if r.NoChanges && len(r.Lines) == 0 {
		text = "No changes."
	}
	d.facts.Buffer().SetText(d.w.app.settings.text(text))
	d.copy.SetSensitive(true)
	if d.summary != nil && len(r.Lines) > 0 {
		d.summarize(r)
	}
}

func (d *reviewDialog) summarize(r review.Review) {
	d.status.SetText("Asking for a summary…  Close stops it.")
	svc := d.w.app.be.Review
	ask := review.Ask{Review: r, Env: d.env, Cwd: d.cwd}
	go func() {
		req, err := svc.Request(ask)
		onMainAsync(func() {
			if d.closed {
				return
			}
			if err != nil {
				d.status.SetText(err.Error())
				return
			}
			d.req, d.ready = req, true
			d.sent.SetSensitive(true)
			d.call = d.w.askAI(&d.s.win.Window, req, func(chunk string) {
				d.reply += chunk
				d.setSummary(d.reply)
			}, d.finish)
		})
	}()
}

func (d *reviewDialog) setSummary(text string) {
	buf := d.summary.Buffer()
	buf.SetText(d.w.app.settings.text(text))
	d.summary.ScrollToIter(buf.EndIter(), 0, false, 0, 1)
}

func (d *reviewDialog) finish(done ai.Done) {
	d.call = nil
	d.done = &done
	if d.closed {
		return
	}
	switch {
	case done.Cancelled && d.reply == "":
		d.status.SetText("No summary asked for.")
	case done.Cancelled:
		d.status.SetText("Stopped.")
	case done.Error != "":
		d.status.SetText("The AI couldn't answer: " + done.Error)
		d.status.AddCSSClass("error")
	default:
		d.reply = done.Text
		d.setSummary(strings.TrimSpace(done.Text))
		d.summary.ScrollToIter(d.summary.Buffer().StartIter(), 0, false, 0, 0)
		d.status.SetText(aiSummary(done))
	}
}

func (d *reviewDialog) summaryText() string {
	if d.summary == nil {
		return ""
	}
	buf := d.summary.Buffer()
	return buf.Text(buf.StartIter(), buf.EndIter(), false)
}

func (d *reviewDialog) copyAll() {
	if d.review == nil {
		return
	}
	text := d.review.Tool + ": " + d.review.CountText + "\n" + strings.Join(d.review.Lines, "\n")
	if s := d.summaryText(); s != "" {
		text += "\n\n" + s
	}
	d.s.win.Clipboard().SetText(d.w.app.settings.text(text))
	d.w.toast("Copied the review")
}

//go:build linux

package gtkui

import (
	"strings"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/dayanraj/wtf/internal/review"
)

// Change reviewer (#26): what a terraform plan, kubectl diff or helm diff
// (a command block or a saved plan file) will do, parsed by
// internal/review and listed as facts. Summarize with Claude asks the
// Claude pane (/wtf:review) for a plain-English summary. Nothing is
// applied. Like the macOS app's ReviewSheet.

// reviewDialog is one review. Main loop only.
type reviewDialog struct {
	w         *Window
	s         *toolsSheet
	sub       *gtk.Label
	facts     *gtk.TextView
	status    *gtk.Label
	copy      *gtk.Button
	summarize *gtk.Button

	// The shell it's about, and what /wtf:review gets.
	pane   int
	block  int
	file   string
	review *review.Review
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
	d.pane, d.block = p.id, id
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
	var env map[string]string
	if ps := w.model.ActivePane(); ps != nil {
		d.pane, env = ps.ID, ps.Env
	}
	d.file = path
	d.status.SetText("Reading the plan…")
	svc := w.app.be.Review
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
	d.facts = d.textBox(240, true)
	d.status = d.s.label("", "dim-label", "wtf-sheet-sub")
	d.s.body.Append(d.status)
	d.copy = d.s.button("Copy", d.copyAll)
	d.copy.SetSensitive(false)
	closeBtn := d.s.button("Close", d.s.close)
	d.summarize = d.s.button("Summarize with Claude", d.askClaude)
	d.summarize.AddCSSClass("suggested-action")
	d.summarize.SetSensitive(false)
	d.s.buttons([]*gtk.Button{d.copy}, []*gtk.Button{closeBtn, d.summarize}, nil)
	closeBtn.GrabFocus()
	d.s.win.ConnectCloseRequest(func() bool {
		d.closed = true
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
	d.summarize.SetSensitive(len(r.Lines) > 0)
}

// askClaude closes the dialog and asks the shell's Claude pane.
func (d *reviewDialog) askClaude() {
	w, pane, block, file := d.w, d.pane, d.block, d.file
	d.s.close()
	if _, ok := w.model.Panes[pane]; ok {
		w.model.FocusPane(pane)
	}
	w.claudeReview(block, file)
}

func (d *reviewDialog) copyAll() {
	if d.review == nil {
		return
	}
	text := d.review.Tool + ": " + d.review.CountText + "\n" + strings.Join(d.review.Lines, "\n")
	d.s.win.Clipboard().SetText(d.w.app.settings.text(text))
	d.w.toast("Copied the review")
}

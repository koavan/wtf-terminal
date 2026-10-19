//go:build linux

package gtkui

import (
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"

	"github.com/dayanraj/wtf/internal/gtkui/vte"
	"github.com/dayanraj/wtf/internal/terminal"
)

// Pane is one terminal: a VTE widget showing a backend shell session. Go
// reads the PTY and feeds VTE (feeder.go); keys come back through VTE's
// commit signal.
type Pane struct {
	win   *Window
	term  *vte.Terminal
	marks *marks
	feed  *feeder
	id    string // the backend session, once started
	sess  *terminal.Session
	// Self-checks watch the output; set before start.
	tap func([]byte)
}

func newPane(w *Window) *Pane {
	p := &Pane{win: w, term: vte.New()}
	p.term.SetFont("JetBrains Mono 11")
	p.term.SetColors("#d8dee9", "#1e2127")
	p.term.SetScrollback(10000)
	p.term.SetHExpand(true)
	p.term.SetVExpand(true)
	p.term.OnTitle(func(title string) {
		if title == "" {
			title = "wtf"
		}
		w.win.SetTitle(title)
	})
	p.marks = newMarks(p.term)
	p.feed = newFeeder(p.term, p.marks)
	p.term.OnGridChanged(func(cols, rows int) {
		p.marks.gridChanged(cols, rows)
		if p.id != "" {
			_ = p.win.app.be.Terminal.Resize(p.id, cols, rows)
		}
	})
	return p
}

// start runs a shell from internal/terminal in this pane.
func (p *Pane) start() error {
	svc := p.win.app.be.Terminal
	id, err := svc.Create(p.win.name, 80, 24, "")
	if err != nil {
		return err
	}
	sess, _, err := terminal.Attach(svc, id)
	if err != nil {
		svc.Close(id)
		return err
	}
	p.id, p.sess = id, sess
	p.term.OnCommit(func(text string) { _, _ = sess.Write([]byte(text)) })
	// The grid may have changed size before the shell started.
	_ = svc.Resize(id, p.term.Columns(), p.term.Rows())
	go func() {
		for chunk := range sess.Output {
			if p.tap != nil {
				p.tap(chunk)
			}
			p.feed.push(chunk)
		}
		<-sess.Done
		// The shell is gone: so is its window, for now (tabs come in M3).
		coreglib.IdleAdd(p.win.win.Close)
	}()
	return nil
}

// zoom makes the text bigger (+1), smaller (-1) or its default size (0).
func (p *Pane) zoom(step int) {
	switch step {
	case 0:
		p.term.SetFontScale(1)
	default:
		p.term.SetFontScale(max(0.5, min(3, p.term.FontScale()+0.1*float64(step))))
	}
}

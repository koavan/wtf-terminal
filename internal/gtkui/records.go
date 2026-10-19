//go:build linux

package gtkui

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/dayanraj/wtf/internal/uicore"
)

// windowRecords is what a window keeps in memory about its terminals: the
// commands run, pinned output, recordings and the files saved from it.
// Never on disk. Like the macOS app's WindowRecords.
type windowRecords struct {
	history uicore.History
	pins    uicore.Pins
	// Panes being recorded: where to, and since when.
	recording map[int]recordingInfo
	saved     []savedFile
	// The command block each pane is running (for pins that follow it).
	runningBlock map[int]int
	observers    []func()
}

type recordingInfo struct {
	path  string
	since time.Time
}

type savedKind string

const (
	savedRecording savedKind = "Recording"
	savedText      savedKind = "Output"
	savedMarkdown  savedKind = "Transcript"
)

type savedFile struct {
	path string
	kind savedKind
	at   time.Time
}

const maxSaved = 20

func newWindowRecords() *windowRecords {
	return &windowRecords{recording: map[int]recordingInfo{}, runningBlock: map[int]int{}}
}

// observe calls f whenever the records change, until off is called.
func (r *windowRecords) observe(f func()) (off func()) {
	r.observers = append(r.observers, f)
	i := len(r.observers) - 1
	return func() { r.observers[i] = nil }
}

func (r *windowRecords) changed() {
	for _, f := range slices.Clone(r.observers) {
		if f != nil {
			f()
		}
	}
}

func (r *windowRecords) remember(path string, kind savedKind) {
	r.saved = slices.DeleteFunc(r.saved, func(s savedFile) bool { return s.path == path })
	r.saved = append([]savedFile{{path, kind, time.Now()}}, r.saved...)
	if len(r.saved) > maxSaved {
		r.saved = r.saved[:maxSaved]
	}
	r.changed()
}

// label is a pane's tab title, for labels.
func (w *Window) label(pane int) string {
	if t, ok := w.model.TabOf(pane); ok {
		return w.model.Title(t)
	}
	return "terminal"
}

// showPane brings a pane to the front: its tab active, the pane focused.
func (w *Window) showPane(id int) {
	t, ok := w.model.TabOf(id)
	if !ok {
		return
	}
	w.model.Activate(t.ID)
	w.model.FocusPane(id)
	onMainAsync(w.focusActive)
}

// revealCommand scrolls a command's prompt into view; false once it has
// left the scrollback.
func (p *Pane) revealCommand(block int) bool {
	row, ok := p.tracker.PromptRow(block)
	if !ok || p.feed.alternate {
		return false
	}
	p.scrollToRow(row)
	p.over.setHighlight(block)
	time.AfterFunc(1200*time.Millisecond, func() { onMainAsync(p.over.clearHighlight) })
	return true
}

// Pins

// pinBlock pins a command's output (following it while it runs) and shows
// the Pinned tab.
func (w *Window) pinBlock(p *Pane, block int) {
	b, ok := p.tracker.Info(block)
	ps := w.model.Panes[p.id]
	if !ok || ps == nil {
		return
	}
	title := b.Text
	if title == "" {
		title = "Command output"
	}
	out, _ := p.tracker.Output(block)
	pin := uicore.Pin{Title: title, Text: out, PaneID: p.id, BlockID: &block, Cwd: ps.Cwd, Running: b.Running(), ExitCode: b.ExitCode}
	if !b.Running() {
		d := b.Duration
		pin.Duration = &d
	}
	w.records.pins.Add(pin)
	w.pinned()
}

func (w *Window) pinSelection(p *Pane, text string) {
	ps := w.model.Panes[p.id]
	if text == "" || ps == nil {
		return
	}
	w.records.pins.Add(uicore.Pin{Title: "Selection", Text: text, PaneID: p.id, Cwd: ps.Cwd})
	w.pinned()
}

func (w *Window) pinLastOutput() {
	p := w.active()
	if p == nil {
		return
	}
	id, ok := p.tracker.LastFinished()
	if !ok {
		w.toast("No finished command to pin yet")
		return
	}
	w.pinBlock(p, id)
}

func (w *Window) pinned() {
	w.records.changed()
	w.sidebarState.Tab, w.sidebarState.Open = uicore.SidebarPins, true
	w.layoutSidebar()
	w.focusActive()
}

// Recording

func (w *Window) isRecording(pane int) bool {
	_, ok := w.records.recording[pane]
	return ok
}

// toggleRecording starts or stops recording a pane (the focused one when
// p is nil).
func (w *Window) toggleRecording(p *Pane) {
	if p == nil {
		p = w.active()
	}
	if p == nil {
		return
	}
	if w.isRecording(p.id) {
		w.stopRecording(p.id)
	} else {
		w.startRecording(p)
	}
}

func (w *Window) startRecording(p *Pane) {
	ps := w.model.Panes[p.id]
	if p.session == "" || ps == nil {
		return
	}
	title := w.label(p.id)
	w.saveDialog("Record Terminal", uicore.ExportName(title, time.Now())+".cast", func(path string) {
		session, svc := p.session, w.app.be.Terminal
		go func() {
			err := svc.StartRecording(session, path, title)
			onMainAsync(func() {
				if err != nil {
					w.toast("Couldn't start recording: " + err.Error())
					return
				}
				w.records.recording[p.id] = recordingInfo{path, time.Now()}
				w.records.changed()
				w.recordingChanged()
				where := uicore.PrettyPath(path, w.app.home)
				if w.app.settings.hide {
					w.toast("Recording to " + where + " — output is recorded as is; Hide Secrets doesn't apply to it")
				} else {
					w.toast("Recording to " + where)
				}
			})
		}()
	})
}

func (w *Window) stopRecording(pane int) {
	rec, ok := w.records.recording[pane]
	if !ok {
		return
	}
	delete(w.records.recording, pane)
	w.records.changed()
	w.recordingChanged()
	ps := w.model.Panes[pane]
	if ps == nil || ps.SessionID == "" {
		return
	}
	session, svc := ps.SessionID, w.app.be.Terminal
	go func() {
		s, err := svc.StopRecording(session)
		onMainAsync(func() {
			if err != nil {
				w.toast("The recording ended with an error: " + err.Error())
				return
			}
			w.records.remember(rec.path, savedRecording)
			w.toast(fmt.Sprintf("Saved recording to %s (%s)", uicore.PrettyPath(rec.path, w.app.home),
				uicore.Duration(time.Duration(s.Seconds*float64(time.Second)))))
		})
	}()
}

// recordsPaneGone: a pane went away (its shell ended, or its tab moved to
// another window).
func (w *Window) recordsPaneGone(id int, moved bool) {
	w.records.history.Clear(id)
	w.records.pins.PaneGone(id)
	delete(w.records.runningBlock, id)
	if rec, ok := w.records.recording[id]; ok {
		delete(w.records.recording, id)
		if !moved {
			w.records.remember(rec.path, savedRecording)
			w.toast("Saved recording to " + uicore.PrettyPath(rec.path, w.app.home))
		}
		w.recordingChanged()
	}
	w.records.changed()
}

// syncRecording: a pane came from another window; is its shell recorded?
func (w *Window) syncRecording(id int) {
	ps := w.model.Panes[id]
	if ps == nil || ps.SessionID == "" {
		return
	}
	session, svc := ps.SessionID, w.app.be.Terminal
	go func() {
		s := svc.Recording(session)
		onMainAsync(func() {
			if s.Active {
				w.records.recording[id] = recordingInfo{s.Path, time.Now().Add(-time.Duration(s.Seconds * float64(time.Second)))}
			} else {
				delete(w.records.recording, id)
			}
			w.records.changed()
			w.recordingChanged()
		})
	}()
}

// recordingChanged updates the REC pill and the tabs' red dots, and runs
// the one-second clock only while something records.
func (w *Window) recordingChanged() {
	on := len(w.records.recording) > 0
	switch {
	case on && w.recClock == nil:
		w.recClock = newPoller(w.app, w, time.Second, func() {
			w.address.shape = ""
			w.address.refresh()
		})
	case !on && w.recClock != nil:
		w.recClock.stop()
		w.recClock = nil
	}
	w.strip.shape = ""
	w.strip.refresh()
	w.address.shape = ""
	w.address.refresh()
}

// Saving output

// saveDialog asks where to save, with a suggested name.
func (w *Window) saveDialog(title, name string, done func(path string)) {
	d := gtk.NewFileDialog()
	d.SetTitle(title)
	d.SetInitialName(name)
	d.Save(context.Background(), &w.win.Window, func(res gio.AsyncResulter) {
		f, err := d.SaveFinish(res)
		if err != nil || f == nil {
			return
		}
		if path := f.Path(); path != "" {
			done(path)
		}
	})
}

func (w *Window) saveOutputAsText(p *Pane) {
	if p == nil {
		p = w.active()
	}
	if p == nil {
		return
	}
	text := w.app.settings.text(p.plainText()) + "\n"
	w.saveDialog("Save Output as Text", uicore.ExportName(w.label(p.id), time.Now())+".txt", func(path string) {
		w.saveText(path, text, savedText, "Saved output to "+uicore.PrettyPath(path, w.app.home))
	})
}

func (w *Window) saveTranscript(p *Pane) {
	if p == nil {
		p = w.active()
	}
	ps := w.model.Panes[p.id]
	if p == nil || ps == nil {
		return
	}
	if !ps.Integrated {
		w.toast("A transcript needs shell integration (bash or zsh) — Save Output as Text works anywhere")
		return
	}
	blocks := p.tracker.Transcript()
	if len(blocks) == 0 {
		w.toast("No commands to save yet")
		return
	}
	title := w.label(p.id)
	md := w.app.settings.text(uicore.ExportMarkdown(blocks, title, uicore.PrettyPath(ps.Cwd, w.app.home), time.Now(), time.Local))
	w.saveDialog("Save Commands as Markdown", uicore.ExportName(title, time.Now())+".md", func(path string) {
		n := len(blocks)
		w.saveText(path, md, savedMarkdown, fmt.Sprintf("Saved %d %s to %s", n, plural(n, "command"), uicore.PrettyPath(path, w.app.home)))
	})
}

func (w *Window) saveText(path, text string, kind savedKind, done string) {
	svc := w.app.be.Files
	go func() {
		err := svc.SaveText(path, text)
		onMainAsync(func() {
			if err != nil {
				w.toast("Couldn't save: " + err.Error())
				return
			}
			w.records.remember(path, kind)
			w.toast(done)
		})
	}()
}

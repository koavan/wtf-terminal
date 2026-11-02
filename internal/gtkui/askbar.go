//go:build linux

package gtkui

import (
	"strings"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/dayanraj/wtf/internal/ai"
	"github.com/dayanraj/wtf/internal/suggest"
	"github.com/dayanraj/wtf/internal/uicore"
)

func init() {
	extraCSS = append(extraCSS, `
.wtf-ask-body { padding: 4px 8px 6px; }
.wtf-ask-command { font-weight: 600; }
`)
}

// Plain English to command (#25), Ctrl+Shift+I: a bar over the window where
// you describe a command ("delete evicted pods in all namespaces"). The
// quick AI gets it with the focused shell's facts (internal/suggest) and
// answers with one command and a sentence about it. Enter types the command
// at the prompt without running it; Explain Flags asks what each part does.
// Like the macOS app's AskBar. One per window.

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

type askBar struct {
	w                            *Window
	p                            *Pane
	panel                        *gtk.Box
	entry                        *gtk.Entry
	body                         *gtk.Box
	command, note, flags, status *gtk.Label
	sent, explainFlags, insert   *gtk.Button
	buttons                      *gtk.Box

	call *aiCall
	// What was asked, and the request that asked it (What's Sent, flags).
	asked      string
	req        ai.Request
	ready      bool
	reply      string
	suggestion *suggest.Suggestion
	flagsAsked bool
	// The last request's end (for the self-check).
	done *ai.Done
}

var askBars = map[*Window]*askBar{}

// toggleAskBar opens the bar for the focused pane, or closes it.
func (w *Window) toggleAskBar() {
	if b := askBars[w]; b != nil {
		b.dismiss()
		return
	}
	if !w.aiEnabled() {
		w.toast("Set up an AI provider in Settings first (Ctrl+,)")
		return
	}
	p := w.active()
	if ps := w.model.ActivePane(); p == nil || ps == nil || !ps.IsShell {
		w.toast("Focus a terminal pane first")
		return
	}
	if w.palette != nil {
		w.palette.dismiss()
	}
	b := &askBar{w: w, p: p}
	askBars[w] = b
	b.build()
	w.overlay.AddOverlay(b.panel)
	b.entry.GrabFocus()
}

func (b *askBar) build() {
	label := func(classes ...string) *gtk.Label {
		l := gtk.NewLabel("")
		l.SetXAlign(0)
		l.SetWrap(true)
		l.SetMaxWidthChars(1)
		l.SetHExpand(true)
		for _, c := range classes {
			l.AddCSSClass(c)
		}
		return l
	}
	b.entry = gtk.NewEntry()
	b.entry.SetHExpand(true)
	b.entry.SetPlaceholderText("Describe a command, e.g. delete evicted pods in all namespaces")
	b.entry.SetIconFromIconName(gtk.EntryIconPrimary, "starred-symbolic")
	b.command = label("wtf-tools-mono", "wtf-ask-command")
	b.command.SetSelectable(true)
	b.note = label()
	b.flags = label("wtf-tools-mono", "dim-label")
	b.flags.SetSelectable(true)
	b.status = label("dim-label", "wtf-sheet-sub")

	b.sent = gtk.NewButtonWithLabel("What's Sent")
	b.sent.ConnectClicked(func() {
		if b.ready {
			b.w.previewAI(nil, b.req)
		}
	})
	b.explainFlags = gtk.NewButtonWithLabel("Explain Flags")
	b.explainFlags.ConnectClicked(b.askFlags)
	b.insert = gtk.NewButtonWithLabel("Insert at Prompt")
	b.insert.AddCSSClass("suggested-action")
	b.insert.ConnectClicked(b.typeIt)
	row := gtk.NewBox(gtk.OrientationHorizontal, 8)
	b.buttons = row
	row.Append(b.sent)
	row.Append(b.explainFlags)
	spacer := gtk.NewBox(gtk.OrientationHorizontal, 0)
	spacer.SetHExpand(true)
	row.Append(spacer)
	row.Append(b.insert)
	b.body = gtk.NewBox(gtk.OrientationVertical, 8)
	b.body.AddCSSClass("wtf-ask-body")
	for _, wd := range []gtk.Widgetter{b.command, b.note, b.flags, b.status, row} {
		b.body.Append(wd)
	}

	b.panel = gtk.NewBox(gtk.OrientationVertical, 8)
	b.panel.AddCSSClass("wtf-palette")
	b.panel.SetHAlign(gtk.AlignCenter)
	b.panel.SetVAlign(gtk.AlignStart)
	b.panel.SetMarginTop(48)
	b.panel.SetSizeRequest(620, -1)
	b.panel.Append(b.entry)
	b.panel.Append(b.body)

	b.entry.ConnectActivate(b.enter)
	keys := gtk.NewEventControllerKey()
	keys.SetPropagationPhase(gtk.PhaseCapture)
	keys.ConnectKeyPressed(func(keyval, _ uint, _ gdk.ModifierType) bool {
		if keyval != gdk.KEY_Escape {
			return false
		}
		if b.call != nil {
			b.call.cancel()
		} else {
			b.dismiss()
		}
		return true
	})
	b.panel.AddController(keys)
	b.show("idle", "")
}

// enter asks, or types the answer when the request hasn't changed since.
func (b *askBar) enter() {
	text := strings.TrimSpace(b.entry.Text())
	if text == b.asked && b.call == nil && b.suggestion != nil && b.suggestion.Command != "" {
		b.typeIt()
	} else {
		b.ask()
	}
}

// show brings the labels and buttons in line with the state: idle, asking,
// answered or failed (with msg).
func (b *askBar) show(state, msg string) {
	has := b.suggestion != nil && b.suggestion.Command != ""
	switch state {
	case "idle":
		b.status.SetText("Enter asks the quick AI with this shell's folder and context. Nothing runs: the command is typed at the prompt for you.")
	case "asking":
		b.status.SetText("Asking…  Esc stops.")
	case "answered":
		if has {
			b.status.SetText("Enter types it at the prompt (not run) · edit the request and Enter to ask again" + msg)
		} else {
			b.status.SetText("No command for that" + msg)
		}
	default:
		b.status.SetText(msg)
	}
	b.command.SetVisible(has)
	b.note.SetVisible(b.note.Text() != "")
	b.flags.SetVisible(b.flags.Text() != "")
	b.insert.SetSensitive(has)
	b.explainFlags.SetSensitive(has && !b.flagsAsked && b.call == nil)
	b.sent.SetSensitive(b.ready)
	b.buttons.SetVisible(state != "idle")
}

func (b *askBar) ask() {
	text := strings.TrimSpace(b.entry.Text())
	if text == "" || b.call != nil {
		return
	}
	b.asked, b.suggestion, b.reply, b.flagsAsked, b.done = text, nil, "", false, nil
	b.command.SetText("")
	b.note.SetText("")
	b.flags.SetText("")
	b.show("asking", "")
	a := suggest.Ask{Text: text}
	if ps := b.w.model.Panes[b.p.id]; ps != nil {
		a.Env, a.Cwd = ps.Env, ps.Cwd
	}
	svc := b.w.app.be.Suggest
	go func() {
		r, err := svc.Request(a)
		onMainAsync(func() {
			if askBars[b.w] != b {
				return
			}
			if err != nil {
				b.show("failed", err.Error())
				return
			}
			b.req, b.ready = r, true
			b.send(r, false)
		})
	}()
}

func (b *askBar) send(r ai.Request, flags bool) {
	var streamed string
	target := b.note
	if flags {
		target = b.flags
	}
	b.call = b.w.askAI(nil, r, func(chunk string) {
		streamed += chunk
		target.SetText(b.w.app.settings.text(streamed))
		b.show("asking", "")
	}, func(d ai.Done) { b.finish(d, flags) })
	b.show("asking", "")
}

func (b *askBar) finish(d ai.Done, flags bool) {
	b.call = nil
	b.done = &d
	if askBars[b.w] != b {
		return
	}
	switch {
	case d.Cancelled:
		if flags {
			b.flagsAsked = false
			b.flags.SetText("")
		}
		if b.suggestion == nil {
			b.show("idle", "")
		} else {
			b.show("answered", "")
		}
		return
	case d.Error != "":
		if flags {
			b.flagsAsked = false
		}
		b.show("failed", "The AI couldn't answer: "+d.Error)
		return
	case flags:
		b.flags.SetText(b.w.app.settings.text(strings.TrimSpace(d.Text)))
		b.show("answered", "")
		return
	}
	b.reply = d.Text
	s := b.w.app.be.Suggest.Parse(d.Text)
	b.suggestion = &s
	b.command.SetText("$ " + s.Command)
	b.note.SetText(b.w.app.settings.text(s.Note))
	b.show("answered", " · "+aiSummary(d))
}

func (b *askBar) askFlags() {
	if !b.ready || b.suggestion == nil || b.suggestion.Command == "" || b.flagsAsked || b.call != nil {
		return
	}
	b.flagsAsked = true
	b.send(b.w.app.be.Suggest.FlagsRequest(b.req, b.reply), true)
}

func (b *askBar) typeIt() {
	if b.suggestion == nil || b.suggestion.Command == "" {
		return
	}
	switch b.p.typeAtPrompt(b.suggestion.Command) {
	case shellBusy:
		b.w.toast("The shell is busy — finish the running program first")
	case copiedInstead:
		b.dismiss()
		b.w.toast("The command has several lines and this shell would run them as typed: copied instead")
	default:
		b.dismiss()
	}
}

func (b *askBar) dismiss() {
	if askBars[b.w] != b {
		return
	}
	delete(askBars, b.w)
	if b.call != nil {
		b.call.cancel()
	}
	b.w.overlay.RemoveOverlay(b.panel)
	b.w.focusActive()
}

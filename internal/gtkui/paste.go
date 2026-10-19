//go:build linux

package gtkui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/dayanraj/wtf/internal/uicore"
)

// paste pastes the clipboard (Ctrl+Shift+V, the menu).
func (p *Pane) paste() { p.pasteFrom(p.term.Clipboard()) }

// pasteFrom reads a clipboard (CLIPBOARD, or PRIMARY for a middle click)
// and pastes it through the paste checks.
func (p *Pane) pasteFrom(cb *gdk.Clipboard) {
	cb.ReadTextAsync(context.Background(), func(res gio.AsyncResulter) {
		text, err := cb.ReadTextFinish(res)
		if err != nil || text == "" {
			return
		}
		p.pasteText(text)
	})
}

// pasteText pastes text, first showing it when it has more than one line,
// a final newline or control characters (and confirm-paste is on).
func (p *Pane) pasteText(text string) {
	if p.win.app.settings.config().ConfirmPaste && uicore.NewPasteFacts(text).NeedsConfirm() {
		p.confirmPaste(text)
		return
	}
	p.term.PasteText(text)
}

// confirmPaste shows a paste as it would arrive before anything reaches
// the shell. Like PasteConfirm.svelte and the macOS app's PasteConfirm.
func (p *Pane) confirmPaste(text string) {
	f := uicore.NewPasteFacts(text)
	heading := "Paste with a newline?"
	switch {
	case f.Lines > 1:
		heading = fmt.Sprintf("Paste %d lines?", f.Lines)
	case f.Controls > 0:
		heading = "Paste text with control characters?"
	}
	var has []string
	if f.Controls > 0 {
		has = append(has, fmt.Sprintf("%d control %s (shown as ␛ and the like)", f.Controls, plural(f.Controls, "character")))
	}
	if f.FinalNewline {
		has = append(has, "a newline at the end")
	}
	var body []string
	if len(has) > 0 {
		body = append(body, "It has "+strings.Join(has, " and ")+".")
	}
	if p.feed.bracketed {
		body = append(body, "The program here takes it as one paste and runs nothing until you press Enter.")
	} else {
		body = append(body, "The program here hasn’t asked for bracketed paste: a shell runs each line as it arrives.")
	}

	d := adw.NewMessageDialog(&p.win.win.Window, heading, strings.Join(body, " "))
	// The text as it would arrive, control characters made visible.
	view := gtk.NewTextView()
	view.SetEditable(false)
	view.SetMonospace(true)
	view.SetWrapMode(gtk.WrapWordChar)
	view.SetTopMargin(6)
	view.SetBottomMargin(6)
	view.SetLeftMargin(6)
	view.SetRightMargin(6)
	view.Buffer().SetText(uicore.ShowControls(p.win.app.settings.text(text)))
	scroll := gtk.NewScrolledWindow()
	scroll.SetChild(view)
	scroll.SetMinContentHeight(min(320, f.Lines*18+24))
	scroll.SetMinContentWidth(520)
	scroll.SetPropagateNaturalHeight(true)
	d.SetExtraChild(scroll)

	d.AddResponse("cancel", "Cancel")
	d.AddResponse("never", "Don’t Ask Again")
	if f.FinalNewline {
		d.AddResponse("trim", "Paste Without Final Newline")
	}
	d.AddResponse("paste", "Paste")
	d.SetResponseAppearance("paste", adw.ResponseSuggested)
	d.SetDefaultResponse("paste")
	d.SetCloseResponse("cancel")
	d.ConnectResponse(func(r string) {
		p.pasteDialog = nil
		switch r {
		case "paste":
			p.term.PasteText(text)
		case "trim":
			p.term.PasteText(uicore.WithoutFinalNewline(text))
		case "never":
			p.term.PasteText(text)
			go func() {
				err := p.win.app.be.Config.SetConfirmPaste(false)
				coreglib.IdleAdd(func() {
					if err != nil {
						p.win.toast("Couldn't save the setting: " + err.Error())
					} else {
						p.win.toast("Pastes go straight in now. Set confirm-paste in config.toml to change it")
					}
				})
			}()
		}
		p.term.GrabFocus()
	})
	p.win.app.debugLog("paste confirm shown: %s", heading)
	p.pasteDialog = d
	d.Present()
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// copySecret copies a secret wtf revealed (a Kubernetes secret's value, a
// decoded token, a generated password) and empties the clipboard after
// clipboard-clear seconds, unless something else was copied meanwhile.
// Like copySecret in lib/clipboard.ts.
func (w *Window) copySecret(text, what string) {
	cb := w.win.Clipboard()
	cb.SetText(text)
	seconds := w.app.settings.config().ClipboardClear
	if seconds <= 0 {
		w.toast("Copied " + what)
		return
	}
	// Any other copy, here or in another app, changes the clipboard, and
	// then it isn't ours to clear (like the macOS change count).
	ours := true
	handle := cb.ConnectChanged(func() { ours = false })
	time.AfterFunc(time.Duration(seconds*float64(time.Second)), func() {
		coreglib.IdleAdd(func() {
			cb.HandlerDisconnect(handle)
			if ours && cb.IsLocal() {
				cb.SetText("")
			}
		})
	})
	s := int(seconds)
	w.toast(fmt.Sprintf("Copied %s. The clipboard clears in %d %s", what, s, plural(s, "second")))
}

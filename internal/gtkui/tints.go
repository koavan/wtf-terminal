//go:build linux

package gtkui

import (
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"

	"github.com/dayanraj/wtf/internal/gtkui/vte"
	"github.com/dayanraj/wtf/internal/uicore"
)

// Log colors: levels, timestamps, URLs, addresses and Kubernetes names on
// screen take the theme's colors, blended over the text (the buffer isn't
// touched, so copies and recordings keep the program's output as is). Only
// what's on screen, only text no program colored, never in full-screen
// programs. Like the macOS app's TintView.

// scheduleTints colors what's on screen 120ms after output, a scroll or a
// resize: one scan however much arrives meanwhile, none while
// log-highlighting is off.
func (p *Pane) scheduleTints() {
	if !p.win.app.settings.config().LogHighlighting {
		if p.tinted {
			p.tinted = false
			p.term.SetTints(nil, false)
		}
		return
	}
	if p.tintTimer != 0 {
		return
	}
	p.tintTimer = coreglib.TimeoutAdd(120, func() bool {
		p.tintTimer = 0
		p.updateTints()
		return false
	})
}

func (p *Pane) updateTints() {
	top, ok := p.top()
	// A hidden pane is colored when it shows again.
	if !ok || !p.term.Mapped() || p.feed.alternate {
		if p.tinted {
			p.tinted = false
			p.term.SetTints(nil, false)
		}
		return
	}
	th := p.win.app.settings.theme()
	colors := uicore.NewLogColors(th.Palette[:], th.Foreground, th.Background)
	runs := uicore.LogTints(p.term, top, p.term.Rows(), colors)
	cw, ch := p.term.CharSize()
	tints := make([]vte.Tint, 0, len(runs))
	for _, r := range runs {
		c, ok := uicore.ParseRGB(r.Color)
		if !ok {
			continue
		}
		// In VTE's own drawing, which starts inside its padding.
		tints = append(tints, vte.Tint{
			X: float32(r.Col * cw), Y: float32(r.Row * ch), W: float32(r.Width * cw), H: float32(ch),
			R: float32(c.R / 255), G: float32(c.G / 255), B: float32(c.B / 255),
		})
	}
	if len(tints) == 0 && !p.tinted {
		return
	}
	p.tinted = len(tints) > 0
	// On a dark theme the darker of text and tint shows; on a light one the lighter.
	p.term.SetTints(tints, !th.Dark)
}

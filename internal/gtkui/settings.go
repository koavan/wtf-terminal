//go:build linux

package gtkui

import (
	"fmt"
	"math"
	"strings"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/dayanraj/wtf/internal/config"
	"github.com/dayanraj/wtf/internal/uicore"
)

// settings is the config from the backend, the desktop's dark preference,
// the text size and Hide Secrets, for every terminal. Like the macOS app's
// Settings and Redaction.
type settings struct {
	app      *App
	resolved config.Resolved
	// The size in use: config.toml's, until changed with Bigger/Smaller Text.
	fontSize float64
	// Hide Secrets: secrets are covered on screen and replaced when copied.
	hide      bool
	style     *adw.StyleManager
	observers []func()
	problems  []string
}

const (
	minFontSize = 6.0
	maxFontSize = 72.0
)

// The left padding is the gutter for command marks, as in the other builds.
const padTop, padRight, padBottom, padLeft = 10, 6, 6, 14

func newSettings(a *App) *settings {
	s := &settings{app: a, style: adw.StyleManagerGetDefault()}
	s.apply(a.be.Config.Current(), true)
	a.events.on(config.EventChanged, func(data any) {
		if r, ok := data.(config.Resolved); ok {
			s.apply(r, false)
		}
	})
	s.style.NotifyProperty("dark", s.changed)

	css := gtk.NewCSSProvider()
	css.LoadFromData(fmt.Sprintf(`
vte-terminal { padding: %dpx %dpx %dpx %dpx; }
.find-bar { background: alpha(@popover_bg_color, 0.97); border-radius: 8px; padding: 4px 4px 4px 8px;
  box-shadow: 0 2px 8px alpha(black, 0.25); }
.find-bar label.dim-label { font-size: smaller; }
.paste-preview { font-family: monospace; }
`, padTop, padRight, padBottom, padLeft))
	gtk.StyleContextAddProviderForDisplay(gdk.DisplayGetDefault(), css, gtk.STYLE_PROVIDER_PRIORITY_APPLICATION)
	return s
}

func (s *settings) apply(r config.Resolved, initial bool) {
	sizeChanged := !initial && r.Config.FontSize != s.resolved.Config.FontSize
	switch {
	case initial:
		s.hide = r.Config.RedactSecrets
	case r.Config.RedactSecrets && !s.resolved.Config.RedactSecrets:
		s.hide = true
	}
	s.resolved = r
	if initial || sizeChanged {
		s.fontSize = r.Config.FontSize
	}
	if !initial && strings.Join(r.Problems, "\n") != strings.Join(s.problems, "\n") {
		s.problems = r.Problems
		if len(r.Problems) > 0 {
			msg := "Settings: " + r.Problems[0]
			if n := len(r.Problems) - 1; n > 0 {
				msg += fmt.Sprintf(" (+%d more)", n)
			}
			s.app.toast(msg)
		}
	}
	s.changed()
}

func (s *settings) config() config.Config { return s.resolved.Config }

// theme is the dark or light theme, as the desktop prefers.
func (s *settings) theme() config.Theme {
	if s.style.Dark() {
		return s.resolved.Dark
	}
	return s.resolved.Light
}

// observe calls f whenever the config, theme, text size or Hide Secrets
// changes. Panes remove theirs when they close.
func (s *settings) observe(f func()) (remove func()) {
	s.observers = append(s.observers, f)
	i := len(s.observers) - 1
	return func() { s.observers[i] = nil }
}

func (s *settings) changed() {
	for _, f := range s.observers {
		if f != nil {
			f()
		}
	}
}

func (s *settings) setFontSize(size float64) {
	if math.IsNaN(size) || math.IsInf(size, 0) {
		return
	}
	s.fontSize = min(maxFontSize, max(minFontSize, math.Round(size)))
	s.changed()
}

func (s *settings) setHide(on bool) {
	if on != s.hide {
		s.hide = on
		s.changed()
	}
}

// text is text as it may be shown or copied now.
func (s *settings) text(t string) string {
	if s.hide && t != "" {
		return uicore.Redact(t)
	}
	return t
}

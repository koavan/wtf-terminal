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
	// Styles that follow the theme: group colors, the dimmed panes.
	themeCSS *gtk.CSSProvider
	// A theme shown while browsing themes in the palette.
	previewing *config.Theme
}

const (
	minFontSize = 6.0
	maxFontSize = 72.0
)

// extraCSS are styles side-pane tabs add from their files (in init).
var extraCSS []string

// The left padding is the gutter for command marks, as in the other builds.
const padTop, padRight, padBottom, padLeft = 10, 6, 6, 14

func newSettings(a *App) *settings {
	s := &settings{app: a, style: adw.StyleManagerGetDefault(), themeCSS: gtk.NewCSSProvider()}
	gtk.StyleContextAddProviderForDisplay(gdk.DisplayGetDefault(), s.themeCSS, gtk.STYLE_PROVIDER_PRIORITY_APPLICATION)
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
.wtf-bar { padding: 5px 6px 5px 6px; min-height: 36px; }
.wtf-tabs { padding: 0 2px; }
.wtf-tab { padding: 0 2px 0 10px; border-radius: 7px; min-height: 30px; }
.wtf-tab:hover { background: alpha(currentColor, 0.06); }
.wtf-tab.active { background: alpha(currentColor, 0.13); }
.wtf-tab label { font-size: 0.95em; }
.wtf-tab-close { min-width: 22px; min-height: 22px; padding: 0; opacity: 0.55; }
.wtf-tab-close:hover { opacity: 1; }
.wtf-tab-status { font-size: 0.8em; }
.wtf-chip { min-height: 22px; padding: 0 9px; border-radius: 11px; font-weight: bold; font-size: 0.85em; }
.wtf-chip.empty { min-width: 14px; padding: 0; }
.wtf-palette { background: @popover_bg_color; border-radius: 14px; padding: 8px;
  box-shadow: 0 6px 24px alpha(black, 0.3), 0 0 0 1px alpha(currentColor, 0.08); }
.wtf-palette-list { background: transparent; }
.wtf-palette-row { padding: 6px 8px; }
.wtf-palette-heading { font-size: 0.8em; font-weight: bold; opacity: 0.6; padding: 8px 8px 2px; }
.wtf-palette-key { font-size: 0.85em; }
.wtf-broadcast { background: alpha(#f5a524, 0.22); border-radius: 8px; padding: 4px 6px 4px 10px; margin: 0 8px 4px; }
.wtf-broadcast image { color: #f5a524; }
.wtf-broadcast button { background: #f5a524; color: black; }
.wtf-broadcasting { border: 2px solid #f5a524; border-radius: 6px; margin: 2px; }
.wtf-tab-broadcast { color: #f5a524; }
.wtf-address { padding: 0 8px 6px; }
.wtf-omnibox { background: alpha(currentColor, 0.05); border-radius: 8px; min-height: 28px; padding: 0 6px 0 8px; }
.wtf-pill { border-radius: 6px; padding: 1px 7px; font-size: 0.88em; min-height: 20px; }
.wtf-pill-shell, .wtf-pill-group, .wtf-badge { background: alpha(currentColor, 0.08); font-weight: 600; }
.wtf-badge { font-weight: 500; }
.wtf-badge-kind { font-size: 0.8em; font-weight: bold; opacity: 0.6; }
.wtf-crumbs { font-size: 0.95em; }
.wtf-dot { min-width: 7px; min-height: 7px; border-radius: 4px; }
.wtf-clock { background: alpha(currentColor, 0.12); padding: 0 4px; border-radius: 4px; }
.wtf-clock-soon { background: alpha(#e5b000, 0.3); padding: 0 4px; border-radius: 4px; }
.wtf-pill-button { padding: 0; min-height: 0; }
.wtf-pill-running { font-family: monospace; }
.wtf-overview { background: @window_bg_color; padding: 24px 32px; }
.wtf-card { background: alpha(currentColor, 0.05); border-radius: 10px; padding: 10px 12px;
  border: 1px solid alpha(currentColor, 0.08); }
flowboxchild:selected .wtf-card { border: 2px solid @accent_color; }
flowboxchild { padding: 0; background: none; }
.wtf-card-title { font-weight: bold; }
.wtf-rail { padding: 6px 4px; }
.wtf-rail-button { min-width: 32px; min-height: 32px; padding: 0; border-radius: 7px; }
.wtf-rail-button.active { background: alpha(@accent_color, 0.16); color: @accent_color; }
.wtf-side-content { min-width: 160px; }
.wtf-pane-header { padding: 4px 4px 2px 10px; min-height: 28px; }
.wtf-pane-title { font-size: 0.82em; font-weight: bold; }
.wtf-tool { min-width: 24px; min-height: 24px; padding: 0; }
.wtf-side-list { background: transparent; }
.wtf-side-list row { padding: 0; border-radius: 6px; margin: 0 4px; }
.wtf-side-row { padding: 3px 6px; min-height: 22px; }
.wtf-side-row.highlighted { background: alpha(@accent_color, 0.12); border-radius: 6px; }
.wtf-side-title { font-size: 0.95em; }
.wtf-side-detail { font-size: 0.82em; }
.wtf-side-heading { font-size: 0.78em; font-weight: bold; opacity: 0.7; padding: 8px 10px 2px; }
.wtf-side-group { font-size: 0.72em; opacity: 0.5; padding: 10px 10px 0; }
.wtf-side-note { opacity: 0.6; padding: 12px; font-size: 0.9em; }
.wtf-side-pill { font-size: 0.75em; background: alpha(currentColor, 0.08); }
.wtf-side-letter { font-family: monospace; font-weight: bold; font-size: 0.85em; min-width: 12px; }
.wtf-side-icon { opacity: 0.75; }
.wtf-side-button { min-width: 22px; min-height: 22px; padding: 0; }
.wtf-git-bar { padding: 0 10px; font-size: 0.85em; }
.wtf-git-button { font-size: 0.85em; font-weight: bold; padding: 0 6px; min-height: 20px; }
.wtf-side-filter { margin: 0 8px; }
.wtf-tool.active { color: @accent_color; }
.wtf-card-preview { font-family: monospace; font-size: 0.75em; opacity: 0.8; }
`, padTop, padRight, padBottom, padLeft) + strings.Join(extraCSS, "\n"))
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
	// A saved theme replaces the one previewed.
	s.previewing = nil
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
	if s.previewing != nil {
		return *s.previewing
	}
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
	s.loadThemeCSS()
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

// preview shows a theme everywhere until it's saved or the palette closes.
func (s *settings) preview(t *config.Theme) {
	if t != s.previewing {
		s.previewing = t
		s.changed()
	}
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

// loadThemeCSS sets the styles that depend on the theme and on dark or
// light: group colors (as on macOS), status colors, the veil over
// unfocused split panes.
func (s *settings) loadThemeCSS() {
	th := s.theme()
	dark := th.Dark
	var b strings.Builder
	for _, c := range uicore.GroupColors {
		hex := c.Hex(dark)
		text := "#ffffff"
		if dark {
			text = "#1d1f24"
		}
		fmt.Fprintf(&b, ".wtf-tab.group-%[1]s { box-shadow: inset 0 -2px %[2]s; }\n"+
			".wtf-chip.group-%[1]s { background: %[2]s; color: %[3]s; }\n", c, hex, text)
	}
	ok, fail := "#2f9e6a", "#d93036"
	if dark {
		ok, fail = "#5bb98c", "#e5484d"
	}
	accent := uicore.Accent(th.Cursor, th.Background, th.Palette[:], dark)
	fmt.Fprintf(&b, ".wtf-tab-status.ok { color: %s; } .wtf-tab-status.fail { color: %s; } .wtf-tab-status.activity { color: %s; }\n",
		ok, fail, accent.Hex())
	fmt.Fprintf(&b, ".wtf-dim { background-color: %s; }\n", th.Background)
	// Git status, as the macOS app colors it.
	gm, ga, gd := "#c98a00", "#188038", "#d93036"
	if dark {
		gm, ga, gd = "#fdd663", "#81c995", "#e5484d"
	}
	fmt.Fprintf(&b, ".git-modified { color: %[1]s; } .git-added { color: %[2]s; } .git-deleted { color: %[3]s; }\n"+
		".dot-git-modified { background: %[1]s; } .dot-git-added { background: %[2]s; } .dot-git-deleted { background: %[3]s; }\n", gm, ga, gd)
	for _, c := range uicore.GroupColors {
		fmt.Fprintf(&b, ".group-dot-%s { background: %s; }\n", c, c.Hex(dark))
	}
	fmt.Fprintf(&b, ".wtf-pill-busy, .wtf-pill-running { background: alpha(%[1]s, 0.16); color: %[1]s; font-weight: 600; }\n"+
		".wtf-running-dot { background: %[1]s; min-width: 6px; min-height: 6px; }\n"+
		".wtf-pill-ok { background: alpha(%[2]s, 0.16); color: %[2]s; }\n"+
		".wtf-pill-fail, .wtf-clock-expired { background: alpha(%[3]s, 0.16); color: %[3]s; }\n", accent.Hex(), ok, fail)
	s.themeCSS.LoadFromData(b.String())
}

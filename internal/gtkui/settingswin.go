//go:build linux

package gtkui

import (
	"slices"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/dayanraj/wtf/internal/config"
	"github.com/dayanraj/wtf/internal/notify"
)

// The Settings window: General (theme, config.toml, the themes folder, Ask
// Claude's auto mode). One per app. The other settings are in config.toml
// for now. Like the macOS app's SettingsWindow.

// settingsWindow is the open Settings window. Main loop only.
type settingsWindow struct {
	// The terminal window it was opened from, for the palette (Change Theme).
	w     *Window
	win   *adw.PreferencesWindow
	theme *adw.ActionRow
	off   func()
	gone  bool
}

var settingsWindows = map[*App]*settingsWindow{}

// showSettings opens Settings, or brings it to the front.
func (w *Window) showSettings() {
	s := settingsWindows[w.app]
	if s == nil {
		s = newSettingsWindow(w)
		settingsWindows[w.app] = s
	}
	s.win.Present()
}

func newSettingsWindow(w *Window) *settingsWindow {
	s := &settingsWindow{w: w}
	s.win = adw.NewPreferencesWindow()
	s.win.SetApplication(&w.app.gtk.Application)
	s.win.SetTransientFor(&w.win.Window)
	s.win.SetDestroyWithParent(true)
	s.win.SetTitle("Settings")
	s.win.SetDefaultSize(720, 560)
	s.win.SetSearchEnabled(false)
	s.win.Add(s.generalPage())

	keys := gtk.NewEventControllerKey()
	keys.ConnectKeyPressed(func(keyval, _ uint, _ gdk.ModifierType) bool {
		if keyval != gdk.KEY_Escape {
			return false
		}
		s.win.Close()
		return true
	})
	s.win.AddController(keys)
	s.off = w.app.events.on(config.EventChanged, func(any) {
		s.theme.SetSubtitle(s.w.app.settings.config().Theme)
	})
	s.win.ConnectCloseRequest(func() bool {
		s.closed()
		return false
	})
	s.win.ConnectDestroy(s.closed)
	return s
}

// closed forgets the window.
func (s *settingsWindow) closed() {
	if s.gone {
		return
	}
	s.gone = true
	s.off()
	if settingsWindows[s.w.app] == s {
		delete(settingsWindows, s.w.app)
	}
}

// General

func (s *settingsWindow) generalPage() *adw.PreferencesPage {
	page := adw.NewPreferencesPage()
	page.SetName("general")
	page.SetTitle("General")
	page.SetIconName("preferences-system-symbolic")
	button := func(label string, run func()) *gtk.Button {
		b := gtk.NewButtonWithLabel(label)
		b.SetVAlign(gtk.AlignCenter)
		b.ConnectClicked(run)
		return b
	}
	look := adw.NewPreferencesGroup()
	look.SetTitle("Appearance")
	s.theme = adw.NewActionRow()
	s.theme.SetUseMarkup(false)
	s.theme.SetTitle("Theme")
	s.theme.SetSubtitle(s.w.app.settings.config().Theme)
	s.theme.AddSuffix(button("Change Theme…", func() {
		s.w.win.Present()
		s.w.togglePalette(paletteThemes)
	}))
	look.Add(s.theme)
	page.Add(look)

	files := adw.NewPreferencesGroup()
	files.SetTitle("Files")
	files.SetDescription("Font, scrollback, badges and the other settings are in config.toml for now. Changes apply as soon as it's saved.")
	cfg := adw.NewActionRow()
	cfg.SetUseMarkup(false)
	cfg.SetTitle("config.toml")
	cfg.SetSubtitle("Every setting, with notes on each")
	cfg.AddSuffix(button("Open config.toml", s.w.openConfigFile))
	files.Add(cfg)
	themes := adw.NewActionRow()
	themes.SetTitle("Themes folder")
	themes.SetSubtitle("Your own themes: Ghostty or iTerm2 color files")
	themes.AddSuffix(button("Open Themes Folder", func() { s.w.win.ActivateAction("themes.folder", nil) }))
	files.Add(themes)
	page.Add(files)

	claude := adw.NewPreferencesGroup()
	claude.SetTitle("Ask Claude")
	claude.SetDescription("Ask Claude (Ctrl+Shift+I) runs your own Claude Code in a split beside the shell, with wtf's tools for that shell. " +
		"wtf makes Claude Code ask before every command it runs or types in your shells and every file it changes, even in its auto mode or after \u201cdon't ask again\u201d.")
	auto := adw.NewSwitchRow()
	auto.SetUseMarkup(false)
	auto.SetTitle("Let Claude Code's own permission mode decide instead")
	auto.SetSubtitle("claude-auto-mode; applies to Claude panes opened from now on")
	auto.SetActive(s.w.app.settings.config().ClaudeAutoMode)
	auto.Connect("notify::active", func() {
		on := auto.Active()
		if on == s.w.app.settings.config().ClaudeAutoMode {
			return
		}
		if err := s.w.app.be.Config.SetClaudeAutoMode(on); err != nil {
			auto.SetActive(!on)
			s.w.toast("Couldn't change the setting: " + err.Error())
		}
	})
	claude.Add(auto)
	agents := adw.NewSwitchRow()
	agents.SetUseMarkup(false)
	agents.SetTitle("Show every claude you start in the Agents tab")
	agents.SetSubtitle("agent-status; wtf's shell integration adds hooks that report what it's doing, for that run only (nothing is written to ~/.claude); applies to new panes")
	agents.SetActive(s.w.app.settings.config().AgentStatus)
	agents.Connect("notify::active", func() {
		on := agents.Active()
		if on == s.w.app.settings.config().AgentStatus {
			return
		}
		if err := s.w.app.be.Config.SetAgentStatus(on); err != nil {
			agents.SetActive(!on)
			s.w.toast("Couldn't change the setting: " + err.Error())
		}
	})
	claude.Add(agents)
	sound := adw.NewSwitchRow()
	sound.SetUseMarkup(false)
	sound.SetTitle("Play a sound when an agent needs you or finishes")
	sound.SetSubtitle("agent-sound; with its notification, a different sound for each (your desktop's sound theme)")
	sound.SetActive(s.w.app.settings.config().AgentSound)
	sound.Connect("notify::active", func() {
		on := sound.Active()
		if on == s.w.app.settings.config().AgentSound {
			return
		}
		if err := s.w.app.be.Config.SetAgentSound(on); err != nil {
			sound.SetActive(!on)
			s.w.toast("Couldn't change the setting: " + err.Error())
			return
		}
		// Turned on: what it sounds like.
		if on {
			s.w.app.be.Notify.PlaySound(notify.SoundAttention)
		}
	})
	claude.Add(sound)
	group := adw.NewSwitchRow()
	group.SetUseMarkup(false)
	group.SetTitle("Group tabs running Claude Code under “agents”")
	group.SetSubtitle("agent-group; a tab joins its window's agents tab group when Claude Code starts in it, and leaves when it ends")
	group.SetActive(s.w.app.settings.config().AgentGroup)
	group.Connect("notify::active", func() {
		on := group.Active()
		if on == s.w.app.settings.config().AgentGroup {
			return
		}
		if err := s.w.app.be.Config.SetAgentGroup(on); err != nil {
			group.SetActive(!on)
			s.w.toast("Couldn't change the setting: " + err.Error())
		}
	})
	claude.Add(group)
	modes := []string{"ask", "background", "end"}
	closing := adw.NewComboRow()
	closing.SetUseMarkup(false)
	closing.SetTitle("Closing a tab running Claude Code")
	closing.SetSubtitle("agent-close; background keeps it running (the Agents tab opens it again) until wtf quits")
	closing.SetModel(gtk.NewStringList([]string{"Ask", "Keep running in background", "End it"}))
	closing.SetSelected(uint(max(0, slices.Index(modes, s.w.app.settings.config().AgentClose))))
	closing.Connect("notify::selected", func() {
		i := int(closing.Selected())
		if i < 0 || i >= len(modes) || modes[i] == s.w.app.settings.config().AgentClose {
			return
		}
		if err := s.w.app.be.Config.SetAgentClose(modes[i]); err != nil {
			s.w.toast("Couldn't change the setting: " + err.Error())
		}
	})
	claude.Add(closing)
	page.Add(claude)
	return page
}

// openConfigFile opens config.toml in the text editor.
func (w *Window) openConfigFile() {
	cfg := w.app.be.Config
	go func() {
		if err := cfg.OpenConfigFile(); err != nil {
			onMainAsync(func() { w.toast("Couldn't open settings: " + err.Error()) })
		}
	}()
}

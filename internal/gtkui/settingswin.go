//go:build linux

package gtkui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"

	"github.com/dayanraj/wtf/internal/ai"
	"github.com/dayanraj/wtf/internal/config"
)

func init() {
	extraCSS = append(extraCSS, `
.wtf-ai-marker { font-size: 0.8em; font-weight: bold; border-radius: 6px; padding: 0 6px;
  background: alpha(@accent_color, 0.15); color: @accent_color; }
.wtf-ai-reply { background: alpha(currentColor, 0.05); border-radius: 6px; padding: 6px 8px; }
.wtf-settings-row { padding: 10px 12px; }
`)
}

// The Settings window: AI (providers: adding, editing, keys, which answers
// what, testing) and General (theme, config.toml, the themes folder). One
// per app. The other settings are in config.toml for now. Like the macOS
// app's SettingsWindow.

// settingsWindow is the open Settings window. Main loop only.
type settingsWindow struct {
	// The terminal window it was opened from: the focused shell's PATH for
	// command providers, and the palette for Change Theme.
	w      *Window
	win    *adw.PreferencesWindow
	aiPage *adw.PreferencesPage
	groups []*adw.PreferencesGroup
	theme  *adw.ActionRow
	status *ai.Status
	err    string
	tests  map[string]*aiTest
	form   *aiForm
	off    func()
	gone   bool
	// A Status read is on its way; again: read once more after it, as
	// something changed meanwhile.
	loading, again bool
}

var settingsWindows = map[*App]*settingsWindow{}

// showSettings opens Settings at a page ("ai", "general"; "" keeps the one
// shown), or brings it to the front. Providers and keys are read then and
// after each change, never on a timer.
func (w *Window) showSettings(page string) {
	s := settingsWindows[w.app]
	if s == nil {
		s = newSettingsWindow(w)
		settingsWindows[w.app] = s
	}
	if page != "" {
		s.win.SetVisiblePageName(page)
	}
	s.win.Present()
	s.load()
}

func newSettingsWindow(w *Window) *settingsWindow {
	s := &settingsWindow{w: w, tests: map[string]*aiTest{}}
	s.win = adw.NewPreferencesWindow()
	s.win.SetApplication(&w.app.gtk.Application)
	s.win.SetTransientFor(&w.win.Window)
	s.win.SetDestroyWithParent(true)
	s.win.SetTitle("Settings")
	s.win.SetDefaultSize(720, 680)
	s.win.SetSearchEnabled(false)

	s.aiPage = adw.NewPreferencesPage()
	s.aiPage.SetName("ai")
	s.aiPage.SetTitle("AI")
	s.aiPage.SetIconName("starred-symbolic")
	s.win.Add(s.aiPage)
	s.win.Add(s.generalPage())
	s.win.SetVisiblePageName("ai")
	s.render()

	keys := gtk.NewEventControllerKey()
	keys.ConnectKeyPressed(func(keyval, _ uint, _ gdk.ModifierType) bool {
		if keyval != gdk.KEY_Escape {
			return false
		}
		if !s.win.PopSubpage() {
			s.win.Close()
		}
		return true
	})
	s.win.AddController(keys)
	s.off = w.app.events.on(config.EventChanged, func(any) {
		s.theme.SetSubtitle(s.w.app.settings.config().Theme)
		s.load()
	})
	s.win.ConnectCloseRequest(func() bool {
		s.closed()
		return false
	})
	s.win.ConnectDestroy(s.closed)
	return s
}

// closed stops what's running and forgets the window.
func (s *settingsWindow) closed() {
	if s.gone {
		return
	}
	s.gone = true
	s.off()
	for _, t := range s.tests {
		if t.call != nil {
			t.call.cancel()
		}
	}
	if settingsWindows[s.w.app] == s {
		delete(settingsWindows, s.w.app)
	}
}

func (s *settingsWindow) parent() *gtk.Window { return &s.win.Window.Window }

func (s *settingsWindow) toast(msg string) {
	t := adw.NewToast(msg)
	t.SetTimeout(3)
	s.win.AddToast(t)
	s.w.app.debugLog("settings toast: %s", msg)
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
		"wtf makes Claude Code ask before every command it runs or types in your shells, even in its auto mode or after \u201cdon't ask again\u201d.")
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
	page.Add(claude)
	return page
}

// AI

func (s *settingsWindow) load() {
	if s.gone {
		return
	}
	if s.loading {
		s.again = true
		return
	}
	s.loading = true
	svc := s.w.app.be.AI
	go func() {
		st := svc.Status()
		onMainAsync(func() {
			s.loading = false
			if s.gone {
				return
			}
			s.status = &st
			s.render()
			if s.again {
				s.again = false
				s.load()
			}
		})
	}()
}

// render rebuilds the AI page.
func (s *settingsWindow) render() {
	for _, g := range s.groups {
		s.aiPage.Remove(g)
	}
	s.groups = nil
	add := func(g *adw.PreferencesGroup) {
		s.aiPage.Add(g)
		s.groups = append(s.groups, g)
	}

	list := adw.NewPreferencesGroup()
	list.SetTitle("AI Providers")
	list.SetDescription("The default answers most AI features; the quick one answers short inline ones. API keys are kept in the keyring, never in config.toml.")
	addB := gtk.NewButtonWithLabel("Add Provider…")
	addB.AddCSSClass("suggested-action")
	addB.SetVAlign(gtk.AlignCenter)
	addB.ConnectClicked(s.choosePreset)
	list.SetHeaderSuffix(addB)
	switch {
	case s.status == nil:
		list.Add(s.textRow(settingsLabel("Checking…", "dim-label")))
	case len(s.status.Providers) == 0:
		list.Add(s.textRow(settingsLabel("No AI provider is set up, so AI features are hidden. AI works with any AI tool: the Claude API, any OpenAI-compatible endpoint (OpenAI, Gemini, Groq, OpenRouter, Ollama, LM Studio…), or any command-line tool (claude -p, codex exec, gemini, llm). Add one with Add Provider…")))
	default:
		for _, p := range s.status.Providers {
			list.Add(s.textRow(s.row(p)))
		}
	}
	if s.err != "" {
		list.Add(settingsLabel(s.err, "error"))
	}
	add(list)

	more := adw.NewPreferencesGroup()
	buttons := gtk.NewBox(gtk.OrientationHorizontal, 8)
	cfg := gtk.NewButtonWithLabel("Open config.toml")
	cfg.ConnectClicked(s.w.openConfigFile)
	help := gtk.NewButtonWithLabel("Help")
	help.SetTooltipText("The manual's AI page")
	help.ConnectClicked(func() { s.w.app.showManual("ai") })
	buttons.Append(cfg)
	buttons.Append(help)
	more.Add(buttons)
	add(more)
}

// textRow is a list row holding any widget.
func (s *settingsWindow) textRow(child gtk.Widgetter) *adw.PreferencesRow {
	r := adw.NewPreferencesRow()
	r.SetActivatable(false)
	r.SetChild(child)
	gtk.BaseWidget(child).AddCSSClass("wtf-settings-row")
	return r
}

// settingsLabel is wrapping text; classes style it (dim-label, error…).
func settingsLabel(text string, classes ...string) *gtk.Label {
	l := gtk.NewLabel(text)
	l.SetXAlign(0)
	l.SetWrap(true)
	l.SetWrapMode(pango.WrapWordChar)
	l.SetMaxWidthChars(1)
	l.SetHExpand(true)
	for _, c := range classes {
		l.AddCSSClass(c)
	}
	return l
}

// aiTestRequest is what Test sends: a few tokens, to see the connection
// works.
func aiTestRequest(name string) ai.Request {
	return ai.Request{
		Provider:  name,
		System:    "You check that a connection works.",
		Messages:  []ai.Message{{Role: "user", Content: "Reply with the single word OK."}},
		MaxTokens: 20,
	}
}

// aiTest is one provider's Test: the reply so far, then how it ended.
type aiTest struct {
	call *aiCall
	text string
	done *ai.Done
	// The reply's label while it's shown.
	reply *gtk.Label
}

// aiKeyStatus says where a provider's key comes from, and whether it's
// missing.
func aiKeyStatus(p ai.ProviderInfo) (string, bool) {
	switch {
	case p.Kind == "command":
		return "Uses the tool's own login", false
	case p.KeyFrom == "keychain":
		return "Key in keyring", false
	case strings.HasPrefix(p.KeyFrom, "$") && p.HasKey:
		return "Key from " + p.KeyFrom, false
	case strings.HasPrefix(p.KeyFrom, "$") && p.NeedsKey:
		return "Needs an API key: " + p.KeyFrom + " is empty", true
	case p.NeedsKey:
		return "Needs an API key", true
	}
	return "No key needed", false
}

// row is one provider: name and markers, kind · model, where requests go,
// the key, then Test and a menu of the rest; and the last Test's result.
func (s *settingsWindow) row(p ai.ProviderInfo) *gtk.Box {
	text := gtk.NewBox(gtk.OrientationVertical, 2)
	text.SetHExpand(true)
	head := gtk.NewBox(gtk.OrientationHorizontal, 8)
	name := gtk.NewLabel(p.Name)
	name.AddCSSClass("wtf-tools-name")
	head.Append(name)
	marker := func(s, tip string) {
		l := gtk.NewLabel(s)
		l.AddCSSClass("wtf-ai-marker")
		l.SetVAlign(gtk.AlignCenter)
		l.SetTooltipText(tip)
		head.Append(l)
	}
	if p.Default {
		marker("Default", "Answers AI features (ai-default)")
	}
	if p.Fast {
		marker("Quick", "Answers quick inline features (ai-fast)")
	}
	text.Append(head)
	muted := func(s string, classes ...string) *gtk.Label {
		l := settingsLabel(s, append([]string{"wtf-tools-muted"}, classes...)...)
		text.Append(l)
		return l
	}
	kind := p.Kind
	if p.Model != "" {
		kind += " · " + p.Model
	}
	muted(kind)
	dest := p.Destination
	if p.Local {
		dest += " · on this machine"
	}
	muted(dest, "wtf-tools-mono").SetTooltipText(map[bool]string{true: "The command wtf runs", false: "Where requests go"}[p.Kind == "command"])
	key, missing := aiKeyStatus(p)
	if missing {
		muted(key, "warning")
	} else {
		muted(key)
	}

	side := gtk.NewBox(gtk.OrientationHorizontal, 4)
	side.SetVAlign(gtk.AlignStart)
	t := s.tests[p.Name]
	running := t != nil && t.done == nil
	test := gtk.NewButtonWithLabel("Test")
	test.SetTooltipText("Send a tiny request and show the reply")
	if running {
		test.SetLabel("Stop")
		test.SetTooltipText("Stop the test")
	}
	test.ConnectClicked(func() {
		if running {
			t.call.cancel()
		} else {
			s.test(p.Name)
		}
	})
	side.Append(test)
	side.Append(s.menu(p))

	top := gtk.NewBox(gtk.OrientationHorizontal, 10)
	top.Append(text)
	top.Append(side)
	row := gtk.NewBox(gtk.OrientationVertical, 6)
	row.Append(top)
	if t != nil {
		row.Append(s.testResult(t))
	}
	return row
}

// menu is a provider's other actions.
func (s *settingsWindow) menu(p ai.ProviderInfo) *gtk.MenuButton {
	group := gio.NewSimpleActionGroup()
	menu := gio.NewMenu()
	section := gio.NewMenu()
	add := func(id, label string, run func()) {
		act := gio.NewSimpleAction(id, nil)
		act.ConnectActivate(func(*glib.Variant) { run() })
		group.AddAction(act)
		section.Append(label, "ai."+id)
	}
	next := func() {
		if section.NItems() > 0 {
			menu.AppendSection("", section)
		}
		section = gio.NewMenu()
	}
	add("edit", "Edit…", func() { s.edit(p) })
	if p.Kind != "command" {
		if p.KeyFrom == "keychain" {
			add("setkey", "Replace Key…", func() { s.setKey(p) })
			add("removekey", "Remove Key…", func() { s.removeKey(p) })
		} else if !strings.HasPrefix(p.KeyFrom, "$") {
			add("setkey", "Set Key…", func() { s.setKey(p) })
		}
	}
	next()
	if !p.Default {
		add("default", "Use as Default", func() { s.choose(false, p.Name) })
	}
	if !p.Fast {
		add("fast", "Use for Quick Features", func() { s.choose(true, p.Name) })
	}
	add("preview", "What's Sent by Test…", func() { s.w.previewAI(s.parent(), aiTestRequest(p.Name)) })
	next()
	add("delete", "Delete…", func() { s.confirmDelete(p) })
	next()
	b := gtk.NewMenuButton()
	b.SetIconName("view-more-symbolic")
	b.SetTooltipText("Edit, keys, default and more")
	b.InsertActionGroup("ai", group)
	b.SetMenuModel(menu)
	return b
}

// testResult is a Test's reply as it streams, then its time and tokens, or
// its error.
func (s *settingsWindow) testResult(t *aiTest) *gtk.Box {
	box := gtk.NewBox(gtk.OrientationVertical, 4)
	if t.text != "" || t.done == nil {
		t.reply = settingsLabel(t.text, "wtf-tools-mono", "wtf-ai-reply")
		t.reply.SetSelectable(true)
		t.reply.SetVisible(t.text != "")
		box.Append(t.reply)
	}
	line := gtk.NewBox(gtk.OrientationHorizontal, 6)
	switch r := t.done; {
	case r == nil:
		spin := gtk.NewSpinner()
		spin.Start()
		line.Append(spin)
		line.Append(settingsLabel("Waiting for the reply…", "dim-label", "wtf-sheet-sub"))
	case r.Error != "":
		line.Append(settingsLabel(r.Error, "error", "wtf-sheet-sub"))
	case r.Cancelled:
		line.Append(settingsLabel("Stopped.", "dim-label", "wtf-sheet-sub"))
	default:
		line.Append(settingsLabel(aiUsage(*r), "dim-label", "wtf-sheet-sub"))
	}
	box.Append(line)
	return box
}

// aiUsage is how long a reply took, and its tokens and cost when the
// provider said.
func aiUsage(r ai.Done) string {
	parts := []string{fmt.Sprintf("Replied in %.1f s", float64(r.Millis)/1000)}
	if strings.TrimSpace(r.Text) == "" {
		parts[0] = fmt.Sprintf("An empty reply after %.1f s", float64(r.Millis)/1000)
	}
	if u := r.Usage; u.Counted {
		parts = append(parts, fmt.Sprintf("%d tokens in, %d out", u.InputTokens, u.OutputTokens))
	}
	if r.Usage.Cost > 0 {
		parts = append(parts, fmt.Sprintf("$%.4f", r.Usage.Cost))
	}
	return strings.Join(parts, " · ")
}

// test sends the test request to one provider, previewed first if it's
// the first.
func (s *settingsWindow) test(name string) {
	t := &aiTest{}
	s.tests[name] = t
	t.call = s.w.askAI(s.parent(), aiTestRequest(name), func(text string) {
		t.text += text
		if t.reply != nil {
			t.reply.SetText(t.text)
			t.reply.SetVisible(true)
		}
	}, func(r ai.Done) {
		if s.tests[name] != t {
			return
		}
		if r.Cancelled && r.Text == "" && t.text == "" && !t.call.sent {
			// The preview was cancelled: nothing to show.
			delete(s.tests, name)
		} else {
			if r.Text != "" {
				t.text = r.Text
			}
			t.done = &r
		}
		if !s.gone {
			s.render()
		}
	})
	s.render()
}

// forget drops a provider's Test, stopping it.
func (s *settingsWindow) forget(name string) {
	if t := s.tests[name]; t != nil {
		delete(s.tests, name)
		t.call.cancel()
	}
}

// setKey asks for a provider's API key and stores it in the keyring.
func (s *settingsWindow) setKey(p ai.ProviderInfo) {
	f := newSSHForm(s.w, "API Key for "+p.Name, "Stored in the keyring (Secret Service), never in config.toml.", 420)
	f.d.SetTransientFor(s.parent())
	e := gtk.NewPasswordEntry()
	e.SetShowPeekIcon(true)
	e.SetHExpand(true)
	e.ConnectActivate(func() { f.d.Response("save") })
	f.field("Key", e, "Sent only to "+p.Destination+".")
	f.button("cancel", "Cancel", adw.ResponseDefault, nil)
	f.button("save", "Save Key", adw.ResponseSuggested, func() {
		key := strings.TrimSpace(e.Text())
		if key == "" {
			f.fail("Enter a key")
			return
		}
		svc := s.w.app.be.AI
		go func() {
			err := svc.SetKey(p.Name, key)
			onMainAsync(func() {
				if err != nil {
					f.fail(err.Error())
					return
				}
				f.close()
				s.err = ""
				s.load()
			})
		}()
	})
	f.present(e)
}

// removeKey deletes a provider's key from the keyring, once confirmed.
func (s *settingsWindow) removeKey(p ai.ProviderInfo) {
	s.w.app.confirm(s.parent(), "Remove the key for "+p.Name+"?",
		"Deletes it from the keyring. "+p.Name+" can't answer until a key is set again.", "Remove Key", func() {
			svc := s.w.app.be.AI
			go func() {
				err := svc.DeleteKey(p.Name)
				onMainAsync(func() {
					s.err = ""
					if err != nil {
						s.err = "Couldn't remove the key: " + err.Error()
					}
					s.load()
				})
			}()
		})
}

// choose writes ai-default or ai-fast; config.toml's change event reloads
// the list.
func (s *settingsWindow) choose(fast bool, name string) {
	cfg := s.w.app.be.Config
	go func() {
		err := cfg.SetAIProvider(fast, name)
		onMainAsync(func() {
			if err != nil && !s.gone {
				s.err = "Couldn't change the setting: " + err.Error()
				s.render()
			}
		})
	}()
}

// confirmDelete removes a provider from config.toml and its key from the
// keyring, once confirmed.
func (s *settingsWindow) confirmDelete(p ai.ProviderInfo) {
	body := "Removes [ai.providers." + p.Name + "] from config.toml"
	if p.Kind != "command" {
		body += ", and its API key from the keyring"
	}
	body += "."
	if p.Default {
		body += " Another provider becomes the default."
	}
	s.w.app.confirm(s.parent(), "Delete "+p.Name+"?", body, "Delete Provider", func() { s.delete(p.Name, p.Kind) })
}

func (s *settingsWindow) delete(name, kind string) {
	s.forget(name)
	cfg, svc := s.w.app.be.Config, s.w.app.be.AI
	go func() {
		err := cfg.DeleteAIProvider(name)
		var keyErr error
		if err == nil && kind != "command" {
			keyErr = svc.DeleteKey(name)
		}
		onMainAsync(func() {
			if s.gone {
				return
			}
			s.err = ""
			switch {
			case err != nil:
				s.err = "Couldn't delete " + name + ": " + err.Error()
			case keyErr != nil:
				s.err = "Deleted " + name + ", but its key is still in the keyring: " + keyErr.Error()
			default:
				s.toast("Deleted " + name)
			}
			s.load()
		})
	}()
}

// aiEntries are the palette's per-provider entries: which one is the
// default, and which answers quick features. From config.toml alone, so the
// palette doesn't wait for the keyring.
func (w *Window) aiEntries() []paletteEntry {
	c := w.app.settings.config()
	names := make([]string, 0, len(c.AI.Providers))
	for name := range c.AI.Providers {
		names = append(names, name)
	}
	slices.Sort(names)
	var out []paletteEntry
	for _, name := range names {
		p := c.AI.Providers[name]
		detail := p.Kind
		if p.Model != "" {
			detail += " · " + p.Model
		}
		if name != c.AIDefault {
			out = append(out, paletteEntry{section: "Commands", label: "AI: Use " + name + " as Default", detail: detail,
				run: func() { w.setAIProvider(false, name) }})
		}
		if name != c.AIFast {
			out = append(out, paletteEntry{section: "Commands", label: "AI: Use " + name + " for Quick Features", detail: detail,
				run: func() { w.setAIProvider(true, name) }})
		}
	}
	return out
}

// setAIProvider writes ai-default or ai-fast, then says so.
func (w *Window) setAIProvider(fast bool, name string) {
	done := name + " answers AI features"
	if fast {
		done = name + " answers quick AI features"
	}
	cfg := w.app.be.Config
	w.setting(func() error { return cfg.SetAIProvider(fast, name) }, done)
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

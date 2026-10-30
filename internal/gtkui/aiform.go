//go:build linux

package gtkui

import (
	"fmt"
	"strings"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/dayanraj/wtf/internal/ai"
	"github.com/dayanraj/wtf/internal/config"
)

// Settings → AI's Add Provider and Edit: a preset to start from, then a
// form whose fields follow the kind. Saved into config.toml's
// [ai.providers] (the rest of the file is kept); a typed key goes to the
// keyring. Like the macOS app's AIProviderForm.

// aiKinds are the Kind dropdown's choices.
var aiKinds = []struct{ id, label string }{
	{"anthropic", "Claude API (anthropic)"},
	{"openai", "OpenAI-compatible (openai)"},
	{"command", "Command-line tool (command)"},
}

// aiDefaultBase is where requests go with no Base URL.
var aiDefaultBase = map[string]string{"anthropic": "https://api.anthropic.com", "openai": "https://api.openai.com/v1"}

// choosePreset shows the presets, by group; choosing one opens the form.
func (s *settingsWindow) choosePreset() {
	page := adw.NewPreferencesPage()
	var group *adw.PreferencesGroup
	last := ""
	for _, p := range s.w.app.be.AI.Presets() {
		if group == nil || p.Group != last {
			group = adw.NewPreferencesGroup()
			group.SetTitle(p.Group)
			page.Add(group)
			last = p.Group
		}
		row := adw.NewActionRow()
		row.SetUseMarkup(false)
		row.SetTitle(p.Label)
		row.SetSubtitle(p.Help)
		row.SetActivatable(true)
		row.AddSuffix(gtk.NewImageFromIconName("go-next-symbolic"))
		row.ConnectActivated(func() { s.openForm("", p.Name, configOf(p), p.Help, false) })
		group.Add(row)
	}
	s.push(page, "Add Provider", nil)
}

// configOf is a preset as a config.toml table.
func configOf(p ai.Preset) config.AIProvider {
	return config.AIProvider{Kind: p.Kind, BaseURL: p.BaseURL, Model: p.Model, Command: p.Command}
}

// push shows a page over the AI page, with end in its header bar.
func (s *settingsWindow) push(content gtk.Widgetter, title string, end gtk.Widgetter) {
	head := adw.NewHeaderBar()
	if end != nil {
		head.PackEnd(end)
	}
	view := adw.NewToolbarView()
	view.AddTopBar(head)
	view.SetContent(content)
	s.win.PushSubpage(adw.NewNavigationPage(view, title))
}

// edit opens the form for a provider as config.toml has it.
func (s *settingsWindow) edit(p ai.ProviderInfo) {
	c, ok := s.w.app.settings.config().AI.Providers[p.Name]
	if !ok {
		s.toast(p.Name + " isn't in config.toml any more")
		return
	}
	s.openForm(p.Name, p.Name, c, "", p.KeyFrom == "keychain")
}

// aiForm is the provider form. Main loop only.
type aiForm struct {
	s *settingsWindow
	// The provider being edited ("" when adding), and its kind then.
	old, oldKind string
	hasKey       bool
	name         *adw.EntryRow
	kind         *adw.ComboRow
	api, cmd     *adw.PreferencesGroup
	baseURL      *gtk.Entry
	model        *gtk.Entry
	models       *gtk.MenuButton
	modelList    *gtk.ListBox
	fetched      []string
	fetch        *gtk.Button
	spin         *gtk.Spinner
	modelNote    *gtk.Label
	key          *adw.PasswordEntryRow
	keyEnv       *adw.EntryRow
	headers      *gtk.TextView
	headersRow   *adw.ExpanderRow
	command      *adw.EntryRow
	err          *gtk.Label
	save         *gtk.Button
	busy         bool
}

// openForm shows the form: old is the provider edited ("" to add one),
// name and p fill it, help (a preset's) says where to get a key.
func (s *settingsWindow) openForm(old, name string, p config.AIProvider, help string, hasKey bool) {
	f := &aiForm{s: s, old: old, oldKind: p.Kind, hasKey: hasKey}
	if old == "" {
		name = s.freeName(name)
	}
	page := adw.NewPreferencesPage()

	top := adw.NewPreferencesGroup()
	if help != "" {
		top.SetDescription(help)
	} else if old != "" {
		top.SetDescription("[ai.providers." + old + "] in config.toml.")
	}
	f.name = adw.NewEntryRow()
	f.name.SetTitle("Name")
	f.name.SetText(name)
	f.name.ConnectEntryActivated(f.submit)
	top.Add(f.name)
	f.kind = adw.NewComboRow()
	f.kind.SetTitle("Kind")
	// The choice under the title, at full width: beside it, it's cut.
	f.kind.SetUseSubtitle(true)
	labels := make([]string, len(aiKinds))
	selected := 0
	for i, k := range aiKinds {
		labels[i] = k.label
		if k.id == p.Kind {
			selected = i
		}
	}
	f.kind.SetModel(gtk.NewStringList(labels))
	f.kind.SetSelected(uint(selected))
	f.kind.NotifyProperty("selected", f.kindChanged)
	top.Add(f.kind)
	page.Add(top)

	// anthropic and openai
	f.api = adw.NewPreferencesGroup()
	f.api.SetTitle("Connection")
	base := adw.NewActionRow()
	base.SetTitle("Base URL")
	f.baseURL = gtk.NewEntry()
	f.baseURL.SetText(p.BaseURL)
	f.baseURL.SetHExpand(true)
	f.baseURL.SetWidthChars(30)
	f.baseURL.SetVAlign(gtk.AlignCenter)
	f.baseURL.ConnectActivate(f.submit)
	base.AddSuffix(f.baseURL)
	f.api.Add(base)

	modelRow := adw.NewActionRow()
	modelRow.SetTitle("Model")
	f.model = gtk.NewEntry()
	f.model.SetText(p.Model)
	f.model.SetHExpand(true)
	f.model.SetWidthChars(22)
	f.model.ConnectActivate(f.submit)
	f.modelList = gtk.NewListBox()
	f.modelList.SetActivateOnSingleClick(true)
	scroll := gtk.NewScrolledWindow()
	scroll.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	scroll.SetMaxContentHeight(300)
	scroll.SetPropagateNaturalHeight(true)
	scroll.SetChild(f.modelList)
	pop := gtk.NewPopover()
	pop.SetChild(scroll)
	f.models = gtk.NewMenuButton()
	f.models.SetIconName("pan-down-symbolic")
	f.models.SetTooltipText("Fetched models")
	f.models.SetPopover(pop)
	f.models.SetSensitive(false)
	f.modelList.ConnectRowActivated(func(r *gtk.ListBoxRow) {
		if i := r.Index(); i >= 0 && i < len(f.fetched) {
			f.model.SetText(f.fetched[i])
			f.model.SetPosition(-1)
		}
		pop.Popdown()
	})
	combo := gtk.NewBox(gtk.OrientationHorizontal, 0)
	combo.AddCSSClass("linked")
	combo.SetVAlign(gtk.AlignCenter)
	combo.Append(f.model)
	combo.Append(f.models)
	f.fetch = gtk.NewButtonWithLabel("Fetch Models")
	f.fetch.SetVAlign(gtk.AlignCenter)
	f.fetch.SetTooltipText("Ask the provider which models it has")
	f.fetch.ConnectClicked(f.fetchModels)
	f.spin = gtk.NewSpinner()
	f.spin.SetVisible(false)
	modelRow.AddSuffix(combo)
	modelRow.AddSuffix(f.fetch)
	modelRow.AddSuffix(f.spin)
	f.api.Add(modelRow)

	f.key = adw.NewPasswordEntryRow()
	f.key.SetTitle("API key")
	if hasKey {
		f.key.SetTitle("API key (leave empty to keep the saved one)")
	}
	f.key.ConnectEntryActivated(f.submit)
	f.api.Add(f.key)

	env := adw.NewExpanderRow()
	env.SetTitle("Read the key from an environment variable")
	env.SetSubtitle("Instead of the keyring: wtf's own environment, not the shell's")
	f.keyEnv = adw.NewEntryRow()
	f.keyEnv.SetTitle("Variable name, e.g. OPENAI_API_KEY")
	f.keyEnv.SetText(p.APIKeyEnv)
	f.keyEnv.ConnectEntryActivated(f.submit)
	env.AddRow(f.keyEnv)
	env.SetExpanded(p.APIKeyEnv != "")
	f.api.Add(env)

	hdrs := adw.NewExpanderRow()
	hdrs.SetTitle("Extra headers")
	hdrs.SetSubtitle("Sent with every request, e.g. a workspace or organization id")
	f.headers = gtk.NewTextView()
	f.headers.SetMonospace(true)
	f.headers.SetWrapMode(gtk.WrapWordChar)
	f.headers.SetTopMargin(6)
	f.headers.SetBottomMargin(6)
	f.headers.SetLeftMargin(8)
	f.headers.SetRightMargin(8)
	f.headers.SetSizeRequest(-1, 64) // about 3 lines
	f.headers.Buffer().SetText(config.FormatHeaders(p.Headers))
	frame := gtk.NewFrame("")
	frame.SetChild(f.headers)
	box := gtk.NewBox(gtk.OrientationVertical, 6)
	box.Append(frame)
	box.Append(settingsLabel("One per line, as Name: value — e.g. anthropic-workspace-id: wrkspc_… Not for keys.", "dim-label", "wtf-sheet-sub"))
	hdrs.AddRow(s.textRow(box))
	hdrs.SetExpanded(len(p.Headers) > 0)
	f.api.Add(hdrs)
	f.headersRow = hdrs
	f.modelNote = settingsLabel("", "dim-label", "wtf-sheet-sub")
	f.modelNote.SetMarginTop(6)
	f.modelNote.SetVisible(false)
	f.api.Add(f.modelNote)
	page.Add(f.api)

	// command
	f.cmd = adw.NewPreferencesGroup()
	f.cmd.SetTitle("Command")
	f.cmd.SetDescription("wtf runs it in an empty folder and gives it the prompt on stdin; its output is the reply. The program is found on the focused shell's PATH, and its own login and settings apply. Quote arguments as in a shell.")
	f.command = adw.NewEntryRow()
	f.command.SetTitle("Command, e.g. claude -p")
	f.command.SetText(config.JoinCommand(p.Command))
	f.command.ConnectEntryActivated(f.submit)
	f.cmd.Add(f.command)
	page.Add(f.cmd)

	errs := adw.NewPreferencesGroup()
	f.err = settingsLabel("", "error")
	f.err.SetVisible(false)
	errs.Add(f.err)
	page.Add(errs)

	f.save = gtk.NewButtonWithLabel("Save")
	f.save.AddCSSClass("suggested-action")
	f.save.ConnectClicked(f.submit)
	title := "Add Provider"
	if old != "" {
		title = "Edit " + old
	}
	s.form = f
	s.push(page, title, f.save)
	f.kindChanged()
	f.name.GrabFocus()
}

// freeName is name, or name-2, -3… when it's taken.
func (s *settingsWindow) freeName(name string) string {
	if name == "" {
		return ""
	}
	taken := s.w.app.settings.config().AI.Providers
	free := name
	for i := 2; ; i++ {
		if _, ok := taken[free]; !ok {
			return free
		}
		free = fmt.Sprintf("%s-%d", name, i)
	}
}

func (f *aiForm) kindID() string {
	return aiKinds[min(int(f.kind.Selected()), len(aiKinds)-1)].id
}

// kindChanged shows the fields the kind uses.
func (f *aiForm) kindChanged() {
	k := f.kindID()
	f.api.SetVisible(k != "command")
	f.cmd.SetVisible(k == "command")
	f.baseURL.SetPlaceholderText(aiDefaultBase[k])
}

// provider is the form as a config.toml table.
func (f *aiForm) provider() (config.AIProvider, error) {
	p := config.AIProvider{Kind: f.kindID()}
	if p.Kind == "command" {
		args, err := config.SplitCommand(f.command.Text())
		if err != nil {
			return p, err
		}
		p.Command = args
		return p, nil
	}
	p.BaseURL = strings.TrimSpace(f.baseURL.Text())
	p.Model = strings.TrimSpace(f.model.Text())
	p.APIKeyEnv = strings.TrimSpace(f.keyEnv.Text())
	h, err := config.ParseHeaders(bufferText(f.headers.Buffer()))
	if err != nil {
		return p, err
	}
	p.Headers = h
	return p, nil
}

func (f *aiForm) fail(msg string) {
	f.err.SetText(msg)
	f.err.SetVisible(msg != "")
}

// fetchModels asks the provider for its models, with the typed key (or
// the saved one), and offers them under the Model field.
func (f *aiForm) fetchModels() {
	p, err := f.provider()
	if err != nil {
		f.note(err.Error(), true)
		return
	}
	f.fetch.SetSensitive(false)
	f.spin.SetVisible(true)
	f.spin.Start()
	f.note("", false)
	svc, old, key := f.s.w.app.be.AI, f.old, strings.TrimSpace(f.key.Text())
	go func() {
		ids, err := svc.Models(old, p, key)
		onMainAsync(func() {
			f.spin.Stop()
			f.spin.SetVisible(false)
			f.fetch.SetSensitive(true)
			switch {
			case err != nil:
				f.note("Couldn't fetch the models: "+err.Error(), true)
			case len(ids) == 0:
				f.note("The provider listed no models.", false)
			default:
				f.setModels(ids)
				f.note(fmt.Sprintf("%d models: pick one with the arrow, or type any name.", len(ids)), false)
				if f.s.form == f && f.models.Mapped() {
					f.models.Popup()
				}
			}
		})
	}()
}

func (f *aiForm) setModels(ids []string) {
	f.fetched = ids
	for c := f.modelList.FirstChild(); c != nil; c = f.modelList.FirstChild() {
		f.modelList.Remove(c)
	}
	for _, id := range ids {
		l := gtk.NewLabel(id)
		l.SetXAlign(0)
		l.SetMarginStart(6)
		l.SetMarginEnd(6)
		f.modelList.Append(l)
	}
	f.models.SetSensitive(true)
}

// note is the line under the connection fields: the models found, or why
// not.
func (f *aiForm) note(msg string, bad bool) {
	f.modelNote.SetText(msg)
	f.modelNote.SetVisible(msg != "")
	if bad {
		f.modelNote.RemoveCSSClass("dim-label")
		f.modelNote.AddCSSClass("error")
	} else {
		f.modelNote.RemoveCSSClass("error")
		f.modelNote.AddCSSClass("dim-label")
	}
}

// submit saves the provider; after a rename its key moves along, and a
// typed key goes to the keyring. On an error the form stays.
func (f *aiForm) submit() {
	if f.busy {
		return
	}
	name := strings.TrimSpace(f.name.Text())
	p, err := f.provider()
	if err == nil {
		_, err = config.CheckAIProvider(name, p)
	}
	if err != nil {
		f.fail(err.Error())
		return
	}
	key := ""
	if p.Kind != "command" {
		key = strings.TrimSpace(f.key.Text())
	}
	f.busy = true
	f.save.SetSensitive(false)
	f.fail("")
	cfg, svc, old, oldKind := f.s.w.app.be.Config, f.s.w.app.be.AI, f.old, f.oldKind
	go func() {
		err := cfg.SaveAIProvider(old, name, p)
		saved := err == nil
		if saved && old != "" && old != name && oldKind != "command" {
			if err = svc.MoveKey(old, name); err != nil {
				err = fmt.Errorf("saved, but its key couldn't move to the new name: %w", err)
			}
		}
		if err == nil && key != "" {
			if err = svc.SetKey(name, key); err != nil {
				err = fmt.Errorf("saved, but the key couldn't be stored: %w", err)
			}
		}
		onMainAsync(func() {
			f.busy = false
			f.save.SetSensitive(true)
			s := f.s
			if saved {
				// Saving again edits what's there now.
				if old != name {
					s.forget(old)
				}
				f.old, f.oldKind = name, p.Kind
			}
			if err != nil {
				f.fail(err.Error())
				return
			}
			if s.form == f {
				s.form = nil
			}
			if s.gone {
				return
			}
			for s.win.PopSubpage() {
			}
			s.toast("Saved " + name)
			s.load()
		})
	}()
}

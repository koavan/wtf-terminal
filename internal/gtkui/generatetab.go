//go:build linux

package gtkui

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"

	"github.com/dayanraj/wtf/internal/sshhosts"
	"github.com/dayanraj/wtf/internal/uicore"
)

func init() {
	registerSidebarTab(sidebarTabInfo{tab: uicore.SidebarGenerate, label: "Generate Keys & Passwords", icon: "dialog-password-symbolic",
		cmd: "sidebar.generate", make: func(w *Window) sidePaneTab { return newGenerateTab(w) }})
	extraCSS = append(extraCSS, `
.wtf-gen-form { padding: 6px 14px 16px; }
.wtf-gen-link { color: @accent_color; font-size: 0.9em; padding: 0 4px; min-height: 22px; }
.wtf-gen-password { font-family: monospace; font-size: 1.15em; padding: 8px 10px;
  background: alpha(currentColor, 0.05); border-radius: 6px; }
`)
}

// generateTab is the Generate tab: a new SSH key (made by wtf, with a
// passphrase if you like) or a random password. Nothing loads until the
// key form shows. Like the macOS app's GenerateView.
type generateTab struct {
	w        *Window
	root     *gtk.Box
	switches map[string]*gtk.ToggleButton
	scroll   *gtk.ScrolledWindow
	key      *generateKeyForm
	pass     *generatePasswordForm
}

func newGenerateTab(w *Window) *generateTab {
	g := &generateTab{w: w, switches: map[string]*gtk.ToggleButton{}}
	bar := gtk.NewBox(gtk.OrientationHorizontal, 0)
	bar.AddCSSClass("linked")
	bar.AddCSSClass("wtf-ssh-switcher")
	bar.SetHomogeneous(true)
	var first *gtk.ToggleButton
	for _, s := range []struct{ id, label string }{{"key", "SSH Key"}, {"password", "Password"}} {
		b := gtk.NewToggleButtonWithLabel(s.label)
		b.SetFocusOnClick(false)
		if first == nil {
			first = b
		} else {
			b.SetGroup(first)
		}
		id := s.id
		b.ConnectToggled(func() {
			if b.Active() && w.sidebarState.GenerateSection != id {
				w.sidebarState.GenerateSection = id
				w.scheduleSave()
				g.show()
			}
		})
		g.switches[id] = b
		bar.Append(b)
	}
	g.scroll = gtk.NewScrolledWindow()
	g.scroll.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	g.scroll.SetVExpand(true)
	g.root = gtk.NewBox(gtk.OrientationVertical, 4)
	g.root.Append(bar)
	g.root.Append(g.scroll)
	g.show()
	return g
}

func (g *generateTab) widget() gtk.Widgetter { return g.root }

func (g *generateTab) paneChanged() {}

// commandFinished: ssh-keygen and the like may have taken the suggested
// key name.
func (g *generateTab) commandFinished() {
	ps := g.w.model.ActivePane()
	if g.key != nil && ps != nil && ps.LastCommand != nil && uicore.IsKeyTool(ps.LastCommand.Text) {
		g.key.loadKeys()
	}
}

func (g *generateTab) close() { g.drop() }

func (g *generateTab) drop() {
	if g.key != nil {
		g.key.closed = true
		g.key = nil
	}
	if g.pass != nil {
		g.pass.off()
		g.pass = nil
	}
}

// show shows the remembered section (showGenerateKey sets it first).
func (g *generateTab) show() {
	g.drop()
	if g.w.sidebarState.GenerateSection == "password" {
		g.switches["password"].SetActive(true)
		g.pass = newGeneratePasswordForm(g.w)
		g.scroll.SetChild(g.pass.root)
		return
	}
	g.switches["key"].SetActive(true)
	g.key = newGenerateKeyForm(g.w)
	g.scroll.SetChild(g.key.root)
}

// showGenerateKey opens the Generate tab on the SSH key form (never closes
// the pane).
func (w *Window) showGenerateKey() { w.showGenerator("key") }

// showGenerator opens the Generate tab on a section: "key" or "password".
func (w *Window) showGenerator(section string) {
	w.sidebarState.GenerateSection = section
	w.sidebarState.Tab, w.sidebarState.Open = uicore.SidebarGenerate, true
	w.layoutSidebar()
	if g, ok := w.sidePane().(*generateTab); ok {
		g.show()
	}
}

// Form parts

func generateForm() *gtk.Box {
	b := gtk.NewBox(gtk.OrientationVertical, 8)
	b.AddCSSClass("wtf-gen-form")
	return b
}

func generateHeading(text string) *gtk.Label { return wrapLabel(text, "caption-heading") }

func generateNote(text string) *gtk.Label {
	l := wrapLabel(text, "dim-label")
	l.AddCSSClass("caption")
	return l
}

func generateLink(label string, run func()) *gtk.Button {
	b := gtk.NewButtonWithLabel(label)
	b.AddCSSClass("flat")
	b.AddCSSClass("wtf-gen-link")
	b.SetFocusOnClick(false)
	b.SetHAlign(gtk.AlignStart)
	b.ConnectClicked(run)
	return b
}

func generateButton(label string, suggested bool, run func()) *gtk.Button {
	b := gtk.NewButtonWithLabel(label)
	if suggested {
		b.AddCSSClass("suggested-action")
	}
	b.SetHAlign(gtk.AlignStart)
	b.ConnectClicked(run)
	return b
}

// generateSSH is the SSH tab's dialogs (install on host, git identity, key
// generation in a terminal) without its view, so they open over this tab
// as on macOS: a closed sshTab skips redrawing and only loads what the
// dialogs need.
func generateSSH(w *Window) *sshTab {
	return &sshTab{w: w, closed: true, switches: map[string]*gtk.ToggleButton{}, testing: map[string]bool{}}
}

// SSH key

// generateKeyForm makes a key pair in ~/.ssh through Go: the passphrase
// is used once, in memory, and never stored. Then it shows the public key
// and what to do with it. Like the macOS app's KeyGenForm.
type generateKeyForm struct {
	w    *Window
	root *gtk.Box

	keyType, name, comment string
	passphrase, confirm    string
	nameTouched, naming    bool
	showPass, addToAgent   bool
	busy, closed           bool
	err                    string
	taken                  map[string]bool
	result                 *sshhosts.KeyGenResult

	nameEntry *gtk.Entry
	saved     *gtk.Label
	problem   *gtk.Label
	copyLink  *gtk.Button
	submit    *gtk.Button
}

func newGenerateKeyForm(w *Window) *generateKeyForm {
	f := &generateKeyForm{w: w, root: generateForm(), keyType: "ed25519", addToAgent: true, taken: map[string]bool{}}
	f.suggestName()
	f.render()
	f.loadKeys()
	return f
}

// loadKeys reads the keys in ~/.ssh, for a free name.
func (f *generateKeyForm) loadKeys() {
	svc := f.w.app.be.SSHHosts
	go func() {
		l := svc.KeyList()
		onMainAsync(func() {
			if f.closed {
				return
			}
			f.taken = map[string]bool{}
			for _, k := range l.Keys {
				f.taken[uicore.PrettyPath(k.Path, f.w.app.home)] = true
			}
			if !f.nameTouched && f.result == nil {
				f.suggestName()
			}
		})
	}()
}

func (f *generateKeyForm) suggestName() {
	file := uicore.SSHKeyTypes[0].File
	for _, k := range uicore.SSHKeyTypes {
		if k.ID == f.keyType {
			file = k.File
		}
	}
	f.name = uicore.FreeKeyName(file, f.taken)
	if f.nameEntry != nil {
		f.naming = true
		f.nameEntry.SetText(f.name)
		f.naming = false
	}
}

func (f *generateKeyForm) clear() {
	for c := f.root.FirstChild(); c != nil; c = f.root.FirstChild() {
		f.root.Remove(c)
	}
	f.nameEntry, f.copyLink, f.submit = nil, nil, nil
}

func (f *generateKeyForm) render() {
	f.clear()
	if f.result != nil {
		f.renderResult(*f.result)
		return
	}
	add := f.root.Append
	var labels []string
	selected := 0
	for i, k := range uicore.SSHKeyTypes {
		labels = append(labels, k.Label)
		if k.ID == f.keyType {
			selected = i
		}
	}
	typ := gtk.NewDropDownFromStrings(labels)
	typ.SetSelected(uint(selected))
	typ.SetHAlign(gtk.AlignStart)
	typ.NotifyProperty("selected", func() {
		f.keyType = uicore.SSHKeyTypes[min(int(typ.Selected()), len(uicore.SSHKeyTypes)-1)].ID
		if !f.nameTouched {
			f.suggestName()
		}
	})
	add(generateHeading("Type"))
	add(typ)

	add(generateHeading("File name"))
	f.nameEntry = gtk.NewEntry()
	f.nameEntry.SetText(f.name)
	f.nameEntry.ConnectChanged(func() {
		f.name = f.nameEntry.Text()
		if !f.naming {
			f.nameTouched = true
		}
		f.refresh()
	})
	add(f.nameEntry)
	f.saved = generateNote("")
	add(f.saved)

	add(generateHeading("Comment"))
	comment := gtk.NewEntry()
	comment.SetText(f.comment)
	comment.SetPlaceholderText("you@this-computer")
	comment.ConnectChanged(func() { f.comment = comment.Text() })
	add(comment)
	add(generateNote("A label stored with the key, e.g. which account or machine it's for."))

	add(generateHeading("Passphrase"))
	secret := func(text, placeholder string, set func(string)) *gtk.Entry {
		e := gtk.NewEntry()
		e.SetText(text)
		e.SetPlaceholderText(placeholder)
		e.SetVisibility(f.showPass)
		e.SetInputPurpose(gtk.InputPurposePassword)
		e.SetHExpand(true)
		e.ConnectChanged(func() {
			set(e.Text())
			f.refresh()
		})
		return e
	}
	p1 := secret(f.passphrase, "none", func(s string) { f.passphrase = s })
	eyeIcon, eyeTip := "view-reveal-symbolic", "Show"
	if f.showPass {
		eyeIcon, eyeTip = "view-conceal-symbolic", "Hide"
	}
	eye := sshToolButton(eyeIcon, eyeTip, func() {
		f.showPass = !f.showPass
		f.render()
	})
	add(sshRowOf(p1, eye))
	add(secret(f.confirm, "again", func(s string) { f.confirm = s }))
	add(generateNote("Encrypts the key file, so a copy of it is useless without the passphrase. Leave both empty for none."))
	f.copyLink = generateLink("Copy", func() { f.w.copySecret(f.passphrase, "passphrase") })
	links := gtk.NewBox(gtk.OrientationHorizontal, 4)
	links.Append(generateLink("Suggest one", f.suggestPassphrase))
	links.Append(f.copyLink)
	add(links)
	f.problem = wrapLabel("", "error")
	f.problem.AddCSSClass("caption")
	add(f.problem)

	agent := gtk.NewCheckButtonWithLabel("Load it into ssh-agent")
	agent.SetActive(f.addToAgent)
	agent.ConnectToggled(func() { f.addToAgent = agent.Active() })
	add(agent)
	if f.err != "" {
		add(wrapLabel(f.err, "error"))
	}
	label := "Generate Key"
	if f.busy {
		label = "Generating…"
	}
	f.submit = generateButton(label, true, f.generate)
	add(f.submit)
	add(generateNote("The passphrase is used once, to encrypt the file, and isn't stored. Prefer it never pass through wtf?"))
	add(generateLink("Generate in a terminal instead", func() { generateSSH(f.w).showKeyGen() }))
	f.refresh()
}

// refresh updates what follows typing: the file note, mismatch, the button.
func (f *generateKeyForm) refresh() {
	if f.submit == nil {
		return
	}
	n := strings.TrimSpace(f.name)
	shown := n
	if shown == "" {
		shown = "…"
	}
	f.saved.SetText("Saved as ~/.ssh/" + shown + " and .pub. Existing files are never replaced.")
	differ := f.passphrase != "" && f.confirm != "" && f.passphrase != f.confirm
	short := f.passphrase != "" && utf8.RuneCountInString(f.passphrase) < 5
	switch {
	case differ:
		f.problem.SetText("The two passphrases differ.")
	case short:
		f.problem.SetText("Use at least 5 characters, or none.")
	default:
		f.problem.SetText("")
	}
	f.problem.SetVisible(differ || short)
	f.copyLink.SetVisible(f.passphrase != "" && !differ)
	f.submit.SetSensitive(n != "" && !f.busy && f.passphrase == f.confirm && !short)
}

func (f *generateKeyForm) suggestPassphrase() {
	o := uicore.NewPasswordOptions()
	o.Symbols = false
	o.AvoidAmbiguous = true
	f.passphrase = uicore.GeneratePassword(o, nil)
	f.confirm = f.passphrase
	hidden := f.w.app.settings.hide
	f.showPass = !hidden
	if hidden {
		f.w.toast("Passphrase filled in. Hidden while Hide Secrets is on: copy it to your password manager")
	}
	f.render()
}

func (f *generateKeyForm) generate() {
	if f.submit == nil || !f.submit.Sensitive() {
		return
	}
	f.busy, f.err = true, ""
	f.render()
	req := sshhosts.KeyGenRequest{Type: f.keyType, Name: strings.TrimSpace(f.name), Comment: strings.TrimSpace(f.comment),
		Passphrase: f.passphrase, AddToAgent: f.addToAgent}
	svc, home := f.w.app.be.SSHHosts, f.w.app.home
	go func() {
		r, err := svc.GenerateKey(req)
		onMainAsync(func() {
			f.busy = false
			if err != nil {
				f.err = err.Error()
			} else {
				f.passphrase, f.confirm, f.showPass = "", "", false
				f.result = &r
				f.w.toast("Created " + uicore.PrettyPath(r.Path, home))
			}
			if f.closed {
				return
			}
			if err == nil {
				f.loadKeys()
			}
			f.render()
		})
	}()
}

func (f *generateKeyForm) renderResult(r sshhosts.KeyGenResult) {
	w, add := f.w, f.root.Append
	pretty := uicore.PrettyPath(r.Path, w.app.home)
	done := wrapLabel("✓ Created "+pretty+" and .pub", "success")
	done.AddCSSClass("heading")
	add(done)
	info := r.Fingerprint
	switch {
	case r.InAgent:
		info += "\nLoaded into ssh-agent until it restarts."
	case r.AgentError != "":
		info += "\nNot loaded into ssh-agent: " + r.AgentError
	}
	fp := generateNote(info)
	fp.AddCSSClass("monospace")
	fp.SetSelectable(true)
	add(fp)
	add(generateHeading("Public key: this is what servers and GitHub need"))
	pub := wrapLabel(r.PublicKey, "monospace")
	pub.AddCSSClass("caption")
	pub.SetWrapMode(pango.WrapChar)
	pub.SetSelectable(true)
	add(pub)
	copyPublic := func() {
		w.win.Clipboard().SetText(r.PublicKey)
		w.toast("Copied the public key")
	}
	add(generateButton("Copy Public Key", true, copyPublic))
	add(generateButton("Add to GitHub…", false, func() {
		copyPublic()
		sshOpenURL(w, "https://github.com/settings/ssh/new")
	}))
	add(generateButton("Install on Host…", false, func() {
		generateSSH(w).showKeyInstall(&sshhosts.Key{Path: r.Path}, nil)
	}))
	add(generateButton("Use for a Git Identity…", false, func() { generateSSH(w).showIdentityEditor(nil, r.Path) }))
	// No keychain on Linux: ssh-add asks for the passphrase in the shell.
	if !r.InAgent {
		load := generateButton("Load into ssh-agent", false, func() {
			w.runInPane(uicore.SSHAgentAdd("~/.ssh/"+uicore.ShellQuote(filepath.Base(r.Path)), false))
		})
		load.SetTooltipText("Runs ssh-add in your shell, which asks for the passphrase")
		add(load)
	}
	add(generateLink("Make another key", func() {
		f.result, f.nameTouched, f.comment = nil, false, ""
		f.suggestName()
		f.render()
	}))
}

// Password

// generatePasswordOptions last while the app runs.
var generatePasswordOptions = uicore.NewPasswordOptions()

// generatePasswordForm makes random passwords from the system's secure
// source: never saved, never sent. Like the macOS app's
// PasswordGenerator.
type generatePasswordForm struct {
	w        *Window
	root     *gtk.Box
	password string
	revealed bool
	setting  bool
	off      func()

	output                   *gtk.Label
	bits, lengthLabel        *gtk.Label
	scale                    *gtk.Scale
	number                   *gtk.SpinButton
	copyBtn, newBtn, showBtn *gtk.Button
}

func newGeneratePasswordForm(w *Window) *generatePasswordForm {
	g := &generatePasswordForm{w: w, root: generateForm()}
	o := &generatePasswordOptions
	add := g.root.Append
	g.output = wrapLabel("", "wtf-gen-password")
	g.output.SetWrapMode(pango.WrapChar)
	add(g.output)
	g.copyBtn = generateButton("Copy", true, func() {
		if g.password != "" {
			w.copySecret(g.password, "password")
		}
	})
	g.newBtn = generateButton("New", false, g.regenerate)
	g.newBtn.SetTooltipText("Make another")
	g.showBtn = generateButton("Show", false, func() {
		g.revealed = true
		g.update()
	})
	add(sshRowOf(g.copyBtn, g.newBtn, g.showBtn))
	g.bits = generateNote("")
	add(g.bits)
	g.lengthLabel = generateHeading("")
	add(g.lengthLabel)
	g.scale = gtk.NewScaleWithRange(gtk.OrientationHorizontal, uicore.PasswordMinLength, 64, 1)
	g.scale.SetHExpand(true)
	g.scale.ConnectValueChanged(func() {
		if !g.setting {
			o.Length = int(g.scale.Value() + 0.5)
			g.regenerate()
		}
	})
	g.number = gtk.NewSpinButtonWithRange(uicore.PasswordMinLength, uicore.PasswordMaxLength, 1)
	g.number.SetNumeric(true)
	g.number.ConnectValueChanged(func() {
		if !g.setting {
			o.Length = uicore.PasswordClamp(g.number.ValueAsInt())
			g.regenerate()
		}
	})
	add(sshRowOf(g.scale, g.number))
	for _, b := range []struct {
		label string
		on    *bool
	}{
		{"Lowercase a–z", &o.Lower}, {"Uppercase A–Z", &o.Upper}, {"Digits 0–9", &o.Digits},
		{"Symbols !#$%&*+-.:;=?@^_~ …", &o.Symbols}, {"Leave out look-alikes I l 1 O 0 o |", &o.AvoidAmbiguous},
	} {
		c := gtk.NewCheckButtonWithLabel(b.label)
		c.SetActive(*b.on)
		on := b.on
		c.ConnectToggled(func() {
			*on = c.Active()
			g.regenerate()
		})
		add(c)
	}
	add(generateNote("Made on this computer from the system's secure random source. Never saved or sent anywhere. No quotes, spaces or backslashes, so it pastes into shells and config files as-is."))
	// Turning Hide Secrets on covers the password again.
	g.off = w.app.settings.observe(func() {
		if w.app.settings.hide {
			g.revealed = false
		}
		g.update()
	})
	g.regenerate()
	return g
}

func (g *generatePasswordForm) regenerate() {
	g.password = uicore.GeneratePassword(generatePasswordOptions, nil)
	g.revealed = false
	g.update()
}

func (g *generatePasswordForm) masked() bool { return g.w.app.settings.hide && !g.revealed }

func (g *generatePasswordForm) update() {
	o := generatePasswordOptions
	none := len(uicore.PasswordClasses(o)) == 0
	switch {
	case none:
		g.output.SetText("Choose at least one kind of character")
		g.output.AddCSSClass("dim-label")
		g.output.SetSelectable(false)
		g.output.SetTooltipText("")
	case g.masked():
		g.output.SetText(strings.Repeat("•", min(utf8.RuneCountInString(g.password), 32)))
		g.output.RemoveCSSClass("dim-label")
		g.output.SetSelectable(false)
		g.output.SetTooltipText("Hidden while Hide Secrets is on")
	default:
		g.output.SetText(g.password)
		g.output.RemoveCSSClass("dim-label")
		g.output.SetSelectable(true)
		g.output.SetTooltipText("")
	}
	g.copyBtn.SetSensitive(g.password != "")
	g.newBtn.SetSensitive(!none)
	g.showBtn.SetVisible(g.masked() && !none)
	g.bits.SetText("")
	if !none {
		g.bits.SetText(fmt.Sprintf("About %d bits of randomness", uicore.PasswordEntropyBits(o)))
	}
	g.lengthLabel.SetText(fmt.Sprintf("Length: %d", o.Length))
	g.setting = true
	g.scale.SetValue(float64(min(64, o.Length)))
	g.number.SetValue(float64(o.Length))
	g.setting = false
}

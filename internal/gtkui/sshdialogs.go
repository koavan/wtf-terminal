//go:build linux

package gtkui

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"

	"github.com/dayanraj/wtf/internal/sshhosts"
	"github.com/dayanraj/wtf/internal/uicore"
)

// The SSH tab's dialogs: host editor, scp transfer, key install, key
// generation in a terminal, tunnel editor, host keys and git identities.
// Strings and rules follow the macOS app's SshDialogs.

// sshForm is a dialog with a form: an AdwMessageDialog whose extra child
// holds the fields, and an error line. Answering hides it rather than
// destroying it, so a save that fails shows why and comes back.
type sshForm struct {
	d    *adw.MessageDialog
	box  *gtk.Box
	err  *gtk.Label
	runs map[string]func()
}

func newSSHForm(w *Window, title, subtitle string, width int) *sshForm {
	f := &sshForm{runs: map[string]func(){}}
	f.d = adw.NewMessageDialog(&w.win.Window, title, subtitle)
	f.d.SetHideOnClose(true)
	f.d.SetCloseResponse("cancel")
	f.box = gtk.NewBox(gtk.OrientationVertical, 10)
	f.box.SetSizeRequest(width, -1)
	f.err = f.label("", false)
	f.err.RemoveCSSClass("dim-label")
	f.err.AddCSSClass("error")
	f.d.SetExtraChild(f.box)
	f.d.ConnectResponse(func(r string) {
		if run := f.runs[r]; run != nil {
			f.setError("")
			run()
			return
		}
		f.close()
	})
	return f
}

// button adds a response; one without run just closes the dialog.
func (f *sshForm) button(id, label string, look adw.ResponseAppearance, run func()) {
	f.d.AddResponse(id, label)
	f.d.SetResponseAppearance(id, look)
	if look == adw.ResponseSuggested {
		f.d.SetDefaultResponse(id)
	}
	if run != nil {
		f.runs[id] = run
	}
}

func (f *sshForm) enable(id string, on bool) { f.d.SetResponseEnabled(id, on) }

// label is a wrapping note, monospaced and selectable for commands.
func (f *sshForm) label(text string, mono bool) *gtk.Label {
	l := gtk.NewLabel(text)
	l.SetXAlign(0)
	l.SetWrap(true)
	// As wide as the form, never wider.
	l.SetMaxWidthChars(1)
	l.SetHExpand(true)
	if mono {
		l.AddCSSClass("monospace")
		l.SetWrapMode(pango.WrapWordChar)
		l.SetSelectable(true)
	} else {
		l.AddCSSClass("dim-label")
		l.AddCSSClass("caption")
	}
	return l
}

func (f *sshForm) add(child gtk.Widgetter) { f.box.Append(child) }

// sshFormRow is a labelled field, to hide or relabel.
type sshFormRow struct {
	root  *gtk.Box
	label *gtk.Label
}

// field adds a labelled control, with optional help under it.
func (f *sshForm) field(label string, control gtk.Widgetter, help string) *sshFormRow {
	r := &sshFormRow{root: gtk.NewBox(gtk.OrientationVertical, 4), label: gtk.NewLabel(label)}
	r.label.SetXAlign(0)
	r.label.AddCSSClass("caption-heading")
	r.root.Append(r.label)
	r.root.Append(control)
	if help != "" {
		r.root.Append(f.label(help, false))
	}
	f.add(r.root)
	return r
}

func (f *sshForm) entry(text, placeholder string) *gtk.Entry {
	e := gtk.NewEntry()
	e.SetText(text)
	e.SetPlaceholderText(placeholder)
	e.SetActivatesDefault(true)
	e.SetHExpand(true)
	return e
}

// sshNarrow makes an entry port-sized.
func sshNarrow(e *gtk.Entry) *gtk.Entry {
	e.SetHExpand(false)
	e.SetWidthChars(6)
	return e
}

func (f *sshForm) check(label string, on bool) *gtk.CheckButton {
	c := gtk.NewCheckButtonWithLabel(label)
	c.SetActive(on)
	return c
}

// sshRowOf lays controls side by side.
func sshRowOf(children ...gtk.Widgetter) *gtk.Box {
	b := gtk.NewBox(gtk.OrientationHorizontal, 8)
	for _, c := range children {
		b.Append(c)
	}
	return b
}

// sshColumn is a label over a control, for fields side by side.
func sshColumn(label string, control gtk.Widgetter) *gtk.Box {
	b := gtk.NewBox(gtk.OrientationVertical, 4)
	l := gtk.NewLabel(label)
	l.SetXAlign(0)
	l.AddCSSClass("caption-heading")
	b.Append(l)
	b.Append(control)
	return b
}

// sshCombo is a text field with suggestions under its arrow; the field
// is the box's first child.
func sshCombo(choices []string, text, placeholder string) (*gtk.Box, *gtk.Entry) {
	e := gtk.NewEntry()
	e.SetText(text)
	e.SetPlaceholderText(placeholder)
	e.SetActivatesDefault(true)
	e.SetHExpand(true)
	box := gtk.NewBox(gtk.OrientationHorizontal, 0)
	box.AddCSSClass("linked")
	box.SetHExpand(true)
	box.Append(e)
	if len(choices) == 0 {
		return box, e
	}
	list := gtk.NewListBox()
	list.SetActivateOnSingleClick(true)
	for _, c := range choices {
		l := gtk.NewLabel(c)
		l.SetXAlign(0)
		l.SetMarginStart(6)
		l.SetMarginEnd(6)
		list.Append(l)
	}
	scroll := gtk.NewScrolledWindow()
	scroll.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	scroll.SetMaxContentHeight(240)
	scroll.SetPropagateNaturalHeight(true)
	scroll.SetChild(list)
	pop := gtk.NewPopover()
	pop.SetChild(scroll)
	b := gtk.NewMenuButton()
	b.SetIconName("pan-down-symbolic")
	b.SetPopover(pop)
	list.ConnectRowActivated(func(r *gtk.ListBoxRow) {
		e.SetText(choices[r.Index()])
		e.SetPosition(-1)
		pop.Popdown()
	})
	box.Append(b)
	return box, e
}

// setError shows an error under the form, or clears it.
func (f *sshForm) setError(msg string) {
	f.err.SetText(msg)
	f.err.SetVisible(msg != "")
}

// fail shows the error and the dialog again.
func (f *sshForm) fail(msg string) {
	f.setError(msg)
	f.d.Present()
}

func (f *sshForm) close() { f.d.Destroy() }

func (f *sshForm) present(focus gtk.Widgetter) {
	f.add(f.err)
	f.setError("")
	f.d.Present()
	if focus != nil {
		gtk.BaseWidget(focus).GrabFocus()
	}
}

func sshTextOf(v *gtk.TextView) string {
	b := v.Buffer()
	return b.Text(b.StartIter(), b.EndIter(), false)
}

// Host editor

func (t *sshTab) showHostEditor(h *sshhosts.Host, prefill *uicore.HostDraft) {
	w := t.w
	main := "~/.ssh/config"
	if t.list.MainConfig != "" {
		main = t.pretty(t.list.MainConfig)
	}
	title, sub := "New SSH Host", "Added to "+main
	switch {
	case h != nil:
		title, sub = "Edit "+h.Alias, fmt.Sprintf("In %s, line %d", t.pretty(h.Source), h.Line)
	case prefill != nil && prefill.HostName != "":
		title = "Save Known Host"
	}
	f := newSSHForm(w, title, sub, 480)
	d := uicore.NewHostDraft(h)
	if prefill != nil {
		d = *prefill
	}
	var cur sshhosts.Host
	if h != nil {
		cur = *h
	}
	// A value set by another entry that matches this host shows as the
	// placeholder.
	inherited := func(key, value, fallback string) string {
		own := slices.ContainsFunc(cur.Options, func(o sshhosts.Option) bool { return strings.EqualFold(o.Key, key) })
		if h == nil || own || value == "" || value == h.Alias {
			return fallback
		}
		return value + " (from another entry)"
	}
	name := f.entry(d.Name, "web1")
	address := f.entry(d.HostName, inherited("HostName", cur.HostName, "web1.example.com or 10.0.0.5"))
	user := f.entry(d.User, inherited("User", cur.User, "your user name"))
	port := sshNarrow(f.entry(d.Port, inherited("Port", cur.Port, "22")))
	keyBox, key := sshCombo(t.keyFiles, d.IdentityFile, "your default keys")
	var aliases []string
	for _, x := range t.list.Hosts {
		if !x.Known && (h == nil || x.Alias != h.Alias) {
			aliases = append(aliases, x.Alias)
		}
	}
	jumpBox, jump := sshCombo(aliases, d.ProxyJump, inherited("ProxyJump", cur.ProxyJump, "none"))
	more := gtk.NewTextView()
	more.SetMonospace(true)
	more.SetWrapMode(gtk.WrapWordChar)
	more.Buffer().SetText(d.More)
	moreScroll := gtk.NewScrolledWindow()
	moreScroll.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	moreScroll.SetMinContentHeight(60)
	moreScroll.SetHasFrame(true)
	moreScroll.SetChild(more)
	// Enter makes a new line there; Ctrl+Enter saves.
	keys := gtk.NewEventControllerKey()
	keys.ConnectKeyPressed(func(keyval, _ uint, state gdk.ModifierType) bool {
		if (keyval == gdk.KEY_Return || keyval == gdk.KEY_KP_Enter) && state&gdk.ControlMask != 0 {
			f.d.Response("save")
			return true
		}
		return false
	})
	more.AddController(keys)

	f.field("Name", name, "What you type after ssh. Separate several names with spaces.")
	f.field("Address", address, "")
	user.SetHExpand(true)
	userCol := sshColumn("User", user)
	userCol.SetHExpand(true)
	f.add(sshRowOf(userCol, sshColumn("Port", port)))
	f.field("Key file", keyBox, "")
	f.field("Jump host", jumpBox, "")
	f.field("More settings", moreScroll, "One per line, as in an ssh config file. Ctrl+Enter saves.")

	save := func(connect bool) {
		draft := uicore.HostDraft{Name: name.Text(), HostName: address.Text(), User: user.Text(), Port: port.Text(),
			IdentityFile: key.Text(), ProxyJump: jump.Text(), More: sshTextOf(more)}
		options, err := draft.Options()
		if err != nil {
			f.fail(err.Error())
			return
		}
		patterns := strings.Fields(draft.Name)
		entry := sshhosts.Entry{Version: t.list.Versions[t.list.MainConfig], Patterns: patterns, Options: options}
		if h != nil {
			entry.File, entry.Line, entry.Version = h.Source, h.Line, t.list.Versions[h.Source]
		}
		svc := t.svc()
		go func() {
			l, err := svc.Save(entry)
			onMainAsync(func() {
				if err != nil {
					f.fail(err.Error())
					return
				}
				t.list = l
				t.changed()
				f.close()
				first := ""
				if len(patterns) > 0 {
					first = patterns[0]
				}
				if h != nil {
					w.toast("Saved " + first)
				} else {
					w.toast("Added " + first + " to " + main)
				}
				if !connect {
					return
				}
				for _, x := range l.Hosts {
					if !x.Known && x.Alias == first {
						t.openHosts([]sshhosts.Host{x}, sshOpenHere)
						return
					}
				}
			})
		}()
	}
	if h != nil {
		host := *h
		f.button("delete", "Delete…", adw.ResponseDestructive, func() {
			f.close()
			t.confirmDeleteHost(host)
		})
	}
	f.button("cancel", "Cancel", adw.ResponseDefault, nil)
	if h == nil {
		f.button("connect", "Save & Connect", adw.ResponseDefault, func() { save(true) })
	}
	f.button("save", "Save", adw.ResponseSuggested, func() { save(false) })
	f.present(name)
}

func (t *sshTab) confirmDeleteHost(h sshhosts.Host) {
	w := t.w
	var others []string
	for _, p := range h.Patterns {
		if p != h.Alias {
			others = append(others, p)
		}
	}
	msg := fmt.Sprintf("Removes its entry from %s (line %d).", t.pretty(h.Source), h.Line)
	if len(others) > 0 {
		msg += " The entry also names " + strings.Join(others, ", ") + ", which go with it."
	}
	msg += " A copy of the file is kept in wtf’s cache folder."
	w.app.confirm(&w.win.Window, "Delete "+h.Alias+"?", msg, "Delete Host", func() {
		svc, version := t.svc(), t.list.Versions[h.Source]
		go func() {
			l, err := svc.Delete(h.Source, h.Line, version)
			onMainAsync(func() {
				if err != nil {
					w.toast("Couldn’t delete " + h.Alias + ": " + err.Error())
					return
				}
				t.list = l
				t.changed()
				w.toast("Deleted " + h.Alias)
			})
		}()
	})
}

// scp

func (t *sshTab) showTransfer(h sshhosts.Host, upload bool, files []string) {
	w, home := t.w, t.w.app.home
	title, action, actionLabel := "Download from "+h.Alias, "download", "Download"
	if upload {
		title, action, actionLabel = "Upload to "+h.Alias, "upload", "Upload"
	}
	f := newSSHForm(w, title, "Copies with scp in your shell (a new tab if it’s busy), where you’ll see its progress", 460)
	locals := slices.Clone(files)
	localDir := filepath.Join(home, "Downloads")
	preview := f.label("", true)
	listBox := gtk.NewBox(gtk.OrientationVertical, 2)
	placeholder := "/var/log/app.log"
	if upload {
		placeholder = "your home folder"
	}
	remote := f.entry("", placeholder)
	localLabel := gtk.NewLabel(t.pretty(localDir))
	localLabel.SetXAlign(0)
	localLabel.SetHExpand(true)
	localLabel.SetEllipsize(pango.EllipsizeMiddle)

	command := func() string {
		if upload {
			if len(locals) == 0 {
				return ""
			}
			return uicore.SCPUpload(h, locals, remote.Text(), home)
		}
		r := strings.TrimSpace(remote.Text())
		if r == "" {
			return ""
		}
		return uicore.SCPDownload(h, r, localDir, home)
	}
	var refresh func()
	refresh = func() {
		// One row per file, each with a × to leave it out.
		for c := listBox.FirstChild(); c != nil; c = listBox.FirstChild() {
			listBox.Remove(c)
		}
		for _, l := range locals {
			name := gtk.NewLabel(t.pretty(l))
			name.SetXAlign(0)
			name.SetHExpand(true)
			name.AddCSSClass("monospace")
			name.SetEllipsize(pango.EllipsizeMiddle)
			x := gtk.NewButtonFromIconName("window-close-symbolic")
			x.AddCSSClass("flat")
			x.SetTooltipText("Remove")
			x.ConnectClicked(func() {
				locals = slices.DeleteFunc(locals, func(s string) bool { return s == l })
				refresh()
			})
			listBox.Append(sshRowOf(name, x))
		}
		preview.SetText(command())
		f.enable(action, command() != "")
	}
	parent := &f.d.Window
	chooseFiles := func() {
		fd := gtk.NewFileDialog()
		fd.SetTitle("Upload to " + h.Alias)
		fd.SetAcceptLabel("Choose")
		fd.OpenMultiple(context.Background(), parent, func(res gio.AsyncResulter) {
			l, err := fd.OpenMultipleFinish(res)
			if err != nil {
				return
			}
			for i := range l.NItems() {
				if p := (&gio.File{Object: l.Item(i)}).Path(); p != "" && !slices.Contains(locals, p) {
					locals = append(locals, p)
				}
			}
			refresh()
		})
	}
	chooseFolder := func() {
		fd := gtk.NewFileDialog()
		fd.SetAcceptLabel("Choose")
		if upload {
			fd.SetTitle("Upload to " + h.Alias)
		} else {
			fd.SetTitle("Download into")
			fd.SetInitialFolder(gio.NewFileForPath(localDir))
		}
		fd.SelectFolder(context.Background(), parent, func(res gio.AsyncResulter) {
			dir, err := fd.SelectFolderFinish(res)
			if err != nil || dir.Path() == "" {
				return
			}
			if upload {
				if !slices.Contains(locals, dir.Path()) {
					locals = append(locals, dir.Path())
				}
			} else {
				localDir = dir.Path()
				localLabel.SetText(t.pretty(localDir))
			}
			refresh()
		})
	}
	button := func(label string, run func()) *gtk.Button {
		b := gtk.NewButtonWithLabel(label)
		b.ConnectClicked(run)
		return b
	}
	if upload {
		col := gtk.NewBox(gtk.OrientationVertical, 4)
		col.Append(listBox)
		col.Append(sshRowOf(button("Choose Files…", chooseFiles), button("Choose Folder…", chooseFolder)))
		f.field("Files and folders", col, "")
		f.field("Into folder on "+h.Alias, remote, "A path on the server, like /tmp or ~/deploy. Empty means your home folder.")
	} else {
		f.field("File or folder on "+h.Alias, remote, "")
		f.field("Into folder here", sshRowOf(localLabel, button("Choose…", chooseFolder)), "")
	}
	f.add(preview)
	remote.ConnectChanged(refresh)
	f.button("cancel", "Cancel", adw.ResponseDefault, nil)
	f.button(action, actionLabel, adw.ResponseSuggested, func() {
		c := command()
		if c == "" {
			f.fail("")
			return
		}
		f.close()
		w.runInPane(c)
	})
	f.present(remote)
	refresh()
	if upload && len(files) == 0 {
		chooseFiles()
	}
}

// Keys

func (t *sshTab) showKeyInstall(key *sshhosts.Key, host *sshhosts.Host) {
	t.whenLoaded(true, true, func() { t.buildKeyInstall(key, host) })
}

func (t *sshTab) buildKeyInstall(key *sshhosts.Key, host *sshhosts.Host) {
	w, home := t.w, t.w.app.home
	var keys []sshhosts.Key
	for _, k := range t.keys.Keys {
		if k.Path != "" {
			keys = append(keys, k)
		}
	}
	hosts := t.list.Hosts
	f := newSSHForm(w, "Install Public Key", "Adds the key to the server’s ~/.ssh/authorized_keys, so you can log in with it", 480)
	var keyLabels []string
	defaultKey := -1
	for i, k := range keys {
		label := k.Name + " — " + k.Type
		if k.Comment != "" {
			label += " · " + k.Comment
		}
		keyLabels = append(keyLabels, label)
		if key != nil && k.Path == key.Path {
			defaultKey = i
		}
	}
	if defaultKey < 0 {
		defaultKey = max(0, slices.IndexFunc(keys, func(k sshhosts.Key) bool { return k.InAgent }))
	}
	keyPop := gtk.NewDropDownFromStrings(keyLabels)
	keyPop.SetSelected(uint(defaultKey))
	hostLabels := []string{"Choose a host"}
	selected := 0
	for i, x := range hosts {
		label := x.Alias
		if x.Known && x.Port != "" {
			label += ":" + x.Port
		}
		hostLabels = append(hostLabels, label)
		if host != nil && uicore.SSHHostID(x) == uicore.SSHHostID(*host) {
			selected = i + 1
		}
	}
	hostPop := gtk.NewDropDownFromStrings(hostLabels)
	hostPop.SetSelected(uint(selected))
	preview := f.label("", true)
	current := func() (sshhosts.Key, sshhosts.Host, bool) {
		ki, hi := int(keyPop.Selected()), int(hostPop.Selected())-1
		if ki < 0 || ki >= len(keys) || hi < 0 || hi >= len(hosts) {
			return sshhosts.Key{}, sshhosts.Host{}, false
		}
		return keys[ki], hosts[hi], true
	}
	refresh := func() {
		k, h, ok := current()
		preview.SetText("")
		if ok {
			preview.SetText(uicore.SSHInstallKey(k.Path, h, home))
		}
		f.enable("install", ok)
	}
	keyPop.NotifyProperty("selected", refresh)
	hostPop.NotifyProperty("selected", refresh)
	f.field("Key", keyPop, "")
	f.field("Host", hostPop, "")
	f.add(f.label("ssh-copy-id runs in your shell (a new tab if it’s busy) and asks for your password on the server if it needs it. Keys already there aren’t added twice.", false))
	f.add(preview)
	f.button("cancel", "Cancel", adw.ResponseDefault, nil)
	f.button("install", "Install in Terminal", adw.ResponseSuggested, func() {
		k, h, ok := current()
		if !ok {
			f.fail("")
			return
		}
		run := func() {
			f.close()
			w.runInPane(uicore.SSHInstallKey(k.Path, h, home))
		}
		if k.HasPub {
			run()
			return
		}
		svc := t.svc()
		go func() {
			err := svc.WritePublicKey(k.Path)
			onMainAsync(func() {
				if err != nil {
					f.fail(err.Error())
					return
				}
				run()
			})
		}()
	})
	f.present(nil)
	refresh()
}

// showKeyGen is New SSH Key: ssh-keygen in a terminal, which asks for the
// passphrase there.
func (t *sshTab) showKeyGen() { t.whenLoaded(true, false, t.buildKeyGen) }

func (t *sshTab) buildKeyGen() {
	w := t.w
	taken := map[string]bool{}
	for _, k := range t.keys.Keys {
		taken[t.pretty(k.Path)] = true
	}
	f := newSSHForm(w, "New SSH Key", "Creates a key pair in ~/.ssh", 460)
	var labels []string
	for _, k := range uicore.SSHKeyTypes {
		labels = append(labels, k.Label)
	}
	typ := gtk.NewDropDownFromStrings(labels)
	keyType := func() uicore.SSHKeyType {
		return uicore.SSHKeyTypes[min(int(typ.Selected()), len(uicore.SSHKeyTypes)-1)]
	}
	name := f.entry(uicore.FreeKeyName("id_ed25519", taken), "")
	comment := f.entry("", "you@this-computer")
	agent := f.check("Load it into ssh-agent afterwards", true)
	preview := f.label("", true)
	savedAs := f.label("", false)
	// The name follows the type until it's typed in.
	touched, naming := false, false
	refresh := func() {
		n := strings.TrimSpace(name.Text())
		preview.SetText(uicore.SSHKeygen(keyType().ID, name.Text(), comment.Text(), agent.Active(), false))
		f.enable("generate", n != "")
		if n == "" {
			n = "…"
		}
		savedAs.SetText("Saved as ~/.ssh/" + n + " and .pub")
	}
	name.ConnectChanged(func() {
		if !naming {
			touched = true
		}
		refresh()
	})
	comment.ConnectChanged(refresh)
	agent.ConnectToggled(refresh)
	typ.NotifyProperty("selected", func() {
		if !touched {
			naming = true
			name.SetText(uicore.FreeKeyName(keyType().File, taken))
			naming = false
		}
		refresh()
	})
	f.field("Type", typ, "")
	f.field("File name", name, "")
	f.add(savedAs)
	f.field("Comment", comment, "A label stored with the key, shown in authorized_keys on servers.")
	f.add(agent)
	f.add(f.label("ssh-keygen runs in your shell (a new tab if it’s busy) and asks for a passphrase there. Leave it empty for none.", false))
	f.add(preview)
	f.button("cancel", "Cancel", adw.ResponseDefault, nil)
	f.button("generate", "Generate in Terminal", adw.ResponseSuggested, func() {
		n, cmd := strings.TrimSpace(name.Text()), preview.Text()
		if n == "" {
			f.fail("")
			return
		}
		svc := t.svc()
		go func() {
			free, err := svc.KeyNameFree(n)
			onMainAsync(func() {
				switch {
				case err != nil:
					f.fail(err.Error())
				case !free:
					f.fail("~/.ssh/" + n + " already exists — choose another name")
				default:
					f.close()
					w.runInPane(cmd)
				}
			})
		}()
	})
	f.present(name)
	refresh()
}

// Tunnels

var tunnelKinds = []struct{ id, label string }{
	{"local", "Local -L"}, {"remote", "Remote -R"}, {"dynamic", "SOCKS -D"}, {"kubectl", "Kubernetes port-forward"},
}

var tunnelExplain = map[string]string{
	"local":   "Reach a service near the server — a database, an internal web app — on a port of this computer.",
	"remote":  "Let the server reach a service on this computer, through a port on the server.",
	"dynamic": "A SOCKS proxy on this computer that sends traffic out through the server — point a browser at it.",
	"kubectl": "Reach a pod, service or deployment in a cluster on a port of this computer. If its pod restarts, the forward reconnects.",
}

// showTunnelEditor edits a tunnel, or makes one (through host, or from
// prefill). "Through host" lists your ssh hosts, so they load first.
func (t *sshTab) showTunnelEditor(x *sshhosts.TunnelStatus, host *sshhosts.Host, prefill *uicore.TunnelDraft) {
	t.whenLoaded(false, true, func() { t.buildTunnelEditor(x, host, prefill) })
}

func (t *sshTab) buildTunnelEditor(x *sshhosts.TunnelStatus, host *sshhosts.Host, prefill *uicore.TunnelDraft) {
	w := t.w
	var d uicore.TunnelDraft
	switch {
	case x != nil:
		d = uicore.TunnelDraftOf(*x)
	case prefill != nil:
		d = *prefill
	case host != nil:
		port := ""
		if host.Known && host.Port != "22" {
			port = host.Port
		}
		d = uicore.NewTunnelDraft(host.Alias, port)
	default:
		d = uicore.NewTunnelDraft("", "")
	}
	editing := x != nil
	title, sub := "New Port Forward", "A port forward over ssh, run in the background while it’s on"
	if editing {
		title = "Edit Port Forward"
	}
	if d.Kind == "kubectl" {
		sub = "A kubectl port-forward, run in the background while it’s on"
	}
	f := newSSHForm(w, title, sub, 500)
	var kindLabels []string
	selected := 0
	for i, k := range tunnelKinds {
		kindLabels = append(kindLabels, k.label)
		if k.id == d.Kind {
			selected = i
		}
	}
	kind := gtk.NewDropDownFromStrings(kindLabels)
	kind.SetSelected(uint(selected))
	explainer := f.label("", false)
	var aliases []string
	for _, h := range t.list.Hosts {
		if !h.Known {
			aliases = append(aliases, h.Alias)
		}
	}
	hostBox, hostField := sshCombo(aliases, d.Host, "bastion")
	portField := sshNarrow(f.entry(d.Port, "config"))
	listen := f.entry(d.ListenPort, "8080")
	destHost := f.entry(d.DestHost, "localhost")
	destPort := sshNarrow(f.entry(d.DestPort, "80"))
	kubeContext := f.entry(d.Context, "kubectl context")
	namespace := f.entry(d.Namespace, "default")
	resource := f.entry(d.Resource, "svc/web, deploy/api or pod/name")
	// Its own field: a widget lives in one row only.
	targetPort := sshNarrow(f.entry(d.DestPort, "80"))
	name := f.entry(d.Name, "optional, e.g. staging db")
	auto := f.check("Start when wtf opens", d.AutoStart)
	preview := f.label("", true)
	hint := f.label("", false)

	f.field("Kind", kind, "")
	f.add(explainer)
	sshRow := f.field("Through host and ssh port", sshRowOf(hostBox, portField), "")
	kubeRow := f.field("Context and namespace", sshRowOf(kubeContext, namespace), "")
	listenRow := f.field("Port here", listen, "")
	destRow := f.field("Destination", sshRowOf(destHost, destPort), "")
	resRow := f.field("Target and its port", sshRowOf(resource, targetPort), "")
	f.field("Name", name, "")
	f.add(auto)
	f.add(preview)
	f.add(hint)

	paneEnv := func() map[string]string {
		if ps := w.model.ActivePane(); ps != nil {
			return ps.Env
		}
		return nil
	}
	read := func() {
		d.Kind = tunnelKinds[min(int(kind.Selected()), len(tunnelKinds)-1)].id
		d.Host, d.Port = hostField.Text(), portField.Text()
		d.ListenPort, d.DestHost, d.DestPort = listen.Text(), destHost.Text(), destPort.Text()
		if d.Kind == "kubectl" {
			d.DestPort = targetPort.Text()
		}
		d.Context, d.Namespace, d.Resource = kubeContext.Text(), namespace.Text(), resource.Text()
		d.Name, d.AutoStart = name.Text(), auto.Active()
	}
	or := func(s, fallback string) string {
		if s == "" {
			return fallback
		}
		return s
	}
	refresh := func() {
		read()
		k := d.Kind
		kube := k == "kubectl"
		explainer.SetText(tunnelExplain[k])
		sshRow.root.SetVisible(!kube)
		kubeRow.root.SetVisible(kube)
		resRow.root.SetVisible(kube)
		destRow.root.SetVisible(!kube && k != "dynamic")
		switch k {
		case "dynamic":
			listenRow.label.SetText("SOCKS port here")
		case "remote":
			listenRow.label.SetText("Port on server")
		default:
			listenRow.label.SetText("Port here")
		}
		if k == "remote" {
			destRow.label.SetText("Destination, as seen from here")
		} else {
			destRow.label.SetText("Destination, as seen from " + or(strings.TrimSpace(d.Host), "the host"))
		}
		if !editing {
			f.enable("run", kube)
		}
		spec := or(d.Spec(), "…")
		var h string
		if kube {
			env := d.Env
			if len(env) == 0 {
				env = uicore.KubectlTunnelEnv(paneEnv())
			}
			prefix := ""
			if v, ok := env["KUBECONFIG"]; ok {
				prefix += "KUBECONFIG=" + uicore.ShellQuote(v) + " "
			}
			if v, ok := env["AWS_PROFILE"]; ok {
				prefix += "AWS_PROFILE=" + uicore.ShellQuote(v) + " "
			}
			preview.SetText(prefix + "kubectl --context " + uicore.ShellQuote(or(d.Context, "…")) + " --namespace " + uicore.ShellQuote(or(d.Namespace, "…")) +
				" port-forward " + or(d.Resource, "…") + " " + spec)
			whose := "focused shell’s"
			if editing {
				whose = "saved"
			}
			h = "Runs with the " + whose + " KUBECONFIG, AWS profile and PATH, so it reaches the same cluster. Listens on 127.0.0.1 only."
		} else {
			flag := "-L"
			switch k {
			case "dynamic":
				flag = "-D"
			case "remote":
				flag = "-R"
			}
			p := ""
			if d.Port != "" {
				p = "-p " + d.Port + " "
			}
			preview.SetText("ssh -N " + flag + " " + spec + " " + p + or(d.Host, "…"))
			h = "Runs without prompts, so the host must accept a key from ssh-agent or a key without a passphrase."
		}
		if editing && x.State != sshhosts.TunnelStopped && x.State != sshhosts.TunnelFailed {
			h += " Saving restarts it."
		}
		hint.SetText(h)
	}
	for _, e := range []*gtk.Entry{hostField, portField, listen, destHost, destPort, targetPort, kubeContext, namespace, resource, name} {
		e.ConnectChanged(refresh)
	}
	kind.NotifyProperty("selected", refresh)

	save := func(start bool) {
		read()
		if d.Kind == "kubectl" && !editing && len(d.Env) == 0 {
			d.Env = uicore.KubectlTunnelEnv(paneEnv())
		}
		tn := sshhosts.Tunnel{Name: strings.TrimSpace(d.Name), Host: strings.TrimSpace(d.Host), Port: strings.TrimSpace(d.Port),
			Kind: d.Kind, Spec: d.Spec(), AutoStart: d.AutoStart, Context: strings.TrimSpace(d.Context),
			Namespace: strings.TrimSpace(d.Namespace), Resource: strings.TrimSpace(d.Resource), Env: d.Env}
		if editing {
			tn.ID = x.ID
		}
		svc := t.svc()
		go func() {
			l, err := svc.SaveTunnel(tn)
			if err == nil && start {
				id := tn.ID
				if id == "" && len(l) > 0 {
					id = l[len(l)-1].ID
				}
				l, err = svc.StartTunnel(id)
			}
			onMainAsync(func() {
				if err != nil {
					f.fail(err.Error())
					return
				}
				t.setTunnels(l)
				f.close()
				switch {
				case start:
				case editing:
					w.toast("Saved tunnel")
				default:
					w.toast("Added tunnel")
				}
			})
		}()
	}
	f.button("cancel", "Cancel", adw.ResponseDefault, nil)
	if editing {
		f.button("save", "Save", adw.ResponseSuggested, func() { save(false) })
	} else {
		// A one-off kubectl port-forward in a new tab, not saved.
		f.button("run", "Run in Terminal", adw.ResponseDefault, func() {
			read()
			if d.Kind != "kubectl" {
				f.fail("")
				return
			}
			f.close()
			w.model.Open(uicore.OpenOptions{Command: preview.Text()})
		})
		f.button("save", "Save", adw.ResponseDefault, func() { save(false) })
		f.button("start", "Save & Start", adw.ResponseSuggested, func() { save(true) })
	}
	if d.Kind == "kubectl" {
		f.present(kubeContext)
	} else {
		f.present(hostField)
	}
	refresh()
}

// Host keys

func (t *sshTab) showHostKeys(h sshhosts.Host) {
	w := t.w
	port := ""
	if h.Known {
		port = h.Port
	}
	f := newSSHForm(w, "Host Keys — "+h.Alias, "Reading known_hosts…", 520)
	// Fingerprints are selectable, to copy and compare.
	storedList := f.label("", true)
	scanNote := f.label("", false)
	scanList := f.label("", true)
	var info *sshhosts.HostKeys
	var scanned []sshhosts.HostKey
	reallyForget := false
	scan := gtk.NewButtonWithLabel("Scan")
	trust := gtk.NewButtonWithLabel("Trust Server’s Keys")
	trust.AddCSSClass("suggested-action")
	trust.SetTooltipText("Replaces the stored keys with these")
	forget := gtk.NewButtonWithLabel("Forget Stored Keys")
	forget.AddCSSClass("destructive-action")
	forget.SetTooltipText("Removes the entries with ssh-keygen -R; the old file is kept as known_hosts.old")
	setBusy := func(busy bool) {
		for _, b := range []*gtk.Button{scan, trust, forget} {
			b.SetSensitive(!busy)
		}
	}
	line := func(k sshhosts.HostKey, note string) string {
		typ := k.Type
		if strings.HasPrefix(k.Type, "RSA") {
			typ += fmt.Sprintf(" %d", k.Bits)
		}
		return typ + "  " + k.Fingerprint + "  " + note
	}
	refresh := func() {
		if info == nil {
			return
		}
		f.d.SetBody("Stored in known_hosts as " + info.Target)
		stored := map[string]bool{}
		revoked := map[string]bool{}
		var lines []string
		for _, k := range info.Stored {
			switch k.Marker {
			case "":
				stored[k.Fingerprint] = true
				lines = append(lines, line(k, fmt.Sprintf("%s:%d", t.pretty(k.File), k.Line)))
			case "revoked":
				revoked[k.Fingerprint] = true
				lines = append(lines, line(k, k.Marker))
			default:
				lines = append(lines, line(k, k.Marker))
			}
		}
		if len(lines) == 0 {
			storedList.SetText("No keys stored for " + info.Target + ". ssh will ask you to confirm the server’s key when you connect.")
		} else {
			storedList.SetText(strings.Join(lines, "\n"))
		}
		switch {
		case info.Via != "":
			scanNote.SetText("Reached through " + info.Via + ", so wtf can’t ask it directly. Connect, and ssh shows the key it’s offered.")
		case len(scanned) == 0:
			p := info.Port
			if p == "" {
				p = "22"
			}
			scanNote.SetText("Ask " + info.HostName + " on port " + p + " which keys it presents (ssh-keyscan). Nothing is saved until you choose.")
		default:
			var lines []string
			for _, k := range scanned {
				note := "not stored"
				if revoked[k.Fingerprint] {
					note = "revoked"
				} else if stored[k.Fingerprint] {
					note = "stored"
				}
				lines = append(lines, line(k, note))
			}
			scanList.SetText(strings.Join(lines, "\n"))
			scanNote.SetText(uicore.HostKeyVerdict(scanned, info.Stored) +
				" Compare fingerprints with ones from the server’s admin (ssh-keygen -lf /etc/ssh/ssh_host_*_key.pub) before trusting new keys.")
		}
		scanList.SetVisible(len(scanned) > 0)
		scan.SetVisible(info.Via == "")
		scan.SetLabel("Scan")
		if len(scanned) > 0 {
			scan.SetLabel("Scan Again")
		}
		allStored := len(scanned) > 0 && len(scanned) == len(stored) &&
			!slices.ContainsFunc(scanned, func(k sshhosts.HostKey) bool { return !stored[k.Fingerprint] })
		trust.SetVisible(len(scanned) > 0 && !allStored)
		trust.SetLabel("Trust Server’s Keys")
		if len(stored) > 0 {
			trust.SetLabel("Replace with Server’s Keys")
		}
		forget.SetVisible(len(stored) > 0)
		forget.SetLabel("Forget Stored Keys")
		if reallyForget {
			forget.SetLabel("Really Forget?")
		}
	}
	svc := t.svc()
	load := func() {
		go func() {
			hk, err := svc.HostKeys(h.Alias, port)
			onMainAsync(func() {
				if err != nil {
					f.setError(err.Error())
					return
				}
				info = &hk
				refresh()
			})
		}()
	}
	// run does a host-key action, the buttons off meanwhile.
	run := func(do func() error, done func()) {
		setBusy(true)
		go func() {
			err := do()
			onMainAsync(func() {
				setBusy(false)
				if err != nil {
					f.setError(err.Error())
					return
				}
				f.setError("")
				done()
			})
		}()
	}
	scan.ConnectClicked(func() {
		var keys []sshhosts.HostKey
		run(func() (err error) {
			keys, err = svc.ScanHostKeys(h.Alias, port)
			return err
		}, func() {
			scanned = keys
			refresh()
		})
	})
	trust.ConnectClicked(func() {
		if info == nil {
			return
		}
		var fps []string
		for _, k := range scanned {
			fps = append(fps, k.Fingerprint)
		}
		target := info.Target
		run(func() error { return svc.TrustHostKeys(h.Alias, port, fps) }, func() {
			w.toast("Saved " + target + "’s keys")
			load()
		})
	})
	forget.ConnectClicked(func() {
		if info == nil {
			return
		}
		// Two steps in place: the first click asks, the second forgets.
		if !reallyForget {
			reallyForget = true
			refresh()
			return
		}
		reallyForget = false
		target := info.Target
		run(func() error { return svc.ForgetHostKeys(h.Alias, port) }, func() {
			w.toast("Forgot " + target + "’s keys")
			if h.Known {
				t.loadHosts()
			}
			load()
		})
	})
	f.field("Stored", storedList, "")
	f.field("On the server now", scanNote, "")
	f.add(scanList)
	spacer := gtk.NewBox(gtk.OrientationHorizontal, 0)
	spacer.SetHExpand(true)
	f.add(sshRowOf(forget, spacer, scan, trust))
	scanList.SetVisible(false)
	scan.SetVisible(false)
	trust.SetVisible(false)
	forget.SetVisible(false)
	f.button("cancel", "Close", adw.ResponseDefault, nil)
	f.present(nil)
	load()
}

// Git identities

// showIdentityEditor edits an identity, or makes one (with keyPath, if
// given). Its key list needs the keys, so they load first.
func (t *sshTab) showIdentityEditor(i *sshhosts.GitIdentity, keyPath string) {
	t.whenLoaded(true, false, func() { t.buildIdentityEditor(i, keyPath) })
}

func (t *sshTab) buildIdentityEditor(i *sshhosts.GitIdentity, keyPath string) {
	w := t.w
	title := "New Git Identity"
	var cur sshhosts.GitIdentity
	if i != nil {
		cur = *i
		title = "Edit " + i.Name
	}
	f := newSSHForm(w, title, "An SSH key for GitHub and the author to commit as, for the tabs you choose", 470)
	name := f.entry(cur.Name, "work")
	var keys, labels []string
	for _, k := range t.keys.Keys {
		if k.Path == "" {
			continue
		}
		label := t.pretty(k.Path)
		if k.Comment != "" && k.Comment != k.Name {
			label += " — " + k.Comment
		}
		keys = append(keys, k.Path)
		labels = append(labels, label)
	}
	current := cur.KeyPath
	if current == "" {
		current = keyPath
	}
	if current != "" && !slices.Contains(keys, current) {
		keys = append([]string{current}, keys...)
		labels = append([]string{t.pretty(current) + " (not found in ~/.ssh)"}, labels...)
	}
	key := gtk.NewDropDownFromStrings(append([]string{"Choose a key…"}, labels...))
	key.SetSelected(uint(slices.Index(keys, current) + 1))
	author := f.entry(cur.AuthorName, "Your Name (blank: from git config)")
	email := f.entry(cur.AuthorEmail, "you@example.com (blank: from git config)")
	useNow := f.check("Use it in this tab after saving", i == nil)
	preview := f.label("", true)
	action, actionLabel := "save", "Save"
	if i == nil {
		action, actionLabel = "add", "Add Identity"
	}
	draft := func() sshhosts.GitIdentity {
		d := sshhosts.GitIdentity{ID: cur.ID, Name: strings.TrimSpace(name.Text()),
			AuthorName: strings.TrimSpace(author.Text()), AuthorEmail: strings.TrimSpace(email.Text())}
		if ki := int(key.Selected()) - 1; ki >= 0 && ki < len(keys) {
			d.KeyPath = keys[ki]
		}
		return d
	}
	refresh := func() {
		d := draft()
		ok := d.Name != "" && d.KeyPath != ""
		preview.SetText("")
		if ok {
			preview.SetText("A tab using it runs:\n" + uicore.UseIdentity(d))
		}
		f.enable(action, ok)
	}
	for _, e := range []*gtk.Entry{name, author, email} {
		e.ConnectChanged(refresh)
	}
	key.NotifyProperty("selected", refresh)
	f.field("Name", name, "Shown in the address bar of tabs using it.")
	f.field("SSH key", key, "git offers GitHub only this key, so GitHub knows which account you are.")
	newKey := gtk.NewButtonWithLabel("New key…")
	newKey.AddCSSClass("flat")
	newKey.SetHAlign(gtk.AlignStart)
	newKey.ConnectClicked(func() {
		f.close()
		t.showKeyGen()
	})
	f.add(newKey)
	f.field("Commit as", author, "")
	f.field("Email", email, "For GitHub to link commits to the account, use an email on it (or its noreply address).")
	f.add(useNow)
	f.add(preview)
	f.button("cancel", "Cancel", adw.ResponseDefault, nil)
	f.button(action, actionLabel, adw.ResponseSuggested, func() {
		d := draft()
		if d.Name == "" || d.KeyPath == "" {
			f.fail("")
			return
		}
		use := useNow.Active()
		svc := t.svc()
		go func() {
			l, err := svc.SaveGitIdentity(d)
			onMainAsync(func() {
				if err != nil {
					f.fail(err.Error())
					return
				}
				t.identities = l
				t.changed()
				f.close()
				switch {
				case use:
					if !t.runInShell(uicore.UseIdentity(d)) {
						w.toast("Opened a new tab as " + d.Name + ": that one was busy")
					}
				case i == nil:
					w.toast("Added " + d.Name)
				default:
					w.toast("Saved " + d.Name)
				}
			})
		}()
	})
	f.present(name)
	refresh()
}

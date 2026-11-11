//go:build linux

package gtkui

import (
	"maps"
	"strconv"
	"strings"
	"time"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/dayanraj/wtf/internal/cloudsecrets"
	"github.com/dayanraj/wtf/internal/uicore"
)

func init() {
	registerSidebarTab(sidebarTabInfo{tab: uicore.SidebarSecrets, label: "Cloud Secrets", icon: "dialog-password-symbolic", cmd: "sidebar.secrets",
		make: func(w *Window) sidePaneTab { return newCloudSecretsTab(w) }})
	extraCSS = append(extraCSS, `
.wtf-secrets-where { padding: 0 8px 0 10px; }
.wtf-secrets-where > label { font-size: 0.9em; }
.wtf-secrets-where entry { font-family: monospace; font-size: 0.9em; }
.wtf-secrets-editor { padding: 4px 16px 16px; }
.wtf-secrets-value { font-family: monospace; font-size: 0.92em; }
`)
}

// The Cloud Secrets tab (#18): AWS Secrets Manager, Parameter Store and
// Vault, as the focused shell sees them. Lists hold names only; a value
// is read when its secret is opened. Nothing polls. Like the macOS app's
// CloudSecretsView.

var secretsSources = []struct{ id, label string }{
	{cloudsecrets.SecretsManager, "Secrets Manager"},
	{cloudsecrets.ParameterStore, "Parameters"},
	{cloudsecrets.Vault, "Vault"},
}

// secretsState is kept per window while it lives: the region picked here
// and where Vault is open.
type secretsState struct {
	region string
	path   string
	// Each Vault mount's KV version, from the mount list.
	kv map[string]int
}

var secretsStates = map[*Window]*secretsState{}

func secretsStateOf(w *Window) *secretsState {
	for k := range secretsStates {
		if k.closing {
			delete(secretsStates, k)
		}
	}
	s := secretsStates[w]
	if s == nil {
		s = &secretsState{kv: map[string]int{}}
		secretsStates[w] = s
	}
	return s
}

// kvOf is the KV version of the mount a path is in (2 when unknown).
func (s *secretsState) kvOf(path string) int {
	best, kv := "", 2
	for m, v := range s.kv {
		if strings.HasPrefix(path, m) && len(m) > len(best) {
			best, kv = m, v
		}
	}
	return kv
}

type cloudSecretsTab struct {
	w        *Window
	st       *secretsState
	root     *gtk.Box
	header   *paneHeader
	reload   *gtk.Button
	switches [3]*gtk.ToggleButton
	switchng bool

	awsRow   *gtk.Box
	awsLabel *gtk.Label
	region   *gtk.Entry
	regions  *menuPopover
	vaultRow *gtk.Box
	vaultLbl *gtk.Label
	path     *gtk.Entry
	up       *gtk.Button

	filter *gtk.SearchEntry
	note   *gtk.Box
	list   *sideList

	seq     int
	loading bool
	loaded  bool
	err     string
	target  cloudsecrets.Target
	items   []cloudsecrets.Item
	mounts  []cloudsecrets.Mount
	lastEnv map[string]string
	closed  bool
	// The region or path field is being typed in: rebuilds leave it alone.
	editing bool
}

func newCloudSecretsTab(w *Window) *cloudSecretsTab {
	t := &cloudSecretsTab{w: w, st: secretsStateOf(w)}
	t.header = newPaneHeader("Cloud Secrets")
	t.header.tool("list-add-symbolic", "New secret", func() { t.create() })
	t.reload = t.header.tool("view-refresh-symbolic", "Reload", t.load)

	bar := gtk.NewBox(gtk.OrientationHorizontal, 0)
	bar.AddCSSClass("linked")
	bar.AddCSSClass("wtf-kube-switch")
	bar.SetHomogeneous(true)
	for i, s := range secretsSources {
		b := gtk.NewToggleButtonWithLabel(s.label)
		b.SetFocusOnClick(false)
		if i > 0 {
			b.SetGroup(t.switches[0])
		}
		b.ConnectToggled(func() {
			if b.Active() && !t.switchng {
				t.choose(s.id)
			}
		})
		t.switches[i] = b
		bar.Append(b)
	}

	// AWS: the profile, and a region to look in.
	t.awsLabel = sideLabel("", "")
	t.awsLabel.SetXAlign(0)
	t.region = gtk.NewEntry()
	t.region.SetHExpand(true)
	t.region.SetTooltipText("The region to look in. Empty follows the shell's")
	t.region.ConnectActivate(func() { t.setRegion(strings.TrimSpace(t.region.Text())) })
	pick := gtk.NewButtonFromIconName("pan-down-symbolic")
	pick.SetTooltipText("Regions")
	pick.SetFocusOnClick(false)
	field := gtk.NewBox(gtk.OrientationHorizontal, 0)
	field.AddCSSClass("linked")
	field.SetHExpand(true)
	field.Append(t.region)
	field.Append(pick)
	t.regions = newMenuPopover(field)
	pick.ConnectClicked(func() {
		items := []*menuItem{item("The shell's region", func() { t.setRegion("") }), menuSection}
		for _, r := range uicore.AWSRegions {
			items = append(items, item(r, func() { t.setRegion(r) }))
		}
		t.regions.show(float64(field.Width()-12), float64(field.Height()), items, nil)
	})
	regionRow := gtk.NewBox(gtk.OrientationHorizontal, 8)
	regionRow.Append(sideLabel("Region", "dim-label"))
	regionRow.Append(field)
	t.awsRow = gtk.NewBox(gtk.OrientationVertical, 4)
	t.awsRow.AddCSSClass("wtf-secrets-where")
	t.awsRow.Append(t.awsLabel)
	t.awsRow.Append(regionRow)

	// Vault: the server, and the folder shown.
	t.vaultLbl = sideLabel("", "")
	t.vaultLbl.SetXAlign(0)
	t.up = gtk.NewButtonFromIconName("go-up-symbolic")
	t.up.SetTooltipText("Up a folder")
	t.up.SetFocusOnClick(false)
	t.up.ConnectClicked(t.goUp)
	t.path = gtk.NewEntry()
	t.path.SetHExpand(true)
	t.path.SetPlaceholderText("Mounts · type a path like secret/app")
	t.path.ConnectActivate(func() { t.open(normalVaultPath(t.path.Text())) })
	pathRow := gtk.NewBox(gtk.OrientationHorizontal, 4)
	pathRow.Append(t.up)
	pathRow.Append(t.path)
	t.vaultRow = gtk.NewBox(gtk.OrientationVertical, 4)
	t.vaultRow.AddCSSClass("wtf-secrets-where")
	t.vaultRow.Append(t.vaultLbl)
	t.vaultRow.Append(pathRow)

	t.filter = gtk.NewSearchEntry()
	t.filter.SetPlaceholderText("Filter by name")
	t.filter.AddCSSClass("wtf-side-filter")
	t.filter.ConnectSearchChanged(t.rebuild)
	t.filter.ConnectStopSearch(func() {
		t.filter.SetText("")
		w.focusActive()
	})
	t.note = gtk.NewBox(gtk.OrientationVertical, 0)
	t.list = newSideList(false)
	t.list.onActivate = t.activated
	t.list.onMenu = func(ids []string, x, y float64) {
		if items := t.menu(ids[0]); len(items) > 0 {
			t.list.menu.show(x, y, items, nil)
		}
	}

	for _, e := range []*gtk.Entry{t.region, t.path} {
		focus := gtk.NewEventControllerFocus()
		focus.ConnectEnter(func() { t.editing = true })
		focus.ConnectLeave(func() { t.editing = false })
		e.AddController(focus)
	}

	t.root = gtk.NewBox(gtk.OrientationVertical, 4)
	t.root.Append(t.header.root)
	t.root.Append(bar)
	t.root.Append(t.awsRow)
	t.root.Append(t.vaultRow)
	t.root.Append(t.filter)
	t.root.Append(t.note)
	t.root.Append(t.list.root)
	t.showSwitch()
	t.load()
	return t
}

func (t *cloudSecretsTab) widget() gtk.Widgetter { return t.root }
func (t *cloudSecretsTab) commandFinished()      {}
func (t *cloudSecretsTab) close()                { t.closed = true }

func (t *cloudSecretsTab) source() string { return t.w.sidebarState.SecretsSource }
func (t *cloudSecretsTab) vault() bool    { return t.source() == cloudsecrets.Vault }

func (t *cloudSecretsTab) svc() *cloudsecrets.Service { return t.w.app.be.Secrets }

// req is the focused shell's view, with the region picked here.
func (t *cloudSecretsTab) req() cloudsecrets.Request {
	return cloudsecrets.Request{Env: kubeShell(t.w).Env, Region: t.st.region}
}

// paneChanged follows the focused shell: another profile, region or Vault.
func (t *cloudSecretsTab) paneChanged() {
	if env := kubeShell(t.w).Env; !maps.Equal(env, t.lastEnv) {
		t.load()
	}
}

// choose shows a store and remembers it for the window.
func (t *cloudSecretsTab) choose(source string) {
	t.w.sidebarState.SecretsSource = source
	t.w.scheduleSave()
	t.showSwitch()
	t.load()
}

func (t *cloudSecretsTab) showSwitch() {
	t.switchng = true
	for i, s := range secretsSources {
		t.switches[i].SetActive(s.id == t.source())
	}
	t.switchng = false
}

func (t *cloudSecretsTab) setRegion(r string) {
	t.st.region = r
	t.load()
}

func normalVaultPath(p string) string {
	p = strings.Trim(strings.TrimSpace(p), "/")
	if p == "" {
		return ""
	}
	return p + "/"
}

// open shows a Vault folder ("" is the mounts).
func (t *cloudSecretsTab) open(path string) {
	t.st.path = path
	t.filter.SetText("")
	t.load()
}

func (t *cloudSecretsTab) goUp() {
	p := strings.TrimSuffix(t.st.path, "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		t.open(p[:i+1])
	} else {
		t.open("")
	}
}

// load reads the store shown; only then, and when asked.
func (t *cloudSecretsTab) load() {
	t.seq++
	mine := t.seq
	req := t.req()
	t.lastEnv = req.Env
	source, path, svc := t.source(), t.st.path, t.svc()
	t.loading, t.err = true, ""
	t.items, t.mounts = nil, nil
	t.loaded = false
	t.rebuild()
	go func() {
		target := svc.Target(req)
		var items []cloudsecrets.Item
		var mounts []cloudsecrets.Mount
		var err error
		switch source {
		case cloudsecrets.SecretsManager:
			if target.AWSError == "" {
				items, err = svc.ListSecrets(req)
			}
		case cloudsecrets.ParameterStore:
			if target.AWSError == "" {
				items, err = svc.ListParameters(req)
			}
		case cloudsecrets.Vault:
			if path == "" {
				mounts, err = svc.VaultMounts(req)
			} else {
				items, err = svc.VaultList(req, path)
			}
		}
		onMainAsync(func() {
			if t.closed || mine != t.seq {
				return
			}
			t.target, t.items, t.mounts, t.loading, t.loaded = target, items, mounts, false, true
			if err != nil {
				t.err = err.Error()
			}
			for _, m := range mounts {
				t.st.kv[m.Path] = m.KV
			}
			t.rebuild()
		})
	}()
}

// where says whose secrets these are, for dialogs and the header.
func (t *cloudSecretsTab) where() string {
	tg := t.target
	if t.vault() {
		s := tg.VaultAddr
		if s == "" {
			s = "VAULT_ADDR not set (the vault CLI's default)"
		}
		if tg.VaultNamespace != "" {
			s += " · namespace " + tg.VaultNamespace
		}
		return s
	}
	s := "Profile " + tg.Profile
	if tg.Account != "" {
		s += " · account " + tg.Account
	}
	if tg.Region != "" {
		s += " · " + tg.Region
	}
	return s
}

func (t *cloudSecretsTab) rebuild() {
	if t.closed {
		return
	}
	vault := t.vault()
	t.reload.SetSensitive(!t.loading)
	t.awsRow.SetVisible(!vault)
	t.vaultRow.SetVisible(vault)
	tg := t.target
	if vault {
		t.vaultLbl.SetText(t.where())
		t.vaultLbl.SetTooltipText("The focused shell's VAULT_ADDR and VAULT_NAMESPACE. The token is the vault CLI's own (vault login)")
		if !t.editing {
			t.path.SetText(t.st.path)
		}
		t.up.SetSensitive(t.st.path != "")
	} else {
		switch {
		case tg.Profile == "":
			t.awsLabel.SetText("No AWS profile")
		case tg.Account != "":
			t.awsLabel.SetText(tg.Profile + " · " + tg.Account)
		default:
			t.awsLabel.SetText(tg.Profile)
		}
		t.awsLabel.SetTooltipText("The focused shell's AWS profile. Switch it in AWS Profiles → Use in This Shell")
		if !t.editing {
			t.region.SetText(t.st.region)
		}
		if t.st.region == "" && tg.Region != "" {
			t.region.SetPlaceholderText(tg.Region + " (the shell's)")
		} else if t.st.region == "" {
			t.region.SetPlaceholderText("Pick a region")
		}
	}

	var rows []sideRow
	q := strings.ToLower(strings.TrimSpace(t.filter.Text()))
	matches := func(s string) bool { return q == "" || strings.Contains(strings.ToLower(s), q) }
	now := time.Now()
	changed := func(iso string) string {
		if a := uicore.KubeAge(iso, now); a != "" {
			return "changed " + a + " ago"
		}
		return ""
	}
	join := func(parts ...string) string {
		var out []string
		for _, p := range parts {
			if p != "" {
				out = append(out, p)
			}
		}
		return strings.Join(out, " · ")
	}
	if vault && t.st.path == "" {
		for _, m := range t.mounts {
			if matches(m.Path) {
				rows = append(rows, sideRow{id: "m:" + m.Path, icon: "folder-symbolic", title: m.Path, mono: true,
					detail: join("KV "+strconv.Itoa(m.KV), m.Description), tip: "Double-click to open"})
			}
		}
	}
	for _, it := range t.items {
		if !matches(it.Name + " " + it.Description) {
			continue
		}
		switch {
		case it.Folder:
			rows = append(rows, sideRow{id: "f:" + it.Name, icon: "folder-symbolic", title: it.Name, mono: true, tip: "Double-click to open"})
		case vault:
			rows = append(rows, sideRow{id: "s:" + it.Name, icon: "dialog-password-symbolic", title: it.Name, mono: true,
				tip: t.st.path + it.Name + "\nDouble-click to see its value"})
		case t.source() == cloudsecrets.ParameterStore:
			tip := it.Name + "\n" + it.Type + ", version " + it.Version
			if it.Description != "" {
				tip += "\n" + it.Description
			}
			rows = append(rows, sideRow{id: "s:" + it.Name, title: it.Name, mono: true,
				detail: join(it.Type, "v"+it.Version, changed(it.Changed)), tip: tip + "\nLast changed " + it.Changed})
		default:
			detail := it.Description
			if detail == "" {
				detail = changed(it.Changed)
			}
			if it.Rotation {
				detail = join(detail, "rotation")
			}
			tip := it.Name
			if it.Description != "" {
				tip += "\n" + it.Description
			}
			if it.Changed != "" {
				tip += "\nLast changed " + it.Changed
			}
			if it.Accessed != "" {
				tip += "\nLast read " + it.Accessed
			}
			if it.Rotation {
				tip += "\nAutomatic rotation is on"
			}
			rows = append(rows, sideRow{id: "s:" + it.Name, title: it.Name, mono: true, detail: detail, tip: tip})
		}
	}
	t.list.set([]sideSection{{rows: rows}})

	label := map[string]string{cloudsecrets.SecretsManager: "secrets", cloudsecrets.ParameterStore: "parameters", cloudsecrets.Vault: "secrets"}[t.source()]
	switch {
	case !vault && tg.AWSError != "" && t.loaded:
		setNote(t.note, "Can’t browse AWS from this shell", tg.AWSError)
	case t.err != "" && vault && t.st.path == "":
		setNote(t.note, "Can’t list Vault’s mounts", t.err+"\nType a path above, like secret/app, and press Return.")
		installLink(t.w, t.note, t.err)
	case t.err != "":
		setNote(t.note, "Can’t list the "+label, t.err)
		installLink(t.w, t.note, t.err)
	case t.loading:
		setNote(t.note, "", "Reading…")
	case t.loaded && len(rows) == 0 && q != "":
		setNote(t.note, "", "Nothing matches “"+t.filter.Text()+"”.")
	case t.loaded && len(rows) == 0 && vault && t.st.path == "":
		setNote(t.note, "No key/value mounts", "Type a path above, like secret/app, and press Return.")
	case t.loaded && len(rows) == 0 && vault:
		setNote(t.note, "", "This folder is empty. Press + to add a secret here.")
	case t.loaded && len(rows) == 0:
		setNote(t.note, "No "+label, "Press + to add one in "+tg.Region+".")
	default:
		setNote(t.note, "", "")
	}
}

// fullName is a row's secret name: for Vault, its whole path.
func (t *cloudSecretsTab) fullName(id string) (kind, name string) {
	kind, name, _ = strings.Cut(id, ":")
	if t.vault() && kind != "m" {
		name = t.st.path + name
	}
	return kind, name
}

func (t *cloudSecretsTab) activated(id string) {
	kind, name := t.fullName(id)
	switch kind {
	case "m", "f":
		t.open(normalVaultPath(name))
	case "s":
		t.showValue(name)
	}
}

func (t *cloudSecretsTab) menu(id string) []*menuItem {
	kind, name := t.fullName(id)
	copyName := func() {
		t.w.win.Clipboard().SetText(name)
		t.w.toast("Copied " + name)
	}
	switch kind {
	case "m", "f":
		return []*menuItem{item("Open", func() { t.open(normalVaultPath(name)) }), menuSection, item("Copy Path", copyName)}
	case "s":
		label := "Copy Name"
		if t.vault() {
			label = "Copy Path"
		}
		return []*menuItem{
			item("Show Value…", func() { t.showValue(name) }),
			item("Edit Value…", func() { t.edit(name) }),
			menuSection,
			item(label, copyName),
		}
	}
	return nil
}

// secretsHidden says so (and is true) while Hide Secrets is on.
func (w *Window) secretsHidden() bool {
	if w.app.settings.hide {
		w.toast("Hide Secrets is on: turn it off to see or change values")
		return true
	}
	return false
}

// readValue reads one secret's value from a store.
func readCloudValue(svc *cloudsecrets.Service, req cloudsecrets.Request, source, name string, kv int) (cloudsecrets.Value, error) {
	switch source {
	case cloudsecrets.ParameterStore:
		return svc.ReadParameter(req, name)
	case cloudsecrets.Vault:
		return svc.VaultRead(req, name, kv)
	}
	return svc.ReadSecret(req, name)
}

// cloudCtx is what a dialog needs to read and write: the store, the
// request and where it goes, and a way to refresh the tab.
type cloudCtx struct {
	w      *Window
	source string
	req    cloudsecrets.Request
	where  string
	kv     int
	reload func()
}

func (t *cloudSecretsTab) ctx(name string) cloudCtx {
	return cloudCtx{w: t.w, source: t.source(), req: t.req(), where: t.where(), kv: t.st.kvOf(name),
		reload: func() {
			if !t.closed {
				t.load()
			}
		}}
}

func (t *cloudSecretsTab) showValue(name string) { showCloudValue(t.ctx(name), name) }

// edit reads the value, then opens it in the editor.
func (t *cloudSecretsTab) edit(name string) {
	if t.w.secretsHidden() {
		return
	}
	c := t.ctx(name)
	svc := t.svc()
	go func() {
		v, err := readCloudValue(svc, c.req, c.source, name, c.kv)
		onMainAsync(func() {
			if err != nil {
				c.w.toast(err.Error())
				return
			}
			showCloudEditor(c, name, &v)
		})
	}()
}

func (t *cloudSecretsTab) create() {
	if t.vault() {
		showCloudEditor(t.ctx(t.st.path), t.st.path, nil)
		return
	}
	if t.target.AWSError != "" {
		t.w.toast(t.target.AWSError)
		return
	}
	showCloudEditor(t.ctx(""), "", nil)
}

// Value viewer

// cloudValueViewer shows a secret's value, key by key when it's a JSON
// object, hidden until revealed and hidden again when wtf loses focus.
// The value is dropped when it closes.
type cloudValueViewer struct {
	*viewer
	c        cloudCtx
	name     string
	value    *cloudsecrets.Value
	err      string
	revealed map[string]bool
}

func showCloudValue(c cloudCtx, name string) {
	v := &cloudValueViewer{viewer: newViewer(c.w, 580), c: c, name: name, revealed: map[string]bool{}}
	v.onClosed = func() { v.value, v.revealed = nil, map[string]bool{} }
	v.onLosingFocus(func() {
		if len(v.revealed) > 0 {
			v.revealed = map[string]bool{}
			v.render()
		}
	})
	v.render()
	v.show()
	svc := c.w.app.be.Secrets
	go func() {
		val, err := readCloudValue(svc, c.req, c.source, name, c.kv)
		onMainAsync(func() {
			if v.closed {
				return
			}
			if err != nil {
				v.err = err.Error()
			} else {
				v.value = &val
			}
			v.render()
		})
	}()
}

// entries are the value's keys, or one "value".
func (v *cloudValueViewer) entries() []cloudsecrets.Field {
	if v.value == nil {
		return nil
	}
	if len(v.value.Fields) > 0 {
		return v.value.Fields
	}
	return []cloudsecrets.Field{{Key: "value", Value: v.value.Text}}
}

func (v *cloudValueViewer) render() {
	v.clear()
	sub := v.c.where
	if val := v.value; val != nil {
		if val.Version != "" {
			ver := val.Version
			if len(ver) > 12 {
				ver = ver[:8] + "…"
			}
			sub += " · version " + ver
		}
		if val.Type != "" {
			sub += " · " + val.Type
		}
		if val.Changed != "" {
			sub += " · changed " + val.Changed
		}
	}
	v.setTitle(v.name, sub+". Values hide again when wtf loses focus.")
	switch {
	case v.err != "":
		v.add(viewerValue(v.err, "error"))
		v.done()
		return
	case v.value == nil:
		v.add(viewerValue("Reading the secret…", "dim-label"))
		v.done()
		return
	}
	w := v.w
	list := gtk.NewBox(gtk.OrientationVertical, 8)
	entries := v.entries()
	for _, e := range entries {
		key := gtk.NewLabel(e.Key)
		key.SetXAlign(0)
		key.SetHExpand(true)
		key.AddCSSClass("wtf-secret-key")
		shown := v.revealed[e.Key]
		eye, eyeTip := "view-reveal-symbolic", "Reveal"
		if shown {
			eye, eyeTip = "view-conceal-symbolic", "Hide"
		}
		toggle := gtk.NewButtonFromIconName(eye)
		toggle.AddCSSClass("flat")
		toggle.SetTooltipText(eyeTip)
		toggle.ConnectClicked(func() {
			if shown {
				delete(v.revealed, e.Key)
			} else if !w.secretsHidden() {
				v.revealed[e.Key] = true
			}
			v.render()
		})
		what := e.Key
		if e.Key == "value" && len(v.value.Fields) == 0 {
			what = v.name
		}
		if v.value.Binary {
			what += " (base64)"
		}
		copyB := gtk.NewButtonFromIconName("edit-copy-symbolic")
		copyB.AddCSSClass("flat")
		copyB.SetTooltipText("Copy")
		copyB.ConnectClicked(func() {
			if !w.secretsHidden() {
				w.copySecret(e.Value, what)
			}
		})
		list.Append(viewerRow(key, toggle, copyB))
		if !shown {
			dots := gtk.NewLabel("••••••••")
			dots.SetXAlign(0)
			dots.AddCSSClass("dim-label")
			list.Append(dots)
			continue
		}
		box, _ := v.textBox(e.Value, v.maxBoxHeight()/4, true)
		list.Append(box)
	}
	scroll := gtk.NewScrolledWindow()
	scroll.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	scroll.SetPropagateNaturalHeight(true)
	scroll.SetMaxContentHeight(v.maxBoxHeight() * 6 / 10)
	scroll.SetChild(list)
	v.add(scroll)

	allLabel := "Reveal All"
	all := len(v.revealed) == len(entries)
	if all {
		allLabel = "Hide All"
	}
	reveal := viewerButton(allLabel, func() {
		if all {
			v.revealed = map[string]bool{}
		} else if !w.secretsHidden() {
			for _, e := range entries {
				v.revealed[e.Key] = true
			}
		}
		v.render()
	})
	var left []*gtk.Button
	left = append(left, reveal)
	if !v.value.Binary {
		val := *v.value
		left = append(left, viewerButton("Edit…", func() {
			if w.secretsHidden() {
				return
			}
			v.win.Close()
			showCloudEditor(v.c, v.name, &val)
		}))
	}
	v.done(left...)
}

// Editor

// cloudEditor is a new value for a secret, or a new secret: the form, then
// the plan (no values in it), saved once confirmed.
type cloudEditor struct {
	c       cloudCtx
	win     *adw.Window
	name    *gtk.Entry
	kind    *gtk.DropDown
	desc    *gtk.Entry
	value   *gtk.TextView
	errText *gtk.Label
	review  *gtk.Button
	editing *cloudsecrets.Value
	busy    bool
}

var paramTypes = []string{"SecureString", "String", "StringList"}

// showCloudEditor edits value (of name), or creates a secret when value is
// nil (name is then where to start: a Vault folder).
func showCloudEditor(c cloudCtx, name string, value *cloudsecrets.Value) {
	if value != nil && c.w.secretsHidden() {
		return
	}
	e := &cloudEditor{c: c, editing: value}
	vault := c.source == cloudsecrets.Vault
	e.win = adw.NewWindow()
	e.win.SetTransientFor(&c.w.win.Window)
	e.win.SetModal(true)
	e.win.SetDefaultSize(540, 460)
	title := "New Secret"
	switch {
	case value != nil:
		title = "Edit " + name
	case c.source == cloudsecrets.ParameterStore:
		title = "New Parameter"
	}
	e.win.SetTitle(title)
	head := adw.NewHeaderBar()
	head.SetTitleWidget(adw.NewWindowTitle(title, ""))

	body := gtk.NewBox(gtk.OrientationVertical, 10)
	body.AddCSSClass("wtf-secrets-editor")
	where := formNote(c.where, "dim-label")
	body.Append(where)
	grid := gtk.NewGrid()
	grid.SetRowSpacing(8)
	grid.SetColumnSpacing(12)
	rows := 0
	field := func(label string, w gtk.Widgetter) {
		l := gtk.NewLabel(label)
		l.SetXAlign(0)
		grid.Attach(l, 0, rows, 1, 1)
		grid.Attach(w, 1, rows, 1, 1)
		rows++
	}
	if value == nil {
		e.name = gtk.NewEntry()
		e.name.SetHExpand(true)
		e.name.SetText(name)
		label := "Name"
		switch c.source {
		case cloudsecrets.Vault:
			label = "Path"
			e.name.SetPlaceholderText("secret/app/db")
		case cloudsecrets.ParameterStore:
			e.name.SetPlaceholderText("/app/prod/db-password")
		default:
			e.name.SetPlaceholderText("prod/app/db")
		}
		field(label, e.name)
		if c.source == cloudsecrets.ParameterStore {
			e.kind = gtk.NewDropDownFromStrings(paramTypes)
			e.kind.SetTooltipText("SecureString is encrypted with the account's default key (aws/ssm)")
			field("Type", e.kind)
		}
		if !vault {
			e.desc = gtk.NewEntry()
			e.desc.SetHExpand(true)
			e.desc.SetPlaceholderText("Optional")
			field("Description", e.desc)
		}
	} else {
		n := formNote(name, "monospace")
		n.SetSelectable(true)
		field("Name", n)
	}
	if rows > 0 {
		body.Append(grid)
	}
	hint := "The value"
	if vault {
		hint = "A JSON object of keys and values"
	}
	body.Append(formNote(hint, "dim-label"))
	e.value = gtk.NewTextView()
	e.value.SetMonospace(true)
	e.value.SetWrapMode(gtk.WrapWordChar)
	e.value.SetTopMargin(8)
	e.value.SetBottomMargin(8)
	e.value.SetLeftMargin(8)
	e.value.SetRightMargin(8)
	e.value.AddCSSClass("wtf-secrets-value")
	switch {
	case value != nil:
		e.value.Buffer().SetText(value.Text)
	case vault:
		e.value.Buffer().SetText("{\n  \"key\": \"\"\n}")
	}
	scroll := gtk.NewScrolledWindow()
	scroll.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	scroll.SetVExpand(true)
	scroll.SetMinContentHeight(160)
	scroll.AddCSSClass("card")
	scroll.SetChild(e.value)
	body.Append(scroll)
	e.errText = formNote("", "wtf-form-error")
	e.errText.SetSelectable(true)
	e.errText.SetVisible(false)
	body.Append(e.errText)

	cancel := gtk.NewButtonWithLabel("Cancel")
	cancel.ConnectClicked(func() { e.win.Close() })
	e.review = gtk.NewButtonWithLabel("Review…")
	e.review.AddCSSClass("suggested-action")
	e.review.SetHExpand(true)
	e.review.SetHAlign(gtk.AlignEnd)
	e.review.ConnectClicked(e.reviewChange)
	buttons := gtk.NewBox(gtk.OrientationHorizontal, 8)
	buttons.Append(cancel)
	buttons.Append(e.review)
	body.Append(buttons)

	view := adw.NewToolbarView()
	view.AddTopBar(head)
	view.SetContent(body)
	e.win.SetContent(view)
	keys := gtk.NewEventControllerKey()
	keys.ConnectKeyPressed(func(keyval, _ uint, _ gdk.ModifierType) bool {
		if keyval == gdk.KEY_Escape {
			e.win.Close()
			return true
		}
		return false
	})
	e.win.AddController(keys)
	e.win.ConnectCloseRequest(func() bool {
		// The value goes with the dialog.
		e.value.Buffer().SetText("")
		e.editing = nil
		return false
	})
	e.win.Present()
	if e.name != nil {
		e.name.GrabFocus()
	} else {
		e.value.GrabFocus()
	}
}

func (e *cloudEditor) setError(msg string) {
	e.errText.SetText(msg)
	e.errText.SetVisible(msg != "")
}

func (e *cloudEditor) setBusy(b bool) {
	e.busy = b
	e.review.SetSensitive(!b)
}

func (e *cloudEditor) change() cloudsecrets.Change {
	buf := e.value.Buffer()
	ch := cloudsecrets.Change{Store: e.c.source, Value: buf.Text(buf.StartIter(), buf.EndIter(), false)}
	if e.editing != nil {
		ch.Name, ch.Version = e.editing.Name, e.editing.Version
	} else {
		ch.Name, ch.Create = strings.TrimSpace(e.name.Text()), true
		if e.kind != nil {
			if i := int(e.kind.Selected()); i >= 0 && i < len(paramTypes) {
				ch.Type = paramTypes[i]
			}
		}
		if e.desc != nil {
			ch.Description = strings.TrimSpace(e.desc.Text())
		}
	}
	if ch.Store == cloudsecrets.Vault {
		ch.Name = strings.Trim(ch.Name, "/")
		ch.KV = secretsStateOf(e.c.w).kvOf(ch.Name)
	}
	return ch
}

// reviewChange plans the change, shows it, and makes it once confirmed.
func (e *cloudEditor) reviewChange() {
	if e.busy || e.c.w.secretsHidden() {
		return
	}
	ch := e.change()
	e.setError("")
	e.setBusy(true)
	svc, req := e.c.w.app.be.Secrets, e.c.req
	go func() {
		steps, err := svc.PlanChange(req, ch)
		onMainAsync(func() {
			e.setBusy(false)
			if err != nil {
				e.setError(err.Error())
				return
			}
			title, button := "Save a new value for "+ch.Name+"?", "Save"
			if ch.Create {
				title, button = "Create "+ch.Name+"?", "Create"
			}
			body := "wtf will make this change. Values aren't shown here.\n\n• " + strings.Join(steps, "\n• ")
			e.c.w.app.confirm(&e.win.Window, title, body, button, func() { e.apply(ch) })
		})
	}()
}

func (e *cloudEditor) apply(ch cloudsecrets.Change) {
	e.setBusy(true)
	svc, req := e.c.w.app.be.Secrets, e.c.req
	go func() {
		err := svc.ApplyChange(req, ch)
		onMainAsync(func() {
			e.setBusy(false)
			if err != nil {
				e.setError(err.Error())
				return
			}
			done := "Saved a new version of " + ch.Name
			if ch.Create {
				done = "Created " + ch.Name
			}
			e.win.Close()
			e.c.w.toast(done)
			e.c.reload()
		})
	}()
}

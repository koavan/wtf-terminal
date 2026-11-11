//go:build linux

package gtkui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/dayanraj/wtf/internal/sshhosts"
	"github.com/dayanraj/wtf/internal/uicore"
)

func init() {
	registerSidebarTab(sidebarTabInfo{tab: uicore.SidebarSSH, label: "SSH Hosts", icon: "network-server-symbolic", cmd: "sidebar.ssh",
		make: func(w *Window) sidePaneTab { return newSSHTab(w) }})
	extraCSS = append(extraCSS, `
.wtf-ssh-switcher { margin: 4px 8px 2px; }
.wtf-ssh-switcher button { padding: 2px 4px; min-height: 24px; font-size: 0.88em; }
.wtf-ssh-bar { padding: 2px 8px 4px 10px; font-size: 0.88em; }
.wtf-ssh-bar button { font-size: 0.95em; padding: 0 8px; min-height: 22px; }
.wtf-ssh-note { padding: 10px 12px 4px; font-size: 0.9em; }
.wtf-ssh-note-title { font-weight: bold; }
.dot-tunnel-up { background: @success_color; }
.dot-tunnel-connecting { background: @warning_color; }
.dot-tunnel-failed { background: @error_color; }
.dot-tunnel-stopped { background: alpha(currentColor, 0.3); }
`)
}

// The SSH tab's sections, as saved in the sidebar state, and their labels.
var (
	sshSectionIDs    = []string{"hosts", "keys", "git", "tunnels"}
	sshSectionLabels = []string{"Hosts", "Keys", "Git", "Tunnels"}
)

// sshTab is the SSH tab: Hosts, Keys, Git identities and Tunnels, one
// shown at a time. What they show is loaded when a section first appears
// and after ssh-keygen, ssh-add and the like finish, never polled; tunnels
// follow the ssh:tunnels event while the tab is open. Like the macOS
// app's SshView (its SshStore lives here, per tab).
type sshTab struct {
	w        *Window
	root     *gtk.Box
	switches map[string]*gtk.ToggleButton
	holder   *gtk.Box
	section  sshSection
	shown    string
	closed   bool
	off      func() // stops the tunnels subscription

	list                      sshhosts.List
	keyFiles                  []string
	hostsLoaded, hostsLoading bool
	keys                      sshhosts.KeyList
	keysLoaded, keysLoading   bool
	tunnels                   []sshhosts.TunnelStatus
	tunnelsLoaded             bool
	identities                []sshhosts.GitIdentity
	identitiesLoaded          bool
	// Identities whose GitHub login is being tested.
	testing map[string]bool
	waiting []sshWaiter
}

// sshSection is one of the tab's sections; only the shown one exists.
type sshSection interface {
	widget() gtk.Widgetter
	rebuild()
}

// sshWaiter runs once the keys and/or hosts it needs are loaded.
type sshWaiter struct {
	keys, hosts bool
	run         func()
}

func newSSHTab(w *Window) *sshTab {
	t := &sshTab{w: w, switches: map[string]*gtk.ToggleButton{}, testing: map[string]bool{}}
	bar := gtk.NewBox(gtk.OrientationHorizontal, 0)
	bar.AddCSSClass("linked")
	bar.AddCSSClass("wtf-ssh-switcher")
	bar.SetHomogeneous(true)
	var first *gtk.ToggleButton
	for i, id := range sshSectionIDs {
		b := gtk.NewToggleButtonWithLabel(sshSectionLabels[i])
		b.SetFocusOnClick(false)
		if first == nil {
			first = b
		} else {
			b.SetGroup(first)
		}
		b.ConnectToggled(func() {
			if b.Active() && id != t.shown {
				w.sidebarState.SSHSection = id
				w.scheduleSave()
				t.show(id)
			}
		})
		t.switches[id] = b
		bar.Append(b)
	}
	t.holder = gtk.NewBox(gtk.OrientationVertical, 0)
	t.holder.SetVExpand(true)
	t.root = gtk.NewBox(gtk.OrientationVertical, 4)
	t.root.Append(bar)
	t.root.Append(t.holder)
	t.off = w.app.events.on(sshhosts.EventTunnels, func(data any) {
		if l, ok := data.([]sshhosts.TunnelStatus); ok && !t.closed {
			t.tunnels = l
			t.changed()
		}
	})
	// Tunnels load with the tab, for the running count.
	t.loadTunnels()
	t.show(w.sidebarState.SSHSection)
	return t
}

func (t *sshTab) widget() gtk.Widgetter { return t.root }

// close stops listening for tunnel changes.
func (t *sshTab) close() {
	t.closed = true
	t.off()
}

// paneChanged: the Git section shows the focused shell's identity.
func (t *sshTab) paneChanged() {
	if g, ok := t.section.(*sshGitSection); ok && g.current != t.currentIdentity() {
		g.rebuild()
	}
}

// commandFinished: ssh-keygen, ssh-add and the like may have changed keys
// and hosts.
func (t *sshTab) commandFinished() {
	ps := t.w.model.ActivePane()
	if ps == nil || ps.LastCommand == nil || !uicore.IsKeyTool(ps.LastCommand.Text) {
		return
	}
	if t.keysLoaded {
		t.loadKeys()
	}
	if t.hostsLoaded {
		t.loadHosts()
	}
}

func (t *sshTab) show(id string) {
	if !slices.Contains(sshSectionIDs, id) {
		id = "hosts"
	}
	t.shown = id
	t.switches[id].SetActive(true)
	if t.section != nil {
		t.holder.Remove(t.section.widget())
	}
	// Each section reads its data when it appears; the tab reads tunnels.
	load := func() {}
	switch id {
	case "keys":
		t.section, load = newSSHKeysSection(t), t.loadKeys
	case "git":
		t.section, load = newSSHGitSection(t), func() {
			t.loadIdentities()
			if !t.keysLoaded {
				t.loadKeys()
			}
		}
	case "tunnels":
		t.section = newSSHTunnelsSection(t)
	default:
		t.section, load = newSSHHostsSection(t), t.loadHosts
	}
	t.holder.Append(t.section.widget())
	t.updateBadge()
	t.section.rebuild()
	load()
}

func (t *sshTab) updateBadge() {
	n := 0
	for _, x := range t.tunnels {
		if uicore.TunnelIsOn(x) && x.Kind != "kubectl" {
			n++
		}
	}
	b := t.switches["tunnels"]
	if n > 0 {
		b.SetLabel(fmt.Sprintf("Tunnels %d", n))
		b.SetTooltipText(fmt.Sprintf("%d running", n))
	} else {
		b.SetLabel("Tunnels")
		b.SetTooltipText("")
	}
}

// Loading

func (t *sshTab) svc() *sshhosts.Service { return t.w.app.be.SSHHosts }

// changed redraws the shown section and runs the dialogs that were
// waiting for data.
func (t *sshTab) changed() {
	var ready, later []sshWaiter
	for _, x := range t.waiting {
		if (!x.keys || t.keysLoaded) && (!x.hosts || t.hostsLoaded) {
			ready = append(ready, x)
		} else {
			later = append(later, x)
		}
	}
	t.waiting = later
	if !t.closed {
		t.updateBadge()
		t.section.rebuild()
	}
	for _, x := range ready {
		x.run()
	}
}

// whenLoaded runs once the keys and/or hosts are loaded (loading them if
// needed), so dialogs built from them never open with empty lists.
func (t *sshTab) whenLoaded(keys, hosts bool, run func()) {
	if (!keys || t.keysLoaded) && (!hosts || t.hostsLoaded) {
		run()
		return
	}
	t.waiting = append(t.waiting, sshWaiter{keys, hosts, run})
	if keys && !t.keysLoaded && !t.keysLoading {
		t.loadKeys()
	}
	if hosts && !t.hostsLoaded && !t.hostsLoading {
		t.loadHosts()
	}
}

func (t *sshTab) loadHosts() {
	t.hostsLoading = true
	t.changed()
	svc := t.svc()
	go func() {
		l, files := svc.List(), svc.Keys()
		onMainAsync(func() {
			t.list, t.keyFiles = l, files
			t.hostsLoaded, t.hostsLoading = true, false
			t.changed()
		})
	}()
}

func (t *sshTab) loadKeys() {
	t.keysLoading = true
	t.changed()
	svc := t.svc()
	go func() {
		k := svc.KeyList()
		onMainAsync(func() {
			t.keys = k
			t.keysLoaded, t.keysLoading = true, false
			t.changed()
		})
	}()
}

func (t *sshTab) loadTunnels() {
	svc := t.svc()
	go func() {
		l := svc.Tunnels()
		onMainAsync(func() {
			t.tunnels, t.tunnelsLoaded = l, true
			t.changed()
		})
	}()
}

func (t *sshTab) loadIdentities() {
	svc := t.svc()
	go func() {
		l := svc.GitIdentities()
		onMainAsync(func() {
			t.identities, t.identitiesLoaded = l, true
			t.changed()
		})
	}()
}

// setTunnels takes the list a tunnel action returned.
func (t *sshTab) setTunnels(l []sshhosts.TunnelStatus) {
	t.tunnels = l
	t.changed()
}

// Helpers

func (t *sshTab) pretty(path string) string { return uicore.PrettyPath(path, t.w.app.home) }

// copy puts text on the clipboard and says so.
func (t *sshTab) copy(text, toast string) {
	t.w.win.Clipboard().SetText(text)
	t.w.toast(toast)
}

// runInShell types a command into the focused shell (runInPane), and says
// whether it ran there rather than in a new tab.
func (t *sshTab) runInShell(command string) bool {
	ps := t.w.model.ActivePane()
	here := ps != nil && t.w.active() != nil && ps.IsShell && !ps.Running
	t.w.runInPane(command)
	return here
}

// act does a quick backend action off the main loop: its toast, or the
// error; keys are read again after.
func (t *sshTab) act(success string, run func() error) {
	go func() {
		err := run()
		onMainAsync(func() {
			if err != nil {
				t.w.toast(err.Error())
			} else {
				t.w.toast(success)
			}
			t.loadKeys()
		})
	}()
}

// copyPublicKey copies a key's .pub, toasting what.
func (t *sshTab) copyPublicKey(path, toast string, then func()) {
	svc := t.svc()
	go func() {
		pub, err := svc.PublicKey(path)
		onMainAsync(func() {
			if err != nil {
				t.w.toast(err.Error())
				return
			}
			t.copy(pub, toast)
			if then != nil {
				then()
			}
		})
	}()
}

// sshOpenURL opens a web address with the desktop's handler.
func sshOpenURL(w *Window, url string) {
	gtk.NewURILauncher(url).Launch(context.Background(), &w.win.Window, nil)
}

// sshOpenWhere is where hosts open.
type sshOpenWhere int

const (
	sshOpenHere sshOpenWhere = iota
	sshOpenRight
	sshOpenDown
	sshOpenTabs
	sshOpenGroup
	sshOpenBroadcast
)

// openHosts connects to hosts: in the focused shell, a split, tabs, or a
// tab group (with broadcast input).
func (t *sshTab) openHosts(hosts []sshhosts.Host, where sshOpenWhere) {
	if len(hosts) == 0 {
		return
	}
	m := t.w.model
	first := uicore.SSHCommand(hosts[0])
	switch where {
	case sshOpenHere:
		t.w.runInPane(first)
	case sshOpenRight:
		m.Split(uicore.SplitRow, nil, first)
	case sshOpenDown:
		m.Split(uicore.SplitColumn, nil, first)
	case sshOpenTabs:
		for _, h := range hosts {
			m.Open(uicore.OpenOptions{Command: uicore.SSHCommand(h)})
		}
	case sshOpenGroup, sshOpenBroadcast:
		var ids []int
		for _, h := range hosts {
			ids = append(ids, m.Open(uicore.OpenOptions{Command: uicore.SSHCommand(h)}))
		}
		if len(ids) < 2 {
			return
		}
		g := m.CreateGroup(ids[0])
		if g == 0 {
			return
		}
		name := uicore.SSHGroupName(hosts)
		m.SetGroup(g, &name, nil)
		for _, id := range ids[1:] {
			m.AddToGroup(id, g)
		}
		if where == sshOpenBroadcast {
			t.w.toggleBroadcastGroup(g)
		}
		m.Activate(ids[0])
	}
}

// sshNote is a section's empty state: a bold line (if any), then the text.
func sshNote(title, text string) *gtk.Box {
	b := gtk.NewBox(gtk.OrientationVertical, 4)
	b.AddCSSClass("wtf-ssh-note")
	if title != "" {
		b.Append(sideLabel(title, "wtf-ssh-note-title"))
	}
	b.Append(wrapLabel(text, "dim-label"))
	return b
}

// sshSetNote shows a note in holder, or none.
func sshSetNote(holder *gtk.Box, note *gtk.Box) {
	if c := holder.FirstChild(); c != nil {
		holder.Remove(c)
	}
	if note != nil {
		holder.Append(note)
	}
}

func sshProblemRows(problems []string) []sideRow {
	var rows []sideRow
	for _, p := range problems {
		rows = append(rows, sideRow{id: "p:" + p, icon: "dialog-warning-symbolic", title: p, titleClass: "error", tip: p})
	}
	return rows
}

// sshToolButton is a small icon button outside a pane header.
func sshToolButton(icon, tip string, run func()) *gtk.Button {
	b := gtk.NewButtonFromIconName(icon)
	b.AddCSSClass("flat")
	b.AddCSSClass("wtf-tool")
	b.SetTooltipText(tip)
	b.SetFocusOnClick(false)
	b.ConnectClicked(run)
	return b
}

// Hosts

type sshHostsSection struct {
	t           *sshTab
	root        *gtk.Box
	filter      *gtk.SearchEntry
	reload      *gtk.Button
	note        *gtk.Box
	list        *sideList
	footer      *gtk.Box
	footerLabel *gtk.Label
}

func newSSHHostsSection(t *sshTab) *sshHostsSection {
	s := &sshHostsSection{t: t}
	s.filter = gtk.NewSearchEntry()
	s.filter.SetPlaceholderText("Filter hosts")
	s.filter.SetHExpand(true)
	s.filter.ConnectSearchChanged(s.rebuild)
	s.filter.ConnectActivate(func() {
		if ids := s.list.selected(); len(ids) > 0 {
			s.open(ids, sshOpenEnter(ids))
		}
	})
	s.filter.ConnectStopSearch(func() { s.list.list.UnselectAll() })
	keys := gtk.NewEventControllerKey()
	keys.ConnectKeyPressed(func(keyval, _ uint, _ gdk.ModifierType) bool {
		if keyval == gdk.KEY_Down {
			s.list.list.ChildFocus(gtk.DirTabForward)
			return true
		}
		return false
	})
	s.filter.AddController(keys)
	top := gtk.NewBox(gtk.OrientationHorizontal, 2)
	top.SetMarginStart(8)
	top.SetMarginEnd(4)
	top.Append(s.filter)
	top.Append(sshToolButton("list-add-symbolic", "Add host", func() { t.showHostEditor(nil, nil) }))
	s.reload = sshToolButton("view-refresh-symbolic", "Reload ~/.ssh/config", t.loadHosts)
	top.Append(s.reload)

	s.note = gtk.NewBox(gtk.OrientationVertical, 0)
	s.list = newSideList(true)
	s.list.onActivate = func(id string) {
		if ids := s.list.selected(); len(ids) > 1 && slices.Contains(ids, id) {
			s.open(ids, sshOpenGroup)
		} else {
			s.open([]string{id}, sshOpenHere)
		}
	}
	s.list.onMenu = func(ids []string, x, y float64) {
		if items := s.menu(ids); len(items) > 0 {
			s.list.menu.show(x, y, items, nil)
		}
	}
	s.list.onSelection = func([]string) { s.updateFooter() }
	s.list.onDrop = func(id, path string) {
		if h, ok := s.host(id); ok {
			t.showTransfer(h, true, []string{path})
		}
	}

	s.footerLabel = sideLabel("", "")
	s.footerLabel.SetHExpand(true)
	openGroup := gtk.NewButtonWithLabel("Open as Tab Group")
	openGroup.AddCSSClass("flat")
	openGroup.SetFocusOnClick(false)
	openGroup.ConnectClicked(func() { s.open(s.list.selected(), sshOpenGroup) })
	s.footer = gtk.NewBox(gtk.OrientationHorizontal, 4)
	s.footer.AddCSSClass("wtf-ssh-bar")
	s.footer.Append(s.footerLabel)
	s.footer.Append(openGroup)
	s.footer.SetVisible(false)

	s.root = gtk.NewBox(gtk.OrientationVertical, 4)
	s.root.Append(top)
	s.root.Append(s.note)
	s.root.Append(s.list.root)
	s.root.Append(s.footer)
	return s
}

func (s *sshHostsSection) widget() gtk.Widgetter { return s.root }

func (s *sshHostsSection) host(id string) (sshhosts.Host, bool) {
	for _, h := range s.t.list.Hosts {
		if uicore.SSHHostID(h) == id {
			return h, true
		}
	}
	return sshhosts.Host{}, false
}

func (s *sshHostsSection) hosts(ids []string) []sshhosts.Host {
	var out []sshhosts.Host
	for _, id := range ids {
		if h, ok := s.host(id); ok {
			out = append(out, h)
		}
	}
	return out
}

// sshOpenEnter is where Enter opens the selected hosts.
func sshOpenEnter(ids []string) sshOpenWhere {
	if len(ids) > 1 {
		return sshOpenGroup
	}
	return sshOpenHere
}

func (s *sshHostsSection) rebuild() {
	t := s.t
	s.reload.SetSensitive(!t.hostsLoading)
	q := s.filter.Text()
	var shown, configured, known []sshhosts.Host
	var files []string
	for _, h := range t.list.Hosts {
		if !uicore.SSHMatches(h, q) {
			continue
		}
		shown = append(shown, h)
		if h.Known {
			known = append(known, h)
			continue
		}
		configured = append(configured, h)
		if !slices.Contains(files, h.Source) {
			files = append(files, h.Source)
		}
	}
	var sections []sideSection
	for _, f := range files {
		sec := sideSection{title: t.pretty(f), tip: t.pretty(f)}
		if len(files) == 1 {
			sec.title = "SSH config"
		}
		for _, h := range configured {
			if h.Source == f {
				sec.rows = append(sec.rows, s.row(h))
			}
		}
		sections = append(sections, sec)
	}
	if len(known) > 0 {
		sec := sideSection{title: "Known hosts", tip: "Servers in ~/.ssh/known_hosts that your config doesn’t name"}
		for _, h := range known {
			sec.rows = append(sec.rows, s.row(h))
		}
		sections = append(sections, sec)
	}
	if len(t.list.Problems) > 0 {
		sections = append(sections, sideSection{title: "Problems", rows: sshProblemRows(t.list.Problems)})
	}
	s.list.set(sections)
	var note *gtk.Box
	if t.hostsLoaded && len(shown) == 0 {
		if strings.TrimSpace(q) == "" {
			note = sshNote("No SSH hosts", "Hosts from ~/.ssh/config (and the files it includes) show here, along with servers you’ve connected to before. Press + to add one.")
		} else {
			note = sshNote("", "No hosts match “"+q+"”.")
		}
	}
	sshSetNote(s.note, note)
	s.updateFooter()
}

func (s *sshHostsSection) row(h sshhosts.Host) sideRow {
	tip := uicore.SSHCommand(h)
	if h.Source != "" {
		tip += "\nfrom " + s.t.pretty(h.Source)
	}
	tip += "\nDrop a file here to upload it"
	var pills []sidePill
	for _, f := range uicore.SSHFacts(h) {
		pills = append(pills, sidePill{text: f.Label, tip: f.Title})
	}
	return sideRow{id: uicore.SSHHostID(h), icon: "network-server-symbolic", title: h.Alias, mono: true,
		detail: uicore.SSHDetail(h), pills: pills, tip: tip,
		button: "media-playback-start-symbolic", buttonTip: "Connect in this shell (a new tab if it’s busy)",
		buttonRun: func() { s.t.openHosts([]sshhosts.Host{h}, sshOpenHere) }}
}

func (s *sshHostsSection) updateFooter() {
	ids := s.list.selected()
	s.footer.SetVisible(len(ids) > 1)
	if len(ids) > 1 {
		s.footerLabel.SetText(fmt.Sprintf("%d selected", len(ids)))
		s.footer.SetTooltipText("Opens each host in a tab, grouped as “" + uicore.SSHGroupName(s.hosts(ids)) + "”")
	}
}

func (s *sshHostsSection) open(ids []string, where sshOpenWhere) {
	hosts := s.hosts(ids)
	if len(hosts) == 0 {
		return
	}
	s.list.list.UnselectAll()
	s.t.openHosts(hosts, where)
}

func (s *sshHostsSection) menu(ids []string) []*menuItem {
	t, w := s.t, s.t.w
	hosts := s.hosts(ids)
	if len(hosts) > 1 {
		n := len(hosts)
		return []*menuItem{
			item(fmt.Sprintf("Open %d Hosts in a Tab Group", n), func() { s.open(ids, sshOpenGroup) }),
			item(fmt.Sprintf("Open %d Hosts in a Tab Group with Broadcast Input", n), func() { s.open(ids, sshOpenBroadcast) }),
			item(fmt.Sprintf("Open %d Hosts in Tabs", n), func() { s.open(ids, sshOpenTabs) }),
			menuSection,
			item("Copy ssh Commands", func() {
				var cmds []string
				for _, h := range hosts {
					cmds = append(cmds, uicore.SSHCommand(h))
				}
				t.copy(strings.Join(cmds, "\n"), "Copied ssh commands")
			}),
		}
	}
	if len(hosts) == 0 {
		return nil
	}
	h := hosts[0]
	items := []*menuItem{
		item("Open Here", func() { s.open(ids, sshOpenHere) }),
		item("Open in New Tab", func() { s.open(ids, sshOpenTabs) }),
		item("Open in Split Right", func() { s.open(ids, sshOpenRight) }),
		item("Open in Split Down", func() { s.open(ids, sshOpenDown) }),
		item("Open sftp Session", func() { w.runInPane(uicore.SFTPCommand(h)) }),
		menuSection,
		item("Upload Files…", func() { t.showTransfer(h, true, nil) }),
		item("Download…", func() { t.showTransfer(h, false, nil) }),
		item("New Tunnel…", func() { t.showTunnelEditor(nil, &h, nil) }),
		menuSection,
		item("Install Public Key…", func() { t.showKeyInstall(nil, &h) }),
		item("Host Keys…", func() { t.showHostKeys(h) }),
		menuSection,
		item("Copy ssh Command", func() { t.copy(uicore.SSHCommand(h), "Copied ssh command") }),
		item("Copy Host Name", func() {
			name := h.HostName
			if name == "" {
				name = h.Alias
			}
			t.copy(name, "Copied host name")
		}),
		menuSection,
	}
	switch {
	case h.Known:
		items = append(items, item("Save as Host…", func() {
			d := uicore.HostDraft{Name: h.Alias, HostName: h.Alias, Port: h.Port}
			t.showHostEditor(nil, &d)
		}))
	case !h.Editable:
		items = append(items, item("Managed by "+t.pretty(h.Source), nil))
	default:
		items = append(items,
			item("Edit Host…", func() { t.showHostEditor(&h, nil) }),
			item("Delete Host…", func() { t.confirmDeleteHost(h) }))
	}
	return items
}

// Keys

type sshKeysSection struct {
	t         *sshTab
	root      *gtk.Box
	reload    *gtk.Button
	note      *gtk.Box
	list      *sideList
	agent     *gtk.Label
	unloadAll *gtk.Button
}

func newSSHKeysSection(t *sshTab) *sshKeysSection {
	s := &sshKeysSection{t: t}
	header := newPaneHeader("~/.ssh")
	header.tool("list-add-symbolic", "New key", t.w.showGenerateKey)
	s.reload = header.tool("view-refresh-symbolic", "Reload", t.loadKeys)
	s.note = gtk.NewBox(gtk.OrientationVertical, 0)
	s.list = newSideList(false)
	s.list.onActivate = func(id string) {
		if k, ok := s.key(id); ok {
			if k.Path == "" {
				s.copyFingerprint(k)
			} else {
				t.copyPublicKey(k.Path, "Copied "+k.Name+".pub", nil)
			}
		}
	}
	s.list.onMenu = func(ids []string, x, y float64) {
		if k, ok := s.key(ids[0]); ok {
			s.list.menu.show(x, y, s.menu(k), nil)
		}
	}
	s.agent = sideLabel("", "")
	s.agent.SetHExpand(true)
	s.unloadAll = gtk.NewButtonWithLabel("Unload All")
	s.unloadAll.AddCSSClass("flat")
	s.unloadAll.SetFocusOnClick(false)
	s.unloadAll.ConnectClicked(func() {
		t.w.app.confirm(&t.w.win.Window, "Unload all keys from ssh-agent?",
			"The key files stay in ~/.ssh. Connections already open keep working.", "Unload All", func() {
				svc := t.svc()
				t.act("Unloaded every key", svc.AgentRemoveAll)
			})
	})
	footer := gtk.NewBox(gtk.OrientationHorizontal, 4)
	footer.AddCSSClass("wtf-ssh-bar")
	footer.Append(s.agent)
	footer.Append(s.unloadAll)

	s.root = gtk.NewBox(gtk.OrientationVertical, 4)
	s.root.Append(header.root)
	s.root.Append(s.note)
	s.root.Append(s.list.root)
	s.root.Append(footer)
	return s
}

func (s *sshKeysSection) widget() gtk.Widgetter { return s.root }

func sshKeyID(k sshhosts.Key) string {
	if k.Path == "" {
		return "a:" + k.Fingerprint
	}
	return k.Path
}

func (s *sshKeysSection) key(id string) (sshhosts.Key, bool) {
	for _, k := range s.t.keys.Keys {
		if sshKeyID(k) == id {
			return k, true
		}
	}
	return sshhosts.Key{}, false
}

// keyName is the key's file name in ~/.ssh (its path elsewhere), or the
// agent's name for it.
func (t *sshTab) keyName(k sshhosts.Key) string {
	if k.Path == "" {
		return k.Name
	}
	return strings.ReplaceAll(t.pretty(k.Path), "~/.ssh/", "")
}

func (s *sshKeysSection) row(k sshhosts.Key) sideRow {
	var pills []sidePill
	if k.Unprotected != "" {
		pills = append(pills, sidePill{text: k.Unprotected, class: "wtf-pill-fail", tip: "ssh refuses keys other users can read"})
	}
	if k.InAgent {
		pills = append(pills, sidePill{text: "agent", class: "wtf-pill-running"})
	}
	if n := len(k.UsedBy); n > 0 {
		pills = append(pills, sidePill{text: fmt.Sprintf("%d %s", n, plural(n, "host"))})
	}
	tip := []string{k.Fingerprint}
	if k.Path != "" {
		if k.Encrypted {
			tip = append(tip, "Protected by a passphrase")
		} else {
			tip = append(tip, "No passphrase (or not recorded in the file)")
		}
		if d, err := time.Parse(time.RFC3339, k.Modified); err == nil {
			tip = append(tip, "File written "+d.Local().Format("Jan 2, 2006")+": when the key was made, unless its passphrase was changed since")
		}
	}
	if len(k.UsedBy) > 0 {
		tip = append(tip, "IdentityFile for "+strings.Join(k.UsedBy, ", "))
	}
	if k.Unprotected != "" {
		tip = append(tip, "Readable by other users ("+k.Unprotected+"): ssh won’t use it until it’s made private")
	}
	return sideRow{id: sshKeyID(k), icon: "dialog-password-symbolic", title: s.t.keyName(k), mono: true,
		detail: uicore.SSHKeyDetails(k), pills: pills, tip: strings.Join(tip, "\n")}
}

func (s *sshKeysSection) rebuild() {
	t := s.t
	s.reload.SetSensitive(!t.keysLoading)
	files := sideSection{}
	agentOnly := sideSection{title: "Agent only", tip: "Keys ssh-agent holds that aren’t files in ~/.ssh — from a password manager or a security key, say"}
	inAgent := 0
	for _, k := range t.keys.Keys {
		if k.Path == "" {
			agentOnly.rows = append(agentOnly.rows, s.row(k))
		} else {
			files.rows = append(files.rows, s.row(k))
		}
		if k.InAgent {
			inAgent++
		}
	}
	sections := []sideSection{files, agentOnly}
	if len(t.keys.Problems) > 0 {
		sections = append(sections, sideSection{title: "Problems", rows: sshProblemRows(t.keys.Problems)})
	}
	s.list.set(sections)
	var note *gtk.Box
	if t.keysLoaded && len(t.keys.Keys) == 0 {
		note = sshNote("No keys in ~/.ssh", "A key lets you log in without a password. Press + to make one, then install it on your servers.")
	}
	sshSetNote(s.note, note)

	// The agent, in the footer.
	s.agent.RemoveCSSClass("error")
	s.agent.RemoveCSSClass("dim-label")
	switch {
	case t.keys.AgentError != "":
		s.agent.SetText("ssh-agent: " + t.keys.AgentError)
		s.agent.AddCSSClass("error")
	case inAgent == 0:
		s.agent.SetText("ssh-agent: no keys loaded")
		s.agent.AddCSSClass("dim-label")
	default:
		s.agent.SetText(fmt.Sprintf("ssh-agent: %d %s loaded", inAgent, plural(inAgent, "key")))
		s.agent.AddCSSClass("dim-label")
	}
	if t.keys.AgentSocket == "" {
		s.agent.SetTooltipText("SSH_AUTH_SOCK isn’t set")
	} else {
		s.agent.SetTooltipText("SSH_AUTH_SOCK=" + t.keys.AgentSocket)
	}
	s.unloadAll.SetVisible(inAgent > 0 && t.keys.AgentError == "")
}

func (s *sshKeysSection) copyFingerprint(k sshhosts.Key) {
	s.t.copy(k.Fingerprint, "Copied fingerprint")
}

func (s *sshKeysSection) menu(k sshhosts.Key) []*menuItem {
	t, w := s.t, s.t.w
	svc := t.svc()
	n := t.keyName(k)
	quoted := uicore.QuotePath(k.Path, w.app.home)
	file := k.Path != ""
	var items []*menuItem
	if file {
		items = append(items,
			item("Copy Public Key", func() { t.copyPublicKey(k.Path, "Copied "+k.Name+".pub", nil) }),
			item("Install on Host…", func() { t.showKeyInstall(&k, nil) }),
			menuSection)
	}
	if file && !k.InAgent {
		items = append(items, item("Load into Agent", func() { w.runInPane(uicore.SSHAgentAdd(quoted, false)) }))
	}
	if k.InAgent {
		items = append(items, item("Unload from Agent", func() {
			t.act("Unloaded "+n, func() error { return svc.AgentRemove(k.Fingerprint) })
		}))
	}
	if file {
		label := "Set Passphrase…"
		if k.Encrypted {
			label = "Change Passphrase…"
		}
		items = append(items, item(label, func() { w.runInPane("ssh-keygen -p -f " + quoted) }))
		if k.Unprotected != "" {
			items = append(items, item("Make Private (chmod 600)", func() {
				t.act(n+" is now private", func() error { return svc.ProtectKey(k.Path) })
			}))
		}
		if !k.HasPub {
			items = append(items, item("Create .pub File", func() {
				t.act("Wrote "+n+".pub", func() error { return svc.WritePublicKey(k.Path) })
			}))
		}
	}
	items = append(items, menuSection, item("Copy Fingerprint", func() { s.copyFingerprint(k) }))
	if file {
		items = append(items,
			item("Show in Files", func() {
				go func() {
					if err := w.app.be.Files.Reveal(k.Path); err != nil {
						onMainAsync(func() { w.toast("Couldn't show it: " + err.Error()) })
					}
				}()
			}),
			menuSection,
			item("Delete Key…", func() { s.confirmDelete(k) }))
	}
	return items
}

func (s *sshKeysSection) confirmDelete(k sshhosts.Key) {
	t := s.t
	n := t.keyName(k)
	pub := ""
	if k.HasPub {
		pub = " and its .pub"
	}
	msg := "Moves the key" + pub + " out of ~/.ssh into wtf’s cache folder (ssh-backups/keys), where you can still get it back. Servers that only accept this key will refuse you."
	if k.InAgent {
		msg += " The agent keeps its copy until you unload it."
	}
	if len(k.UsedBy) > 0 {
		var used []string
		for _, h := range k.UsedBy {
			used = append(used, "Used by "+h)
		}
		msg += "\n\n" + strings.Join(used, "\n")
	}
	svc := t.svc()
	t.w.app.confirm(&t.w.win.Window, "Delete "+n+"?", msg, "Delete Key", func() {
		t.act("Moved "+n+" to wtf’s cache folder", func() error { return svc.DeleteKey(k.Path) })
	})
}

// Tunnels

type sshTunnelsSection struct {
	t    *sshTab
	root *gtk.Box
	note *gtk.Box
	list *sideList
}

func newSSHTunnelsSection(t *sshTab) *sshTunnelsSection {
	s := &sshTunnelsSection{t: t}
	header := newPaneHeader("Port forwards")
	header.tool("list-add-symbolic", "New tunnel", func() { t.showTunnelEditor(nil, nil, nil) })
	s.note = gtk.NewBox(gtk.OrientationVertical, 0)
	s.list = newSideList(false)
	s.list.onActivate = func(id string) {
		if x, ok := s.tunnel(id); ok {
			t.showTunnelEditor(&x, nil, nil)
		}
	}
	s.list.onMenu = func(ids []string, x, y float64) {
		if tn, ok := s.tunnel(ids[0]); ok {
			s.list.menu.show(x, y, tunnelItems(t.w, tn, t.setTunnels, func() { t.showTunnelEditor(&tn, nil, nil) }), nil)
		}
	}
	s.root = gtk.NewBox(gtk.OrientationVertical, 4)
	s.root.Append(header.root)
	s.root.Append(s.note)
	s.root.Append(s.list.root)
	return s
}

func (s *sshTunnelsSection) widget() gtk.Widgetter { return s.root }

func (s *sshTunnelsSection) tunnel(id string) (sshhosts.TunnelStatus, bool) {
	for _, x := range s.t.tunnels {
		if x.ID == id {
			return x, true
		}
	}
	return sshhosts.TunnelStatus{}, false
}

func (s *sshTunnelsSection) rebuild() {
	t := s.t
	sec := sideSection{}
	for _, x := range t.tunnels {
		if x.Kind != "kubectl" {
			sec.rows = append(sec.rows, tunnelRow(t.w, x, t.setTunnels))
		}
	}
	s.list.set([]sideSection{sec})
	var note *gtk.Box
	if t.tunnelsLoaded && len(sec.rows) == 0 {
		note = sshNote("No tunnels", "Forward a port through ssh: reach a database behind a bastion on localhost, expose a local dev server to a server, or browse through a SOCKS proxy. Press + to add one, or right-click a host.")
	}
	sshSetNote(s.note, note)
}

// tunnelRow is a tunnel's row, for the Kubernetes forwards too; set takes
// the list a start or stop returns.
func tunnelRow(w *Window, x sshhosts.TunnelStatus, set func([]sshhosts.TunnelStatus)) sideRow {
	state := "stopped"
	switch x.State {
	case sshhosts.TunnelUp:
		state = "up"
	case sshhosts.TunnelConnecting, sshhosts.TunnelRetrying:
		state = "connecting"
	case sshhosts.TunnelFailed:
		state = "failed"
	}
	desc := uicore.TunnelDescribe(x)
	r := sideRow{id: x.ID, dot: "dot-tunnel-" + state, title: desc, detail: uicore.TunnelStateLabel(x.State), tip: x.Command}
	if x.Name != "" {
		r.title, r.detail = x.Name, desc+" · "+r.detail
	}
	if x.State == sshhosts.TunnelFailed || x.State == sshhosts.TunnelRetrying {
		r.alert = x.Error
	}
	if x.AutoStart {
		r.tip += "\nStarts when wtf opens"
	}
	r.button, r.buttonTip = "media-playback-start-symbolic", "Start"
	if uicore.TunnelIsOn(x) {
		r.button, r.buttonTip = "media-playback-stop-symbolic", "Stop"
	}
	r.buttonRun = func() { toggleTunnel(w, x, set) }
	return r
}

// toggleTunnel starts a tunnel that's off, or stops one that's on.
func toggleTunnel(w *Window, x sshhosts.TunnelStatus, set func([]sshhosts.TunnelStatus)) {
	svc := w.app.be.SSHHosts
	on := uicore.TunnelIsOn(x)
	go func() {
		var l []sshhosts.TunnelStatus
		var err error
		if on {
			l, err = svc.StopTunnel(x.ID)
		} else {
			l, err = svc.StartTunnel(x.ID)
		}
		onMainAsync(func() {
			if err != nil {
				w.toast(err.Error())
				return
			}
			set(l)
		})
	}()
}

// tunnelItems are a tunnel's menu, for the Kubernetes forwards too.
func tunnelItems(w *Window, x sshhosts.TunnelStatus, set func([]sshhosts.TunnelStatus), edit func()) []*menuItem {
	on := uicore.TunnelIsOn(x)
	label := "Start"
	if on {
		label = "Stop"
	}
	items := []*menuItem{item(label, func() { toggleTunnel(w, x, set) })}
	if port, ok := uicore.TunnelLocalPort(x); ok {
		if x.Kind == "local" || x.Kind == "kubectl" {
			items = append(items, item("Open localhost:"+port+" in Browser", func() { sshOpenURL(w, "http://localhost:"+port) }))
		}
		items = append(items, item("Copy localhost:"+port, func() {
			w.win.Clipboard().SetText("localhost:" + port)
			w.toast("Copied localhost:" + port)
		}))
	}
	items = append(items,
		item("Copy Command", func() {
			w.win.Clipboard().SetText(x.Command)
			w.toast("Copied command")
		}),
		menuSection,
		item("Edit…", edit),
		item("Delete", func() {
			title := x.Name
			if title == "" {
				title = "this port forward"
			}
			msg := uicore.TunnelDescribe(x) + "."
			if on {
				msg = uicore.TunnelDescribe(x) + ". It stops now."
			}
			svc := w.app.be.SSHHosts
			w.app.confirm(&w.win.Window, "Delete "+title+"?", msg, "Delete", func() {
				go func() {
					l, err := svc.DeleteTunnel(x.ID)
					onMainAsync(func() {
						if err != nil {
							w.toast(err.Error())
							return
						}
						set(l)
					})
				}()
			})
		}))
	return items
}

// Git identities

type sshGitSection struct {
	t       *sshTab
	root    *gtk.Box
	bar     *gtk.Box
	barText *gtk.Label
	note    *gtk.Box
	list    *sideList
	// The identity the focused shell uses, as last shown.
	current string
}

func newSSHGitSection(t *sshTab) *sshGitSection {
	s := &sshGitSection{t: t}
	header := newPaneHeader("Git identities")
	header.tool("list-add-symbolic", "New identity", func() { t.showIdentityEditor(nil, "") })
	s.barText = sideLabel("", "")
	s.barText.SetHExpand(true)
	stop := gtk.NewButtonWithLabel("Stop Using")
	stop.AddCSSClass("flat")
	stop.SetFocusOnClick(false)
	stop.ConnectClicked(s.stopUsing)
	s.bar = gtk.NewBox(gtk.OrientationHorizontal, 4)
	s.bar.AddCSSClass("wtf-ssh-bar")
	s.bar.Append(s.barText)
	s.bar.Append(stop)
	s.note = gtk.NewBox(gtk.OrientationVertical, 0)
	s.list = newSideList(false)
	s.list.onActivate = func(id string) {
		if i, ok := s.identity(id); ok {
			s.use(i)
		}
	}
	s.list.onMenu = func(ids []string, x, y float64) {
		if i, ok := s.identity(ids[0]); ok {
			s.list.menu.show(x, y, s.menu(i), nil)
		}
	}
	s.root = gtk.NewBox(gtk.OrientationVertical, 4)
	s.root.Append(header.root)
	s.root.Append(s.bar)
	s.root.Append(s.note)
	s.root.Append(s.list.root)
	return s
}

func (s *sshGitSection) widget() gtk.Widgetter { return s.root }

// currentIdentity is the identity the focused shell uses, if any.
func (t *sshTab) currentIdentity() string {
	if ps := t.w.model.ActivePane(); ps != nil {
		return ps.Env[uicore.IdentityVar]
	}
	return ""
}

func (s *sshGitSection) identity(id string) (sshhosts.GitIdentity, bool) {
	for _, i := range s.t.identities {
		if i.ID == id {
			return i, true
		}
	}
	return sshhosts.GitIdentity{}, false
}

// author is "Name <email>", or whichever is set.
func identityAuthor(i sshhosts.GitIdentity) string {
	switch {
	case i.AuthorName != "" && i.AuthorEmail != "":
		return i.AuthorName + " <" + i.AuthorEmail + ">"
	case i.AuthorName != "":
		return i.AuthorName
	case i.AuthorEmail != "":
		return i.AuthorEmail
	}
	return "author from git config"
}

func (s *sshGitSection) rebuild() {
	t := s.t
	cur := t.currentIdentity()
	s.current = cur
	s.bar.SetVisible(cur != "")
	s.barText.SetMarkup("This tab: <b>" + escapeMarkup(cur) + "</b>")
	sec := sideSection{}
	for _, i := range t.identities {
		var pills []sidePill
		if i.GitHubUser != "" {
			pills = append(pills, sidePill{text: "@" + i.GitHubUser, tip: "The GitHub account this key logged in as at the last test"})
		}
		if i.Name == cur {
			pills = append(pills, sidePill{text: "this tab", class: "wtf-pill-running"})
		}
		if t.testing[i.ID] {
			pills = append(pills, sidePill{text: "testing…"})
		}
		tested := "GitHub account: not tested yet (right-click → Test GitHub Login)"
		if i.GitHubUser != "" {
			tested = "GitHub account: " + i.GitHubUser + ", as of the last test (" + uicore.RelativeTime(time.UnixMilli(i.TestedAt), time.Now()) + ")"
		}
		keyName := strings.ReplaceAll(t.pretty(i.KeyPath), "~/.ssh/", "")
		sec.rows = append(sec.rows, sideRow{id: i.ID, icon: "dialog-password-symbolic", title: i.Name,
			detail: identityAuthor(i) + " · " + keyName, pills: pills, highlighted: i.Name == cur,
			tip: "Commits as: " + identityAuthor(i) + "\nSSH key: " + t.pretty(i.KeyPath) + " (offered alone: IdentitiesOnly)\n" + tested})
	}
	s.list.set([]sideSection{sec})
	var note *gtk.Box
	if t.identitiesLoaded && len(sec.rows) == 0 {
		note = sshNote("Work on GitHub as more than one user", "An identity pairs an SSH key with the name and email to commit as. Use one in a tab and git in that tab pushes and commits as that user, while other tabs stay as they are. Press + to add one.")
	}
	sshSetNote(s.note, note)
}

func (s *sshGitSection) use(i sshhosts.GitIdentity) {
	if !s.t.runInShell(uicore.UseIdentity(i)) {
		s.t.w.toast("Opened a new tab as " + i.Name + ": that one was busy")
	}
}

func (s *sshGitSection) stopUsing() {
	s.t.w.runInPane(uicore.StopIdentity)
}

func (s *sshGitSection) menu(i sshhosts.GitIdentity) []*menuItem {
	t, w := s.t, s.t.w
	svc := t.svc()
	keyName := strings.ReplaceAll(t.pretty(i.KeyPath), "~/.ssh/", "")
	isCurrent := i.Name == t.currentIdentity()
	use := "Use in This Tab"
	if isCurrent {
		use = "Use Again in This Tab"
	}
	items := []*menuItem{
		item(use, func() { s.use(i) }),
		item("Open New Tab as "+i.Name, func() { w.model.Open(uicore.OpenOptions{Command: uicore.UseIdentity(i)}) }),
	}
	if isCurrent {
		items = append(items, item("Stop Using in This Tab", s.stopUsing))
	}
	return append(items,
		menuSection,
		item("Test GitHub Login", func() {
			t.testing[i.ID] = true
			t.changed()
			go func() {
				r, err := svc.TestGitIdentity(i.ID)
				onMainAsync(func() {
					delete(t.testing, i.ID)
					switch {
					case err != nil:
						w.toast(err.Error())
					case r.OK:
						w.toast(i.Name + " logs in to GitHub as " + r.User)
						t.loadIdentities()
					default:
						w.toast(i.Name + ": " + r.Message)
					}
					t.changed()
				})
			}()
		}),
		item("Add Key to GitHub…", func() {
			t.copyPublicKey(i.KeyPath, "Copied "+keyName+".pub. Paste it on GitHub while signed in as the account for "+i.Name,
				func() { sshOpenURL(w, "https://github.com/settings/ssh/new") })
		}),
		item("Copy Public Key", func() { t.copyPublicKey(i.KeyPath, "Copied "+keyName+".pub", nil) }),
		menuSection,
		item("Edit…", func() { t.showIdentityEditor(&i, "") }),
		item("Delete…", func() {
			w.app.confirm(&w.win.Window, "Delete the identity "+i.Name+"?",
				"wtf forgets it. The key stays in "+t.pretty(i.KeyPath)+", and GitHub keeps it. Tabs using it keep their variables until they change them or close.",
				"Delete Identity", func() {
					go func() {
						l, err := svc.DeleteGitIdentity(i.ID)
						onMainAsync(func() {
							if err != nil {
								w.toast(err.Error())
								return
							}
							t.identities = l
							t.changed()
						})
					}()
				})
		}))
}

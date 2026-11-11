//go:build linux

package gtkui

import (
	"fmt"
	"strings"
	"time"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/dayanraj/wtf/internal/netconn"
	"github.com/dayanraj/wtf/internal/sshhosts"
	"github.com/dayanraj/wtf/internal/uicore"
)

func init() {
	registerSidebarTab(sidebarTabInfo{tab: uicore.SidebarNetwork, label: "Network", icon: "network-transmit-receive-symbolic", cmd: "sidebar.network",
		make: func(w *Window) sidePaneTab { return newNetworkTab(w) }})
	extraCSS = append(extraCSS, `
.wtf-side-pill.net-exposed { color: @warning_color; background: alpha(@warning_color, 0.14); }
.wtf-net-link { margin: 0 6px; padding: 0 6px; min-height: 22px; font-size: 0.9em; font-weight: 600; color: @accent_color; }
`)
}

// netStore is the connection listing, shared by the app's windows:
// refreshed by whichever Network tab is on screen. Scope and filter last
// while the app runs (the Processes tab sets the filter to "pid:N"). Like
// the macOS app's NetStore; it also keeps the port forwards, for the
// Forwards section, while a Network tab is open.
type netStore struct {
	a             *App
	conns         []netconn.Conn
	err           string
	loaded, busy  bool
	updatedAt     time.Time
	terminalsOnly bool
	filter        string

	tunnels       []sshhosts.TunnelStatus
	tunnelsLoaded bool
	offTunnels    func()
	tabs          map[*networkTab]bool
}

var netStores = map[*App]*netStore{}

// netStoreOf is the app's store, made with the scope w saved.
func netStoreOf(w *Window) *netStore {
	s := netStores[w.app]
	if s == nil {
		s = &netStore{a: w.app, terminalsOnly: w.sidebarState.NetworkScope != "all", tabs: map[*networkTab]bool{}}
		netStores[w.app] = s
	}
	return s
}

// changed redraws every open Network tab.
func (s *netStore) changed() {
	for t := range s.tabs {
		t.rebuild()
	}
}

func (s *netStore) refresh() {
	if s.busy {
		return
	}
	s.busy = true
	svc := s.a.be.NetConn
	go func() {
		l, err := svc.List()
		onMainAsync(func() {
			if err != nil {
				s.err = err.Error()
			} else {
				s.conns, s.err, s.updatedAt = l, "", time.Now()
			}
			s.loaded, s.busy = true, false
			s.changed()
		})
	}()
}

// join adds an open tab: the first one follows the port forwards (read
// now, then kept by their event) until the last one closes.
func (s *netStore) join(t *networkTab) {
	s.tabs[t] = true
	if s.offTunnels != nil {
		return
	}
	s.offTunnels = s.a.events.on(sshhosts.EventTunnels, func(data any) {
		if l, ok := data.([]sshhosts.TunnelStatus); ok {
			s.setTunnels(l)
		}
	})
	svc := s.a.be.SSHHosts
	go func() {
		l := svc.Tunnels()
		onMainAsync(func() { s.setTunnels(l) })
	}()
}

func (s *netStore) leave(t *networkTab) {
	delete(s.tabs, t)
	if len(s.tabs) == 0 && s.offTunnels != nil {
		s.offTunnels()
		s.offTunnels = nil
	}
}

// setTunnels takes the list a tunnel action or event carries.
func (s *netStore) setTunnels(l []sshhosts.TunnelStatus) {
	s.tunnels, s.tunnelsLoaded = l, true
	s.changed()
}

// setScope changes the scope for the app, saved in every window.
func (s *netStore) setScope(terminalsOnly bool) {
	s.terminalsOnly = terminalsOnly
	scope := "all"
	if terminalsOnly {
		scope = "terminals"
	}
	for _, w := range s.a.windows {
		w.sidebarState.NetworkScope = scope
		w.scheduleSave()
	}
	s.changed()
}

// Where a session runs, for the Network and Processes tabs

// netPlace is the tab and pane in this window whose shell is session.
func (w *Window) netPlace(session string) (uicore.Tab, *uicore.PaneState, bool) {
	if session == "" {
		return uicore.Tab{}, nil, false
	}
	ps := w.model.PaneBySession(session)
	if ps == nil {
		return uicore.Tab{}, nil, false
	}
	t, ok := w.model.TabOf(ps.ID)
	return t, ps, ok
}

// netJump shows the tab and focuses the pane running session, if it's in
// this window.
func (w *Window) netJump(session string) {
	t, ps, ok := w.netPlace(session)
	if !ok {
		return
	}
	w.model.Activate(t.ID)
	w.model.FocusPane(ps.ID)
	w.focusActive()
}

// netOwnerPill is who started a connection or process: its tab's title,
// or another window.
func (w *Window) netOwnerPill(session string) (sidePill, bool) {
	if session == "" {
		return sidePill{}, false
	}
	t, _, ok := w.netPlace(session)
	if !ok {
		return sidePill{text: "Another window", tip: "Started from a terminal in another window"}, true
	}
	title := w.model.Title(t)
	short := title
	if r := []rune(title); len(r) > 18 {
		short = string(r[:17]) + "…"
	}
	return sidePill{text: short, class: "in-use", tip: "Go to “" + title + "” (double-click)"}, true
}

// netRefreshTools adds the pause and refresh tools to a header; the
// returned func makes the pause button follow the setting.
func netRefreshTools(w *Window, h *paneHeader, refresh func()) func() {
	pause := h.tool("media-playback-pause-symbolic", "", func() { toggleAutoRefresh(w) })
	h.tool("view-refresh-symbolic", "Refresh now", refresh)
	return func() {
		if w.app.settings.config().SidebarAutoRefresh {
			pause.SetIconName("media-playback-pause-symbolic")
			pause.SetTooltipText("Pause auto-refresh (side pane updates only when you ask)")
			pause.RemoveCSSClass("active")
		} else {
			pause.SetIconName("media-playback-start-symbolic")
			pause.SetTooltipText("Resume auto-refresh")
			pause.AddCSSClass("active")
		}
	}
}

// netPausedText is "Paused", with when it last updated.
func netPausedText(updated time.Time) string {
	if updated.IsZero() {
		return "Paused"
	}
	return "Paused · updated " + updated.Format("15:04:05")
}

// netScopeSwitch is the Terminals / All switch; set marks one without
// calling changed.
func netScopeSwitch(tip string, labels []string, changed func(i int)) (box *gtk.Box, set func(i int)) {
	box = gtk.NewBox(gtk.OrientationHorizontal, 0)
	box.AddCSSClass("linked")
	box.AddCSSClass("wtf-ssh-switcher")
	box.SetHomogeneous(true)
	box.SetTooltipText(tip)
	var buttons []*gtk.ToggleButton
	setting := false
	for i, l := range labels {
		b := gtk.NewToggleButtonWithLabel(l)
		b.SetFocusOnClick(false)
		if i > 0 {
			b.SetGroup(buttons[0])
		}
		b.ConnectToggled(func() {
			if b.Active() && !setting {
				changed(i)
			}
		})
		buttons = append(buttons, b)
		box.Append(b)
	}
	set = func(i int) {
		setting = true
		buttons[i].SetActive(true)
		setting = false
	}
	return box, set
}

// netNoteButton adds a link-like button under a note.
// installLink adds an Install… link to a note whose error says a tool is
// missing; it opens Command-Line Tools at that tool.
func installLink(w *Window, note *gtk.Box, err string) {
	if tool := uicore.MissingTool(err); tool != "" {
		netNoteButton(note, "Install…", func() { w.showTools(tool) })
	}
}

func netNoteButton(note *gtk.Box, label string, run func()) {
	b := gtk.NewButtonWithLabel(label)
	b.AddCSSClass("flat")
	b.AddCSSClass("wtf-net-link")
	b.SetHAlign(gtk.AlignStart)
	b.SetFocusOnClick(false)
	b.ConnectClicked(run)
	note.Append(b)
	note.SetVisible(true)
}

// networkTab is the Network tab: port forwards, then what's listening,
// connected and otherwise open — from your terminals or everything.
// Polled only while on screen and the window is in front. Like
// the macOS app's NetworkView.
type networkTab struct {
	w          *Window
	st         *netStore
	root       *gtk.Box
	pauseLook  func()
	setScope   func(int)
	paused     *gtk.Label
	filter     *gtk.SearchEntry
	note       *gtk.Box
	list       *sideList
	poller     *poller
	unobserve  func()
	othersOpen bool
	rows       map[string]netconn.Conn
	closed     bool
}

func newNetworkTab(w *Window) *networkTab {
	n := &networkTab{w: w, st: netStoreOf(w), rows: map[string]netconn.Conn{}}
	header := newPaneHeader("Network")
	header.tool("list-add-symbolic", "New port forward (ssh tunnel or kubectl port-forward)", func() { n.editTunnel(nil) })
	n.pauseLook = netRefreshTools(w, header, n.st.refresh)
	var scope *gtk.Box
	scope, n.setScope = netScopeSwitch("Show connections from", []string{"Terminals", "All"}, func(i int) { n.st.setScope(i == 0) })
	n.paused = sideLabel("Paused", "wtf-side-note")
	n.filter = gtk.NewSearchEntry()
	n.filter.SetPlaceholderText("Filter by port, host, process, PID")
	n.filter.AddCSSClass("wtf-side-filter")
	n.filter.ConnectSearchChanged(func() {
		if q := n.filter.Text(); q != n.st.filter {
			n.st.filter = q
			n.st.changed()
		}
	})
	n.filter.ConnectStopSearch(func() {
		n.filter.SetText("")
		w.focusActive()
	})
	n.note = gtk.NewBox(gtk.OrientationVertical, 0)
	n.list = newSideList(false)
	n.list.onActivate = n.activated
	n.list.onMenu = func(ids []string, x, y float64) {
		if items := n.menu(ids[0]); len(items) > 0 {
			n.list.menu.show(x, y, items, nil)
		}
	}

	n.root = gtk.NewBox(gtk.OrientationVertical, 4)
	n.root.Append(header.root)
	n.root.Append(scope)
	n.root.Append(n.paused)
	n.root.Append(n.filter)
	n.root.Append(n.note)
	n.root.Append(n.list.root)
	n.st.join(n)
	n.unobserve = w.app.settings.observe(func() {
		n.poller.update(w.app.sidebarInterval())
		n.rebuild()
	})
	n.poller = newPoller(w.app, w, w.app.sidebarInterval(), n.st.refresh)
	n.rebuild()
	return n
}

func (n *networkTab) widget() gtk.Widgetter { return n.root }
func (n *networkTab) paneChanged()          {}
func (n *networkTab) commandFinished()      {}

func (n *networkTab) close() {
	n.closed = true
	n.poller.stop()
	n.unobserve()
	n.st.leave(n)
}

func netKey(c netconn.Conn) string {
	return fmt.Sprintf("c:%d|%s|%s|%s|%s", c.PID, c.Protocol, c.Local, c.Remote, c.State)
}

func (n *networkTab) tunnel(id string) (sshhosts.TunnelStatus, bool) {
	tid, ok := strings.CutPrefix(id, "t:")
	if !ok {
		return sshhosts.TunnelStatus{}, false
	}
	for _, x := range n.st.tunnels {
		if x.ID == tid {
			return x, true
		}
	}
	return sshhosts.TunnelStatus{}, false
}

// editTunnel opens the SSH tab's port forward editor (a new one for nil).
// It runs on a detached SSH tab state that loads the hosts it lists; the
// list here follows the tunnels event.
func (n *networkTab) editTunnel(x *sshhosts.TunnelStatus) {
	t := &sshTab{w: n.w, closed: true, switches: map[string]*gtk.ToggleButton{}, testing: map[string]bool{}}
	t.showTunnelEditor(x, nil, nil)
}

func (n *networkTab) rebuild() {
	if n.closed {
		return
	}
	st := n.st
	if n.filter.Text() != st.filter {
		n.filter.SetText(st.filter)
	}
	if st.terminalsOnly {
		n.setScope(0)
	} else {
		n.setScope(1)
	}
	n.pauseLook()
	n.paused.SetVisible(!n.w.app.settings.config().SidebarAutoRefresh)
	n.paused.SetText(netPausedText(st.updatedAt))

	forwardPorts := map[string]bool{}
	for _, t := range st.tunnels {
		if p, ok := uicore.TunnelLocalPort(t); ok && uicore.TunnelIsOn(t) {
			forwardPorts[p] = true
		}
	}
	shown := uicore.NetVisible(st.conns, st.terminalsOnly, st.filter, forwardPorts)
	var listen, connections, others []netconn.Conn
	for _, c := range shown {
		switch {
		case c.State == "LISTEN":
			listen = append(listen, c)
		case c.Remote != "" || c.Target != "":
			connections = append(connections, c)
		default:
			others = append(others, c)
		}
	}
	listening := uicore.NetMergeListeners(listen)
	n.rows = map[string]netconn.Conn{}
	for _, l := range [][]netconn.Conn{listening, connections, others} {
		for _, c := range l {
			n.rows[netKey(c)] = c
		}
	}

	q := strings.ToLower(strings.TrimSpace(st.filter))
	forwards := sideSection{title: "Forwards", always: true}
	for _, t := range st.tunnels {
		if q == "" || strings.Contains(strings.ToLower(t.Name+" "+uicore.TunnelDescribe(t)+" "+t.Command), q) {
			r := tunnelRow(n.w, t, st.setTunnels)
			r.id = "t:" + t.ID
			forwards.rows = append(forwards.rows, r)
		}
	}
	if len(forwards.rows) > 0 {
		forwards.title = fmt.Sprintf("Forwards (%d)", len(forwards.rows))
	} else if st.tunnelsLoaded && q == "" {
		forwards.empty = "None yet. + adds an ssh tunnel or a kubectl port-forward that runs in the background."
	}
	sections := []sideSection{forwards}
	if len(listening) > 0 {
		s := sideSection{title: fmt.Sprintf("Listening (%d)", len(listening))}
		for _, c := range listening {
			s.rows = append(s.rows, n.listenRow(c))
		}
		sections = append(sections, s)
	}
	if len(connections) > 0 {
		s := sideSection{title: fmt.Sprintf("Connections (%d)", len(connections))}
		for _, c := range connections {
			s.rows = append(s.rows, n.connectionRow(c))
		}
		sections = append(sections, s)
	}
	if len(others) > 0 {
		s := sideSection{title: "Other sockets", open: n.othersOpen, badge: fmt.Sprint(len(others)), toggle: func() {
			n.othersOpen = !n.othersOpen
			n.rebuild()
		}}
		for _, c := range others {
			r := sideRow{id: netKey(c), icon: "radio-symbolic", title: c.Process, detail: c.Protocol + " · " + c.Local}
			if p, ok := n.w.netOwnerPill(c.Session); ok {
				r.pills = []sidePill{p}
			}
			s.rows = append(s.rows, r)
		}
		sections = append(sections, s)
	}
	n.list.set(sections)

	switch {
	case st.err != "":
		setNote(n.note, "Can't list connections", st.err)
		installLink(n.w, n.note, st.err)
	case st.loaded && len(shown) == 0 && q != "":
		setNote(n.note, "", "No connections match “"+st.filter+"”.")
	case st.loaded && len(shown) == 0 && st.terminalsOnly:
		setNote(n.note, "Nothing from your terminals", "Programs you start here — dev servers, ssh, database clients — show up as they connect.")
		netNoteButton(n.note, "Show all connections", func() { st.setScope(false) })
	case st.loaded && len(shown) == 0:
		setNote(n.note, "", "No network connections.")
	default:
		setNote(n.note, "", "")
	}
}

func (n *networkTab) listenRow(c netconn.Conn) sideRow {
	host, port := uicore.NetSplitAddress(c.Local)
	detail := c.Protocol
	if s := uicore.NetServiceName(port); s != "" {
		detail += " · " + s
	}
	tip := fmt.Sprintf("%s (pid %d) listening on %s", c.Process, c.PID, c.Local)
	for _, f := range n.st.tunnels {
		if p, ok := uicore.TunnelLocalPort(f); ok && p == port && uicore.TunnelIsOn(f) {
			name := f.Name
			if name == "" {
				name = uicore.TunnelDescribe(f)
			}
			detail += " · forward · " + name
			tip += "\nForward: " + uicore.TunnelDescribe(f)
			break
		}
	}
	pills := []sidePill{{text: "local", tip: "Only reachable from this computer"}}
	if uicore.NetIsExposed(host) {
		pills = []sidePill{{text: "network", class: "net-exposed", tip: "Reachable from other machines on your network"}}
	}
	if p, ok := n.w.netOwnerPill(c.Session); ok {
		pills = append(pills, p)
	}
	return sideRow{id: netKey(c), icon: "network-wireless-hotspot-symbolic", title: ":" + port + "  " + c.Process, detail: detail,
		pills: pills, tip: tip}
}

func (n *networkTab) connectionRow(c netconn.Conn) sideRow {
	var host, port, name, note string
	if c.Remote != "" {
		host, port = uicore.NetSplitAddress(c.Remote)
	} else {
		host = c.TargetIP
		if host == "" {
			host = c.Target
		}
		if c.TargetIP != "" && c.Target != c.TargetIP {
			name = c.Target
		}
		if c.TargetResolved {
			note = fmt.Sprintf("%s resolved here to %s; %s may be using another of its addresses", c.Target, c.TargetIP, c.Process)
		} else if c.TargetIP == "" {
			note = "Couldn't resolve " + c.Target
		}
	}
	hostPort := host
	if port != "" {
		hostPort += ":" + port
	}
	dest := hostPort
	if c.TargetResolved {
		dest += " ≈"
	}
	detail := c.Process + " · " + c.Protocol
	if name != "" {
		detail = name + " · " + detail
	}
	if s := uicore.NetServiceName(port); s != "" {
		detail += " · " + s
	}
	var pills []sidePill
	if s := uicore.NetStateLabel(c.State); s != "" {
		pills = append(pills, sidePill{text: s})
	}
	if p, ok := n.w.netOwnerPill(c.Session); ok {
		pills = append(pills, p)
	}
	tip := fmt.Sprintf("%s (pid %d) → %s", c.Process, c.PID, hostPort)
	if note != "" {
		tip += "\n" + note
	}
	return sideRow{id: netKey(c), icon: "network-transmit-symbolic", title: dest, mono: true, detail: detail, pills: pills, tip: tip}
}

// activated (double-click, Enter): a forward opens its editor; a listener
// or connection goes to the terminal that started it.
func (n *networkTab) activated(id string) {
	if t, ok := n.tunnel(id); ok {
		n.editTunnel(&t)
		return
	}
	if c, ok := n.rows[id]; ok && (c.State == "LISTEN" || c.Remote != "" || c.Target != "") {
		n.w.netJump(c.Session)
	}
}

func (n *networkTab) menu(id string) []*menuItem {
	w := n.w
	if t, ok := n.tunnel(id); ok {
		return tunnelItems(w, t, n.st.setTunnels, func() { n.editTunnel(&t) })
	}
	c, ok := n.rows[id]
	if !ok {
		return nil
	}
	copyText := func(text, what string) func() {
		return func() {
			w.win.Clipboard().SetText(text)
			w.toast("Copied " + what)
		}
	}
	_, port := uicore.NetSplitAddress(c.Local)
	var items []*menuItem
	if c.State == "LISTEN" && c.Protocol == "TCP" {
		items = append(items, item("Open localhost:"+port+" in Browser", func() { sshOpenURL(w, "http://localhost:"+port) }))
	}
	if _, _, here := w.netPlace(c.Session); here {
		items = append(items, item("Go to Tab", func() { w.netJump(c.Session) }))
	}
	if len(items) > 0 {
		items = append(items, menuSection)
	}
	items = append(items, item("Copy Local Address", copyText(c.Local, c.Local)))
	if c.Remote != "" {
		items = append(items, item("Copy Remote Address", copyText(c.Remote, c.Remote)))
	}
	if c.TargetIP != "" {
		items = append(items, item("Copy "+c.TargetIP, copyText(c.TargetIP, c.TargetIP)))
	}
	if c.Target != "" && c.Target != c.TargetIP {
		items = append(items, item("Copy "+c.Target, copyText(c.Target, c.Target)))
	}
	pid := fmt.Sprint(c.PID)
	items = append(items, item("Copy PID ("+pid+")", copyText(pid, pid)),
		menuSection,
		item("End "+c.Process+"…", func() { n.end(c) }))
	return items
}

// end asks the process to quit (SIGTERM), after a confirmation.
func (n *networkTab) end(c netconn.Conn) {
	w := n.w
	_, port := uicore.NetSplitAddress(c.Local)
	msg := fmt.Sprintf("Process %d will be asked to quit", c.PID)
	if c.State == "LISTEN" {
		msg += ", freeing port " + port
	}
	what := c.Protocol + " " + c.Local
	if c.Remote != "" {
		what += " → " + c.Remote
	}
	st, svc := n.st, w.app.be.NetConn
	w.app.confirm(&w.win.Window, "End "+c.Process+"?", msg+".\n\n"+what, "End Process", func() {
		go func() {
			err := svc.Kill(c.PID)
			onMainAsync(func() {
				if err != nil {
					w.toast("Couldn't end " + c.Process + ": " + err.Error())
					return
				}
				w.toast("Asked " + c.Process + " to quit")
				coreglib.TimeoutAdd(500, func() bool {
					st.refresh()
					return false
				})
			})
		}()
	})
}

//go:build linux

package gtkui

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/dayanraj/wtf/internal/infra"
	"github.com/dayanraj/wtf/internal/uicore"
)

// Window is one top-level window: its tabs and panes (uicore.WindowModel),
// the tab strip, a page per tab holding its split panes, and a terminal
// per pane, kept while its tab is in the background. Like the macOS app's
// WindowController.
type Window struct {
	app    *App
	name   string // the backend's name for it, as in the saved session
	win    *adw.ApplicationWindow
	model  *uicore.WindowModel
	strip  *tabStrip
	stack  *gtk.Stack
	toasts *adw.ToastOverlay
	// Over the panes: the palette.
	overlay   *gtk.Overlay
	palette   *palette
	broadcast *broadcast
	shortcuts *gtk.ShortcutsWindow
	// Show All Tabs, while shown.
	overviewShown *overview
	// The workspace dialog last shown (for checks).
	workspaceDialog *adw.MessageDialog
	// Commands run, pins, recordings, files saved: in memory for this window.
	records  *windowRecords
	recClock *poller
	// The side pane, while open, its state (saved in the session) and the
	// split that holds it.
	sidebar       *sidebar
	sidebarState  uicore.SidebarState
	split         *gtk.Paned
	applyingSplit bool
	address       *addressBar
	// Each pane's resolved context, and the newest request for it.
	infra       map[int]*infra.Context
	infraSeq    int
	infraLatest map[int]int
	// A terminal for every pane, a request editor for every API pane, and
	// a page for every tab.
	panes map[int]*Pane
	apis  map[int]*apiPane
	pages map[int]*tabPage
	// Panes whose shells went to another window: not hung up when they go.
	detached map[int]bool

	saveTimer  coreglib.SourceHandle
	lastSaved  string
	extra      map[string]json.RawMessage
	closing    bool
	lastFront  [2]int
	syncQueued bool
	unobserve  func()
}

func newWindow(a *App, name string) *Window {
	w := &Window{app: a, name: name, model: uicore.NewWindowModel(a.home), sidebarState: uicore.NewSidebarState(),
		panes: map[int]*Pane{}, apis: map[int]*apiPane{}, pages: map[int]*tabPage{}, detached: map[int]bool{},
		infra: map[int]*infra.Context{}, infraLatest: map[int]int{}, records: newWindowRecords()}
	w.win = adw.NewApplicationWindow(&a.gtk.Application)
	w.win.SetTitle("wtf")
	w.win.SetDefaultSize(1000, 640)

	// The bar is wtf's own: the tabs take it from the left, then the menu
	// and the window's buttons; dragging an empty part moves the window.
	w.strip = newTabStrip(w)
	bar := gtk.NewBox(gtk.OrientationHorizontal, 4)
	bar.AddCSSClass("wtf-bar")
	bar.Append(w.strip.root)
	bar.Append(w.mainMenuButton())
	bar.Append(gtk.NewWindowControls(gtk.PackEnd))
	header := gtk.NewWindowHandle()
	header.SetChild(bar)
	w.stack = gtk.NewStack()
	w.stack.SetHExpand(true)
	w.stack.SetVExpand(true)
	w.overlay = gtk.NewOverlay()
	w.overlay.SetChild(w.stack)
	w.split = w.newSidebarSplit(w.overlay)
	w.toasts = adw.NewToastOverlay()
	w.toasts.SetChild(w.split)
	w.broadcast = newBroadcast(w)
	w.address = newAddressBar(w)
	view := adw.NewToolbarView()
	view.AddTopBar(header)
	view.AddTopBar(w.address.root)
	view.AddTopBar(w.broadcast.bar)
	view.SetContent(w.toasts)
	w.win.SetContent(view)

	w.installCommands()
	w.model.OnChange = w.queueSync
	// The strip follows the theme and Hide Secrets; the veils the dimming.
	badges := a.settings.config().ContextBadges
	w.unobserve = a.settings.observe(func() {
		w.strip.shape = ""
		// Badges turned on or off.
		if on := a.settings.config().ContextBadges; on != badges {
			badges = on
			w.refreshContexts()
		}
		// An AI provider added or removed shows or hides Investigate.
		if w.sidebar != nil {
			w.sidebar.update()
		}
		w.queueSync()
	})
	w.model.OnEmpty = w.closeForGood

	// Polling pauses while no wtf window is in front.
	w.win.NotifyProperty("is-active", func() {
		a.be.Active.SetFocused(w.name, w.win.IsActive())
		if w.win.IsActive() {
			w.focusActive()
		}
	})
	w.win.ConnectCloseRequest(func() bool {
		if w.closing || a.quitting {
			return false
		}
		ids := make([]int, 0, len(w.model.Panes))
		for id := range w.model.Panes {
			ids = append(ids, id)
		}
		w.confirmIfBusy(ids, "Close this window?", "Close Window", false, w.closeForGood)
		return true
	})
	return w
}

// bootstrap opens what the window starts with: a saved session, a tab from
// another window, or one fresh tab.
func (w *Window) bootstrap(start windowStart) {
	restored := false
	switch {
	case start.tab != nil:
		w.adopt(start.tab)
	case start.plan != nil && w.model.OpenPlan(*start.plan) != 0:
		// A workspace opened in this new window.
	case start.session != nil && w.model.Restore(*start.session):
		w.extra = start.session.Extra
		w.sidebarState = uicore.ParseSidebarState(w.extra["sidebar"])
		restored = true
	default:
		w.model.Open(uicore.OpenOptions{Cwd: new(string)})
	}
	w.sync()
	w.layoutSidebar()
	// A restored window is in the file already; a new one is saved soon.
	if restored {
		w.lastSaved = w.sessionJSON()
	}
}

// Model → views

// queueSync brings the views in line with the model once per pass of the
// main loop, however many changes there were.
func (w *Window) queueSync() {
	if w.syncQueued || w.closing {
		return
	}
	w.syncQueued = true
	coreglib.IdleAdd(func() {
		w.syncQueued = false
		w.sync()
	})
}

// sync brings the views in line with the model: terminals for new panes,
// closed panes hung up, pages for the tabs, the active one shown and
// focused.
func (w *Window) sync() {
	if w.closing {
		return
	}
	m := w.model
	for id, p := range w.panes {
		if _, ok := m.Panes[id]; !ok {
			delete(w.panes, id)
			moved := w.detached[id]
			if moved {
				delete(w.detached, id)
			} else {
				p.close()
				w.app.claudeClosed(p.session)
			}
			w.recordsPaneGone(id, moved)
			detachWidget(&p.root.Widget)
		}
	}
	for id, a := range w.apis {
		if _, ok := m.Panes[id]; !ok {
			delete(w.apis, id)
			a.close()
			detachWidget(&a.root.Widget)
		}
	}
	for id, ps := range m.Panes {
		switch {
		case w.panes[id] != nil || w.apis[id] != nil:
		case ps.Kind == uicore.PaneAPI:
			w.apis[id] = newAPIPane(w, id, *ps.API)
		default:
			w.startTerminal(ps)
		}
	}
	tabs := m.Tabs()
	live := map[int]bool{}
	for _, t := range tabs {
		live[t.ID] = true
		w.page(t).update(t)
	}
	for id, pg := range w.pages {
		if !live[id] {
			pg.destroy()
			delete(w.pages, id)
		}
	}
	if t, ok := m.Active(); ok {
		w.stack.SetVisibleChild(w.pages[t.ID].root)
		front := [2]int{t.ID, t.FocusedPaneID}
		if front != w.lastFront {
			w.lastFront = front
			w.focusActive()
		}
	}
	w.pruneBroadcast()
	for id := range w.infra {
		if w.model.Panes[id] == nil {
			delete(w.infra, id)
		}
	}
	w.address.refresh()
	w.strip.refresh()
	w.updateTitle()
	if sp := w.sidePane(); sp != nil {
		sp.paneChanged()
	}
	w.scheduleSave()
}

func (w *Window) page(t uicore.Tab) *tabPage {
	pg := w.pages[t.ID]
	if pg == nil {
		pg = newTabPage(w, t.ID)
		w.pages[t.ID] = pg
		w.stack.AddChild(pg.root)
	}
	return pg
}

// updateTitle names the window after its active tab.
func (w *Window) updateTitle() {
	title := "wtf"
	if t, ok := w.model.Active(); ok {
		title = w.app.settings.text(w.model.Title(t))
	}
	w.win.SetTitle(title)
}

// startTerminal makes a pane's terminal and starts its shell.
func (w *Window) startTerminal(ps *uicore.PaneState) {
	p := newPane(w, ps.ID)
	w.panes[ps.ID] = p
	// The self-checks that feed synthetic output start no shell.
	if d := w.app.debug; d == "marks" || d == "view" {
		return
	}
	command := ps.InitialCommand
	ps.InitialCommand = ""
	if err := p.start(ps.InitialCwd, w.identityCommand(ps.GitIdentity), command); err != nil {
		w.toast("Couldn't start a shell: " + err.Error())
		return
	}
	ps.SessionID = p.session
	w.claudeStarted(ps)
}

// runInPane runs a command in the focused shell when it's idle at a
// prompt (Ctrl-U clears anything half-typed), else in a new tab in its
// folder. For the side pane's exec, describe and SSH actions.
func (w *Window) runInPane(command string) {
	ps := w.model.ActivePane()
	p := w.active()
	if ps != nil && p != nil && ps.IsShell && !ps.Running {
		p.write(uicore.ClearLine + command + "\r")
		p.term.GrabFocus()
		return
	}
	var cwd *string
	if ps != nil {
		cwd = &ps.Cwd
	}
	w.model.Open(uicore.OpenOptions{Cwd: cwd, Command: command})
}

// identityCommand switches a new shell to a saved git identity; empty
// when it no longer exists.
func (w *Window) identityCommand(name string) string {
	if name == "" {
		return ""
	}
	for _, i := range w.app.be.SSHHosts.GitIdentities() {
		if i.Name == name {
			return uicore.UseIdentity(i)
		}
	}
	w.toast(fmt.Sprintf("Git identity “%s” no longer exists: this shell uses your own git and ssh config", name))
	return ""
}

// activeAPI is the active tab's focused pane when it's an API pane.
func (w *Window) activeAPI() *apiPane {
	if ps := w.model.ActivePane(); ps != nil {
		return w.apis[ps.ID]
	}
	return nil
}

// runFromAPI is Run in Shell from an API pane: typed at an idle shell in
// the same tab, else in a new tab.
func (w *Window) runFromAPI(apiPane int, command string) {
	if t, ok := w.model.TabOf(apiPane); ok {
		for _, id := range t.Root.Leaves() {
			ps, p := w.model.Panes[id], w.panes[id]
			if ps != nil && p != nil && ps.IsShell && !ps.Running {
				w.model.FocusPane(id)
				p.write(uicore.ClearLine + command + "\r")
				p.term.GrabFocus()
				return
			}
		}
	}
	var cwd *string
	if ps := w.model.Panes[apiPane]; ps != nil {
		cwd = &ps.Cwd
	}
	w.model.Open(uicore.OpenOptions{Cwd: cwd, Command: command})
}

// active is the active tab's focused pane.
func (w *Window) active() *Pane {
	if ps := w.model.ActivePane(); ps != nil {
		return w.panes[ps.ID]
	}
	return nil
}

// focusActive gives the keyboard to the active pane, unless a text field
// in the window (renaming, find) has it.
func (w *Window) focusActive() {
	if a := w.activeAPI(); a != nil {
		a.focus()
		return
	}
	p := w.active()
	if p == nil {
		return
	}
	if f := w.win.Focus(); f != nil {
		if _, typing := f.(*gtk.Text); typing {
			return
		}
	}
	p.term.GrabFocus()
}

// paneShowing says the pane is on screen: in the active tab, and not
// hidden behind another zoomed pane.
func (w *Window) paneShowing(p *Pane) bool {
	t, ok := w.model.TabOf(p.id)
	return ok && t.ID == w.model.ActiveID() && (t.ZoomedPaneID == 0 || t.ZoomedPaneID == p.id)
}

// What panes report

func (w *Window) paneFocused(p *Pane) { w.model.FocusPane(p.id) }

func (w *Window) paneOutput(p *Pane) {
	if !w.paneShowing(p) {
		w.model.MarkActivity(p.id)
	}
}

func (w *Window) paneTitle(p *Pane, title string) {
	if ps := w.model.Panes[p.id]; ps != nil && ps.OscTitle != title {
		ps.OscTitle = title
		w.strip.refresh()
		w.updateTitle()
	}
}

func (w *Window) panePrompt(p *Pane) {
	if ps := w.model.Panes[p.id]; ps != nil {
		ps.Integrated = true
	}
}

func (w *Window) commandStarted(p *Pane, text string, block int) {
	if ps := w.model.Panes[p.id]; ps != nil {
		w.records.history.Started(p.id, block, text, ps.Cwd, time.Now())
		w.records.runningBlock[p.id] = block
		w.records.changed()
		ps.Running, ps.Command = true, text
		w.strip.refresh()
		w.address.refresh()
	}
}

func (w *Window) commandEnded(p *Pane, code int, took time.Duration) {
	ps := w.model.Panes[p.id]
	if ps == nil {
		return
	}
	ps.LastCommand = &uicore.LastCommand{Text: ps.Command, ExitCode: code, Duration: took}
	w.records.history.Finished(p.id, code, took, time.Now())
	// A pin following this command gets its final output.
	if b, ok := w.records.runningBlock[p.id]; ok {
		delete(w.records.runningBlock, p.id)
		out, _ := p.tracker.Output(b)
		w.records.pins.Update(p.id, b, out, &uicore.PinDone{ExitCode: code, Duration: took})
	}
	w.records.changed()
	if uicore.IsScanCommand(ps.Command) {
		w.scanFinished()
	}
	ps.Running, ps.Command = false, ""
	w.model.MarkFinished(p.id, code == 0)
	w.strip.refresh()
	w.address.refresh()
	if sp := w.sidePane(); sp != nil && w.active() == p {
		sp.commandFinished()
	}
}

func (w *Window) paneEnv(p *Pane, text string) {
	if ps := w.model.Panes[p.id]; ps != nil {
		ps.Env = uicore.ParseEnv(text)
		// The folder, at once: the process poll pauses while no window is
		// in front.
		if pwd := ps.Env["PWD"]; pwd != "" && pwd != ps.Cwd {
			ps.Cwd = pwd
			w.strip.refresh()
			w.address.refresh()
		}
		w.resolveContext(p.id)
		w.scheduleSave()
	}
}

// owns says one of this window's panes runs a backend session.
func (w *Window) owns(session string) bool { return w.model.PaneBySession(session) != nil }

// Commands

// windowCommands are the window's commands by id (uicore.Commands).
func (w *Window) windowCommands() map[string]func() {
	m, s := w.model, w.app.settings
	onPane := func(f func(*Pane)) func() {
		return func() {
			if p := w.active(); p != nil {
				f(p)
			}
		}
	}
	cmds := map[string]func(){
		"term.copy":       onPane((*Pane).copySelection),
		"term.paste":      onPane((*Pane).paste),
		"term.selectAll":  onPane(func(p *Pane) { p.term.SelectAll() }),
		"term.copyOutput": onPane((*Pane).copyLastOutput),
		"term.clear":      onPane((*Pane).clearBuffer),
		"find": func() {
			if a := w.activeAPI(); a != nil {
				a.showFind()
			} else if p := w.active(); p != nil {
				p.showFind()
			}
		},
		"find.next": func() {
			if a := w.activeAPI(); a != nil {
				a.find(1)
			} else if p := w.active(); p != nil {
				p.findNext()
			}
		},
		"find.prev": func() {
			if a := w.activeAPI(); a != nil {
				a.find(-1)
			} else if p := w.active(); p != nil {
				p.findPrevious()
			}
		},
		"prompt.prev": onPane(func(p *Pane) { p.jumpPrompt(-1) }),
		"prompt.next": onPane(func(p *Pane) { p.jumpPrompt(1) }),
		"secrets.toggle": func() {
			s.setHide(!s.hide)
			if s.hide {
				w.toast("Secrets hidden — on screen and when copied")
			} else {
				w.toast("Secrets shown")
			}
		},
		"font.bigger":  func() { s.setFontSize(s.fontSize + 1) },
		"font.smaller": func() { s.setFontSize(s.fontSize - 1) },
		"font.reset":   func() { s.setFontSize(s.config().FontSize) },

		"tab.new":             func() { m.Open(uicore.OpenOptions{}) },
		"tab.newApi":          func() { m.OpenAPI(uicore.APIBlank(), nil, 0) },
		"tab.duplicate":       func() { m.Duplicate() },
		"tab.close":           func() { w.closeTabs([]int{m.ActiveID()}) },
		"tab.reopen":          m.Reopen,
		"tab.next":            func() { m.Cycle(1) },
		"tab.prev":            func() { m.Cycle(-1) },
		"tab.moveRight":       func() { m.MoveActive(1) },
		"tab.moveLeft":        func() { m.MoveActive(-1) },
		"tab.group":           func() { w.newGroup(m.ActiveID()) },
		"tab.select.last":     m.SelectLast,
		"tab.toNewWindow":     func() { w.moveToNewWindow(m.ActiveID()) },
		"pane.splitRight":     func() { m.Split(uicore.SplitRow, nil, "") },
		"pane.splitDown":      func() { m.Split(uicore.SplitColumn, nil, "") },
		"pane.close":          w.closeActivePane,
		"pane.zoom":           m.ToggleZoom,
		"pane.next":           func() { m.CyclePane(1) },
		"pane.prev":           func() { m.CyclePane(-1) },
		"pane.left":           func() { m.FocusDirection(uicore.FocusLeft) },
		"pane.right":          func() { m.FocusDirection(uicore.FocusRight) },
		"pane.up":             func() { m.FocusDirection(uicore.FocusUp) },
		"pane.down":           func() { m.FocusDirection(uicore.FocusDown) },
		"window.close":        func() { w.win.Close() },
		"palette":             func() { w.togglePalette(paletteCommands) },
		"broadcast.toggle":    w.toggleBroadcast,
		"help.shortcuts":      w.showShortcuts,
		"term.pinOutput":      w.pinLastOutput,
		"ai.explain":          w.explainLastFailure,
		"ai.ask":              w.claudeCommand,
		"ai.investigate":      w.claudeInvestigate,
		"claude.ask":          w.askClaude,
		"review.last":         w.reviewLastCommand,
		"findings.scan":       w.runSecurityScan,
		"usage.show":          w.showUsage,
		"snippets.run":        func() { w.togglePalette(paletteSnippets) },
		"snippets.open":       w.openSnippetsFile,
		"app.about":           w.showAbout,
		"tools.show":          func() { w.showTools("") },
		"help.manual":         w.showHelp,
		"tab.rename":          func() { w.strip.rename(m.ActiveID()) },
		"inspect.tls":         func() { w.showCertHost("") },
		"generate.key":        func() { w.showGenerator("key") },
		"generate.password":   func() { w.showGenerator("password") },
		"sidebar.autoRefresh": func() { toggleAutoRefresh(w) },
		"paste.confirm": func() {
			on := !w.app.settings.config().ConfirmPaste
			w.setting(func() error { return w.app.be.Config.SetConfirmPaste(on) },
				map[bool]string{true: "Multi-line pastes are shown first", false: "Pastes go straight in"}[on])
		},
		"usage.toggle": func() {
			on := !w.app.settings.config().ShowUsage
			w.setting(func() error { return w.app.be.Config.SetShowUsage(on) },
				map[bool]string{true: "wtf’s CPU and memory show in the address bar", false: "Resource usage hidden"}[on])
		},
		"config.open": w.showSettings,
		"config.file": w.openConfigFile,
		"themes.folder": func() {
			cfg := w.app.be.Config
			go func() {
				if err := cfg.OpenThemesFolder(); err != nil {
					onMainAsync(func() { w.toast("Couldn't open the themes folder: " + err.Error()) })
				}
			}()
		},
		"workspace.save":  w.saveWindowAsWorkspace,
		"record.toggle":   func() { w.toggleRecording(nil) },
		"export.text":     func() { w.saveOutputAsText(nil) },
		"export.markdown": func() { w.saveTranscript(nil) },
		"tab.overview":    w.toggleOverview,
		"workspace.open":  func() { w.togglePalette(paletteWorkspaces) },
		"sidebar.toggle":  w.toggleSidebar,
		"theme.pick":      func() { w.togglePalette(paletteThemes) },
	}
	for i := 1; i <= 8; i++ {
		cmds[fmt.Sprintf("tab.select.%d", i)] = func() { m.SelectNumber(i) }
	}
	for _, t := range sidebarTabs {
		tab := t.tab
		cmds[t.cmd] = func() { w.showSidebar(tab) }
	}
	return cmds
}

// setting changes a setting off the main loop, then says so.
func (w *Window) setting(change func() error, done string) {
	go func() {
		err := change()
		onMainAsync(func() {
			if err != nil {
				w.toast("Couldn't change the setting: " + err.Error())
			} else {
				w.toast(done)
			}
		})
	}()
}

// appCommandIDs are the commands the app runs ("app." actions).
var appCommandIDs = []string{"window.new", "app.quit"}

// installCommands adds the window's commands as "win." actions, and a
// shortcut for each of them and of the app's commands. The shortcuts run in
// the capture phase, before VTE turns the keys into input.
func (w *Window) installCommands() {
	keys := gtk.NewShortcutController()
	keys.SetPropagationPhase(gtk.PhaseCapture)
	keys.SetScope(gtk.ShortcutScopeLocal)
	shortcut := func(id, action string) {
		c, ok := uicore.CommandByID(id)
		if !ok || c.Keys == "" {
			return
		}
		keys.AddShortcut(gtk.NewShortcut(gtk.NewShortcutTriggerParseString(uicore.Accel(c.Keys)), gtk.NewNamedAction(action)))
	}
	for id, f := range w.windowCommands() {
		act := gio.NewSimpleAction(id, nil)
		act.ConnectActivate(func(*glib.Variant) { f() })
		w.win.AddAction(act)
		shortcut(id, "win."+id)
	}
	for _, id := range appCommandIDs {
		shortcut(id, "app."+id)
	}
	// Ctrl+Tab too, as in a browser (and the macOS app).
	keys.AddShortcut(gtk.NewShortcut(gtk.NewShortcutTriggerParseString("<Control>Tab"), gtk.NewNamedAction("win.tab.next")))
	keys.AddShortcut(gtk.NewShortcut(gtk.NewShortcutTriggerParseString("<Control><Shift>ISO_Left_Tab"), gtk.NewNamedAction("win.tab.prev")))
	w.win.AddController(keys)
}

// action is the detailed action that runs a command here; "" when none
// does yet.
func (w *Window) action(id string) string {
	for _, a := range appCommandIDs {
		if a == id {
			return "app." + id
		}
	}
	if w.win.LookupAction(id) != nil {
		return "win." + id
	}
	return ""
}

// mainMenuButton is the header bar's menu: the commands that work here, in
// the sections of the macOS menus.
func (w *Window) mainMenuButton() *gtk.MenuButton {
	sections := [][]string{
		{"tab.new", "tab.newApi", "window.new", "pane.splitRight", "pane.splitDown", "tab.duplicate", "tab.toNewWindow"},
		{"tab.reopen", "pane.close", "tab.close", "window.close"},
		{"palette", "config.open", "config.file", "theme.pick", "themes.folder", "snippets.run", "workspace.open", "workspace.save", "findings.scan", "usage.show"},
		{"sidebar.toggle", "sidebar.files", "sidebar.ssh", "sidebar.aws", "sidebar.kube", "sidebar.snippets", "sidebar.workspaces",
			"sidebar.network", "sidebar.processes", "sidebar.history", "sidebar.pins", "sidebar.findings", "sidebar.share", "sidebar.generate"},
		{"find", "term.copyOutput", "term.pinOutput", "claude.ask", "ai.explain", "ai.ask", "ai.investigate", "review.last", "term.clear", "secrets.toggle"},
		{"record.toggle", "export.text", "export.markdown"},
		{"font.bigger", "font.smaller", "font.reset"},
		{"tab.overview", "tab.group", "broadcast.toggle", "pane.zoom", "pane.next", "pane.prev"},
		{"help.manual", "help.shortcuts", "tools.show", "app.about"},
		{"app.quit"},
	}
	cmds := w.windowCommands()
	root := gio.NewMenu()
	for _, ids := range sections {
		sec := gio.NewMenu()
		for _, id := range ids {
			c, _ := uicore.CommandByID(id)
			switch _, win := cmds[id]; {
			case win:
				sec.Append(c.Title, "win."+id)
			case id == "window.new" || id == "app.quit":
				sec.Append(c.Title, "app."+id)
			}
		}
		root.AppendSection("", sec)
	}
	b := gtk.NewMenuButton()
	b.SetIconName("open-menu-symbolic")
	b.SetTooltipText("Menu")
	b.SetMenuModel(root)
	return b
}

// toast shows a short message at the bottom of the window.
func (w *Window) toast(msg string) {
	t := adw.NewToast(msg)
	t.SetTimeout(3)
	w.toasts.AddToast(t)
	w.app.debugLog("toast: %s", msg)
}

// Saving

func (w *Window) sessionJSON() string {
	extra := map[string]json.RawMessage{}
	for k, v := range w.extra {
		extra[k] = v
	}
	extra["sidebar"] = w.sidebarState.JSON()
	b, err := json.Marshal(w.model.Snapshot(w.app.settings.fontSize, extra))
	if err != nil {
		return ""
	}
	return string(b)
}

// scheduleSave saves 400ms after the last change, and only what changed.
func (w *Window) scheduleSave() {
	if w.closing {
		return
	}
	if w.saveTimer != 0 {
		glib.SourceRemove(w.saveTimer)
	}
	w.saveTimer = coreglib.TimeoutAdd(400, func() bool {
		w.saveTimer = 0
		w.saveNow(false)
		return false
	})
}

// saveNow writes the session if it changed; it waits for it when quitting.
func (w *Window) saveNow(wait bool) {
	if w.closing || len(w.model.Tabs()) == 0 {
		return
	}
	data := w.sessionJSON()
	if data == "" || data == w.lastSaved {
		return
	}
	w.lastSaved = data
	save := func() {
		if err := w.app.be.State.SaveWindow(w.name, data); err != nil {
			log.Printf("saving %s: %v", w.name, err)
		}
	}
	if wait {
		save()
	} else {
		go save()
	}
}

// Closing

// confirmIfBusy asks first when programs would be ended; runs action once
// it's fine.
func (w *Window) confirmIfBusy(paneIDs []int, title, button string, quitting bool, action func()) {
	var busy []*uicore.PaneState
	for _, id := range paneIDs {
		if ps := w.model.Panes[id]; ps != nil && ps.Busy() {
			busy = append(busy, ps)
		}
	}
	if len(busy) == 0 {
		action()
		return
	}
	verb := "Closing"
	if quitting {
		verb = "Quitting"
	}
	body := fmt.Sprintf("A program is still running. %s will end it.", verb)
	if n := len(busy); n > 1 {
		body = fmt.Sprintf("%d programs are still running. %s will end them.", n, verb)
	}
	var lines []string
	for i, ps := range busy {
		if i == 6 {
			lines = append(lines, fmt.Sprintf("and %d more", len(busy)-6))
			break
		}
		what := ps.Process
		if ps.Running && ps.Command != "" {
			what = w.app.settings.text(ps.Command)
		}
		lines = append(lines, what+" — "+uicore.PrettyPath(ps.Cwd, w.app.home))
	}
	w.app.confirm(&w.win.Window, title, body+"\n\n"+strings.Join(lines, "\n"), button, action)
}

func (w *Window) closeTabs(ids []int) {
	var panes []int
	for _, id := range ids {
		if t, ok := w.model.Tab(id); ok {
			panes = append(panes, t.Root.Leaves()...)
		}
	}
	title, button := "Close this tab?", "Close Tab"
	if len(ids) > 1 {
		title, button = fmt.Sprintf("Close %d tabs?", len(ids)), "Close Tabs"
	}
	w.confirmIfBusy(panes, title, button, false, func() { w.model.CloseMany(ids) })
}

func (w *Window) closeActivePane() {
	ps := w.model.ActivePane()
	if ps == nil {
		return
	}
	t, _ := w.model.TabOf(ps.ID)
	title, button := "Close this pane?", "Close Pane"
	if len(t.Root.Leaves()) == 1 {
		title, button = "Close this tab?", "Close Tab"
	}
	w.confirmIfBusy([]int{ps.ID}, title, button, false, func() { w.model.ClosePane(ps.ID) })
}

// closeForGood closes the window and forgets its session.
func (w *Window) closeForGood() {
	if w.closing {
		return
	}
	w.closing = true
	if w.saveTimer != 0 {
		glib.SourceRemove(w.saveTimer)
		w.saveTimer = 0
	}
	for id, p := range w.panes {
		if !w.detached[id] {
			p.close()
		}
	}
	for _, a := range w.apis {
		a.close()
	}
	if w.sidebar != nil {
		w.sidebar.drop()
	}
	if w.recClock != nil {
		w.recClock.stop()
	}
	w.address.usage.stop()
	w.unobserve()
	name, store := w.name, w.app.be.Store
	go func() { _ = store.Forget(name) }()
	w.app.windowClosed(w)
	w.win.Destroy()
}

// quitting: the app quits; the session is saved now and stays for the
// next launch.
func (w *Window) quitting() {
	if w.saveTimer != 0 {
		glib.SourceRemove(w.saveTimer)
		w.saveTimer = 0
	}
	w.saveNow(true)
	w.closing = true
}

// scanFinished tells the Findings tab, if it shows, that a scanner finished
// in some terminal.
func (w *Window) scanFinished() {
	if s, ok := w.sidePane().(interface{ scanFinished() }); ok {
		s.scanFinished()
	}
}

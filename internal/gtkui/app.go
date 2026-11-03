//go:build linux

// Package gtkui is the native Linux UI: GTK 4 and libadwaita windows on the
// Go backend, with VTE terminals that Go feeds (NATIVE_LINUX_ROADMAP.md).
//
// Environment:
//
//	WTF_NATIVE_DEBUG=…   self-checks, see selfcheck.go
//
// Everything here runs on the GTK main loop unless it says otherwise.
package gtkui

import (
	"context"
	"fmt"
	"log"
	"maps"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/dayanraj/wtf/internal/backend"
	"github.com/dayanraj/wtf/internal/bridge"
	"github.com/dayanraj/wtf/internal/infra"
	"github.com/dayanraj/wtf/internal/paths"
	"github.com/dayanraj/wtf/internal/state"
	"github.com/dayanraj/wtf/internal/terminal"
	"github.com/dayanraj/wtf/internal/uicore"
)

// The application ID, the same as the macOS app's bundle ID. Self-checks
// (and WTF_NATIVE_PROFILE=separate) use their own, so they never join a
// running wtf.
const appID = "com.dayanraj.wtf"

// separateProfile says whether this run keeps its own state, caches and app
// ID: debug self-checks do, so a test run never touches the real session.
func separateProfile() bool {
	return os.Getenv("WTF_NATIVE_DEBUG") != "" || os.Getenv("WTF_NATIVE_PROFILE") == "separate"
}

// Build says which build is running.
type Build struct{ Version, Commit, Date string }

// App is the running application: one per process. A second `wtf-gtk`
// asks this one for a new window and exits.
type App struct {
	gtk      *adw.Application
	build    Build
	be       *backend.Backend
	claude   *claudePanes
	cancel   context.CancelFunc
	events   *eventPump
	settings *settings
	debug    string

	windows []*Window
	// Window names in the session file, not to be reused for new windows.
	savedNames map[string]bool
	started    bool
	// Quitting was confirmed: windows close without asking and keep their
	// sessions.
	quitting bool
	home     string
}

// Main runs the app and returns the exit status.
func Main(build Build) int {
	log.SetFlags(log.Ltime | log.Lmicroseconds)
	a := &App{build: build, debug: os.Getenv("WTF_NATIVE_DEBUG"), savedNames: map[string]bool{}, claude: newClaudePanes()}
	a.home, _ = os.UserHomeDir()
	flags := gio.ApplicationFlagsNone
	if a.debug != "" {
		// Self-checks run beside a real instance without joining it.
		flags = gio.ApplicationNonUnique
	}
	id := appID
	if separateProfile() {
		id += ".Native"
	}
	setProgramName(id)
	addBundledFonts()
	a.gtk = adw.NewApplication(id, flags)
	// Startup runs only in the first instance; later ones just activate it.
	a.gtk.ConnectStartup(a.startup)
	a.gtk.ConnectActivate(a.activate)
	a.gtk.ConnectShutdown(a.shutdown)

	// A logout or `kill` quits like the Quit command does.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		<-sig
		coreglib.IdleAdd(a.quitNow)
	}()
	return a.gtk.Run(os.Args[:1])
}

func (a *App) startup() {
	state, cache, err := profileDirs()
	if err == nil {
		if cache != "" {
			paths.SetCacheRoot(cache)
		}
		a.be, err = backend.New(a.build.Version, a.build.Commit, a.build.Date, state)
	}
	if err == nil {
		a.events = newEventPump()
		var ctx context.Context
		ctx, a.cancel = context.WithCancel(context.Background())
		err = a.be.Start(ctx)
	}
	if err != nil {
		a.fail("Couldn't start wtf", err)
		return
	}
	gtk.WindowSetDefaultIconName(appID)
	info := a.be.About.Info()
	log.Printf("wtf %s (%s), state %s, cache %s", a.build.Version, a.build.Commit, info.StateDir, info.CacheDir)
	a.settings = newSettings(a)
	a.installActions()
	// Backend events for a shell go to the window it's in.
	a.events.on(terminal.EventInfo, func(data any) {
		if info, ok := data.(terminal.Info); ok {
			if w := a.windowOf(info.ID); w != nil {
				w.model.ApplyInfo(info.ID, info.Process, info.Cwd, info.IsShell)
			}
		}
	})
	a.events.on(bridge.EventRequest, func(data any) {
		if req, ok := data.(bridge.UIRequest); ok {
			a.bridgeRequest(req)
		}
	})
	a.events.on(bridge.EventStatus, func(data any) {
		if st, ok := data.(bridge.Status); ok {
			a.claudeStatus(st)
		}
	})
	a.events.on(infra.EventChanged, func(any) {
		for _, w := range a.windows {
			w.refreshContexts()
		}
	})
	a.events.on(terminal.EventExit, func(data any) {
		if e, ok := data.(terminal.ExitEvent); ok {
			if w := a.windowOf(e.ID); w != nil {
				w.model.SessionExited(e.ID)
			}
		}
	})
}

func (a *App) shutdown() {
	if a.cancel != nil {
		a.cancel()
	}
	if a.be != nil {
		a.be.Shutdown()
	}
}

// fail shows an error and quits; nothing can work without the backend.
func (a *App) fail(what string, err error) {
	log.Printf("%s: %v", what, err)
	d := adw.NewMessageDialog(nil, what, err.Error())
	d.AddResponse("quit", "Quit")
	d.ConnectResponse(func(string) { a.gtk.Quit() })
	a.gtk.Hold() // keep running until the dialog is answered
	d.SetApplication(&a.gtk.Application)
	d.Present()
}

// profileDirs says where state and caches live: empty for wtf's own
// folders (~/.config/wtf, ~/.cache/wtf), shared with earlier versions, so
// windows, settings and port forwards carry over. A separate profile keeps
// them in ~/.config/wtf-native and ~/.cache/wtf-native.
func profileDirs() (state, cache string, err error) {
	if !separateProfile() {
		return "", "", nil
	}
	config, err := os.UserConfigDir()
	if err != nil {
		return "", "", err
	}
	caches, err := os.UserCacheDir()
	if err != nil {
		return "", "", err
	}
	state, cache = filepath.Join(config, "wtf-native"), filepath.Join(caches, "wtf-native")
	for _, d := range []string{state, cache} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return "", "", fmt.Errorf("creating %s: %w", d, err)
		}
	}
	return state, cache, nil
}

// activate opens the saved windows at the first launch, and a fresh window
// when wtf-gtk is started again.
func (a *App) activate() {
	if a.be == nil {
		return
	}
	if a.started {
		a.openWindowWith(windowStart{})
		return
	}
	a.started = true
	var saved []state.Window
	if a.debug == "" || a.debug == "restore" {
		saved = a.be.Store.Windows()
	}
	var sessions []*uicore.SavedSession
	for _, sw := range saved {
		a.savedNames[sw.Name] = true
		s, err := uicore.ParseSavedSession(sw.Session)
		if err != nil {
			sessions = append(sessions, nil)
			continue
		}
		sessions = append(sessions, &s)
	}
	if len(sessions) > 0 && sessions[0] != nil && sessions[0].FontSize != nil {
		a.settings.setFontSize(*sessions[0].FontSize)
	}
	if len(saved) == 0 {
		a.openWindow("main", windowStart{})
	}
	for i, sw := range saved {
		a.openWindow(sw.Name, windowStart{session: sessions[i]})
	}
	// Once ever: which tools the side pane needs aren't installed.
	if w := a.front(); w != nil {
		w.toolsFirstRun()
	}
}

// windowStart is what a window opens with: a saved session, a tab from
// another window, a workspace, or (when empty) one fresh tab.
type windowStart struct {
	session *uicore.SavedSession
	tab     *movingTab
	plan    *uicore.Plan
}

// openWindowWith opens a new window, named the next free w<i>.
func (a *App) openWindowWith(start windowStart) *Window { return a.openWindow(a.newName(), start) }

// openWindow opens a window by name.
func (a *App) openWindow(name string, start windowStart) *Window {
	w := newWindow(a, name)
	a.windows = append(a.windows, w)
	w.bootstrap(start)
	w.win.Present()
	if len(a.windows) == 1 && a.debug != "" {
		// Checks may close every window; the app quits when they're done.
		a.gtk.Hold()
		go a.selfCheck(w)
	}
	return w
}

// newName is the lowest w<i> (i ≥ 2) not open and not in the session file.
func (a *App) newName() string {
	taken := maps.Clone(a.savedNames)
	for _, w := range a.windows {
		taken[w.name] = true
	}
	i := 2
	for taken[fmt.Sprintf("w%d", i)] {
		i++
	}
	return fmt.Sprintf("w%d", i)
}

// window finds an open window by name.
func (a *App) window(name string) *Window {
	for _, w := range a.windows {
		if w.name == name {
			return w
		}
	}
	return nil
}

// windowOf finds the window a backend session is in.
func (a *App) windowOf(session string) *Window {
	for _, w := range a.windows {
		if w.owns(session) {
			return w
		}
	}
	return nil
}

func (a *App) windowClosed(w *Window) {
	for i, x := range a.windows {
		if x == w {
			a.windows = append(a.windows[:i], a.windows[i+1:]...)
			break
		}
	}
	delete(a.savedNames, w.name)
	a.be.Active.Forget(w.name)
	a.be.Terminal.CloseWindow(w.name)
}

// quit asks first when programs are running in any window.
func (a *App) quit() {
	go func() {
		running := a.be.Terminal.RunningProcesses()
		coreglib.IdleAdd(func() {
			if len(running) == 0 {
				a.quitNow()
				return
			}
			body := "A program is still running. Quitting will end it."
			if n := len(running); n > 1 {
				body = fmt.Sprintf("%d programs are still running. Quitting will end them.", n)
			}
			var lines []string
			for i, r := range running {
				if i == 6 {
					lines = append(lines, fmt.Sprintf("and %d more", len(running)-6))
					break
				}
				lines = append(lines, r.Process+" — "+uicore.PrettyPath(r.Cwd, a.home))
			}
			var parent *gtk.Window
			if w := a.front(); w != nil {
				parent = &w.win.Window
			}
			a.confirm(parent, "Quit wtf?", body+"\n\n"+strings.Join(lines, "\n"), "Quit", a.quitNow)
		})
	}()
}

// quitNow saves every window's session, which stays for the next launch,
// and quits.
func (a *App) quitNow() {
	a.quitting = true
	for _, w := range a.windows {
		w.quitting()
	}
	a.gtk.Quit()
}

// front is the window in front, or the first one.
func (a *App) front() *Window {
	for _, w := range a.windows {
		if w.win.IsActive() {
			return w
		}
	}
	if len(a.windows) > 0 {
		return a.windows[0]
	}
	return nil
}

// confirm asks before something that can't be undone; the button is
// shown as destructive.
func (a *App) confirm(parent *gtk.Window, title, body, button string, action func()) {
	d := adw.NewMessageDialog(parent, title, body)
	d.AddResponse("cancel", "Cancel")
	d.AddResponse("ok", button)
	d.SetResponseAppearance("ok", adw.ResponseDestructive)
	d.SetDefaultResponse("cancel")
	d.SetCloseResponse("cancel")
	d.ConnectResponse(func(r string) {
		if r == "ok" {
			action()
		}
	})
	a.debugLog("confirm: %s", title)
	d.Present()
}

// ask asks for a line of text.
func (a *App) ask(parent *gtk.Window, title, body, initial, button string, done func(string)) {
	d := adw.NewMessageDialog(parent, title, body)
	entry := gtk.NewEntry()
	entry.SetText(initial)
	entry.SetActivatesDefault(true)
	d.SetExtraChild(entry)
	d.AddResponse("cancel", "Cancel")
	d.AddResponse("ok", button)
	d.SetResponseAppearance("ok", adw.ResponseSuggested)
	d.SetDefaultResponse("ok")
	d.SetCloseResponse("cancel")
	d.ConnectResponse(func(r string) {
		if r == "ok" {
			done(entry.Text())
		}
	})
	d.Present()
	entry.GrabFocus()
}

// installActions adds the app-wide commands as "app." actions. Windows add
// their own, and the shortcuts for both (window.go).
func (a *App) installActions() {
	add := func(name string, run func()) {
		act := gio.NewSimpleAction(name, nil)
		act.ConnectActivate(func(*glib.Variant) { run() })
		a.gtk.AddAction(act)
	}
	add("window.new", func() { a.openWindowWith(windowStart{}) })
	add("app.quit", a.quit)
}

// onMain runs f on the GTK main loop and waits for it (at once when
// already there).
func onMain(f func()) {
	if glib.MainContextDefault().IsOwner() {
		f()
		return
	}
	done := make(chan struct{})
	coreglib.IdleAdd(func() {
		f()
		close(done)
	})
	<-done
}

// onMainAsync runs f on the GTK main loop, later.
func onMainAsync(f func()) { coreglib.IdleAdd(f) }

// toast shows a message in the window in front.
func (a *App) toast(msg string) {
	if w := a.front(); w != nil {
		w.toast(msg)
	}
}

// debugLog logs during self-checks.
func (a *App) debugLog(format string, args ...any) {
	if a.debug != "" {
		log.Printf(format, args...)
	}
}

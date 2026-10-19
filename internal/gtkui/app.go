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
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"

	"github.com/dayanraj/wtf/internal/backend"
	"github.com/dayanraj/wtf/internal/paths"
)

// The application ID. Until the native app replaces the Wails one (M7) it
// has an ID of its own, so the two can run side by side.
const appID = "com.dayanraj.wtf.Native"

// Build says which build is running.
type Build struct{ Version, Commit, Date string }

// App is the running application: one per process. A second `wtf-gtk`
// asks this one for a new window and exits.
type App struct {
	gtk      *adw.Application
	build    Build
	be       *backend.Backend
	cancel   context.CancelFunc
	events   *eventPump
	settings *settings
	debug    string

	windows []*Window
	nextWin int
}

// Main runs the app and returns the exit status.
func Main(build Build) int {
	log.SetFlags(log.Ltime | log.Lmicroseconds)
	a := &App{build: build, debug: os.Getenv("WTF_NATIVE_DEBUG")}
	flags := gio.ApplicationFlagsNone
	if a.debug != "" {
		// Self-checks run beside a real instance without joining it.
		flags = gio.ApplicationNonUnique
	}
	a.gtk = adw.NewApplication(appID, flags)
	// Startup runs only in the first instance; later ones just activate it.
	a.gtk.ConnectStartup(a.startup)
	a.gtk.ConnectActivate(func() { a.newWindow() })
	a.gtk.ConnectShutdown(a.shutdown)

	// A logout or `kill` quits like the Quit command does.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		<-sig
		coreglib.IdleAdd(a.gtk.Quit)
	}()
	return a.gtk.Run(os.Args[:1])
}

func (a *App) startup() {
	state, cache, err := profileDirs()
	if err == nil {
		paths.SetCacheRoot(cache)
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
	log.Printf("wtf %s (%s), state %s, cache %s", a.build.Version, a.build.Commit, state, cache)
	a.settings = newSettings(a)
	a.installActions()
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

// profileDirs says where state and caches live. Until the native app
// replaces the Wails one (M7) it keeps its own (~/.config/wtf-native,
// ~/.cache/wtf-native), so a running Wails wtf keeps its kube overlays,
// tunnels and windows. Settings (config.toml) and snippets are shared.
func profileDirs() (state, cache string, err error) {
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

// newWindow opens a window with one shell.
func (a *App) newWindow() *Window {
	if a.be == nil {
		return nil
	}
	a.nextWin++
	w := newWindow(a, fmt.Sprintf("w%d", a.nextWin))
	a.windows = append(a.windows, w)
	w.win.Present()
	if len(a.windows) == 1 && a.debug != "" {
		// Checks may close every window; the app quits when they're done.
		a.gtk.Hold()
		go a.selfCheck(w)
	}
	return w
}

func (a *App) windowClosed(w *Window) {
	for i, x := range a.windows {
		if x == w {
			a.windows = append(a.windows[:i], a.windows[i+1:]...)
			break
		}
	}
	a.be.Active.Forget(w.name)
	a.be.Terminal.CloseWindow(w.name)
}

// installActions adds the app-wide commands as "app." actions. Windows add
// their own, and the shortcuts for both (window.go).
func (a *App) installActions() {
	add := func(name string, run func()) {
		act := gio.NewSimpleAction(name, nil)
		act.ConnectActivate(func(*glib.Variant) { run() })
		a.gtk.AddAction(act)
	}
	add("window.new", func() { a.newWindow() })
	add("app.quit", a.gtk.Quit)
}

// onMain runs f on the GTK main loop and waits for it. Not for use on the
// main loop itself.
func onMain(f func()) {
	done := make(chan struct{})
	coreglib.IdleAdd(func() {
		f()
		close(done)
	})
	<-done
}

// toast shows a message in the window in front.
func (a *App) toast(msg string) {
	for _, w := range a.windows {
		if w.win.IsActive() {
			w.toast(msg)
			return
		}
	}
	if len(a.windows) > 0 {
		a.windows[0].toast(msg)
	}
}

// debugLog logs during self-checks.
func (a *App) debugLog(format string, args ...any) {
	if a.debug != "" {
		log.Printf(format, args...)
	}
}

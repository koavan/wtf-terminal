//go:build linux

// Command wtf-gtk is the native Linux UI (GTK 4 + VTE). For now it's the M0
// spike from NATIVE_LINUX_ROADMAP.md: one window, one shell.
//
// Environment:
//
//	WTF_GTK_PTY=vte      VTE reads the PTY itself (comparison mode)
//	WTF_NATIVE_DEBUG=…   self-checks, see selfcheck.go
package main

import (
	"context"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/creack/pty"
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/dayanraj/wtf/internal/gtkui/vte"
	"github.com/dayanraj/wtf/internal/terminal"
)

const version = "1.0.2"

type app struct {
	gtk   *gtk.Application
	win   *gtk.ApplicationWindow
	term  *vte.Terminal
	marks *marks
	feed  *feeder
	svc   *terminal.Service
	sess  *terminal.Session
	tap   func([]byte) // self-checks watch the output
}

func main() {
	log.SetFlags(log.Ltime | log.Lmicroseconds)
	a := &app{gtk: gtk.NewApplication("com.dayanraj.wtf.gtk", gio.ApplicationNonUnique)}
	a.gtk.ConnectActivate(a.activate)
	a.gtk.ConnectShutdown(func() {
		if a.svc != nil {
			_ = a.svc.ServiceShutdown()
		}
	})
	// A logout or `kill` quits like closing the window does.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		<-sig
		coreglib.IdleAdd(a.gtk.Quit)
	}()
	os.Exit(a.gtk.Run(os.Args[:1]))
}

func (a *app) activate() {
	a.win = gtk.NewApplicationWindow(a.gtk)
	a.win.SetTitle("wtf")
	a.win.SetDefaultSize(900, 600)

	a.term = vte.New()
	a.term.SetFont("JetBrains Mono 11")
	a.term.SetColors("#d8dee9", "#1e2127")
	a.term.SetScrollback(10000)
	a.term.SetHExpand(true)
	a.term.SetVExpand(true)
	a.term.OnTitle(func(title string) {
		if title == "" {
			title = "wtf"
		}
		a.win.SetTitle(title)
	})
	a.win.SetChild(a.term)

	debug := os.Getenv("WTF_NATIVE_DEBUG")
	switch {
	case debug == "marks":
		a.marks = newMarks(a.term)
		a.feed = newFeeder(a.term, a.marks)
		a.term.OnGridChanged(a.marks.gridChanged)
		go a.checkMarks()
	case os.Getenv("WTF_GTK_PTY") == "vte":
		a.startVTEShell()
	default:
		a.startShell()
		if debug == "signals" {
			go a.checkSignals()
		}
	}
	a.win.Present()
	a.term.GrabFocus()
}

// startShell runs a shell from internal/terminal; Go reads the PTY and
// feeds VTE, as the real app will.
func (a *app) startShell() {
	a.svc = terminal.NewService(version, nil, nil)
	if err := a.svc.ServiceStartup(context.Background()); err != nil {
		log.Fatal(err)
	}
	id, err := a.svc.Create("w1", 80, 24, "")
	if err != nil {
		log.Fatal(err)
	}
	sess, _, err := terminal.Attach(a.svc, id)
	if err != nil {
		log.Fatal(err)
	}
	a.sess = sess
	a.marks = newMarks(a.term)
	a.feed = newFeeder(a.term, a.marks)
	a.term.OnCommit(func(text string) { _, _ = sess.Write([]byte(text)) })
	a.term.OnGridChanged(func(cols, rows int) {
		a.marks.gridChanged(cols, rows)
		_ = a.svc.Resize(id, cols, rows)
	})
	go func() {
		for chunk := range sess.Output {
			if a.tap != nil {
				a.tap(chunk)
			}
			a.feed.push(chunk)
		}
		<-sess.Done
		coreglib.IdleAdd(a.gtk.Quit)
	}()
}

// startVTEShell lets VTE read and resize the PTY (the M0 comparison).
func (a *app) startVTEShell() {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}
	cmd := exec.Command(shell, "-l")
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 80, Rows: 24})
	if err != nil {
		log.Fatal(err)
	}
	// VTE takes ownership of the fd it's given.
	fd, err := syscall.Dup(int(f.Fd()))
	if err != nil {
		log.Fatal(err)
	}
	_ = f.Close()
	if !a.term.UsePTY(fd) {
		log.Fatal("VTE refused the PTY")
	}
	a.term.OnEOF(a.gtk.Quit)
	go func() {
		_ = cmd.Wait()
		coreglib.IdleAdd(a.gtk.Quit)
	}()
}

// onMain runs f on the GTK main loop and waits for it.
func onMain(f func()) {
	done := make(chan struct{})
	coreglib.IdleAdd(func() {
		f()
		close(done)
	})
	<-done
}

//go:build linux

package gtkui

import (
	"fmt"
	"strings"
	"time"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/dayanraj/wtf/internal/gtkui/vte"
	"github.com/dayanraj/wtf/internal/terminal"
	"github.com/dayanraj/wtf/internal/uicore"
)

// Pane is one terminal: a VTE widget showing a backend shell session, with
// its command blocks (OSC 133), secret covers, find bar and menus drawn
// over it. Go reads the PTY and feeds VTE (feeder.go); keys come back
// through VTE's commit signal. Like the macOS app's TerminalController.
type Pane struct {
	// The window it's in (changes when its tab moves) and its id in that
	// window's model.
	win     *Window
	id      int
	root    *gtk.Overlay // the terminal, the drawing over it, the find bar
	term    *vte.Terminal
	lines   *uicore.Lines
	tracker *uicore.PromptTracker
	feed    *feeder
	over    *overlay
	menu    *menuPopover
	find    *findBar
	session string // the backend session, once started
	sess    *terminal.Session
	// Input for the shell, written in order off the main loop: a shell
	// that isn't reading would block the window.
	input chan []byte
	// Self-checks watch the output; set before start.
	tap func([]byte)

	unobserve func()
	// The scrollback was cleared: drop marks once VTE has parsed it.
	clearing bool
	// The paste confirmation showing, if any.
	pasteDialog *adw.MessageDialog
	// Log colors: shown, and the pending scan.
	tinted    bool
	tintTimer coreglib.SourceHandle
	// The veil over an unfocused split pane, and the outline of one that
	// receives broadcast input.
	dim, outline *gtk.Box
	// The bar offering to resume the Claude Code that ran here when wtf
	// quit (#38), while it shows.
	resumeBar *gtk.Box
}

// Links that Ctrl-click opens: URLs, as VTE matches them (PCRE2).
const urlPattern = `\b(?:https?|ftp|file)://[^\s<>"'` + "`" + `]*[^\s<>"'` + "`" + `.,;:!?)\]}]`

func newPane(w *Window, id int) *Pane {
	p := &Pane{win: w, id: id, term: vte.New()}
	p.term.SetHExpand(true)
	p.term.SetVExpand(true)
	p.term.Quiet()
	p.term.AddURLMatch(urlPattern)
	p.term.OnTitle(func(title string) { p.win.paneTitle(p, title) })

	p.lines = uicore.NewLines(p.term)
	p.term.OnBeforeAllocate(p.lines.BeforeAllocate)
	p.tracker = uicore.NewPromptTracker(p.term, p.lines)
	p.tracker.OnChange = p.refresh
	p.tracker.OnPrompt = func() { p.win.panePrompt(p) }
	p.tracker.OnCommandStart = func(text string, id int) { p.win.commandStarted(p, text, id) }
	p.tracker.OnCommandEnd = func(id, code int, took time.Duration) {
		p.commandEnded(id, code, took)
		p.win.commandEnded(p, code, took)
	}
	p.tracker.OnEnv = func(text string) { p.win.paneEnv(p, text) }

	p.feed = newFeeder(p.term)
	p.feed.onMark = p.tracker.OSC133
	p.feed.on633 = p.tracker.OSC633
	p.feed.onView = func() {
		if p.clearing {
			p.clearing = false
			p.tracker.Cleared()
		}
		if p.find != nil {
			p.find.outputArrived()
		}
		p.refresh()
		p.scheduleTints()
		p.win.paneOutput(p)
	}
	p.term.OnGridChanged(func(cols, rows int) {
		p.lines.GridChanged(cols)
		p.feed.requestView()
		if p.session != "" {
			_ = p.win.app.be.Terminal.Resize(p.session, cols, rows)
		}
	})
	p.term.OnCommit(func(text string) {
		if !p.feed.takeReport(text) {
			p.write(text)
			p.win.fanOut(p, text)
		}
	})
	p.term.OnScrolled(func() {
		p.refresh()
		p.scheduleTints()
	})
	// Shown again (its tab, or the window): color what's there.
	p.term.ConnectMap(p.scheduleTints)
	p.term.OnSelectionChanged(p.selectionChanged)

	p.root = gtk.NewOverlay()
	p.root.SetChild(p.term)
	p.over = newOverlay(p)
	p.root.AddOverlay(p.over.area)
	p.dim = gtk.NewBox(gtk.OrientationVertical, 0)
	p.dim.AddCSSClass("wtf-dim")
	p.dim.SetCanTarget(false)
	p.dim.SetVisible(false)
	p.root.AddOverlay(p.dim)
	p.outline = gtk.NewBox(gtk.OrientationVertical, 0)
	p.outline.AddCSSClass("wtf-broadcasting")
	p.outline.SetCanTarget(false)
	p.outline.SetVisible(false)
	p.root.AddOverlay(p.outline)
	p.menu = newMenuPopover(p.term)
	p.installMouse()
	focus := gtk.NewEventControllerFocus()
	focus.ConnectEnter(func() { p.win.paneFocused(p) })
	p.term.AddController(focus)

	p.unobserve = w.app.settings.observe(p.applySettings)
	p.applySettings()
	return p
}

// start runs a shell from internal/terminal in this pane, in a folder (""
// for home), then types the commands given, if any.
func (p *Pane) start(cwd string, commands ...string) error {
	svc := p.win.app.be.Terminal
	id, err := svc.Create(p.win.name, max(2, p.term.Columns()), max(2, p.term.Rows()), cwd)
	if err != nil {
		return err
	}
	sess, _, err := terminal.Attach(svc, id)
	if err != nil {
		svc.Close(id)
		return err
	}
	p.session, p.sess = id, sess
	p.input = make(chan []byte, 256)
	go func() {
		for b := range p.input {
			_, _ = sess.Write(b)
		}
	}()
	// The grid may have changed size before the shell started.
	_ = svc.Resize(id, p.term.Columns(), p.term.Rows())
	// The tty holds them until the shell reads them.
	for _, c := range commands {
		if c != "" {
			p.write(c + "\r")
		}
	}
	go func() {
		for chunk := range sess.Output {
			if p.tap != nil {
				p.tap(chunk)
			}
			p.feed.push(chunk)
		}
		<-sess.Done
		// terminal:exit closes the pane.
		coreglib.IdleAdd(func() { close(p.input); p.input = nil })
	}()
	return nil
}

// write sends input to the shell.
func (p *Pane) write(text string) {
	if p.input != nil && text != "" {
		p.input <- []byte(text)
	}
}

// close hangs up the shell and stops following the settings.
func (p *Pane) close() {
	if p.session != "" {
		p.win.app.be.Terminal.Close(p.session)
	}
	if p.unobserve != nil {
		p.unobserve()
		p.unobserve = nil
	}
	if p.tintTimer != 0 {
		coreglib.SourceRemove(p.tintTimer)
		p.tintTimer = 0
	}
}

// applySettings sets the look from config.toml and the theme.
func (p *Pane) applySettings() {
	s := p.win.app.settings
	cfg, th := s.config(), s.theme()
	family := cfg.FontFamily
	if family == "" {
		family = "JetBrains Mono"
	}
	p.term.SetFont(fmt.Sprintf("%s, JetBrains Mono, monospace %g", family, s.fontSize))
	p.term.SetLineHeight(max(1, cfg.LineHeight))
	p.term.SetCursor(cfg.CursorStyle, cfg.CursorBlink)
	p.term.SetScrollback(max(100, cfg.Scrollback))
	p.term.SetColors(vte.Colors{
		Foreground: th.Foreground, Background: th.Background, Palette: th.Palette[:],
		Cursor: th.Cursor, CursorText: th.CursorText,
		SelectionBackground: th.SelectionBackground, SelectionForeground: th.SelectionForeground,
	})
	p.over.setTheme(th)
	p.refresh()
	p.scheduleTints()
}

// commandEnded notifies about a slow command while its window isn't in
// front.
func (p *Pane) commandEnded(id, code int, took time.Duration) {
	after := p.win.app.settings.config().NotifyAfter
	if after <= 0 || took < time.Duration(after*float64(time.Second)) || p.win.win.IsActive() && p.win.paneShowing(p) {
		return
	}
	dur := uicore.Duration(took)
	title := "✓ Finished in " + dur
	if code != 0 {
		title = fmt.Sprintf("✗ Failed (exit %d) after %s", code, dur)
	}
	b, _ := p.tracker.Info(id)
	body := p.win.app.settings.text(b.Text)
	sid, svc, notify := p.session, p.win.app.be.Terminal, p.win.app.be.Notify
	go func() {
		cwd := ""
		if info, err := svc.Info(sid); err == nil {
			cwd = info.Cwd
		}
		_ = notify.Notify(title, cwd, body)
	}()
}

// Where the screen is

// top is the absolute row at the top of the view; ok is false until the
// feeder has found the screen.
func (p *Pane) top() (int, bool) {
	if !p.feed.viewKnown {
		return 0, false
	}
	_, upper, value, _ := p.term.View()
	return p.firstRow(upper) + int(value), true
}

// firstRow is the oldest row still in the scrollback: the adjustment counts
// from it, and its upper bound is where the screen ends.
func (p *Pane) firstRow(upper float64) int {
	return p.feed.screenTop + p.term.Rows() - int(upper)
}

// scrollToRow shows an absolute row at the top of the view.
func (p *Pane) scrollToRow(row int) {
	_, upper, _, _ := p.term.View()
	p.term.ScrollTo(float64(row - p.firstRow(upper)))
}

func (p *Pane) scrollToBottom() {
	_, upper, _, page := p.term.View()
	p.term.ScrollTo(upper - page)
}

// refresh redraws the overlay for what's on screen, once per pass of the
// main loop however many frames or scrolls asked.
func (p *Pane) refresh() { p.over.schedule() }

// Actions

// copySelection copies the selection, with secrets replaced while hidden.
func (p *Pane) copySelection() {
	if !p.term.HasSelection() {
		return
	}
	if p.win.app.settings.hide {
		p.term.Clipboard().SetText(p.win.app.settings.text(p.term.Selection()))
		return
	}
	p.term.Copy()
}

func (p *Pane) selectionChanged() {
	if p.win.app.settings.config().CopyOnSelect && p.term.HasSelection() {
		if sel := p.term.Selection(); sel != "" {
			p.term.Clipboard().SetText(p.win.app.settings.text(sel))
		}
	}
}

// copyText copies text wtf put together (a command, its output), with
// secrets replaced while hidden, and says what was copied.
func (p *Pane) copyText(text, what string) {
	text = p.win.app.settings.text(text)
	p.term.Clipboard().SetText(text)
	if lines := strings.Count(text, "\n") + 1; text != "" && lines > 1 {
		p.win.toast(fmt.Sprintf("Copied %s (%d lines)", what, lines))
	} else {
		p.win.toast("Copied " + what)
	}
}

func (p *Pane) copyLastOutput() {
	id, ok := p.tracker.LastFinished()
	if !ok {
		p.win.toast("No finished command yet")
		return
	}
	out, _ := p.tracker.Output(id)
	p.term.Clipboard().SetText(p.win.app.settings.text(out))
	switch lines := strings.Count(out, "\n") + 1; {
	case out == "":
		p.win.toast("Command had no output")
	case lines == 1:
		p.win.toast("Copied 1 line of output")
	default:
		p.win.toast(fmt.Sprintf("Copied %d lines of output", lines))
	}
}

// runAgain types a command at the prompt: Ctrl-U clears anything
// half-typed first.
func (p *Pane) runAgain(text string) {
	if p.session != "" {
		if info, err := p.win.app.be.Terminal.Info(p.session); err == nil && !info.IsShell {
			p.win.toast("The shell is busy — finish the running program first")
			return
		}
	}
	p.write("\x15" + text + "\r")
	p.term.GrabFocus()
}

// jumpPrompt scrolls the previous (-1) or next (1) prompt to the top.
func (p *Pane) jumpPrompt(direction int) {
	top, ok := p.top()
	if !ok || p.feed.alternate {
		return
	}
	if row, ok := p.tracker.Jump(direction, top); ok {
		p.scrollToRow(row)
	} else if direction > 0 {
		p.scrollToBottom()
	}
}

// clearBuffer clears the scrollback and screen, keeping the line the
// cursor is on (the prompt) as the new first line, like xterm.js's clear().
func (p *Pane) clearBuffer() {
	if p.feed.alternate || !p.feed.viewKnown {
		return
	}
	_, cursor := p.term.Cursor()
	seq := "\x1b[3J"
	if y := cursor - p.feed.screenTop; y > 0 {
		seq = fmt.Sprintf("\x1b[%dS\x1b[%dA\x1b[3J", y, y)
	}
	p.term.Feed([]byte(seq))
	p.clearing = true
	p.feed.requestView()
}

// toggleSecrets turns Hide Secrets on or off, for every terminal.
func (p *Pane) toggleSecrets() {
	s := p.win.app.settings
	s.setHide(!s.hide)
	if s.hide {
		p.win.toast("Secrets hidden — on screen and when copied")
	} else {
		p.win.toast("Secrets shown")
	}
}

// setDimmed veils the pane while another pane of its split tab has the
// focus (unfocused-split-dim).
func (p *Pane) setDimmed(on bool, amount float64) {
	on = on && amount > 0
	p.dim.SetVisible(on)
	if on {
		p.dim.SetOpacity(min(0.9, amount))
	}
}

// plainText is the whole scrollback as text: soft-wrapped rows joined,
// trailing blank lines dropped.
func (p *Pane) plainText() string {
	if !p.feed.viewKnown {
		return ""
	}
	_, upper, _, _ := p.term.View()
	first := p.firstRow(upper)
	last := p.feed.screenTop + p.term.Rows() - 1
	return strings.TrimRight(uicore.TextOf(p.term, first, last), "\n \t")
}

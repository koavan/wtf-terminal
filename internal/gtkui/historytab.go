//go:build linux

package gtkui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/dayanraj/wtf/internal/uicore"
)

func init() {
	registerSidebarTab(sidebarTabInfo{tab: uicore.SidebarHistory, label: "Command History", icon: "document-open-recent-symbolic",
		cmd: "sidebar.history", make: func(w *Window) sidePaneTab { return newHistoryTab(w) }})
}

// clockTime is when something happened: the time today, or the day too.
func clockTime(t time.Time) string {
	now := time.Now()
	if t.YearDay() == now.YearDay() && t.Year() == now.Year() {
		return t.Format("15:04:05")
	}
	return t.Format("Jan 2 15:04:05")
}

// historyTab is the History tab: the commands this pane (or every pane in
// the window) ran, newest first, with how they went. Click to put one at
// the prompt, double-click to run it again. Like HistoryView.svelte and the
// macOS app's HistoryView.
type historyTab struct {
	w      *Window
	root   *gtk.Box
	scope  [2]*gtk.ToggleButton
	trash  *gtk.Button
	filter *gtk.SearchEntry
	list   *sideList
	shown  []uicore.HistoryEntry
	off    func()
}

func newHistoryTab(w *Window) *historyTab {
	h := &historyTab{w: w}
	bar := gtk.NewBox(gtk.OrientationHorizontal, 4)
	bar.AddCSSClass("wtf-pane-header")
	scope := gtk.NewBox(gtk.OrientationHorizontal, 0)
	scope.AddCSSClass("linked")
	scope.SetHExpand(true)
	scope.SetHomogeneous(true)
	for i, label := range []string{"This pane", "All panes"} {
		b := gtk.NewToggleButtonWithLabel(label)
		b.SetFocusOnClick(false)
		if i > 0 {
			b.SetGroup(h.scope[0])
		}
		all := i == 1
		b.ConnectToggled(func() {
			if b.Active() && h.all() != all {
				w.sidebarState.HistoryScope = map[bool]string{true: "all", false: "pane"}[all]
				w.scheduleSave()
				h.rebuild()
			}
		})
		h.scope[i] = b
		scope.Append(b)
	}
	h.scope[map[bool]int{true: 1, false: 0}[h.all()]].SetActive(true)
	bar.Append(scope)
	h.trash = gtk.NewButtonFromIconName("user-trash-symbolic")
	h.trash.AddCSSClass("flat")
	h.trash.AddCSSClass("wtf-tool")
	h.trash.SetTooltipText("Clear history")
	h.trash.SetFocusOnClick(false)
	h.trash.ConnectClicked(h.clear)
	bar.Append(h.trash)

	h.filter = gtk.NewSearchEntry()
	h.filter.SetPlaceholderText("Filter commands")
	h.filter.AddCSSClass("wtf-side-filter")
	h.filter.ConnectSearchChanged(h.rebuild)
	h.filter.ConnectStopSearch(func() {
		h.filter.SetText("")
		w.focusActive()
	})
	h.list = newSideList(false)
	h.list.onClick = func(id string) {
		if e, ok := h.entry(id); ok {
			h.insert(e)
		}
	}
	h.list.onActivate = func(id string) {
		if e, ok := h.entry(id); ok {
			h.runAgain(e)
		}
	}
	h.list.onMenu = func(ids []string, x, y float64) {
		if e, ok := h.entry(ids[0]); ok {
			h.list.menu.show(x, y, h.menu(e), nil)
		}
	}
	h.root = gtk.NewBox(gtk.OrientationVertical, 4)
	h.root.Append(bar)
	h.root.Append(h.filter)
	h.root.Append(h.list.root)
	h.off = w.records.observe(h.rebuild)
	h.rebuild()
	return h
}

func (h *historyTab) widget() gtk.Widgetter { return h.root }
func (h *historyTab) paneChanged()          { h.rebuild() }
func (h *historyTab) commandFinished()      {}
func (h *historyTab) close()                { h.off() }

func (h *historyTab) all() bool { return h.w.sidebarState.HistoryScope == "all" }

func historyKey(e uicore.HistoryEntry) string {
	return fmt.Sprintf("%d:%d:%d", e.PaneID, e.ID, e.StartedAt.UnixNano())
}

func (h *historyTab) entry(id string) (uicore.HistoryEntry, bool) {
	for _, e := range h.shown {
		if historyKey(e) == id {
			return e, true
		}
	}
	return uicore.HistoryEntry{}, false
}

func (h *historyTab) source() []uicore.HistoryEntry {
	r := h.w.records
	if h.all() {
		ids := make([]int, 0, len(h.w.model.Panes))
		for id := range h.w.model.Panes {
			ids = append(ids, id)
		}
		return r.history.Merged(ids)
	}
	if ps := h.w.model.ActivePane(); ps != nil {
		return r.history.Of(ps.ID)
	}
	return nil
}

func (h *historyTab) rebuild() {
	src := h.source()
	q := h.filter.Text()
	h.shown = h.shown[:0]
	for _, e := range src {
		if uicore.HistoryMatches(e, q) {
			h.shown = append(h.shown, e)
		}
	}
	slices.Reverse(h.shown)
	h.trash.SetSensitive(len(src) > 0 || q != "")
	section := sideSection{}
	for _, e := range h.shown {
		section.rows = append(section.rows, h.row(e))
	}
	if len(h.shown) == 0 {
		ps := h.w.model.ActivePane()
		switch {
		case strings.TrimSpace(q) != "":
			section.empty = "No commands match “" + q + "”."
		case !h.all() && ps != nil && !ps.Integrated:
			section.empty = "No history for this shell. History comes from shell integration, which supports bash and zsh. " +
				"Check that shell-integration is on in your settings; it applies to new panes."
		default:
			section.empty = "No commands yet. Commands you run here appear with when they finished and how long they took."
		}
	}
	h.list.set([]sideSection{section})
}

func (h *historyTab) row(e uicore.HistoryEntry) sideRow {
	s := h.w.app.settings
	cmd := s.text(e.Command)
	cwd := uicore.PrettyPath(e.Cwd, h.w.app.home)
	r := sideRow{id: historyKey(e), title: cmd, mono: true,
		tip: fmt.Sprintf("%s\nin %s\nstarted %s", cmd, cwd, clockTime(e.StartedAt))}
	switch {
	case e.Running():
		r.dot = "tone-running"
		r.detail = fmt.Sprintf("running · since %s · %s", clockTime(e.StartedAt), cwd)
	default:
		r.detail = fmt.Sprintf("%s · %s · %s", clockTime(e.FinishedAt), uicore.Duration(e.Duration), cwd)
		if e.ExitCode != nil && *e.ExitCode == 0 {
			r.dot = "dot-git-added"
		} else {
			r.dot = "dot-git-deleted"
		}
	}
	if e.ExitCode != nil && *e.ExitCode != 0 {
		r.pills = append(r.pills, sidePill{text: fmt.Sprintf("exit %d", *e.ExitCode), class: "wtf-pill-fail"})
	}
	if h.all() {
		if t, ok := h.w.model.TabOf(e.PaneID); ok {
			title := h.w.model.Title(t)
			short := title
			if len([]rune(title)) > 16 {
				short = string([]rune(title)[:15]) + "…"
			}
			r.pills = append(r.pills, sidePill{text: short, tip: "In “" + title + "”"})
		}
	}
	return r
}

func (h *historyTab) insert(e uicore.HistoryEntry) {
	if p := h.w.active(); p != nil {
		p.term.PasteText(e.Command)
		p.term.GrabFocus()
	}
}

func (h *historyTab) runAgain(e uicore.HistoryEntry) {
	ps, p := h.w.model.ActivePane(), h.w.active()
	if ps == nil || p == nil || !ps.IsShell || ps.Running {
		h.w.toast("The shell is busy — finish the running program first")
		return
	}
	p.write(uicore.ClearLine + e.Command + "\r")
	p.term.GrabFocus()
}

func (h *historyTab) menu(e uicore.HistoryEntry) []*menuItem {
	w := h.w
	items := []*menuItem{
		item("Run Again", func() { h.runAgain(e) }),
		item("Insert at Prompt", func() { h.insert(e) }),
		item("Show in Terminal", func() {
			if w.model.Panes[e.PaneID] == nil {
				return
			}
			w.showPane(e.PaneID)
			if p := w.panes[e.PaneID]; p == nil || !p.revealCommand(e.ID) {
				w.toast("That command has scrolled out of the terminal")
			}
		}),
		menuSection,
		item("Copy Command", func() {
			w.win.Clipboard().SetText(w.app.settings.text(e.Command))
			w.toast("Copied command")
		}),
		item("Copy Folder", func() {
			w.win.Clipboard().SetText(e.Cwd)
			w.toast("Copied folder")
		}),
	}
	return append(items, menuSection, item("Save as Snippet…", func() { w.saveAsSnippet(e.Command) }))
}

// clear empties the list, and the shells' own history for this session
// where the shell is idle at its prompt (a key binding: nothing typed
// shows).
func (h *historyTab) clear() {
	w := h.w
	var targets []*uicore.PaneState
	if h.all() {
		for _, ps := range w.model.Panes {
			targets = append(targets, ps)
		}
	} else if ps := w.model.ActivePane(); ps != nil {
		targets = append(targets, ps)
	}
	if len(targets) == 0 {
		return
	}
	idle := func(ps *uicore.PaneState) bool { return ps.Integrated && ps.IsShell && !ps.Running }
	count := 0
	var busy []string
	for _, ps := range targets {
		count += len(w.records.history.Of(ps.ID))
		if ps.Integrated && !idle(ps) {
			busy = append(busy, w.label(ps.ID)+": a program is running — only the list will be cleared")
		}
	}
	title := "Clear command history?"
	if len(targets) > 1 {
		title = "Clear command history for all panes?"
	}
	body := fmt.Sprintf("Removes %d %s from this list and from the shell's own history for this session (↑ and Ctrl-R). "+
		"History from earlier sessions stays, and so do commands your shell already wrote to its history file.", count, plural(count, "command"))
	if len(busy) > 0 {
		body += "\n\n" + strings.Join(busy, "\n")
	}
	w.app.confirm(&w.win.Window, title, body, "Clear History", func() {
		shells := 0
		for _, ps := range targets {
			w.records.history.Clear(ps.ID)
			if p := w.panes[ps.ID]; p != nil && idle(ps) {
				p.write("\x18\x1d")
				shells++
			}
		}
		w.records.changed()
		switch n := len(busy); {
		case n > 0:
			w.toast(fmt.Sprintf("Cleared — %d busy %s own history left as is", n, map[bool]string{true: "shell's", false: "shells'"}[n == 1]))
		case shells > 1:
			w.toast(fmt.Sprintf("Cleared this session's history in %d shells", shells))
		case shells == 1:
			w.toast("Cleared this session's history")
		default:
			w.toast("Cleared the list")
		}
	})
}

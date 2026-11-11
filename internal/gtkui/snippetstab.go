//go:build linux

package gtkui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/dayanraj/wtf/internal/snippets"
	"github.com/dayanraj/wtf/internal/uicore"
)

func init() {
	registerSidebarTab(sidebarTabInfo{tab: uicore.SidebarSnippets, label: "Snippets", icon: "utilities-terminal-symbolic", cmd: "sidebar.snippets",
		make: func(w *Window) sidePaneTab { return newSnippetsTab(w) }})
	extraCSS = append(extraCSS, `
.wtf-snippets-examples { margin: 0 8px; padding: 0 6px; min-height: 22px; color: @accent_color; font-weight: 500; }
.wtf-snippet-sheet { padding: 4px 22px 16px; }
.wtf-snippet-review { font-family: monospace; font-size: 0.92em; background: alpha(currentColor, 0.05); border-radius: 6px; padding: 8px 10px; }
`)
}

// snippetStore is the snippets files' contents for the focused shell's
// folder: snippets and workspaces, read afresh when asked (they're
// hand-edited). One for the app, used on the main loop only. Like the
// macOS app's SnippetStore.
type snippetStore struct {
	list      snippets.List
	loaded    bool
	cwd       string
	seq       int
	observers map[int]func()
	next      int
}

var snippetsShared = &snippetStore{observers: map[int]func(){}}

// observe calls f whenever the list changes; off stops it.
func (s *snippetStore) observe(f func()) (off func()) {
	s.next++
	id := s.next
	s.observers[id] = f
	return func() { delete(s.observers, id) }
}

// load reads the files for a folder, off the main loop.
func (s *snippetStore) load(svc *snippets.Service, cwd string) {
	s.cwd = cwd
	s.seq++
	mine := s.seq
	go func() {
		l := svc.List(cwd)
		onMainAsync(func() {
			if mine == s.seq {
				s.set(l, cwd)
			}
		})
	}()
}

// set takes a list the backend returned (after an edit, or read elsewhere).
func (s *snippetStore) set(l snippets.List, cwd string) {
	s.seq++
	s.list, s.cwd, s.loaded = l, cwd, true
	for _, f := range s.observers {
		f()
	}
}

func (s *snippetStore) snippet(id string) (snippets.Snippet, bool) {
	for _, x := range s.list.Snippets {
		if x.ID == id {
			return x, true
		}
	}
	return snippets.Snippet{}, false
}

// targetKind is a file's kind: project, user or team ("" when unknown).
func (s *snippetStore) targetKind(path string) string {
	for _, t := range s.list.Targets {
		if t.Path == path {
			return t.Kind
		}
	}
	return ""
}

// defaultTarget is where something new is saved: the user's file, else
// the first there is.
func (s *snippetStore) defaultTarget() string {
	for _, t := range s.list.Targets {
		if t.Kind == "user" {
			return t.Path
		}
	}
	if len(s.list.Targets) > 0 {
		return s.list.Targets[0].Path
	}
	return ""
}

// snippetsSectionTitle is a file's heading in the Snippets and Workspaces
// tabs.
func snippetsSectionTitle(w *Window, path string) string {
	switch snippetsShared.targetKind(path) {
	case "project":
		return "This project · " + uicore.PrettyPath(path, w.app.home)
	case "user":
		return "Yours"
	}
	return uicore.PrettyPath(path, w.app.home)
}

// How a snippet's command goes to the shell.
type snippetHow int

const (
	snippetInsert snippetHow = iota // at the prompt, to check first
	snippetRun
	snippetNewTab
)

// activeCwd is the focused shell's folder.
func (w *Window) activeCwd() string {
	if ps := w.model.ActivePane(); ps != nil {
		return ps.Cwd
	}
	return ""
}

// snippetReady says a snippet can go into the pane (0: the focused one)
// now: an idle shell.
func (w *Window) snippetReady(pane int) bool {
	if pane == 0 {
		if ps := w.model.ActivePane(); ps != nil {
			pane = ps.ID
		}
	}
	ps := w.model.Panes[pane]
	return ps != nil && w.panes[pane] != nil && ps.IsShell && !ps.Running
}

// runSnippet puts a snippet's command at the shell's prompt (pane 0: the
// focused one), runs it there, or runs it in a new tab. False when the
// shell is busy.
func (w *Window) runSnippet(command string, how snippetHow, pane int) bool {
	if how == snippetNewTab {
		w.model.Open(uicore.OpenOptions{Command: command})
		return true
	}
	if !w.snippetReady(pane) {
		w.toast("The shell is busy — finish the running program first, or use Run in New Tab")
		return false
	}
	if pane == 0 {
		pane = w.model.ActivePane().ID
	}
	p := w.panes[pane]
	text := uicore.ClearLine + command
	if how == snippetRun {
		text += "\r"
	}
	p.write(text)
	p.term.GrabFocus()
	return true
}

// openSnippet: a snippet with nothing to fill in goes straight to the
// prompt; the rest open the run dialog.
func (w *Window) openSnippet(s snippets.Snippet) {
	if !uicore.SnippetIsRunbook(s) && len(uicore.SnippetFields(s)) == 0 {
		w.runSnippet(s.Command, snippetInsert, 0)
		return
	}
	w.showSnippetRun(s)
}

// openSnippetsFile (Open Snippets File) opens the user's snippets file in
// the text editor, made with examples the first time.
func (w *Window) openSnippetsFile() {
	w.openSnippetsFileThen("Couldn't open the snippets file", nil)
}

func (w *Window) openSnippetsFileThen(failed string, then func()) {
	svc := w.app.be.Snippets
	go func() {
		err := svc.OpenUserFile()
		onMainAsync(func() {
			if err != nil {
				w.toast(failed + ": " + err.Error())
			}
			if then != nil {
				then()
			}
		})
	}()
}

// snippetEntries are the snippets for the palette, read afresh (the
// Snippets tab follows); trailing adds Open Snippets File….
func (w *Window) snippetEntries(trailing bool) []paletteEntry {
	cwd := w.activeCwd()
	list := w.app.be.Snippets.List(cwd)
	snippetsShared.set(list, cwd)
	var out []paletteEntry
	for _, s := range list.Snippets {
		runbook := uicore.SnippetIsRunbook(s)
		detail := s.Description
		switch {
		case detail != "":
		case runbook:
			detail = fmt.Sprintf("%d steps", len(s.Steps))
		default:
			detail = s.Command
		}
		var hints []string
		if runbook {
			hints = append(hints, "runbook")
		}
		if s.Project {
			hints = append(hints, "project")
		}
		out = append(out, paletteEntry{section: "Snippets", label: s.Name, detail: detail,
			hint: strings.Join(hints, " · "), run: func() { w.openSnippet(s) }})
	}
	if trailing {
		out = append(out, paletteEntry{section: "Snippets", label: "Open Snippets File…",
			detail: "Add your own; examples included", run: w.openSnippetsFile})
	}
	return out
}

// snippetsTab is the Snippets tab: snippets and runbooks from the snippets
// files, grouped by file. Run (▶ or double-click), edit, duplicate,
// delete. The files are read when it opens, when the focused shell's
// folder changes (project files depend on it) and on Reload; never polled.
// Like the macOS app's SnippetsView.
type snippetsTab struct {
	w        *Window
	root     *gtk.Box
	filter   *gtk.SearchEntry
	note     *gtk.Box
	examples *gtk.Button
	list     *sideList
	lastCwd  string
	off      func()
}

func newSnippetsTab(w *Window) *snippetsTab {
	t := &snippetsTab{w: w}
	header := newPaneHeader("Snippets")
	header.tool("list-add-symbolic", "New snippet", func() { w.showSnippetEditor(nil, nil, "", false) })
	header.tool("document-open-symbolic", "Open your snippets file in a text editor", func() {
		w.openSnippetsFileThen("Couldn't open it", nil)
	})
	header.tool("view-refresh-symbolic", "Reload", t.reload)
	t.filter = gtk.NewSearchEntry()
	t.filter.SetPlaceholderText("Filter snippets")
	t.filter.AddCSSClass("wtf-side-filter")
	t.filter.ConnectSearchChanged(t.rebuild)
	t.filter.ConnectStopSearch(func() {
		t.filter.SetText("")
		w.focusActive()
	})
	t.note = gtk.NewBox(gtk.OrientationVertical, 0)
	t.examples = gtk.NewButtonWithLabel("Start from examples")
	t.examples.AddCSSClass("flat")
	t.examples.AddCSSClass("wtf-snippets-examples")
	t.examples.SetHAlign(gtk.AlignStart)
	t.examples.SetFocusOnClick(false)
	t.examples.ConnectClicked(func() { w.openSnippetsFileThen("Couldn't open it", t.reload) })
	t.list = newSideList(false)
	t.list.onActivate = func(id string) {
		if s, ok := snippetsShared.snippet(id); ok {
			w.openSnippet(s)
		}
	}
	t.list.onMenu = func(ids []string, x, y float64) {
		if s, ok := snippetsShared.snippet(ids[0]); ok {
			t.list.menu.show(x, y, t.menu(s), nil)
		}
	}
	t.root = gtk.NewBox(gtk.OrientationVertical, 4)
	t.root.Append(header.root)
	t.root.Append(t.filter)
	t.root.Append(t.note)
	t.root.Append(t.examples)
	t.root.Append(t.list.root)
	t.off = snippetsShared.observe(t.rebuild)
	t.reload()
	return t
}

func (t *snippetsTab) widget() gtk.Widgetter { return t.root }
func (t *snippetsTab) commandFinished()      {}
func (t *snippetsTab) close()                { t.off() }

// paneChanged follows the focused shell's folder: project files depend on it.
func (t *snippetsTab) paneChanged() {
	if t.w.activeCwd() != t.lastCwd {
		t.reload()
	}
}

func (t *snippetsTab) reload() {
	t.lastCwd = t.w.activeCwd()
	snippetsShared.load(t.w.app.be.Snippets, t.lastCwd)
	t.rebuild()
}

func (t *snippetsTab) rebuild() {
	st := snippetsShared
	q := t.filter.Text()
	var sections []sideSection
	for _, f := range st.list.Files {
		var rows []sideRow
		for _, s := range st.list.Snippets {
			if s.Source == f && uicore.SnippetMatches(s, q) {
				rows = append(rows, t.row(s))
			}
		}
		if len(rows) > 0 {
			sections = append(sections, sideSection{title: snippetsSectionTitle(t.w, f), tip: f, rows: rows})
		}
	}
	shown := sections
	var problems []string
	for _, p := range st.list.Problems {
		if !slices.Contains(problems, p) {
			problems = append(problems, p)
		}
	}
	if len(problems) > 0 {
		shown = append(shown, sideSection{title: "Problems", rows: sshProblemRows(problems)})
	}
	t.list.set(shown)
	var note *gtk.Box
	empty := st.loaded && len(sections) == 0
	if empty {
		if strings.TrimSpace(q) == "" {
			note = sshNote("No snippets yet", "Save commands you run often, with blanks to fill in — or runbooks of steps. Press + to make one, or right-click a command in the terminal → Save as Snippet….")
		} else {
			note = sshNote("", "No snippets match “"+q+"”.")
		}
	}
	sshSetNote(t.note, note)
	t.examples.SetVisible(empty && strings.TrimSpace(q) == "")
}

func (t *snippetsTab) row(s snippets.Snippet) sideRow {
	runbook := uicore.SnippetIsRunbook(s)
	r := sideRow{id: s.ID, icon: "utilities-terminal-symbolic", title: s.Name, detail: uicore.SnippetSubtitle(s),
		monoDetail: !runbook && s.Description == "", tip: uicore.SnippetTooltip(s),
		button: "media-playback-start-symbolic", buttonTip: "Put it at the prompt", buttonRun: func() { t.w.openSnippet(s) }}
	if runbook {
		r.icon = "view-list-ordered-symbolic"
		r.pills = []sidePill{{text: fmt.Sprintf("%d steps", len(s.Steps))}}
	}
	if runbook || len(uicore.SnippetFields(s)) > 0 {
		r.buttonTip = "Fill in and run"
	}
	return r
}

func (t *snippetsTab) menu(s snippets.Snippet) []*menuItem {
	w := t.w
	asks := uicore.SnippetIsRunbook(s) || len(uicore.SnippetFields(s)) > 0
	first := "Insert at Prompt"
	if asks {
		first = "Run…"
	}
	items := []*menuItem{item(first, func() { w.openSnippet(s) })}
	if !uicore.SnippetIsRunbook(s) {
		items = append(items, item("Copy Command", func() {
			w.win.Clipboard().SetText(s.Command)
			w.toast("Copied command")
		}))
	}
	return append(items,
		menuSection,
		item("Edit…", func() { w.showSnippetEditor(&s, &s, "", false) }),
		item("Duplicate…", func() { w.showSnippetEditor(nil, &s, "", false) }),
		menuSection,
		item("Delete…", func() { t.confirmDelete(s) }),
	)
}

func (t *snippetsTab) confirmDelete(s snippets.Snippet) {
	w := t.w
	w.app.confirm(&w.win.Window, "Delete “"+s.Name+"”?",
		"Removes it from "+uicore.PrettyPath(s.Source, w.app.home)+". The file’s other snippets and comments stay.",
		"Delete Snippet", func() {
			svc, cwd, version := w.app.be.Snippets, t.lastCwd, snippetsShared.list.Versions[s.Source]
			go func() {
				l, err := svc.Delete("snippet", s.Source, s.Index, version, cwd)
				onMainAsync(func() {
					if err != nil {
						w.toast(err.Error())
						return
					}
					snippetsShared.set(l, cwd)
					w.toast("Deleted “" + s.Name + "”")
				})
			}()
		})
}

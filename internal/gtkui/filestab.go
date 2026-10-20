//go:build linux

package gtkui

import (
	"fmt"
	"slices"
	"strings"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/dayanraj/wtf/internal/files"
	"github.com/dayanraj/wtf/internal/uicore"
)

func init() {
	registerSidebarTab(sidebarTabInfo{tab: uicore.SidebarFiles, label: "Files", icon: "folder-symbolic", cmd: "sidebar.files",
		make: func(w *Window) sidePaneTab { return newFilesTab(w) }})
}

// Programs that mean the pane is on another machine.
var remoteProcesses = map[string]bool{"ssh": true, "mosh": true, "mosh-client": true, "et": true, "telnet": true}

// filesTab is the Files tab: the focused pane's folder (or a pinned one) as
// a tree, with git status. Clicking a file types its path at the prompt;
// double-clicking a folder cds there. Nothing in it changes files. It reads
// folders when the root changes and after each command (while auto-refresh
// is on), never on a timer. Like FileTree.svelte and the macOS app's
// FilesView.
type filesTab struct {
	w      *Window
	root   *gtk.Box
	header *paneHeader
	back   *gtk.Button
	up     *gtk.Button
	pin    *gtk.Button
	paused *gtk.Label
	// Shows the auto-refresh button as on or paused.
	pauseLook func()
	gitBar    *gtk.Box
	gitText   *gtk.Label
	gitBtn    *gtk.Button
	filter    *gtk.SearchEntry
	list      *sideList

	tree        *uicore.FileTree
	git         *uicore.GitIndex
	gitSeq      int
	changesOnly bool
	shownRoot   string
	loading     map[string]bool
	closed      bool
	refresh     coreglib.SourceHandle
}

func newFilesTab(w *Window) *filesTab {
	f := &filesTab{w: w, tree: uicore.NewFileTree(), loading: map[string]bool{}}
	f.header = newPaneHeader("")
	f.back = f.header.tool("go-previous-symbolic", "Back", f.goBack)
	f.up = f.header.tool("go-up-symbolic", "Up", f.goUp)
	f.pin = f.header.tool("view-pin-symbolic", "Pin this folder", f.togglePin)
	f.pauseLook = netRefreshTools(w, f.header, f.reload)
	f.header.tool("view-restore-symbolic", "Collapse all", func() {
		f.tree.Expanded = map[string]bool{}
		f.rebuild()
	})
	f.paused = sideLabel("Paused: refresh by hand (sidebar-auto-refresh is off)", "wtf-side-note")
	f.gitText = sideLabel("", "wtf-git-branch")
	f.gitText.SetHExpand(true)
	f.gitBtn = gtk.NewButtonWithLabel("")
	f.gitBtn.AddCSSClass("flat")
	f.gitBtn.AddCSSClass("wtf-git-button")
	f.gitBtn.SetFocusOnClick(false)
	f.gitBtn.ConnectClicked(func() {
		f.changesOnly = !f.changesOnly
		f.rebuild()
	})
	f.gitBar = gtk.NewBox(gtk.OrientationHorizontal, 4)
	f.gitBar.AddCSSClass("wtf-git-bar")
	f.gitBar.Append(gtk.NewImageFromIconName("media-playlist-consecutive-symbolic"))
	f.gitBar.Append(f.gitText)
	f.gitBar.Append(f.gitBtn)
	f.filter = gtk.NewSearchEntry()
	f.filter.SetPlaceholderText("Filter")
	f.filter.AddCSSClass("wtf-side-filter")
	f.filter.ConnectSearchChanged(f.rebuild)
	f.filter.ConnectStopSearch(func() {
		f.filter.SetText("")
		w.focusActive()
	})

	f.list = newSideList(false)
	f.list.onClick = f.clicked
	f.list.onActivate = f.activated
	f.list.onMenu = func(ids []string, x, y float64) { f.list.menu.show(x, y, f.menu(ids[0]), nil) }
	f.list.dragData = func(id string) string {
		if p, ok := f.path(id); ok && !p.deleted {
			return p.path
		}
		return ""
	}
	keys := gtk.NewEventControllerKey()
	keys.ConnectKeyPressed(func(keyval, _ uint, state gdk.ModifierType) bool {
		sel := f.list.selected()
		switch {
		case keyval == gdk.KEY_BackSpace:
			f.goBack()
		case keyval == gdk.KEY_Up && state&gdk.AltMask != 0:
			f.goUp()
		case keyval == gdk.KEY_Escape:
			w.focusActive()
		case (keyval == gdk.KEY_Right || keyval == gdk.KEY_Left) && len(sel) == 1:
			if p, ok := f.path(sel[0]); ok && p.dir && f.tree.Expanded[p.path] == (keyval == gdk.KEY_Left) {
				f.toggle(p.path)
			}
		default:
			return false
		}
		return true
	})
	f.list.list.AddController(keys)

	f.root = gtk.NewBox(gtk.OrientationVertical, 4)
	f.root.Append(f.header.root)
	f.root.Append(f.paused)
	f.root.Append(f.gitBar)
	f.root.Append(f.filter)
	f.root.Append(f.list.root)
	f.paneChanged()
	return f
}

func (f *filesTab) widget() gtk.Widgetter { return f.root }

func (f *filesTab) close() {
	f.closed = true
	if f.refresh != 0 {
		coreglib.SourceRemove(f.refresh)
		f.refresh = 0
	}
}

// State

func (f *filesTab) pane() *uicore.PaneState { return f.w.model.ActivePane() }
func (f *filesTab) pinned() string          { return f.w.sidebarState.Pinned }

func (f *filesTab) rootPath() string {
	if p := f.pinned(); p != "" {
		return p
	}
	if ps := f.pane(); ps != nil {
		return ps.Cwd
	}
	return ""
}

func (f *filesTab) remote() bool {
	ps := f.pane()
	return f.pinned() == "" && ps != nil && remoteProcesses[ps.Process]
}

func (f *filesTab) historyKey() string {
	if f.pinned() != "" {
		return "pinned"
	}
	id := 0
	if ps := f.pane(); ps != nil {
		id = ps.ID
	}
	return fmt.Sprintf("pane:%d", id)
}

// paneChanged: the focused pane, its folder or its program changed.
func (f *filesTab) paneChanged() {
	r := f.rootPath()
	if r != f.shownRoot {
		f.shownRoot = r
		if r != "" {
			f.tree.Visit(f.historyKey(), r)
			f.load(r, true)
			if !f.remote() {
				f.loadGit(r)
			} else {
				f.git = nil
			}
		} else {
			f.git = nil
		}
	} else if f.remote() {
		f.git = nil
	}
	f.rebuild()
}

// commandFinished: read the open folders again shortly after (it may have
// changed them), if auto-refresh is on.
func (f *filesTab) commandFinished() {
	if !f.w.app.settings.config().SidebarAutoRefresh {
		return
	}
	if f.refresh != 0 {
		coreglib.SourceRemove(f.refresh)
	}
	f.refresh = coreglib.TimeoutAdd(250, func() bool {
		f.refresh = 0
		f.reload()
		return false
	})
}

func (f *filesTab) load(path string, force bool) {
	if f.loading[path] {
		return
	}
	if _, ok := f.tree.Folders[path]; ok && !force {
		return
	}
	f.loading[path] = true
	svc := f.w.app.be.Files
	go func() {
		l, err := svc.List(path)
		onMainAsync(func() {
			delete(f.loading, path)
			if f.closed {
				return
			}
			if err != nil {
				f.tree.Folders[path] = uicore.FileFolder{Error: err.Error()}
			} else {
				entries := l.Entries
				if entries == nil {
					entries = []files.Entry{}
				}
				f.tree.Folders[path] = uicore.FileFolder{Entries: entries, Truncated: l.Truncated}
			}
			f.rebuild()
		})
	}()
}

func (f *filesTab) loadGit(dir string) {
	f.gitSeq++
	mine := f.gitSeq
	if f.git != nil && !uicore.InsidePath(dir, f.git.Status.Root) {
		f.git = nil
	}
	svc := f.w.app.be.Files
	go func() {
		s := svc.GitStatus(dir)
		onMainAsync(func() {
			if mine != f.gitSeq || f.closed {
				return
			}
			f.git = nil
			if s.Root != "" {
				f.git = uicore.NewGitIndex(s)
			}
			f.rebuild()
		})
	}()
}

// reload reads the shown folders and git again.
func (f *filesTab) reload() {
	r := f.rootPath()
	if r == "" || f.remote() {
		return
	}
	f.load(r, true)
	for path := range f.tree.Expanded {
		if path != r && uicore.InsidePath(path, r) {
			f.load(path, true)
		}
	}
	f.loadGit(r)
}

// Building

// rowPath is what a row stands for.
type rowPath struct {
	path         string
	dir, deleted bool
}

// path decodes a row id: "f:<path>" (a tree entry, "d:" for a folder) or
// "c:<status>:<path>" (a change).
func (f *filesTab) path(id string) (rowPath, bool) {
	switch {
	case strings.HasPrefix(id, "f:"):
		return rowPath{path: id[2:]}, true
	case strings.HasPrefix(id, "d:"):
		return rowPath{path: id[2:], dir: true}, true
	case strings.HasPrefix(id, "c:"):
		status, path, _ := strings.Cut(id[2:], ":")
		dir := strings.HasSuffix(path, "/")
		return rowPath{path: strings.TrimSuffix(path, "/"), dir: dir, deleted: status == "deleted"}, true
	}
	return rowPath{}, false
}

func changeClass(k uicore.ChangeKind) string {
	switch k {
	case uicore.ChangeModified, uicore.ChangeRenamed:
		return "git-modified"
	case uicore.ChangeAdded, uicore.ChangeUntracked:
		return "git-added"
	case uicore.ChangeDeleted, uicore.ChangeConflict:
		return "git-deleted"
	}
	return ""
}

func (f *filesTab) rebuild() {
	if f.closed {
		return
	}
	shown := f.shownRoot
	title := uicore.PrettyPath(shown, f.w.app.home)
	if title != "/" && title != "" {
		title = uicore.BaseName(title)
	}
	f.header.setTitle(title, shown)
	h := f.tree.History[f.historyKey()]
	f.back.SetSensitive(len(h) > 1 && !f.remote())
	if len(h) > 1 {
		f.back.SetTooltipText("Back to " + uicore.PrettyPath(h[len(h)-2], f.w.app.home) + " (Backspace)")
	}
	r := f.rootPath()
	f.up.SetSensitive(r != "" && r != "/" && !f.remote())
	if f.pinned() != "" {
		f.pin.SetTooltipText("Unpin: follow the current folder")
		f.pin.AddCSSClass("active")
	} else {
		f.pin.SetTooltipText("Pin this folder")
		f.pin.RemoveCSSClass("active")
	}
	f.paused.SetVisible(!f.w.app.settings.config().SidebarAutoRefresh)
	f.pauseLook()

	g := f.git
	f.gitBar.SetVisible(g != nil && !f.remote())
	if g != nil {
		f.gitText.SetText(uicore.GitBranchLabel(g.Status))
		if g.Status.Upstream == "" {
			f.gitText.SetTooltipText("No upstream branch")
		} else {
			f.gitText.SetTooltipText("Tracking " + g.Status.Upstream)
		}
		switch n := g.Count(); {
		case g.Status.Error != "":
			f.gitBtn.SetLabel("status unavailable")
			f.gitBtn.SetTooltipText(g.Status.Error)
			f.gitBtn.SetSensitive(false)
		case n > 0:
			more := ""
			if g.Status.Truncated {
				more = "+"
			}
			f.gitBtn.SetLabel(fmt.Sprintf("%d%s %s", n, more, plural(n, "change")))
			f.gitBtn.SetSensitive(true)
			if f.changesOnly {
				f.gitBtn.SetTooltipText("Back to the folder tree")
			} else {
				f.gitBtn.SetTooltipText("List the changed files")
			}
		default:
			f.gitBtn.SetLabel("no changes")
			f.gitBtn.SetSensitive(false)
		}
	}

	section := sideSection{}
	q := f.filter.Text()
	switch {
	case f.remote():
		process := "ssh"
		if ps := f.pane(); ps != nil {
			process = ps.Process
		}
		section.empty = "Remote session: this pane is connected to another machine with " + process + "; its files aren't shown here."
	case r == "":
		section.empty = "Waiting for the shell…"
	case g != nil && f.changesOnly && g.Count() > 0:
		changes := slices.Clone(g.Status.Changes)
		slices.SortFunc(changes, func(a, b files.GitChange) int { return strings.Compare(a.Path, b.Path) })
		for _, c := range changes {
			section.rows = append(section.rows, f.changeRow(c))
		}
	case f.tree.Folders[r].Error != "":
		section.empty = "Can't show " + uicore.PrettyPath(r, f.w.app.home) + ": " + f.tree.Folders[r].Error
	default:
		for _, row := range f.tree.Rows(r, q) {
			section.rows = append(section.rows, f.entryRow(row))
		}
		if len(section.rows) == 0 && f.tree.Folders[r].Entries != nil {
			section.empty = "Empty folder"
			if strings.TrimSpace(q) != "" {
				section.empty = "No matches in open folders"
			}
		}
	}
	f.list.set([]sideSection{section})
}

func (f *filesTab) entryRow(row uicore.FileRow) sideRow {
	if row.More {
		return sideRow{id: "", title: "More files not shown", muted: true, depth: row.Depth}
	}
	e := row.Entry
	r := sideRow{title: e.Name, depth: row.Depth, italic: e.Symlink, muted: e.Hidden || e.Ignored, tip: e.Path}
	if e.Dir {
		r.id = "d:" + e.Path
		r.icon = "folder-symbolic"
		if f.tree.Expanded[e.Path] {
			r.icon = "folder-open-symbolic"
		}
	} else {
		r.id = "f:" + e.Path
		r.icon = "text-x-generic-symbolic"
	}
	if g := f.git; g != nil {
		if e.Dir {
			if k, ok := g.Folder(e.Path); ok {
				r.trailDot = "dot-" + changeClass(k)
				r.tip += "\nChanges inside"
			}
		} else if c, ok := g.File(e.Path); ok {
			k := uicore.ChangeKind(c.Status)
			r.letter, r.titleClass = k.Letter(), changeClass(k)
			r.tip += "\n" + uicore.GitChangeSummary(c)
		}
	}
	return r
}

func (f *filesTab) changeRow(c files.GitChange) sideRow {
	root := f.git.Status.Root
	rel := uicore.RelativePath(root, c.Path)
	dir := strings.HasSuffix(c.Path, "/")
	trimmed := strings.TrimSuffix(rel, "/")
	k := uicore.ChangeKind(c.Status)
	r := sideRow{id: "c:" + c.Status + ":" + c.Path, title: uicore.BaseName(trimmed), detail: uicore.ParentPath(trimmed),
		letter: k.Letter(), titleClass: changeClass(k), strike: c.Status == "deleted", icon: "text-x-generic-symbolic"}
	if r.detail == "." || r.detail == trimmed {
		r.detail = ""
	}
	if dir {
		r.icon = "folder-symbolic"
	}
	r.tip = rel + " — " + uicore.GitChangeSummary(c)
	if c.From != "" {
		r.tip += " (was " + uicore.RelativePath(root, c.From) + ")"
	}
	return r
}

// Actions

func (f *filesTab) clicked(id string) {
	p, ok := f.path(id)
	if !ok || p.deleted {
		return
	}
	if p.dir && !strings.HasPrefix(id, "c:") {
		f.toggle(p.path)
	} else {
		f.insertPath(p.path)
	}
}

// activated (double-click, Enter): a folder is cd'd into.
func (f *filesTab) activated(id string) {
	p, ok := f.path(id)
	if !ok || !p.dir || strings.HasPrefix(id, "c:") {
		return
	}
	// The first click opened it; put it back, and cd.
	f.toggle(p.path)
	f.cd(p.path)
}

func (f *filesTab) toggle(path string) {
	if f.tree.Expanded[path] {
		delete(f.tree.Expanded, path)
	} else {
		f.tree.Expanded[path] = true
		f.load(path, false)
	}
	f.rebuild()
}

func (f *filesTab) pathForShell(path string) string {
	cwd := ""
	if ps := f.pane(); ps != nil {
		cwd = ps.Cwd
	}
	return uicore.ShellQuote(uicore.RelativePath(cwd, path))
}

// insertPath types the path at the prompt (as a paste); nothing runs.
func (f *filesTab) insertPath(path string) {
	if p := f.w.active(); p != nil {
		p.term.PasteText(f.pathForShell(path) + " ")
		p.term.GrabFocus()
	}
}

func (f *filesTab) shellIdle() bool {
	ps := f.pane()
	return ps != nil && ps.IsShell && !ps.Running
}

func (f *filesTab) cd(path string) {
	p := f.w.active()
	if p == nil {
		return
	}
	if !f.shellIdle() {
		f.w.toast("The shell is busy — finish the running program first")
		return
	}
	p.write(uicore.ClearLine + "cd " + uicore.ShellQuote(path) + "\r")
	p.term.GrabFocus()
}

func (f *filesTab) goTo(path string) {
	if f.pinned() != "" {
		f.w.sidebarState.Pinned = path
		f.w.scheduleSave()
		f.paneChanged()
	} else {
		f.cd(path)
	}
}

func (f *filesTab) goUp() {
	if r := f.rootPath(); r != "" && r != "/" && !f.remote() {
		f.goTo(uicore.ParentPath(r))
	}
}

func (f *filesTab) goBack() {
	h := f.tree.History[f.historyKey()]
	if f.remote() || len(h) < 2 {
		return
	}
	if f.pinned() == "" && !f.shellIdle() {
		f.w.toast("The shell is busy — finish the running program first")
		return
	}
	h = h[:len(h)-1]
	f.tree.History[f.historyKey()] = h
	f.goTo(h[len(h)-1])
}

func (f *filesTab) togglePin() {
	if f.pinned() != "" {
		f.w.sidebarState.Pinned = ""
	} else {
		f.w.sidebarState.Pinned = f.rootPath()
	}
	f.w.scheduleSave()
	f.paneChanged()
}

// openInEditor opens a file in $EDITOR, in a split to the right.
func (w *Window) openInEditor(path string, line int) {
	cmd := "${EDITOR:-vi} "
	if line > 0 {
		cmd += fmt.Sprintf("+%d ", line)
	}
	dir := uicore.ParentPath(path)
	w.model.Split(uicore.SplitRow, &dir, cmd+uicore.ShellQuote(path))
}

func (f *filesTab) menu(id string) []*menuItem {
	p, ok := f.path(id)
	if !ok {
		return nil
	}
	w, m := f.w, f.w.model
	var items []*menuItem
	if !p.deleted {
		path := p.path
		if p.dir {
			items = append(items,
				item("cd Here", func() { f.cd(path) }),
				item("Open in New Tab", func() { m.Open(uicore.OpenOptions{Cwd: &path}) }),
				item("Open in New Split", func() { m.Split(uicore.SplitRow, &path, "") }),
				item("Insert Path", func() { f.insertPath(path) }),
				menuSection,
				item("Make Root", func() {
					w.sidebarState.Pinned = path
					w.scheduleSave()
					f.paneChanged()
				}))
		} else {
			items = append(items,
				item("Insert Path", func() { f.insertPath(path) }),
				item("Open in Editor", func() { w.openInEditor(path, 0) }),
				item("Open with Default App", func() { f.run("Couldn't open it", func() error { return w.app.be.Files.Open(path) }) }))
			if uicore.InspectIsCertFile(path) {
				items = append(items, item("Inspect Certificate…", func() { w.showCertFile(path) }))
			}
		}
		cwd := ""
		if ps := f.pane(); ps != nil {
			cwd = ps.Cwd
		}
		items = append(items,
			item("Show in Files", func() { f.run("Couldn't show it", func() error { return w.app.be.Files.Reveal(path) }) }),
			menuSection,
			item("Copy Path", func() {
				w.win.Clipboard().SetText(path)
				w.toast("Copied path")
			}),
			item("Copy Relative Path", func() {
				w.win.Clipboard().SetText(uicore.RelativePath(cwd, path))
				w.toast("Copied relative path")
			}))
	}
	if g := f.git; g != nil {
		if git := g.Menu(p.path, p.dir); len(git) > 0 {
			items = append(items, menuSection)
			root := g.Status.Root
			for _, gi := range git {
				if gi.Heading {
					items = append(items, item(gi.Label, nil))
					continue
				}
				cmd := gi.Command
				items = append(items, item(gi.Label, func() { m.Open(uicore.OpenOptions{Cwd: &root, Command: cmd}) }))
			}
		}
	}
	return items
}

// run does something off the main loop, with a toast if it fails.
func (f *filesTab) run(failed string, do func() error) {
	go func() {
		if err := do(); err != nil {
			onMainAsync(func() { f.w.toast(failed + ": " + err.Error()) })
		}
	}()
}

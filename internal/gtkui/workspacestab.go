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
	registerSidebarTab(sidebarTabInfo{tab: uicore.SidebarWorkspaces, label: "Workspaces", icon: "view-grid-symbolic", cmd: "sidebar.workspaces",
		make: func(w *Window) sidePaneTab { return newWorkspacesTab(w) }})
}

// workspacesTab is the Workspaces tab: workspaces from the snippets files,
// grouped by file. Open (▶ or double-click), edit, duplicate, delete; save
// this window as one. Read like the Snippets tab: when it opens, when the
// focused shell's folder changes and on Reload. Like the Workspaces part of
// SnippetsView.svelte and the macOS app's WorkspacesView.
type workspacesTab struct {
	w       *Window
	root    *gtk.Box
	filter  *gtk.SearchEntry
	note    *gtk.Box
	list    *sideList
	lastCwd string
	off     func()
}

func newWorkspacesTab(w *Window) *workspacesTab {
	t := &workspacesTab{w: w}
	header := newPaneHeader("Workspaces")
	header.tool("document-save-symbolic", "Save this window's tabs as a workspace", w.saveWindowAsWorkspace)
	header.tool("list-add-symbolic", "New workspace", func() { w.showWorkspaceEditor(nil, nil) })
	header.tool("document-open-symbolic", "Open your snippets file in a text editor", w.openSnippetsFile)
	header.tool("view-refresh-symbolic", "Reload", t.reload)
	t.filter = gtk.NewSearchEntry()
	t.filter.SetPlaceholderText("Filter workspaces")
	t.filter.AddCSSClass("wtf-side-filter")
	t.filter.ConnectSearchChanged(t.rebuild)
	t.filter.ConnectStopSearch(func() {
		t.filter.SetText("")
		w.focusActive()
	})
	t.note = gtk.NewBox(gtk.OrientationVertical, 0)
	t.list = newSideList(false)
	t.list.onActivate = func(id string) {
		if ws, ok := workspaceByID(id); ok {
			w.openWorkspace(ws)
		}
	}
	t.list.onMenu = func(ids []string, x, y float64) {
		if ws, ok := workspaceByID(ids[0]); ok {
			t.list.menu.show(x, y, t.menu(ws), nil)
		}
	}
	t.root = gtk.NewBox(gtk.OrientationVertical, 4)
	t.root.Append(header.root)
	t.root.Append(t.filter)
	t.root.Append(t.note)
	t.root.Append(t.list.root)
	t.off = snippetsShared.observe(t.rebuild)
	t.reload()
	return t
}

func (t *workspacesTab) widget() gtk.Widgetter { return t.root }
func (t *workspacesTab) commandFinished()      {}
func (t *workspacesTab) close()                { t.off() }

// paneChanged follows the focused shell's folder: project files depend on it.
func (t *workspacesTab) paneChanged() {
	if t.w.activeCwd() != t.lastCwd {
		t.reload()
	}
}

func (t *workspacesTab) reload() {
	t.lastCwd = t.w.activeCwd()
	snippetsShared.load(t.w.app.be.Snippets, t.lastCwd)
	t.rebuild()
}

func workspaceByID(id string) (snippets.Workspace, bool) {
	for _, ws := range snippetsShared.list.Workspaces {
		if ws.ID == id {
			return ws, true
		}
	}
	return snippets.Workspace{}, false
}

// workspaceMatches is the filter: name, description, and its tabs' titles,
// groups and commands.
func workspaceMatches(ws snippets.Workspace, q string) bool {
	if q == "" {
		return true
	}
	hay := []string{ws.Name, ws.Description}
	for _, t := range ws.Tabs {
		hay = append(hay, t.Title, t.Group, t.Command)
		for _, p := range t.Panes {
			hay = append(hay, p.Command)
		}
	}
	return strings.Contains(strings.ToLower(strings.Join(hay, " ")), q)
}

func (t *workspacesTab) rebuild() {
	st := snippetsShared
	q := strings.ToLower(strings.TrimSpace(t.filter.Text()))
	var sections []sideSection
	shown := 0
	for _, f := range st.list.Files {
		var rows []sideRow
		for _, ws := range st.list.Workspaces {
			if ws.Source == f && workspaceMatches(ws, q) {
				rows = append(rows, t.row(ws))
			}
		}
		if len(rows) > 0 {
			shown += len(rows)
			sections = append(sections, sideSection{title: snippetsSectionTitle(t.w, f), tip: f, rows: rows})
		}
	}
	if len(st.list.Problems) > 0 {
		sections = append(sections, sideSection{title: "Problems", rows: sshProblemRows(st.list.Problems)})
	}
	t.list.set(sections)
	var note *gtk.Box
	if st.loaded && shown == 0 {
		if q == "" {
			note = sshNote("No workspaces yet", "A workspace opens a set of tabs in one go — folders, split panes, tab groups and startup commands. Arrange your tabs, then save this window as a workspace (the first button above) — or press + to build one.")
		} else {
			note = sshNote("", "No workspaces match “"+t.filter.Text()+"”.")
		}
	}
	sshSetNote(t.note, note)
}

func (t *workspacesTab) row(ws snippets.Workspace) sideRow {
	sub := ws.Description
	if sub == "" {
		parts := []string{fmt.Sprintf("%d %s", len(ws.Tabs), plural(len(ws.Tabs), "tab"))}
		var groups []string
		for _, tab := range ws.Tabs {
			if tab.Group != "" && !slices.Contains(groups, tab.Group) {
				groups = append(groups, tab.Group)
			}
		}
		slices.Sort(groups)
		for _, g := range groups {
			parts = append(parts, "group "+g)
		}
		sub = strings.Join(parts, " · ")
	}
	tips := make([]string, len(ws.Tabs))
	for i, tab := range ws.Tabs {
		what := tab.Title
		if what == "" {
			what = tab.Command
		}
		if what == "" {
			var cmds []string
			for _, p := range tab.Panes {
				if p.Command != "" {
					cmds = append(cmds, p.Command)
				}
			}
			what = strings.Join(cmds, " | ")
		}
		if what == "" {
			what = "shell"
		}
		tips[i] = fmt.Sprintf("%d. %s", i+1, what)
	}
	r := sideRow{id: ws.ID, icon: "view-grid-symbolic", title: ws.Name, detail: sub, tip: strings.Join(tips, "\n"),
		button: "media-playback-start-symbolic", buttonTip: "Open", buttonRun: func() { t.w.openWorkspace(ws) }}
	if ws.Window == "new" {
		r.pills = []sidePill{{text: "new window"}}
	}
	return r
}

func (t *workspacesTab) menu(ws snippets.Workspace) []*menuItem {
	w := t.w
	return []*menuItem{
		item("Open…", func() { w.openWorkspace(ws) }),
		menuSection,
		item("Edit…", func() { w.showWorkspaceEditor(&ws, nil) }),
		item("Duplicate…", func() {
			def := workspaceDefOf(ws)
			def.Name += " (copy)"
			w.showWorkspaceEditor(nil, &def)
		}),
		menuSection,
		item("Delete…", func() { t.confirmDelete(ws) }),
	}
}

func (t *workspacesTab) confirmDelete(ws snippets.Workspace) {
	w := t.w
	w.app.confirm(&w.win.Window, "Delete workspace “"+ws.Name+"”?",
		"Removes it from "+uicore.PrettyPath(ws.Source, w.app.home)+". The file’s snippets, other workspaces and comments stay. Open tabs aren’t affected.",
		"Delete Workspace", func() {
			svc, cwd, version := w.app.be.Snippets, t.lastCwd, snippetsShared.list.Versions[ws.Source]
			go func() {
				l, err := svc.Delete("workspace", ws.Source, ws.Index, version, cwd)
				onMainAsync(func() {
					if err != nil {
						w.toast(err.Error())
						return
					}
					snippetsShared.set(l, cwd)
					w.toast("Deleted “" + ws.Name + "”")
				})
			}()
		})
}

func workspaceDefOf(ws snippets.Workspace) snippets.WorkspaceDef {
	return snippets.WorkspaceDef{Name: ws.Name, Description: ws.Description, Window: ws.Window, Tabs: ws.Tabs, Params: ws.Params}
}

// saveWindowAsWorkspace (Save Window as Workspace…) opens the workspace
// editor with this window's tabs, groups, splits, folders and running
// commands.
func (w *Window) saveWindowAsWorkspace() {
	def := w.model.CaptureWorkspace(w.app.home)
	w.showWorkspaceEditor(nil, &def)
}

// Workspace editor

type workspacePaneDraft struct{ cwd, command string }

type workspaceTabDraft struct {
	title, group, color, layout string
	panes                       []workspacePaneDraft
}

// workspaceEditor is the workspace editor: a form of tabs (folders, split
// panes, groups, startup commands) and placeholders, then a review of
// exactly what will be written to the snippets file. Like
// WorkspaceEditor.svelte.
type workspaceEditor struct {
	w       *Window
	sheet   *snippetSheet
	editing *snippets.Workspace

	name, summary string
	newWindow     bool
	tabs          []workspaceTabDraft
	params        map[string]*snippetParamDraft
	file          string
	err           string
	review        *snippetReview
	busy, closed  bool

	paramsBox   *gtk.Box
	shownParams string
	targetBox   *gtk.Box
	reviewBtn   *gtk.Button
	first       *gtk.Entry // the field the keyboard starts in
	off         func()
}

// showWorkspaceEditor edits a workspace (editing), or makes one: from def
// (a captured window, a copy) or empty.
func (w *Window) showWorkspaceEditor(editing *snippets.Workspace, def *snippets.WorkspaceDef) {
	e := &workspaceEditor{w: w, editing: editing, params: map[string]*snippetParamDraft{}}
	if editing != nil {
		d := workspaceDefOf(*editing)
		def = &d
	}
	if def != nil {
		e.name, e.summary, e.newWindow = def.Name, def.Description, def.Window == "new"
		for _, t := range def.Tabs {
			d := workspaceTabDraft{title: t.Title, group: t.Group, color: t.Color, layout: "row"}
			if t.Layout == "column" {
				d.layout = "column"
			}
			for _, p := range uicore.AllPanes(t) {
				d.panes = append(d.panes, workspacePaneDraft{p.Cwd, p.Command})
			}
			e.tabs = append(e.tabs, d)
		}
		e.params = snippetParamDrafts(def.Params)
	}
	if len(e.tabs) == 0 {
		e.tabs = []workspaceTabDraft{{layout: "row", panes: []workspacePaneDraft{{}}}}
	}
	st := snippetsShared
	if editing != nil {
		e.file = editing.Source
	} else {
		e.file = st.defaultTarget()
	}
	e.sheet = newSnippetSheet(w, 680, func() {
		e.closed = true
		if e.off != nil {
			e.off()
		}
	})
	e.render()
	if !st.loaded || editing == nil {
		e.off = st.observe(e.listLoaded)
		st.load(w.app.be.Snippets, w.activeCwd())
	}
	e.sheet.win.Present()
	e.first.GrabFocus()
}

func (e *workspaceEditor) listLoaded() {
	if e.editing != nil {
		return
	}
	ok := false
	for _, t := range snippetsShared.list.Targets {
		ok = ok || t.Path == e.file
	}
	if e.file == "" || !ok {
		e.file = snippetsShared.defaultTarget()
	}
	if e.review == nil {
		e.renderTarget()
		e.updateReview()
	}
}

func (e *workspaceEditor) render() {
	s := e.sheet
	if e.review != nil {
		e.renderReview()
		return
	}
	title := "New Workspace"
	if e.editing != nil {
		title = "Edit “" + e.editing.Name + "”"
	}
	s.clear(title, "Tabs — with folders, split panes, groups and startup commands — opened in one go")
	name := snippetEntry(e.name, "payments on call", false, func(v string) {
		e.name = v
		e.updateReview()
	})
	nameCol := sshColumn("Name", name)
	nameCol.SetHExpand(true)
	descCol := sshColumn("Description", snippetEntry(e.summary, "optional", false, func(v string) { e.summary = v }))
	descCol.SetHExpand(true)
	s.add(sshRowOf(nameCol, descCol))

	for i, t := range e.tabs {
		s.add(e.card(i, t))
	}
	add := gtk.NewButtonWithLabel("+ Add Tab")
	add.AddCSSClass("flat")
	add.SetHAlign(gtk.AlignStart)
	add.ConnectClicked(func() {
		group := ""
		if n := len(e.tabs); n > 0 {
			group = e.tabs[n-1].group
		}
		e.tabs = append(e.tabs, workspaceTabDraft{group: group, layout: "row", panes: []workspacePaneDraft{{}}})
		e.render()
	})
	s.add(add)
	s.add(snippetNote("Names, groups, folders and commands can use placeholders, asked for when it opens: {{service}}, or {{ns=$kube_namespace}}."))

	// Placeholders, as used now in titles, groups, folders and commands:
	// redrawn when they change, so the field being typed in keeps the keyboard.
	e.paramsBox = gtk.NewBox(gtk.OrientationVertical, 6)
	e.shownParams = "\x00"
	s.add(e.paramsBox)
	e.renderParams()

	nw := gtk.NewCheckButtonWithLabel("Open in a new window (otherwise as new tabs in the current one)")
	nw.SetActive(e.newWindow)
	nw.ConnectToggled(func() { e.newWindow = nw.Active() })
	s.add(nw)
	e.targetBox = gtk.NewBox(gtk.OrientationVertical, 0)
	s.add(e.targetBox)
	e.renderTarget()
	s.setError(e.err)
	s.button("Cancel", false, s.close)
	e.reviewBtn = s.button("Review…", true, e.startReview)
	e.updateReview()
	e.first = name
}

func (e *workspaceEditor) renderTarget() {
	for c := e.targetBox.FirstChild(); c != nil; c = e.targetBox.FirstChild() {
		e.targetBox.Remove(c)
	}
	if e.editing == nil {
		e.targetBox.Append(snippetTargetPicker(e.w, e.file, func(f string) {
			e.file = f
			e.updateReview()
		}))
	}
}

func (e *workspaceEditor) updateReview() {
	if e.reviewBtn != nil {
		e.reviewBtn.SetSensitive(!e.busy && strings.TrimSpace(e.name) != "" && e.file != "")
	}
}

// card is one tab: its name, group and color, its panes, and the layout.
func (e *workspaceEditor) card(i int, t workspaceTabDraft) *gtk.Box {
	colors := []string{"color"}
	selected := 0
	for j, c := range uicore.GroupColors {
		colors = append(colors, string(c))
		if string(c) == t.color {
			selected = j + 1
		}
	}
	color := gtk.NewDropDownFromStrings(colors)
	color.SetSelected(uint(selected))
	color.SetSensitive(strings.TrimSpace(t.group) != "")
	color.SetTooltipText(fmt.Sprintf("Group color of tab %d", i+1))
	color.NotifyProperty("selected", func() {
		e.tabs[i].color = ""
		if j := int(color.Selected()); j > 0 && j < len(colors) {
			e.tabs[i].color = colors[j]
		}
	})
	n := sideLabel(fmt.Sprint(i+1), "heading")
	title := snippetEntry(t.title, "Tab name (optional)", false, func(v string) {
		e.tabs[i].title = v
		e.renderParams()
	})
	group := snippetEntry(t.group, "Group (optional)", false, func(v string) {
		e.tabs[i].group = v
		e.renderParams()
		color.SetSensitive(strings.TrimSpace(v) != "")
	})
	head := sshRowOf(n, title, group, color,
		snippetIconButton("go-up-symbolic", fmt.Sprintf("Move tab %d up", i+1), i > 0, func() { e.move(i, -1) }),
		snippetIconButton("go-down-symbolic", fmt.Sprintf("Move tab %d down", i+1), i < len(e.tabs)-1, func() { e.move(i, 1) }),
		snippetIconButton("window-close-symbolic", fmt.Sprintf("Remove tab %d", i+1), len(e.tabs) > 1, func() {
			e.tabs = append(e.tabs[:i], e.tabs[i+1:]...)
			e.render()
		}))
	card := gtk.NewBox(gtk.OrientationVertical, 6)
	card.AddCSSClass("wtf-card")
	card.Append(head)
	for j, p := range t.panes {
		row := sshRowOf(
			snippetEntry(p.cwd, "folder (e.g. ~/src/app)", false, func(v string) {
				e.tabs[i].panes[j].cwd = v
				e.renderParams()
			}),
			snippetEntry(p.command, "startup command (optional)", false, func(v string) {
				e.tabs[i].panes[j].command = v
				e.renderParams()
			}))
		if len(t.panes) > 1 {
			row.Append(snippetIconButton("window-close-symbolic", fmt.Sprintf("Remove pane %d of tab %d", j+1, i+1), true, func() {
				e.tabs[i].panes = append(e.tabs[i].panes[:j], e.tabs[i].panes[j+1:]...)
				e.render()
			}))
		}
		card.Append(row)
	}
	split := gtk.NewButtonWithLabel("+ Split")
	split.AddCSSClass("flat")
	split.ConnectClicked(func() {
		panes := e.tabs[i].panes
		e.tabs[i].panes = append(panes, workspacePaneDraft{cwd: panes[len(panes)-1].cwd})
		e.render()
	})
	foot := sshRowOf(split)
	if len(t.panes) > 1 {
		layout := gtk.NewDropDownFromStrings([]string{"side by side", "stacked"})
		if t.layout == "column" {
			layout.SetSelected(1)
		}
		layout.NotifyProperty("selected", func() {
			e.tabs[i].layout = "row"
			if layout.Selected() == 1 {
				e.tabs[i].layout = "column"
			}
		})
		foot.Append(layout)
	}
	card.Append(foot)
	return card
}

func (e *workspaceEditor) move(i, d int) {
	j := i + d
	if j < 0 || j >= len(e.tabs) {
		return
	}
	e.tabs[i], e.tabs[j] = e.tabs[j], e.tabs[i]
	e.render()
}

func (e *workspaceEditor) placeholderNames() []string {
	var texts []string
	for _, t := range e.tabs {
		texts = append(texts, t.title, t.group)
		for _, p := range t.panes {
			texts = append(texts, p.cwd, p.command)
		}
	}
	var out []string
	for _, f := range uicore.Fields(texts, nil) {
		out = append(out, f.Name)
	}
	return out
}

func (e *workspaceEditor) renderParams() {
	names := e.placeholderNames()
	key := strings.Join(names, "\x00")
	if key == e.shownParams {
		return
	}
	e.shownParams = key
	for c := e.paramsBox.FirstChild(); c != nil; c = e.paramsBox.FirstChild() {
		e.paramsBox.Remove(c)
	}
	if len(names) == 0 {
		return
	}
	head := gtk.NewLabel("Placeholders")
	head.SetXAlign(0)
	head.AddCSSClass("caption-heading")
	e.paramsBox.Append(head)
	grid := gtk.NewGrid()
	grid.SetRowSpacing(6)
	grid.SetColumnSpacing(6)
	for i, n := range names {
		p := snippetParamOf(e.params, n)
		label := sideLabel(n, "monospace")
		label.SetWidthChars(10)
		label.SetMaxWidthChars(10)
		label.SetTooltipText(n)
		grid.Attach(label, 0, i, 1, 1)
		grid.Attach(snippetEntry(p.description, "what to enter", false, func(v string) { p.description = v }), 1, i, 1, 1)
		grid.Attach(snippetEntry(p.def, "default", false, func(v string) { p.def = v }), 2, i, 1, 1)
		grid.Attach(snippetEntry(p.choices, "choices, comma-separated", false, func(v string) { p.choices = v }), 3, i, 1, 1)
	}
	e.paramsBox.Append(grid)
}

func (e *workspaceEditor) startReview() {
	if e.busy {
		return
	}
	switch {
	case strings.TrimSpace(e.name) == "":
		e.err = "Give the workspace a name"
		e.render()
		return
	case e.file == "":
		e.err = "There's no snippets file to save to from here"
		e.render()
		return
	}
	def := snippets.WorkspaceDef{Name: strings.TrimSpace(e.name), Description: strings.TrimSpace(e.summary),
		Tabs: []snippets.WTab{}, Params: []snippets.Param{}}
	if e.newWindow {
		def.Window = "new"
	}
	for _, t := range e.tabs {
		wt := snippets.WTab{Title: t.title, Group: t.group, Color: t.color, Layout: t.layout, Panes: []snippets.WPane{}}
		for _, p := range t.panes {
			wt.Panes = append(wt.Panes, snippets.WPane{Cwd: p.cwd, Command: p.command})
		}
		def.Tabs = append(def.Tabs, wt)
	}
	for _, n := range e.placeholderNames() {
		p := snippetParamOf(e.params, n)
		def.Params = append(def.Params, snippets.Param{Name: n, Description: p.description, Default: p.def,
			Choices: snippetSplitChoices(p.choices), Raw: p.raw})
	}
	edit := snippets.Edit{Kind: "workspace", File: e.file, Index: -1, Version: snippetsShared.list.Versions[e.file], Workspace: def}
	if e.editing != nil {
		edit.Index = e.editing.Index
	}
	e.busy = true
	e.updateReview()
	svc, cwd := e.w.app.be.Snippets, e.w.activeCwd()
	go func() {
		preview, err := svc.PlanEdit(edit, cwd)
		onMainAsync(func() {
			e.busy = false
			if e.closed {
				return
			}
			if err != nil {
				e.err = err.Error()
			} else {
				e.err = ""
				e.review = &snippetReview{edit, preview}
			}
			e.render()
		})
	}()
}

func (e *workspaceEditor) renderReview() {
	s, r := e.sheet, e.review
	verb := snippetReviewVerb(r.preview, e.editing != nil, "Replaces this workspace in")
	s.clear("Review", "")
	s.add(snippetNote(verb + " " + uicore.PrettyPath(r.preview.File, e.w.app.home) + ". Nothing is written until you confirm."))
	text := snippetMonoLabel(r.preview.Text)
	text.AddCSSClass("wtf-snippet-review")
	s.add(text)
	if e.editing != nil {
		s.add(snippetNote("The rest of the file — snippets, other workspaces, your comments — stays as it is."))
	}
	s.setError("")
	s.button("Back", false, func() {
		e.review = nil
		e.render()
	})
	save := s.button("Confirm & Save", true, func() { e.save(r.edit) })
	save.GrabFocus()
}

func (e *workspaceEditor) save(edit snippets.Edit) {
	if e.busy {
		return
	}
	e.busy = true
	w := e.w
	svc, cwd := w.app.be.Snippets, w.activeCwd()
	go func() {
		l, err := svc.ApplyEdit(edit, cwd)
		onMainAsync(func() {
			e.busy = false
			if err == nil {
				snippetsShared.set(l, cwd)
			}
			if e.closed {
				return
			}
			if err != nil {
				e.err = err.Error()
				e.review = nil
				e.render()
				return
			}
			e.sheet.close()
			n := edit.Workspace.Name
			if e.editing != nil {
				w.toast("Saved “" + n + "”")
			} else {
				w.toast("Added “" + n + "” — open it from the Workspaces tab or Open Workspace…")
			}
		})
	}()
}

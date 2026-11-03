//go:build linux

package gtkui

import (
	"fmt"
	"strings"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"

	"github.com/dayanraj/wtf/internal/snippets"
	"github.com/dayanraj/wtf/internal/uicore"
)

// The Snippets tab's dialogs: the run dialog (fill in placeholders, then
// insert or run; or a runbook's steps one by one) and the snippet editor.
// The editors here and in workspacestab.go show a review of exactly what
// will be written before anything is. Strings and rules follow the macOS
// app's SnippetRun and SnippetEditor.

// Run dialog

// showSnippetRun fills in a snippet's placeholders, then inserts or runs
// it — or runs a runbook's steps one by one in the shell that was focused.
// Like SnippetDialog.svelte.
func (w *Window) showSnippetRun(s snippets.Snippet) {
	pane := 0
	var facts uicore.ShellFacts
	if ps := w.model.ActivePane(); ps != nil {
		pane = ps.ID
		facts = uicore.ShellFactsOf(ps, w.infra[ps.ID])
	}
	runbook := uicore.SnippetIsRunbook(s)
	fields := uicore.SnippetFields(s)
	var sub []string
	if s.Description != "" {
		sub = append(sub, s.Description)
	}
	sub = append(sub, "From "+uicore.PrettyPath(s.Source, w.app.home))
	d := adw.NewMessageDialog(&w.win.Window, s.Name, strings.Join(sub, " · "))
	// Kept when the shell turns out busy, so nothing typed is lost.
	d.SetHideOnClose(true)

	form := gtk.NewGrid()
	form.SetRowSpacing(8)
	form.SetColumnSpacing(12)
	type input struct {
		name  string
		value func() string
	}
	var inputs []input
	var first gtk.Widgetter
	values := func() map[string]string {
		v := map[string]string{}
		for _, in := range inputs {
			v[in.name] = in.value()
		}
		return v
	}
	filled := func(command string) (string, error) { return uicore.Fill(command, values(), fields) }

	preview := snippetMonoLabel("")
	type stepRow struct {
		badge, command *gtk.Label
		buttons        []*gtk.Button
	}
	var steps []stepRow
	done := map[int]bool{}
	// Redrawn on every edit.
	changed := func() {
		if runbook {
			for i, r := range steps {
				c, err := filled(s.Steps[i].Command)
				if err != nil {
					r.command.SetText(err.Error())
					r.command.RemoveCSSClass("monospace")
					r.command.AddCSSClass("dim-label")
				} else {
					r.command.SetText(c)
					r.command.AddCSSClass("monospace")
					if done[i] {
						r.command.AddCSSClass("dim-label")
					} else {
						r.command.RemoveCSSClass("dim-label")
					}
				}
				for _, b := range r.buttons {
					b.SetSensitive(err == nil)
				}
				if done[i] {
					r.badge.SetText("✓")
					r.badge.RemoveCSSClass("accent")
					r.badge.AddCSSClass("success")
				} else {
					r.badge.SetText(fmt.Sprint(i + 1))
					r.badge.RemoveCSSClass("success")
					r.badge.AddCSSClass("accent")
				}
			}
			return
		}
		c, err := filled(s.Command)
		if err != nil {
			preview.SetText(err.Error())
			preview.RemoveCSSClass("monospace")
			preview.AddCSSClass("dim-label")
		} else {
			preview.SetText(c)
			preview.AddCSSClass("monospace")
			preview.RemoveCSSClass("dim-label")
		}
		for _, id := range []string{"tab", "run", "insert"} {
			d.SetResponseEnabled(id, err == nil)
		}
	}

	for i, f := range fields {
		label := f.Name
		if f.Description != "" {
			label += " — " + f.Description
		}
		l := gtk.NewLabel(label)
		l.SetXAlign(0)
		form.Attach(l, 0, i, 1, 1)
		initial := f.InitialValue(facts)
		if len(f.Choices) > 0 {
			dd := gtk.NewDropDownFromStrings(f.Choices)
			for j, c := range f.Choices {
				if c == initial {
					dd.SetSelected(uint(j))
				}
			}
			choices := f.Choices
			dd.NotifyProperty("selected", changed)
			inputs = append(inputs, input{f.Name, func() string {
				if s := int(dd.Selected()); s >= 0 && s < len(choices) {
					return choices[s]
				}
				return ""
			}})
			form.Attach(dd, 1, i, 1, 1)
			if first == nil {
				first = dd
			}
			continue
		}
		e := gtk.NewEntry()
		e.SetText(initial)
		e.SetPlaceholderText(f.Default)
		e.SetHExpand(true)
		e.SetActivatesDefault(true)
		e.ConnectChanged(changed)
		inputs = append(inputs, input{f.Name, e.Text})
		form.Attach(e, 1, i, 1, 1)
		if first == nil {
			first = e
		}
	}

	box := gtk.NewBox(gtk.OrientationVertical, 12)
	box.SetSizeRequest(520, -1)
	if len(inputs) > 0 {
		box.Append(form)
	}
	if runbook {
		box.SetSizeRequest(600, -1)
		for i, st := range s.Steps {
			badge := gtk.NewLabel("")
			badge.AddCSSClass("heading")
			badge.SetWidthChars(2)
			title := st.Title
			if title == "" {
				title = fmt.Sprintf("Step %d", i+1)
			}
			name := sideLabel(title, "heading")
			name.SetHExpand(true)
			insert := gtk.NewButtonWithLabel("Insert")
			insert.SetTooltipText("Put it at the prompt")
			run := gtk.NewButtonWithLabel("Run")
			run.AddCSSClass("suggested-action")
			for _, b := range []*gtk.Button{insert, run} {
				b.AddCSSClass("small")
				b.SetVAlign(gtk.AlignCenter)
			}
			step := func(how snippetHow) {
				c, err := filled(s.Steps[i].Command)
				if err == nil && w.runSnippet(c, how, pane) {
					done[i] = true
					changed()
				}
			}
			insert.ConnectClicked(func() { step(snippetInsert) })
			run.ConnectClicked(func() { step(snippetRun) })
			cmd := snippetMonoLabel("")
			head := gtk.NewBox(gtk.OrientationHorizontal, 8)
			head.Append(badge)
			head.Append(name)
			head.Append(insert)
			head.Append(run)
			col := gtk.NewBox(gtk.OrientationVertical, 3)
			col.Append(head)
			col.Append(cmd)
			box.Append(col)
			steps = append(steps, stepRow{badge, cmd, []*gtk.Button{insert, run}})
		}
		box.Append(snippetNote("Steps run in the focused terminal, one at a time: watch each one finish before the next."))
		d.AddResponse("close", "Close")
		d.SetCloseResponse("close")
	} else {
		box.Append(preview)
		d.AddResponse("cancel", "Cancel")
		d.AddResponse("tab", "Run in New Tab")
		d.AddResponse("run", "Run")
		d.AddResponse("insert", "Insert")
		d.SetResponseAppearance("insert", adw.ResponseSuggested)
		d.SetDefaultResponse("insert")
		d.SetCloseResponse("cancel")
	}
	d.SetExtraChild(box)

	act := func(how snippetHow) {
		// A response hides the dialog: it comes back after.
		c, err := filled(s.Command)
		if err != nil {
			onMainAsync(d.Present)
			return
		}
		// The dialog goes first so the terminal can take the keyboard.
		if how == snippetNewTab || w.snippetReady(pane) {
			d.Destroy()
			w.runSnippet(c, how, pane)
			return
		}
		w.runSnippet(c, how, pane)
		onMainAsync(d.Present)
	}
	d.ConnectResponse(func(r string) {
		switch r {
		case "tab":
			act(snippetNewTab)
		case "run":
			act(snippetRun)
		case "insert":
			act(snippetInsert)
		default:
			d.Destroy()
			w.focusActive()
		}
	})
	// Ctrl+Enter runs it; Enter inserts it to check first.
	keys := gtk.NewEventControllerKey()
	keys.SetPropagationPhase(gtk.PhaseCapture)
	keys.ConnectKeyPressed(func(keyval, _ uint, state gdk.ModifierType) bool {
		if runbook || state&gdk.ControlMask == 0 || (keyval != gdk.KEY_Return && keyval != gdk.KEY_KP_Enter) {
			return false
		}
		if _, err := filled(s.Command); err == nil {
			d.Response("run")
		}
		return true
	})
	d.AddController(keys)
	changed()
	d.Present()
	if first != nil {
		gtk.BaseWidget(first).GrabFocus()
	}
}

// snippetMonoLabel is a command: monospaced, wrapping, selectable.
func snippetMonoLabel(text string) *gtk.Label {
	l := gtk.NewLabel(text)
	l.SetXAlign(0)
	l.SetWrap(true)
	l.SetWrapMode(pango.WrapWordChar)
	l.SetMaxWidthChars(1)
	l.SetHExpand(true)
	l.SetSelectable(true)
	l.AddCSSClass("monospace")
	return l
}

// snippetNote is small dimmed help text.
func snippetNote(text string) *gtk.Label {
	l := formNote(text, "dim-label")
	l.AddCSSClass("caption")
	l.SetMaxWidthChars(1)
	l.SetHExpand(true)
	return l
}

// Editor window

// snippetSheet is an editor's window: a title, a scrolling form rebuilt as
// it changes, and an error line and buttons that stay in view. Escape
// closes it.
type snippetSheet struct {
	w       *Window
	win     *adw.Window
	title   *adw.WindowTitle
	body    *gtk.Box
	err     *gtk.Label
	buttons *gtk.Box
	onClose func()
}

func newSnippetSheet(w *Window, width int, onClose func()) *snippetSheet {
	s := &snippetSheet{w: w, onClose: onClose}
	s.win = adw.NewWindow()
	s.win.SetTransientFor(&w.win.Window)
	s.win.SetModal(true)
	s.win.SetDefaultSize(width, -1)
	s.title = adw.NewWindowTitle("", "")
	head := adw.NewHeaderBar()
	head.SetTitleWidget(s.title)
	s.body = gtk.NewBox(gtk.OrientationVertical, 10)
	s.body.AddCSSClass("wtf-snippet-sheet")
	scroll := gtk.NewScrolledWindow()
	scroll.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	scroll.SetPropagateNaturalHeight(true)
	scroll.SetMaxContentHeight(max(300, w.win.Height()*85/100-120))
	scroll.SetVExpand(true)
	scroll.SetChild(s.body)
	s.err = formNote("", "error")
	s.err.SetVisible(false)
	s.buttons = gtk.NewBox(gtk.OrientationHorizontal, 8)
	s.buttons.SetHAlign(gtk.AlignEnd)
	foot := gtk.NewBox(gtk.OrientationVertical, 8)
	foot.SetMarginStart(22)
	foot.SetMarginEnd(22)
	foot.SetMarginBottom(16)
	foot.SetMarginTop(8)
	foot.Append(s.err)
	foot.Append(s.buttons)
	content := gtk.NewBox(gtk.OrientationVertical, 0)
	content.Append(scroll)
	content.Append(foot)
	view := adw.NewToolbarView()
	view.AddTopBar(head)
	view.SetContent(content)
	s.win.SetContent(view)
	keys := gtk.NewEventControllerKey()
	keys.ConnectKeyPressed(func(keyval, _ uint, _ gdk.ModifierType) bool {
		if keyval == gdk.KEY_Escape {
			s.win.Close()
			return true
		}
		return false
	})
	s.win.AddController(keys)
	s.win.ConnectCloseRequest(func() bool {
		if s.onClose != nil {
			s.onClose()
		}
		w.focusActive()
		return false
	})
	return s
}

// clear empties the form for a new rendering.
func (s *snippetSheet) clear(title, subtitle string) {
	s.title.SetTitle(title)
	s.title.SetSubtitle(subtitle)
	for c := s.body.FirstChild(); c != nil; c = s.body.FirstChild() {
		s.body.Remove(c)
	}
	for c := s.buttons.FirstChild(); c != nil; c = s.buttons.FirstChild() {
		s.buttons.Remove(c)
	}
}

func (s *snippetSheet) add(c gtk.Widgetter) { s.body.Append(c) }

func (s *snippetSheet) setError(msg string) {
	s.err.SetText(msg)
	s.err.SetVisible(msg != "")
}

// button adds a button at the bottom; the main one is what Enter presses.
func (s *snippetSheet) button(label string, main bool, run func()) *gtk.Button {
	b := gtk.NewButtonWithLabel(label)
	if main {
		b.AddCSSClass("suggested-action")
		s.win.SetDefaultWidget(b)
	}
	b.ConnectClicked(run)
	s.buttons.Append(b)
	return b
}

func (s *snippetSheet) close() { s.win.Close() }

// snippetEntry is a form's text field: it reports every edit, and Enter
// presses the main button.
func snippetEntry(value, placeholder string, mono bool, set func(string)) *gtk.Entry {
	e := gtk.NewEntry()
	e.SetText(value)
	e.SetPlaceholderText(placeholder)
	e.SetActivatesDefault(true)
	e.SetHExpand(true)
	if mono {
		e.AddCSSClass("monospace")
	}
	e.ConnectChanged(func() { set(e.Text()) })
	return e
}

// snippetIconButton is a small flat button: move up or down, remove.
func snippetIconButton(icon, tip string, on bool, run func()) *gtk.Button {
	b := gtk.NewButtonFromIconName(icon)
	b.AddCSSClass("flat")
	b.SetTooltipText(tip)
	b.SetSensitive(on)
	b.SetVAlign(gtk.AlignCenter)
	b.ConnectClicked(run)
	return b
}

// snippetTargetPicker is "Save to": the files something new can go to.
func snippetTargetPicker(w *Window, file string, set func(string)) *gtk.Box {
	targets := snippetsShared.list.Targets
	labels := make([]string, len(targets))
	selected := 0
	for i, t := range targets {
		kind := "yours"
		switch t.Kind {
		case "project":
			kind = "this project’s, for git"
		case "team":
			kind = "team file"
		}
		labels[i] = uicore.PrettyPath(t.Path, w.app.home) + " — " + kind
		if !t.Exists {
			labels[i] += " (new file)"
		}
		if t.Path == file {
			selected = i
		}
	}
	dd := gtk.NewDropDownFromStrings(labels)
	if len(targets) > 0 {
		dd.SetSelected(uint(selected))
	}
	dd.NotifyProperty("selected", func() {
		if i := int(dd.Selected()); i >= 0 && i < len(targets) {
			set(targets[i].Path)
		}
	})
	l := gtk.NewLabel("Save to")
	l.AddCSSClass("caption-heading")
	return sshRowOf(l, dd)
}

// snippetSplitChoices reads "a, b, c".
func snippetSplitChoices(s string) []string {
	var out []string
	for _, c := range strings.Split(s, ",") {
		if c = strings.TrimSpace(c); c != "" {
			out = append(out, c)
		}
	}
	return out
}

// snippetReviewVerb says what saving does to the file.
func snippetReviewVerb(p snippets.Preview, editing bool, replaces string) string {
	switch {
	case p.NewFile:
		return "Creates"
	case editing:
		return replaces
	}
	return "Adds to the end of"
}

// snippetParamDraft is a placeholder's settings as typed.
type snippetParamDraft struct {
	description, def, choices string
	raw                       bool
}

func snippetParamDrafts(params []snippets.Param) map[string]*snippetParamDraft {
	out := map[string]*snippetParamDraft{}
	for _, p := range params {
		out[p.Name] = &snippetParamDraft{description: p.Description, def: p.Default, choices: strings.Join(p.Choices, ", "), raw: p.Raw}
	}
	return out
}

func snippetParamOf(drafts map[string]*snippetParamDraft, name string) *snippetParamDraft {
	p := drafts[name]
	if p == nil {
		p = &snippetParamDraft{}
		drafts[name] = p
	}
	return p
}

// Snippet editor

// saveAsSnippet opens the snippet editor with a command filled in (a
// command block's Save as Snippet…).
func (w *Window) saveAsSnippet(command string) { w.showSnippetEditor(nil, nil, command, false) }

// showSnippetDraft opens the editor filled in by Claude (open_snippet_editor,
// #29): a new snippet, named as given.
func (w *Window) showSnippetDraft(s snippets.Snippet) {
	w.showSnippetEditor(nil, &s, "", true)
}

// snippetEditor is the snippet editor: a command or a runbook of steps,
// its placeholders, the file to save to; then a review of exactly what
// will be written. Like SnippetEditor.svelte.
type snippetEditor struct {
	w       *Window
	sheet   *snippetSheet
	editing *snippets.Snippet
	tags    []string

	name, summary, command string
	runbook                bool
	steps                  []snippets.Step
	params                 map[string]*snippetParamDraft
	file                   string
	err                    string
	review                 *snippetReview
	busy, closed           bool

	paramsBox   *gtk.Box
	shownParams string
	targetBox   *gtk.Box
	reviewBtn   *gtk.Button
	first       *gtk.Entry // the field the keyboard starts in
	off         func()
}

type snippetReview struct {
	edit    snippets.Edit
	preview snippets.Preview
}

// showSnippetEditor edits a snippet (editing), or makes one: a copy of
// from, or a command.
// snippetEditorShown is the open editor, if any (for the self-check).
var snippetEditorShown *snippetEditor

// draft: from is new as given (Claude's), not a copy to rename.
func (w *Window) showSnippetEditor(editing, from *snippets.Snippet, command string, draft bool) {
	e := &snippetEditor{w: w, editing: editing, command: command, steps: []snippets.Step{{}}, params: map[string]*snippetParamDraft{}}
	if from != nil {
		e.tags = from.Tags
		e.name = from.Name
		if editing == nil && !draft {
			e.name += " (copy)"
		}
		e.summary, e.command = from.Description, from.Command
		e.runbook = uicore.SnippetIsRunbook(*from)
		if e.runbook {
			e.steps = append([]snippets.Step{}, from.Steps...)
		}
		e.params = snippetParamDrafts(from.Params)
	}
	st := snippetsShared
	if editing != nil {
		e.file = editing.Source
	} else {
		e.file = st.defaultTarget()
	}
	e.sheet = newSnippetSheet(w, 600, func() {
		e.closed = true
		if e.off != nil {
			e.off()
		}
		if snippetEditorShown == e {
			snippetEditorShown = nil
		}
	})
	snippetEditorShown = e
	e.render()
	if !st.loaded || editing == nil {
		e.off = st.observe(e.listLoaded)
		st.load(w.app.be.Snippets, w.activeCwd())
	}
	e.sheet.win.Present()
	e.first.GrabFocus()
}

// listLoaded: the targets are known; something new goes to the user's file.
func (e *snippetEditor) listLoaded() {
	if e.editing != nil {
		return
	}
	targets := snippetsShared.list.Targets
	ok := false
	for _, t := range targets {
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

func (e *snippetEditor) render() {
	s := e.sheet
	if e.review != nil {
		e.renderReview()
		return
	}
	title := "New Snippet"
	if e.editing != nil {
		title = "Edit “" + e.editing.Name + "”"
	}
	s.clear(title, "A command with blanks to fill in, or a runbook of steps")
	name := snippetEntry(e.name, "Restart a deployment", false, func(v string) {
		e.name = v
		e.updateReview()
	})
	s.add(sshColumn("Name", name))
	s.add(sshColumn("Description", snippetEntry(e.summary, "optional — shown in the list", false, func(v string) { e.summary = v })))

	command := gtk.NewToggleButtonWithLabel("Command")
	runbook := gtk.NewToggleButtonWithLabel("Runbook (steps)")
	runbook.SetGroup(command)
	command.SetActive(!e.runbook)
	runbook.SetActive(e.runbook)
	pick := func(b *gtk.ToggleButton, on bool) {
		b.ConnectToggled(func() {
			if b.Active() && e.runbook != on {
				e.runbook = on
				onMainAsync(e.render)
			}
		})
	}
	pick(command, false)
	pick(runbook, true)
	kind := gtk.NewBox(gtk.OrientationHorizontal, 0)
	kind.AddCSSClass("linked")
	kind.SetHAlign(gtk.AlignStart)
	kind.Append(command)
	kind.Append(runbook)
	s.add(kind)

	if e.runbook {
		for i, st := range e.steps {
			s.add(e.stepRow(i, st))
		}
		add := gtk.NewButtonWithLabel("+ Add Step")
		add.AddCSSClass("flat")
		add.SetHAlign(gtk.AlignStart)
		add.ConnectClicked(func() {
			e.steps = append(e.steps, snippets.Step{})
			e.render()
		})
		s.add(add)
	} else {
		s.add(sshColumn("Command", snippetEntry(e.command, "kubectl -n {{ns=$kube_namespace}} rollout restart deploy/{{name}}", true, func(v string) {
			e.command = v
			e.renderParams()
		})))
	}
	s.add(snippetNote("{{name}} asks for a value; {{name=default}} fills one in. Defaults can follow the shell: $kube_namespace, $kube_context, $aws_profile, $aws_region, $cwd."))
	e.paramsBox = gtk.NewBox(gtk.OrientationVertical, 6)
	e.shownParams = "\x00"
	s.add(e.paramsBox)
	e.renderParams()
	e.targetBox = gtk.NewBox(gtk.OrientationVertical, 0)
	s.add(e.targetBox)
	e.renderTarget()
	s.setError(e.err)
	s.button("Cancel", false, s.close)
	e.reviewBtn = s.button("Review…", true, e.startReview)
	e.updateReview()
	e.first = name
}

func (e *snippetEditor) renderTarget() {
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

func (e *snippetEditor) updateReview() {
	if e.reviewBtn != nil {
		e.reviewBtn.SetSensitive(!e.busy && strings.TrimSpace(e.name) != "" && e.file != "")
	}
}

func (e *snippetEditor) stepRow(i int, st snippets.Step) *gtk.Box {
	n := sideLabel(fmt.Sprint(i+1), "heading")
	n.SetVAlign(gtk.AlignStart)
	n.SetMarginTop(6)
	fields := gtk.NewBox(gtk.OrientationVertical, 4)
	fields.SetHExpand(true)
	fields.Append(snippetEntry(st.Title, "What this step does (optional)", false, func(v string) { e.steps[i].Title = v }))
	fields.Append(snippetEntry(st.Command, "kubectl cordon {{node}}", true, func(v string) {
		e.steps[i].Command = v
		e.renderParams()
	}))
	return sshRowOf(n, fields,
		snippetIconButton("go-up-symbolic", fmt.Sprintf("Move step %d up", i+1), i > 0, func() { e.move(i, -1) }),
		snippetIconButton("go-down-symbolic", fmt.Sprintf("Move step %d down", i+1), i < len(e.steps)-1, func() { e.move(i, 1) }),
		snippetIconButton("window-close-symbolic", fmt.Sprintf("Remove step %d", i+1), len(e.steps) > 1, func() {
			e.steps = append(e.steps[:i], e.steps[i+1:]...)
			e.render()
		}))
}

func (e *snippetEditor) move(i, d int) {
	j := i + d
	if j < 0 || j >= len(e.steps) {
		return
	}
	e.steps[i], e.steps[j] = e.steps[j], e.steps[i]
	e.render()
}

// currentFields are the placeholders as the command (or steps) stand.
func (e *snippetEditor) currentFields() []uicore.Field {
	if !e.runbook {
		return uicore.Fields([]string{e.command}, nil)
	}
	var texts []string
	for _, st := range e.steps {
		if strings.TrimSpace(st.Command) != "" {
			texts = append(texts, st.Command)
		}
	}
	return uicore.Fields(texts, nil)
}

// renderParams shows the placeholders as the command stands: redrawn when
// they change, so the command field keeps the keyboard.
func (e *snippetEditor) renderParams() {
	fields := e.currentFields()
	var key strings.Builder
	for _, f := range fields {
		key.WriteString(f.Name + "=" + f.Default + "\x00")
	}
	if key.String() == e.shownParams {
		return
	}
	e.shownParams = key.String()
	for c := e.paramsBox.FirstChild(); c != nil; c = e.paramsBox.FirstChild() {
		e.paramsBox.Remove(c)
	}
	if len(fields) == 0 {
		return
	}
	head := gtk.NewLabel("Placeholders")
	head.SetXAlign(0)
	head.AddCSSClass("caption-heading")
	e.paramsBox.Append(head)
	grid := gtk.NewGrid()
	grid.SetRowSpacing(6)
	grid.SetColumnSpacing(6)
	for i, f := range fields {
		p := snippetParamOf(e.params, f.Name)
		name := sideLabel(f.Name, "monospace")
		name.SetWidthChars(10)
		name.SetMaxWidthChars(10)
		name.SetTooltipText(f.Name)
		defHint := "default"
		if f.Default != "" && f.Default != p.def {
			defHint = "default (" + f.Default + " in the command)"
		}
		raw := gtk.NewCheckButtonWithLabel("raw")
		raw.SetActive(p.raw)
		raw.SetTooltipText("Insert as typed instead of shell-quoting (for several flags at once)")
		raw.ConnectToggled(func() { p.raw = raw.Active() })
		grid.Attach(name, 0, i, 1, 1)
		grid.Attach(snippetEntry(p.description, "what to enter", false, func(v string) { p.description = v }), 1, i, 1, 1)
		grid.Attach(snippetEntry(p.def, defHint, false, func(v string) { p.def = v }), 2, i, 1, 1)
		grid.Attach(snippetEntry(p.choices, "choices, comma-separated", false, func(v string) { p.choices = v }), 3, i, 1, 1)
		grid.Attach(raw, 4, i, 1, 1)
	}
	e.paramsBox.Append(grid)
}

func (e *snippetEditor) startReview() {
	if e.busy {
		return
	}
	switch {
	case strings.TrimSpace(e.name) == "":
		e.err = "Give the snippet a name"
		e.render()
		return
	case e.file == "":
		e.err = "There's no snippets file to save to from here"
		e.render()
		return
	}
	def := snippets.Def{Name: e.name, Description: e.summary, Tags: e.tags, Params: []snippets.Param{}}
	if e.runbook {
		def.Steps = e.steps
	} else {
		def.Command = e.command
	}
	for _, f := range e.currentFields() {
		p := snippetParamOf(e.params, f.Name)
		def.Params = append(def.Params, snippets.Param{Name: f.Name, Description: p.description, Default: p.def,
			Choices: snippetSplitChoices(p.choices), Raw: p.raw})
	}
	edit := snippets.Edit{Kind: "snippet", File: e.file, Index: -1, Version: snippetsShared.list.Versions[e.file], Snippet: def}
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

func (e *snippetEditor) renderReview() {
	s, r := e.sheet, e.review
	verb := snippetReviewVerb(r.preview, e.editing != nil, "Replaces this snippet in")
	s.clear("Review", "")
	s.add(snippetNote(verb + " " + uicore.PrettyPath(r.preview.File, e.w.app.home) + ". Nothing is written until you confirm."))
	text := snippetMonoLabel(r.preview.Text)
	text.AddCSSClass("wtf-snippet-review")
	s.add(text)
	if e.editing != nil {
		s.add(snippetNote("The rest of the file — other snippets, your comments — stays as it is."))
	}
	s.setError("")
	back := s.button("Back", false, func() {
		e.review = nil
		e.render()
	})
	save := s.button("Confirm & Save", true, func() { e.save(r.edit) })
	back.SetSensitive(!e.busy)
	save.SetSensitive(!e.busy)
	save.GrabFocus()
}

func (e *snippetEditor) save(edit snippets.Edit) {
	if e.busy {
		return
	}
	e.busy = true
	w := e.w
	svc, cwd := w.app.be.Snippets, w.activeCwd()
	go func() {
		l, err := svc.ApplyEdit(edit, cwd)
		onMainAsync(func() {
			if err == nil {
				snippetsShared.set(l, cwd)
			}
			if e.closed {
				return
			}
			if err != nil {
				e.err = err.Error()
				e.review = nil
				e.busy = false
				e.render()
				return
			}
			e.sheet.close()
			n := strings.TrimSpace(edit.Snippet.Name)
			if e.editing != nil {
				w.toast("Saved “" + n + "”")
			} else {
				w.toast("Added “" + n + "”")
			}
		})
	}()
}

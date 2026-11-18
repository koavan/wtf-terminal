package gtkui

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"

	"github.com/dayanraj/wtf/internal/bridge"
)

// What your agents wait on you for (#39): a permission request or a
// question. approvalBody and approvalActions say what one shows (the
// Agents tab's rows and the floating cards share them); approvalDeck is the
// stack of cards floating over a window's terminals, bottom right
// (approval-cards), as macos/Sources/Wtf/ApprovalCards.swift.

func init() {
	extraCSS = append(extraCSS, `
.wtf-approval-card { background: @popover_bg_color; border: 1px solid alpha(@warning_color, 0.6); border-radius: 10px;
  padding: 8px 10px 9px 10px; box-shadow: 0 2px 12px alpha(black, 0.25); }
.wtf-approval-card.question { border-color: alpha(@accent_color, 0.6); }
.wtf-approval-peek { background: shade(@popover_bg_color, 0.94); border: 1px solid alpha(currentColor, 0.12);
  border-top: none; border-radius: 0 0 9px 9px; min-height: 5px; }
.wtf-approval-pill { background: @warning_color; color: white; border-radius: 12px; font-weight: bold; padding: 2px 12px; min-height: 0; }
.wtf-approval-pill.question { background: @accent_color; }
.wtf-approval-count { font-size: 0.85em; font-feature-settings: "tnum"; }
`)
}

// requestKind names what an agent asks: run, edit, tool or question.
func requestKind(ap bridge.Approval) string {
	if len(ap.Questions) > 0 {
		return "question"
	}
	switch ap.Tool {
	case "Bash", "exec_command", "shell", "local_shell", "mcp__plugin_wtf_wtf__run_readonly", "mcp__plugin_wtf_wtf__type_at_prompt":
		return "run"
	case "Edit", "MultiEdit", "Write", "NotebookEdit", "apply_patch":
		return "edit"
	}
	return "tool"
}

// requestVerb is what the agent wants, after its name.
func requestVerb(ap bridge.Approval) string {
	switch requestKind(ap) {
	case "run":
		return "wants to run"
	case "edit":
		return "wants to edit"
	case "question":
		if n := len(ap.Questions); n > 1 {
			return fmt.Sprintf("asks you %d questions", n)
		}
		return "asks you"
	}
	return "wants to use " + toolName(ap.Tool)
}

// approvalBody: what it asks, as wrapped lines: the command or file, where
// it would act, an edit's change (diffLines of it), or the questions.
func approvalBody(a *App, ap bridge.Approval, diffLines int) []sideText {
	var more []sideText
	if len(ap.Questions) > 0 {
		for _, q := range ap.Questions {
			more = append(more, sideText{text: a.settings.text(q.Question)})
			if !ap.Clickable() {
				for _, o := range q.Options {
					more = append(more, sideText{text: "· " + a.settings.text(o), classes: []string{"dim-label"}})
				}
			}
		}
		return more
	}
	if ap.Description != "" {
		more = append(more, sideText{text: a.settings.text(ap.Description), classes: []string{"dim-label"}})
	}
	if ap.What != "" {
		more = append(more, sideText{text: a.settings.text(ap.What), classes: []string{"monospace"}})
	}
	for _, f := range ap.Facts {
		more = append(more, sideText{text: a.settings.text(f)})
	}
	if ap.Diff != "" {
		lines := strings.Split(ap.Diff, "\n")
		shown := lines[:min(len(lines), diffLines)]
		for _, l := range shown {
			class := "dim-label"
			switch {
			case strings.HasPrefix(l, "- "):
				class = "wtf-approval-diff-del"
			case strings.HasPrefix(l, "+ "):
				class = "wtf-approval-diff-add"
			}
			more = append(more, sideText{text: a.settings.text(l), classes: []string{"monospace", class}})
		}
		if n := len(lines) - len(shown); n > 0 {
			more = append(more, sideText{text: fmt.Sprintf("… %d more lines in the pane", n), classes: []string{"dim-label"}})
		}
	}
	return more
}

// approvalActions: Allow (▾ its "don't ask again" choices) / Deny (▾ Deny
// and Tell…) / Go to Agent, or a question's options and Other….
// shortcut: Allow names its key (the floating card's).
func approvalActions(w *Window, st bridge.Status, ap bridge.Approval, shortcut bool) []sideAction {
	a := w.app
	b := a.be.Bridge
	id, session, product := ap.ID, st.Session, agentProduct(st)
	if len(ap.Questions) > 0 {
		if !ap.Clickable() {
			return []sideAction{{label: "Go to Agent", key: true, tip: "Answer on its floating card, or in its pane", run: func() { a.goToAgent(session) }}}
		}
		var acts []sideAction
		for _, o := range ap.Questions[0].Options {
			acts = append(acts, sideAction{label: a.settings.text(o), tip: "Answer " + a.settings.text(o) + ", as choosing it in " + product + "'s own dialog",
				run: func() { w.answerQuestion(id, o) }})
		}
		if ap.FreeText {
			question := a.settings.text(ap.Questions[0].Question)
			acts = append(acts, sideAction{label: "Other…", tip: "Answer in your own words, as typing your own in " + product + "'s dialog",
				runAt: func(at *gtk.Button) {
					w.tellAt(at, question, "Your answer", "Answer", func(text string) error { return b.AnswerText(id, text) })
				}})
		}
		return append(acts, sideAction{label: "Go to Agent", tip: "Show its pane", run: func() { a.goToAgent(session) }})
	}
	allow := "Allow"
	if k := keyOf("approval.allow"); shortcut && k != "" {
		allow += " " + k
	}
	acts := []sideAction{{label: allow, key: true, tip: "Let " + product + " do this once, as Yes in its own dialog", run: func() { w.answerApproval(id, bridge.Allow) }}}
	if len(ap.Always) > 0 {
		var items []*menuItem
		for i, label := range ap.Always {
			items = append(items, item(a.settings.text(label), func() { w.allowAlways(id, i) }))
		}
		acts = append(acts, sideAction{label: "▾", tip: "Allow, and don't ask again", runAt: func(at *gtk.Button) { w.menuAt(at, items) }})
	}
	tell := "Deny, and tell " + product + " what to do instead"
	return append(acts,
		sideAction{label: "Deny", tip: "Refuse it and stop " + product + "'s turn, as No in its own dialog", run: func() { w.answerApproval(id, bridge.Deny) }},
		sideAction{label: "▾", tip: tell, runAt: func(at *gtk.Button) {
			w.menuAt(at, []*menuItem{item("Deny and Tell "+product+"…", func() {
				w.tellAt(at, tell, "e.g. use pnpm instead", "Deny and Send", func(text string) error { return b.DenyWith(id, text) })
			})})
		}},
		sideAction{label: "Go to Agent", tip: "Show its pane", run: func() { a.goToAgent(session) }},
	)
}

// menuAt shows a ▾'s menu under its button. The menu hangs off the window's
// overlay, which outlives the button when its row or card is rebuilt.
func (w *Window) menuAt(at *gtk.Button, items []*menuItem) {
	if w.actionMenu == nil {
		w.actionMenu = newMenuPopover(w.overlay)
	}
	r, ok := at.ComputeBounds(w.overlay)
	if !ok {
		return
	}
	w.actionMenu.show(float64(r.X()), float64(r.Y()+r.Height()), items, nil)
}

// tellAt asks, in a popover from a button, for what you tell an agent:
// what to do instead, or your own answer. Only it takes the keyboard, and
// only once you've asked for it; Enter sends, Esc cancels. Like menuAt's,
// it hangs off the overlay, one kept for the window.
func (w *Window) tellAt(at *gtk.Button, title, placeholder, send string, do func(string) error) {
	r, ok := at.ComputeBounds(w.overlay)
	if !ok {
		return
	}
	if w.tellPop == nil {
		w.tellPop = gtk.NewPopover()
		w.tellPop.SetParent(w.overlay)
		w.tellPop.ConnectClosed(func() { coreglib.IdleAdd(w.focusActive) })
	}
	pop := w.tellPop
	box := gtk.NewBox(gtk.OrientationVertical, 8)
	box.SetMarginTop(6)
	box.SetMarginBottom(6)
	box.SetMarginStart(6)
	box.SetMarginEnd(6)
	head := gtk.NewLabel(title)
	head.SetWrap(true)
	head.SetMaxWidthChars(44)
	head.SetXAlign(0)
	head.AddCSSClass("heading")
	entry := gtk.NewEntry()
	entry.SetPlaceholderText(placeholder)
	entry.SetWidthChars(40)
	go_ := func() {
		text := strings.TrimSpace(entry.Text())
		if text == "" {
			entry.ErrorBell()
			return
		}
		pop.Popdown()
		go func() {
			if err := do(text); err != nil {
				coreglib.IdleAdd(func() { w.toast("Not sent: " + err.Error()) })
			}
		}()
	}
	entry.ConnectActivate(go_)
	cancel := gtk.NewButtonWithLabel("Cancel")
	cancel.ConnectClicked(pop.Popdown)
	ok_ := gtk.NewButtonWithLabel(send)
	ok_.AddCSSClass("suggested-action")
	ok_.ConnectClicked(go_)
	row := gtk.NewBox(gtk.OrientationHorizontal, 6)
	row.SetHAlign(gtk.AlignEnd)
	row.Append(cancel)
	row.Append(ok_)
	box.Append(head)
	box.Append(entry)
	box.Append(row)
	pop.SetChild(box)
	rect := gdk.NewRectangle(int(r.X()), int(r.Y()), int(r.Width()), int(r.Height()))
	pop.SetPointingTo(&rect)
	pop.Popup()
	entry.GrabFocus()
}

// approvalDeck: the requests waiting on you, floating over a window's
// terminals, bottom right: the oldest as a card (what it asks and its
// buttons), the others peeking out under it, "1 of 3". Hidden for an agent
// whose pane is in front in this window (its own dialog is right there).
// Collapsed, it's a pill with the count; a new request opens it again. It
// never takes the keyboard: approval.allow allows the card's request.
type approvalDeck struct {
	w         *Window
	root      *gtk.Box
	collapsed bool
	shown     []approval
	known     map[string]bool
	topID     string
	stop      func()
	// What's built: rebuilt only when it changes, so a form keeps its
	// focus and what you've typed.
	built string
	// A form's answers so far, by request.
	picks map[string][]bridge.Pick
}

const deckWidth = 380

func newApprovalDeck(w *Window) *approvalDeck {
	d := &approvalDeck{w: w, known: map[string]bool{}}
	d.root = gtk.NewBox(gtk.OrientationVertical, 0)
	d.root.SetHAlign(gtk.AlignEnd)
	d.root.SetVAlign(gtk.AlignEnd)
	d.root.SetMarginEnd(14)
	d.root.SetMarginBottom(12)
	d.root.SetCanFocus(false)
	d.root.SetVisible(false)
	d.picks = map[string][]bridge.Pick{}
	offAgents := w.app.agents.observe(d.refresh)
	offSettings := w.app.settings.observe(func() { d.rebuild(true) })
	d.stop = func() { offAgents(); offSettings() }
	return d
}

// top is the request approval.allow answers: one to allow, or a filled-in
// form to submit (a single question's options answer it).
func (d *approvalDeck) top() (bridge.Approval, bool) {
	if d.collapsed || len(d.shown) == 0 {
		return bridge.Approval{}, false
	}
	ap := d.shown[0].ap
	return ap, len(ap.Questions) == 0 || ap.Form && d.complete(ap)
}

func (d *approvalDeck) allowTop() {
	ap, ok := d.top()
	switch {
	case !ok:
	case ap.Form:
		d.submit(ap)
	default:
		d.w.answerApproval(ap.ID, bridge.Allow)
	}
}

// complete: every question has an option or your own words.
func (d *approvalDeck) complete(ap bridge.Approval) bool {
	p := d.picks[ap.ID]
	if len(p) != len(ap.Questions) {
		return false
	}
	for _, x := range p {
		if len(x.Options) == 0 && strings.TrimSpace(x.Text) == "" {
			return false
		}
	}
	return true
}

func (d *approvalDeck) submit(ap bridge.Approval) {
	w, b, picks := d.w, d.w.app.be.Bridge, slices.Clone(d.picks[ap.ID])
	go func() {
		if err := b.AnswerAll(ap.ID, picks); err != nil {
			coreglib.IdleAdd(func() { w.toast("Not answered: " + err.Error()) })
		}
	}()
}

// refresh rebuilds from the agents' requests (they changed, or the tab in
// front did), when what it shows changed.
func (d *approvalDeck) refresh() { d.rebuild(false) }

func (d *approvalDeck) rebuild(force bool) {
	w := d.w
	front := map[string]bool{}
	if t, ok := w.model.Active(); ok {
		for _, id := range t.Root.Leaves() {
			if ps := w.model.Panes[id]; ps != nil {
				front[ps.SessionID] = true
			}
		}
	}
	d.shown = nil
	if !w.background && w.app.settings.config().ApprovalCards {
		for _, ask := range w.app.agents.approvals() {
			if !front[ask.st.Session] {
				d.shown = append(d.shown, ask)
			}
		}
	}
	ids := map[string]bool{}
	for _, ask := range d.shown {
		ids[ask.ap.ID] = true
		// Something new asks: open it again.
		if !d.known[ask.ap.ID] {
			d.collapsed = false
		}
	}
	d.known = ids
	maps.DeleteFunc(d.picks, func(id string, _ []bridge.Pick) bool { return !ids[id] })
	var sig strings.Builder
	for _, ask := range d.shown {
		sig.WriteString(ask.ap.ID + ",")
	}
	fmt.Fprintf(&sig, "|%v|", d.collapsed)
	if len(d.shown) > 0 {
		title, _ := w.app.agentTitle(d.shown[0].st)
		sig.WriteString(title + strings.Join(d.shown[0].ap.Facts, "\n"))
	}
	if !force && sig.String() == d.built {
		return
	}
	d.built = sig.String()
	for c := d.root.FirstChild(); c != nil; c = d.root.FirstChild() {
		d.root.Remove(c)
	}
	if len(d.shown) == 0 {
		d.root.SetVisible(false)
		d.topID = ""
		return
	}
	d.root.SetVisible(true)
	if d.collapsed {
		d.root.Append(d.pill())
		d.topID = ""
		return
	}
	ask := d.shown[0]
	card := d.card(ask.st, ask.ap)
	if ask.ap.ID != d.topID {
		// A new card slides up into place, once.
		rev := gtk.NewRevealer()
		rev.SetTransitionType(gtk.RevealerTransitionTypeSlideUp)
		rev.SetTransitionDuration(180)
		rev.SetChild(card)
		d.root.Append(rev)
		coreglib.IdleAdd(func() { rev.SetRevealChild(true) })
	} else {
		d.root.Append(card)
	}
	// The others, peeking out under the card.
	for i := 0; i < min(len(d.shown)-1, 2); i++ {
		p := gtk.NewBox(gtk.OrientationHorizontal, 0)
		p.AddCSSClass("wtf-approval-peek")
		p.SetMarginStart(10 * (i + 1))
		p.SetMarginEnd(10 * (i + 1))
		d.root.Append(p)
	}
	d.topID = ask.ap.ID
}

func (d *approvalDeck) card(st bridge.Status, ap bridge.Approval) gtk.Widgetter {
	a := d.w.app
	kind := requestKind(ap)
	box := gtk.NewBox(gtk.OrientationVertical, 4)
	box.AddCSSClass("wtf-approval-card")
	if kind == "question" {
		box.AddCSSClass("question")
	}
	box.SetSizeRequest(deckWidth, -1)

	head := gtk.NewBox(gtk.OrientationHorizontal, 6)
	chip := pill(kind, "wtf-side-pill")
	chip.AddCSSClass("wtf-request-chip-" + kind)
	chip.SetVAlign(gtk.AlignCenter)
	head.Append(chip)
	who, _ := a.agentTitle(st)
	if ap.Subagent != "" {
		who += " · " + ap.Subagent
	}
	name := gtk.NewLabel(who)
	name.AddCSSClass("heading")
	name.SetEllipsize(pango.EllipsizeEnd)
	name.SetXAlign(0)
	head.Append(name)
	verb := gtk.NewLabel(requestVerb(ap))
	verb.AddCSSClass("dim-label")
	verb.SetEllipsize(pango.EllipsizeEnd)
	verb.SetXAlign(0)
	verb.SetHExpand(true)
	head.Append(verb)
	if n := len(d.shown); n > 1 {
		c := gtk.NewLabel(fmt.Sprintf("1 of %d", n))
		c.AddCSSClass("dim-label")
		c.AddCSSClass("wtf-approval-count")
		head.Append(c)
	}
	hide := gtk.NewButtonFromIconName("pan-down-symbolic")
	hide.AddCSSClass("flat")
	hide.SetFocusOnClick(false)
	hide.SetTooltipText("Collapse to a count; a new request opens it again")
	hide.ConnectClicked(func() {
		d.collapsed = true
		d.refresh()
	})
	head.Append(hide)
	box.Append(head)

	if ap.Form {
		d.form(box, st, ap)
		return box
	}
	for _, m := range approvalBody(a, ap, 8) {
		t := wrapLabel(m.text, "wtf-side-detail")
		for _, c := range m.classes {
			t.AddCSSClass(c)
		}
		t.SetSelectable(true)
		t.SetFocusable(false)
		t.SetMaxWidthChars(52)
		box.Append(t)
	}
	acts := approvalActions(d.w, st, ap, true)
	row := gtk.NewBox(gtk.OrientationHorizontal, 4)
	if kind == "question" {
		// One under another: an option's label can be long.
		row.SetOrientation(gtk.OrientationVertical)
		row.SetHAlign(gtk.AlignStart)
	}
	row.SetMarginTop(3)
	for _, act := range acts {
		row.Append(actionButton(act))
	}
	box.Append(row)
	return box
}

// form: several questions, or several choices, answered at once: each
// question with its options (radio buttons, or checkboxes when it takes
// several) and, for Claude, an entry for your own words; then Submit, which
// needs every question answered.
func (d *approvalDeck) form(box *gtk.Box, st bridge.Status, ap bridge.Approval) {
	a := d.w.app
	id := ap.ID
	if len(d.picks[id]) != len(ap.Questions) {
		d.picks[id] = make([]bridge.Pick, len(ap.Questions))
	}
	submit := "Submit"
	if k := keyOf("approval.allow"); k != "" {
		submit += " " + k
	}
	send := gtk.NewButtonWithLabel(submit)
	send.AddCSSClass("wtf-side-action")
	send.AddCSSClass("suggested-action")
	send.SetFocusOnClick(false)
	send.SetTooltipText("Send these answers to " + agentProduct(st) + ", as answering in its own dialog")
	update := func() { send.SetSensitive(d.complete(ap)) }
	send.ConnectClicked(func() {
		if d.complete(ap) {
			d.submit(ap)
		}
	})
	for i, q := range ap.Questions {
		title := q.Question
		if q.Header != "" {
			title = q.Header + " · " + title
		}
		t := wrapLabel(a.settings.text(title), "wtf-side-detail")
		t.AddCSSClass("heading")
		t.SetMaxWidthChars(52)
		t.SetMarginTop(4)
		box.Append(t)
		var checks []*gtk.CheckButton
		var entry *gtk.Entry
		setting := false
		for _, o := range q.Options {
			c := gtk.NewCheckButtonWithLabel(a.settings.text(o))
			c.SetFocusOnClick(false)
			c.SetTooltipText(a.settings.text(o))
			if !q.Multi && len(checks) > 0 {
				c.SetGroup(checks[0])
			}
			c.SetActive(slices.Contains(d.picks[id][i].Options, o))
			c.ConnectToggled(func() {
				p := d.picks[id]
				if setting || i >= len(p) {
					return
				}
				var chosen []string
				for j, x := range checks {
					if x.Active() {
						chosen = append(chosen, q.Options[j])
					}
				}
				p[i].Options = chosen
				if len(chosen) > 0 && entry != nil && entry.Text() != "" {
					// A choice instead of your own words.
					p[i].Text = ""
					setting = true
					entry.SetText("")
					setting = false
				}
				update()
			})
			checks = append(checks, c)
			box.Append(c)
		}
		if ap.FreeText {
			entry = gtk.NewEntry()
			if len(q.Options) == 0 {
				entry.SetPlaceholderText("Your answer")
			} else {
				entry.SetPlaceholderText("Or in your own words")
			}
			entry.SetText(d.picks[id][i].Text)
			entry.ConnectChanged(func() {
				p := d.picks[id]
				if setting || i >= len(p) {
					return
				}
				p[i].Text = entry.Text()
				if strings.TrimSpace(p[i].Text) != "" && len(p[i].Options) > 0 {
					// Your own words instead of a choice.
					p[i].Options = nil
					setting = true
					for _, c := range checks {
						c.SetActive(false)
					}
					setting = false
				}
				update()
			})
			box.Append(entry)
		}
	}
	session := st.Session
	row := gtk.NewBox(gtk.OrientationHorizontal, 4)
	row.SetMarginTop(3)
	row.Append(send)
	row.Append(actionButton(sideAction{label: "Go to Agent", tip: "Show its pane", run: func() { a.goToAgent(session) }}))
	box.Append(row)
	update()
}

// pill: collapsed, "3 waiting for you"; a click shows the card again.
func (d *approvalDeck) pill() gtk.Widgetter {
	b := gtk.NewButtonWithLabel(fmt.Sprintf("%d waiting for you", len(d.shown)))
	b.AddCSSClass("wtf-approval-pill")
	question := true
	for _, ask := range d.shown {
		if len(ask.ap.Questions) == 0 {
			question = false
		}
	}
	if question {
		b.AddCSSClass("question")
	}
	b.SetFocusOnClick(false)
	b.SetTooltipText("What your agents are waiting on you for: click to show")
	b.ConnectClicked(func() {
		d.collapsed = false
		d.refresh()
	})
	return b
}

func (d *approvalDeck) close() {
	if d.stop != nil {
		d.stop()
	}
}

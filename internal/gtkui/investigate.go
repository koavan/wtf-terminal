//go:build linux

package gtkui

import (
	"fmt"
	"strings"
	"time"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/dayanraj/wtf/internal/ai"
	"github.com/dayanraj/wtf/internal/investigate"
	"github.com/dayanraj/wtf/internal/uicore"
)

func init() {
	registerSidebarTab(sidebarTabInfo{tab: uicore.SidebarInvestigate, label: "Investigate (AI)", icon: "system-search-symbolic",
		cmd: "ai.investigate", make: func(w *Window) sidePaneTab { return newInvestigateTab(w) }})
	extraCSS = append(extraCSS, `
.wtf-inv-step { font-family: monospace; font-size: 0.9em; padding: 2px 4px; min-height: 0; }
.wtf-inv-refused { color: @warning_color; font-family: monospace; font-size: 0.9em; }
.wtf-inv-ok { color: @success_color; }
.wtf-inv-await { color: @warning_color; font-weight: bold; }
.wtf-inv-finding { padding: 4px 4px 0; }
.wtf-inv-bad { color: @error_color; }
`)
}

// Incident investigator (#27): ask why something is broken; the AI
// investigates with read-only commands that internal/investigate checks
// against its allowlist. Each one waits for the user's Run (or Skip) before
// it's typed into the shell they asked from, so nothing runs unasked and
// each is a normal command block. It ends with a finding citing the
// steps, and a fix to type at the prompt (never run). The run lives in
// investigation, per window, so it goes on while the side pane shows
// another tab; investigateTab only shows it. Like the macOS app's
// Investigation and InvestigateView.

type invState int

const (
	invRunning invState = iota
	invFinished
	invTimedOut
	invFailed
	// Waiting for the user's Run or Skip.
	invAwaiting
	invDeclined
)

type invEntry struct {
	// "note", "step" or "refused".
	kind string
	text string
	// A step's number, line, state, exit code and block.
	n     int
	line  string
	state invState
	exit  int
	block int
	// A refusal's reason; a step's purpose.
	reason  string
	purpose string
}

type investigation struct {
	w        *Window
	id       string
	question string
	paneID   int
	context  string
	entries  []invEntry
	done     *investigate.Done
	steps    int
	offs     []func()
	onChange func()
	preview  *aiPreview
	// The command waiting for the user's Run or Skip.
	pending *investigate.Run
}

var (
	investigations = map[*Window]*investigation{}
	invSeq         int
)

// stepTimeout: a command still running then gets Ctrl-C.
const invStepTimeout = 60 * time.Second

func (inv *investigation) busy() bool { return inv.done == nil }

func (inv *investigation) changed() {
	if inv.onChange != nil {
		inv.onChange()
	}
}

// invContext is what the focused shell is set up for: "tab · kube … · aws …".
func (w *Window) invContext() string {
	ps := w.model.ActivePane()
	if ps == nil {
		return ""
	}
	parts := []string{"this shell"}
	if t, ok := w.model.TabOf(ps.ID); ok {
		parts[0] = w.app.settings.text(w.model.Title(t))
	}
	if c := w.infra[ps.ID]; c != nil {
		if c.KubeContext != "" {
			k := "kube " + c.KubeContext
			if c.KubeNamespace != "" {
				k += "/" + c.KubeNamespace
			}
			parts = append(parts, k)
		}
		if c.AWSProfile != "" {
			a := "aws " + c.AWSProfile
			if c.AWSRegion != "" {
				a += " " + c.AWSRegion
			}
			parts = append(parts, a)
		}
	}
	return strings.Join(parts, " · ")
}

// showInvestigate opens the Investigate tab with the question focused.
func (w *Window) showInvestigate() {
	if !w.aiEnabled() {
		w.toast("Set up an AI provider in Settings first (Ctrl+,)")
		return
	}
	w.sidebarState.Tab, w.sidebarState.Open = uicore.SidebarInvestigate, true
	w.layoutSidebar()
	if t, ok := w.sidePane().(*investigateTab); ok {
		t.entry.GrabFocus()
	}
}

// startInvestigation starts one in the focused pane's shell; nil (after a
// toast) when it can't.
func (w *Window) startInvestigation(question string) *investigation {
	question = strings.TrimSpace(question)
	if question == "" {
		return nil
	}
	if cur := investigations[w]; cur != nil && cur.busy() {
		w.toast("An investigation is already running: stop it first")
		return nil
	}
	ps, p := w.model.ActivePane(), w.active()
	switch {
	case ps == nil || p == nil || !ps.IsShell:
		w.toast("Focus a terminal pane: its shell runs the commands")
		return nil
	case !ps.Integrated:
		w.toast("This shell has no shell integration, so wtf can't tell when a command ends")
		return nil
	case ps.Running:
		w.toast("The shell is busy: finish the running program first")
		return nil
	}
	invSeq++
	inv := &investigation{w: w, id: fmt.Sprintf("gtk-inv-%d", invSeq), question: question, paneID: ps.ID, context: w.invContext()}
	investigations[w] = inv
	q := investigate.Question{Text: question, Env: ps.Env, Cwd: ps.Cwd}
	for _, b := range p.tracker.Recent(3) {
		r := investigate.Recent{Command: b.Text}
		if b.ExitCode != nil {
			r.ExitCode = *b.ExitCode
		}
		if b.Output != nil {
			r.Output = *b.Output
		}
		q.Recent = append(q.Recent, r)
	}
	svc, aisvc := w.app.be.Investigate, w.app.be.AI
	go func() {
		req, err := svc.Request(q)
		var out ai.Outgoing
		if err == nil {
			out, err = aisvc.Preview(req)
		}
		onMainAsync(func() {
			switch {
			case inv.done != nil:
			case err != nil:
				inv.finish(investigate.Done{ID: inv.id, Error: err.Error()})
			case out.FirstSend:
				inv.preview = showAIPreview(w, nil, out, true, func(send bool) {
					inv.preview = nil
					if send {
						inv.begin(q, true)
					} else {
						inv.finish(investigate.Done{ID: inv.id, Cancelled: true})
					}
				})
			default:
				inv.begin(q, false)
			}
		})
	}()
	return inv
}

func (inv *investigation) begin(q investigate.Question, shown bool) {
	ev := inv.w.app.events
	inv.offs = append(inv.offs,
		ev.on(investigate.EventText, func(data any) {
			if t, ok := data.(investigate.Text); ok && t.ID == inv.id {
				inv.note(t.Text)
			}
		}),
		ev.on(investigate.EventRun, func(data any) {
			if r, ok := data.(investigate.Run); ok && r.ID == inv.id {
				inv.perform(r)
			}
		}),
		ev.on(investigate.EventRefused, func(data any) {
			if r, ok := data.(investigate.Refusal); ok && r.ID == inv.id {
				inv.entries = append(inv.entries, invEntry{kind: "refused", line: strings.Join(r.Argv, " "), reason: r.Reason})
				inv.changed()
			}
		}),
		ev.on(investigate.EventDone, func(data any) {
			if d, ok := data.(investigate.Done); ok && d.ID == inv.id {
				inv.finish(d)
			}
		}))
	svc, id := inv.w.app.be.Investigate, inv.id
	go func() {
		err := svc.Start(id, q, shown)
		if err != nil {
			onMainAsync(func() { inv.finish(investigate.Done{ID: id, Error: err.Error()}) })
		}
	}()
	inv.changed()
}

func (inv *investigation) stop() {
	if !inv.busy() {
		return
	}
	if inv.preview != nil {
		p := inv.preview
		inv.preview = nil
		p.answered = true
		p.win.Close()
		inv.finish(investigate.Done{ID: inv.id, Cancelled: true})
		return
	}
	svc, id := inv.w.app.be.Investigate, inv.id
	go svc.Stop(id)
}

func (inv *investigation) finish(d investigate.Done) {
	if inv.done != nil {
		return
	}
	inv.done = &d
	if p := inv.pending; p != nil {
		inv.pending = nil
		inv.setStep(p.Step, invDeclined, 0, 0)
	}
	// The finding streamed in as text too: show it once.
	if n := len(inv.entries); n > 0 && inv.entries[n-1].kind == "note" && d.Finding != "" &&
		strings.TrimSpace(inv.entries[n-1].text) == strings.TrimSpace(d.Finding) {
		inv.entries = inv.entries[:n-1]
	}
	for _, off := range inv.offs {
		off()
	}
	inv.offs = nil
	inv.changed()
}

func (inv *investigation) note(text string) {
	if n := len(inv.entries); n > 0 && inv.entries[n-1].kind == "note" {
		inv.entries[n-1].text += text
	} else {
		inv.entries = append(inv.entries, invEntry{kind: "note", text: text})
	}
	inv.changed()
}

func (inv *investigation) setStep(n int, st invState, exit, block int) {
	for i := range inv.entries {
		if e := &inv.entries[i]; e.kind == "step" && e.n == n {
			e.state, e.exit = st, exit
			if block > 0 {
				e.block = block
			}
		}
	}
	inv.changed()
}

func (inv *investigation) report(step int, r investigate.StepResult) {
	svc, id := inv.w.app.be.Investigate, inv.id
	go svc.Result(id, step, r)
}

// perform types a step's command into the shell and reports its block.
// perform shows the command and waits for Run or Skip: nothing runs unasked.
func (inv *investigation) perform(r investigate.Run) {
	inv.steps = r.Step
	inv.pending = &r
	inv.entries = append(inv.entries, invEntry{kind: "step", n: r.Step, line: r.Line, purpose: r.Purpose, state: invAwaiting})
	inv.changed()
}

// skip declines the waiting command.
func (inv *investigation) skip() {
	r := inv.pending
	if r == nil {
		return
	}
	inv.pending = nil
	inv.setStep(r.Step, invDeclined, 0, 0)
	inv.report(r.Step, investigate.StepResult{Declined: true})
}

// approve runs the waiting command in the shell.
func (inv *investigation) approve() {
	if inv.pending == nil {
		return
	}
	r := *inv.pending
	inv.pending = nil
	w := inv.w
	inv.setStep(r.Step, invRunning, 0, 0)
	// The shell reports it's back at its prompt a moment after the last
	// command's block ends: wait for that, briefly.
	settle := time.Now().Add(5 * time.Second)
	coreglib.TimeoutAdd(100, func() bool {
		if inv.done != nil {
			return false
		}
		ps, p := w.model.Panes[inv.paneID], w.panes[inv.paneID]
		idle := ps != nil && p != nil && ps.IsShell && !ps.Running
		switch {
		case idle:
			inv.run(r, p)
			return false
		case time.Now().After(settle) || ps == nil || p == nil:
			inv.setStep(r.Step, invFailed, 0, 0)
			inv.report(r.Step, investigate.StepResult{Error: "The shell isn't at a prompt any more (a program is running, or the pane closed)"})
			return false
		}
		return true
	})
}

// run types the step's line and waits for its block.
func (inv *investigation) run(r investigate.Run, p *Pane) {
	w := inv.w
	before, _ := p.tracker.LastFinished()
	p.write(uicore.ClearLine + r.Line + "\r")
	deadline := time.Now().Add(invStepTimeout)
	interrupted := false
	coreglib.TimeoutAdd(150, func() bool {
		if inv.done != nil || w.panes[inv.paneID] != p {
			return false
		}
		if id, ok := p.tracker.LastFinished(); ok && id != before && !p.feed.busy() {
			if b, _ := p.tracker.Info(id); b.ExitCode != nil {
				out, _ := p.tracker.Output(id)
				st := invFinished
				if interrupted {
					st = invTimedOut
				}
				inv.setStep(r.Step, st, *b.ExitCode, id)
				inv.report(r.Step, investigate.StepResult{ExitCode: *b.ExitCode, Output: out, TimedOut: interrupted, Block: id})
				return false
			}
		}
		if time.Now().After(deadline) {
			if interrupted {
				inv.setStep(r.Step, invFailed, 0, 0)
				inv.report(r.Step, investigate.StepResult{Error: "A command didn't stop after Ctrl-C"})
				return false
			}
			p.write("\x03")
			interrupted = true
			deadline = time.Now().Add(5 * time.Second)
		}
		return true
	})
}

func (inv *investigation) reveal(n int) {
	p := inv.w.panes[inv.paneID]
	for _, e := range inv.entries {
		if e.kind == "step" && e.n == n && e.block > 0 && p != nil {
			if !p.revealCommand(e.block) {
				inv.w.toast("That command has left the scrollback")
			}
		}
	}
}

func (inv *investigation) typeFix() {
	p := inv.w.panes[inv.paneID]
	if inv.done == nil || inv.done.Fix == "" || p == nil {
		return
	}
	switch p.typeAtPrompt(inv.done.Fix) {
	case shellBusy:
		inv.w.toast("The shell is busy — finish the running program first")
	case copiedInstead:
		inv.w.toast("The fix has several lines and this shell would run them as typed: copied instead")
	}
}

func stateText(e invEntry) string {
	switch e.state {
	case invAwaiting:
		return "  (waiting for you)"
	case invDeclined:
		return "  (skipped)"
	case invRunning:
		return "  (running)"
	case invTimedOut:
		return "  (timed out)"
	case invFailed:
		return "  (couldn't run)"
	}
	if e.exit != 0 {
		return fmt.Sprintf("  (exit %d)", e.exit)
	}
	return ""
}

func (inv *investigation) transcript() string {
	lines := []string{"Question: " + inv.question, "In: " + inv.context, ""}
	for _, e := range inv.entries {
		switch e.kind {
		case "note":
			lines = append(lines, strings.TrimSpace(e.text))
		case "step":
			lines = append(lines, fmt.Sprintf("[%d] $ %s%s", e.n, e.line, stateText(e)))
		case "refused":
			lines = append(lines, "refused: "+e.line+" — "+e.reason)
		}
	}
	if inv.done != nil && inv.done.Finding != "" {
		lines = append(lines, "", inv.done.Finding)
	}
	return strings.Join(lines, "\n")
}

// ── The tab ──────────────────────────────────────────────────────────────

type investigateTab struct {
	w        *Window
	root     *gtk.Box
	header   *paneHeader
	newTool  *gtk.Button
	entry    *gtk.Entry
	goButton *gtk.Button
	context  *gtk.Label
	list     *gtk.Box
	scroll   *gtk.ScrolledWindow
	status   *gtk.Label
	fix      *gtk.Button
	copy     *gtk.Button
	run      *gtk.Button
	skip     *gtk.Button
	inv      *investigation
	// A scroll to the end is queued.
	following bool
}

func newInvestigateTab(w *Window) *investigateTab {
	t := &investigateTab{w: w}
	t.header = newPaneHeader("Investigate")
	t.newTool = t.header.tool("document-new-symbolic", "New investigation", t.newInvestigation)
	t.entry = gtk.NewEntry()
	t.entry.SetHExpand(true)
	t.entry.SetPlaceholderText("Why is checkout-api crashlooping in staging?")
	t.entry.ConnectActivate(t.goClicked)
	t.goButton = gtk.NewButtonWithLabel("Investigate")
	t.goButton.ConnectClicked(t.goClicked)
	ask := gtk.NewBox(gtk.OrientationHorizontal, 6)
	ask.Append(t.entry)
	ask.Append(t.goButton)
	t.context = wrapLabel("", "wtf-side-note")
	note := wrapLabel("The AI may ask to run read-only commands (kubectl get/describe/logs, aws describe-*, helm history, dig, curl -I…) in this shell. Each one waits for your Run; nothing runs unasked. Fixes are typed at the prompt, never run.", "wtf-side-note")
	note.AddCSSClass("dim-label")
	t.list = gtk.NewBox(gtk.OrientationVertical, 6)
	t.scroll = gtk.NewScrolledWindow()
	t.scroll.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	t.scroll.SetVExpand(true)
	t.scroll.SetChild(t.list)

	t.status = wrapLabel("", "wtf-side-note")
	t.status.AddCSSClass("dim-label")
	t.copy = gtk.NewButtonWithLabel("Copy")
	t.copy.ConnectClicked(func() {
		if t.inv != nil {
			t.w.win.Clipboard().SetText(t.w.app.settings.text(t.inv.transcript()))
			t.w.toast("Copied the investigation")
		}
	})
	t.fix = gtk.NewButtonWithLabel("Type Fix at Prompt")
	t.fix.AddCSSClass("suggested-action")
	t.fix.ConnectClicked(func() {
		if t.inv != nil {
			t.inv.typeFix()
		}
	})
	t.run = gtk.NewButtonWithLabel("Run")
	t.run.AddCSSClass("suggested-action")
	t.run.SetTooltipText("Types this command into the shell and runs it")
	t.run.ConnectClicked(func() {
		if t.inv != nil {
			t.inv.approve()
		}
	})
	t.skip = gtk.NewButtonWithLabel("Skip")
	t.skip.SetTooltipText("Tells the AI you declined; it continues without it")
	t.skip.ConnectClicked(func() {
		if t.inv != nil {
			t.inv.skip()
		}
	})
	buttons := gtk.NewBox(gtk.OrientationHorizontal, 6)
	spacer := gtk.NewBox(gtk.OrientationHorizontal, 0)
	spacer.SetHExpand(true)
	buttons.Append(t.copy)
	buttons.Append(t.skip)
	buttons.Append(spacer)
	buttons.Append(t.run)
	buttons.Append(t.fix)
	body := gtk.NewBox(gtk.OrientationVertical, 6)
	body.SetMarginStart(8)
	body.SetMarginEnd(8)
	body.SetMarginBottom(8)
	for _, wd := range []gtk.Widgetter{ask, t.context, note, t.scroll, t.status, buttons} {
		body.Append(wd)
	}
	body.SetVExpand(true)
	t.root = gtk.NewBox(gtk.OrientationVertical, 4)
	t.root.Append(t.header.root)
	t.root.Append(body)
	t.attach(investigations[w])
	return t
}

func (t *investigateTab) widget() gtk.Widgetter { return t.root }
func (t *investigateTab) commandFinished()      {}

func (t *investigateTab) paneChanged() {
	if t.inv == nil || !t.inv.busy() {
		t.render()
	}
}

func (t *investigateTab) close() {
	if t.inv != nil {
		t.inv.onChange = nil
	}
}

func (t *investigateTab) attach(inv *investigation) {
	if t.inv != nil {
		t.inv.onChange = nil
	}
	t.inv = inv
	if inv != nil {
		inv.onChange = t.render
		t.entry.SetText(inv.question)
	}
	t.render()
}

func (t *investigateTab) goClicked() {
	if t.inv != nil && t.inv.busy() {
		t.inv.stop()
		return
	}
	if inv := t.w.startInvestigation(t.entry.Text()); inv != nil {
		t.attach(inv)
	}
}

func (t *investigateTab) newInvestigation() {
	if t.inv != nil && t.inv.busy() {
		return
	}
	delete(investigations, t.w)
	t.entry.SetText("")
	t.attach(nil)
	t.entry.GrabFocus()
}

func (t *investigateTab) render() {
	inv := t.inv
	busy := inv != nil && inv.busy()
	if busy {
		t.goButton.SetLabel("Stop")
	} else {
		t.goButton.SetLabel("Investigate")
	}
	t.entry.SetEditable(!busy)
	t.newTool.SetVisible(inv != nil && !busy)
	if inv != nil {
		t.context.SetText("In " + inv.context)
	} else {
		t.context.SetText(t.w.invContext())
	}
	fix := ""
	if inv != nil && inv.done != nil {
		fix = inv.done.Fix
	}
	t.fix.SetVisible(fix != "")
	t.fix.SetTooltipText("Types " + fix + " at the prompt, without Enter")
	t.copy.SetVisible(inv != nil && !busy)
	waiting := inv != nil && inv.pending != nil
	t.run.SetVisible(waiting)
	t.skip.SetVisible(waiting)
	if waiting {
		t.run.SetLabel(fmt.Sprintf("Run [%d]", inv.pending.Step))
	}
	t.status.SetText(t.statusText())

	for c := t.list.FirstChild(); c != nil; c = t.list.FirstChild() {
		t.list.Remove(c)
	}
	text := t.w.app.settings.text
	if inv == nil {
		t.list.Append(wrapLabel("Ask about something that's broken. The AI runs read-only commands in the focused shell, then gives a finding that cites them.", "wtf-side-note"))
		return
	}
	for _, e := range inv.entries {
		switch e.kind {
		case "note":
			if s := strings.TrimSpace(e.text); s != "" {
				l := wrapLabel(text(s), "wtf-side-note")
				l.AddCSSClass("dim-label")
				l.SetSelectable(true)
				t.list.Append(l)
			}
		case "step":
			glyph := map[invState]string{invRunning: "…", invFinished: "✓", invTimedOut: "⏱", invFailed: "✗", invAwaiting: "?", invDeclined: "–"}[e.state]
			if e.state == invFinished && e.exit != 0 {
				glyph = "✗"
			}
			b := gtk.NewButton()
			b.AddCSSClass("flat")
			b.AddCSSClass("wtf-inv-step")
			l := gtk.NewLabel(fmt.Sprintf("%s [%d] %s%s", glyph, e.n, text(e.line), stateText(e)))
			l.SetXAlign(0)
			l.SetWrap(true)
			l.SetMaxWidthChars(1)
			l.SetHExpand(true)
			switch {
			case e.state == invFinished && e.exit == 0:
				l.AddCSSClass("wtf-inv-ok")
			case e.state == invAwaiting:
				l.AddCSSClass("wtf-inv-await")
			case e.state == invDeclined:
				l.AddCSSClass("dim-label")
			case e.state != invRunning:
				l.AddCSSClass("wtf-inv-bad")
			}
			if e.block > 0 {
				b.SetChild(l)
				b.SetTooltipText("Show it in the terminal")
				n := e.n
				b.ConnectClicked(func() { inv.reveal(n) })
				t.list.Append(b)
			} else {
				// Nothing to show yet: plain text, not a dimmed button.
				l.AddCSSClass("wtf-inv-step")
				t.list.Append(l)
			}
			if e.state == invAwaiting && e.purpose != "" {
				l := wrapLabel("Why: "+e.purpose, "wtf-side-note")
				l.AddCSSClass("dim-label")
				t.list.Append(l)
			}
		case "refused":
			l := wrapLabel("⊘ refused: "+text(e.line)+"\n"+e.reason, "wtf-inv-refused")
			t.list.Append(l)
		}
	}
	if d := inv.done; d != nil {
		if d.Error != "" {
			l := wrapLabel(d.Error, "wtf-side-note")
			l.AddCSSClass("error")
			t.list.Append(l)
		}
		if d.Finding != "" {
			l := wrapLabel(text(d.Finding), "wtf-inv-finding")
			l.SetSelectable(true)
			t.list.Append(l)
		}
	}
	t.follow()
}

// follow scrolls to the newest entry once the list has its new size.
func (t *investigateTab) follow() {
	if t.following {
		return
	}
	t.following = true
	coreglib.TimeoutAdd(60, func() bool {
		t.following = false
		adj := t.scroll.VAdjustment()
		adj.SetValue(adj.Upper() - adj.PageSize())
		return false
	})
}

func (t *investigateTab) statusText() string {
	inv := t.inv
	if inv == nil {
		return ""
	}
	d := inv.done
	if d == nil {
		if p := inv.pending; p != nil {
			return fmt.Sprintf("Waiting for you: Run [%d] types it into the shell, Skip declines it. Nothing runs until you choose.", p.Step)
		}
		return fmt.Sprintf("Step %d of up to %d · the shell is in use until it ends", inv.steps, investigate.MaxSteps)
	}
	var parts []string
	switch {
	case d.Cancelled:
		parts = append(parts, "Stopped")
	case d.Limit == "steps":
		parts = append(parts, fmt.Sprintf("Stopped at the %d-command limit", investigate.MaxSteps))
	case d.Limit == "tokens":
		parts = append(parts, "Stopped at the token limit")
	}
	ran := d.Steps - d.Declined
	cmds := "commands run"
	if ran == 1 {
		cmds = "command run"
	}
	if d.Declined > 0 {
		cmds += fmt.Sprintf(", %d skipped", d.Declined)
	}
	parts = append(parts, fmt.Sprintf("%d %s", ran, cmds), aiSummary(ai.Done{Millis: d.Millis}))
	if d.Usage.Counted {
		parts = append(parts, fmt.Sprintf("%d tokens", d.Usage.InputTokens+d.Usage.OutputTokens))
	}
	if d.Usage.Cost > 0 {
		parts = append(parts, fmt.Sprintf("$%.4f", d.Usage.Cost))
	}
	return strings.Join(parts, " · ")
}

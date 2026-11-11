//go:build linux

package gtkui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/dayanraj/wtf/internal/cmdhistory"
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
// the window) ran, newest first, with how they went; or Saved: every
// shell's, across windows and restarts, with the Kubernetes and AWS context
// each ran in (#17), filtered by it. Click to put one at the prompt,
// double-click to run it again. Like the macOS app's HistoryView.
type historyTab struct {
	w     *Window
	root  *gtk.Box
	scope [3]*gtk.ToggleButton
	trash *gtk.Button
	// Saved: ask Claude about it (#31).
	ask    *gtk.Button
	filter *gtk.SearchEntry
	list   *sideList
	shown  []uicore.HistoryEntry
	off    func()

	// Saved: the filters (time, Kubernetes, AWS: their values beside the
	// dropdowns' labels) and what Search returned.
	filters                *gtk.Box
	timeDrop, kube, aws    *gtk.DropDown
	kubeValues, awsValues  []string
	since                  int
	kubeSel, awsSel        string
	filling                bool
	result                 *cmdhistory.Result
	loading, again, closed bool
}

func newHistoryTab(w *Window) *historyTab {
	h := &historyTab{w: w}
	bar := gtk.NewBox(gtk.OrientationHorizontal, 4)
	bar.AddCSSClass("wtf-pane-header")
	scope := gtk.NewBox(gtk.OrientationHorizontal, 0)
	scope.AddCSSClass("linked")
	scope.SetHExpand(true)
	scope.SetHomogeneous(true)
	scopes := []string{"pane", "all", "saved"}
	for i, label := range []string{"This pane", "All panes", "Saved"} {
		b := gtk.NewToggleButtonWithLabel(label)
		b.SetFocusOnClick(false)
		if i > 0 {
			b.SetGroup(h.scope[0])
		}
		want := scopes[i]
		b.ConnectToggled(func() {
			if b.Active() && w.sidebarState.HistoryScope != want {
				w.sidebarState.HistoryScope = want
				w.scheduleSave()
				h.rebuild()
			}
		})
		h.scope[i] = b
		scope.Append(b)
	}
	h.scope[max(0, slices.Index(scopes, w.sidebarState.HistoryScope))].SetActive(true)
	bar.Append(scope)
	h.trash = gtk.NewButtonFromIconName("user-trash-symbolic")
	h.trash.AddCSSClass("flat")
	h.trash.AddCSSClass("wtf-tool")
	h.trash.SetTooltipText("Clear history")
	h.trash.SetFocusOnClick(false)
	h.trash.ConnectClicked(h.clear)
	h.ask = gtk.NewButtonFromIconName("starred-symbolic")
	h.ask.AddCSSClass("flat")
	h.ask.AddCSSClass("wtf-tool")
	h.ask.SetTooltipText("Ask your history (Claude)")
	h.ask.SetFocusOnClick(false)
	h.ask.ConnectClicked(w.claudeHistory)
	bar.Append(h.ask)
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
		if e, ok := h.savedEntry(id); ok && h.saved() {
			h.insertText(e.Command)
		} else if e, ok := h.entry(id); ok {
			h.insert(e)
		}
	}
	h.list.onActivate = func(id string) {
		if e, ok := h.savedEntry(id); ok && h.saved() {
			h.runText(e.Command)
		} else if e, ok := h.entry(id); ok {
			h.runAgain(e)
		}
	}
	h.list.onMenu = func(ids []string, x, y float64) {
		if e, ok := h.savedEntry(ids[0]); ok && h.saved() {
			h.list.menu.show(x, y, h.savedMenu(e), nil)
		} else if e, ok := h.entry(ids[0]); ok {
			h.list.menu.show(x, y, h.menu(e), nil)
		}
	}
	h.timeDrop = gtk.NewDropDownFromStrings([]string{"Any time", "Today", "Last 7 days", "Last 30 days"})
	h.kube = gtk.NewDropDownFromStrings([]string{"Any Kubernetes"})
	h.aws = gtk.NewDropDownFromStrings([]string{"Any AWS"})
	h.filters = gtk.NewBox(gtk.OrientationHorizontal, 4)
	h.filters.SetHomogeneous(true)
	h.filters.SetMarginStart(6)
	h.filters.SetMarginEnd(6)
	for _, d := range []*gtk.DropDown{h.timeDrop, h.kube, h.aws} {
		d.AddCSSClass("flat")
		h.filters.Append(d)
	}
	h.timeDrop.NotifyProperty("selected", func() {
		if !h.filling {
			h.since = int(h.timeDrop.Selected())
			h.loadSaved()
		}
	})
	h.kube.NotifyProperty("selected", func() {
		if i := int(h.kube.Selected()); !h.filling && i < len(h.kubeValues) {
			h.kubeSel = h.kubeValues[i]
			h.loadSaved()
		}
	})
	h.aws.NotifyProperty("selected", func() {
		if i := int(h.aws.Selected()); !h.filling && i < len(h.awsValues) {
			h.awsSel = h.awsValues[i]
			h.loadSaved()
		}
	})
	h.root = gtk.NewBox(gtk.OrientationVertical, 4)
	h.root.Append(bar)
	h.root.Append(h.filter)
	h.root.Append(h.filters)
	h.root.Append(h.list.root)
	h.off = w.records.observe(h.rebuild)
	h.rebuild()
	return h
}

func (h *historyTab) widget() gtk.Widgetter { return h.root }
func (h *historyTab) paneChanged()          { h.rebuild() }
func (h *historyTab) commandFinished()      {}
func (h *historyTab) close() {
	h.closed = true
	h.off()
}

func (h *historyTab) all() bool   { return h.w.sidebarState.HistoryScope == "all" }
func (h *historyTab) saved() bool { return h.w.sidebarState.HistoryScope == "saved" }

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
	h.filters.SetVisible(h.saved())
	h.ask.SetVisible(h.saved())
	if h.saved() {
		h.filter.SetPlaceholderText("Search saved commands")
		h.loadSaved()
		return
	}
	h.filter.SetPlaceholderText("Filter commands")
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

func (h *historyTab) insert(e uicore.HistoryEntry) { h.insertText(e.Command) }

func (h *historyTab) insertText(cmd string) {
	if p := h.w.active(); p != nil {
		p.term.PasteText(cmd)
		p.term.GrabFocus()
	}
}

func (h *historyTab) runAgain(e uicore.HistoryEntry) { h.runText(e.Command) }

func (h *historyTab) runText(cmd string) {
	ps, p := h.w.model.ActivePane(), h.w.active()
	if ps == nil || p == nil || !ps.IsShell || ps.Running {
		h.w.toast("The shell is busy — finish the running program first")
		return
	}
	p.write(uicore.ClearLine + cmd + "\r")
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
	if h.saved() {
		h.forgetAllSaved()
		return
	}
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
		"It also goes from wtf's saved history. History from earlier sessions stays, and so do commands your shell already wrote to its history file.", count, plural(count, "command"))
	if len(busy) > 0 {
		body += "\n\n" + strings.Join(busy, "\n")
	}
	w.app.confirm(&w.win.Window, title, body, "Clear History", func() {
		shells := 0
		for _, ps := range targets {
			w.records.history.Clear(ps.ID)
			if ps.SessionID != "" {
				hist, sid, app := w.app.be.History, ps.SessionID, w.app
				go func() {
					_ = hist.ForgetSession(sid)
					onMainAsync(app.savedHistoryChanged)
				}()
			}
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

// Saved (#17)

func (h *historyTab) query() cmdhistory.Query {
	q := cmdhistory.Query{Text: h.filter.Text(), Limit: 500, KubeContext: h.kubeSel}
	now := time.Now()
	switch h.since {
	case 1:
		y, m, d := now.Date()
		q.Since = time.Date(y, m, d, 0, 0, 0, 0, now.Location()).Format(time.RFC3339)
	case 2:
		q.Since = now.AddDate(0, 0, -7).Format(time.RFC3339)
	case 3:
		q.Since = now.AddDate(0, 0, -30).Format(time.RFC3339)
	}
	if kind, value, ok := strings.Cut(h.awsSel, ":"); ok {
		switch kind {
		case "account":
			q.AWSAccount = value
		case "profile":
			q.AWSProfile = value
		default:
			q.AWSRegion = value
		}
	}
	return q
}

// loadSaved asks the backend off the main loop, only while Saved shows
// (once more if asked meanwhile).
func (h *historyTab) loadSaved() {
	if h.loading {
		h.again = true
		return
	}
	h.loading = true
	q, svc := h.query(), h.w.app.be.History
	go func() {
		r := svc.Search(q)
		onMainAsync(func() {
			h.loading = false
			if h.closed {
				return
			}
			if h.again {
				h.again = false
				h.loadSaved()
				return
			}
			if h.saved() {
				h.result = &r
				h.showSaved()
			}
		})
	}()
}

// fillDrops lists the saved contexts in the dropdowns, keeping the choices.
func (h *historyTab) fillDrops(r cmdhistory.Result) {
	h.filling = true
	defer func() { h.filling = false }()
	h.kubeValues = []string{""}
	labels := []string{"Any Kubernetes"}
	for _, f := range r.KubeContexts {
		h.kubeValues = append(h.kubeValues, f.Value)
		labels = append(labels, fmt.Sprintf("%s (%d)", f.Value, f.Count))
	}
	h.kube.SetModel(gtk.NewStringList(labels))
	h.kube.SetSelected(uint(max(0, slices.Index(h.kubeValues, h.kubeSel))))
	h.awsValues = []string{""}
	labels = []string{"Any AWS"}
	add := func(kind, prefix string, list []cmdhistory.Facet) {
		for _, f := range list {
			h.awsValues = append(h.awsValues, kind+":"+f.Value)
			labels = append(labels, fmt.Sprintf("%s%s (%d)", prefix, f.Value, f.Count))
		}
	}
	add("account", "Account ", r.AWSAccounts)
	add("profile", "Profile ", r.AWSProfiles)
	add("region", "Region ", r.AWSRegions)
	h.aws.SetModel(gtk.NewStringList(labels))
	h.aws.SetSelected(uint(max(0, slices.Index(h.awsValues, h.awsSel))))
	h.timeDrop.SetSelected(uint(h.since))
}

func (h *historyTab) showSaved() {
	r := h.result
	h.fillDrops(*r)
	h.trash.SetSensitive(r.Saving)
	section := sideSection{}
	if r.Total > len(r.Entries) {
		section.title = fmt.Sprintf("Newest %d of %d", len(r.Entries), r.Total)
	}
	for _, e := range r.Entries {
		section.rows = append(section.rows, h.savedRow(e))
	}
	if len(r.Entries) == 0 {
		switch {
		case !r.Saving:
			section.empty = "Saving is off: history-days is 0 in config.toml, so commands aren't saved. Set it to the days to keep (90 by default)."
		case h.filter.Text() != "" || h.since != 0 || h.kubeSel != "" || h.awsSel != "":
			section.empty = "No saved commands match."
		default:
			section.empty = fmt.Sprintf("No saved commands yet. Commands you run in any wtf shell are kept here for %d days, "+
				"with the Kubernetes context and AWS account they ran under. Secrets are hidden before they're saved.", r.Days)
		}
	}
	h.list.set([]sideSection{section})
}

func (h *historyTab) savedEntry(id string) (cmdhistory.Entry, bool) {
	if h.result == nil {
		return cmdhistory.Entry{}, false
	}
	for _, e := range h.result.Entries {
		if e.ID == id {
			return e, true
		}
	}
	return cmdhistory.Entry{}, false
}

func (h *historyTab) savedRow(e cmdhistory.Entry) sideRow {
	s := h.w.app.settings
	cmd := s.text(e.Command)
	cwd := uicore.PrettyPath(e.Cwd, h.w.app.home)
	at, _ := time.Parse(time.RFC3339, e.StartedAt)
	at = at.Local()
	r := sideRow{id: e.ID, title: cmd, mono: true,
		detail: fmt.Sprintf("%s · %s · %s", clockTime(at), uicore.Duration(time.Duration(e.DurationMs)*time.Millisecond), cwd),
		tip:    fmt.Sprintf("%s\nin %s\nstarted %s", cmd, cwd, clockTime(at)), dot: "dot-git-added"}
	if e.ExitCode != 0 {
		r.dot = "dot-git-deleted"
		r.pills = append(r.pills, sidePill{text: fmt.Sprintf("exit %d", e.ExitCode), class: "wtf-pill-fail"})
	}
	if e.KubeContext != "" {
		text, tip := e.KubeContext, "Kubernetes context "+e.KubeContext
		if e.KubeNamespace != "" {
			text += "/" + e.KubeNamespace
			tip += ", namespace " + e.KubeNamespace
		}
		r.pills = append(r.pills, sidePill{text: text, tip: tip})
	}
	acct := e.AWSAccount
	if acct == "" {
		acct = e.AWSProfile
	}
	if t := strings.TrimSpace(acct + " " + e.AWSRegion); t != "" {
		tip := "AWS profile " + e.AWSProfile
		if e.AWSAccount != "" {
			tip += ", account " + e.AWSAccount
		}
		if e.AWSRegion != "" {
			tip += ", region " + e.AWSRegion
		}
		r.pills = append(r.pills, sidePill{text: t, tip: tip})
	}
	return r
}

func (h *historyTab) savedMenu(e cmdhistory.Entry) []*menuItem {
	w := h.w
	return []*menuItem{
		item("Run Again", func() { h.runText(e.Command) }),
		item("Insert at Prompt", func() { h.insertText(e.Command) }),
		menuSection,
		item("Copy Command", func() {
			w.win.Clipboard().SetText(w.app.settings.text(e.Command))
			w.toast("Copied command")
		}),
		item("Copy Folder", func() {
			w.win.Clipboard().SetText(e.Cwd)
			w.toast("Copied folder")
		}),
		menuSection,
		item("Save as Snippet…", func() { w.saveAsSnippet(e.Command) }),
		item("Forget This Command", func() {
			svc, app, id := w.app.be.History, w.app, e.ID
			go func() {
				_ = svc.Forget(id)
				onMainAsync(app.savedHistoryChanged)
			}()
		}),
	}
}

func (h *historyTab) forgetAllSaved() {
	w := h.w
	w.app.confirm(&w.win.Window, "Forget all saved commands?",
		"Removes every command wtf saved, from every shell, with its context. Each shell's own history (↑ and Ctrl-R) stays as it is.",
		"Forget All", func() {
			svc, app := w.app.be.History, w.app
			go func() {
				err := svc.ForgetAll()
				onMainAsync(func() {
					if err != nil {
						w.toast("Couldn't forget them: " + err.Error())
						return
					}
					w.toast("Forgot the saved commands")
					app.savedHistoryChanged()
				})
			}()
		})
}

// savedHistoryChanged reloads every window's History tab showing Saved.
func (a *App) savedHistoryChanged() {
	for _, w := range a.windows {
		if h, ok := w.sidePane().(*historyTab); ok && h.saved() {
			h.loadSaved()
		}
	}
}

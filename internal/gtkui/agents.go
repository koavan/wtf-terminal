//go:build linux

package gtkui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"

	"github.com/dayanraj/wtf/internal/bridge"
	"github.com/dayanraj/wtf/internal/notify"
	"github.com/dayanraj/wtf/internal/state"
	"github.com/dayanraj/wtf/internal/uicore"
)

// Agents (#33): every Claude Code running in wtf's shells, from its hooks
// (bridge:status): Ask Claude's panes through wtf's plugin, a claude you
// start yourself through agent-status's settings. The tab badges, the
// Agents side tab and the picker read it here; wtf notifies when one needs
// you or finishes while you're looking elsewhere. As
// macos/Sources/Wtf/Agents.swift.

// What a tab's or an agent's badge shows: bridge's states, and done (it
// finished while you weren't looking).
const agentDone = "done"

type agentList struct {
	// Session → its Claude Code.
	all map[string]bridge.Status
	// Called after every change (the Agents tab while it shows).
	observers map[int]func()
	nextObs   int
	// The agents whose sub-agents the Agents tab shows (collapsed by
	// default; remembered while wtf runs).
	subagentsShown map[string]bool
	// When an agent notification last played its sound (agent-sound).
	lastSound time.Time
	// #38: background agents saved when wtf last quit, not yet resumed or
	// dismissed; and Claude Code session → the name its agent had, given
	// back when it reports in again after resuming.
	resumable    []state.Agent
	pendingNames map[string]string
	// Session → its agent's one notification: they don't pile up.
	notes      map[string]*agentNote
	noteSerial int
}

// agentNote is an agent's notification, its latest (a new one takes its
// place), taken away once it's out of date: answered, the agent moved on,
// or you look at it.
type agentNote struct {
	// The state it showed (agentState), and the approval it asks.
	state, approval string
	// notify-send's id, once known; or the one with Allow and Deny (#39).
	id    string
	asked *notify.Asked
}

func (n *agentNote) close() {
	if n.asked != nil {
		go n.asked.Close()
	} else if n.id != "" {
		go notify.Close(n.id)
	}
}

// clearNote takes an agent's notification away unless it still says
// unless ("": whatever it says).
func (l *agentList) clearNote(session, unless string) {
	n := l.notes[session]
	if n == nil || unless != "" && n.state == unless {
		return
	}
	delete(l.notes, session)
	n.close()
}

func newAgentList() *agentList {
	return &agentList{all: map[string]bridge.Status{}, observers: map[int]func(){}, subagentsShown: map[string]bool{},
		pendingNames: map[string]string{}, notes: map[string]*agentNote{}}
}

// changed tells the observers something they show changed.
func (l *agentList) changed() {
	for _, f := range l.observers {
		f()
	}
}

// toggleSubagents shows or hides an agent's sub-agents in the Agents tab.
func (l *agentList) toggleSubagents(session string) {
	if l.subagentsShown[session] {
		delete(l.subagentsShown, session)
	} else {
		l.subagentsShown[session] = true
	}
	for _, f := range l.observers {
		f()
	}
}

// observe calls f after every change until the returned stop is called.
func (l *agentList) observe(f func()) (stop func()) {
	id := l.nextObs
	l.nextObs++
	l.observers[id] = f
	return func() { delete(l.observers, id) }
}

func agentState(st bridge.Status) string {
	if st.State == bridge.StateIdle && st.Unseen {
		return agentDone
	}
	return st.State
}

// agentRank: most urgent first.
func agentRank(state string) int {
	switch state {
	case bridge.StateWaiting:
		return 0
	case agentDone:
		return 1
	case bridge.StateBusy:
		return 2
	}
	return 3
}

func agentLabel(state string) string {
	switch state {
	case bridge.StateWaiting:
		return "Needs you"
	case agentDone:
		return "Done"
	case bridge.StateBusy:
		return "Working"
	}
	return "Idle"
}

// inBackground: what a paused turn waits on ("2 in background").
func inBackground(n int) string { return fmt.Sprintf("%d in background", n) }

func agentProduct(st bridge.Status) string { return productName(st.Provider) }

// productName names an agent's provider: Codex, else Claude Code.
func productName(provider string) string {
	if provider == "codex" {
		return "Codex"
	}
	return "Claude Code"
}

// sessionProduct names the agent in a session (Claude Code when unknown).
func (a *App) sessionProduct(session string) string { return agentProduct(a.agents.all[session]) }

// sessionsProduct names what runs in sessions: Claude Code or Codex when
// they're all one, "" when they're mixed.
func (a *App) sessionsProduct(sessions []string) string {
	name := ""
	for i, s := range sessions {
		p := a.sessionProduct(s)
		if i > 0 && p != name {
			return ""
		}
		name = p
	}
	return name
}

// agentsRun: "Codex runs", "2 Claude Code sessions run" or, mixed, "3 agents
// (Claude Code and Codex) run", for n agents of product (see sessionsProduct).
func agentsRun(product string, n int) string {
	switch {
	case product == "":
		return fmt.Sprintf("%d agents (Claude Code and Codex) run", n)
	case n == 1:
		return product + " runs"
	}
	return fmt.Sprintf("%d %s sessions run", n, product)
}

// sorted: most urgent first, then the longest in that state.
func (l *agentList) sorted() []bridge.Status {
	out := make([]bridge.Status, 0, len(l.all))
	for _, st := range l.all {
		out = append(out, st)
	}
	slices.SortFunc(out, func(a, b bridge.Status) int {
		if ra, rb := agentRank(agentState(a)), agentRank(agentState(b)); ra != rb {
			return ra - rb
		}
		if a.Since != b.Since {
			if a.Since < b.Since {
				return -1
			}
			return 1
		}
		if a.Session < b.Session {
			return -1
		}
		return 1
	})
	return out
}

// approval is one permission prompt with the agent asking (#39).
type approval struct {
	st bridge.Status
	ap bridge.Approval
}

// approvals: every permission prompt waiting, oldest first.
func (l *agentList) approvals() []approval {
	var out []approval
	for _, st := range l.all {
		for _, ap := range st.Approvals {
			out = append(out, approval{st, ap})
		}
	}
	slices.SortFunc(out, func(a, b approval) int {
		if a.ap.Since != b.ap.Since {
			if a.ap.Since < b.ap.Since {
				return -1
			}
			return 1
		}
		return strings.Compare(a.ap.ID, b.ap.ID)
	})
	return out
}

// toolName is a tool as the inbox names it: wtf's own by what they do.
func toolName(tool string) string {
	switch tool {
	case "mcp__plugin_wtf_wtf__type_at_prompt":
		return "Type at your prompt"
	case "mcp__plugin_wtf_wtf__run_readonly":
		return "Run read-only"
	}
	if strings.HasPrefix(tool, "mcp__") {
		return tool[strings.LastIndex(tool, "__")+2:]
	}
	return tool
}

// nextApproval (Next Approval) goes to the agent of the oldest permission
// prompt after the focused pane's, round and round, with the Agents tab
// showing it.
func (w *Window) nextApproval() {
	var sessions []string
	for _, ask := range w.app.agents.approvals() {
		if !slices.Contains(sessions, ask.st.Session) {
			sessions = append(sessions, ask.st.Session)
		}
	}
	if len(sessions) == 0 {
		w.toast("No agent is asking for approval")
		return
	}
	current := ""
	if t, ok := w.model.Active(); ok {
		if ps := w.model.Panes[t.FocusedPaneID]; ps != nil {
			current = ps.SessionID
		}
	}
	next := sessions[(slices.Index(sessions, current)+1)%len(sessions)]
	w.app.goToAgent(next)
	// showSidebar toggles: only when the Agents tab isn't showing already.
	if aw := w.app.windowOf(next); aw != nil && !aw.background && !(aw.sidebarState.Open && aw.sidebarState.Tab == uicore.SidebarAgents) {
		aw.showSidebar(uicore.SidebarAgents)
	}
}

// answerApproval answers a permission prompt from the inbox; one answered
// in the pane meanwhile says so.
func (w *Window) answerApproval(id, decision string) {
	b := w.app.be.Bridge
	go func() {
		if err := b.Answer(id, decision); err != nil {
			coreglib.IdleAdd(func() { w.toast("Already answered in the pane") })
		}
	}()
}

// allowAlways allows an approval and saves its choice'th "don't ask again".
func (w *Window) allowAlways(id string, choice int) {
	b := w.app.be.Bridge
	go func() {
		if err := b.AllowAlways(id, choice); err != nil {
			coreglib.IdleAdd(func() { w.toast("Already answered in the pane") })
		}
	}()
}

// answerQuestion answers a question Claude asks with one of its options.
func (w *Window) answerQuestion(id, option string) {
	b := w.app.be.Bridge
	go func() {
		if err := b.AnswerQuestion(id, []string{option}); err != nil {
			coreglib.IdleAdd(func() { w.toast("Already answered in the pane") })
		}
	}()
}

// agentGroupName is the tab group agents' tabs join (agent-group).
const agentGroupName = "ungrouped agents"

// autoGroup (agent-group): an agent's tab joins its window's "ungrouped
// agents" group, made when needed. A tab already in a group stays there.
// Not in the background window, whose tabs aren't shown.
func (a *App) autoGroup(session string) {
	w := a.windowOf(session)
	if w == nil || w.background || !a.settings.config().AgentGroup {
		return
	}
	ps := w.model.PaneBySession(session)
	if ps == nil {
		return
	}
	t, ok := w.model.TabOf(ps.ID)
	if !ok || t.GroupID != 0 {
		return
	}
	w.model.JoinNamedGroup(t.ID, agentGroupName)
}

// autoUngroup: its Claude Code ended, so the tab leaves the "ungrouped agents" group,
// unless another agent runs in one of its panes.
func (w *Window) autoUngroup(session string) {
	if !w.app.settings.config().AgentGroup {
		return
	}
	ps := w.model.PaneBySession(session)
	if ps == nil {
		return
	}
	t, ok := w.model.TabOf(ps.ID)
	if !ok || t.GroupID == 0 || t.GroupID != w.model.GroupNamed(agentGroupName) {
		return
	}
	for _, id := range t.Root.Leaves() {
		if p := w.model.Panes[id]; id != ps.ID && p != nil && p.SessionID != "" {
			if _, ok := w.app.agents.all[p.SessionID]; ok {
				return
			}
		}
	}
	w.model.RemoveFromGroup(t.ID)
}

// listed: the order wtf first heard of them, for the Agents tab, so a row
// stays where it is whatever its state.
func (l *agentList) listed() []bridge.Status {
	out := make([]bridge.Status, 0, len(l.all))
	for _, st := range l.all {
		out = append(out, st)
	}
	slices.SortFunc(out, func(a, b bridge.Status) int {
		if a.Started != b.Started {
			if a.Started < b.Started {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Session, b.Session)
	})
	return out
}

// agentTitle is what an agent is called: the name you gave it, or its
// tab's title; tab is the tab's title when it has a name ("" otherwise).
func (a *App) agentTitle(st bridge.Status) (title, tab string) {
	t, _ := a.agentPlace(st.Session)
	if st.Name == "" {
		return t, ""
	}
	if t == a.settings.text(st.Name) {
		return st.Name, "" // the tab carries the same name
	}
	return st.Name, t
}

// renameAgent (Rename…): a name for the agent, shown in the Agents tab, the
// picker and its notifications, and as its tab's title. It stays with its
// shell.
func (w *Window) renameAgent(session string) {
	st, ok := w.app.agents.all[session]
	if !ok {
		return
	}
	tab, _ := w.app.agentPlace(session)
	f := newKubeForm(w, "Rename Agent", "Shown in the Agents tab, Go to Agent…, its notifications and as its tab's title. It stays with this shell: an agent you start there again gets it too. Leave it empty to use the tab's title.")
	field := f.entry(st.Name, tab)
	f.field("Name", field)
	f.button("ok", "Rename")
	bridgeSvc := w.app.be.Bridge
	f.d.ConnectResponse(func(id string) {
		if id == "ok" {
			name := field.Text()
			w.app.renameAgentTab(session, st.Name, name)
			go bridgeSvc.Rename(session, name)
		}
	})
	f.present(field)
}

// renameAgentTab gives an agent's tab its new name. Clearing the name puts
// the tab back to automatic, unless you've since renamed the tab yourself.
func (a *App) renameAgentTab(session, old, name string) {
	w := a.windowOf(session)
	if w == nil {
		return
	}
	ps := w.model.PaneBySession(session)
	if ps == nil {
		return
	}
	t, ok := w.model.TabOf(ps.ID)
	if !ok {
		return
	}
	if uicore.CleanTitle(name) != "" {
		w.model.Rename(t.ID, name)
	} else if old != "" && t.CustomTitle == uicore.CleanTitle(old) {
		w.model.Rename(t.ID, "")
	}
}

// agentTabState is a tab's badge: its most urgent agent ("" for none).
func (w *Window) agentTabState(leaves []int) string {
	found := ""
	for _, id := range leaves {
		ps := w.model.Panes[id]
		if ps == nil || ps.SessionID == "" {
			continue
		}
		st, ok := w.app.agents.all[ps.SessionID]
		if !ok {
			continue
		}
		if s := agentState(st); found == "" || agentRank(s) < agentRank(found) {
			found = s
		}
	}
	return found
}

// agentVisible: you're looking at the pane: its tab is in front in the
// active window.
func (w *Window) agentVisible(session string) bool {
	if !w.win.IsActive() {
		return false
	}
	t, ok := w.model.Active()
	if !ok {
		return false
	}
	ps := w.model.PaneBySession(session)
	return ps != nil && slices.Contains(t.Root.Leaves(), ps.ID)
}

// agentsSeen: the finished agents in front of you in w are no longer done.
func (w *Window) agentsSeen() {
	if !w.win.IsActive() {
		return
	}
	t, ok := w.model.Active()
	if !ok {
		return
	}
	for _, id := range t.Root.Leaves() {
		ps := w.model.Panes[id]
		if ps == nil {
			continue
		}
		// You're looking at it: its notification has nothing to add.
		w.app.agents.clearNote(ps.SessionID, "")
		if st, ok := w.app.agents.all[ps.SessionID]; ok && st.Unseen {
			go w.app.be.Bridge.Seen(st.Session)
		}
	}
}

func (a *App) agentStatus(st bridge.Status) {
	l := a.agents
	old, had := l.all[st.Session]
	// Approvals answered (here, in the pane, or gone with their agent)
	// take their notification away.
	if n := l.notes[st.Session]; n != nil && n.approval != "" &&
		!slices.ContainsFunc(st.Approvals, func(o bridge.Approval) bool { return o.ID == n.approval }) {
		l.clearNote(st.Session, "")
	}
	// So does one that no longer says how it is (working again, or quit).
	if st.State == bridge.StateEnded {
		l.clearNote(st.Session, "")
	} else {
		l.clearNote(st.Session, agentState(st))
	}
	if st.State == bridge.StateEnded {
		delete(l.all, st.Session)
		delete(l.subagentsShown, st.Session)
	} else {
		l.all[st.Session] = st
	}
	w := a.windowOf(st.Session)
	if w != nil {
		w.noteResumable(st)
	}
	if w != nil && !w.background {
		if !had && st.State != bridge.StateEnded {
			a.autoGroup(st.Session)
		} else if st.State == bridge.StateEnded {
			w.autoUngroup(st.Session)
		}
	}
	if w != nil {
		w.strip.refresh()
	}
	for _, f := range l.observers {
		f()
	}
	// A background agent that quit leaves only its shell: close it.
	if w != nil && w.background && st.State == bridge.StateEnded {
		if ps := w.model.PaneBySession(st.Session); ps != nil {
			w.model.ClosePane(ps.ID)
		}
	}
	if w == nil || st.State == bridge.StateEnded {
		return
	}
	if w.agentVisible(st.Session) {
		l.clearNote(st.Session, "")
		if st.Unseen {
			go a.be.Bridge.Seen(st.Session)
		}
		return
	}
	now := agentState(st)
	// #39: a new permission prompt says what it asks and where.
	var ask *bridge.Approval
	for i, ap := range st.Approvals {
		if !slices.ContainsFunc(old.Approvals, func(o bridge.Approval) bool { return o.ID == ap.ID }) {
			ask = &st.Approvals[i]
			break
		}
	}
	if had && agentState(old) == now && ask == nil {
		return
	}
	ps := w.model.PaneBySession(st.Session)
	if ps == nil {
		return
	}
	t, ok := w.model.TabOf(ps.ID)
	if !ok {
		return
	}
	title := a.settings.text(w.model.Title(t))
	if st.Name != "" {
		title = st.Name
	}
	var head, body, sound string
	switch now {
	case bridge.StateWaiting:
		head, body, sound = agentProduct(st)+" needs you", a.settings.text(st.Message), notify.SoundAttention
		if ask != nil {
			body = a.settings.text(strings.Join(append([]string{toolName(ask.Tool) + ": " + ask.What}, ask.Facts...), "\n"))
		}
		if ask != nil && len(ask.Questions) > 0 {
			head = agentProduct(st) + " asks you"
			var qs []string
			for _, q := range ask.Questions {
				qs = append(qs, q.Question)
			}
			body = a.settings.text(strings.Join(qs, "\n"))
		}
	case agentDone:
		head, body, sound = agentProduct(st)+" finished", a.settings.text(st.Prompt), notify.SoundComplete
	default:
		return
	}
	if !a.agents.soundDue(a.settings.config().AgentSound) {
		sound = ""
	}
	// It takes the place of the agent's last one.
	n, session := a.be.Notify, st.Session
	replace := ""
	if prev := l.notes[session]; prev != nil {
		if prev.asked != nil {
			prev.close()
		} else {
			replace = prev.id
		}
	}
	note := &agentNote{state: now}
	l.notes[session] = note
	if ask == nil || now != bridge.StateWaiting {
		go func() {
			id := notify.Post(n, replace, head, title, body, sound)
			coreglib.IdleAdd(func() {
				if cur := l.notes[session]; cur == note {
					note.id = id
				} else if cur == nil || cur.id != id {
					go notify.Close(id) // out of date while it was shown
				}
			})
		}()
		return
	}
	// A permission prompt: Allow and Deny on the notification itself; a
	// question: its options, when one click answers it (else none).
	id := ask.ID
	actions := []notify.Action{{Key: bridge.Allow, Label: "Allow"}, {Key: bridge.Deny, Label: "Deny"}}
	if len(ask.Questions) > 0 {
		actions = nil
		if ask.Clickable() {
			for i, o := range ask.Questions[0].Options {
				actions = append(actions, notify.Action{Key: "option:" + strconv.Itoa(i), Label: a.settings.text(o)})
			}
		}
	}
	options := []string(nil)
	if ask.Clickable() {
		options = ask.Questions[0].Options
	}
	note.approval = id
	if replace != "" {
		go notify.Close(replace)
	}
	go func() {
		if len(actions) == 0 {
			id := notify.Post(n, "", head, title, body, sound)
			coreglib.IdleAdd(func() {
				if l.notes[session] == note {
					note.id = id
				} else {
					go notify.Close(id)
				}
			})
			return
		}
		asked, _ := notify.Ask(n, head, title, body, sound, actions)
		if asked == nil {
			return
		}
		coreglib.IdleAdd(func() {
			if l.notes[session] != note {
				asked.Close() // answered, or out of date, meanwhile
				return
			}
			note.asked = asked
		})
		k := <-asked.Chosen
		i, err := strconv.Atoi(strings.TrimPrefix(k, "option:"))
		switch {
		case k == bridge.Allow || k == bridge.Deny:
		case strings.HasPrefix(k, "option:") && err == nil && i < len(options):
		default:
			return
		}
		coreglib.IdleAdd(func() {
			if l.notes[session] == note {
				delete(l.notes, session)
			}
			if strings.HasPrefix(k, "option:") {
				w.answerQuestion(id, options[i])
			} else {
				w.answerApproval(id, k)
			}
		})
	}()
}

// soundGap: agents that change together make one sound.
const soundGap = 1500 * time.Millisecond

// soundDue says whether an agent notification may play its sound now
// (agent-sound on, and none played in the last soundGap).
func (l *agentList) soundDue(on bool) bool {
	if !on || time.Since(l.lastSound) < soundGap {
		return false
	}
	l.lastSound = time.Now()
	return true
}

// goToAgent shows an agent's window, tab and pane; a background one comes
// back into a tab first.
func (a *App) goToAgent(session string) {
	w := a.windowOf(session)
	if w == nil {
		return
	}
	if w.background {
		a.openAgent(session)
		if w = a.windowOf(session); w == nil {
			return
		}
	}
	ps := w.model.PaneBySession(session)
	if ps == nil {
		return
	}
	if t, ok := w.model.TabOf(ps.ID); ok {
		w.model.Activate(t.ID)
	}
	w.model.FocusPane(ps.ID)
	w.win.Present()
	w.agentsSeen()
}

// nextAgent goes to the next agent that needs you (then the next done one),
// after the focused pane's, round and round.
func (w *Window) nextAgent() {
	var list []bridge.Status
	for _, st := range w.app.agents.sorted() {
		if s := agentState(st); s == bridge.StateWaiting || s == agentDone {
			list = append(list, st)
		}
	}
	if len(list) == 0 {
		w.toast("No agent needs you")
		return
	}
	current := ""
	if t, ok := w.model.Active(); ok {
		if ps := w.model.Panes[t.FocusedPaneID]; ps != nil {
			current = ps.SessionID
		}
	}
	i := slices.IndexFunc(list, func(st bridge.Status) bool { return st.Session == current })
	w.app.goToAgent(list[(i+1)%len(list)].Session)
}

// agentPlace is an agent's tab title and folder, as shown.
func (a *App) agentPlace(session string) (title, cwd string) {
	w := a.windowOf(session)
	if w == nil {
		return session, ""
	}
	ps := w.model.PaneBySession(session)
	if ps == nil {
		return session, ""
	}
	title = session
	if t, ok := w.model.TabOf(ps.ID); ok {
		title = a.settings.text(w.model.Title(t))
	}
	return title, uicore.PrettyPath(ps.Cwd, a.home)
}

// agentAge: how long ago, short: "now", "4m", "2h", "3d".
func agentAge(since int64) string {
	if since <= 0 {
		return ""
	}
	d := time.Since(time.UnixMilli(since))
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// agentEntries are the picker's rows (Go to Agent…): every Claude Code,
// most urgent first, by its name or tab title.
func (w *Window) agentEntries() []paletteEntry {
	a := w.app
	var out []paletteEntry
	for _, st := range a.agents.sorted() {
		state := agentState(st)
		_, cwd := a.agentPlace(st.Session)
		title, tab := a.agentTitle(st)
		what := st.Prompt
		if state == bridge.StateWaiting && st.Message != "" {
			what = st.Message
		}
		detail := agentProduct(st) + " · " + agentLabel(state)
		for _, s := range []string{tab, cwd, uicore.BranchLabel(st.Git), a.settings.text(what)} {
			if s != "" {
				detail += " · " + s
			}
		}
		session := st.Session
		out = append(out, paletteEntry{section: "Agents", label: title + " — " + strings.ToLower(agentLabel(state)), detail: detail,
			hint: agentAge(st.Since), run: func() { a.goToAgent(session) }})
	}
	if len(out) == 0 {
		out = append(out, paletteEntry{section: "Agents", label: "No agents running", detail: "Start codex or claude in any tab", run: func() {}})
	}
	return out
}

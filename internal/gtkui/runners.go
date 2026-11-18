//go:build linux

package gtkui

import (
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/dayanraj/wtf/internal/bridge"
	"github.com/dayanraj/wtf/internal/notify"
	"github.com/dayanraj/wtf/internal/runner"
	"github.com/dayanraj/wtf/internal/sshhosts"
	"github.com/dayanraj/wtf/internal/uicore"
)

func init() {
	extraCSS = append(extraCSS, `.runner-down { background: @error_color; }`)
}

// runnerList is the agent runners (#41): remote Linux machines, reached
// over SSH, where Claude Code agents run with the Claude account you sign
// in to there, never an API key. Phase 1: add one from an SSH host, check
// it (facts only), install wtf-runner after you confirm, and open a tab for
// what needs you there. Listed in the Agents tab; as
// macos/Sources/Wtf/Runners.swift. Main thread only.
type runnerList struct {
	list []runner.Status
	// Each runner's agents and connection (phase 3), from its stream; and
	// the notification each agent last showed, replaced by its next.
	agents map[string]runner.Agents
	notes  map[string]string
	busy   map[string]bool
	loaded bool
	obs    map[int]func()
	next   int
}

func newRunnerList() *runnerList {
	return &runnerList{busy: map[string]bool{}, obs: map[int]func(){}, agents: map[string]runner.Agents{}, notes: map[string]string{}}
}

func (l *runnerList) observe(f func()) (stop func()) {
	id := l.next
	l.next++
	l.obs[id] = f
	return func() { delete(l.obs, id) }
}

func (l *runnerList) changed() {
	for _, f := range l.obs {
		f()
	}
}

func (l *runnerList) put(st runner.Status) {
	if i := slices.IndexFunc(l.list, func(r runner.Status) bool { return r.ID == st.ID }); i >= 0 {
		l.list[i] = st
	} else {
		l.list = append(l.list, st)
	}
}

func (l *runnerList) find(id string) (runner.Status, bool) {
	i := slices.IndexFunc(l.list, func(r runner.Status) bool { return r.ID == id })
	if i < 0 {
		return runner.Status{}, false
	}
	return l.list[i], true
}

// loadRunners reads the saved runners and checks each once: only when the
// Agents tab shows them (load on demand), never on a timer.
func (a *App) loadRunners() {
	l := a.runners
	if l.loaded {
		return
	}
	l.loaded = true
	l.list = a.be.Runners.List()
	for _, r := range l.list {
		a.checkRunner(r.ID)
	}
}

func (a *App) checkRunner(id string) {
	l := a.runners
	if l.busy[id] {
		return
	}
	l.busy[id] = true
	l.changed()
	svc := a.be.Runners
	go func() {
		st, err := svc.Check(id)
		coreglib.IdleAdd(func() {
			if err == nil {
				l.put(st)
			}
			delete(l.busy, id)
			l.changed()
		})
	}()
}

// addRunner: Add Runner…, an SSH host (from the SSH tab's hosts) and a
// name; host preselects one.
func (w *Window) addRunner(host *sshhosts.Host) {
	hosts := w.app.be.SSHHosts.List().Hosts
	if len(hosts) == 0 {
		w.toast("Add the machine as an SSH host first (SSH tab)")
		return
	}
	names := make([]string, len(hosts))
	sel := 0
	for i, h := range hosts {
		names[i] = h.Alias
		if h.Known {
			names[i] += " (known host)"
		}
		if host != nil && uicore.SSHHostID(h) == uicore.SSHHostID(*host) {
			sel = i
		}
	}
	f := newKubeForm(w, "Add Agent Runner", "A Linux machine where Claude Code agents run, signed in to your own Claude account (no API key). wtf reaches it over SSH with your keys; adding it changes nothing there.")
	pick := gtk.NewDropDownFromStrings(names)
	pick.SetSelected(uint(sel))
	f.field("SSH host", pick)
	name := f.entry("", "the host's name")
	f.field("Name", name)
	f.button("ok", "Add")
	f.d.ConnectResponse(func(r string) {
		if r != "ok" {
			return
		}
		h := hosts[pick.Selected()]
		port := ""
		if h.Known {
			port = h.Port
		}
		st, err := w.app.be.Runners.Add(h.Alias, port, name.Text())
		if err != nil {
			w.toast(err.Error())
			return
		}
		w.app.runners.put(st)
		w.app.runners.changed()
		w.app.checkRunner(st.ID)
	})
	f.present(pick)
}

// runnerFix is the next thing to do on a runner, from its facts, in the
// order it has to happen; ok false when it's ready.
func (w *Window) runnerFix(st runner.Status) (label string, run func(), ok bool) {
	f := st.Facts
	if f == nil {
		return "", nil, false
	}
	id := st.ID
	tab := func(what, title string) func() { return func() { w.runnerTab(id, what, title) } }
	switch {
	case !f.Reachable:
		return "Open a Shell There…", tab(runner.TabShell, "SSH"), true
	case f.OS != "Linux" || f.GoArch == "":
		return "", nil, false
	case f.Tmux == "" && f.PackageManager != "":
		return "Install tmux…", tab(runner.TabInstallTmux, "Install tmux"), true
	case f.Claude == "":
		return "Install Claude Code…", tab(runner.TabInstallClaude, "Install Claude Code"), true
	case !f.SignedIn || f.AuthMethod != "claude.ai":
		return "Sign In…", tab(runner.TabSignIn, "Sign in"), true
	case !f.Onboarded:
		return "Finish Claude Code Setup…", tab(runner.TabSetup, "Claude Code setup"), true
	case f.RunnerVersion == "":
		return "Install wtf-runner…", func() { w.installRunner(id) }, true
	case st.Outdated:
		return "Update wtf-runner…", func() { w.installRunner(id) }, true
	}
	return "", nil, false
}

// runnerTab opens a tab running what needs you on the runner; Check Again
// when you're done.
func (w *Window) runnerTab(id, what, title string) {
	cmd, err := w.app.be.Runners.TabCommand(id, what)
	if err != nil {
		w.toast(err.Error())
		return
	}
	st, _ := w.app.runners.find(id)
	t := w.model.Open(uicore.OpenOptions{Command: cmd})
	w.model.Rename(t, title+" · "+st.Name)
	w.win.Present()
}

func (w *Window) installRunner(id string) {
	st, ok := w.app.runners.find(id)
	if !ok || st.Facts == nil {
		return
	}
	verb, button := "Install", "Install"
	body := "wtf copies these to ~/.local/share/wtf-runner on " + st.Alias + " over SSH"
	if v := st.Facts.RunnerVersion; v != "" {
		verb, button = "Update", "Update"
		body += ", replacing version " + v
	}
	body += ". Nothing else there changes.\n\n• wtf-runner (holds the agents wtf starts there)\n• wtf-mcp (Claude Code's hook helper)\n• status-settings.json (the hooks that report each agent's state)"
	l, svc := w.app.runners, w.app.be.Runners
	w.confirmPlan(&w.win.Window, verb+" wtf-runner on "+st.Name+"?", body, button, false, func() {
		l.busy[id] = true
		l.changed()
		go func() {
			st, err := svc.Install(id)
			coreglib.IdleAdd(func() {
				if err != nil {
					w.toast(err.Error())
				} else {
					l.put(st)
				}
				delete(l.busy, id)
				l.changed()
			})
		}()
	}, nil)
}

func (w *Window) runnerMenu(id string) []*menuItem {
	st, ok := w.app.runners.find(id)
	if !ok {
		return nil
	}
	var items []*menuItem
	if !w.app.runners.busy[id] {
		items = append(items, item("Check Again", func() { w.app.checkRunner(id) }))
	}
	label, run, fix := w.runnerFix(st)
	if fix {
		items = append(items, item(label, run))
	}
	items = append(items, menuSection, item("Open a Shell There", func() { w.runnerTab(id, runner.TabShell, "SSH") }))
	if f := st.Facts; f != nil && f.Reachable {
		if f.Claude != "" {
			l := "Sign In…"
			if f.SignedIn {
				l = "Sign In Again…"
			}
			items = append(items, item(l, func() { w.runnerTab(id, runner.TabSignIn, "Sign in") }))
		}
		if f.RunnerVersion != "" && !strings.HasSuffix(label, "wtf-runner…") {
			items = append(items, item("Reinstall wtf-runner…", func() { w.installRunner(id) }))
		}
	}
	items = append(items, item("Rename…", func() { w.renameRunner(st) }), menuSection,
		item("Remove Runner…", func() { w.removeRunner(st) }))
	return items
}

func (w *Window) renameRunner(st runner.Status) {
	w.app.ask(&w.win.Window, "Rename Runner", "What wtf calls "+st.Alias+".", st.Name, "Rename", func(name string) {
		if err := w.app.be.Runners.Rename(st.ID, name); err != nil {
			w.toast(err.Error())
			return
		}
		w.app.runners.list = w.app.be.Runners.List()
		w.app.runners.changed()
	})
}

// removeRunner forgets the runner; optionally removes wtf-runner from it.
func (w *Window) removeRunner(st runner.Status) {
	also := gtk.NewCheckButtonWithLabel("Also delete ~/.local/share/wtf-runner there")
	l, svc := w.app.runners, w.app.be.Runners
	w.confirmPlanWith(&w.win.Window, "Remove "+st.Name+" from wtf?",
		"Claude Code, its sign-in and your files on "+st.Alias+" stay as they are.", also, "Remove", func() {
			uninstall := also.Active()
			go func() {
				var err error
				if uninstall {
					err = svc.Uninstall(st.ID)
				}
				if err == nil {
					err = svc.Remove(st.ID)
				}
				coreglib.IdleAdd(func() {
					if err != nil {
						w.toast(err.Error())
						return
					}
					l.list = slices.DeleteFunc(l.list, func(r runner.Status) bool { return r.ID == st.ID })
					delete(l.agents, st.ID)
					l.changed()
				})
			}()
		})
}

// confirmPlanWith asks before removing something, with an extra widget
// (a check box) under the text.
func (w *Window) confirmPlanWith(parent *gtk.Window, title, body string, extra gtk.Widgetter, button string, ok func()) {
	d := adw.NewMessageDialog(parent, title, body)
	d.SetExtraChild(extra)
	d.AddResponse("cancel", "Cancel")
	d.AddResponse("ok", button)
	d.SetResponseAppearance("ok", adw.ResponseDestructive)
	d.SetDefaultResponse("cancel")
	d.SetCloseResponse("cancel")
	d.ConnectResponse(func(r string) {
		if r == "ok" {
			ok()
		}
	})
	w.app.debugLog("confirm: %s", title)
	d.Present()
}

// runnerAgents: a runner's agents changed. A notification and a sound
// when one needs you or finishes, as for agents here.
func (a *App) runnerAgents(ev runner.Agents) {
	l := a.runners
	st, ok := l.find(ev.Runner)
	if !ok {
		return
	}
	old, had := l.agents[ev.Runner]
	l.agents[ev.Runner] = ev
	l.changed()
	if !had {
		return
	}
	for _, ag := range ev.Agents {
		if ag.Status == nil {
			continue
		}
		now, was := agentState(*ag.Status), ""
		for _, o := range old.Agents {
			if o.ID == ag.ID && o.Status != nil {
				was = agentState(*o.Status)
			}
		}
		if now == was {
			continue
		}
		var head, body, sound string
		switch now {
		case bridge.StateWaiting:
			head, body, sound = "Claude Code on "+st.Name+" needs you", a.settings.text(ag.Status.Message), notify.SoundAttention
		case agentDone:
			head, body, sound = "Claude Code on "+st.Name+" finished", a.settings.text(ag.Status.Prompt), notify.SoundComplete
		default:
			continue
		}
		if !a.agents.soundDue(a.settings.config().AgentSound) {
			sound = ""
		}
		key, n, title := ev.Runner+"|"+ag.ID, a.be.Notify, remoteTitle(ag)
		replace := l.notes[key]
		go func() {
			id := notify.Post(n, replace, head, title, body, sound)
			coreglib.IdleAdd(func() { l.notes[key] = id })
		}()
	}
}

// remoteTitle is an agent's name, else its folder's.
func remoteTitle(ag runner.RemoteAgent) string {
	if ag.Name != "" {
		return ag.Name
	}
	return filepath.Base(ag.Dir)
}

// remoteAgentRows: a runner's agents under it, "ragent:<runner>|<id>".
func (t *agentsTab) remoteAgentRows(id string) []sideRow {
	a := t.w.app
	home := "" // the runner's, so its folders read ~/…
	if st, ok := a.runners.find(id); ok && st.Facts != nil {
		home = st.Facts.Home
	}
	var rows []sideRow
	for _, ag := range a.runners.agents[id].Agents {
		r := sideRow{id: "ragent:" + id + "|" + ag.ID, dot: "agent-idle", title: remoteTitle(ag), depth: 1,
			tip: "Claude Code on the runner; right-click for more"}
		label := "Starting"
		switch {
		case !ag.Alive:
			label = "Ended"
			if ag.Exit != "" {
				label += " (" + ag.Exit + ")"
			}
		case ag.Status != nil:
			state := agentState(*ag.Status)
			r.dot, label = "agent-"+state, agentLabel(state)
			r.pills = []sidePill{{text: agentAge(ag.Status.Since), tip: "In this state since then"}}
		}
		r.detail = label + " · " + uicore.PrettyPath(ag.Dir, home)
		switch {
		case ag.Status != nil && ag.Status.State == bridge.StateWaiting && ag.Status.Message != "":
			r.alert, r.alertClass = a.settings.text(ag.Status.Message), "agent-alert"
		case ag.Status != nil && ag.Status.Prompt != "":
			r.alert, r.alertClass = a.settings.text(ag.Status.Prompt), "dim-label"
		case ag.Prompt != "":
			r.alert, r.alertClass = a.settings.text(ag.Prompt), "dim-label"
		}
		rows = append(rows, r)
	}
	return rows
}

func (w *Window) remoteAgentMenu(rowID string) []*menuItem {
	id, agentID, _ := strings.Cut(strings.TrimPrefix(rowID, "ragent:"), "|")
	var ag runner.RemoteAgent
	found := false
	for _, x := range w.app.runners.agents[id].Agents {
		if x.ID == agentID {
			ag, found = x, true
		}
	}
	if !found {
		return nil
	}
	svc := w.app.be.Runners
	run := func(f func() error) {
		go func() {
			if err := f(); err != nil {
				coreglib.IdleAdd(func() { w.toast(err.Error()) })
			}
		}()
	}
	var items []*menuItem
	if ag.Status != nil && ag.Status.Unseen {
		items = append(items, item("Mark as Seen", func() { run(func() error { return svc.SeenAgent(id, agentID) }) }))
	}
	items = append(items,
		item("Rename…", func() {
			w.app.ask(&w.win.Window, "Rename Agent", "Shown in the Agents tab and its notifications.", ag.Name, "Rename", func(name string) {
				run(func() error { return svc.RenameAgent(id, agentID, name) })
			})
		}),
		item("Copy Folder Path", func() { w.win.Clipboard().SetText(ag.Dir) }),
		menuSection)
	if ag.Alive {
		items = append(items, item("End Claude Code…", func() {
			w.app.confirm(&w.win.Window, "End "+remoteTitle(ag)+"?", "Its tmux session on the runner closes, and Claude Code with it.", "End", func() {
				run(func() error { return svc.EndAgent(id, agentID) })
			})
		}))
	} else {
		items = append(items, item("Remove from List", func() { run(func() error { return svc.ForgetAgent(id, agentID) }) }))
	}
	return items
}

// runnerRows is the Runners section's rows: "runner:<id>" each, then
// "runner-add".
func (t *agentsTab) runnerRows() []sideRow {
	w := t.w
	var rows []sideRow
	for _, st := range w.app.runners.list {
		busy := w.app.runners.busy[st.ID]
		f := st.Facts
		r := sideRow{id: "runner:" + st.ID, dot: "agent-idle", title: st.Name, tip: "Right-click for more"}
		var lines []string
		if st.Alias != st.Name {
			lines = append(lines, st.Alias)
		}
		state := ""
		switch {
		case f == nil:
		case !f.Reachable:
			r.dot, state = "runner-down", "unreachable"
			r.alert, r.alertClass = f.Error, "error"
		case len(st.Problems) == 0 && !st.Outdated:
			r.dot, state = "agent-done", "ready"
		default:
			r.dot, state = "agent-waiting", "needs setup"
		}
		if busy {
			state = "checking…"
		}
		if state != "" {
			r.pills = []sidePill{{text: state}}
		}
		if f != nil && f.Reachable {
			lines = append(lines, runnerSystem(*f))
			if s := runnerAccount(*f); s != "" {
				lines = append(lines, s)
			}
			if f.APIKeySet {
				lines = append(lines, "ANTHROPIC_API_KEY is set there; agents wtf starts don't get it")
			}
			if len(st.Problems) > 0 {
				p := st.Problems[0]
				r.alert, r.alertClass = strings.ToUpper(p[:1])+p[1:], "agent-alert"
			} else if st.Outdated {
				r.alert, r.alertClass = "Its wtf-runner ("+f.RunnerVersion+") isn't this wtf's", "agent-alert"
			}
		}
		for i, s := range lines {
			if i == 0 {
				r.detail = s
			} else {
				r.more = append(r.more, sideText{text: s, classes: []string{"dim-label"}})
			}
		}
		if label, run, ok := w.runnerFix(st); ok && !busy {
			r.actions = []sideAction{{label: label, key: true, run: run}}
		}
		if c := w.app.runners.agents[st.ID]; f != nil && f.Reachable && !c.Connected && c.Error != "" {
			r.more = append([]sideText{{text: "Not connected: " + c.Error + " (retrying)", classes: []string{"agent-alert"}}}, r.more...)
		}
		rows = append(rows, r)
		rows = append(rows, t.remoteAgentRows(st.ID)...)
	}
	rows = append(rows, sideRow{id: "runner-add", icon: "list-add-symbolic", title: "Add Runner…",
		tip: "Run agents on a Linux machine over SSH, signed in to your own Claude account there"})
	return rows
}

// runnerSystem: "Linux arm64 · tmux 3.3a · Claude Code 2.1.327".
func runnerSystem(f runner.Facts) string {
	arch := f.GoArch
	if arch == "" {
		arch = f.Arch
	}
	var parts []string
	for _, s := range []string{strings.TrimSpace(f.OS + " " + arch), f.Tmux, prefixed("Claude Code ", f.ClaudeVersion), prefixed("wtf-runner ", f.RunnerVersion)} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " · ")
}

func prefixed(p, s string) string {
	if s == "" {
		return ""
	}
	return p + s
}

// runnerAccount: "Signed in as me@x.com (team) · until Dec 17".
func runnerAccount(f runner.Facts) string {
	if !f.SignedIn {
		return ""
	}
	s := "Signed in" + prefixed(" as ", f.Account)
	if f.Plan != "" {
		s += " (" + f.Plan + ")"
	}
	if f.AuthMethod != "claude.ai" {
		s += " with " + f.AuthMethod
	}
	if t, err := time.Parse(time.RFC3339Nano, f.LoginExpires); err == nil {
		s += " · until " + t.Local().Format("Jan 2")
	}
	return s
}

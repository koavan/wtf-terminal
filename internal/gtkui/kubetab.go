//go:build linux

package gtkui

import (
	"context"
	"fmt"
	"maps"
	"strconv"
	"strings"
	"time"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/dayanraj/wtf/internal/kube"
	"github.com/dayanraj/wtf/internal/uicore"
)

func init() {
	registerSidebarTab(sidebarTabInfo{tab: uicore.SidebarKube, label: "Kubernetes", icon: "application-x-addon-symbolic", cmd: "sidebar.kube",
		make: func(w *Window) sidePaneTab { return newKubeTab(w) }})
	extraCSS = append(extraCSS, `
.dot-kube-ok { background: @success_color; }
.dot-kube-wait { background: @warning_color; }
.dot-kube-bad { background: @error_color; }
.dot-kube-done { background: alpha(currentColor, 0.3); }
.wtf-kube-switch { margin: 4px 6px 0; }
.wtf-kube-ns { padding: 0 8px 0 10px; }
.wtf-kube-ns > label { font-size: 0.9em; opacity: 0.7; }
.wtf-kube-ns entry { font-family: monospace; font-size: 0.9em; }
.wtf-kube-tools { padding: 0 4px 0 0; }
.wtf-side-pill.in-use { color: @accent_color; background: alpha(@accent_color, 0.14); }
.wtf-side-note-bold { font-weight: bold; padding-bottom: 0; }
.wtf-form-error { color: @error_color; }
.wtf-secret-key { font-family: monospace; font-weight: bold; }
.wtf-secret-value { font-family: monospace; font-size: 0.92em; background: alpha(currentColor, 0.05); border-radius: 8px; }
`)
}

// kubeTab is the Kubernetes tab: Resources (what the focused shell's
// context holds) and Contexts (the kubeconfigs). Only the shown view
// exists. Like KubeView.svelte and the macOS app's KubeView.
type kubeTab struct {
	w         *Window
	root      *gtk.Box
	switches  [2]*gtk.ToggleButton
	body      sidePaneTab
	switching bool
}

func newKubeTab(w *Window) *kubeTab {
	k := &kubeTab{w: w, root: gtk.NewBox(gtk.OrientationVertical, 4)}
	bar := gtk.NewBox(gtk.OrientationHorizontal, 0)
	bar.AddCSSClass("linked")
	bar.AddCSSClass("wtf-kube-switch")
	bar.SetHomogeneous(true)
	for i, view := range []string{"cluster", "contexts"} {
		b := gtk.NewToggleButtonWithLabel([]string{"Resources", "Contexts"}[i])
		b.SetFocusOnClick(false)
		if i > 0 {
			b.SetGroup(k.switches[0])
		}
		b.ConnectToggled(func() {
			if b.Active() && !k.switching {
				k.choose(view)
			}
		})
		k.switches[i] = b
		bar.Append(b)
	}
	k.root.Append(bar)
	k.show(w.sidebarState.KubeView)
	return k
}

// choose shows a view and remembers it for the window.
func (k *kubeTab) choose(view string) {
	k.w.sidebarState.KubeView = view
	k.w.scheduleSave()
	k.show(view)
}

func (k *kubeTab) show(view string) {
	k.switching = true
	k.switches[0].SetActive(view != "contexts")
	k.switches[1].SetActive(view == "contexts")
	k.switching = false
	if k.body != nil {
		k.body.close()
		k.root.Remove(k.body.widget())
	}
	if view == "contexts" {
		k.body = newKubeContexts(k)
	} else {
		k.body = newKubeResources(k.w)
	}
	k.root.Append(k.body.widget())
}

func (k *kubeTab) widget() gtk.Widgetter { return k.root }
func (k *kubeTab) paneChanged()          { k.body.paneChanged() }
func (k *kubeTab) commandFinished()      { k.body.commandFinished() }
func (k *kubeTab) close()                { k.body.close() }

// State

// kubeState is the tab's state, per window: kept while the window lives
// so open sections and test results come back as they were.
type kubeState struct {
	target      *kube.Target
	targetError string
	targetSeq   int
	sections    map[string]*kubeSection
	open        map[string]bool
	// Namespace picked here per context; absent follows the shell.
	namespaces    map[string]string
	namespaceList []string
	namespacesFor string
	deployment    *kube.Deployment
	// Test Connection results, by context key.
	checks map[string]kubeCheck
}

type kubeSection struct {
	items     any
	err       string
	loading   bool
	updatedAt time.Time
	seq       int
}

type kubeCheck struct {
	busy        bool
	result, err string
}

var kubeStates = map[*Window]*kubeState{}

func kubeStateOf(w *Window) *kubeState {
	for k := range kubeStates {
		if k.closing {
			delete(kubeStates, k)
		}
	}
	s := kubeStates[w]
	if s == nil {
		s = &kubeState{sections: map[string]*kubeSection{}, open: map[string]bool{}, namespaces: map[string]string{},
			checks: map[string]kubeCheck{}}
		kubeStates[w] = s
	}
	return s
}

func (s *kubeState) section(id string) *kubeSection {
	if x := s.sections[id]; x != nil {
		return x
	}
	x := &kubeSection{}
	s.sections[id] = x
	return x
}

// forget drops what was read; loads in flight are ignored when they land.
func (s *kubeState) forget() {
	for _, x := range s.sections {
		*x = kubeSection{seq: x.seq + 1}
	}
	s.deployment = nil
}

// kubeShell is the focused shell's context, namespace and variables.
func kubeShell(w *Window) uicore.KubeTarget {
	t := uicore.KubeTarget{Env: map[string]string{}}
	if ps := w.model.ActivePane(); ps != nil {
		if ps.Env != nil {
			t.Env = ps.Env
		}
		if c := w.infra[ps.ID]; c != nil {
			t.Context, t.Namespace = c.KubeContext, c.KubeNamespace
		}
	}
	return t
}

// kubeShellIdle: the focused pane is a shell at its prompt.
func kubeShellIdle(w *Window) bool {
	ps := w.model.ActivePane()
	return ps != nil && ps.IsShell && !ps.Running && w.active() != nil
}

// Resources

var kubeGroups = []struct {
	name string
	ids  []string
}{
	{"Workloads", []string{"deployments", "statefulsets", "daemonsets", "jobs", "cronjobs", "pods"}},
	{"Network", []string{"services", "ingresses"}},
	{"Config", []string{"configmaps", "secrets"}},
	{"Storage", []string{"pvcs"}},
	{"Scaling", []string{"hpas"}},
	{"Cluster", []string{"nodes", "events"}},
}

func kubeTableKind(id string) (uicore.KubeTableKind, bool) {
	for _, k := range uicore.KubeTableKinds {
		if k.ID == id {
			return k, true
		}
	}
	return uicore.KubeTableKind{}, false
}

func kubeLabel(id string) string {
	switch id {
	case "deployments":
		return "Deployments"
	case "pods":
		return "Pods"
	case "events":
		return "Events"
	}
	if k, ok := kubeTableKind(id); ok {
		return k.Label
	}
	return id
}

func toneDot(t uicore.Tone) string {
	return [...]string{"dot-kube-ok", "dot-kube-wait", "dot-kube-bad", "dot-kube-done"}[t]
}

// kubeResources is the Resources view: sections by kind, each read from
// the cluster only while open, and refreshed every max(setting, 10s) while
// the window is in front.
type kubeResources struct {
	w         *Window
	st        *kubeState
	root      *gtk.Box
	header    *paneHeader
	pause     *gtk.Button
	nsRow     *gtk.Box
	ns        *gtk.Entry
	nsMenu    *menuPopover
	nsEditing bool
	paused    *gtk.Label
	filter    *gtk.SearchEntry
	note      *gtk.Box
	list      *sideList
	timer     coreglib.SourceHandle
	unobserve func()
	lastShell *uicore.KubeTarget
	closed    bool
}

func newKubeResources(w *Window) *kubeResources {
	r := &kubeResources{w: w, st: kubeStateOf(w)}
	r.header = newPaneHeader("Kubernetes")
	r.pause = r.header.tool("media-playback-pause-symbolic", "", func() { toggleAutoRefresh(w) })
	r.header.tool("view-refresh-symbolic", "Refresh now", r.resolve)

	r.ns = gtk.NewEntry()
	r.ns.SetHExpand(true)
	r.ns.ConnectActivate(r.namespaceCommitted)
	focus := gtk.NewEventControllerFocus()
	focus.ConnectEnter(func() {
		r.nsEditing = true
		r.loadNamespaces(nil)
	})
	focus.ConnectLeave(func() { r.nsEditing = false })
	r.ns.AddController(focus)
	pick := gtk.NewButtonFromIconName("pan-down-symbolic")
	pick.SetTooltipText("Namespaces in this context")
	pick.SetFocusOnClick(false)
	field := gtk.NewBox(gtk.OrientationHorizontal, 0)
	field.AddCSSClass("linked")
	field.SetHExpand(true)
	field.Append(r.ns)
	field.Append(pick)
	r.nsMenu = newMenuPopover(field)
	pick.ConnectClicked(func() { r.loadNamespaces(func() { r.showNamespaces(field) }) })
	r.nsRow = gtk.NewBox(gtk.OrientationHorizontal, 8)
	r.nsRow.AddCSSClass("wtf-kube-ns")
	r.nsRow.Append(sideLabel("Namespace", ""))
	r.nsRow.Append(field)

	r.paused = sideLabel("Paused", "wtf-side-note")
	r.filter = gtk.NewSearchEntry()
	r.filter.SetPlaceholderText("Filter by name, status, node")
	r.filter.AddCSSClass("wtf-side-filter")
	r.filter.ConnectSearchChanged(r.rebuild)
	r.filter.ConnectStopSearch(func() {
		r.filter.SetText("")
		w.focusActive()
	})
	r.note = gtk.NewBox(gtk.OrientationVertical, 0)

	r.list = newSideList(false)
	r.list.onClick = func(id string) {
		if d, ok := r.deploymentOf(id); ok {
			r.selectDeployment(&d)
		}
	}
	r.list.onActivate = r.activated
	r.list.onMenu = func(ids []string, x, y float64) {
		if items := r.menu(ids[0]); len(items) > 0 {
			r.list.menu.show(x, y, items, nil)
		}
	}

	r.root = gtk.NewBox(gtk.OrientationVertical, 4)
	r.root.Append(r.header.root)
	r.root.Append(r.nsRow)
	r.root.Append(r.paused)
	r.root.Append(r.filter)
	r.root.Append(r.note)
	r.root.Append(r.list.root)
	r.unobserve = w.app.settings.observe(r.startPolling)
	r.resolve()
	r.startPolling()
	return r
}

func (r *kubeResources) widget() gtk.Widgetter { return r.root }
func (r *kubeResources) commandFinished()      {}

func (r *kubeResources) close() {
	r.closed = true
	if r.timer != 0 {
		coreglib.SourceRemove(r.timer)
		r.timer = 0
	}
	r.unobserve()
	// Loads in flight land nowhere; the next view reads again.
	for _, s := range r.st.sections {
		if s.loading {
			s.seq++
			s.loading = false
		}
	}
}

// commandTarget is the target as commands use it: its context and
// namespace, the shell's variables.
func (r *kubeResources) commandTarget() (uicore.KubeTarget, bool) {
	t := r.st.target
	if t == nil {
		return uicore.KubeTarget{}, false
	}
	return uicore.KubeTarget{Context: t.Context, Namespace: t.Namespace, Env: kubeShell(r.w).Env}, true
}

func (r *kubeResources) paneChanged() {
	s := kubeShell(r.w)
	if l := r.lastShell; l == nil || l.Context != s.Context || l.Namespace != s.Namespace || !maps.Equal(l.Env, s.Env) {
		r.resolve()
	}
}

// resolve works out which context and namespace to show, then reads the
// open sections.
func (r *kubeResources) resolve() {
	s := kubeShell(r.w)
	r.lastShell = &s
	st := r.st
	st.targetSeq++
	mine := st.targetSeq
	req := kube.Request{Env: s.Env, Context: s.Context, Namespace: st.namespaces[s.Context]}
	svc := r.w.app.be.Kube
	go func() {
		t, err := svc.Target(req)
		onMainAsync(func() {
			if r.closed || mine != st.targetSeq {
				return
			}
			if err != nil {
				st.target = nil
				st.targetError = err.Error()
				st.forget()
			} else {
				if st.target == nil || t.Context != st.target.Context || t.Namespace != st.target.Namespace {
					st.forget()
				}
				if st.namespacesFor != t.Context {
					st.namespaceList = nil
				}
				st.target = &t
				st.targetError = ""
				r.refreshOpen()
			}
			r.rebuild()
		})
	}()
}

func (r *kubeResources) refreshOpen() {
	for id := range r.st.open {
		r.refresh(id)
	}
	r.rebuild()
}

func (r *kubeResources) refresh(id string) {
	t := r.st.target
	if t == nil {
		return
	}
	s := r.st.section(id)
	if s.loading {
		return
	}
	s.seq++
	s.loading = true
	mine := s.seq
	req := kube.Request{Env: kubeShell(r.w).Env, Context: t.Context, Namespace: t.Namespace}
	var selector map[string]string
	if d := r.st.deployment; d != nil {
		selector = d.Selector
	}
	svc := r.w.app.be.Kube
	go func() {
		var items any
		var err error
		switch id {
		case "deployments":
			items, err = svc.Deployments(req)
		case "pods":
			items, err = svc.Pods(req, selector)
		case "events":
			items, err = svc.Events(req)
		default:
			items, err = svc.Table(req, id)
		}
		onMainAsync(func() {
			if mine != s.seq {
				return
			}
			if err != nil {
				s.err = err.Error()
			} else {
				s.items, s.err, s.updatedAt = items, "", time.Now()
			}
			s.loading = false
			r.rebuild()
		})
	}()
}

func (r *kubeResources) toggle(id string) {
	if r.st.open[id] {
		delete(r.st.open, id)
		s := r.st.section(id)
		s.seq++
		s.loading = false
	} else {
		r.st.open[id] = true
		r.refresh(id)
	}
	r.rebuild()
}

// selectDeployment shows a deployment's pods; again (or nil) shows all.
func (r *kubeResources) selectDeployment(d *kube.Deployment) {
	if cur := r.st.deployment; d != nil && (cur == nil || cur.Name != d.Name) {
		r.st.deployment = d
	} else {
		r.st.deployment = nil
	}
	pods := r.st.section("pods")
	pods.items = nil
	pods.seq++
	pods.loading = false
	r.st.open["pods"] = true
	r.refresh("pods")
	r.rebuild()
}

// startPolling refreshes open sections every max(setting, 10s), only
// while the window is in front.
func (r *kubeResources) startPolling() {
	if r.timer != 0 {
		coreglib.SourceRemove(r.timer)
		r.timer = 0
	}
	if cfg := r.w.app.settings.config(); cfg.SidebarAutoRefresh {
		seconds := max(10, cfg.SidebarRefreshInterval)
		r.timer = coreglib.TimeoutAdd(uint(seconds*1000), func() bool {
			if r.w.win.IsActive() {
				r.refreshOpen()
			}
			return true
		})
	}
	r.rebuild()
}

// toggleAutoRefresh turns the side pane's auto-refresh off or on.
func toggleAutoRefresh(w *Window) {
	on := !w.app.settings.config().SidebarAutoRefresh
	svc := w.app.be.Config
	go func() {
		err := svc.SetSidebarAutoRefresh(on)
		onMainAsync(func() {
			switch {
			case err != nil:
				w.toast("Couldn't change the setting: " + err.Error())
			case on:
				w.toast("Side pane auto-refresh on")
			default:
				w.toast("Side pane auto-refresh off")
			}
		})
	}()
}

// Building

func (r *kubeResources) deployments() []kube.Deployment {
	d, _ := r.st.section("deployments").items.([]kube.Deployment)
	return d
}

func (r *kubeResources) deploymentOf(id string) (kube.Deployment, bool) {
	for _, d := range r.deployments() {
		if "deployments:"+d.Name == id {
			return d, true
		}
	}
	return kube.Deployment{}, false
}

func (r *kubeResources) pod(id string) (kube.Pod, bool) {
	pods, _ := r.st.section("pods").items.([]kube.Pod)
	for _, p := range pods {
		if "pods:"+p.Name == id {
			return p, true
		}
	}
	return kube.Pod{}, false
}

func (r *kubeResources) query() string { return strings.ToLower(strings.TrimSpace(r.filter.Text())) }

// cells is a table row by column.
func cells(t kube.Table, row []string) map[string]string {
	c := map[string]string{}
	for i, col := range t.Columns {
		if i < len(row) {
			c[col] = row[i]
		}
	}
	return c
}

// setNote shows a note above the list: a bold line, then text.
func setNote(box *gtk.Box, bold, text string) {
	for c := box.FirstChild(); c != nil; c = box.FirstChild() {
		box.Remove(c)
	}
	if bold != "" {
		b := wrapLabel(bold, "wtf-side-note")
		b.AddCSSClass("wtf-side-note-bold")
		box.Append(b)
	}
	if text != "" {
		t := wrapLabel(text, "wtf-side-note")
		box.Append(t)
	}
	box.SetVisible(bold != "" || text != "")
}

func (r *kubeResources) rebuild() {
	if r.closed {
		return
	}
	st, w := r.st, r.w
	t, shell := st.target, kubeShell(w)
	cfg := w.app.settings.config()
	if cfg.SidebarAutoRefresh {
		r.pause.SetIconName("media-playback-pause-symbolic")
		r.pause.SetTooltipText("Pause auto-refresh (side pane updates only when you ask)")
		r.pause.RemoveCSSClass("active")
	} else {
		r.pause.SetIconName("media-playback-start-symbolic")
		r.pause.SetTooltipText("Resume auto-refresh")
		r.pause.AddCSSClass("active")
	}
	switch {
	case t != nil:
		cluster := t.Cluster
		if cluster == "" {
			cluster = t.Context
		}
		r.header.setTitle(uicore.ShortClusterName(cluster), "Context: "+t.Context+"\nCluster: "+t.Cluster+"\nFollows the focused shell's context")
		if !r.nsEditing {
			r.ns.SetText(t.Namespace)
		}
	case shell.Context != "":
		r.header.setTitle(shell.Context, "")
	default:
		r.header.setTitle("Kubernetes", "")
	}
	if shell.Namespace == "" {
		r.ns.SetTooltipText("The namespace to show")
	} else {
		r.ns.SetTooltipText("The namespace to show. The shell’s is " + shell.Namespace)
	}
	r.nsRow.SetVisible(t != nil)
	anyOpen := len(st.open) > 0
	r.paused.SetVisible(anyOpen && !cfg.SidebarAutoRefresh)
	if r.paused.Visible() {
		var last time.Time
		for id := range st.open {
			if u := st.section(id).updatedAt; u.After(last) {
				last = u
			}
		}
		text := "Paused"
		if !last.IsZero() {
			text += " · updated " + last.Format("15:04:05")
		}
		r.paused.SetText(text)
	}
	r.filter.SetVisible(anyOpen)
	switch {
	case st.targetError != "":
		setNote(r.note, "Can’t find a Kubernetes context", st.targetError)
		installLink(w, r.note, st.targetError)
	case t != nil && !anyOpen:
		setNote(r.note, "", "Open a section to load it. Only open sections are read from the cluster, and refreshed.")
	default:
		setNote(r.note, "", "")
	}
	if t == nil {
		r.list.set(nil)
		return
	}

	q, now := r.query(), time.Now()
	matches := func(text string) bool { return q == "" || strings.Contains(strings.ToLower(text), q) }
	var sections []sideSection
	for _, g := range kubeGroups {
		for i, id := range g.ids {
			s := st.section(id)
			open := st.open[id]
			label := kubeLabel(id)
			kind, _ := kubeTableKind(id)
			var rows []sideRow
			if s.err != "" {
				row := sideRow{id: id + ":!error", icon: "dialog-warning-symbolic", title: s.err, titleClass: "error", tip: s.err}
				if tool := uicore.MissingTool(s.err); tool != "" {
					row.button, row.buttonTip = "system-software-install-symbolic", "Show how to install it"
					row.buttonRun = func() { w.showTools(tool) }
				}
				rows = append(rows, row)
			}
			count := 0
			switch id {
			case "deployments":
				for _, d := range r.deployments() {
					if !matches(d.Name) {
						continue
					}
					tone := uicore.DeploymentTone(d)
					// One red line per problem, as in the Wails build.
					rows = append(rows, sideRow{id: "deployments:" + d.Name, title: d.Name, mono: true, detail: uicore.DeploymentDetail(d, now),
						alert: strings.Join(d.Problems, "\n"), tip: strings.Join(append(append([]string{}, d.Images...), d.Problems...), "\n"),
						dot: toneDot(tone), highlighted: st.deployment != nil && st.deployment.Name == d.Name})
					count++
				}
			case "pods":
				pods, _ := s.items.([]kube.Pod)
				for _, p := range pods {
					if matches(p.Name + " " + p.Status + " " + p.Node) {
						rows = append(rows, sideRow{id: "pods:" + p.Name, title: p.Name, mono: true, detail: uicore.PodDetail(p, now),
							tip: uicore.PodTip(p), dot: toneDot(uicore.PodTone(p))})
						count++
					}
				}
			case "events":
				events, _ := s.items.([]kube.Event)
				for n, e := range events {
					if !matches(e.Object + " " + e.Reason + " " + e.Message) {
						continue
					}
					icon := "dialog-information-symbolic"
					if e.Type == "Warning" {
						icon = "dialog-warning-symbolic"
					}
					title := e.Reason + " " + e.Object
					if e.Count > 1 {
						title += " ×" + strconv.Itoa(e.Count)
					}
					rows = append(rows, sideRow{id: "events:" + strconv.Itoa(n), icon: icon, title: title + " · " + uicore.KubeAge(e.Last, now),
						detail: e.Message, tip: e.Type + " · " + e.Last})
					count++
				}
			default:
				table, _ := s.items.(kube.Table)
				for _, row := range table.Rows {
					c := cells(table, row)
					name := ""
					if len(row) > 0 {
						name = row[0]
					}
					if !matches(name + " " + strings.Join(row, " ")) {
						continue
					}
					var tip []string
					for _, col := range table.Columns {
						if v := c[col]; v != "" && v != "<none>" {
							tip = append(tip, col+": "+v)
						}
					}
					sr := sideRow{id: id + ":" + name, title: name, mono: true, detail: uicore.RowSummary(id, c), tip: strings.Join(tip, "\n")}
					if tone, ok := uicore.RowTone(id, c); ok {
						sr.dot = toneDot(tone)
					}
					rows = append(rows, sr)
					count++
				}
			}
			loaded := s.items != nil
			badge := ""
			if open {
				switch {
				case loaded:
					badge = strconv.Itoa(count)
				case s.loading:
					badge = "…"
				}
			}
			// Pods of one deployment: a row says so, with a way back to all.
			filtered := id == "pods" && st.deployment != nil
			if filtered && open {
				rows = append([]sideRow{{id: "pods:!filter", icon: "edit-find-symbolic", title: "Pods of deployment " + st.deployment.Name,
					tip: "Click × to show all pods", button: "window-close-symbolic", buttonTip: "Show all pods",
					buttonRun: func() { r.selectDeployment(nil) }}}, rows...)
			}
			empty := ""
			if loaded && count == 0 {
				switch {
				case id == "events":
					empty = "No recent events"
				case q != "" || filtered:
					empty = "No " + strings.ToLower(label) + " match"
				default:
					empty = "No " + strings.ToLower(label)
				}
				if len(rows) > 0 {
					rows = append(rows, sideRow{title: empty, muted: true})
				}
			}
			tip := "Show " + label
			if open {
				tip = "Collapse " + label
			}
			if kind.Note != "" {
				tip += "\n" + kind.Note
			}
			sec := sideSection{title: label, tip: tip, rows: rows, toggle: func() { r.toggle(id) }, open: open, badge: badge, empty: empty}
			if i == 0 {
				sec.group = g.name
			}
			sections = append(sections, sec)
		}
	}
	r.list.set(sections)
}

// Namespace

// loadNamespaces reads the context's namespaces once, then runs then.
func (r *kubeResources) loadNamespaces(then func()) {
	t := r.st.target
	if t == nil {
		return
	}
	if r.st.namespacesFor == t.Context {
		if then != nil && r.st.namespaceList != nil {
			then()
		}
		return
	}
	r.st.namespacesFor = t.Context
	req := kube.Request{Env: kubeShell(r.w).Env, Context: t.Context, Namespace: t.Namespace}
	svc, st := r.w.app.be.Kube, r.st
	go func() {
		list, err := svc.Namespaces(req)
		onMainAsync(func() {
			if err != nil || st.namespacesFor != req.Context {
				return
			}
			st.namespaceList = list
			if then != nil && !r.closed {
				then()
			}
		})
	}()
}

func (r *kubeResources) showNamespaces(at *gtk.Box) {
	var items []*menuItem
	for _, n := range r.st.namespaceList {
		items = append(items, item(n, func() {
			r.ns.SetText(n)
			r.namespaceCommitted()
		}))
	}
	if len(items) > 0 {
		r.nsMenu.show(float64(at.Width()-12), float64(at.Height()), items, nil)
	}
}

// namespaceCommitted shows another namespace here; the shell keeps its own.
func (r *kubeResources) namespaceCommitted() {
	v := strings.TrimSpace(r.ns.Text())
	t := r.st.target
	if t == nil || v == "" || v == t.Namespace {
		if t != nil {
			r.ns.SetText(t.Namespace)
		}
		return
	}
	shell := kubeShell(r.w)
	if v == shell.Namespace && t.Context == shell.Context {
		delete(r.st.namespaces, t.Context)
	} else {
		r.st.namespaces[t.Context] = v
	}
	r.w.focusActive()
	r.resolve()
}

// Actions

func (r *kubeResources) newTab(command string) { r.w.model.Open(uicore.OpenOptions{Command: command}) }

// here types a command into the focused shell without the variables (it
// has them), or runs it in a new tab with them.
func (r *kubeResources) here(build func(uicore.KubeTarget) string) {
	ct, ok := r.commandTarget()
	if !ok {
		return
	}
	bare := ct
	bare.Env = nil
	if kubeShellIdle(r.w) {
		r.w.runInPane(build(bare))
		return
	}
	var cwd *string
	if ps := r.w.model.ActivePane(); ps != nil {
		cwd = &ps.Cwd
	}
	r.w.model.Open(uicore.OpenOptions{Cwd: cwd, Command: build(ct)})
}

func (r *kubeResources) copy(text, toast string) {
	r.w.win.Clipboard().SetText(text)
	r.w.toast(toast)
}

// whereText says where a command goes: "Context x (cluster y), namespace z."
func (r *kubeResources) whereText() string {
	t := r.st.target
	if t == nil {
		return ""
	}
	cluster := ""
	if t.Cluster != "" {
		cluster = " (cluster " + uicore.ShortClusterName(t.Cluster) + ")"
	}
	return "Context " + t.Context + cluster + ", namespace " + t.Namespace + "."
}

// confirm confirms a command that changes the cluster, showing it, then
// runs it in a new tab.
func (r *kubeResources) confirm(title, button, command string) {
	r.w.app.confirm(&r.w.win.Window, title, r.whereText()+" This runs in a new tab:\n\n"+command, button, func() { r.newTab(command) })
}

// activated (double-click, Enter): a pod's logs, a secret's values.
func (r *kubeResources) activated(id string) {
	ct, ok := r.commandTarget()
	if !ok {
		return
	}
	if p, ok := r.pod(id); ok {
		container := ""
		if len(p.Containers) > 1 {
			container = p.Containers[0].Name
		}
		r.newTab(ct.Logs(p.Name, container, false))
	} else if name, ok := strings.CutPrefix(id, "secrets:"); ok {
		r.w.showSecret(name, ct)
	}
}

// portForward offers a kubectl port-forward, saved as a tunnel.
func (r *kubeResources) portForward(ct uicore.KubeTarget, resource, port string) {
	_, name, _ := strings.Cut(resource, "/")
	r.w.kubePortForward(uicore.TunnelDraft{Kind: "kubectl", Name: name, Context: ct.Context, Namespace: ct.Namespace,
		Resource: resource, ListenPort: port, DestPort: port, Env: uicore.KubectlTunnelEnv(ct.Env)})
}

func (r *kubeResources) menu(id string) []*menuItem {
	ct, ok := r.commandTarget()
	section, name, found := strings.Cut(id, ":")
	if !ok || !found {
		return nil
	}
	var items []*menuItem
	add := func(label string, run func()) { items = append(items, item(label, run)) }
	switch section {
	case "pods":
		p, ok := r.pod(id)
		if !ok {
			return nil
		}
		many := len(p.Containers) > 1
		if many {
			add("Logs", nil)
			for _, c := range p.Containers {
				add(c.Name, func() { r.newTab(ct.Logs(p.Name, c.Name, false)) })
			}
			add("Shell", nil)
			for _, c := range p.Containers {
				add(c.Name, func() { r.here(func(t uicore.KubeTarget) string { return t.Exec(p.Name, c.Name) }) })
			}
		} else {
			add("Logs", func() { r.newTab(ct.Logs(p.Name, "", false)) })
			add("Shell (exec)", func() { r.here(func(t uicore.KubeTarget) string { return t.Exec(p.Name, "") }) })
		}
		for _, c := range p.Containers {
			if c.Restarts == 0 {
				continue
			}
			if many {
				add("Logs Before Last Restart: "+c.Name, func() { r.newTab(ct.Logs(p.Name, c.Name, true)) })
			} else {
				add("Logs Before Last Restart", func() { r.newTab(ct.Logs(p.Name, "", true)) })
			}
		}
		items = append(items, menuSection)
		add("Describe", func() { r.here(func(t uicore.KubeTarget) string { return t.Describe("pod", p.Name) }) })
		add("Port Forward…", func() { r.portForward(ct, "pod/"+p.Name, "") })
		items = append(items, menuSection)
		add("Copy Name", func() { r.copy(p.Name, "Copied pod name") })
		if p.IP != "" {
			add("Copy IP ("+p.IP+")", func() { r.copy(p.IP, "Copied pod IP") })
		}
		items = append(items, menuSection)
		add("Delete Pod…", func() { r.confirm("Delete pod "+p.Name+"?", "Delete Pod", ct.DeletePod(p.Name)) })
	case "deployments":
		d, ok := r.deploymentOf(id)
		if !ok {
			return nil
		}
		show := "Show Its Pods"
		if r.st.deployment != nil && r.st.deployment.Name == d.Name {
			show = "Show All Pods"
		}
		add(show, func() { r.selectDeployment(&d) })
		add("Logs (all pods)", func() { r.newTab(ct.WorkloadLogs("deployment", name)) })
		add("Describe", func() { r.here(func(t uicore.KubeTarget) string { return t.Describe("deployment", name) }) })
		add("View YAML", func() { r.newTab(ct.YAML("deployment", name)) })
		add("Rollout Status", func() { r.newTab(ct.RolloutStatus("deployment", name)) })
		add("Rollout History", func() { r.newTab(ct.RolloutHistory("deployment", name)) })
		add("Port Forward…", func() { r.portForward(ct, "deployment/"+name, "") })
		items = append(items, menuSection)
		add("Restart…", func() { r.confirm("Restart "+name+"?", "Restart", ct.Restart("deployment", name)) })
		add("Scale…", func() { r.scale("deployment", name, d.Desired) })
		items = append(items, menuSection)
		add("Copy Name", func() { r.copy(name, "Copied deployment name") })
	case "events":
		return nil
	default:
		kind, ok := kubeTableKind(section)
		if !ok {
			return nil
		}
		k := kind.Kind
		table, _ := r.st.section(section).items.(kube.Table)
		var row []string
		for _, rw := range table.Rows {
			if len(rw) > 0 && rw[0] == name {
				row = rw
				break
			}
		}
		c := cells(table, row)
		switch section {
		case "statefulsets", "daemonsets":
			add("Logs (all pods)", func() { r.newTab(ct.WorkloadLogs(k, name)) })
			add("Rollout Status", func() { r.newTab(ct.RolloutStatus(k, name)) })
			items = append(items, menuSection)
			add("Restart…", func() { r.confirm("Restart "+name+"?", "Restart", ct.Restart(k, name)) })
			if section == "statefulsets" {
				ready := c["READY"]
				current, _ := strconv.Atoi(ready[strings.LastIndex(ready, "/")+1:])
				add("Scale…", func() { r.scale(k, name, current) })
			}
			items = append(items, menuSection)
		case "jobs":
			add("Logs", func() { r.newTab(ct.WorkloadLogs("job", name)) })
			items = append(items, menuSection)
		case "cronjobs":
			add("Run Now…", func() { r.confirm("Run "+name+" now?", "Create Job", ct.TriggerCronJob(name, time.Now())) })
			items = append(items, menuSection)
		case "services":
			add("Port Forward…", func() { r.portForward(ct, "service/"+name, uicore.FirstPort(c["PORT(S)"])) })
			items = append(items, menuSection)
		case "ingresses":
			var hosts []string
			for _, h := range strings.Split(c["HOSTS"], ",") {
				if h != "" && h != "*" {
					hosts = append(hosts, h)
				}
			}
			for _, h := range hosts {
				add("Open https://"+h, func() {
					gtk.NewURILauncher("https://"+h).Launch(context.Background(), &r.w.win.Window, nil)
				})
			}
			if len(hosts) > 0 {
				items = append(items, menuSection)
			}
		case "secrets":
			add("View Decoded Values…", func() { r.w.showSecret(name, ct) })
			add("Decode in Terminal", func() { r.newTab(ct.DecodeSecret(name)) })
			items = append(items, menuSection)
		}
		add("Describe", func() { r.here(func(t uicore.KubeTarget) string { return t.Describe(k, name) }) })
		if section != "secrets" {
			add("View YAML", func() { r.newTab(ct.YAML(k, name)) })
		}
		items = append(items, menuSection)
		add("Copy Name", func() { r.copy(name, fmt.Sprintf("Copied %s name", k)) })
	}
	return items
}

// Contexts

// kubeContexts is the Contexts view: the kubeconfigs' contexts, the ones
// the focused shell reads first. Read when shown, when the shell's
// variables change, and after commands that change a kubeconfig.
type kubeContexts struct {
	tab     *kubeTab
	w       *Window
	st      *kubeState
	root    *gtk.Box
	filter  *gtk.SearchEntry
	add     *gtk.Button
	addMenu *menuPopover
	note    *gtk.Box
	list    *sideList
	configs kube.ConfigList
	loaded  bool
	lastEnv map[string]string
	seq     int
	closed  bool
}

func newKubeContexts(tab *kubeTab) *kubeContexts {
	w := tab.w
	c := &kubeContexts{tab: tab, w: w, st: kubeStateOf(w)}
	c.filter = gtk.NewSearchEntry()
	c.filter.SetPlaceholderText("Filter contexts")
	c.filter.AddCSSClass("wtf-side-filter")
	c.filter.SetHExpand(true)
	c.filter.ConnectSearchChanged(c.rebuild)
	c.filter.ConnectStopSearch(func() {
		c.filter.SetText("")
		w.focusActive()
	})
	tools := gtk.NewBox(gtk.OrientationHorizontal, 0)
	tools.AddCSSClass("wtf-kube-tools")
	tools.Append(c.filter)
	tool := func(icon, tip string, run func()) *gtk.Button {
		b := gtk.NewButtonFromIconName(icon)
		b.AddCSSClass("flat")
		b.AddCSSClass("wtf-tool")
		b.SetTooltipText(tip)
		b.SetFocusOnClick(false)
		b.SetVAlign(gtk.AlignCenter)
		b.ConnectClicked(run)
		tools.Append(b)
		return b
	}
	c.add = tool("list-add-symbolic", "Add a cluster", c.showAddMenu)
	tool("view-refresh-symbolic", "Reload", c.load)
	c.addMenu = newMenuPopover(tools)
	c.note = gtk.NewBox(gtk.OrientationVertical, 0)

	c.list = newSideList(false)
	c.list.onActivate = func(id string) {
		if k, ok := c.context(id); ok {
			c.use(k, false)
		}
	}
	c.list.onMenu = func(ids []string, x, y float64) {
		if k, ok := c.context(ids[0]); ok {
			c.list.menu.show(x, y, c.menu(k), nil)
		}
	}
	c.root = gtk.NewBox(gtk.OrientationVertical, 4)
	c.root.Append(tools)
	c.root.Append(c.note)
	c.root.Append(c.list.root)
	c.load()
	return c
}

func (c *kubeContexts) widget() gtk.Widgetter { return c.root }
func (c *kubeContexts) close()                { c.closed = true }

func (c *kubeContexts) env() map[string]string { return kubeShell(c.w).Env }

func (c *kubeContexts) paneChanged() {
	if !maps.Equal(c.env(), c.lastEnv) {
		c.load()
	}
}

func (c *kubeContexts) commandFinished() {
	if ps := c.w.model.ActivePane(); c.loaded && ps != nil && ps.LastCommand != nil && uicore.ChangesKubeconfig(ps.LastCommand.Text) {
		c.load()
	}
}

func (c *kubeContexts) load() {
	env := c.env()
	c.lastEnv = env
	c.seq++
	mine := c.seq
	svc := c.w.app.be.Kube
	go func() {
		l := svc.Contexts(env)
		onMainAsync(func() {
			if c.closed || mine != c.seq {
				return
			}
			c.configs, c.loaded = l, true
			c.rebuild()
		})
	}()
}

func kubeKey(k kube.KubeContext) string { return k.File + "\n" + k.Name }

func (c *kubeContexts) context(id string) (kube.KubeContext, bool) {
	for _, k := range c.configs.Contexts {
		if kubeKey(k) == id {
			return k, true
		}
	}
	return kube.KubeContext{}, false
}

func (c *kubeContexts) rebuild() {
	if c.closed {
		return
	}
	home := c.w.app.home
	q := strings.ToLower(strings.TrimSpace(c.filter.Text()))
	var shown []kube.KubeContext
	for _, k := range c.configs.Contexts {
		if q == "" || strings.Contains(strings.ToLower(strings.Join([]string{k.Name, k.Cluster, k.Server, k.Namespace, k.EKSCluster}, " ")), q) {
			shown = append(shown, k)
		}
	}
	inUse := kubeShell(c.w).Context
	row := func(k kube.KubeContext) sideRow {
		var pills []sidePill
		if k.Active && k.Name == inUse {
			pills = append(pills, sidePill{text: "in use", class: "in-use", tip: "The focused shell uses this context"})
		}
		if k.Current {
			pills = append(pills, sidePill{text: "default", tip: "The kubeconfig's current-context: what new shells use"})
		}
		issues := append([]string{}, k.Problems...)
		if k.Auth == "exec" && !k.PluginFound {
			issues = append(issues, k.ExecCommand+" isn’t on this shell’s PATH")
		}
		// A test's answer is a line of its own, under the context's details.
		answer, answerClass := "", "dim-label"
		if ch, ok := c.st.checks[kubeKey(k)]; ok {
			switch {
			case ch.busy:
				answer = "Asking the cluster…"
			case ch.result != "":
				answer, answerClass = ch.result, "success"
			case ch.err != "":
				issues = append(issues, ch.err)
			}
		}
		tip := ""
		if !k.Active {
			tip = "From " + uicore.PrettyPath(k.File, home) + "\n"
		}
		tip += "Cluster: " + k.Cluster
		if k.Server != "" {
			tip += " (" + k.Server + ")"
		}
		tip += "\nUser: " + k.User
		if k.ExecCommand != "" {
			tip += "\nLogs in with: " + k.ExecCommand + " " + strings.Join(k.ExecArgs, " ")
		}
		if len(k.ExecEnv) > 0 {
			tip += "\nWith: " + strings.Join(k.ExecEnv, ", ")
		}
		sr := sideRow{id: kubeKey(k), icon: "application-x-addon-symbolic", title: k.Name, mono: true, detail: uicore.ContextDetail(k),
			pills: pills, tip: tip, button: "media-playback-start-symbolic", buttonTip: "Use in this shell", buttonRun: func() { c.use(k, false) }}
		if len(issues) > 0 {
			sr.alert = strings.Join(issues, " · ")
		} else {
			sr.alert, sr.alertClass = answer, answerClass
		}
		return sr
	}
	var sections []sideSection
	var active []sideRow
	for _, k := range shown {
		if k.Active {
			active = append(active, row(k))
		}
	}
	if len(active) > 0 {
		files := make([]string, len(c.configs.Files))
		for i, f := range c.configs.Files {
			files[i] = uicore.PrettyPath(f, home)
		}
		sections = append(sections, sideSection{title: "Used by this shell · " + strings.Join(files, ", "),
			tip: "The files kubectl reads in the focused shell (its KUBECONFIG, or ~/.kube/config)", rows: active})
	}
	for _, f := range c.configs.OtherFiles {
		var rows []sideRow
		for _, k := range shown {
			if !k.Active && k.File == f {
				rows = append(rows, row(k))
			}
		}
		if len(rows) > 0 {
			sections = append(sections, sideSection{title: uicore.PrettyPath(f, home),
				tip: f + "\nNot read by the focused shell; using one of its contexts points the shell at this file only", rows: rows})
		}
	}
	if len(c.configs.Problems) > 0 {
		var rows []sideRow
		for _, p := range c.configs.Problems {
			rows = append(rows, sideRow{id: "p:" + p, icon: "dialog-warning-symbolic", title: p, titleClass: "error", tip: p})
		}
		sections = append(sections, sideSection{title: "Problems", rows: rows})
	}
	c.list.set(sections)
	switch {
	case !c.loaded || len(shown) > 0:
		setNote(c.note, "", "")
	case q == "":
		setNote(c.note, "No Kubernetes contexts",
			"Contexts from your kubeconfig ($KUBECONFIG or ~/.kube/config) show here. Press + to add an EKS cluster or merge a kubeconfig file.")
	default:
		setNote(c.note, "", "No contexts match “"+c.filter.Text()+"”.")
	}
}

// use points the shell at a context with an overlay file; ~/.kube/config
// stays as it is.
func (c *kubeContexts) use(k kube.KubeContext, newTab bool) {
	w, env, svc := c.w, c.env(), c.w.app.be.Kube
	go func() {
		command, err := svc.ShellConfig(env, k.Name, k.File, "")
		onMainAsync(func() {
			switch {
			case err != nil:
				w.toast(err.Error())
			case !newTab && kubeShellIdle(w):
				w.runInPane(command)
				w.toast("This shell now uses " + k.Name)
			default:
				w.model.Open(uicore.OpenOptions{Command: command})
				w.toast("Opened a tab using " + k.Name)
			}
		})
	}()
}

// confirmEdit shows the planned changes to the kubeconfig, then makes them.
func (c *kubeContexts) confirmEdit(op kube.Op, title, button, note string) {
	w, env, svc := c.w, c.env(), c.w.app.be.Kube
	go func() {
		steps, err := svc.PlanEdit(env, op)
		onMainAsync(func() {
			if err != nil {
				w.toast(err.Error())
				return
			}
			w.app.confirm(&w.win.Window, title, "wtf will make these changes"+note+". A copy of the kubeconfig is kept first (kube-backups).\n\n• "+
				strings.Join(steps, "\n• "), button, func() { c.apply(op, false) })
		})
	}()
}

// apply makes a planned change; from a dialog, it says only what failed.
func (c *kubeContexts) apply(op kube.Op, fromDialog bool) {
	w, env, versions, svc := c.w, c.env(), c.configs.Versions, c.w.app.be.Kube
	go func() {
		l, err := svc.ApplyEdit(env, op, versions)
		onMainAsync(func() {
			if err != nil {
				w.toast(err.Error())
				if !c.closed {
					c.load()
				}
				return
			}
			if !fromDialog {
				w.toast("Kubeconfig updated")
			}
			if !c.closed {
				c.configs = l
				c.rebuild()
			}
		})
	}()
}

func (c *kubeContexts) testConnection(k kube.KubeContext) {
	key, st, env, svc := kubeKey(k), c.st, c.env(), c.w.app.be.Kube
	st.checks[key] = kubeCheck{busy: true}
	c.rebuild()
	go func() {
		r, err := svc.TestConnection(env, k.Name, k.File)
		onMainAsync(func() {
			if err != nil {
				st.checks[key] = kubeCheck{err: err.Error()}
			} else {
				as := ""
				if r.Username != "" {
					as = " · as " + r.Username
				}
				st.checks[key] = kubeCheck{result: "Connected · " + r.Version + as}
			}
			c.rebuild()
		})
	}()
}

func (c *kubeContexts) menu(k kube.KubeContext) []*menuItem {
	w := c.w
	var items []*menuItem
	if ch, ok := c.st.checks[kubeKey(k)]; ok {
		if tool := uicore.MissingTool(ch.err); tool != "" {
			items = append(items, item(installTitle(tool), func() { w.showTools(tool) }), menuSection)
		}
	}
	items = append(items,
		item("Use in This Shell", func() { c.use(k, false) }),
		item("Open New Tab with Context", func() { c.use(k, true) }),
		item("Show Its Resources", func() {
			c.use(k, false)
			c.tab.choose("cluster")
		}),
		menuSection,
		item("Test Connection", func() { c.testConnection(k) }),
	)
	if strings.HasSuffix(k.ExecCommand, "aws-iam-authenticator") {
		items = append(items, item("Log In with aws eks get-token…", func() {
			c.confirmEdit(kube.Op{Kind: "fixAuth", Name: k.Name, File: k.File}, "Switch "+k.Name+" to aws eks get-token?", "Switch",
				", so it needs only the aws CLI")
		}))
	}
	items = append(items, menuSection,
		item("Set Default Namespace…", func() { c.prompt(k, false) }),
		item("Rename…", func() { c.prompt(k, true) }),
		menuSection,
		item("Copy Name", func() {
			w.win.Clipboard().SetText(k.Name)
			w.toast("Copied context name")
		}))
	if k.Server != "" {
		items = append(items, item("Copy Server URL", func() {
			w.win.Clipboard().SetText(k.Server)
			w.toast("Copied server URL")
		}))
	}
	return append(items,
		item("Show Kubeconfig in Files", func() {
			go func() {
				if err := w.app.be.Files.Reveal(k.File); err != nil {
					onMainAsync(func() { w.toast("Couldn't show it: " + err.Error()) })
				}
			}()
		}),
		menuSection,
		item("Delete…", func() {
			c.confirmEdit(kube.Op{Kind: "delete", Name: k.Name, File: k.File}, "Delete "+k.Name+"?", "Delete Context", "")
		}))
}

func (c *kubeContexts) showAddMenu() {
	b := c.add
	x, y, _ := b.TranslateCoordinates(c.root.FirstChild(), 0, float64(b.Height()))
	c.addMenu.show(x, y, []*menuItem{
		item("Add EKS Cluster…", c.addEKS),
		item("Merge a Kubeconfig File…", c.merge),
	}, nil)
}

// merge picks a kubeconfig file and merges it into the shell's, after
// confirming.
func (c *kubeContexts) merge() {
	d := gtk.NewFileDialog()
	d.SetTitle("Merge a Kubeconfig")
	d.SetAcceptLabel("Merge…")
	d.Open(context.Background(), &c.w.win.Window, func(res gio.AsyncResulter) {
		f, err := d.OpenFinish(res)
		if err != nil || f == nil || c.closed {
			return
		}
		path := f.Path()
		c.confirmEdit(kube.Op{Kind: "merge", Value: path}, "Merge "+uicore.BaseName(path)+"?", "Merge", " to your kubeconfig")
	})
}

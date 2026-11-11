//go:build linux

package gtkui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/dayanraj/wtf/internal/procs"
	"github.com/dayanraj/wtf/internal/uicore"
)

func init() {
	registerSidebarTab(sidebarTabInfo{tab: uicore.SidebarProcesses, label: "Processes", icon: "system-run-symbolic", cmd: "sidebar.processes",
		make: func(w *Window) sidePaneTab { return newProcessTab(w) }})
	extraCSS = append(extraCSS, `
.wtf-side-pill.proc-pid, .wtf-side-pill.proc-cpu, .wtf-side-pill.proc-cpu-idle, .wtf-side-pill.proc-cpu-hot,
.wtf-side-pill.proc-mem { background: none; padding: 0 2px; font-size: 0.85em; font-feature-settings: "tnum"; }
.wtf-side-pill.proc-pid { min-width: 40px; opacity: 0.45; }
.wtf-side-pill.proc-cpu, .wtf-side-pill.proc-cpu-idle, .wtf-side-pill.proc-cpu-hot { min-width: 36px; }
.wtf-side-pill.proc-cpu-idle { opacity: 0.6; }
.wtf-side-pill.proc-cpu-hot { color: @warning_color; font-weight: bold; }
.wtf-side-pill.proc-mem { min-width: 46px; }
`)
}

// The most rows All shows; filtering finds the rest.
const procCap = 300

// procStore is your processes, shared by the app's windows: refreshed by
// whichever Processes tab is on screen. Scope and sort last while the app
// runs. Like the macOS app's ProcStore.
type procStore struct {
	a             *App
	list          []procs.Proc
	err           string
	loaded, busy  bool
	updatedAt     time.Time
	terminalsOnly bool
	sort          uicore.ProcSort
	tabs          map[*processTab]bool
}

var procStores = map[*App]*procStore{}

// procStoreOf is the app's store, made with the scope and sort w saved.
func procStoreOf(w *Window) *procStore {
	s := procStores[w.app]
	if s == nil {
		s = &procStore{a: w.app, terminalsOnly: w.sidebarState.ProcessScope != "all", sort: uicore.ProcSort(w.sidebarState.ProcessSort),
			tabs: map[*processTab]bool{}}
		switch s.sort {
		case uicore.ProcSortCPU, uicore.ProcSortMemory, uicore.ProcSortName:
		default:
			s.sort = uicore.ProcSortCPU
		}
		procStores[w.app] = s
	}
	return s
}

func (s *procStore) changed() {
	for t := range s.tabs {
		t.rebuild()
	}
}

func (s *procStore) refresh() {
	if s.busy {
		return
	}
	s.busy = true
	svc := s.a.be.Procs
	go func() {
		l, err := svc.List()
		onMainAsync(func() {
			if err != nil {
				s.err = err.Error()
			} else {
				s.list, s.err, s.updatedAt = l, "", time.Now()
			}
			s.loaded, s.busy = true, false
			s.changed()
		})
	}()
}

// save keeps the scope and sort in every window's saved state.
func (s *procStore) save() {
	scope := "all"
	if s.terminalsOnly {
		scope = "terminals"
	}
	for _, w := range s.a.windows {
		w.sidebarState.ProcessScope, w.sidebarState.ProcessSort = scope, string(s.sort)
		w.scheduleSave()
	}
	s.changed()
}

var procSorts = []uicore.ProcSort{uicore.ProcSortCPU, uicore.ProcSortMemory, uicore.ProcSortName}

// processTab is the Processes tab: each terminal's process tree, or all
// your processes by CPU, memory or name; end or force quit them. Polled
// only while on screen and the window is in front. Like
// the macOS app's ProcessView.
type processTab struct {
	w         *Window
	st        *procStore
	root      *gtk.Box
	pauseLook func()
	setScope  func(int)
	sortBar   *gtk.Box
	setSort   func(int)
	paused    *gtk.Label
	filter    *gtk.SearchEntry
	note      *gtk.Box
	list      *sideList
	poller    *poller
	unobserve func()
	closed    bool
}

func newProcessTab(w *Window) *processTab {
	p := &processTab{w: w, st: procStoreOf(w)}
	header := newPaneHeader("Processes")
	p.pauseLook = netRefreshTools(w, header, p.st.refresh)
	var scope *gtk.Box
	scope, p.setScope = netScopeSwitch("Show processes from", []string{"Terminals", "All"}, func(i int) {
		p.st.terminalsOnly = i == 0
		p.st.save()
	})
	p.sortBar, p.setSort = netScopeSwitch("Sort by CPU, memory or name", []string{"CPU", "Memory", "A–Z"}, func(i int) {
		p.st.sort = procSorts[i]
		p.st.save()
	})
	p.paused = sideLabel("Paused", "wtf-side-note")
	p.filter = gtk.NewSearchEntry()
	p.filter.SetPlaceholderText("Filter by name, command, PID")
	p.filter.AddCSSClass("wtf-side-filter")
	p.filter.ConnectSearchChanged(p.rebuild)
	p.filter.ConnectStopSearch(func() {
		p.filter.SetText("")
		w.focusActive()
	})
	p.note = gtk.NewBox(gtk.OrientationVertical, 0)
	p.list = newSideList(false)
	p.list.onActivate = func(id string) {
		if x, ok := p.proc(id); ok {
			w.netJump(x.Session)
		}
	}
	p.list.onMenu = func(ids []string, x, y float64) {
		if pr, ok := p.proc(ids[0]); ok {
			p.list.menu.show(x, y, p.menu(pr), nil)
		}
	}

	p.root = gtk.NewBox(gtk.OrientationVertical, 4)
	p.root.Append(header.root)
	p.root.Append(scope)
	p.root.Append(p.paused)
	p.root.Append(p.filter)
	p.root.Append(p.sortBar)
	p.root.Append(p.note)
	p.root.Append(p.list.root)
	p.st.tabs[p] = true
	p.unobserve = w.app.settings.observe(func() {
		p.poller.update(w.app.sidebarInterval())
		p.rebuild()
	})
	p.poller = newPoller(w.app, w, w.app.sidebarInterval(), p.st.refresh)
	p.rebuild()
	return p
}

func (p *processTab) widget() gtk.Widgetter { return p.root }
func (p *processTab) paneChanged()          {}
func (p *processTab) commandFinished()      {}

func (p *processTab) close() {
	p.closed = true
	p.poller.stop()
	p.unobserve()
	delete(p.st.tabs, p)
}

func (p *processTab) proc(id string) (procs.Proc, bool) {
	pid, err := strconv.Atoi(id)
	if err != nil {
		return procs.Proc{}, false
	}
	for _, x := range p.st.list {
		if x.PID == pid {
			return x, true
		}
	}
	return procs.Proc{}, false
}

func (p *processTab) rebuild() {
	if p.closed {
		return
	}
	st, w := p.st, p.w
	if st.terminalsOnly {
		p.setScope(0)
	} else {
		p.setScope(1)
	}
	for i, s := range procSorts {
		if s == st.sort {
			p.setSort(i)
		}
	}
	p.sortBar.SetVisible(!st.terminalsOnly)
	p.pauseLook()
	p.paused.SetVisible(!w.app.settings.config().SidebarAutoRefresh)
	p.paused.SetText(netPausedText(st.updatedAt))
	q := strings.TrimSpace(p.filter.Text())

	var sections []sideSection
	note := ""
	switch {
	case st.err != "":
		setNote(p.note, "Can't list processes", st.err)
		p.list.set(nil)
		return
	case st.terminalsOnly:
		bySession := map[string][]procs.Proc{}
		for _, x := range st.list {
			if x.Session != "" {
				bySession[x.Session] = append(bySession[x.Session], x)
			}
		}
		type group struct {
			key   int
			label string
			here  bool
			rows  []uicore.ProcNode
		}
		var groups []group
		for session, list := range bySession {
			var rows []uicore.ProcNode
			if q == "" {
				rows = uicore.ProcTree(list)
			} else {
				var matching []procs.Proc
				for _, x := range list {
					if uicore.ProcMatches(x, q) {
						matching = append(matching, x)
					}
				}
				for _, x := range uicore.ProcSorted(matching, uicore.ProcSortName) {
					rows = append(rows, uicore.ProcNode{Proc: x})
				}
			}
			if len(rows) == 0 {
				continue
			}
			g := group{key: int(^uint(0) >> 1), label: "Another window", rows: rows}
			if t, _, ok := w.netPlace(session); ok {
				g.key, g.label, g.here = w.model.Index(t.ID), w.model.Title(t), true
			}
			groups = append(groups, g)
		}
		sort.Slice(groups, func(i, j int) bool {
			if groups[i].key != groups[j].key {
				return groups[i].key < groups[j].key
			}
			return groups[i].label < groups[j].label
		})
		for _, g := range groups {
			s := sideSection{title: g.label, tip: "A terminal in another window"}
			if g.here {
				s.tip = "Double-click a process to go to “" + g.label + "”"
			}
			for _, n := range g.rows {
				s.rows = append(s.rows, p.row(n.Proc, n.Depth))
			}
			sections = append(sections, s)
		}
		if st.loaded && len(sections) == 0 {
			note = "No terminal processes yet."
			if q != "" {
				note = "No processes match “" + q + "”."
			}
		}
	default:
		var matching []procs.Proc
		for _, x := range st.list {
			if uicore.ProcMatches(x, q) {
				matching = append(matching, x)
			}
		}
		all := uicore.ProcSorted(matching, st.sort)
		s := sideSection{}
		for i, x := range all {
			if i == procCap {
				s.rows = append(s.rows, sideRow{title: fmt.Sprintf("%d more — filter to find them.", len(all)-procCap), muted: true})
				break
			}
			s.rows = append(s.rows, p.row(x, 0))
		}
		sections = []sideSection{s}
		if st.loaded && len(all) == 0 {
			note = "No processes match “" + q + "”."
		}
	}
	setNote(p.note, "", note)
	p.list.set(sections)
}

func (p *processTab) row(x procs.Proc, depth int) sideRow {
	cpu := "proc-cpu-idle"
	switch {
	case x.CPU >= 50:
		cpu = "proc-cpu-hot"
	case x.CPU >= 10:
		cpu = "proc-cpu"
	}
	return sideRow{id: strconv.Itoa(x.PID), title: x.Name, depth: depth, muted: x.Shell,
		tip: fmt.Sprintf("%s\nPID %d · parent %d · running %s", x.Command, x.PID, x.PPID, uicore.ProcElapsed(x.Elapsed)),
		pills: []sidePill{
			{text: strconv.Itoa(x.PID), class: "proc-pid"},
			{text: uicore.ProcCPU(x.CPU), class: cpu},
			{text: uicore.ProcBytes(x.Memory), class: "proc-mem"},
		}}
}

func (p *processTab) menu(x procs.Proc) []*menuItem {
	w := p.w
	t, _, here := w.netPlace(x.Session)
	var items []*menuItem
	if here {
		items = append(items, item("Go to Tab", func() { w.netJump(x.Session) }))
	}
	pid := strconv.Itoa(x.PID)
	items = append(items,
		item("Show Network Connections", func() {
			ns := netStoreOf(w)
			ns.filter = "pid:" + pid
			ns.changed()
			w.sidebarState.Tab = uicore.SidebarNetwork
			w.layoutSidebar()
		}),
		menuSection,
		item("Copy PID ("+pid+")", func() {
			w.win.Clipboard().SetText(pid)
			w.toast("Copied PID")
		}),
		item("Copy Command", func() {
			w.win.Clipboard().SetText(x.Command)
			w.toast("Copied command")
		}),
		menuSection)
	if x.Shell && here {
		// Interactive shells ignore SIGTERM: close the tab instead.
		items = append(items, item("Close Tab…", func() { w.closeTabs([]int{t.ID}) }))
	} else {
		items = append(items,
			item("End "+x.Name+"…", func() { p.end(x, false) }),
			item("Force Quit "+x.Name+"…", func() { p.end(x, true) }))
	}
	return items
}

// end asks the process to quit (SIGTERM), or force quits it (SIGKILL),
// after a confirmation.
func (p *processTab) end(x procs.Proc, force bool) {
	w := p.w
	title, msg, button := "End "+x.Name+"?", fmt.Sprintf("Process %d will be asked to quit.", x.PID), "End Process"
	if force {
		title, button = "Force quit "+x.Name+"?", "Force Quit"
		msg = fmt.Sprintf("Process %d will stop immediately, without a chance to save or clean up.", x.PID)
	}
	st, svc := p.st, w.app.be.Procs
	w.app.confirm(&w.win.Window, title, msg+"\n\n"+x.Command, button, func() {
		go func() {
			err := svc.Kill(x.PID, force)
			onMainAsync(func() {
				if err != nil {
					w.toast("Couldn't end " + x.Name + ": " + err.Error())
					return
				}
				if force {
					w.toast("Force quit " + x.Name)
				} else {
					w.toast("Asked " + x.Name + " to quit")
				}
				coreglib.TimeoutAdd(400, func() bool {
					st.refresh()
					return false
				})
			})
		}()
	})
}

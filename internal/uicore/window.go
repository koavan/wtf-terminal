package uicore

import (
	"encoding/json"
	"math"
	"os"
	"strings"
	"time"
)

// One window's tabs, panes and groups, and the rules that keep them
// consistent. A port of frontend/src/lib/tabs.svelte.ts (and
// macos/Sources/WtfCore/WindowModel.swift); the views observe it through
// OnChange. No GTK here, so it's tested on its own.
//
// Tab, pane and group ids start at 1; 0 means none.

// LastCommand is the last command a pane's shell finished.
type LastCommand struct {
	Text     string
	ExitCode int
	Duration time.Duration
}

// PaneState is one pane's shell and what's known about it.
type PaneState struct {
	ID int
	// Backend session ("s<N>"), empty until the shell is started.
	SessionID  string
	InitialCwd string
	// Latest known folder of the foreground process (terminal:info).
	Cwd     string
	Process string
	IsShell bool
	// Title set by the running program (OSC 0/2).
	OscTitle string
	// Shell integration seen (a prompt mark arrived).
	Integrated  bool
	Running     bool
	Command     string
	LastCommand *LastCommand
	// The variables the shell reported at its last prompt; nil until then.
	Env map[string]string
	// Typed into the shell once, after it starts.
	InitialCommand string
	// Git identity to switch to once, at start (before InitialCommand).
	GitIdentity string
	// Scrollback brought from another window, written once.
	RestoreBuffer string
}

func newPaneState(id int, cwd string) *PaneState {
	return &PaneState{ID: id, InitialCwd: cwd, Cwd: cwd, IsShell: true}
}

// Busy: a program other than the shell runs, or a command line does.
func (p *PaneState) Busy() bool { return p.Running || (!p.IsShell && p.Process != "") }

// Identity is the git identity this pane uses: what the shell reported,
// else the one pending. Empty for none.
func (p *PaneState) Identity() string {
	if p.Env != nil {
		return p.Env["WTF_GIT_IDENTITY"]
	}
	return p.GitIdentity
}

// Tab is one tab: its split tree and what's focused in it.
type Tab struct {
	ID            int
	Root          *Layout
	FocusedPaneID int
	ZoomedPaneID  int
	// Output arrived while in the background.
	Activity bool
	// A command finished while in the background: true for success.
	Finished    *bool
	GroupID     int
	CustomTitle string
}

// GroupColor is a tab group's colour.
type GroupColor string

const (
	GroupBlue   GroupColor = "blue"
	GroupRed    GroupColor = "red"
	GroupYellow GroupColor = "yellow"
	GroupGreen  GroupColor = "green"
	GroupPink   GroupColor = "pink"
	GroupPurple GroupColor = "purple"
	GroupCyan   GroupColor = "cyan"
	GroupOrange GroupColor = "orange"
	GroupGrey   GroupColor = "grey"
)

// GroupColors in the order new groups take them.
var GroupColors = []GroupColor{GroupBlue, GroupRed, GroupYellow, GroupGreen, GroupPink, GroupPurple, GroupCyan, GroupOrange, GroupGrey}

// parseGroupColor is the colour named s, else blue.
func parseGroupColor(s string) GroupColor {
	for _, c := range GroupColors {
		if string(c) == s {
			return c
		}
	}
	return GroupBlue
}

// Hex is the colour in the dark and light themes.
func (c GroupColor) Hex(dark bool) string {
	pick := func(d, l string) string {
		if dark {
			return d
		}
		return l
	}
	switch c {
	case GroupBlue:
		return pick("#8ab4f8", "#1a73e8")
	case GroupRed:
		return pick("#f28b82", "#d93025")
	case GroupYellow:
		return pick("#fdd663", "#c98a00")
	case GroupGreen:
		return pick("#81c995", "#188038")
	case GroupPink:
		return pick("#ff8bcb", "#d01884")
	case GroupPurple:
		return pick("#c58af9", "#a142f4")
	case GroupCyan:
		return pick("#78d9ec", "#007b83")
	case GroupOrange:
		return pick("#fcad70", "#e8710a")
	default:
		return pick("#a9adb8", "#5f6368")
	}
}

// TabGroup is a named, coloured run of tabs.
type TabGroup struct {
	ID        int
	Name      string
	Color     GroupColor
	Collapsed bool
}

// MaxTitle is the longest custom tab title, in characters.
const MaxTitle = 60

const maxReopen = 20

// OpenOptions are where and how Open opens a tab.
type OpenOptions struct {
	// Nil: the active pane's folder.
	Cwd *string
	// Nil: at the end.
	At          *int
	Group       int
	Command     string
	GitIdentity string
}

// WindowModel is one window's tabs, panes and groups.
type WindowModel struct {
	tabs     []Tab
	groups   []TabGroup
	activeID int
	// Panes by id; Tabs hold their ids.
	Panes          map[int]*PaneState
	recentlyClosed []string
	nextTab        int
	nextPane       int
	nextGroup      int
	home           string

	// OnEmpty: the last tab went, the window closes.
	OnEmpty func()
	// OnChange: something changed, redraw and save the session.
	OnChange func()
}

// NewWindowModel is an empty window. home shortens paths in titles; empty
// means the user's home folder.
func NewWindowModel(home string) *WindowModel {
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	return &WindowModel{Panes: map[int]*PaneState{}, nextTab: 1, nextPane: 1, nextGroup: 1, home: home}
}

func (w *WindowModel) changed() {
	if w.OnChange != nil {
		w.OnChange()
	}
}

// Lookups

func (w *WindowModel) Tabs() []Tab        { return append([]Tab(nil), w.tabs...) }
func (w *WindowModel) Groups() []TabGroup { return append([]TabGroup(nil), w.groups...) }
func (w *WindowModel) ActiveID() int      { return w.activeID }
func (w *WindowModel) Tab(id int) (Tab, bool) {
	if i := w.Index(id); i >= 0 {
		return w.tabs[i], true
	}
	return Tab{}, false
}
func (w *WindowModel) Active() (Tab, bool) { return w.Tab(w.activeID) }

// Index is the tab's position, -1 when it isn't here.
func (w *WindowModel) Index(id int) int {
	for i, t := range w.tabs {
		if t.ID == id {
			return i
		}
	}
	return -1
}

func (w *WindowModel) groupIndex(id int) int {
	for i, g := range w.groups {
		if g.ID == id {
			return i
		}
	}
	return -1
}

func (w *WindowModel) Group(id int) (TabGroup, bool) {
	if i := w.groupIndex(id); i >= 0 {
		return w.groups[i], true
	}
	return TabGroup{}, false
}

func (w *WindowModel) TabsIn(group int) []Tab {
	var out []Tab
	for _, t := range w.tabs {
		if t.GroupID == group {
			out = append(out, t)
		}
	}
	return out
}

func (w *WindowModel) IsHidden(t Tab) bool {
	g, ok := w.Group(t.GroupID)
	return ok && g.Collapsed
}

func (w *WindowModel) Visible() []Tab {
	var out []Tab
	for _, t := range w.tabs {
		if !w.IsHidden(t) {
			out = append(out, t)
		}
	}
	return out
}

func (w *WindowModel) FocusedPane(t Tab) *PaneState { return w.Panes[t.FocusedPaneID] }

func (w *WindowModel) ActivePane() *PaneState {
	if t, ok := w.Active(); ok {
		return w.FocusedPane(t)
	}
	return nil
}

func (w *WindowModel) TabOf(pane int) (Tab, bool) {
	for _, t := range w.tabs {
		if t.Root.contains(pane) {
			return t, true
		}
	}
	return Tab{}, false
}

func (w *WindowModel) PaneBySession(session string) *PaneState {
	for _, p := range w.Panes {
		if p.SessionID != "" && p.SessionID == session {
			return p
		}
	}
	return nil
}

func (w *WindowModel) update(id int, f func(*Tab)) {
	if i := w.Index(id); i >= 0 {
		f(&w.tabs[i])
	}
}

// Titles

// PrettyPath is "~" for home and below it: the way paths are shown.
func PrettyPath(path, home string) string {
	if home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+"/") {
		return "~" + path[len(home):]
	}
	return path
}

// PaneTitle is what a pane is called: the running program, or the folder of
// an idle shell.
func PaneTitle(p *PaneState, home string) string {
	if !p.IsShell && p.Process != "" {
		if p.OscTitle != "" {
			return p.OscTitle
		}
		if p.Process == "ssh" && p.Running && strings.HasPrefix(p.Command, "ssh ") {
			return p.Command
		}
		return p.Process
	}
	path := PrettyPath(p.Cwd, home)
	if path == "" {
		return "Terminal"
	}
	if path == "~" || path == "/" {
		return path
	}
	parts := strings.FieldsFunc(path, func(r rune) bool { return r == '/' })
	if len(parts) == 0 {
		return path
	}
	return parts[len(parts)-1]
}

func (w *WindowModel) Title(t Tab) string {
	if t.CustomTitle != "" {
		return t.CustomTitle
	}
	if p := w.FocusedPane(t); p != nil {
		return PaneTitle(p, w.home)
	}
	return "Terminal"
}

// CleanTitle collapses whitespace, trims, and keeps at most MaxTitle
// characters; empty means automatic.
func CleanTitle(s string) string {
	r := []rune(strings.Join(strings.Fields(s), " "))
	if len(r) > MaxTitle {
		r = r[:MaxTitle]
	}
	return string(r)
}

func (w *WindowModel) Rename(id int, draft string) {
	t, ok := w.Tab(id)
	if !ok {
		return
	}
	// Typing the automatic name back keeps the tab automatic.
	if t.CustomTitle == "" && strings.TrimSpace(draft) == w.Title(t) {
		return
	}
	w.update(id, func(t *Tab) { t.CustomTitle = CleanTitle(draft) })
	w.changed()
}

// Tabs

func (w *WindowModel) newPane(cwd string) *PaneState {
	p := newPaneState(w.nextPane, cwd)
	w.nextPane++
	w.Panes[p.ID] = p
	return p
}

// Open opens a one-pane tab, activates it, and returns its id.
func (w *WindowModel) Open(o OpenOptions) int {
	cwd := ""
	if o.Cwd != nil {
		cwd = *o.Cwd
	} else if ap := w.ActivePane(); ap != nil {
		cwd = ap.Cwd
	}
	p := w.newPane(cwd)
	p.InitialCommand = o.Command
	p.GitIdentity = o.GitIdentity
	t := Tab{ID: w.nextTab, Root: Leaf(p.ID), FocusedPaneID: p.ID, GroupID: o.Group}
	w.nextTab++
	at := len(w.tabs)
	if o.At != nil {
		at = min(max(0, *o.At), len(w.tabs))
	}
	w.tabs = insertTab(w.tabs, at, t)
	w.activate(t.ID, false)
	w.changed()
	return t.ID
}

func insertTab(tabs []Tab, at int, t Tab) []Tab {
	tabs = append(tabs, Tab{})
	copy(tabs[at+1:], tabs[at:])
	tabs[at] = t
	return tabs
}

func removeTab(tabs []Tab, i int) ([]Tab, Tab) {
	t := tabs[i]
	return append(tabs[:i], tabs[i+1:]...), t
}

// OpenNextTo opens a tab right after id, in its group: a copy of its folder
// and identity when duplicating, else in the active pane's folder. 0 when
// id isn't here.
func (w *WindowModel) OpenNextTo(id int, duplicate bool) int {
	i := w.Index(id)
	if i < 0 {
		return 0
	}
	t := w.tabs[i]
	at := i + 1
	o := OpenOptions{At: &at, Group: t.GroupID}
	if src := w.FocusedPane(t); duplicate && src != nil {
		cwd := src.Cwd
		o.Cwd = &cwd
		o.GitIdentity = src.Identity()
	}
	return w.Open(o)
}

// Duplicate copies the active tab; 0 when there's none.
func (w *WindowModel) Duplicate() int {
	if w.activeID == 0 {
		return 0
	}
	return w.OpenNextTo(w.activeID, true)
}

// nearestVisible is the nearest visible tab to an index, looking right
// first; 0 when there's none.
func (w *WindowModel) nearestVisible(i int) int {
	n := len(w.tabs)
	if n == 0 {
		return 0
	}
	start := min(i, n-1)
	for d := 0; d <= n; d++ {
		js := []int{start + d, start - d}
		if d == 0 {
			js = js[:1]
		}
		for _, j := range js {
			if j >= 0 && j < n && !w.IsHidden(w.tabs[j]) {
				return w.tabs[j].ID
			}
		}
	}
	return 0
}

// activeIndex is the active tab's position, else 0.
func (w *WindowModel) activeIndex() int {
	if i := w.Index(w.activeID); i >= 0 {
		return i
	}
	return 0
}

func (w *WindowModel) Close(id int) { w.CloseMany([]int{id}) }

func (w *WindowModel) CloseMany(ids []int) {
	closing := map[int]bool{}
	for _, id := range ids {
		closing[id] = true
	}
	activeIndex := w.activeIndex()
	wasActive := w.activeID != 0 && closing[w.activeID]
	kept := w.tabs[:0:0]
	for _, t := range w.tabs {
		if !closing[t.ID] {
			kept = append(kept, t)
			continue
		}
		if p := w.FocusedPane(t); p != nil {
			w.recentlyClosed = append(w.recentlyClosed, p.Cwd)
			if len(w.recentlyClosed) > maxReopen {
				w.recentlyClosed = w.recentlyClosed[1:]
			}
		}
		for _, l := range t.Root.Leaves() {
			delete(w.Panes, l)
		}
	}
	w.tabs = kept
	w.prune()
	if len(w.tabs) == 0 {
		w.activeID = 0
		w.changed()
		if w.OnEmpty != nil {
			w.OnEmpty()
		}
		return
	}
	if wasActive {
		if next := w.nearestVisible(min(activeIndex, len(w.tabs)-1)); next != 0 {
			w.activate(next, false)
		} else {
			empty := ""
			w.Open(OpenOptions{Cwd: &empty})
		}
	}
	w.changed()
}

// Reopen opens the last closed tab's folder again.
func (w *WindowModel) Reopen() {
	n := len(w.recentlyClosed)
	if n == 0 {
		return
	}
	cwd := w.recentlyClosed[n-1]
	w.recentlyClosed = w.recentlyClosed[:n-1]
	w.Open(OpenOptions{Cwd: &cwd})
}

func (w *WindowModel) Activate(id int) { w.activate(id, true) }

func (w *WindowModel) activate(id int, notify bool) {
	t, ok := w.Tab(id)
	if !ok {
		return
	}
	if gi := w.groupIndex(t.GroupID); t.GroupID != 0 && gi >= 0 {
		w.groups[gi].Collapsed = false
	}
	w.activeID = id
	w.update(id, func(t *Tab) {
		t.Activity = false
		t.Finished = nil
	})
	if notify {
		w.changed()
	}
}

func wrap(i, n int) int { return (i%n + n) % n }

func indexOfTab(ts []Tab, id int) int {
	for i, t := range ts {
		if t.ID == id {
			return i
		}
	}
	return -1
}

// Cycle activates the next or previous visible tab, wrapping.
func (w *WindowModel) Cycle(step int) {
	v := w.Visible()
	if len(v) == 0 {
		return
	}
	i := max(indexOfTab(v, w.activeID), 0)
	w.Activate(v[wrap(i+step, len(v))].ID)
}

// SelectNumber is Ctrl+1…8: the n-th visible tab (1-based); 9: the last.
func (w *WindowModel) SelectNumber(n int) {
	v := w.Visible()
	if n == 9 {
		if len(v) > 0 {
			w.Activate(v[len(v)-1].ID)
		}
		return
	}
	if n >= 1 && n <= len(v) {
		w.Activate(v[n-1].ID)
	}
}

func (w *WindowModel) SelectLast() { w.SelectNumber(9) }

// Order and groups

// MoveBefore moves a tab before another (0: to the end), then settles its group.
func (w *WindowModel) MoveBefore(id, before int) {
	i := w.Index(id)
	if i < 0 || id == before {
		return
	}
	var t Tab
	w.tabs, t = removeTab(w.tabs, i)
	j := w.Index(before)
	if j < 0 {
		j = len(w.tabs)
	}
	w.tabs = insertTab(w.tabs, j, t)
	w.settle(id)
	w.prune()
	w.changed()
}

// settle: a moved tab joins the open group around it, stays in its own
// group next to a member or when it's the group's only tab, else leaves it.
func (w *WindowModel) settle(id int) {
	i := w.Index(id)
	if i < 0 {
		return
	}
	openGroup := func(j int) int {
		if j < 0 || j >= len(w.tabs) || w.tabs[j].GroupID == 0 {
			return 0
		}
		if g, ok := w.Group(w.tabs[j].GroupID); !ok || g.Collapsed {
			return 0
		}
		return w.tabs[j].GroupID
	}
	left, right := openGroup(i-1), openGroup(i+1)
	mine := w.tabs[i].GroupID
	switch {
	case left != 0 && left == right:
		w.tabs[i].GroupID = left
	case mine != 0 && (left == mine || right == mine):
		return
	case mine != 0 && len(w.TabsIn(mine)) == 1:
		return
	default:
		w.tabs[i].GroupID = 0
	}
}

// MoveActive moves the active tab one slot among the visible tabs.
func (w *WindowModel) MoveActive(step int) {
	id := w.activeID
	if id == 0 {
		return
	}
	v := w.Visible()
	i := indexOfTab(v, id)
	if i < 0 {
		return
	}
	j := i + step
	if j < 0 || j >= len(v) {
		return
	}
	if step < 0 {
		w.MoveBefore(id, v[j].ID)
	} else if j+1 < len(v) {
		w.MoveBefore(id, v[j+1].ID)
	} else {
		w.MoveBefore(id, 0)
	}
}

// prune drops groups without tabs.
func (w *WindowModel) prune() {
	used := map[int]bool{}
	for _, t := range w.tabs {
		if t.GroupID != 0 {
			used[t.GroupID] = true
		}
	}
	kept := w.groups[:0:0]
	for _, g := range w.groups {
		if used[g.ID] {
			kept = append(kept, g)
		}
	}
	w.groups = kept
}

func (w *WindowModel) lastIndexIn(g int) int {
	for i := len(w.tabs) - 1; i >= 0; i-- {
		if w.tabs[i].GroupID == g {
			return i
		}
	}
	return -1
}

// moveAfterGroup moves a tab to just after a group's last member (not
// counting itself).
func (w *WindowModel) moveAfterGroup(id, g int) {
	i := w.Index(id)
	if i < 0 {
		return
	}
	var t Tab
	w.tabs, t = removeTab(w.tabs, i)
	at := min(i, len(w.tabs))
	if last := w.lastIndexIn(g); last >= 0 {
		at = last + 1
	}
	w.tabs = insertTab(w.tabs, at, t)
}

// CreateGroup is "Add Tab to New Group": the first unused colour. 0 when
// the tab isn't here.
func (w *WindowModel) CreateGroup(id int) int {
	t, ok := w.Tab(id)
	if !ok {
		return 0
	}
	used := map[GroupColor]bool{}
	for _, g := range w.groups {
		used[g.Color] = true
	}
	color := GroupColors[len(w.groups)%len(GroupColors)]
	for _, c := range GroupColors {
		if !used[c] {
			color = c
			break
		}
	}
	if t.GroupID != 0 {
		w.moveAfterGroup(id, t.GroupID)
	}
	g := TabGroup{ID: w.nextGroup, Color: color}
	w.nextGroup++
	w.groups = append(w.groups, g)
	w.update(id, func(t *Tab) { t.GroupID = g.ID })
	w.prune()
	w.changed()
	return g.ID
}

func (w *WindowModel) AddToGroup(id, g int) {
	t, ok := w.Tab(id)
	if !ok || w.groupIndex(g) < 0 || t.GroupID == g {
		return
	}
	w.moveAfterGroup(id, g)
	w.update(id, func(t *Tab) { t.GroupID = g })
	if gi := w.groupIndex(g); w.activeID == id && gi >= 0 {
		w.groups[gi].Collapsed = false
	}
	w.prune()
	w.changed()
}

func (w *WindowModel) RemoveFromGroup(id int) {
	t, ok := w.Tab(id)
	if !ok || t.GroupID == 0 {
		return
	}
	w.moveAfterGroup(id, t.GroupID)
	w.update(id, func(t *Tab) { t.GroupID = 0 })
	w.prune()
	w.changed()
}

func (w *WindowModel) Ungroup(g int) {
	for i := range w.tabs {
		if w.tabs[i].GroupID == g {
			w.tabs[i].GroupID = 0
		}
	}
	w.prune()
	w.changed()
}

func (w *WindowModel) CloseGroup(g int) {
	var ids []int
	for _, t := range w.TabsIn(g) {
		ids = append(ids, t.ID)
	}
	w.CloseMany(ids)
}

// SetGroup renames or recolours a group; nil leaves that part.
func (w *WindowModel) SetGroup(g int, name *string, color *GroupColor) {
	gi := w.groupIndex(g)
	if gi < 0 {
		return
	}
	if name != nil {
		w.groups[gi].Name = *name
	}
	if color != nil {
		w.groups[gi].Color = *color
	}
	w.changed()
}

func (w *WindowModel) ToggleCollapsed(g int) {
	gi := w.groupIndex(g)
	if gi < 0 {
		return
	}
	w.groups[gi].Collapsed = !w.groups[gi].Collapsed
	if a, ok := w.Active(); w.groups[gi].Collapsed && ok && a.GroupID == g {
		at := max(w.Index(a.ID), 0)
		if next := w.nearestVisible(at); next != 0 {
			w.activate(next, false)
		} else {
			w.Open(OpenOptions{})
		}
	}
	w.changed()
}

// NewTabInGroup opens a tab after the group's last member; 0 when the
// group has none.
func (w *WindowModel) NewTabInGroup(g int) int {
	last := w.lastIndexIn(g)
	if last < 0 {
		return 0
	}
	at := last + 1
	return w.Open(OpenOptions{At: &at, Group: g})
}

// Panes

// Split splits the active tab's focused pane; the new pane gets focus.
// cwd nil: the focused pane's folder.
func (w *WindowModel) Split(dir SplitDir, cwd *string, command string) (int, bool) {
	t, ok := w.Active()
	if !ok {
		return 0, false
	}
	src := w.FocusedPane(t)
	if src == nil {
		return 0, false
	}
	c := src.Cwd
	if cwd != nil {
		c = *cwd
	}
	p := w.newPane(c)
	p.GitIdentity = src.Identity()
	p.InitialCommand = command
	w.update(t.ID, func(t *Tab) {
		t.Root = t.Root.Splitting(src.ID, dir, p.ID)
		t.ZoomedPaneID = 0
		t.FocusedPaneID = p.ID
	})
	w.changed()
	return p.ID, true
}

func (w *WindowModel) FocusPane(id int) {
	t, ok := w.TabOf(id)
	if !ok || t.FocusedPaneID == id {
		return
	}
	w.update(t.ID, func(t *Tab) { t.FocusedPaneID = id })
	w.changed()
}

func (w *WindowModel) FocusDirection(dir FocusDir) {
	t, ok := w.Active()
	if !ok || t.ZoomedPaneID != 0 {
		return
	}
	if n, ok := t.Root.Neighbour(t.FocusedPaneID, dir); ok {
		w.FocusPane(n)
	}
}

func (w *WindowModel) CyclePane(step int) {
	t, ok := w.Active()
	if !ok || t.ZoomedPaneID != 0 {
		return
	}
	l := t.Root.Leaves()
	i := -1
	for j, p := range l {
		if p == t.FocusedPaneID {
			i = j
			break
		}
	}
	if len(l) <= 1 || i < 0 {
		return
	}
	w.FocusPane(l[wrap(i+step, len(l))])
}

func (w *WindowModel) ToggleZoom() {
	t, ok := w.Active()
	if !ok {
		return
	}
	switch {
	case t.ZoomedPaneID != 0:
		w.update(t.ID, func(t *Tab) { t.ZoomedPaneID = 0 })
	case t.Root.IsSplit():
		w.update(t.ID, func(t *Tab) { t.ZoomedPaneID = t.FocusedPaneID })
	default:
		return
	}
	w.changed()
}

// EvenOut gives each split its share of the panes it holds, so N panes get
// 1/N each.
func (w *WindowModel) EvenOut(id int) {
	var even func(l *Layout) *Layout
	even = func(l *Layout) *Layout {
		if !l.IsSplit() {
			return l
		}
		r := float64(len(l.A.Leaves())) / float64(len(l.Leaves()))
		return &Layout{Dir: l.Dir, Ratio: r, A: even(l.A), B: even(l.B)}
	}
	w.update(id, func(t *Tab) { t.Root = even(t.Root) })
	w.changed()
}

func (w *WindowModel) SetRatio(tab int, path []bool, r float64) {
	w.update(tab, func(t *Tab) { t.Root = t.Root.SettingRatio(path, r) })
	w.changed()
}

// ClosePane removes a pane; the tab goes with its last pane.
func (w *WindowModel) ClosePane(id int) {
	t, ok := w.TabOf(id)
	if !ok {
		return
	}
	root := t.Root.Removing(id)
	if root == nil {
		w.CloseMany([]int{t.ID})
		return
	}
	before := t.Root.Leaves()
	delete(w.Panes, id)
	w.update(t.ID, func(tab *Tab) {
		tab.Root = root
		if tab.ZoomedPaneID == id {
			tab.ZoomedPaneID = 0
		}
		if tab.FocusedPaneID == id {
			// The pane before it, else the first.
			tab.FocusedPaneID = root.Leaves()[0]
			for _, p := range before {
				if p == id {
					break
				}
				if root.contains(p) {
					tab.FocusedPaneID = p
				}
			}
		}
	})
	w.changed()
}

func (w *WindowModel) CloseActivePane() {
	if t, ok := w.Active(); ok {
		w.ClosePane(t.FocusedPaneID)
	}
}

// SessionExited: a shell exited, its pane closes without asking.
func (w *WindowModel) SessionExited(session string) {
	if p := w.PaneBySession(session); p != nil {
		w.ClosePane(p.ID)
	}
}

// ApplyInfo takes terminal:info for one of this window's panes.
func (w *WindowModel) ApplyInfo(session, process, cwd string, isShell bool) {
	p := w.PaneBySession(session)
	if p == nil {
		return
	}
	name := strings.TrimPrefix(process, "-")
	if name != p.Process {
		p.OscTitle = ""
	}
	p.Process = name
	p.IsShell = isShell
	if cwd != "" {
		p.Cwd = cwd
	}
	w.changed()
}

// MarkActivity: output arrived for a pane, mark its tab when it's in the
// background.
func (w *WindowModel) MarkActivity(pane int) {
	t, ok := w.TabOf(pane)
	if !ok || t.ID == w.activeID || t.Activity {
		return
	}
	w.update(t.ID, func(t *Tab) { t.Activity = true })
	w.changed()
}

func (w *WindowModel) MarkFinished(pane int, ok bool) {
	t, found := w.TabOf(pane)
	if !found || t.ID == w.activeID {
		return
	}
	w.update(t.ID, func(t *Tab) { t.Finished = &ok })
	w.changed()
}

// Moving between windows

// Detach takes a tab out of this window, its shells still running, for
// another window to adopt. buffers gives each pane's scrollback.
func (w *WindowModel) Detach(id int, buffers func(*PaneState) string) (*TabPayload, bool) {
	i := w.Index(id)
	if i < 0 {
		return nil, false
	}
	t := w.tabs[i]
	var group *PayloadGroup
	if g, ok := w.Group(t.GroupID); t.GroupID != 0 && ok {
		group = &PayloadGroup{Name: g.Name, Color: string(g.Color)}
	}
	saved := w.save(t, true, buffers)
	activeIndex := w.activeIndex()
	for _, l := range t.Root.Leaves() {
		delete(w.Panes, l)
	}
	w.tabs, _ = removeTab(w.tabs, i)
	w.prune()
	if len(w.tabs) == 0 {
		w.activeID = 0
		w.changed()
		if w.OnEmpty != nil {
			w.OnEmpty()
		}
	} else {
		if w.activeID == id {
			if next := w.nearestVisible(min(activeIndex, len(w.tabs)-1)); next != 0 {
				w.activate(next, false)
			}
		}
		w.changed()
	}
	return &TabPayload{Kind: "tab", Tab: saved, Group: group}, true
}

func prefixRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

// leafAt is leaves[*i] when i is in range.
func leafAt(leaves []int, i *int) (int, bool) {
	if i == nil || *i < 0 || *i >= len(leaves) {
		return 0, false
	}
	return leaves[*i], true
}

// Adopt takes in a tab another window detached. False for a malformed payload.
func (w *WindowModel) Adopt(payload *TabPayload) bool {
	if payload == nil || payload.Kind != "tab" {
		return false
	}
	root := w.load(payload.Tab.Root, 0, true)
	if root == nil {
		return false
	}
	gid := 0
	if pg := payload.Group; pg != nil {
		color := parseGroupColor(pg.Color)
		name := prefixRunes(pg.Name, 100)
		for _, g := range w.groups {
			if name != "" && g.Name == name && g.Color == color {
				gid = g.ID
				break
			}
		}
		if gid == 0 {
			g := TabGroup{ID: w.nextGroup, Name: name, Color: color}
			w.nextGroup++
			w.groups = append(w.groups, g)
			gid = g.ID
		}
	}
	leaves := root.Leaves()
	focused, ok := leafAt(leaves, payload.Tab.Focused)
	if !ok {
		focused = leaves[0]
	}
	t := Tab{ID: w.nextTab, Root: root, FocusedPaneID: focused, GroupID: gid, CustomTitle: cleanSaved(payload.Tab.Title)}
	w.nextTab++
	at := len(w.tabs)
	if last := w.lastIndexIn(gid); gid != 0 && last >= 0 {
		at = last + 1
	}
	w.tabs = insertTab(w.tabs, at, t)
	w.activate(t.ID, false)
	w.changed()
	return true
}

func cleanSaved(title *string) string {
	if title == nil {
		return ""
	}
	return CleanTitle(*title)
}

// Session

// Snapshot is the window as session.json saves it; extra are the fields
// this build doesn't use, written back as they came.
func (w *WindowModel) Snapshot(fontSize float64, extra map[string]json.RawMessage) SavedSession {
	tabs := make([]SavedTab, 0, len(w.tabs))
	for _, t := range w.tabs {
		tabs = append(tabs, w.save(t, false, nil))
	}
	groups := make([]SavedGroup, 0, len(w.groups))
	for _, g := range w.groups {
		name, color, collapsed, id := g.Name, string(g.Color), g.Collapsed, g.ID
		groups = append(groups, SavedGroup{ID: &id, Name: &name, Color: &color, Collapsed: &collapsed})
	}
	version, active := 1, w.activeIndex()
	if extra == nil {
		extra = map[string]json.RawMessage{}
	}
	return SavedSession{Version: &version, FontSize: &fontSize, Tabs: tabs, Groups: groups, Active: &active, Extra: extra}
}

func strPtr(s string) *string { return &s }

func (w *WindowModel) save(t Tab, live bool, buffers func(*PaneState) string) SavedTab {
	var node func(l *Layout) *SavedNode
	node = func(l *Layout) *SavedNode {
		if l.IsSplit() {
			r := l.Ratio
			return &SavedNode{Dir: strPtr(string(l.Dir)), Ratio: &r, A: node(l.A), B: node(l.B)}
		}
		p := w.Panes[l.Pane]
		n := &SavedNode{Cwd: strPtr("")}
		if p == nil {
			return n
		}
		n.Cwd = strPtr(p.Cwd)
		if live {
			if p.SessionID != "" {
				n.SessionID = strPtr(p.SessionID)
			}
			if buffers != nil {
				n.Buffer = strPtr(buffers(p))
			}
		} else if id := p.Identity(); id != "" {
			n.GitIdentity = &id
		}
		return n
	}
	leaves := t.Root.Leaves()
	focused := 0
	var zoomed *int
	for i, p := range leaves {
		if p == t.FocusedPaneID {
			focused = i
			break
		}
	}
	for i, p := range leaves {
		if t.ZoomedPaneID != 0 && p == t.ZoomedPaneID {
			z := i
			zoomed = &z
			break
		}
	}
	var title *string
	if t.CustomTitle != "" {
		title = strPtr(t.CustomTitle)
	}
	var group *int
	if t.GroupID != 0 {
		g := t.GroupID
		group = &g
	}
	return SavedTab{GroupID: group, Root: node(t.Root), Focused: &focused, Zoomed: zoomed, Title: title}
}

// load is a saved tree, made of new panes. Nil when unusable. Only a tab
// moving between windows (live) brings its running shells and scrollback.
func (w *WindowModel) load(n *SavedNode, depth int, live bool) *Layout {
	if n == nil || depth > 16 {
		return nil
	}
	if n.Cwd != nil {
		p := w.newPane(*n.Cwd)
		if n.GitIdentity != nil && *n.GitIdentity != "" {
			p.GitIdentity = prefixRunes(*n.GitIdentity, 64)
		}
		if live {
			if n.SessionID != nil {
				p.SessionID = *n.SessionID
			}
			if n.Buffer != nil {
				p.RestoreBuffer = *n.Buffer
			}
		}
		return Leaf(p.ID)
	}
	if n.Dir == nil || (*n.Dir != string(SplitRow) && *n.Dir != string(SplitColumn)) {
		return nil
	}
	a, b := w.load(n.A, depth+1, live), w.load(n.B, depth+1, live)
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	r := 0.5
	if n.Ratio != nil && !math.IsInf(*n.Ratio, 0) && !math.IsNaN(*n.Ratio) {
		r = ClampRatio(*n.Ratio)
	}
	return &Layout{Dir: SplitDir(*n.Dir), Ratio: r, A: a, B: b}
}

// Restore restores a saved window. False when nothing usable is in it.
func (w *WindowModel) Restore(s SavedSession) bool {
	groupIDs := map[int]int{}
	var newGroups []TabGroup
	for _, g := range s.Groups {
		if g.ID == nil {
			continue
		}
		if _, seen := groupIDs[*g.ID]; seen {
			continue
		}
		ng := TabGroup{ID: w.nextGroup, Collapsed: g.Collapsed != nil && *g.Collapsed, Color: GroupBlue}
		if g.Name != nil {
			ng.Name = prefixRunes(*g.Name, 100)
		}
		if g.Color != nil {
			ng.Color = parseGroupColor(*g.Color)
		}
		w.nextGroup++
		groupIDs[*g.ID] = ng.ID
		newGroups = append(newGroups, ng)
	}
	var parsed []Tab
	for _, st := range s.Tabs {
		root := w.load(st.Root, 0, false)
		if root == nil {
			continue
		}
		leaves := root.Leaves()
		focused, ok := leafAt(leaves, st.Focused)
		if !ok {
			focused = leaves[0]
		}
		zoomed := 0
		if root.IsSplit() {
			zoomed, _ = leafAt(leaves, st.Zoomed)
		}
		gid := 0
		if st.GroupID != nil {
			gid = groupIDs[*st.GroupID]
		}
		parsed = append(parsed, Tab{ID: w.nextTab, Root: root, FocusedPaneID: focused, ZoomedPaneID: zoomed,
			GroupID: gid, CustomTitle: cleanSaved(st.Title)})
		w.nextTab++
	}
	if len(parsed) == 0 {
		return false
	}
	// Members of a group come together where its first member is.
	var ordered []Tab
	placed := map[int]bool{}
	for _, t := range parsed {
		if placed[t.ID] {
			continue
		}
		if t.GroupID != 0 {
			for _, m := range parsed {
				if m.GroupID == t.GroupID {
					ordered = append(ordered, m)
					placed[m.ID] = true
				}
			}
		} else {
			ordered = append(ordered, t)
			placed[t.ID] = true
		}
	}
	w.tabs = ordered
	w.groups = newGroups
	w.prune()
	a := w.tabs[0].ID
	if s.Active != nil && *s.Active >= 0 && *s.Active < len(parsed) {
		a = parsed[*s.Active].ID
	}
	w.activate(a, false)
	w.changed()
	return true
}

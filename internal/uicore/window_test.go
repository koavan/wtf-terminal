package uicore

import (
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
)

// The cases of the Svelte build's tabs tests (and
// macos/Tests/WtfCoreTests/WindowModelTests.swift), and its random-operations check.

type setup struct {
	w       *WindowModel
	emptied int
}

func newSetup() *setup {
	s := &setup{w: NewWindowModel("/home/me")}
	s.w.OnEmpty = func() { s.emptied++ }
	return s
}

// open opens n tabs (in /t0, /t1, …) and returns all tab ids in order.
func (s *setup) open(n int) []int {
	for i := 0; i < n; i++ {
		cwd := fmt.Sprintf("/t%d", i)
		s.w.Open(OpenOptions{Cwd: &cwd})
	}
	return s.ids()
}

func (s *setup) ids() []int {
	var out []int
	for _, t := range s.w.Tabs() {
		out = append(out, t.ID)
	}
	return out
}

func (s *setup) groupOf(id int) int {
	t, _ := s.w.Tab(id)
	return t.GroupID
}

func (s *setup) active() Tab {
	t, _ := s.w.Active()
	return t
}

func (s *setup) paneIDs() []int { return s.active().Root.Leaves() }

func ptr[T any](v T) *T { return &v }

// checkInvariants checks the rules every operation must keep.
func checkInvariants(t *testing.T, w *WindowModel, note string) {
	t.Helper()
	seen := map[int]bool{}
	prev := 0
	for _, tab := range w.Tabs() {
		if g := tab.GroupID; g != 0 && g != prev {
			if seen[g] {
				t.Errorf("group %d is split %s", g, note)
			}
			seen[g] = true
		}
		prev = tab.GroupID
	}
	var groupIDs, used []int
	for _, g := range w.Groups() {
		groupIDs = append(groupIDs, g.ID)
	}
	for _, tab := range w.Tabs() {
		if tab.GroupID != 0 && !slices.Contains(used, tab.GroupID) {
			used = append(used, tab.GroupID)
		}
	}
	sort.Ints(groupIDs)
	sort.Ints(used)
	if !slices.Equal(groupIDs, used) {
		t.Errorf("groups exist exactly when used %s: %v vs %v", note, groupIDs, used)
	}
	panes := 0
	for _, tab := range w.Tabs() {
		leaves := tab.Root.Leaves()
		panes += len(leaves)
		if !slices.Contains(leaves, tab.FocusedPaneID) {
			t.Errorf("tab %d focuses a pane it doesn't hold %s", tab.ID, note)
		}
		if z := tab.ZoomedPaneID; z != 0 && !slices.Contains(leaves, z) {
			t.Errorf("tab %d zooms a pane it doesn't hold %s", tab.ID, note)
		}
		for _, p := range leaves {
			if w.Panes[p] == nil {
				t.Errorf("pane %d is missing %s", p, note)
			}
		}
	}
	if len(w.Tabs()) > 0 {
		if a, ok := w.Active(); !ok {
			t.Errorf("active tab exists %s", note)
		} else if w.IsHidden(a) {
			t.Errorf("active tab is visible %s", note)
		}
	}
	if len(w.Panes) != panes {
		t.Errorf("no stray panes %s: %d panes, %d in tabs", note, len(w.Panes), panes)
	}
}

func TestCreatesGroupsWithTheFirstUnusedColour(t *testing.T) {
	s := newSetup()
	ts := s.open(2)
	g1, g2 := s.w.CreateGroup(ts[0]), s.w.CreateGroup(ts[1])
	a, _ := s.w.Group(g1)
	b, _ := s.w.Group(g2)
	if a.Color != GroupBlue || b.Color != GroupRed {
		t.Errorf("colours = %s, %s", a.Color, b.Color)
	}
	if s.groupOf(ts[0]) != g1 {
		t.Errorf("tab not in its group")
	}
	checkInvariants(t, s.w, "")
}

// agent-group: the first tab makes the "agents" group, the next join it;
// a tab in another group moves over.
func TestJoinNamedGroupMakesItOnceThenJoinsIt(t *testing.T) {
	s := newSetup()
	ts := s.open(4)
	other := s.w.CreateGroup(ts[3])
	s.w.JoinNamedGroup(ts[0], "agents")
	g := s.w.GroupNamed("agents")
	if g == 0 || g == other || s.groupOf(ts[0]) != g {
		t.Fatalf("agents group = %d, tab in %d", g, s.groupOf(ts[0]))
	}
	s.w.JoinNamedGroup(ts[2], "agents")
	s.w.JoinNamedGroup(ts[3], "agents")
	if len(s.w.Groups()) != 1 || s.groupOf(ts[2]) != g || s.groupOf(ts[3]) != g {
		t.Fatalf("groups = %+v", s.w.Groups())
	}
	if got := s.ids(); !slices.Equal(got, []int{ts[0], ts[2], ts[3], ts[1]}) {
		t.Errorf("order = %v", got)
	}
	// Joining again changes nothing.
	s.w.JoinNamedGroup(ts[0], "agents")
	if got := s.ids(); !slices.Equal(got, []int{ts[0], ts[2], ts[3], ts[1]}) {
		t.Errorf("order after joining again = %v", got)
	}
	checkInvariants(t, s.w, "")
}

func TestAddingToAGroupMovesTheTabNextToItsMembers(t *testing.T) {
	s := newSetup()
	ts := s.open(3)
	g := s.w.CreateGroup(ts[0])
	s.w.AddToGroup(ts[2], g)
	if got := s.ids(); !slices.Equal(got, []int{ts[0], ts[2], ts[1]}) {
		t.Errorf("order = %v", got)
	}
	if s.groupOf(ts[2]) != g {
		t.Errorf("tab not in the group")
	}
}

func TestRemovingFromTheMiddleStepsOutAfterTheGroup(t *testing.T) {
	s := newSetup()
	ts := s.open(4)
	g := s.w.CreateGroup(ts[0])
	s.w.AddToGroup(ts[1], g)
	s.w.AddToGroup(ts[2], g)
	s.w.RemoveFromGroup(ts[1])
	if got := s.ids(); !slices.Equal(got, []int{ts[0], ts[2], ts[1], ts[3]}) {
		t.Errorf("order = %v", got)
	}
	if s.groupOf(ts[1]) != 0 {
		t.Errorf("tab still grouped")
	}
	checkInvariants(t, s.w, "")
}

func TestDraggingBetweenMembersJoinsAndAwayLeaves(t *testing.T) {
	s := newSetup()
	ts := s.open(4)
	g := s.w.CreateGroup(ts[0])
	s.w.AddToGroup(ts[1], g)
	s.w.MoveBefore(ts[3], ts[1])
	if s.groupOf(ts[3]) != g {
		t.Errorf("didn't join")
	}
	s.w.MoveBefore(ts[3], 0)
	if s.groupOf(ts[3]) != 0 {
		t.Errorf("didn't leave")
	}
	checkInvariants(t, s.w, "")
}

func TestAOneTabGroupTravelsWithItsTab(t *testing.T) {
	s := newSetup()
	ts := s.open(3)
	g := s.w.CreateGroup(ts[0])
	s.w.MoveBefore(ts[0], 0)
	if ids := s.ids(); ids[len(ids)-1] != ts[0] || s.groupOf(ts[0]) != g {
		t.Errorf("order = %v, group = %d", ids, s.groupOf(ts[0]))
	}
	s.w.MoveBefore(ts[0], ts[2])
	if s.groupOf(ts[0]) != g {
		t.Errorf("group lost")
	}
}

func visibleIDs(w *WindowModel) []int {
	var out []int
	for _, t := range w.Visible() {
		out = append(out, t.ID)
	}
	return out
}

func TestCollapsingTheActiveGroupFocusesTheNearestTabOutsideIt(t *testing.T) {
	s := newSetup()
	ts := s.open(3)
	g := s.w.CreateGroup(ts[1])
	s.w.Activate(ts[1])
	s.w.ToggleCollapsed(g)
	if s.w.ActiveID() != ts[2] {
		t.Errorf("active = %d", s.w.ActiveID())
	}
	if got := visibleIDs(s.w); !slices.Equal(got, []int{ts[0], ts[2]}) {
		t.Errorf("visible = %v", got)
	}
	checkInvariants(t, s.w, "")
}

func TestCollapsingTheOnlyVisibleTabsOpensAFreshTab(t *testing.T) {
	s := newSetup()
	ts := s.open(1)
	g := s.w.CreateGroup(ts[0])
	s.w.ToggleCollapsed(g)
	if len(s.w.Tabs()) != 2 || s.w.ActiveID() == ts[0] {
		t.Errorf("tabs = %d, active = %d", len(s.w.Tabs()), s.w.ActiveID())
	}
	checkInvariants(t, s.w, "")
}

func TestActivatingAHiddenTabExpandsItsGroup(t *testing.T) {
	s := newSetup()
	ts := s.open(2)
	g := s.w.CreateGroup(ts[0])
	s.w.Activate(ts[1])
	s.w.ToggleCollapsed(g)
	s.w.Activate(ts[0])
	if grp, _ := s.w.Group(g); grp.Collapsed {
		t.Errorf("group still collapsed")
	}
}

func TestClosingAGroupClosesItsTabsUngroupingKeepsThem(t *testing.T) {
	s := newSetup()
	ts := s.open(3)
	g := s.w.CreateGroup(ts[0])
	s.w.AddToGroup(ts[1], g)
	s.w.Ungroup(g)
	if !slices.Equal(s.ids(), ts) || len(s.w.Groups()) != 0 {
		t.Errorf("ungroup: ids = %v, groups = %v", s.ids(), s.w.Groups())
	}
	g2 := s.w.CreateGroup(ts[0])
	s.w.AddToGroup(ts[1], g2)
	s.w.CloseGroup(g2)
	if got := s.ids(); !slices.Equal(got, []int{ts[2]}) {
		t.Errorf("close group: ids = %v", got)
	}
	checkInvariants(t, s.w, "")
}

func TestClosingTheLastTabClosesTheWindow(t *testing.T) {
	s := newSetup()
	s.w.Close(s.open(1)[0])
	if s.emptied != 1 {
		t.Errorf("emptied = %d", s.emptied)
	}
}

func TestNewTabInAGroupOpensAfterItsLastMember(t *testing.T) {
	s := newSetup()
	ts := s.open(3)
	g := s.w.CreateGroup(ts[0])
	s.w.AddToGroup(ts[1], g)
	s.w.NewTabInGroup(g)
	added := s.w.ActiveID()
	if !slices.Equal(s.ids(), []int{ts[0], ts[1], added, ts[2]}) || s.groupOf(added) != g {
		t.Errorf("ids = %v, group = %d", s.ids(), s.groupOf(added))
	}
}

func TestCyclesAndNumbersOnlyVisibleTabs(t *testing.T) {
	s := newSetup()
	ts := s.open(3)
	g := s.w.CreateGroup(ts[1])
	s.w.Activate(ts[0])
	s.w.ToggleCollapsed(g)
	s.w.Cycle(1)
	if s.w.ActiveID() != ts[2] {
		t.Errorf("cycle: active = %d", s.w.ActiveID())
	}
	s.w.SelectNumber(1)
	if s.w.ActiveID() != ts[0] {
		t.Errorf("select 1: active = %d", s.w.ActiveID())
	}
	s.w.SelectLast()
	if s.w.ActiveID() != ts[2] {
		t.Errorf("select last: active = %d", s.w.ActiveID())
	}
}

func TestMovesTheActiveTabOneSlotAtATime(t *testing.T) {
	s := newSetup()
	ts := s.open(3)
	s.w.Activate(ts[0])
	for _, c := range []struct {
		step int
		want []int
	}{
		{1, []int{ts[1], ts[0], ts[2]}},
		{1, []int{ts[1], ts[2], ts[0]}},
		{1, []int{ts[1], ts[2], ts[0]}},
		{-1, []int{ts[1], ts[0], ts[2]}},
	} {
		s.w.MoveActive(c.step)
		if got := s.ids(); !slices.Equal(got, c.want) {
			t.Errorf("move %d: ids = %v, want %v", c.step, got, c.want)
		}
	}
}

func TestRandomOperationsKeepTheInvariants(t *testing.T) {
	for seed := 1; seed <= 5; seed++ {
		s := newSetup()
		state := uint64(seed) * 2_654_435_761
		rand := func(n int) int {
			state = (state*1_103_515_245 + 12345) & 0x7fff_ffff
			return int(state % uint64(n))
		}
		pickTab := func(xs []Tab) *Tab {
			if len(xs) == 0 {
				return nil
			}
			return &xs[rand(len(xs))]
		}
		w := s.w
		s.open(4)
		dirs := []FocusDir{FocusLeft, FocusRight, FocusUp, FocusDown}
		for step := 0; step < 2000; step++ {
			tab, visibleTab := pickTab(w.Tabs()), pickTab(w.Visible())
			var group *TabGroup
			if gs := w.Groups(); len(gs) > 0 {
				group = &gs[rand(len(gs))]
			}
			op := rand(18)
			if len(w.Tabs()) == 0 {
				s.open(2)
			}
			switch op {
			case 0:
				w.Open(OpenOptions{})
			case 1:
				if tab != nil && len(w.Tabs()) > 1 {
					w.Close(tab.ID)
				}
			case 2:
				if tab != nil {
					w.CreateGroup(tab.ID)
				}
			case 3:
				if tab != nil && group != nil {
					w.AddToGroup(tab.ID, group.ID)
				}
			case 4:
				if tab != nil {
					w.RemoveFromGroup(tab.ID)
				}
			case 5:
				if group != nil {
					w.ToggleCollapsed(group.ID)
				}
			case 6:
				if group != nil && len(w.Tabs()) > len(w.TabsIn(group.ID)) {
					w.Ungroup(group.ID)
				}
			case 7:
				if visibleTab != nil {
					before := 0
					if v := pickTab(w.Visible()); v != nil {
						before = v.ID
					}
					w.MoveBefore(visibleTab.ID, before)
				}
			case 8:
				if rand(2) == 1 {
					w.MoveActive(1)
				} else {
					w.MoveActive(-1)
				}
			case 9:
				if tab != nil {
					w.Activate(tab.ID)
				}
			case 10:
				if rand(2) == 1 {
					w.Cycle(1)
				} else {
					w.Cycle(-1)
				}
			case 11:
				if group != nil {
					w.NewTabInGroup(group.ID)
				}
			case 12:
				w.Duplicate()
			case 13:
				if rand(2) == 1 {
					w.Split(SplitRow, nil, "")
				} else {
					w.Split(SplitColumn, nil, "")
				}
			case 14:
				w.CloseActivePane()
				if len(w.Tabs()) == 0 {
					s.open(2)
				}
			case 15:
				w.FocusDirection(dirs[rand(4)])
			case 16:
				w.ToggleZoom()
			default:
				w.CyclePane(1)
			}
			if len(w.Tabs()) == 0 {
				s.open(2)
			}
			checkInvariants(t, w, fmt.Sprintf("seed %d step %d op %d", seed, step, op))
			if t.Failed() {
				return
			}
			if ts := w.Tabs(); len(ts) > 30 {
				var ids []int
				for _, x := range ts[:15] {
					ids = append(ids, x.ID)
				}
				w.CloseMany(ids)
			}
		}
	}
}

func TestSplitsTheFocusedPaneAndFocusesTheNewOne(t *testing.T) {
	s := newSetup()
	s.w.Open(OpenOptions{Cwd: ptr("/work")})
	first := s.w.ActivePane().ID
	s.w.Split(SplitRow, nil, "")
	p := s.w.ActivePane()
	if len(s.paneIDs()) != 2 || p.ID == first || p.InitialCwd != "/work" {
		t.Errorf("panes = %v, active = %d in %q", s.paneIDs(), p.ID, p.InitialCwd)
	}
}

func TestMovesFocusByDirectionInA2x2Grid(t *testing.T) {
	s := newSetup()
	s.w.Open(OpenOptions{})
	tl := s.w.ActivePane().ID
	s.w.Split(SplitRow, nil, "")
	tr := s.w.ActivePane().ID
	s.w.Split(SplitColumn, nil, "")
	br := s.w.ActivePane().ID
	s.w.FocusPane(tl)
	s.w.Split(SplitColumn, nil, "")
	bl := s.w.ActivePane().ID
	for _, c := range []struct {
		dir  FocusDir
		want int
	}{{FocusRight, br}, {FocusUp, tr}, {FocusLeft, tl}, {FocusDown, bl}, {FocusDown, bl}} {
		s.w.FocusDirection(c.dir)
		if got := s.w.ActivePane().ID; got != c.want {
			t.Errorf("focus %d: pane %d, want %d", c.dir, got, c.want)
		}
	}
}

func TestClosingAPaneGivesItsSpaceToTheSiblingAndFocusToThePrevious(t *testing.T) {
	s := newSetup()
	s.w.Open(OpenOptions{})
	a := s.w.ActivePane().ID
	s.w.Split(SplitRow, nil, "")
	b := s.w.ActivePane().ID
	s.w.Split(SplitColumn, nil, "")
	s.w.CloseActivePane()
	if !slices.Equal(s.paneIDs(), []int{a, b}) || s.w.ActivePane().ID != b || !s.active().Root.IsSplit() {
		t.Errorf("panes = %v, active = %d", s.paneIDs(), s.w.ActivePane().ID)
	}
}

func TestClosingTheLastPaneClosesTheTab(t *testing.T) {
	s := newSetup()
	ts := s.open(2)
	s.w.Activate(ts[1])
	s.w.CloseActivePane()
	if got := s.ids(); !slices.Equal(got, []int{ts[0]}) {
		t.Errorf("ids = %v", got)
	}
}

func TestAShellExitingClosesJustItsPane(t *testing.T) {
	s := newSetup()
	s.w.Open(OpenOptions{})
	s.w.ActivePane().SessionID = "s1"
	s.w.Split(SplitRow, nil, "")
	s.w.ActivePane().SessionID = "s2"
	s.w.SessionExited("s1")
	if len(s.paneIDs()) != 1 || s.w.ActivePane().SessionID != "s2" {
		t.Errorf("panes = %v, active session = %q", s.paneIDs(), s.w.ActivePane().SessionID)
	}
}

func TestZoomOnlyAppliesToSplitTabsAndClearsWhenItsPaneCloses(t *testing.T) {
	s := newSetup()
	s.w.Open(OpenOptions{})
	s.w.ToggleZoom()
	if s.active().ZoomedPaneID != 0 {
		t.Errorf("zoomed a one-pane tab")
	}
	s.w.Split(SplitRow, nil, "")
	s.w.ToggleZoom()
	if s.active().ZoomedPaneID != s.w.ActivePane().ID {
		t.Errorf("zoomed = %d", s.active().ZoomedPaneID)
	}
	s.w.CloseActivePane()
	if s.active().ZoomedPaneID != 0 {
		t.Errorf("zoom kept after its pane closed")
	}
}

func TestRoutesProcessInfoToTheRightPane(t *testing.T) {
	s := newSetup()
	s.w.Open(OpenOptions{})
	a := s.w.ActivePane()
	a.SessionID = "s9"
	s.w.Split(SplitRow, nil, "")
	s.w.ApplyInfo("s9", "-vim", "/src", false)
	if a.Process != "vim" || a.Cwd != "/src" || s.w.ActivePane().Process != "" {
		t.Errorf("process = %q, cwd = %q, other = %q", a.Process, a.Cwd, s.w.ActivePane().Process)
	}
}

func roundTrip(t *testing.T, s SavedSession) SavedSession {
	t.Helper()
	data, err := s.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	out, err := ParseSavedSession(data)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func parseJSON(t *testing.T, text string) SavedSession {
	t.Helper()
	s, err := ParseSavedSession([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRoundTripsTabsGroupsLayoutsFoldersAndFocus(t *testing.T) {
	s := newSetup()
	ts := s.open(3)
	g := s.w.CreateGroup(ts[0])
	s.w.SetGroup(g, ptr("api"), ptr(GroupGreen))
	s.w.AddToGroup(ts[2], g)
	s.w.Activate(ts[2])
	s.w.Split(SplitRow, nil, "")
	s.w.Split(SplitColumn, nil, "")
	s.w.SetRatio(ts[2], nil, 0.3)
	s.w.Activate(ts[1])
	before := roundTrip(t, s.w.Snapshot(14, nil))

	r := newSetup()
	if !r.w.Restore(before) {
		t.Fatal("restore failed")
	}
	after := r.w.Snapshot(14, nil)
	for i := range before.Tabs {
		if !reflect.DeepEqual(after.Tabs[i].Root, before.Tabs[i].Root) {
			t.Errorf("tab %d root differs", i)
		}
		if *after.Tabs[i].Focused != *before.Tabs[i].Focused {
			t.Errorf("tab %d focus differs", i)
		}
	}
	if len(after.Tabs) != len(before.Tabs) || *after.Active != *before.Active {
		t.Errorf("tabs %d/%d, active %d/%d", len(after.Tabs), len(before.Tabs), *after.Active, *before.Active)
	}
	var groups []string
	for _, g := range r.w.Groups() {
		groups = append(groups, fmt.Sprintf("%s %s", g.Name, g.Color))
	}
	if !slices.Equal(groups, []string{"api green"}) {
		t.Errorf("groups = %v", groups)
	}
	var cwds []string
	for _, p := range r.w.Tabs()[1].Root.Leaves() {
		cwds = append(cwds, r.w.Panes[p].InitialCwd)
	}
	if !slices.Equal(cwds, []string{"/t2", "/t2", "/t2"}) {
		t.Errorf("cwds = %v", cwds)
	}
	checkInvariants(t, r.w, "")
}

func identities(w *WindowModel, tab Tab) []string {
	var out []string
	for _, p := range tab.Root.Leaves() {
		out = append(out, w.Panes[p].GitIdentity)
	}
	return out
}

func TestKeepsAPaneOnItsGitIdentityThroughSplitDuplicateAndRestore(t *testing.T) {
	s := newSetup()
	ts := s.open(1)
	tab, _ := s.w.Tab(ts[0])
	s.w.Panes[tab.FocusedPaneID].Env = map[string]string{"PWD": "/t0", "WTF_GIT_IDENTITY": "work"}
	s.w.Split(SplitRow, nil, "")
	if s.w.ActivePane().GitIdentity != "work" {
		t.Errorf("split: identity = %q", s.w.ActivePane().GitIdentity)
	}
	s.w.Duplicate()
	if s.w.ActivePane().GitIdentity != "work" {
		t.Errorf("duplicate: identity = %q", s.w.ActivePane().GitIdentity)
	}
	s.w.Open(OpenOptions{Cwd: ptr("/plain")})
	if s.w.ActivePane().GitIdentity != "" {
		t.Errorf("open: identity = %q", s.w.ActivePane().GitIdentity)
	}
	snap := roundTrip(t, s.w.Snapshot(13, nil))
	root := snap.Tabs[0].Root
	if root == nil || root.A == nil || root.B == nil || root.A.GitIdentity == nil || *root.A.GitIdentity != "work" ||
		root.B.GitIdentity == nil || *root.B.GitIdentity != "work" {
		t.Errorf("saved split lost its identity")
	}
	if !reflect.DeepEqual(snap.Tabs[2].Root, &SavedNode{Cwd: ptr("/plain")}) {
		t.Errorf("saved plain tab = %+v", snap.Tabs[2].Root)
	}
	r := newSetup()
	r.w.Restore(snap)
	if got := identities(r.w, r.w.Tabs()[0]); !slices.Equal(got, []string{"work", "work"}) {
		t.Errorf("restored identities = %v", got)
	}
	if got := identities(r.w, r.w.Tabs()[2]); !slices.Equal(got, []string{""}) {
		t.Errorf("restored plain identities = %v", got)
	}
}

func TestExpandsACollapsedGroupIfItsTabWasActive(t *testing.T) {
	s := newSetup()
	ts := s.open(2)
	s.w.CreateGroup(ts[0])
	snap := s.w.Snapshot(13, nil)
	snap.Groups[0].Collapsed = ptr(true)
	snap.Active = ptr(0)
	r := newSetup()
	r.w.Restore(snap)
	if r.w.Groups()[0].Collapsed {
		t.Errorf("group still collapsed")
	}
	checkInvariants(t, r.w, "")
}

func TestRejectsGarbageAndSkipsMalformedParts(t *testing.T) {
	for _, text := range []string{`{}`, `{"tabs":"nope"}`, `{"tabs":[{"root":{"dir":"row"}}]}`} {
		if newSetup().w.Restore(parseJSON(t, text)) {
			t.Errorf("restored %s", text)
		}
	}
	s := newSetup()
	ok := s.w.Restore(parseJSON(t, `
	{"tabs":[{"root":{"cwd":"/x"},"groupId":7,"focused":99,"zoomed":0},{"root":"bad"},
	         {"root":{"dir":"row","ratio":42,"a":{"cwd":"/l"},"b":{"nonsense":true}}},{"root":{"cwd":"/y"},"groupId":7}],
	 "groups":[{"id":7,"name":"g","color":"chartreuse","collapsed":"yes"}],"active":0}`))
	if !ok {
		t.Fatal("restore failed")
	}
	if len(s.w.Tabs()) != 3 {
		t.Errorf("tabs = %d", len(s.w.Tabs()))
	}
	if g := s.w.Groups()[0]; g.Color != GroupBlue || g.Collapsed {
		t.Errorf("group = %+v", g)
	}
	for _, tab := range s.w.Tabs() {
		if tab.Root.IsSplit() {
			t.Errorf("tab %d is split", tab.ID)
		}
	}
	checkInvariants(t, s.w, "")
}

func TestRegroupsScatteredMembers(t *testing.T) {
	s := newSetup()
	s.w.Restore(parseJSON(t, `
	{"tabs":[{"root":{"cwd":"/1"},"groupId":1},{"root":{"cwd":"/2"}},{"root":{"cwd":"/3"},"groupId":1}],
	 "groups":[{"id":1,"name":"","color":"red","collapsed":false}],"active":0}`))
	var cwds []string
	for _, tab := range s.w.Tabs() {
		cwds = append(cwds, s.w.FocusedPane(tab).Cwd)
	}
	if !slices.Equal(cwds, []string{"/1", "/3", "/2"}) {
		t.Errorf("cwds = %v", cwds)
	}
	checkInvariants(t, s.w, "")
}

func TestKeepsFieldsItDoesNotKnow(t *testing.T) {
	snap := parseJSON(t, `{"version":1,"tabs":[{"root":{"cwd":"/x"}}],"sidebar":{"open":true,"width":300}}`)
	s := newSetup()
	s.w.Restore(snap)
	out, err := s.w.Snapshot(13, snap.Extra).MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"sidebar":{`) {
		t.Errorf("sidebar dropped: %s", out)
	}
}

func TestDetachesATabWithItsSessionsAndAnotherWindowAdoptsIt(t *testing.T) {
	s := newSetup()
	ts := s.open(2)
	g := s.w.CreateGroup(ts[1])
	s.w.SetGroup(g, ptr("api"), nil)
	s.w.Activate(ts[1])
	s.w.ActivePane().SessionID = "s1"
	s.w.Split(SplitRow, nil, "")
	s.w.ActivePane().SessionID = "s2"
	payload, ok := s.w.Detach(ts[1], func(p *PaneState) string { return fmt.Sprintf("buffer-%d", p.ID) })
	if !ok {
		t.Fatal("detach failed")
	}
	if !slices.Equal(s.ids(), []int{ts[0]}) || s.w.ActiveID() != ts[0] || len(s.w.Groups()) != 0 || s.emptied != 0 {
		t.Errorf("after detach: ids = %v, active = %d, groups = %d, emptied = %d", s.ids(), s.w.ActiveID(), len(s.w.Groups()), s.emptied)
	}

	o := newSetup()
	o.open(1)
	data, err := payload.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	moved, err := ParseTabPayload(data)
	if err != nil {
		t.Fatal(err)
	}
	if !o.w.Adopt(&moved) {
		t.Fatal("adopt failed")
	}
	adopted := o.active()
	var sessions []string
	for _, id := range adopted.Root.Leaves() {
		p := o.w.Panes[id]
		sessions = append(sessions, p.SessionID)
		if !strings.HasPrefix(p.RestoreBuffer, "buffer-") {
			t.Errorf("pane %d buffer = %q", id, p.RestoreBuffer)
		}
	}
	if !slices.Equal(sessions, []string{"s1", "s2"}) {
		t.Errorf("sessions = %v", sessions)
	}
	if grp, _ := o.w.Group(adopted.GroupID); grp.Name != "api" || grp.Color != GroupBlue {
		t.Errorf("group = %+v", grp)
	}
	if o.w.FocusedPane(adopted).SessionID != "s2" {
		t.Errorf("focused = %q", o.w.FocusedPane(adopted).SessionID)
	}
	checkInvariants(t, o.w, "")
}

func TestJoinsAnExistingGroupWithTheSameNameAndColour(t *testing.T) {
	s := newSetup()
	ts := s.open(1)
	g := s.w.CreateGroup(ts[0])
	s.w.SetGroup(g, ptr("api"), nil)
	s.open(1)
	s.w.Adopt(&TabPayload{Kind: "tab", Tab: SavedTab{Root: &SavedNode{Cwd: ptr("/x"), SessionID: ptr("s9")}, Focused: ptr(0)},
		Group: &PayloadGroup{Name: "api", Color: "blue"}})
	if len(s.w.Groups()) != 1 || len(s.w.TabsIn(g)) != 2 {
		t.Errorf("groups = %d, members = %d", len(s.w.Groups()), len(s.w.TabsIn(g)))
	}
	checkInvariants(t, s.w, "")
}

func TestDetachingTheLastTabEmptiesTheWindow(t *testing.T) {
	s := newSetup()
	s.w.Detach(s.open(1)[0], func(*PaneState) string { return "" })
	if s.emptied != 1 {
		t.Errorf("emptied = %d", s.emptied)
	}
}

func TestSavedSessionsNeverCarrySessionIdsIntoARestore(t *testing.T) {
	s := newSetup()
	s.w.Restore(parseJSON(t, `{"tabs":[{"root":{"cwd":"/x","sessionId":"stale","buffer":"x"}}],"groups":[],"active":0}`))
	if p := s.w.ActivePane(); p.SessionID != "" || p.RestoreBuffer != "" {
		t.Errorf("session = %q, buffer = %q", p.SessionID, p.RestoreBuffer)
	}
}

func TestRejectsMalformedAdoptions(t *testing.T) {
	s := newSetup()
	s.open(1)
	if s.w.Adopt(&TabPayload{Kind: "tab"}) {
		t.Errorf("adopted a tab without a root")
	}
	if s.w.Adopt(&TabPayload{Kind: "other", Tab: SavedTab{Root: &SavedNode{Cwd: ptr("/x")}, Focused: ptr(0)}}) {
		t.Errorf("adopted another kind")
	}
	if len(s.w.Tabs()) != 1 {
		t.Errorf("tabs = %d", len(s.w.Tabs()))
	}
}

func TestACustomNameReplacesTheAutomaticOneUntilCleared(t *testing.T) {
	s := newSetup()
	a := s.open(1)[0]
	title := func() string {
		tab, _ := s.w.Tab(a)
		return s.w.Title(tab)
	}
	if title() != "t0" {
		t.Errorf("automatic = %q", title())
	}
	s.w.Rename(a, "  api   server  ")
	if title() != "api server" {
		t.Errorf("renamed = %q", title())
	}
	s.w.ActivePane().Cwd = "/elsewhere"
	if title() != "api server" {
		t.Errorf("after cd = %q", title())
	}
	s.w.Rename(a, "   ")
	if tab, _ := s.w.Tab(a); tab.CustomTitle != "" || title() != "elsewhere" {
		t.Errorf("cleared: custom = %q, title = %q", tab.CustomTitle, title())
	}
	s.w.Rename(a, strings.Repeat("x", 200))
	if tab, _ := s.w.Tab(a); len([]rune(tab.CustomTitle)) != 60 {
		t.Errorf("long title kept %d characters", len([]rune(tab.CustomTitle)))
	}
}

func TestIgnoresJunkTitlesInASavedSession(t *testing.T) {
	s := newSetup()
	s.w.Restore(parseJSON(t, `{"tabs":[{"root":{"cwd":"/x"},"title":42},{"root":{"cwd":"/y"},"title":""}],"groups":[],"active":0}`))
	for _, tab := range s.w.Tabs() {
		if tab.CustomTitle != "" {
			t.Errorf("tab %d title = %q", tab.ID, tab.CustomTitle)
		}
	}
}

func TestPaneTitles(t *testing.T) {
	p := newPaneState(1, "/home/me/src/api")
	if got := PaneTitle(p, "/home/me"); got != "api" {
		t.Errorf("folder = %q", got)
	}
	p.Cwd = "/home/me"
	if got := PaneTitle(p, "/home/me"); got != "~" {
		t.Errorf("home = %q", got)
	}
	p.Process, p.IsShell, p.Running, p.Command = "ssh", false, true, "ssh web1"
	if got := PaneTitle(p, "/home/me"); got != "ssh web1" {
		t.Errorf("ssh = %q", got)
	}
	p.OscTitle = "remote: vim"
	if got := PaneTitle(p, "/home/me"); got != "remote: vim" {
		t.Errorf("osc = %q", got)
	}
}

// #34: one pane of a split leaves for the background window with its shell;
// the tab keeps the others. A tab's only pane takes the tab, without its group.
func TestDetachesOnePaneWithItsSession(t *testing.T) {
	s := newSetup()
	ts := s.open(2)
	s.w.Activate(ts[1])
	s.w.ActivePane().SessionID = "s1"
	left := s.w.ActivePane().ID
	s.w.Split(SplitRow, nil, "")
	right := s.w.ActivePane().ID
	s.w.ActivePane().SessionID = "s2"
	payload, ok := s.w.DetachPane(right, func(p *PaneState) string { return "buf" })
	if !ok || payload.Tab.Root.SessionID == nil || *payload.Tab.Root.SessionID != "s2" || payload.Tab.Root.Dir != nil {
		t.Fatalf("payload = %+v", payload)
	}
	tab, _ := s.w.Tab(ts[1])
	if leaves := tab.Root.Leaves(); !slices.Equal(leaves, []int{left}) || s.w.Panes[right] != nil || s.emptied != 0 {
		t.Errorf("leaves = %v, right pane = %v", leaves, s.w.Panes[right])
	}
	checkInvariants(t, s.w, "")

	o := newSetup()
	o.open(1)
	if !o.w.Adopt(payload) || o.w.ActivePane().SessionID != "s2" {
		t.Fatalf("adopted %+v", o.w.ActivePane())
	}

	// The only pane: the whole tab, out of its group.
	g := s.w.CreateGroup(ts[1])
	s.w.SetGroup(g, ptr("api"), nil)
	payload, ok = s.w.DetachPane(left, nil)
	if !ok || payload.Group != nil || payload.Tab.GroupID != nil || *payload.Tab.Root.SessionID != "s1" {
		t.Fatalf("payload = %+v", payload)
	}
	if !slices.Equal(s.ids(), []int{ts[0]}) || len(s.w.Groups()) != 0 {
		t.Errorf("ids = %v, groups = %d", s.ids(), len(s.w.Groups()))
	}
	checkInvariants(t, s.w, "")
	if _, ok := s.w.DetachPane(9999, nil); ok {
		t.Error("detached a pane that isn't there")
	}
}

func TestACustomNameSurvivesRestoreAndMovingToAnotherWindow(t *testing.T) {
	s := newSetup()
	a := s.open(2)[0]
	s.w.Rename(a, "db")
	snap := roundTrip(t, s.w.Snapshot(14, nil))
	moved, ok := s.w.Detach(a, func(*PaneState) string { return "" })
	if !ok {
		t.Fatal("detach failed")
	}

	r := newSetup()
	r.w.Restore(snap)
	if tabs := r.w.Tabs(); tabs[0].CustomTitle != "db" || tabs[1].CustomTitle != "" {
		t.Errorf("restored titles %q, %q", tabs[0].CustomTitle, tabs[1].CustomTitle)
	}

	o := newSetup()
	o.open(1)
	data, err := moved.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := ParseTabPayload(data)
	if err != nil {
		t.Fatal(err)
	}
	if !o.w.Adopt(&payload) || o.active().CustomTitle != "db" {
		t.Errorf("adopted title %q", o.active().CustomTitle)
	}
}

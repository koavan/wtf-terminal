package uicore

import "math"

// A tab's panes as a split tree (a port of
// macos/Sources/WtfCore/Layout.swift).

// SplitDir is how a split lays out its two sides.
type SplitDir string

const (
	SplitRow    SplitDir = "row"
	SplitColumn SplitDir = "column"
)

// FocusDir is a direction to move focus between panes.
type FocusDir int

const (
	FocusLeft FocusDir = iota
	FocusRight
	FocusUp
	FocusDown
)

// MinRatio is the smallest share either side of a split gets.
const MinRatio = 0.1

// ClampRatio keeps a split's ratio between MinRatio and 1 - MinRatio.
func ClampRatio(r float64) float64 { return math.Min(1-MinRatio, math.Max(MinRatio, r)) }

// Layout is a pane leaf (A == nil, Pane is its id), or a split of two
// subtrees: SplitRow puts A left of B, SplitColumn puts A above B; Ratio is
// the share A takes. Layouts are values: the methods return new trees and
// never change one, so treat a *Layout as immutable.
type Layout struct {
	Pane  int
	Dir   SplitDir
	Ratio float64
	A, B  *Layout
}

// Leaf is a one-pane layout.
func Leaf(id int) *Layout { return &Layout{Pane: id} }

func (l *Layout) IsSplit() bool { return l != nil && l.A != nil }

// Leaves are the pane ids in reading order: depth first, A before B.
func (l *Layout) Leaves() []int {
	if l == nil {
		return nil
	}
	if !l.IsSplit() {
		return []int{l.Pane}
	}
	return append(l.A.Leaves(), l.B.Leaves()...)
}

func (l *Layout) contains(id int) bool {
	for _, p := range l.Leaves() {
		if p == id {
			return true
		}
	}
	return false
}

// Splitting replaces the leaf id with a split of it and newID (newID goes
// right or below).
func (l *Layout) Splitting(id int, dir SplitDir, newID int) *Layout {
	if !l.IsSplit() {
		if l.Pane == id {
			return &Layout{Dir: dir, Ratio: 0.5, A: Leaf(l.Pane), B: Leaf(newID)}
		}
		return l
	}
	return &Layout{Dir: l.Dir, Ratio: l.Ratio, A: l.A.Splitting(id, dir, newID), B: l.B.Splitting(id, dir, newID)}
}

// Removing is the tree without leaf id: its sibling takes the parent's
// place. Nil when that was the only leaf.
func (l *Layout) Removing(id int) *Layout {
	if !l.IsSplit() {
		if l.Pane == id {
			return nil
		}
		return l
	}
	na, nb := l.A.Removing(id), l.B.Removing(id)
	if na != nil && nb != nil {
		return &Layout{Dir: l.Dir, Ratio: l.Ratio, A: na, B: nb}
	}
	if na != nil {
		return na
	}
	return nb
}

// UnitRect is a rectangle in the unit square.
type UnitRect struct{ X, Y, W, H float64 }

var unitSquare = UnitRect{0, 0, 1, 1}

func (r UnitRect) divided(d SplitDir, ratio float64) (UnitRect, UnitRect) {
	if d == SplitRow {
		return UnitRect{r.X, r.Y, r.W * ratio, r.H}, UnitRect{r.X + r.W*ratio, r.Y, r.W * (1 - ratio), r.H}
	}
	return UnitRect{r.X, r.Y, r.W, r.H * ratio}, UnitRect{r.X, r.Y + r.H*ratio, r.W, r.H * (1 - ratio)}
}

// Frames are the unit-square rectangles of every leaf.
func (l *Layout) Frames() map[int]UnitRect {
	out := map[int]UnitRect{}
	l.frames(unitSquare, out)
	return out
}

func (l *Layout) frames(rect UnitRect, out map[int]UnitRect) {
	if l == nil {
		return
	}
	if !l.IsSplit() {
		if _, ok := out[l.Pane]; !ok {
			out[l.Pane] = rect
		}
		return
	}
	ra, rb := rect.divided(l.Dir, l.Ratio)
	l.A.frames(ra, out)
	l.B.frames(rb, out)
}

// SplitInfo is one split, for drawing its divider. Path addresses it for
// SettingRatio: false goes into A, true into B.
type SplitInfo struct {
	Path  []bool
	Dir   SplitDir
	Ratio float64
	Rect  UnitRect
}

// Splits are every split with its rectangle, depth first.
func (l *Layout) Splits() []SplitInfo {
	var out []SplitInfo
	l.splits(unitSquare, nil, &out)
	return out
}

func (l *Layout) splits(rect UnitRect, path []bool, out *[]SplitInfo) {
	if !l.IsSplit() {
		return
	}
	ra, rb := rect.divided(l.Dir, l.Ratio)
	*out = append(*out, SplitInfo{Path: path, Dir: l.Dir, Ratio: l.Ratio, Rect: rect})
	l.A.splits(ra, extend(path, false), out)
	l.B.splits(rb, extend(path, true), out)
}

// extend is path plus one step, never sharing path's array.
func extend(path []bool, step bool) []bool {
	return append(append(make([]bool, 0, len(path)+1), path...), step)
}

// SettingRatio is the tree with the ratio of the split at path set (clamped).
func (l *Layout) SettingRatio(path []bool, r float64) *Layout {
	if !l.IsSplit() {
		return l
	}
	if len(path) == 0 {
		return &Layout{Dir: l.Dir, Ratio: ClampRatio(r), A: l.A, B: l.B}
	}
	if path[0] {
		return &Layout{Dir: l.Dir, Ratio: l.Ratio, A: l.A, B: l.B.SettingRatio(path[1:], r)}
	}
	return &Layout{Dir: l.Dir, Ratio: l.Ratio, A: l.A.SettingRatio(path[1:], r), B: l.B}
}

// Neighbour is the nearest pane from id in a direction (layout.ts neighbour
// rule); false when there's none (no wrapping). Ties go to the larger
// overlap, then to the first in reading order.
func (l *Layout) Neighbour(id int, dir FocusDir) (int, bool) {
	f := l.Frames()
	me, ok := f[id]
	if !ok {
		return 0, false
	}
	best, bestDist, bestOverlap, found := 0, 0.0, 0.0, false
	for _, other := range l.Leaves() {
		if other == id {
			continue
		}
		r := f[other]
		var dist, overlap float64
		switch dir {
		case FocusLeft:
			dist, overlap = me.X-(r.X+r.W), math.Min(me.Y+me.H, r.Y+r.H)-math.Max(me.Y, r.Y)
		case FocusRight:
			dist, overlap = r.X-(me.X+me.W), math.Min(me.Y+me.H, r.Y+r.H)-math.Max(me.Y, r.Y)
		case FocusUp:
			dist, overlap = me.Y-(r.Y+r.H), math.Min(me.X+me.W, r.X+r.W)-math.Max(me.X, r.X)
		default:
			dist, overlap = r.Y-(me.Y+me.H), math.Min(me.X+me.W, r.X+r.W)-math.Max(me.X, r.X)
		}
		if dist < -1e-6 || overlap <= 1e-6 {
			continue
		}
		if !found || dist < bestDist-1e-6 || (math.Abs(dist-bestDist) <= 1e-6 && overlap > bestOverlap) {
			best, bestDist, bestOverlap, found = other, dist, overlap, true
		}
	}
	return best, found
}

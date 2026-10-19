package uicore

import (
	"slices"
	"strings"
)

// Grid is a terminal's text by absolute row: rows count from the first line
// the terminal ever had, so they hold while old lines are trimmed off the
// top of a full scrollback. VTE (gtkui/vte) is one.
type Grid interface {
	// Row is a row's text without its line end; rows that don't exist (any
	// more) are empty.
	Row(row int) string
	// EndsLine says the row isn't soft-wrapped into the next one.
	EndsLine(row int) bool
	// Cursor is the cursor's column and absolute row.
	Cursor() (col, row int)
}

// Marker is a line of the scrollback that stays put as output scrolls, old
// lines are trimmed, and resizes rewrap the text.
type Marker struct {
	Row, Col int
	// The row's text once it was written (the prompt, for a prompt mark):
	// a row that no longer starts with it was cleared or left the scrollback.
	text string
	// Line ends between this marker's row and the cursor, noted before a
	// rewrap.
	lines int
	gone  bool
}

// Valid says the marker's line is still there.
func (m *Marker) Valid() bool { return m != nil && !m.gone }

// Lines keeps markers on their lines.
//
// The grid gives no way to pin a position, and doesn't say where the
// scrollback starts, so:
//   - each marker keeps its row's text, and one whose row doesn't match it
//     any more is dropped (trimmed from the scrollback, or erased by clear);
//   - a width change rewraps the scrollback, so before one each marker notes
//     how many lines up from the cursor it is, and afterwards finds that
//     line again.
type Lines struct {
	g     Grid
	list  []*Marker
	width int
	cols  int
	noted bool
}

func NewLines(g Grid) *Lines { return &Lines{g: g} }

// Add puts a marker at a row. Its text is read later: the row is still
// being written.
func (l *Lines) Add(row, col int) *Marker {
	l.fill()
	m := &Marker{Row: row, Col: col}
	l.list = append(l.list, m)
	return m
}

// Drop stops tracking markers.
func (l *Lines) Drop(ms ...*Marker) {
	for _, m := range ms {
		if m != nil {
			m.gone = true
		}
	}
	l.list = slices.DeleteFunc(l.list, func(m *Marker) bool { return m.gone })
}

// fill notes the text of markers added before now, which is written by now.
func (l *Lines) fill() {
	for i := len(l.list) - 1; i >= 0 && l.list[i].text == ""; i-- {
		l.list[i].text = l.rowText(l.list[i].Row)
	}
}

func (l *Lines) rowText(row int) string { return strings.TrimRight(l.g.Row(row), " ") }

// Prune drops markers whose rows don't hold their text any more.
func (l *Lines) Prune() {
	k := 0
	for _, m := range l.list {
		if m.text == "" {
			m.text = l.rowText(m.Row)
		}
		cur := l.rowText(m.Row)
		// After a rewrap the row may hold less, or more, of the same line.
		if m.text == "" || cur != "" && (strings.HasPrefix(m.text, cur) || strings.HasPrefix(cur, m.text)) {
			l.list[k] = m
			k++
		} else {
			m.gone = true
		}
	}
	clear(l.list[k:])
	l.list = l.list[:k]
}

// BeforeAllocate runs before the terminal takes a new size, while its rows
// are still wrapped for the old width.
func (l *Lines) BeforeAllocate(width int) {
	if width == l.width {
		return
	}
	l.width = width
	l.Prune()
	if len(l.list) == 0 {
		return
	}
	_, cursor := l.g.Cursor()
	slices.SortStableFunc(l.list, func(a, b *Marker) int { return a.Row - b.Row })
	// Walk up from the cursor counting line ends.
	i, n := len(l.list)-1, 0
	for r := cursor - 1; i >= 0; r-- {
		for i >= 0 && l.list[i].Row > r {
			l.list[i].lines = n
			i--
		}
		if r < 0 {
			break
		}
		if l.g.EndsLine(r) {
			n++
		}
	}
	l.noted = true
}

// GridChanged runs after the terminal resized (and maybe rewrapped).
func (l *Lines) GridChanged(cols int) {
	old := l.cols
	l.cols = cols
	if !l.noted {
		return
	}
	l.noted = false
	if old == cols || old == 0 {
		return
	}
	_, cursor := l.g.Cursor()
	// n counts the line ends in rows r+1 up to the cursor, so when row r
	// ends a line, row r+1 starts the line n lines up.
	i, n := len(l.list)-1, 0
	for r := cursor - 1; r >= -1 && i >= 0; r-- {
		if r < 0 || l.g.EndsLine(r) {
			for i >= 0 && l.list[i].lines == n {
				l.list[i].Row = r + 1
				i--
			}
			n++
		}
	}
	l.Prune()
}

// Markers is every marker kept, for checks.
func (l *Lines) Markers() []*Marker { return l.list }

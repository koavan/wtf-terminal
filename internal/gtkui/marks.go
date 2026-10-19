//go:build linux

package gtkui

import (
	"sort"
	"strings"

	"github.com/dayanraj/wtf/internal/gtkui/vte"
)

// mark is an OSC 133 mark at an absolute row of the terminal.
type mark struct {
	kind     byte // 'A' prompt, 'B' command line, 'C' output, 'D' done
	row, col int
	// The row's text once it was written (the prompt, for an A mark): a
	// row that no longer starts with it was cleared or left the scrollback.
	text string
	// Line ends between this mark's row and the cursor, noted before a
	// rewrap.
	lines int
}

// marks keeps OSC 133 marks on their lines.
//
// VTE gives no way to pin a position, and it doesn't say where the
// scrollback starts (its adjustment is relative), so:
//   - each mark keeps its row's text, and a mark whose row doesn't match it
//     any more is dropped (trimmed from the scrollback, or erased by clear);
//   - VTE rewraps the scrollback when the column count changes, so before a
//     width change each mark notes how many lines up from the cursor it is,
//     and afterwards finds that line again.
type marks struct {
	term  *vte.Terminal
	list  []mark
	width int
	cols  int
	noted bool
}

func newMarks(term *vte.Terminal) *marks {
	m := &marks{term: term}
	term.OnBeforeAllocate(m.beforeAllocate)
	return m
}

// add records a mark. By now the marks before it have their text written.
func (m *marks) add(kind byte, row, col int) {
	for i := len(m.list) - 1; i >= 0 && m.list[i].text == ""; i-- {
		m.list[i].text = m.rowText(m.list[i].row)
	}
	m.list = append(m.list, mark{kind: kind, row: row, col: col})
}

func (m *marks) rowText(row int) string { return strings.TrimRight(m.term.Row(row), " ") }

// prune drops marks whose rows don't hold their text any more.
func (m *marks) prune() {
	k := 0
	for _, mk := range m.list {
		if mk.text == "" {
			mk.text = m.rowText(mk.row)
		}
		cur := m.rowText(mk.row)
		// After a rewrap the row may hold less, or more, of the same line.
		if mk.text == "" || cur != "" && (strings.HasPrefix(mk.text, cur) || strings.HasPrefix(cur, mk.text)) {
			m.list[k] = mk
			k++
		}
	}
	m.list = m.list[:k]
}

func (m *marks) beforeAllocate(width int) {
	if width == m.width {
		return
	}
	m.width = width
	m.prune()
	if len(m.list) == 0 {
		return
	}
	_, cursor := m.term.Cursor()
	sort.SliceStable(m.list, func(i, j int) bool { return m.list[i].row < m.list[j].row })
	// Walk up from the cursor counting line ends.
	i, n := len(m.list)-1, 0
	for r := cursor - 1; i >= 0; r-- {
		for i >= 0 && m.list[i].row > r {
			m.list[i].lines = n
			i--
		}
		if r < 0 {
			break
		}
		if m.term.EndsLine(r) {
			n++
		}
	}
	m.noted = true
}

// gridChanged runs after VTE has resized (and maybe rewrapped).
func (m *marks) gridChanged(cols, rows int) {
	old := m.cols
	m.cols = cols
	if !m.noted {
		return
	}
	m.noted = false
	if old == cols || old == 0 {
		return
	}
	_, cursor := m.term.Cursor()
	// n counts the line ends in rows r+1 up to the cursor, so when row r
	// ends a line, row r+1 starts the line n lines up.
	i, n := len(m.list)-1, 0
	for r := cursor - 1; r >= -1 && i >= 0; r-- {
		if r < 0 || m.term.EndsLine(r) {
			for i >= 0 && m.list[i].lines == n {
				m.list[i].row = r + 1
				i--
			}
			n++
		}
	}
	m.prune()
}

package uicore

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Where the secrets on screen are, for covering them while Hide Secrets is
// on: runs of cells per row. Soft-wrapped lines are read whole, and a
// private key that started above the screen is covered to its end. Like
// lib/secretmask.ts (and macos/Sources/WtfCore/SecretCovers.swift).

// Cell is a run of cells on screen: the row from the top of the view, the
// first column, and how many cells.
type Cell struct{ Row, Col, Width int }

// How far above the screen to look for the start of a private key.
const keyLookback = 200

var (
	keyBegin = regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----`)
	keyEnd   = regexp.MustCompile(`-----END [A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----`)
)

// CellWidth is how many cells a character takes: 2 for wide (East Asian
// wide and fullwidth, most emoji), 0 for combining marks, else 1.
func CellWidth(r rune) int {
	switch {
	case r == 0x200B || unicode.In(r, unicode.Mn, unicode.Me):
		return 0
	case r >= 0x1100 && (r <= 0x115F || r == 0x2329 || r == 0x232A ||
		r >= 0x2E80 && r <= 0xA4CF && r != 0x303F ||
		r >= 0xAC00 && r <= 0xD7A3 ||
		r >= 0xF900 && r <= 0xFAFF ||
		r >= 0xFE30 && r <= 0xFE6F ||
		r >= 0xFF00 && r <= 0xFF60 ||
		r >= 0xFFE0 && r <= 0xFFE6 ||
		r >= 0x1F300 && r <= 0x1F64F ||
		r >= 0x1F900 && r <= 0x1F9FF ||
		r >= 0x20000 && r <= 0x3FFFD):
		return 2
	}
	return 1
}

// logical is a soft-wrapped line as text, with the row, column and cell
// width of the character each byte of it belongs to.
type logical struct {
	text               string
	rows, cols, widths []int
	first, last        int
}

// logicalLine reads the soft-wrapped line through an absolute row.
func logicalLine(g Grid, row int) logical {
	first := row
	for row-first < 40 && !g.EndsLine(first-1) && first > 0 {
		first--
	}
	out := logical{first: first, last: first}
	var b strings.Builder
	for r := first; r-first < 80; r++ {
		text := g.Row(r)
		if g.EndsLine(r) {
			// Trailing blanks aren't part of the line.
			text = strings.TrimRight(text, " ")
		}
		col := 0
		for len(text) > 0 {
			ch, n := utf8.DecodeRuneInString(text)
			w := CellWidth(ch)
			for range n {
				out.rows = append(out.rows, r)
				out.cols = append(out.cols, col)
				out.widths = append(out.widths, w)
			}
			b.WriteString(text[:n])
			text = text[n:]
			col += w
		}
		out.last = r
		if g.EndsLine(r) {
			break
		}
	}
	out.text = b.String()
	return out
}

const utf8Error = '�'

// insideKey says a row is inside a private key that started above it.
func insideKey(g Grid, row int) bool {
	for y := row - 1; y >= max(0, row-keyLookback); y-- {
		text := g.Row(y)
		if keyEnd.MatchString(text) {
			return false
		}
		if keyBegin.MatchString(text) {
			return true
		}
	}
	return false
}

// SecretCells returns the covers for rows [top, top+rows) of the grid, with
// rows counted from top.
func SecretCells(g Grid, top, rows int) []Cell {
	bottom := top + rows - 1
	var covers []Cell
	cover := func(l logical, start, end int) {
		for i := start; i < end; {
			row := l.rows[i]
			j := i
			for j+1 < end && l.rows[j+1] == row {
				j++
			}
			if row >= top && row <= bottom {
				covers = append(covers, Cell{row - top, l.cols[i], l.cols[j] - l.cols[i] + max(1, l.widths[j])})
			}
			i = j + 1
		}
	}
	inKey := insideKey(g, top)
	for y := top; y <= bottom; {
		// Most lines have nothing: check the plain text first.
		quick := g.Row(y)
		if !inKey && g.EndsLine(y) && (y == 0 || g.EndsLine(y-1)) && !keyBegin.MatchString(quick) && len(FindSecrets(quick)) == 0 {
			y++
			continue
		}
		l := logicalLine(g, y)
		text := l.text
		if inKey {
			// Inside a private key: the whole line, up to and including its END.
			upto := len(strings.TrimRight(text, " \t"))
			end := keyEnd.FindStringIndex(text)
			if end != nil {
				upto = end[1]
			}
			cover(l, 0, min(upto, len(l.rows)))
			if end != nil {
				inKey = false
			}
			text = strings.Repeat(" ", upto) + text[upto:]
		}
		for _, s := range FindSecrets(text) {
			if s.End <= len(l.rows) {
				cover(l, s.Start, s.End)
			}
		}
		if b := keyBegin.FindStringIndex(text); b != nil && !keyEnd.MatchString(text[b[0]:]) {
			inKey = true
		}
		y = l.last + 1
	}
	return covers
}

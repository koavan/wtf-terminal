package uicore

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Find in the terminal: every match in the scrollback, for "N of M", and the
// matches on screen, for highlighting. Columns are cells. Like
// macos/Sources/WtfCore/Find.swift.

// Match is a match: an absolute row (or a row on screen), its first column
// and how many cells it takes.
type Match struct{ Row, Col, Width int }

// FindIn returns the matches in rows [first, last] of a grid, left to
// right, not overlapping. Each row is searched on its own.
func FindIn(g Grid, first, last int, query string, caseSensitive bool) []Match {
	if query == "" {
		return nil
	}
	q := query
	if !caseSensitive {
		q = strings.ToLower(q)
	}
	var out []Match
	for row := first; row <= last; row++ {
		text := strings.TrimRight(g.Row(row), " ")
		hay := text
		if !caseSensitive {
			hay = lowerSameLength(text)
		}
		for from := 0; ; {
			i := strings.Index(hay[from:], q)
			if i < 0 {
				break
			}
			a := from + i
			b := a + len(q)
			start := cellsBefore(text, a)
			out = append(out, Match{row, start, max(1, cellsBefore(text, b)-start)})
			from = b
		}
	}
	return out
}

// lowerSameLength lower-cases runes whose lower case is as long in UTF-8,
// so byte offsets stay put.
func lowerSameLength(s string) string {
	return strings.Map(func(r rune) rune {
		if l := []rune(strings.ToLower(string(r)))[0]; utf8.RuneLen(l) == utf8.RuneLen(r) {
			return l
		}
		return r
	}, s)
}

// cellsBefore counts the cells taken by text[:i].
func cellsBefore(text string, i int) int {
	n := 0
	for _, r := range text[:i] {
		n += CellWidth(r)
	}
	return n
}

// FindStatus is "3 of 12", "12 found" or "No results".
func FindStatus(matches []Match, current int) string {
	switch {
	case len(matches) == 0:
		return "No results"
	case current < 0:
		return fmt.Sprintf("%d found", len(matches))
	}
	return fmt.Sprintf("%d of %d", current+1, len(matches))
}

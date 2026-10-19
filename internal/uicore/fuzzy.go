package uicore

import (
	"slices"
	"strings"
	"unicode"
)

// Fuzzy matching for the palette and the tab overview. A port of
// frontend/src/lib/fuzzy.ts: characters in order, rewarding runs, word
// starts and the very start, penalising gaps and long texts.

// FuzzyMatch is how well a query matched a text.
type FuzzyMatch struct {
	Score float64
	// Matched character (rune) positions in the text.
	Indices []int
}

func isBoundary(text []rune, i int) bool {
	return i == 0 || unicode.IsSpace(text[i-1]) || strings.ContainsRune("-_/.:", text[i-1])
}

// Fuzzy matches query against text; ok is false if it doesn't match.
func Fuzzy(query, text string) (m FuzzyMatch, ok bool) {
	var q []rune
	for _, r := range query {
		if !unicode.IsSpace(r) {
			q = append(q, unicode.ToLower(r))
		}
	}
	if len(q) == 0 {
		return FuzzyMatch{}, true
	}
	orig := []rune(text)
	t := make([]rune, len(orig))
	for i, r := range orig {
		t[i] = unicode.ToLower(r)
	}
	ti, prev := 0, -2
	for _, ch := range q {
		found := -1
		for j := ti; j < len(t); j++ {
			if t[j] != ch {
				continue
			}
			if found < 0 {
				found = j
			}
			if j == prev+1 || isBoundary(orig, j) {
				found = j
				break
			}
		}
		if found < 0 {
			return FuzzyMatch{}, false
		}
		m.Score++
		if found == prev+1 {
			m.Score += 7
		}
		if isBoundary(orig, found) {
			m.Score += 5
		}
		if found == 0 {
			m.Score += 4
		}
		m.Score -= float64(min(found-ti, 6)) * 0.25
		m.Indices = append(m.Indices, found)
		prev, ti = found, found+1
	}
	m.Score -= float64(len(t)) * 0.02
	return m, true
}

// Ranked is one entry FuzzyRank kept.
type Ranked[T any] struct {
	Item  T
	Match FuzzyMatch
}

// FuzzyRank returns the entries that match, best first; ties keep their order.
func FuzzyRank[T any](query string, items []T, label func(T) string) []Ranked[T] {
	var out []Ranked[T]
	for _, x := range items {
		if m, ok := Fuzzy(query, label(x)); ok {
			out = append(out, Ranked[T]{x, m})
		}
	}
	slices.SortStableFunc(out, func(a, b Ranked[T]) int {
		switch {
		case a.Match.Score > b.Match.Score:
			return -1
		case a.Match.Score < b.Match.Score:
			return 1
		}
		return 0
	})
	return out
}

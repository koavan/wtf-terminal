package uicore

import "slices"

// KeyTrail keeps the last keys typed on the home page, to spot the codes
// that turn the hidden themes on (config.Retro).
type KeyTrail struct{ last []string }

// trailMax is longer than any code.
const trailMax = 16

// Key adds a key ("up", "down", "left", "right", a letter, or "" for any
// other) and returns which code the keys typed now end with, or -1. The
// trail starts over after a code.
func (t *KeyTrail) Key(name string, codes [][]string) int {
	t.last = append(t.last, name)
	if len(t.last) > trailMax {
		t.last = t.last[len(t.last)-trailMax:]
	}
	for i, c := range codes {
		if n := len(c); n > 0 && len(t.last) >= n && slices.Equal(t.last[len(t.last)-n:], c) {
			t.last = nil
			return i
		}
	}
	return -1
}

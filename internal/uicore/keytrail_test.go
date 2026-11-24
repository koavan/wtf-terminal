package uicore

import (
	"strings"
	"testing"
)

// The cases macos/Tests/WtfCoreTests/KeyTrailTests.swift checks too.
func TestKeyTrail(t *testing.T) {
	konami := strings.Fields("up up down down left right left right b a")
	codes := [][]string{konami, strings.Split("amber", ""), strings.Split("paper", "")}
	for _, c := range []struct {
		keys string
		want []int
	}{
		{"up up down down left right left right b a", []int{0}},
		{"up up up up down down left right left right b a", []int{0}},
		{"up up down down left right left right a b", nil},
		{"up up down down left right left right b x a", nil},
		{"a m b e r", []int{1}},
		{"p a p e r a m b e r", []int{2, 1}},
		{"p a p a p e r", []int{2}},
		{"a m b e r e r", []int{1}},
		{"up up down down left right left right b a p a p e r", []int{0, 2}},
	} {
		var trail KeyTrail
		var got []int
		for _, k := range strings.Fields(c.keys) {
			if i := trail.Key(k, codes); i >= 0 {
				got = append(got, i)
			}
		}
		if len(got) != len(c.want) {
			t.Errorf("%q: got %v, want %v", c.keys, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%q: got %v, want %v", c.keys, got, c.want)
			}
		}
	}
}

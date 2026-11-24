package uicore

import (
	"strings"
	"testing"
)

func typeKonami(k *Konami, keys string) (done int) {
	for _, key := range strings.Fields(keys) {
		if k.Key(key) {
			done++
		}
	}
	return done
}

func TestKonami(t *testing.T) {
	code := "up up down down left right left right b a"
	for _, c := range []struct {
		keys string
		want int
	}{
		{code, 1},
		{code + " " + code, 2},
		{"up up down down left right left right a b", 0},
		{"up " + code, 1},
		{"up up up up " + code, 1},
		{"x " + code + " x", 1},
		{"up up down down left right left right b x a", 0},
		{"up up down down left right left right b up " + code, 1},
	} {
		if got := typeKonami(&Konami{}, c.keys); got != c.want {
			t.Errorf("%q: finished %d times, want %d", c.keys, got, c.want)
		}
	}
}

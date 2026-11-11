package uicore

import (
	"math"
	"testing"
)

// The cases of the Svelte build's themes tests (accentFor, parseHex,
// luminance; xtermTheme was xterm.js-only).

var tokyoNight = []string{"#15161e", "#f7768e", "#9ece6a", "#e0af68", "#7aa2f7", "#bb9af7", "#7dcfff", "#a9b1d6",
	"#414868", "#f7768e", "#9ece6a", "#e0af68", "#7aa2f7", "#bb9af7", "#7dcfff", "#c0caf5"}

func TestParseRGBWithOrWithoutAlpha(t *testing.T) {
	for _, s := range []string{"#ff8000", "#ff800080"} {
		if c, ok := ParseRGB(s); !ok || c.R != 255 || c.G != 128 || c.B != 0 {
			t.Errorf("%s: %+v %v", s, c, ok)
		}
	}
	if _, ok := ParseRGB("rgba(1,2,3,0.5)"); ok {
		t.Error("parsed rgba()")
	}
}

func TestLuminance(t *testing.T) {
	if l := (RGB{0, 0, 0, 1}).Luminance(); l != 0 {
		t.Errorf("black %v", l)
	}
	if l := (RGB{255, 255, 255, 1}).Luminance(); math.Abs(l-1) > 0.005 {
		t.Errorf("white %v", l)
	}
}

func TestAccentForThemes(t *testing.T) {
	if a := Accent("#ff79c6", "#1a1b26", tokyoNight, true); a.Hex() != "#ff79c6" {
		t.Errorf("colorful cursor: %s", a.Hex())
	}
	// Tokyo Night's cursor is its (greyish) foreground: the theme's blue instead.
	if a := Accent("#c0caf5", "#1a1b26", tokyoNight, true); a.Hex() != "#7aa2f7" {
		t.Errorf("grey cursor: %s", a.Hex())
	}
	// Colors that would be unreadable on the background are skipped.
	light := make([]string, 16)
	for i := range light {
		light[i] = "#fdf6e3"
	}
	light[4] = "#268bd2"
	if a := Accent("#fdf6e3", "#fdf6e3", light, false); a.Hex() != "#268bd2" {
		t.Errorf("light theme: %s", a.Hex())
	}
}

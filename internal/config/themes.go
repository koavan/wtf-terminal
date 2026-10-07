package config

import (
	"bufio"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Theme is a terminal color scheme. Colors are CSS color strings.
type Theme struct {
	Name                string     `json:"name"`
	Background          string     `json:"background"`
	Foreground          string     `json:"foreground"`
	Cursor              string     `json:"cursor"`
	CursorText          string     `json:"cursorText"`
	SelectionBackground string     `json:"selectionBackground"`
	SelectionForeground string     `json:"selectionForeground"`
	Palette             [16]string `json:"palette"`
	// Dark is derived from the background's luminance.
	Dark bool `json:"dark"`
	// Builtin is false for themes loaded from the themes directory.
	Builtin bool `json:"builtin"`
}

const (
	DefaultDarkTheme  = "wtf Dark"
	DefaultLightTheme = "wtf Light"
)

func palette(s string) (p [16]string) {
	copy(p[:], strings.Fields(s))
	return p
}

var builtinThemes = []Theme{
	{Name: DefaultDarkTheme, Background: "#16171b", Foreground: "#e4e6eb", Cursor: "#a9b5ff", CursorText: "#16171b",
		SelectionBackground: "rgba(139, 156, 255, 0.30)",
		Palette: palette("#2b2d35 #ff6b7d #7fe0a7 #f3d37e #7ea4ff #c79dff #70d6e6 #c9ccd5 " +
			"#5e6272 #ff919e #a0eec2 #ffe3a1 #a1bdff #d8bbff #98e7f2 #f5f6f9")},
	{Name: DefaultLightTheme, Background: "#fcfcfd", Foreground: "#24292f", Cursor: "#4c5bd4", CursorText: "#fcfcfd",
		SelectionBackground: "rgba(76, 91, 212, 0.18)",
		Palette: palette("#24292f #cf222e #116329 #4d2d00 #0969da #8250df #1b7c83 #6e7781 " +
			"#57606a #a40e26 #1a7f37 #633c01 #218bff #a475f9 #3192aa #8c959f")},
	{Name: "Catppuccin Mocha", Background: "#1e1e2e", Foreground: "#cdd6f4", Cursor: "#f5e0dc", CursorText: "#1e1e2e",
		SelectionBackground: "#585b70",
		Palette: palette("#45475a #f38ba8 #a6e3a1 #f9e2af #89b4fa #f5c2e7 #94e2d5 #bac2de " +
			"#585b70 #f38ba8 #a6e3a1 #f9e2af #89b4fa #f5c2e7 #94e2d5 #a6adc8")},
	{Name: "Catppuccin Latte", Background: "#eff1f5", Foreground: "#4c4f69", Cursor: "#dc8a78", CursorText: "#eff1f5",
		SelectionBackground: "#acb0be",
		Palette: palette("#5c5f77 #d20f39 #40a02b #df8e1d #1e66f5 #ea76cb #179299 #acb0be " +
			"#6c6f85 #d20f39 #40a02b #df8e1d #1e66f5 #ea76cb #179299 #bcc0cc")},
	{Name: "Tokyo Night", Background: "#1a1b26", Foreground: "#c0caf5", Cursor: "#c0caf5", CursorText: "#1a1b26",
		SelectionBackground: "#33467c",
		Palette: palette("#15161e #f7768e #9ece6a #e0af68 #7aa2f7 #bb9af7 #7dcfff #a9b1d6 " +
			"#414868 #f7768e #9ece6a #e0af68 #7aa2f7 #bb9af7 #7dcfff #c0caf5")},
	{Name: "Dracula", Background: "#282a36", Foreground: "#f8f8f2", Cursor: "#f8f8f2", CursorText: "#282a36",
		SelectionBackground: "#44475a",
		Palette: palette("#21222c #ff5555 #50fa7b #f1fa8c #bd93f9 #ff79c6 #8be9fd #f8f8f2 " +
			"#6272a4 #ff6e6e #69ff94 #ffffa5 #d6acff #ff92df #a4ffff #ffffff")},
	{Name: "Nord", Background: "#2e3440", Foreground: "#d8dee9", Cursor: "#d8dee9", CursorText: "#2e3440",
		SelectionBackground: "#434c5e",
		Palette: palette("#3b4252 #bf616a #a3be8c #ebcb8b #81a1c1 #b48ead #88c0d0 #e5e9f0 " +
			"#4c566a #bf616a #a3be8c #ebcb8b #81a1c1 #b48ead #8fbcbb #eceff4")},
	{Name: "Gruvbox Dark", Background: "#282828", Foreground: "#ebdbb2", Cursor: "#ebdbb2", CursorText: "#282828",
		SelectionBackground: "#504945",
		Palette: palette("#282828 #cc241d #98971a #d79921 #458588 #b16286 #689d6a #a89984 " +
			"#928374 #fb4934 #b8bb26 #fabd2f #83a598 #d3869b #8ec07c #ebdbb2")},
	{Name: "One Dark", Background: "#282c34", Foreground: "#abb2bf", Cursor: "#528bff", CursorText: "#282c34",
		SelectionBackground: "#3e4451",
		Palette: palette("#282c34 #e06c75 #98c379 #e5c07b #61afef #c678dd #56b6c2 #abb2bf " +
			"#5c6370 #e06c75 #98c379 #e5c07b #61afef #c678dd #56b6c2 #ffffff")},
	{Name: "Rosé Pine", Background: "#191724", Foreground: "#e0def4", Cursor: "#524f67", CursorText: "#e0def4",
		SelectionBackground: "#403d52",
		Palette: palette("#26233a #eb6f92 #31748f #f6c177 #9ccfd8 #c4a7e7 #ebbcba #e0def4 " +
			"#6e6a86 #eb6f92 #31748f #f6c177 #9ccfd8 #c4a7e7 #ebbcba #e0def4")},
	{Name: "Solarized Dark", Background: "#002b36", Foreground: "#839496", Cursor: "#93a1a1", CursorText: "#002b36",
		SelectionBackground: "#073642",
		Palette: palette("#073642 #dc322f #859900 #b58900 #268bd2 #d33682 #2aa198 #eee8d5 " +
			"#002b36 #cb4b16 #586e75 #657b83 #839496 #6c71c4 #93a1a1 #fdf6e3")},
	{Name: "Solarized Light", Background: "#fdf6e3", Foreground: "#657b83", Cursor: "#586e75", CursorText: "#fdf6e3",
		SelectionBackground: "#eee8d5",
		Palette: palette("#073642 #dc322f #859900 #b58900 #268bd2 #d33682 #2aa198 #eee8d5 " +
			"#002b36 #cb4b16 #586e75 #657b83 #839496 #6c71c4 #93a1a1 #fdf6e3")},
}

func init() {
	for i := range builtinThemes {
		builtinThemes[i].Builtin = true
		builtinThemes[i].Dark = isDark(builtinThemes[i].Background)
	}
}

// Themes returns the built-in themes followed by those in dir, by name.
// Unreadable theme files are reported as problems and skipped.
func Themes(dir string) (themes []Theme, problems []string) {
	themes = append(themes, builtinThemes...)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return themes, nil // no themes dir is fine
	}
	var user []Theme
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		t, err := LoadThemeFile(filepath.Join(dir, e.Name()))
		if err != nil {
			problems = append(problems, fmt.Sprintf("theme %q: %v", e.Name(), err))
			continue
		}
		user = append(user, t)
	}
	sort.Slice(user, func(i, j int) bool { return strings.ToLower(user[i].Name) < strings.ToLower(user[j].Name) })
	return append(themes, user...), problems
}

// FindTheme looks a theme up by name, ignoring case.
func FindTheme(themes []Theme, name string) (Theme, bool) {
	for _, t := range themes {
		if strings.EqualFold(t.Name, name) {
			return t, true
		}
	}
	return Theme{}, false
}

// LoadThemeFile reads an iTerm2 .itermcolors file or a Ghostty theme file.
// The theme is named after the file.
func LoadThemeFile(path string) (Theme, error) {
	f, err := os.Open(path)
	if err != nil {
		return Theme{}, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 1<<20))
	if err != nil {
		return Theme{}, err
	}

	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	var t Theme
	if strings.EqualFold(filepath.Ext(path), ".itermcolors") || bytes.Contains(b[:min(len(b), 512)], []byte("<plist")) {
		t, err = parseITermColors(b)
	} else {
		t, err = parseGhostty(b)
	}
	if err != nil {
		return Theme{}, err
	}
	t.Name = name
	if t.Background == "" || t.Foreground == "" {
		return Theme{}, errors.New("missing background or foreground color")
	}
	fillDefaults(&t)
	t.Dark = isDark(t.Background)
	return t, nil
}

// fillDefaults covers colors a theme file may leave out.
func fillDefaults(t *Theme) {
	if t.Cursor == "" {
		t.Cursor = t.Foreground
	}
	if t.CursorText == "" {
		t.CursorText = t.Background
	}
	if t.SelectionBackground == "" {
		t.SelectionBackground = t.Foreground + "40" // translucent foreground
	}
	base := builtinThemes[0].Palette
	if !isDark(t.Background) {
		base = builtinThemes[1].Palette
	}
	for i, c := range t.Palette {
		if c == "" {
			t.Palette[i] = base[i]
		}
	}
}

var hexColor = regexp.MustCompile(`^#?([0-9a-fA-F]{6})$`)

func normalizeHex(s string) (string, bool) {
	m := hexColor.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return "", false
	}
	return "#" + strings.ToLower(m[1]), true
}

// parseGhostty reads Ghostty's "key = value" theme format:
//
//	background = 282a36
//	palette = 1=#ff5555
func parseGhostty(b []byte) (Theme, error) {
	var t Theme
	sc := bufio.NewScanner(bytes.NewReader(b))
	n := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if key == "palette" {
			idx, color, ok := strings.Cut(value, "=")
			i, err := strconv.Atoi(strings.TrimSpace(idx))
			c, valid := normalizeHex(color)
			if ok && err == nil && valid && i >= 0 && i < 16 {
				t.Palette[i] = c
				n++
			}
			continue
		}
		c, valid := normalizeHex(value)
		if !valid {
			continue
		}
		n++
		switch key {
		case "background":
			t.Background = c
		case "foreground":
			t.Foreground = c
		case "cursor-color":
			t.Cursor = c
		case "cursor-text":
			t.CursorText = c
		case "selection-background":
			t.SelectionBackground = c
		case "selection-foreground":
			t.SelectionForeground = c
		}
	}
	if n == 0 {
		return t, errors.New("no colors found")
	}
	return t, sc.Err()
}

// parseITermColors reads an iTerm2 .itermcolors property list.
func parseITermColors(b []byte) (Theme, error) {
	colors, err := plistColors(b)
	if err != nil {
		return Theme{}, err
	}
	var t Theme
	for i := range 16 {
		t.Palette[i] = colors[fmt.Sprintf("Ansi %d Color", i)]
	}
	t.Background = colors["Background Color"]
	t.Foreground = colors["Foreground Color"]
	t.Cursor = colors["Cursor Color"]
	t.CursorText = colors["Cursor Text Color"]
	t.SelectionBackground = colors["Selection Color"]
	t.SelectionForeground = colors["Selected Text Color"]
	return t, nil
}

// plistColors maps each top-level key of an .itermcolors plist to a hex color.
func plistColors(b []byte) (map[string]string, error) {
	dec := xml.NewDecoder(bytes.NewReader(b))
	colors := map[string]string{}
	depth := 0     // <dict> nesting
	var key string // last top-level <key>
	var comp string
	var rgb map[string]float64

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("not a valid .itermcolors file: %w", err)
		}
		switch el := tok.(type) {
		case xml.StartElement:
			switch el.Name.Local {
			case "dict":
				depth++
				if depth == 2 {
					rgb = map[string]float64{}
				}
			case "key":
				var s string
				if err := dec.DecodeElement(&s, &el); err != nil {
					return nil, err
				}
				if depth == 1 {
					key = s
				} else {
					comp = s
				}
			case "real", "integer":
				var s string
				if err := dec.DecodeElement(&s, &el); err != nil {
					return nil, err
				}
				if depth == 2 {
					v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
					rgb[comp] = v
				}
			}
		case xml.EndElement:
			if el.Name.Local == "dict" {
				if depth == 2 && key != "" {
					colors[key] = fmt.Sprintf("#%02x%02x%02x",
						channel(rgb["Red Component"]), channel(rgb["Green Component"]), channel(rgb["Blue Component"]))
				}
				depth--
			}
		}
	}
	if len(colors) == 0 {
		return nil, errors.New("no colors found")
	}
	return colors, nil
}

func channel(v float64) int {
	return int(math.Round(math.Max(0, math.Min(1, v)) * 255))
}

// isDark reports whether a #rrggbb color is dark (relative luminance < 0.4).
func isDark(color string) bool {
	c, ok := normalizeHex(color)
	if !ok {
		return true
	}
	lin := func(h string) float64 {
		v, _ := strconv.ParseUint(h, 16, 8)
		s := float64(v) / 255
		if s <= 0.04045 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	l := 0.2126*lin(c[1:3]) + 0.7152*lin(c[3:5]) + 0.0722*lin(c[5:7])
	return l < 0.4
}

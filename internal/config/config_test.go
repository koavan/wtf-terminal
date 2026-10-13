package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestParseDefaultsAndOverrides(t *testing.T) {
	cfg, problems, err := Parse(`
font-size = 15
cursor-style = "block"
copy-on-select = true
`)
	if err != nil || len(problems) > 0 {
		t.Fatalf("Parse: %v %v", err, problems)
	}
	if cfg.FontSize != 15 || cfg.CursorStyle != "block" || !cfg.CopyOnSelect {
		t.Errorf("overrides not applied: %+v", cfg)
	}
	if cfg.FontFamily != "JetBrains Mono" || !cfg.ShellIntegration {
		t.Errorf("defaults lost: %+v", cfg)
	}
}

func TestTemplateIsValidAndAllDefaults(t *testing.T) {
	cfg, problems, err := Parse(template)
	if err != nil || len(problems) > 0 {
		t.Fatalf("template: %v %v", err, problems)
	}
	if cfg != Defaults() {
		t.Errorf("template parses to %+v, want defaults", cfg)
	}
	// Every documented line, uncommented, must be a known setting.
	var lines []string
	for _, l := range strings.Split(template, "\n") {
		if strings.HasPrefix(l, "# ") && strings.Contains(l, " = ") && !strings.HasPrefix(l, "#   ") {
			lines = append(lines, strings.TrimPrefix(l, "# "))
		}
	}
	if _, problems, err := Parse(strings.Join(lines, "\n")); err != nil || len(problems) > 0 {
		t.Errorf("uncommented template: %v %v", err, problems)
	}
}

func TestParseReportsProblems(t *testing.T) {
	cfg, problems, err := Parse(`
font-size = 500
cursor-style = "triangle"
colour = "red"
`)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.FontSize != 13 || cfg.CursorStyle != "bar" {
		t.Errorf("bad values not reset: %+v", cfg)
	}
	joined := strings.Join(problems, "\n")
	for _, want := range []string{"font-size", "cursor-style", `unknown setting "colour"`} {
		if !strings.Contains(joined, want) {
			t.Errorf("problems %q missing %q", joined, want)
		}
	}
}

func TestParseSyntaxErrorHasLine(t *testing.T) {
	_, _, err := Parse("font-size = 13\nfont-family = \"unterminated\n")
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("err = %v, want one mentioning line 2", err)
	}
}

func TestThemePair(t *testing.T) {
	themes, _ := Themes(t.TempDir())
	cases := []struct {
		spec, dark, light string
		problems          int
	}{
		{"auto", DefaultDarkTheme, DefaultLightTheme, 0},
		{"", DefaultDarkTheme, DefaultLightTheme, 0},
		{"nord", "Nord", "Nord", 0},
		{"dark:Dracula, light:Solarized Light", "Dracula", "Solarized Light", 0},
		{"light:Catppuccin Latte", DefaultDarkTheme, "Catppuccin Latte", 0},
		{"Nope", DefaultDarkTheme, DefaultLightTheme, 1},
		{"dark:Nord,sideways:Nord", "Nord", DefaultLightTheme, 1},
	}
	for _, c := range cases {
		dark, light, problems := ThemePair(c.spec, themes)
		if dark.Name != c.dark || light.Name != c.light || len(problems) != c.problems {
			t.Errorf("ThemePair(%q) = %q, %q, %v", c.spec, dark.Name, light.Name, problems)
		}
	}
}

func TestBuiltinThemesAreComplete(t *testing.T) {
	for _, th := range builtinThemes {
		for i, c := range th.Palette {
			if c == "" {
				t.Errorf("%s: palette[%d] empty", th.Name, i)
			}
		}
		if th.Background == "" || th.Foreground == "" || th.Cursor == "" {
			t.Errorf("%s: missing base colors", th.Name)
		}
	}
	if !builtinThemes[0].Dark || builtinThemes[1].Dark {
		t.Error("wtf Dark/Light detected the wrong way round")
	}
}

func TestWithTheme(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "theme = \"Nord\"\n"},
		{"font-size = 14", "font-size = 14\ntheme = \"Nord\"\n"},
		{"# comment\n# theme = \"auto\"\nfont-size = 14\n", "# comment\ntheme = \"Nord\"\nfont-size = 14\n"},
		{"# theme = \"auto\"\ntheme = \"Dracula\"\n", "# theme = \"auto\"\ntheme = \"Nord\"\n"},
	}
	for _, c := range cases {
		if got := withTheme(c.in, "Nord"); got != c.want {
			t.Errorf("withTheme(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// The template keeps every comment and gains an active theme line.
	out := withTheme(template, "dark:Nord,light:Solarized Light")
	if cfg, _, err := Parse(out); err != nil || cfg.Theme != "dark:Nord,light:Solarized Light" {
		t.Errorf("template with theme: %v %+v", err, cfg)
	}
	if strings.Count(out, "\n") != strings.Count(template, "\n") {
		t.Error("withTheme changed the number of lines in the template")
	}
}

func TestWithSettingLeavesExamplesAlone(t *testing.T) {
	out := withTheme(template, "Nord")
	if !strings.Contains(out, `#   theme = "Tokyo Night"`) {
		t.Error("the documentation example was rewritten")
	}
	if !strings.Contains(out, "\ntheme = \"Nord\"\n") || strings.Contains(out, `# theme = "auto"`) {
		t.Error("the commented-out default wasn't replaced")
	}

	out = withSetting(template, "sidebar-auto-refresh", "false")
	cfg, problems, err := Parse(out)
	if err != nil || len(problems) > 0 || cfg.SidebarAutoRefresh {
		t.Fatalf("after setting: %+v %v %v", cfg, problems, err)
	}
	if got := withSetting(out, "sidebar-auto-refresh", "true"); strings.Count(got, "sidebar-auto-refresh") != 1 {
		t.Errorf("setting twice duplicated the line:\n%s", got)
	}
}

func TestSidebarRefreshInterval(t *testing.T) {
	cfg, problems, _ := Parse("sidebar-refresh-interval = 0.2\n")
	if cfg.SidebarRefreshInterval != 3 || len(problems) != 1 {
		t.Errorf("interval %v problems %v", cfg.SidebarRefreshInterval, problems)
	}
	cfg, problems, _ = Parse("sidebar-refresh-interval = 10\nsidebar-auto-refresh = false\n")
	if cfg.SidebarRefreshInterval != 10 || cfg.SidebarAutoRefresh || len(problems) != 0 {
		t.Errorf("got %+v %v", cfg, problems)
	}
}

func TestContextBadgesDefaultOn(t *testing.T) {
	cfg, _, _ := Parse("")
	if !cfg.ContextBadges {
		t.Error("context badges should default to on")
	}
	if cfg, _, _ = Parse("context-badges = false\n"); cfg.ContextBadges {
		t.Error("context-badges = false was ignored")
	}
}

func TestLogHighlightingDefaultOn(t *testing.T) {
	cfg, _, _ := Parse("")
	if !cfg.LogHighlighting {
		t.Error("log highlighting should be on by default")
	}
	if cfg, _, _ = Parse("log-highlighting = false\n"); cfg.LogHighlighting {
		t.Error("log-highlighting = false was ignored")
	}
}

func TestRedactSecretsDefaultOff(t *testing.T) {
	cfg, _, _ := Parse("")
	if cfg.RedactSecrets {
		t.Error("redact-secrets should be off by default")
	}
	if cfg, _, _ = Parse("redact-secrets = true\n"); !cfg.RedactSecrets {
		t.Error("redact-secrets = true was ignored")
	}
}

func TestServiceSetSidebarAutoRefresh(t *testing.T) {
	s := NewService(t.TempDir())
	if !s.Current().Config.SidebarAutoRefresh {
		t.Fatal("auto-refresh should default to on")
	}
	if err := s.SetSidebarAutoRefresh(false); err != nil {
		t.Fatal(err)
	}
	if s.Current().Config.SidebarAutoRefresh {
		t.Error("still on after turning it off")
	}
}

func TestServiceSetRedactSecrets(t *testing.T) {
	s := NewService(t.TempDir())
	if err := s.SetRedactSecrets(true); err != nil {
		t.Fatal(err)
	}
	if !s.Current().Config.RedactSecrets {
		t.Error("still off after turning it on")
	}
}

func TestGhosttyTheme(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "My Theme")
	os.WriteFile(path, []byte(`# a ghostty theme
palette = 0=#000000
palette = 1=#ff0000
palette = 15=ffffff
background = 101010
foreground = #EEEEEE
cursor-color = #00ff00
selection-background = #333333
`), 0o644)
	th, err := LoadThemeFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if th.Name != "My Theme" || th.Background != "#101010" || th.Foreground != "#eeeeee" ||
		th.Cursor != "#00ff00" || th.Palette[1] != "#ff0000" || th.Palette[15] != "#ffffff" || !th.Dark {
		t.Errorf("parsed %+v", th)
	}
	if th.Palette[4] == "" || th.CursorText != "#101010" {
		t.Error("missing colors were not filled in")
	}
}

func TestITermTheme(t *testing.T) {
	comp := func(r, g, b float64) string {
		return `<dict><key>Blue Component</key><real>` + ftoa(b) + `</real>
<key>Color Space</key><string>sRGB</string>
<key>Green Component</key><real>` + ftoa(g) + `</real>
<key>Red Component</key><real>` + ftoa(r) + `</real></dict>`
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Ansi 1 Color</key>` + comp(1, 0, 0) + `
<key>Background Color</key>` + comp(1, 1, 1) + `
<key>Foreground Color</key>` + comp(0.2, 0.2, 0.2) + `
<key>Cursor Color</key>` + comp(0, 0, 1) + `
</dict></plist>`
	path := filepath.Join(t.TempDir(), "Paper.itermcolors")
	os.WriteFile(path, []byte(plist), 0o644)
	th, err := LoadThemeFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if th.Name != "Paper" || th.Background != "#ffffff" || th.Foreground != "#333333" ||
		th.Cursor != "#0000ff" || th.Palette[1] != "#ff0000" || th.Dark {
		t.Errorf("parsed %+v", th)
	}
}

func TestThemesDirReportsBadFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "empty"), []byte("nothing here"), 0o644)
	os.WriteFile(filepath.Join(dir, "Good"), []byte("background = #000000\nforeground = #ffffff\n"), 0o644)
	themes, problems := Themes(dir)
	if _, ok := FindTheme(themes, "good"); !ok {
		t.Error("good theme not loaded")
	}
	if len(problems) != 1 || !strings.Contains(problems[0], "empty") {
		t.Errorf("problems = %v", problems)
	}
}

func TestServiceSetThemeAndReload(t *testing.T) {
	dir := t.TempDir()
	s := NewService(dir)
	if got := s.Current().Dark.Name; got != DefaultDarkTheme {
		t.Fatalf("default dark theme = %q", got)
	}
	if err := s.SetTheme("Tokyo Night"); err != nil {
		t.Fatal(err)
	}
	r := s.Current()
	if r.Dark.Name != "Tokyo Night" || r.Light.Name != "Tokyo Night" || len(r.Problems) > 0 {
		t.Errorf("after SetTheme: %+v", r)
	}
	// A broken edit keeps the last good settings and reports the problem.
	os.WriteFile(filepath.Join(dir, "config.toml"), []byte("theme = \"Nord\nfont-size = 14\n"), 0o644)
	r = s.reload()
	if r.Dark.Name != "Tokyo Night" || len(r.Problems) == 0 {
		t.Errorf("after broken edit: dark=%q problems=%v", r.Dark.Name, r.Problems)
	}
}

func ftoa(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

func TestClipboardClear(t *testing.T) {
	if cfg, _, _ := Parse(""); cfg.ClipboardClear != 30 {
		t.Fatalf("default = %v, want 30", cfg.ClipboardClear)
	}
	if cfg, problems, _ := Parse("clipboard-clear = 0\n"); cfg.ClipboardClear != 0 || len(problems) != 0 {
		t.Fatalf("0 = %v %v, want 0 (off)", cfg.ClipboardClear, problems)
	}
	if cfg, problems, _ := Parse("clipboard-clear = 9000\n"); cfg.ClipboardClear != 30 || len(problems) != 1 {
		t.Fatalf("9000 = %v %v, want the default and a problem", cfg.ClipboardClear, problems)
	}
}

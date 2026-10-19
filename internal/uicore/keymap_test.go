package uicore

import (
	"os"
	"regexp"
	"testing"
)

func TestKeymapIsValidAndUnique(t *testing.T) {
	ids, keys := map[string]bool{}, map[string]string{}
	for _, c := range Commands {
		if ids[c.ID] {
			t.Errorf("%s listed twice", c.ID)
		}
		ids[c.ID] = true
		if c.Keys == "" {
			continue
		}
		if _, _, err := splitKeys(c.Keys); err != nil {
			t.Error(err)
		}
		a := Accel(c.Keys)
		if other, ok := keys[a]; ok {
			t.Errorf("%s and %s both use %s", other, c.ID, c.Keys)
		}
		keys[a] = c.ID
	}
}

// Plain Ctrl+letter belongs to the shell (Ctrl-C, Ctrl-R, Ctrl-W…), and
// these Ctrl+Alt keys to the desktop.
func TestKeymapLeavesShellAndDesktopKeys(t *testing.T) {
	letter := regexp.MustCompile(`^Ctrl\+[A-Z]$`)
	desktop := map[string]bool{"Ctrl+Alt+T": true, "Ctrl+Alt+L": true, "Ctrl+Alt+D": true, "Ctrl+Alt+Delete": true,
		"Ctrl+Alt+Left": true, "Ctrl+Alt+Right": true, "Ctrl+Alt+Up": true, "Ctrl+Alt+Down": true}
	for _, c := range Commands {
		if letter.MatchString(c.Keys) || desktop[c.Keys] {
			t.Errorf("%s takes %s", c.ID, c.Keys)
		}
	}
}

// Every command in the macOS menus has a Linux entry.
func TestKeymapCoversMacOSMenus(t *testing.T) {
	src, err := os.ReadFile("../../macos/Sources/Wtf/main.swift")
	if err != nil {
		t.Fatal(err)
	}
	found := regexp.MustCompile(`cmd\("[^"]+", "([^"\\]+)"`).FindAllStringSubmatch(string(src), -1)
	if len(found) < 50 {
		t.Fatalf("only %d macOS commands found", len(found))
	}
	for _, m := range found {
		if _, ok := CommandByID(m[1]); !ok {
			t.Errorf("no Linux command for %s", m[1])
		}
	}
	for i := 1; i <= 8; i++ {
		if _, ok := CommandByID("tab.select." + string(rune('0'+i))); !ok {
			t.Errorf("no tab.select.%d", i)
		}
	}
}

func TestAccelAndLabel(t *testing.T) {
	for keys, want := range map[string][2]string{
		"Ctrl+Shift+T":        {"<Control><Shift>t", "Ctrl+Shift+T"},
		"Ctrl+Comma":          {"<Control>comma", "Ctrl+,"},
		"Ctrl+Shift+PageDown": {"<Control><Shift>Page_Down", "Ctrl+Shift+Page Down"},
		"Alt+9":               {"<Alt>9", "Alt+9"},
		"F1":                  {"F1", "F1"},
		"Ctrl+Bar":            {"<Control>bar", "Ctrl+Shift+\\"},
		"Ctrl+Nope":           {"", ""},
		"":                    {"", ""},
	} {
		if a, l := Accel(keys), KeyLabel(keys); a != want[0] || l != want[1] {
			t.Errorf("%q → %q, %q; want %q", keys, a, l, want)
		}
	}
}

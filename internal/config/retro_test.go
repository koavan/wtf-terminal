package config

import "testing"

func TestRetroThemes(t *testing.T) {
	names := map[string]bool{}
	for _, r := range Retro {
		if names[r.Theme.Name] {
			t.Errorf("%s twice", r.Theme.Name)
		}
		names[r.Theme.Name] = true
		if len(r.Code) == 0 || r.Toast == "" {
			t.Errorf("%s has no code or toast", r.Theme.Name)
		}
		if _, ok := FindTheme(builtinThemes, r.Theme.Name); ok {
			t.Errorf("%s is listed with the other themes", r.Theme.Name)
		}
		for i, c := range r.Theme.Palette {
			if c == "" {
				t.Errorf("%s: no color %d", r.Theme.Name, i)
			}
		}
	}
	dark := map[string]bool{"Phosphor": true, "Amber": true, "Aged Paper": false}
	for _, r := range Retro {
		if want, ok := dark[r.Theme.Name]; ok && r.Theme.Dark != want {
			t.Errorf("%s: dark %v, want %v", r.Theme.Name, r.Theme.Dark, want)
		}
	}
}

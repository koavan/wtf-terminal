package uicore

import (
	"slices"
	"testing"
	"time"
)

func TestSecretCellsCoverSecretsOnScreen(t *testing.T) {
	g := &fakeGrid{wrapped: map[int]bool{}, rows: []string{
		"nothing here",
		"export GITHUB_TOKEN=abc123def456",
		"日本 AKIAABCDEFGHIJKLMNOP",
	}}
	got := SecretCells(g, 0, 3)
	want := []Cell{{1, 20, 12}, {2, 5, 20}}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	// Rows count from the top of the view.
	if got := SecretCells(g, 2, 1); !slices.Equal(got, []Cell{{0, 5, 20}}) {
		t.Errorf("scrolled: %v", got)
	}
}

func TestSecretCellsFollowWrappedLines(t *testing.T) {
	// A token split over two rows by a soft wrap.
	g := &fakeGrid{wrapped: map[int]bool{0: true}, rows: []string{
		"token: ghp_abcdefghijklmnopq",
		"rstuvwxyz0123456789 ok",
	}}
	got := SecretCells(g, 0, 2)
	want := []Cell{{0, 7, 21}, {1, 0, 19}}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSecretCellsCoverAPrivateKeyFromAbove(t *testing.T) {
	g := &fakeGrid{wrapped: map[int]bool{}, rows: []string{
		"-----BEGIN OPENSSH PRIVATE KEY-----",
		"b3BlbnNzaC1rZXktdjEAAAAA",
		"-----END OPENSSH PRIVATE KEY----- after",
		"plain",
	}}
	got := SecretCells(g, 1, 3)
	want := []Cell{{0, 0, 24}, {1, 0, 33}}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestFindIn(t *testing.T) {
	g := &fakeGrid{wrapped: map[int]bool{}, rows: []string{"Error: one error", "日本 error"}}
	got := FindIn(g, 0, 1, "error", false)
	want := []Match{{0, 0, 5}, {0, 11, 5}, {1, 5, 5}}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got := FindIn(g, 0, 1, "Error", true); len(got) != 1 {
		t.Errorf("case-sensitive: %v", got)
	}
	if s := FindStatus(want, 1); s != "2 of 3" {
		t.Errorf("status %q", s)
	}
	if s := FindStatus(nil, -1); s != "No results" {
		t.Errorf("status %q", s)
	}
}

func TestThemeColors(t *testing.T) {
	if c := CoverColor("#ffffff", "#000000"); c.Hex() != "#666666" {
		t.Errorf("cover %s", c.Hex())
	}
	if c, _ := ParseRGB("#ff000080"); c.A != 128.0/255 {
		t.Errorf("alpha %v", c.A)
	}
	if _, ok := ParseRGB("red"); ok {
		t.Error("parsed red")
	}
	if a := Accent("#888888", "#000000", []string{12: "#5c7cff"}, true); a.Hex() != "#5c7cff" {
		t.Errorf("accent %s", a.Hex())
	}
}

func TestDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{
		400 * time.Millisecond: "0.4s", 12400 * time.Millisecond: "12s",
		185 * time.Second: "3m 5s", 7800 * time.Second: "2h 10m",
	} {
		if got := Duration(d); got != want {
			t.Errorf("%v → %q, want %q", d, got, want)
		}
	}
}

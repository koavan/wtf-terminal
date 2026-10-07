package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func names(ws []Window) string {
	var out []string
	for _, w := range ws {
		out = append(out, w.Name+"="+string(w.Session))
	}
	return strings.Join(out, " ")
}

func TestMissingFileHasNoWindows(t *testing.T) {
	if got := NewStore(t.TempDir()).Windows(); len(got) != 0 {
		t.Fatalf("Windows() = %v", got)
	}
}

func TestSaveForgetKeepsOrder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "wtf")
	s := NewStore(dir)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(s.SaveWindow("main", `{"a":1}`))
	must(s.SaveWindow("w2", `{"b":2}`))
	must(s.SaveWindow("main", `{"a":3}`))
	if got := names(s.Windows()); got != `main={"a":3} w2={"b":2}` {
		t.Fatalf("after saves: %s", got)
	}
	must(s.Forget("main"))
	if got := names(s.Windows()); got != `w2={"b":2}` {
		t.Fatalf("after forget: %s", got)
	}

	fi, err := os.Stat(filepath.Join(dir, sessionFile))
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("session file mode %v is readable by others", perm)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("dir has %d entries, want just the session file", len(entries))
	}
}

func TestReadsVersion1File(t *testing.T) {
	dir := t.TempDir()
	v1 := `{"version":1,"tabs":[],"active":0}`
	os.WriteFile(filepath.Join(dir, sessionFile), []byte(v1), 0o600)
	got := NewStore(dir).Windows()
	if len(got) != 1 || got[0].Name != "main" || string(got[0].Session) != v1 {
		t.Fatalf("Windows() = %s", names(got))
	}
}

func TestSaveRejectsBadInput(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.SaveWindow("main", "{not json"); err == nil {
		t.Error("saved invalid JSON")
	}
	if err := s.SaveWindow("main", `"`+strings.Repeat("x", maxSessionBytes)+`"`); err == nil {
		t.Error("saved an oversized session")
	}
}

func TestCorruptFileStartsFresh(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, sessionFile), []byte("{trunc"), 0o600)
	s := NewStore(dir)
	if got := s.Windows(); len(got) != 0 {
		t.Fatalf("Windows() = %s", names(got))
	}
	// And saving over it works.
	if err := s.SaveWindow("main", `{}`); err != nil || len(s.Windows()) != 1 {
		t.Fatalf("save over corrupt file: %v", err)
	}
}

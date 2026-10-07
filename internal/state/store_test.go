package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMissingIsEmpty(t *testing.T) {
	got, err := NewStore(t.TempDir()).Load()
	if err != nil || got != "" {
		t.Fatalf("Load() = %q, %v; want empty, nil", got, err)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "wtf")
	s := NewStore(dir)
	for _, want := range []string{`{"version":1,"tabs":[]}`, `{"version":1,"tabs":[{"a":1}]}`} {
		if err := s.Save(want); err != nil {
			t.Fatal(err)
		}
		got, err := s.Load()
		if err != nil || got != want {
			t.Fatalf("Load() = %q, %v; want %q", got, err, want)
		}
	}

	fi, err := os.Stat(filepath.Join(dir, sessionFile))
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("session file mode %v is readable by others", perm)
	}
	// No temp files left behind.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("dir has %d entries, want just the session file", len(entries))
	}
}

func TestSaveRejectsBadInput(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Save("{not json"); err == nil {
		t.Error("saved invalid JSON")
	}
	if err := s.Save(`"` + strings.Repeat("x", maxSessionBytes) + `"`); err == nil {
		t.Error("saved an oversized session")
	}
}

func TestLoadCorruptStartsFresh(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, sessionFile), []byte("{trunc"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := NewStore(dir).Load()
	if err != nil || got != "" {
		t.Fatalf("Load() = %q, %v; want empty, nil", got, err)
	}
}

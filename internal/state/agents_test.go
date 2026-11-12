package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBackgroundAgents(t *testing.T) {
	dir := t.TempDir()
	s := NewService(NewStore(dir))
	tr := filepath.Join(dir, "abc.jsonl")
	if err := os.WriteFile(tr, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	good := Agent{Session: "0b1c-22", Dir: dir, Name: "fixer", Transcript: tr}
	gone := Agent{Session: "x1", Dir: dir, Transcript: filepath.Join(dir, "gone.jsonl")}
	bad := Agent{Session: "x; rm -rf ~", Dir: dir, Transcript: tr}
	noDir := Agent{Session: "x2", Dir: filepath.Join(dir, "nope"), Transcript: tr}
	if got := s.BackgroundAgents(); len(got) != 0 {
		t.Fatalf("empty = %v", got)
	}
	if err := s.SaveBackgroundAgents([]Agent{good, gone, bad, noDir}); err != nil {
		t.Fatal(err)
	}
	if got := s.BackgroundAgents(); len(got) != 1 || got[0] != good {
		t.Fatalf("read = %+v", got)
	}
	if got := s.StillResumable([]Agent{good, gone, bad, noDir}); !got[0] || got[1] || got[2] || got[3] {
		t.Fatalf("still = %v", got)
	}
	if err := s.SaveBackgroundAgents(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, backgroundFile)); !os.IsNotExist(err) {
		t.Fatalf("file kept: %v", err)
	}
	if err := s.SaveBackgroundAgents(nil); err != nil {
		t.Fatal("removing twice:", err)
	}
}

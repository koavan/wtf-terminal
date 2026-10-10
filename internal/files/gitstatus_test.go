package files

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestParseStatus(t *testing.T) {
	out := "# branch.oid 1234567890abcdef\x00# branch.head main\x00# branch.upstream origin/main\x00# branch.ab +2 -1\x00" +
		"1 .M N... 100644 100644 100644 aaa aaa src/app.go\x00" +
		"1 A. N... 000000 100644 100644 000 bbb new.go\x00" +
		"1 D. N... 100644 000000 000000 ccc 000 gone.go\x00" +
		"2 R. N... 100644 100644 100644 ddd ddd R100 lib/b.go\x00lib/a.go\x00" +
		"u UU N... 100644 100644 100644 100644 e1 e2 e3 merge.go\x00" +
		"? notes.txt\x00? scratch/\x00"
	st := parseStatus("/repo", []byte(out))
	if st.Branch != "main" || st.Upstream != "origin/main" || st.Ahead != 2 || st.Behind != 1 || st.Detached {
		t.Errorf("branch %+v", st)
	}
	want := []GitChange{
		{Path: "/repo/src/app.go", Status: "modified", Unstaged: true},
		{Path: "/repo/new.go", Status: "added", Staged: true},
		{Path: "/repo/gone.go", Status: "deleted", Staged: true},
		{Path: "/repo/lib/b.go", Status: "renamed", Staged: true, From: "/repo/lib/a.go"},
		{Path: "/repo/merge.go", Status: "conflict", Unstaged: true},
		{Path: "/repo/notes.txt", Status: "untracked", Unstaged: true},
		{Path: "/repo/scratch/", Status: "untracked", Unstaged: true},
	}
	if len(st.Changes) != len(want) {
		t.Fatalf("changes %+v", st.Changes)
	}
	for i, c := range st.Changes {
		if c != want[i] {
			t.Errorf("change %d = %+v, want %+v", i, c, want[i])
		}
	}
}

func TestParseStatusDetached(t *testing.T) {
	st := parseStatus("/r", []byte("# branch.oid abcdef1234\x00# branch.head (detached)\x00"))
	if !st.Detached || st.Branch != "abcdef1" {
		t.Errorf("%+v", st)
	}
}

func TestGitStatusInARepository(t *testing.T) {
	s := NewService()
	if s.gitPath() == "" {
		t.Skip("git not usable here")
	}
	dir := t.TempDir()
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real // macOS: /var → /private/var, as git reports it
	}
	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644)
	git("add", "a.txt")
	git("commit", "-q", "-m", "first")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("two\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	os.WriteFile(filepath.Join(dir, "sub", "b.txt"), []byte("new\n"), 0o644)

	st := s.GitStatus(filepath.Join(dir, "sub"))
	if st.Root != dir || st.Branch != "main" || st.Error != "" {
		t.Fatalf("status %+v", st)
	}
	got := map[string]string{}
	for _, c := range st.Changes {
		got[c.Path] = c.Status
	}
	if got[filepath.Join(dir, "a.txt")] != "modified" || got[filepath.Join(dir, "sub")+"/"] != "untracked" {
		t.Errorf("changes %+v", st.Changes)
	}
	// git status must not leave an index.lock behind, or take one at all.
	if _, err := os.Stat(filepath.Join(dir, ".git", "index.lock")); err == nil {
		t.Error("index.lock left behind")
	}
}

func TestGitStatusOutsideARepository(t *testing.T) {
	st := NewService().GitStatus(t.TempDir())
	if st.Root != "" || len(st.Changes) != 0 {
		t.Errorf("%+v", st)
	}
}

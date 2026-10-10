package files

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

func names(l Listing) string {
	var out []string
	for _, e := range l.Entries {
		n := e.Name
		if e.Dir {
			n += "/"
		}
		out = append(out, n)
	}
	return strings.Join(out, " ")
}

func TestListSortsFoldersFirst(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"b.txt", "A.txt", "zeta/x", "Alpha/x", ".env", ".DS_Store"} {
		touch(t, filepath.Join(dir, f))
	}
	os.Symlink(filepath.Join(dir, "zeta"), filepath.Join(dir, "link"))

	l, err := NewService().List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := names(l), "Alpha/ link/ zeta/ .env A.txt b.txt"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	for _, e := range l.Entries {
		switch e.Name {
		case ".env":
			if !e.Hidden {
				t.Error(".env not hidden")
			}
		case "link":
			if !e.Symlink || !e.Dir {
				t.Errorf("link = %+v, want a symlink to a folder", e)
			}
		}
		if e.Path != filepath.Join(dir, e.Name) {
			t.Errorf("path %q", e.Path)
		}
	}
}

func TestListRespectsGitignore(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	s := NewService()
	if s.gitPath() == "" {
		t.Skip("git not usable here")
	}
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git init: %v %s", err, out)
	}
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("build/\n*.log\n"), 0o644)
	for _, f := range []string{"build/out", "app.log", "main.go", "node_modules/x"} {
		touch(t, filepath.Join(dir, f))
	}
	l, err := s.List(dir)
	if err != nil {
		t.Fatal(err)
	}
	ignored := map[string]bool{}
	for _, e := range l.Entries {
		ignored[e.Name] = e.Ignored
	}
	for name, want := range map[string]bool{"build": true, "app.log": true, "main.go": false, "node_modules": true, ".gitignore": false, ".git": true} {
		if ignored[name] != want {
			t.Errorf("%s ignored = %v, want %v", name, ignored[name], want)
		}
	}
}

func TestListOutsideRepo(t *testing.T) {
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "a.log"))
	l, err := NewService().List(dir)
	if err != nil || len(l.Entries) != 1 || l.Entries[0].Ignored {
		t.Fatalf("List = %+v, %v", l, err)
	}
}

func TestListErrors(t *testing.T) {
	s := NewService()
	if _, err := s.List("relative/path"); err == nil {
		t.Error("listed a relative path")
	}
	if _, err := s.List(filepath.Join(t.TempDir(), "gone")); err == nil || err.Error() != "no longer exists" {
		t.Errorf("missing dir: %v", err)
	}
	if os.Geteuid() != 0 {
		locked := filepath.Join(t.TempDir(), "locked")
		os.Mkdir(locked, 0o000)
		defer os.Chmod(locked, 0o755)
		if _, err := s.List(locked); err == nil || err.Error() != "permission denied" {
			t.Errorf("locked dir: %v", err)
		}
	}
}

func TestListTruncates(t *testing.T) {
	dir := t.TempDir()
	for i := range MaxEntries + 10 {
		touch(t, filepath.Join(dir, "f"+strings.Repeat("x", i%7)+string(rune('a'+i%26))+strconv.Itoa(i)))
	}
	l, err := NewService().List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Entries) != MaxEntries || !l.Truncated {
		t.Fatalf("got %d entries, truncated=%v", len(l.Entries), l.Truncated)
	}
}

func TestResolveFile(t *testing.T) {
	s := NewService()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "src", "main.go")
	if err := os.WriteFile(file, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ dir, name, want string }{
		{dir, "src/main.go", file},
		{dir, "./src/../src/main.go", file},
		{"/elsewhere", file, file},
		{dir, "src", ""},            // a folder
		{dir, "src/missing.go", ""}, // no such file
		{"", "src/main.go", ""},     // relative with no folder to resolve against
		{dir, "example.com", ""},    // a host, not a file
	} {
		if got := s.ResolveFile(c.dir, c.name); got != c.want {
			t.Errorf("ResolveFile(%q, %q) = %q, want %q", c.dir, c.name, got, c.want)
		}
	}
}

func TestSaveText(t *testing.T) {
	s := NewService()
	path := filepath.Join(t.TempDir(), "out.txt")
	if err := s.SaveText(path, "hello\n"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != "hello\n" {
		t.Errorf("wrote %q", b)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want 0600", fi.Mode().Perm())
	}
	if err := s.SaveText("relative.txt", "x"); err == nil {
		t.Error("a relative path was accepted")
	}
}

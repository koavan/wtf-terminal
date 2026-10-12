package infra

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGit(t *testing.T) {
	if gitPath() == "" {
		t.Skip("git not usable here")
	}
	tmp := t.TempDir()
	if real, err := filepath.EvalSymlinks(tmp); err == nil {
		tmp = real // macOS: /var → /private/var, as git reports it
	}
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(tmp, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@x", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@x")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	origin, repo := filepath.Join(tmp, "origin"), filepath.Join(tmp, "repo")
	git(tmp, "init", "-q", "-b", "Main", origin)
	git(origin, "commit", "-q", "--allow-empty", "-m", "one")
	git(origin, "commit", "-q", "--allow-empty", "-m", "two")
	git(tmp, "clone", "-q", origin, repo)
	git(repo, "config", "user.name", "Ada Lovelace")
	git(repo, "config", "user.email", "ada@example.com")
	git(repo, "remote", "set-url", "origin", "https://ada:s3cret@github.com/ada/engine.git")
	git(repo, "config", "remote.origin.pushurl", "unused")
	git(repo, "reset", "-q", "--hard", "HEAD~1")
	git(repo, "commit", "-q", "--allow-empty", "-m", "mine")
	os.MkdirAll(filepath.Join(repo, "sub"), 0o755)

	s := testService(t.TempDir(), nil)
	g := s.Resolve(map[string]string{"PWD": filepath.Join(repo, "sub")}).Git
	if g == nil {
		t.Fatal("no git info inside a repository")
	}
	want := Git{
		Root: repo, Branch: "Main", Upstream: "origin/Main", Ahead: 1, Behind: 1,
		Remote: "origin", RemoteURL: "https://github.com/ada/engine.git",
		AuthorName: "Ada Lovelace", AuthorEmail: "ada@example.com",
	}
	g.FetchedAt = 0
	if *g != want {
		t.Errorf("got  %+v\nwant %+v", *g, want)
	}

	// A tab using a wtf git identity.
	env := map[string]string{"PWD": repo, "GIT_AUTHOR_NAME": "Ada (work)", "GIT_AUTHOR_EMAIL": "ada@work.example", "WTF_GIT_IDENTITY": "work"}
	if g := s.Resolve(env).Git; g == nil || g.AuthorName != "Ada (work)" || g.AuthorEmail != "ada@work.example" || g.Identity != "work" {
		t.Errorf("identity: %+v", g)
	}

	git(repo, "checkout", "-q", "--detach")
	if g := s.Resolve(map[string]string{"PWD": repo}).Git; g == nil || !g.Detached || g.Upstream != "" || len(g.Branch) < 7 {
		t.Errorf("detached: %+v", g)
	}
	if g := s.Resolve(map[string]string{"PWD": tmp}).Git; g != nil {
		t.Errorf("outside a repository: %+v", g)
	}
}

func TestCleanURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://user:tok@github.com/a/b.git": "https://github.com/a/b.git",
		"https://tok@github.com/a/b":          "https://github.com/a/b",
		"ssh://git:pw@github.com/a/b.git":     "ssh://git@github.com/a/b.git",
		"git@github.com:a/b.git":              "git@github.com:a/b.git",
		"/srv/repos/b.git":                    "/srv/repos/b.git",
	} {
		if got := cleanURL(in); got != want {
			t.Errorf("cleanURL(%q) = %q, want %q", in, got, want)
		}
	}
}

package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dayanraj/wtf/internal/infra"
)

// repo makes a repository with one commit on main, in a temp folder that
// git sees by its real path (macOS /var → /private/var).
func repo(t *testing.T) (*Service, string) {
	t.Helper()
	if infra.GitPath() == "" {
		t.Skip("no git")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(base, "api")
	git(t, base, "init", "-q", "-b", "main", dir)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "a.txt")
	git(t, dir, "commit", "-q", "-m", "first")
	s := NewService()
	s.home = base
	return s, dir
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func TestAddAndRemove(t *testing.T) {
	s, dir := repo(t)
	r, err := s.Repo(filepath.Join(dir))
	if err != nil {
		t.Fatal(err)
	}
	if r.Main != dir || r.Name != "api" || len(r.Worktrees) != 1 || !r.Worktrees[0].Main || r.Worktrees[0].Branch != "main" {
		t.Fatalf("repo = %+v", r)
	}
	if strings.Join(r.Branches, ",") != "main" || r.Parent != dir+".worktrees" {
		t.Fatalf("branches = %v", r.Branches)
	}

	req := AddRequest{Dir: dir, Branch: "feat/login fix", New: true}
	if _, err := s.PlanAdd(req); err == nil || !strings.Contains(err.Error(), "valid branch name") {
		t.Fatalf("bad name: %v", err)
	}
	req.Branch = "feat/login"
	plan, err := s.PlanAdd(req)
	if err != nil {
		t.Fatal(err)
	}
	want := dir + ".worktrees/feat-login"
	if plan.Path != want || plan.Base != "main" || plan.Command != "git -C "+dir+" worktree add -b feat/login "+want+" main" {
		t.Fatalf("plan = %+v", plan)
	}
	if _, err := s.Add(req); err != nil {
		t.Fatal(err)
	}
	r, _ = s.Repo(want)
	if len(r.Worktrees) != 2 || r.Current != want || r.Main != dir || r.Worktrees[1].Branch != "feat/login" {
		t.Fatalf("after add: %+v", r)
	}
	if b := BranchOf(want); b.Branch != "feat/login" || !b.Linked || b.Detached {
		t.Fatalf("branch of worktree = %+v", b)
	}
	if b := BranchOf(dir); b.Branch != "main" || b.Linked {
		t.Fatalf("branch of main = %+v", b)
	}

	// Again: the branch exists now, and is checked out.
	if _, err := s.PlanAdd(req); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("new again: %v", err)
	}
	if _, err := s.PlanAdd(AddRequest{Dir: dir, Branch: "feat/login"}); err == nil || !strings.Contains(err.Error(), "already checked out") {
		t.Fatalf("checked out: %v", err)
	}
	if _, err := s.PlanAdd(AddRequest{Dir: dir, Branch: "nope"}); err == nil || !strings.Contains(err.Error(), "no local branch") {
		t.Fatalf("missing branch: %v", err)
	}
	if _, err := s.PlanAdd(AddRequest{Dir: dir, Branch: "x", New: true, Path: filepath.Join(dir, "sub")}); err == nil {
		t.Fatal("a folder inside the checkout was allowed")
	}

	// The main checkout can't go; an agent's worktree can't either.
	if _, err := s.PlanRemove(dir, dir); err == nil || !strings.Contains(err.Error(), "main checkout") {
		t.Fatalf("remove main: %v", err)
	}
	s.Busy = func(d string) []string {
		if Inside(filepath.Join(want, "x"), d) {
			return []string{"login"}
		}
		return nil
	}
	if _, err := s.Remove(dir, want, true); err == nil || !strings.Contains(err.Error(), "login") {
		t.Fatalf("busy: %v", err)
	}
	s.Busy = nil

	// A new file: git refuses until forced; the branch stays.
	if err := os.WriteFile(filepath.Join(want, "new.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	rp, err := s.PlanRemove(dir, want)
	if err != nil {
		t.Fatal(err)
	}
	if len(rp.Changes) != 1 || rp.Changes[0] != "?? new.txt" || rp.Branch != "feat/login" {
		t.Fatalf("remove plan = %+v", rp)
	}
	res, err := s.Remove(dir, want, false)
	if err != nil || res.Refused == "" {
		t.Fatalf("remove = %+v, %v", res, err)
	}
	if res, err := s.Remove(dir, want, true); err != nil || res.Refused != "" {
		t.Fatalf("force = %+v, %v", res, err)
	}
	if _, err := os.Stat(want); !os.IsNotExist(err) {
		t.Fatalf("folder still there: %v", err)
	}
	if !strings.Contains(git(t, dir, "branch", "--list", "feat/login"), "feat/login") {
		t.Fatal("the branch was deleted")
	}

	// Detached: main is checked out in dir, so only this way.
	det := AddRequest{Dir: dir, Branch: "main", Detach: true}
	plan, err = s.PlanAdd(det)
	if err != nil || !plan.Detach || plan.Branch != "" || plan.Base != "main" ||
		plan.Command != "git -C "+dir+" worktree add --detach "+dir+".worktrees/main main" {
		t.Fatalf("detached plan = %+v, %v", plan, err)
	}
	if _, err := s.Add(det); err != nil {
		t.Fatal(err)
	}
	if b := BranchOf(dir + ".worktrees/main"); !b.Detached || !b.Linked {
		t.Fatalf("detached branch = %+v", b)
	}
	if _, err := s.PlanAdd(AddRequest{Dir: dir, Branch: "nope", Detach: true, Path: "/tmp/x"}); err == nil {
		t.Fatal("detached at nothing was allowed")
	}

	// An existing branch, into worktree-dir.
	s.Dir = func() string { return "~/trees" }
	plan, err = s.PlanAdd(AddRequest{Dir: dir, Branch: "feat/login"})
	if err != nil || plan.Path != filepath.Join(s.home, "trees", "api", "feat-login") || plan.New {
		t.Fatalf("existing = %+v, %v", plan, err)
	}
}

func TestPrunable(t *testing.T) {
	s, dir := repo(t)
	wt := filepath.Join(filepath.Dir(dir), "gone")
	git(t, dir, "worktree", "add", "-q", "-b", "gone", wt)
	if err := os.RemoveAll(wt); err != nil {
		t.Fatal(err)
	}
	rp, err := s.PlanRemove(dir, wt)
	if err != nil || !rp.Prunable || !strings.HasSuffix(rp.Command, "worktree prune") {
		t.Fatalf("plan = %+v, %v", rp, err)
	}
	if _, err := s.Remove(dir, wt, false); err != nil {
		t.Fatal(err)
	}
	if r, _ := s.Repo(dir); len(r.Worktrees) != 1 {
		t.Fatalf("still listed: %+v", r.Worktrees)
	}
}

func TestParseList(t *testing.T) {
	out := "worktree /r\x00bare\x00\x00" +
		"worktree /a\x00HEAD 0123456789abcdef\x00branch refs/heads/main\x00\x00" +
		"worktree /b\x00HEAD fedcba9876543210\x00detached\x00locked reason\x00\x00" +
		"worktree /c\x00HEAD 1111111111111111\x00branch refs/heads/x/y\x00prunable gitdir file points to non-existent location\x00\x00"
	got := parseList(out)
	if len(got) != 3 {
		t.Fatalf("got %+v", got)
	}
	if !got[0].Main || got[0].Branch != "main" || got[0].Head != "0123456" {
		t.Errorf("a = %+v", got[0])
	}
	if got[1].Main || got[1].Branch != "" || !got[1].Locked {
		t.Errorf("b = %+v", got[1])
	}
	if got[2].Branch != "x/y" || !got[2].Prunable {
		t.Errorf("c = %+v", got[2])
	}
}

func TestSlug(t *testing.T) {
	for in, want := range map[string]string{
		"feat/login":     "feat-login",
		"fix login  bug": "fix-login-bug",
		"-weird-":        "weird",
		"日本":             "worktree",
		"v1.2_rc":        "v1.2_rc",
	} {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

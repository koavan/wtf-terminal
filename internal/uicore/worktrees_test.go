package uicore

import (
	"slices"
	"testing"

	"github.com/dayanraj/wtf/internal/worktree"
)

// The same cases as the macOS app's WorktreesTests.swift.
func TestWorktreeRepoHelpers(t *testing.T) {
	r := worktree.Repo{Main: "/src/api", Name: "api", Current: "/src/api.worktrees/login",
		Worktrees: []worktree.Worktree{
			{Path: "/src/api", Branch: "main", Head: "1111111", Main: true},
			{Path: "/src/api.worktrees/login", Branch: "feat/login", Head: "2222222"},
			{Path: "/src/api.worktrees/gone", Head: "3333333", Prunable: true},
		},
		Branches: []string{"feat/login", "main", "fix/x"}, Parent: "/src/api.worktrees"}
	if got := WorktreeFree(r); !slices.Equal(got, []string{"fix/x"}) {
		t.Errorf("free = %v", got)
	}
	if got := WorktreeCurrent(r); got != "feat/login" {
		t.Errorf("current = %q", got)
	}
	if got := WorktreeSuggest(r, "fix/y z"); got != "/src/api.worktrees/fix-y-z" {
		t.Errorf("suggest = %q", got)
	}
	if w, ok := WorktreeContaining("/src/api.worktrees/login/cmd", r); !ok || w.Branch != "feat/login" {
		t.Errorf("containing login = %+v", w)
	}
	if w, ok := WorktreeContaining("/src/api/cmd", r); !ok || !w.Main {
		t.Errorf("containing main = %+v", w)
	}
	if _, ok := WorktreeContaining("/src/apix", r); ok {
		t.Error("/src/apix is in a worktree")
	}
	if got := WorktreeTitle(r.Worktrees[2], "/src"); got != "detached at 3333333 — ~/api.worktrees/gone (folder gone)" {
		t.Errorf("title = %q", got)
	}
	if got := WorktreeTitle(r.Worktrees[0], ""); got != "main — /src/api (main checkout)" {
		t.Errorf("title = %q", got)
	}
}

func TestBranchLabel(t *testing.T) {
	for _, c := range []struct {
		b    worktree.Branch
		want string
	}{
		{worktree.Branch{}, ""},
		{worktree.Branch{Branch: "feat/login", Ahead: 2, Behind: 1}, "feat/login ↑2 ↓1"},
		{worktree.Branch{Branch: "1a2b3c4", Detached: true}, "detached at 1a2b3c4"},
	} {
		if got := BranchLabel(c.b); got != c.want {
			t.Errorf("BranchLabel(%+v) = %q, want %q", c.b, got, c.want)
		}
	}
}

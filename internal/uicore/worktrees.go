package uicore

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dayanraj/wtf/internal/worktree"
)

// Git worktrees (#37): the bits of New Worktree… and the agent rows that
// don't need GTK. Like the macOS app's WtfCore/Worktrees.swift.

// WorktreeSuggest is where New Worktree… suggests putting branch.
func WorktreeSuggest(r worktree.Repo, branch string) string {
	return filepath.Join(r.Parent, worktree.Slug(branch))
}

// WorktreeFree are the branches you can check out in a new worktree: those
// not checked out anywhere yet.
func WorktreeFree(r worktree.Repo) []string {
	used := map[string]bool{}
	for _, w := range r.Worktrees {
		used[w.Branch] = true
	}
	out := []string{}
	for _, b := range r.Branches {
		if !used[b] {
			out = append(out, b)
		}
	}
	return out
}

// WorktreeCurrent is the branch checked out where you asked from ("" when
// detached).
func WorktreeCurrent(r worktree.Repo) string {
	for _, w := range r.Worktrees {
		if w.Path == r.Current {
			return w.Branch
		}
	}
	return ""
}

// BranchLabel is "feat/login ↑2 ↓1", "detached at 1a2b3c4"; "" outside a
// repository.
func BranchLabel(b worktree.Branch) string {
	if b.Branch == "" {
		return ""
	}
	parts := []string{b.Branch}
	if b.Detached {
		parts[0] = "detached at " + b.Branch
	}
	if b.Ahead > 0 {
		parts = append(parts, "↑"+strconv.Itoa(b.Ahead))
	}
	if b.Behind > 0 {
		parts = append(parts, "↓"+strconv.Itoa(b.Behind))
	}
	return strings.Join(parts, " ")
}

// WorktreeTitle is a worktree in a menu or list: its branch (or commit)
// and folder.
func WorktreeTitle(w worktree.Worktree, home string) string {
	name := w.Branch
	if name == "" {
		name = "detached at " + w.Head
	}
	path := w.Path
	if home != "" && (path == home || strings.HasPrefix(path, home+"/")) {
		path = "~" + path[len(home):]
	}
	s := name + " — " + path
	if w.Main {
		s += " (main checkout)"
	}
	if w.Prunable {
		s += " (folder gone)"
	}
	return s
}

// WorktreeContaining is the worktree path is in: the deepest one
// containing it.
func WorktreeContaining(path string, r worktree.Repo) (worktree.Worktree, bool) {
	var best worktree.Worktree
	found := false
	for _, w := range r.Worktrees {
		if worktree.Inside(path, w.Path) && (!found || len(w.Path) > len(best.Path)) {
			best, found = w, true
		}
	}
	return best, found
}

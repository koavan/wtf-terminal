// Package worktree manages git worktrees (#37): more checkouts of one
// repository, each on its own branch in its own folder, so agents (or you)
// can work on several branches at once without touching each other's files.
//
// Reading is free; every change is planned first and runs only after the
// UI's confirm. Removing never deletes a branch, refuses a worktree an
// agent works in, and leaves git's own safety check in place: when git
// refuses (changed or untracked files), the UI must ask again before
// --force.
package worktree

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dayanraj/wtf/internal/infra"
)

// Worktree is one checkout of a repository.
type Worktree struct {
	Path string `json:"path"`
	// The branch checked out; "" when the head is detached.
	Branch string `json:"branch"`
	// The commit it's on, short.
	Head string `json:"head"`
	// The repository's main checkout (the one with the .git folder).
	Main bool `json:"main"`
	// Locked with `git worktree lock`: git won't remove it.
	Locked bool `json:"locked"`
	// Its folder is gone; git still lists it until it's pruned.
	Prunable bool `json:"prunable"`
}

// Repo is the repository a folder is in, with its worktrees.
type Repo struct {
	// The main checkout's folder, and its name.
	Main string `json:"main"`
	Name string `json:"name"`
	// The worktree the folder asked about is in (its top folder).
	Current string `json:"current"`
	// The main checkout first, then the others as git lists them.
	Worktrees []Worktree `json:"worktrees"`
	// Local branches, most recently committed first (at most maxBranches).
	Branches []string `json:"branches"`
	// Where a new worktree goes: <Parent>/<Slug(branch)>.
	Parent string `json:"parent"`
}

// AddRequest asks for a new worktree.
type AddRequest struct {
	// Any folder in the repository.
	Dir string `json:"dir"`
	// The branch to check out: an existing local one, or (New) one to make.
	Branch string `json:"branch"`
	New    bool   `json:"new"`
	// New: what the branch starts from ("" = the head of Dir's checkout).
	Base string `json:"base"`
	// Detach: check out Branch (a branch or any commit) with a detached
	// head, on no branch: a look at a branch that's checked out elsewhere.
	Detach bool `json:"detach"`
	// Where it goes; "" = Suggest's folder. "~/" is your home folder.
	Path string `json:"path"`
}

// AddPlan is what Add would do, for the UI's confirm.
type AddPlan struct {
	Path   string `json:"path"`
	Branch string `json:"branch"`
	New    bool   `json:"new"`
	// New: what the branch starts from; Detach: what's checked out.
	Base   string `json:"base"`
	Detach bool   `json:"detach"`
	// The command, as you'd type it.
	Command string `json:"command"`
}

// RemovePlan is what Remove would do, for the UI's confirm.
type RemovePlan struct {
	Path   string `json:"path"`
	Branch string `json:"branch"`
	// Its folder is already gone: removing only drops git's record of it.
	Prunable bool `json:"prunable"`
	// What `git status --short` lists there: these are lost with --force.
	Changes []string `json:"changes"`
	// More changes than Changes holds.
	More    int    `json:"more"`
	Command string `json:"command"`
}

// RemoveResult: Refused is git's reason when it wouldn't remove the
// worktree (changed or untracked files); the UI may ask again with force.
type RemoveResult struct {
	Refused string `json:"refused"`
}

const (
	maxBranches = 200
	maxChanges  = 20
	readTimeout = 5 * time.Second
	// Checking out a large repository takes a while.
	addTimeout = 2 * time.Minute
)

type Service struct {
	// worktree-dir: where new worktrees go ("" = beside the repository).
	Dir func() string
	// The agents working in a folder or below it (their names), from the
	// Agents tab's bridge: Remove refuses those worktrees.
	Busy func(dir string) []string

	git  func() string
	home string
}

func NewService() *Service {
	home, _ := os.UserHomeDir()
	return &Service{git: infra.GitPath, home: home}
}

var errNoGit = errors.New("git isn't installed")

// run runs git in dir and returns its output; a failure carries git's
// message.
func (s *Service) run(ctx context.Context, dir string, args ...string) (string, error) {
	bin := s.git()
	if bin == "" {
		return "", errNoGit
	}
	cmd := exec.CommandContext(ctx, bin, append([]string{"-C", dir}, args...)...)
	// Never take index.lock for a read; plain English messages to show.
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C", "GIT_TERMINAL_PROMPT=0")
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", errors.New("git took too long")
		}
		return out.String(), errors.New(gitMessage(errOut.String(), err))
	}
	return out.String(), nil
}

// gitMessage is git's error output without "fatal: " and hints, on one line.
func gitMessage(stderr string, err error) string {
	var lines []string
	for _, l := range strings.Split(stderr, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "hint:") {
			continue
		}
		for _, p := range []string{"fatal: ", "error: "} {
			l = strings.TrimPrefix(l, p)
		}
		lines = append(lines, l)
	}
	if len(lines) == 0 {
		return err.Error()
	}
	return strings.Join(lines, "; ")
}

// Repo reads the repository dir is in.
func (s *Service) Repo(dir string) (Repo, error) {
	if !filepath.IsAbs(dir) {
		return Repo{}, errors.New("not a folder")
	}
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()
	top, err := s.run(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		if err == errNoGit {
			return Repo{}, err
		}
		return Repo{}, errors.New("not in a git repository")
	}
	list, err := s.run(ctx, dir, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return Repo{}, err
	}
	trees := parseList(list)
	if len(trees) == 0 {
		return Repo{}, errors.New("git listed no worktrees")
	}
	refs, err := s.run(ctx, dir, "for-each-ref", "--sort=-committerdate", "--count="+strconv.Itoa(maxBranches),
		"--format=%(refname:short)", "refs/heads")
	if err != nil {
		return Repo{}, err
	}
	r := Repo{Main: trees[0].Path, Name: filepath.Base(trees[0].Path), Current: strings.TrimSpace(top), Worktrees: trees, Branches: []string{}}
	for _, b := range strings.Split(refs, "\n") {
		if b = strings.TrimSpace(b); b != "" {
			r.Branches = append(r.Branches, b)
		}
	}
	r.Parent = s.parent(r)
	return r, nil
}

// parseList reads `git worktree list --porcelain -z`: attribute lines, each
// ending in NUL, a worktree's ending in an empty one. The first is the main
// checkout (a bare repository's is skipped).
func parseList(out string) []Worktree {
	trees := []Worktree{}
	var w *Worktree
	bare := false
	end := func() {
		if w != nil && !bare {
			trees = append(trees, *w)
		}
		w, bare = nil, false
	}
	for _, f := range strings.Split(out, "\x00") {
		if f == "" {
			end()
			continue
		}
		key, val, _ := strings.Cut(f, " ")
		switch key {
		case "worktree":
			end()
			w = &Worktree{Path: val, Main: len(trees) == 0}
		case "HEAD":
			if w != nil && len(val) >= 7 {
				w.Head = val[:7]
			}
		case "branch":
			if w != nil {
				w.Branch = strings.TrimPrefix(val, "refs/heads/")
			}
		case "bare":
			bare = true
		case "locked":
			if w != nil {
				w.Locked = true
			}
		case "prunable":
			if w != nil {
				w.Prunable = true
			}
		}
	}
	end()
	if len(trees) > 0 && !trees[0].Main {
		trees[0].Main = true
	}
	return trees
}

// Suggest is where a new worktree for branch goes: beside the repository
// (<repo>.worktrees/<branch>), or under worktree-dir (<dir>/<repo>/<branch>).
func (s *Service) Suggest(dir, branch string) (string, error) {
	r, err := s.Repo(dir)
	if err != nil {
		return "", err
	}
	return s.suggest(r, branch), nil
}

func (s *Service) suggest(r Repo, branch string) string {
	return filepath.Join(s.parent(r), Slug(branch))
}

func (s *Service) parent(r Repo) string {
	if s.Dir != nil {
		if d := s.expand(strings.TrimSpace(s.Dir())); filepath.IsAbs(d) {
			return filepath.Join(filepath.Clean(d), r.Name)
		}
	}
	return r.Main + ".worktrees"
}

// Slug makes a branch name a folder name: "feat/login fix" → "feat-login-fix".
func Slug(branch string) string {
	var b strings.Builder
	dash := false
	for _, c := range strings.TrimSpace(branch) {
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-'
		if ok {
			b.WriteRune(c)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.Trim(b.String(), "-.")
	if out == "" {
		return "worktree"
	}
	return out
}

func (s *Service) expand(p string) string {
	if p == "~" {
		return s.home
	}
	if strings.HasPrefix(p, "~/") && s.home != "" {
		return filepath.Join(s.home, p[2:])
	}
	return p
}

// PlanAdd checks a new worktree and says what Add would run.
func (s *Service) PlanAdd(req AddRequest) (AddPlan, error) {
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()
	p, _, err := s.plan(ctx, req)
	return p, err
}

func (s *Service) plan(ctx context.Context, req AddRequest) (AddPlan, []string, error) {
	r, err := s.Repo(req.Dir)
	if err != nil {
		return AddPlan{}, nil, err
	}
	branch := strings.TrimSpace(req.Branch)
	if branch == "" {
		return AddPlan{}, nil, errors.New("give it a branch")
	}
	if strings.HasPrefix(branch, "-") {
		return AddPlan{}, nil, fmt.Errorf("%q isn't a valid branch name", branch)
	}
	if _, err := s.run(ctx, r.Main, "check-ref-format", "--branch", branch); err != nil {
		return AddPlan{}, nil, fmt.Errorf("%q isn't a valid branch name", branch)
	}
	if req.Detach {
		return s.planDetached(ctx, r, req, branch)
	}
	_, verr := s.run(ctx, r.Main, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	exists := verr == nil
	plan := AddPlan{Branch: branch, New: req.New}
	if req.New {
		if exists {
			return AddPlan{}, nil, fmt.Errorf("a branch %s already exists: check it out instead", branch)
		}
		plan.Base = strings.TrimSpace(req.Base)
		if plan.Base == "" {
			if b := s.currentBranch(r); b != "" {
				plan.Base = b
			} else {
				plan.Base = "HEAD"
			}
		}
		if strings.HasPrefix(plan.Base, "-") {
			return AddPlan{}, nil, fmt.Errorf("%q isn't a branch or commit", plan.Base)
		}
		if _, err := s.run(ctx, r.Current, "rev-parse", "--verify", "--quiet", plan.Base+"^{commit}"); err != nil {
			return AddPlan{}, nil, fmt.Errorf("%q isn't a branch or commit here", plan.Base)
		}
	} else {
		if !exists {
			return AddPlan{}, nil, fmt.Errorf("there's no local branch %s", branch)
		}
		for _, w := range r.Worktrees {
			if w.Branch == branch {
				return AddPlan{}, nil, fmt.Errorf("%s is already checked out in %s", branch, w.Path)
			}
		}
	}
	path, err := s.folder(r, req.Path, branch)
	if err != nil {
		return AddPlan{}, nil, err
	}
	plan.Path = path
	args := []string{"worktree", "add"}
	if req.New {
		args = append(args, "-b", branch, path, plan.Base)
	} else {
		args = append(args, path, branch)
	}
	plan.Command = command(r.Main, args)
	return plan, args, nil
}

// planDetached: `git worktree add --detach <path> <commit>`, allowed for a
// branch checked out elsewhere.
func (s *Service) planDetached(ctx context.Context, r Repo, req AddRequest, rev string) (AddPlan, []string, error) {
	if _, err := s.run(ctx, r.Current, "rev-parse", "--verify", "--quiet", rev+"^{commit}"); err != nil {
		return AddPlan{}, nil, fmt.Errorf("%q isn't a branch or commit here", rev)
	}
	path, err := s.folder(r, req.Path, rev)
	if err != nil {
		return AddPlan{}, nil, err
	}
	args := []string{"worktree", "add", "--detach", path, rev}
	return AddPlan{Path: path, Base: rev, Detach: true, Command: command(r.Main, args)}, args, nil
}

// folder checks where a new worktree goes ("" = the suggested folder for
// name): a full path, outside the main checkout, new or empty.
func (s *Service) folder(r Repo, want, name string) (string, error) {
	path := s.expand(strings.TrimSpace(want))
	if path == "" {
		path = s.suggest(r, name)
	}
	if !filepath.IsAbs(path) {
		return "", errors.New("the folder must be a full path")
	}
	path = filepath.Clean(path)
	if inside(path, r.Main) {
		return "", errors.New("the folder can't be inside the repository's own checkout")
	}
	if entries, err := os.ReadDir(path); err == nil {
		if len(entries) > 0 {
			return "", fmt.Errorf("%s already exists and isn't empty", path)
		}
	} else if _, serr := os.Lstat(path); serr == nil {
		return "", fmt.Errorf("%s already exists", path)
	}
	return path, nil
}

// currentBranch is the branch checked out where the request came from.
func (s *Service) currentBranch(r Repo) string {
	for _, w := range r.Worktrees {
		if w.Path == r.Current {
			return w.Branch
		}
	}
	return ""
}

// Add makes the worktree PlanAdd described (checked again first).
func (s *Service) Add(req AddRequest) (Worktree, error) {
	ctx, cancel := context.WithTimeout(context.Background(), addTimeout)
	defer cancel()
	plan, args, err := s.plan(ctx, req)
	if err != nil {
		return Worktree{}, err
	}
	r, _ := s.Repo(req.Dir)
	if _, err := s.run(ctx, r.Main, args...); err != nil {
		return Worktree{}, err
	}
	return Worktree{Path: plan.Path, Branch: plan.Branch}, nil
}

// find is the linked worktree at path in the repository dir is in.
func (s *Service) find(dir, path string) (Repo, Worktree, error) {
	path = filepath.Clean(path)
	r, err := s.Repo(dir)
	if err != nil {
		return Repo{}, Worktree{}, err
	}
	i := slices.IndexFunc(r.Worktrees, func(w Worktree) bool { return w.Path == path })
	if i < 0 {
		return Repo{}, Worktree{}, fmt.Errorf("%s isn't one of this repository's worktrees", path)
	}
	w := r.Worktrees[i]
	if w.Main {
		return Repo{}, Worktree{}, errors.New("that's the repository's main checkout, which can't be removed as a worktree")
	}
	return r, w, nil
}

// PlanRemove says what Remove would do: the changes there that --force
// would lose. dir is any folder in the repository (the worktree's folder
// may be gone).
func (s *Service) PlanRemove(dir, path string) (RemovePlan, error) {
	r, w, err := s.find(dir, path)
	if err != nil {
		return RemovePlan{}, err
	}
	if err := s.refuse(w); err != nil {
		return RemovePlan{}, err
	}
	p := RemovePlan{Path: w.Path, Branch: w.Branch, Prunable: w.Prunable, Changes: []string{}}
	if !w.Prunable {
		ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
		defer cancel()
		out, err := s.run(ctx, w.Path, "status", "--porcelain=v1", "--untracked-files=normal")
		if err != nil {
			return RemovePlan{}, err
		}
		for _, l := range strings.Split(out, "\n") {
			if strings.TrimSpace(l) == "" {
				continue
			}
			if len(p.Changes) < maxChanges {
				p.Changes = append(p.Changes, l)
			} else {
				p.More++
			}
		}
	}
	p.Command = command(r.Main, removeArgs(w, false))
	return p, nil
}

func removeArgs(w Worktree, force bool) []string {
	if w.Prunable {
		return []string{"worktree", "prune"}
	}
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	return append(args, w.Path)
}

// refuse: worktrees wtf won't remove.
func (s *Service) refuse(w Worktree) error {
	if w.Locked {
		return fmt.Errorf("%s is locked: unlock it first (git worktree unlock)", w.Path)
	}
	if s.Busy != nil {
		if names := s.Busy(w.Path); len(names) > 0 {
			return fmt.Errorf("Claude Code is working there (%s): end it first", strings.Join(names, ", "))
		}
	}
	return nil
}

// Remove removes the worktree at path, in the repository dir is in: its
// folder and git's record of it, never its branch. Without force, git
// refuses one with changed or untracked files: that comes back as Refused.
func (s *Service) Remove(dir, path string, force bool) (RemoveResult, error) {
	r, w, err := s.find(dir, path)
	if err != nil {
		return RemoveResult{}, err
	}
	if err := s.refuse(w); err != nil {
		return RemoveResult{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), addTimeout)
	defer cancel()
	if _, err := s.run(ctx, r.Main, removeArgs(w, force)...); err != nil {
		if !force && !w.Prunable {
			return RemoveResult{Refused: err.Error()}, nil
		}
		return RemoveResult{}, err
	}
	return RemoveResult{}, nil
}

// command is git args as you'd type them.
func command(dir string, args []string) string {
	q := []string{"git", "-C", quote(dir)}
	for _, a := range args {
		q = append(q, quote(a))
	}
	return strings.Join(q, " ")
}

func quote(s string) string {
	if s != "" && strings.IndexFunc(s, func(c rune) bool {
		return !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("/._-+=:@^~", c))
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// inside: path is dir or below it.
func inside(path, dir string) bool {
	return path == dir || strings.HasPrefix(path, strings.TrimSuffix(dir, "/")+"/")
}

// Inside reports whether path is dir or below it (both clean and absolute).
func Inside(path, dir string) bool { return inside(path, dir) }

// Branch is where a folder's checkout stands, for an agent's row.
type Branch struct {
	// The branch, or the short commit when Detached; "" outside a repository.
	Branch   string `json:"branch"`
	Detached bool   `json:"detached"`
	// "origin/main"; "" without one.
	Upstream string `json:"upstream"`
	Ahead    int    `json:"ahead"`
	Behind   int    `json:"behind"`
	// In a linked worktree (not the repository's main checkout).
	Linked bool `json:"linked"`
}

// BranchOf reads dir's branch, and how far it is from its upstream (as of
// the last fetch; it never fetches). Read-only, a few quick git commands.
func BranchOf(dir string) Branch {
	if !filepath.IsAbs(dir) {
		return Branch{}
	}
	s := &Service{git: infra.GitPath}
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()
	out, err := s.run(ctx, dir, "rev-parse", "--path-format=absolute", "--git-dir", "--git-common-dir", "--abbrev-ref", "HEAD")
	if err != nil {
		return Branch{}
	}
	f := strings.Split(strings.TrimSpace(out), "\n")
	if len(f) < 3 {
		return Branch{}
	}
	b := Branch{Branch: strings.TrimSpace(f[2]), Linked: filepath.Clean(f[0]) != filepath.Clean(f[1])}
	if b.Branch == "HEAD" {
		b.Detached = true
		if short, err := s.run(ctx, dir, "rev-parse", "--short", "HEAD"); err == nil {
			b.Branch = strings.TrimSpace(short)
		}
		return b
	}
	up, err := s.run(ctx, dir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	if err != nil {
		return b
	}
	b.Upstream = strings.TrimSpace(up)
	if counts, err := s.run(ctx, dir, "rev-list", "--left-right", "--count", "HEAD...@{upstream}"); err == nil {
		if c := strings.Fields(counts); len(c) == 2 {
			b.Ahead, _ = strconv.Atoi(c[0])
			b.Behind, _ = strconv.Atoi(c[1])
		}
	}
	return b
}

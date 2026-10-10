package files

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Git status for the file tree: which files in the repository a folder is
// in have changed, and the branch. Read-only: `git status` with optional
// locks off, so it never takes the index lock from a git command you're
// running. Asked for only while the Files tab shows, when it refreshes.

// maxChanges caps the changes reported; the tree says when there were more.
const maxChanges = 5000

// gitTimeout gives up on very large repositories rather than hold up the tree.
const gitTimeout = 3 * time.Second

// GitChange is one changed path.
type GitChange struct {
	// Absolute path. An untracked folder ends in "/" (everything in it is new).
	Path string `json:"path"`
	// "modified", "added", "deleted", "renamed", "untracked" or "conflict".
	Status string `json:"status"`
	// Staged: the change is in the index (git add). Unstaged: in the working tree.
	Staged   bool `json:"staged"`
	Unstaged bool `json:"unstaged"`
	// For a rename, the old path.
	From string `json:"from,omitempty"`
}

// GitStatus is a repository's state. Root is "" outside a repository.
type GitStatus struct {
	Root     string      `json:"root"`
	Branch   string      `json:"branch"`
	Detached bool        `json:"detached"`
	Upstream string      `json:"upstream"`
	Ahead    int         `json:"ahead"`
	Behind   int         `json:"behind"`
	Changes  []GitChange `json:"changes"`
	// More changes than maxChanges, or git took too long (then Changes is empty).
	Truncated bool   `json:"truncated"`
	Error     string `json:"error"`
}

// GitStatus reports the repository dir is in, if any.
func (s *Service) GitStatus(dir string) GitStatus {
	git := s.gitPath()
	if git == "" || !filepath.IsAbs(dir) {
		return GitStatus{Changes: []GitChange{}}
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	run := func(args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, git, append([]string{"-C", dir}, args...)...)
		// Never take index.lock: a status run alongside the user's own git command must not break it.
		cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C")
		return cmd.Output()
	}

	top, err := run("rev-parse", "--show-toplevel")
	if err != nil {
		return GitStatus{Changes: []GitChange{}} // not a repository
	}
	root := strings.TrimSpace(string(top))
	out, err := run("status", "--porcelain=v2", "--branch", "-z", "--untracked-files=normal")
	if err != nil {
		st := GitStatus{Root: root, Changes: []GitChange{}}
		if ctx.Err() != nil {
			st.Truncated = true
			st.Error = "git status took too long in this repository"
		} else {
			st.Error = "git status failed"
		}
		return st
	}
	return parseStatus(root, out)
}

// parseStatus reads `git status --porcelain=v2 --branch -z`.
func parseStatus(root string, out []byte) GitStatus {
	st := GitStatus{Root: root, Changes: []GitChange{}}
	fields := bytes.Split(out, []byte{0})
	abs := func(p string) string { return filepath.Join(root, filepath.FromSlash(p)) }
	for i := 0; i < len(fields); i++ {
		line := string(fields[i])
		if line == "" {
			continue
		}
		switch line[0] {
		case '#':
			parseBranch(&st, line)
		case '1', '2', 'u':
			// "1 XY sub mH mI mW hH hI path", "2 … X100 path" + NUL + origPath, "u XY sub m1 m2 m3 mW h1 h2 h3 path"
			n := map[byte]int{'1': 8, '2': 9, 'u': 10}[line[0]]
			parts := strings.SplitN(line, " ", n+1)
			if len(parts) < n+1 {
				continue
			}
			xy := parts[1]
			c := GitChange{Path: abs(parts[n]), Staged: xy[0] != '.', Unstaged: xy[1] != '.'}
			switch {
			case line[0] == 'u':
				c.Status, c.Staged, c.Unstaged = "conflict", false, true
			case line[0] == '2':
				c.Status = "renamed"
				if i+1 < len(fields) {
					i++
					c.From = abs(string(fields[i]))
				}
			case xy[0] == 'D' || xy[1] == 'D':
				c.Status = "deleted"
			case xy[0] == 'A':
				c.Status = "added"
			default:
				c.Status = "modified"
			}
			st.add(c)
		case '?':
			p := line[2:]
			c := GitChange{Path: abs(p), Status: "untracked", Unstaged: true}
			if strings.HasSuffix(p, "/") {
				c.Path += "/"
			}
			st.add(c)
		}
	}
	return st
}

func (st *GitStatus) add(c GitChange) {
	if len(st.Changes) >= maxChanges {
		st.Truncated = true
		return
	}
	st.Changes = append(st.Changes, c)
}

func parseBranch(st *GitStatus, line string) {
	f := strings.Fields(line)
	if len(f) < 3 {
		return
	}
	switch f[1] {
	case "branch.head":
		if f[2] == "(detached)" {
			st.Detached = true
		} else {
			st.Branch = f[2]
		}
	case "branch.oid":
		// Comes before branch.head: the short commit stays as the name when the head is detached.
		if len(f[2]) >= 7 && f[2] != "(initial)" {
			st.Branch = f[2][:7]
		}
	case "branch.upstream":
		st.Upstream = f[2]
	case "branch.ab":
		if len(f) >= 4 {
			st.Ahead, _ = strconv.Atoi(strings.TrimPrefix(f[2], "+"))
			st.Behind, _ = strconv.Atoi(strings.TrimPrefix(f[3], "-"))
		}
	}
}

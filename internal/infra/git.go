package infra

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Git is the repository a shell's folder is in: the branch, where it pushes
// and pulls, how far apart the two are, and who commits there.
//
// Read at each prompt, so it follows commits, pulls and checkouts. Outside a
// repository it costs a few stat(2)s and no git process. Inside, a handful
// of read-only git commands with optional locks off. Never fetches: the
// difference with the remote is as of the last fetch (FetchedAt).
type Git struct {
	Root string `json:"root"`
	// The branch, or the short commit when Detached.
	Branch   string `json:"branch"`
	Detached bool   `json:"detached"`
	// The branch's upstream, e.g. "origin/main"; "" when it has none.
	Upstream string `json:"upstream"`
	// The upstream was set but its remote branch is gone (or never fetched).
	UpstreamGone bool `json:"upstreamGone"`
	// Commits on the branch not on the upstream, and the other way round.
	Ahead  int `json:"ahead"`
	Behind int `json:"behind"`
	// The upstream's remote, else "origin"; URL without any credentials.
	Remote    string `json:"remote"`
	RemoteURL string `json:"remoteURL"`
	// When the repository last fetched (Unix ms), 0 if never.
	FetchedAt int64 `json:"fetchedAt"`
	// Who commits here: GIT_AUTHOR_NAME/EMAIL in the shell, else user.name
	// and user.email as git resolves them.
	AuthorName  string `json:"authorName"`
	AuthorEmail string `json:"authorEmail"`
	// The wtf git identity the shell uses (WTF_GIT_IDENTITY), if any.
	Identity string `json:"identity"`
}

const gitTimeout = 2 * time.Second

var (
	gitOnce sync.Once
	gitBin  string
)

// gitPath finds git; on macOS /usr/bin/git is only a stub until the
// command line tools are installed, and running it would ask to install them.
func gitPath() string {
	gitOnce.Do(func() {
		path, err := exec.LookPath("git")
		if err != nil {
			return
		}
		if runtime.GOOS == "darwin" && path == "/usr/bin/git" && exec.Command("/usr/bin/xcode-select", "-p").Run() != nil {
			return
		}
		gitBin = path
	})
	return gitBin
}

// inRepo reports whether dir or a parent has a .git, without running git.
func inRepo(dir string) bool {
	for {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}

func (s *Service) git(env map[string]string) *Git {
	cwd := env["PWD"]
	if !filepath.IsAbs(cwd) || !inRepo(cwd) {
		return nil
	}
	bin := gitPath()
	if bin == "" {
		return nil
	}
	g := readGit(bin, cwd)
	if g != nil {
		// The shell's own variables win over git config, as they do for git.
		g.AuthorName = firstOf(env["GIT_AUTHOR_NAME"], g.AuthorName)
		g.AuthorEmail = firstOf(env["GIT_AUTHOR_EMAIL"], g.AuthorEmail)
		g.Identity = env["WTF_GIT_IDENTITY"]
	}
	return g
}

func readGit(bin, dir string) *Git {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	run := func(args ...string) (string, bool) {
		cmd := exec.CommandContext(ctx, bin, append([]string{"-C", dir}, args...)...)
		// Never take index.lock, so a git command the user is running can't trip over it.
		cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C")
		out, err := cmd.Output()
		return strings.TrimRight(string(out), "\n"), err == nil
	}

	out, ok := run("rev-parse", "--show-toplevel", "--git-common-dir")
	root, common, _ := strings.Cut(out, "\n")
	if !ok || root == "" {
		return nil // a .git that isn't a repository, or a bare one
	}
	g := &Git{Root: root}
	if !filepath.IsAbs(common) {
		common = filepath.Join(dir, common)
	}
	if fi, err := os.Stat(filepath.Join(common, "FETCH_HEAD")); err == nil {
		g.FetchedAt = fi.ModTime().UnixMilli()
	}

	if b, ok := run("symbolic-ref", "-q", "--short", "HEAD"); ok {
		g.Branch = b
	} else if c, ok := run("rev-parse", "--short", "HEAD"); ok {
		g.Branch, g.Detached = c, true
	}

	conf, _ := run("config", "-z", "--list")
	cfg := parseConfig(conf)
	g.AuthorName, g.AuthorEmail = cfg["user.name"], cfg["user.email"]

	remote, merge := "", ""
	if !g.Detached && g.Branch != "" {
		remote, merge = cfg["branch."+g.Branch+".remote"], cfg["branch."+g.Branch+".merge"]
	}
	if remote != "" && merge != "" {
		g.Upstream = strings.TrimPrefix(merge, "refs/heads/")
		if remote != "." { // "." tracks a local branch
			g.Upstream = remote + "/" + g.Upstream
		}
		if counts, ok := run("rev-list", "--left-right", "--count", "HEAD...@{upstream}"); ok {
			if a, b, ok := strings.Cut(counts, "\t"); ok {
				g.Ahead, _ = strconv.Atoi(a)
				g.Behind, _ = strconv.Atoi(b)
			}
		} else {
			g.UpstreamGone = true
		}
	}
	if remote == "" || remote == "." {
		remote = "origin"
	}
	if u := cfg["remote."+remote+".url"]; u != "" {
		g.Remote, g.RemoteURL = remote, cleanURL(u)
	}
	return g
}

// parseConfig reads `git config -z --list`, keeping only the keys git
// shows here; later entries win, as they do for git.
func parseConfig(out string) map[string]string {
	cfg := map[string]string{}
	for _, entry := range strings.Split(out, "\x00") {
		k, v, _ := strings.Cut(entry, "\n")
		k = normalKey(k)
		if k == "user.name" || k == "user.email" ||
			strings.HasPrefix(k, "branch.") || (strings.HasPrefix(k, "remote.") && strings.HasSuffix(k, ".url")) {
			cfg[k] = v
		}
	}
	return cfg
}

// normalKey lowercases a key's section and variable, which git treats as
// case-insensitive, but not its subsection: "branch.Feature.remote".
func normalKey(k string) string {
	first, last := strings.IndexByte(k, '.'), strings.LastIndexByte(k, '.')
	if first < 0 || first == last {
		return strings.ToLower(k)
	}
	return strings.ToLower(k[:first+1]) + k[first+1:last] + strings.ToLower(k[last:])
}

// cleanURL drops any user and password from a remote URL: a token in an
// https remote must not end up on screen.
func cleanURL(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Scheme != "" && u.Host != "" {
		if u.User != nil && u.Scheme != "ssh" {
			u.User = nil
		} else if u.User != nil {
			u.User = url.User(u.User.Username()) // ssh://git@host keeps its user, never a password
		}
		return u.String()
	}
	return raw // scp-like git@github.com:owner/repo.git has no password
}

// GitPath is the git wtf runs ("" when there's none that can run).
func GitPath() string { return gitPath() }

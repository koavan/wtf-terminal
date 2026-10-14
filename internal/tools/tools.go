// Package tools checks for the command-line programs side-pane features run
// (kubectl, the AWS CLI, git, OpenSSH…) and installs a missing one when the
// user asks: in a terminal tab, after showing exactly what will run.
//
// Nothing here runs on its own: the Tools dialog asks when it opens, and the
// first-run check only looks for the programs (no version, no network).
package tools

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/dayanraj/wtf/internal/paths"
)

// Status is one tool as found on this machine, and how to install it.
type Status struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	About    string   `json:"about"`
	Programs []string `json:"programs"`
	Found    bool     `json:"found"`
	// Where the first program was found, and the version it reports.
	Path    string `json:"path"`
	Version string `json:"version"`
	// Programs it provides that aren't there, e.g. ssh-keyscan alone.
	Missing  []string `json:"missing"`
	UsedBy   []string `json:"usedBy"`
	Homepage string   `json:"homepage"`
	Methods  []Method `json:"methods"`
	// It comes with the system ("macOS"): nothing to install.
	BuiltIn string `json:"builtIn"`
	// A security scanner (Findings tab), not something features need.
	Scanner bool `json:"scanner"`
}

type Service struct {
	stateDir, cacheDir string
	shell              func() (string, bool)
	mu                 sync.Mutex
}

func NewService(stateDir string, shell func() (string, bool)) *Service {
	s := &Service{stateDir: stateDir, shell: shell}
	if dir, err := paths.UserCacheDir(); err == nil {
		s.cacheDir = filepath.Join(dir, "wtf")
	}
	return s
}

const versionTimeout = 5 * time.Second

// Check looks for every tool, using the focused shell's PATH first, and
// asks each found one for its version (in parallel).
func (s *Service) Check(shellPATH string) []Status {
	p := s.platform(shellPATH)
	out := make([]Status, 0, len(catalog))
	for _, t := range catalog {
		if t.only != "" && t.only != p.goos {
			continue
		}
		out = append(out, s.status(t, p))
	}
	var wg sync.WaitGroup
	for i := range out {
		t, _ := lookup(out[i].ID)
		if !out[i].Found || t.version == nil {
			continue
		}
		wg.Add(1)
		go func(st *Status) {
			defer wg.Done()
			st.Version = version(st.Path, t.version)
		}(&out[i])
	}
	wg.Wait()
	return out
}

func (s *Service) status(t tool, p platform) Status {
	st := Status{
		ID: t.id, Name: t.name, About: t.about, Programs: t.programs,
		UsedBy: t.usedBy, Homepage: t.homepage, BuiltIn: t.builtIn[p.goos], Scanner: t.scanner,
	}
	for i, prog := range t.programs {
		path := find(prog, p)
		switch {
		case path == "" && !t.any:
			st.Missing = append(st.Missing, prog)
		case path != "" && st.Path == "" && (i == 0 || t.any):
			st.Path = path
		}
	}
	st.Found = st.Path != ""
	if !st.Found || len(st.Missing) > 0 {
		st.Methods = p.methods(t)
	}
	return st
}

// find is Find, except that on macOS /usr/bin/git without the Command Line
// Tools is only a stub that offers to install them when run.
func find(prog string, p platform) string {
	path := Find(prog, p.shellPATH)
	if p.goos == "darwin" && path == "/usr/bin/git" && exec.Command("/usr/bin/xcode-select", "-p").Run() != nil {
		return ""
	}
	return path
}

// version is the first line a program prints for its version arguments
// (some, like ssh -V, print it on stderr).
func version(path string, args []string) string {
	ctx, cancel := context.WithTimeout(context.Background(), versionTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = append(os.Environ(), "AWS_PAGER=", "AWS_CLI_AUTO_PROMPT=off")
	out, err := cmd.CombinedOutput()
	if err != nil && len(out) == 0 {
		return ""
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	line = strings.TrimPrefix(line, "Client Version: ") // kubectl
	if len(line) > 80 {
		line = line[:80] + "…"
	}
	return line
}

func (s *Service) platform(shellPATH string) platform {
	home, _ := os.UserHomeDir()
	shell := os.Getenv("SHELL")
	if s.shell != nil {
		if sh, _ := s.shell(); sh != "" {
			shell = sh
		}
	}
	return detectPlatform(runtime.GOOS, runtime.GOARCH, home, shellPATH, shell)
}

// Install writes the method's script, when it has one, and returns the
// command for a terminal tab to run. The plan is worked out again here, so
// what runs is what the dialog showed, not something the page sent.
func (s *Service) Install(id, method, shellPATH string) (string, error) {
	t, ok := lookup(id)
	if !ok {
		return "", errors.New("unknown tool " + id)
	}
	for _, m := range s.platform(shellPATH).methods(t) {
		if m.ID != method {
			continue
		}
		if m.Script == "" {
			return m.Command, nil
		}
		return s.writeScript(t.id+"-"+m.ID+".sh", m.Script)
	}
	return "", errors.New("that way of installing " + t.name + " isn't available here")
}

func (s *Service) writeScript(name, script string) (string, error) {
	if s.cacheDir == "" {
		return "", errors.New("no cache folder to write the install script to")
	}
	dir := filepath.Join(s.cacheDir, "install")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	// A fresh file each time: never run something left there before.
	_ = os.Remove(path)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(script); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return "sh " + shellQuote(path), nil
}

// FirstRun is for the first start of wtf only: the tools that aren't
// installed, by name, looked for without running anything. Later calls
// return nothing.
func (s *Service) FirstRun(shellPATH string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	marker := filepath.Join(s.stateDir, "tools-checked")
	if _, err := os.Stat(marker); err == nil || s.stateDir == "" {
		return nil
	}
	if err := os.MkdirAll(s.stateDir, 0o700); err != nil {
		return nil
	}
	if err := os.WriteFile(marker, []byte(time.Now().Format(time.RFC3339)+"\n"), 0o600); err != nil {
		return nil
	}
	p := s.platform(shellPATH)
	var missing []string
	for _, t := range catalog {
		if (t.only != "" && t.only != p.goos) || t.scanner {
			continue
		}
		if st := s.status(t, p); !st.Found && st.BuiltIn == "" {
			missing = append(missing, t.name)
		}
	}
	return missing
}

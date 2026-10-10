// Package files lists directories for the side pane's file tree and opens
// files with the system.
package files

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// MaxEntries caps one listing; the tree says when a folder was cut short.
const MaxEntries = 5000

type Entry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Dir  bool   `json:"dir"`
	// Symlink entries report Dir for what they point at.
	Symlink bool `json:"symlink"`
	Hidden  bool `json:"hidden"`
	// Ignored by git, or a folder that is noise in a tree (.git, node_modules).
	Ignored bool `json:"ignored"`
}

type Listing struct {
	Path      string  `json:"path"`
	Entries   []Entry `json:"entries"`
	Truncated bool    `json:"truncated"`
}

// Always shown dimmed, git or not.
var noisy = map[string]bool{".git": true, "node_modules": true, ".DS_Store": true}

// Never shown.
var skipped = map[string]bool{".DS_Store": true}

type Service struct {
	gitOnce sync.Once
	git     string // "" when git isn't usable
}

func NewService() *Service { return &Service{} }

// List returns a directory's entries: folders first, then files, each
// sorted case-insensitively.
func (s *Service) List(dir string) (Listing, error) {
	if !filepath.IsAbs(dir) {
		return Listing{}, fmt.Errorf("not an absolute path: %q", dir)
	}
	dir = filepath.Clean(dir)
	des, err := os.ReadDir(dir)
	if err != nil {
		return Listing{}, friendly(err)
	}

	out := Listing{Path: dir, Entries: make([]Entry, 0, min(len(des), MaxEntries))}
	for _, de := range des {
		name := de.Name()
		if skipped[name] {
			continue
		}
		if len(out.Entries) == MaxEntries {
			out.Truncated = true
			break
		}
		e := Entry{
			Name:    name,
			Path:    filepath.Join(dir, name),
			Dir:     de.IsDir(),
			Symlink: de.Type()&os.ModeSymlink != 0,
			Hidden:  strings.HasPrefix(name, "."),
			Ignored: noisy[name],
		}
		if e.Symlink {
			if fi, err := os.Stat(e.Path); err == nil {
				e.Dir = fi.IsDir()
			}
		}
		out.Entries = append(out.Entries, e)
	}

	sort.Slice(out.Entries, func(i, j int) bool {
		a, b := out.Entries[i], out.Entries[j]
		if a.Dir != b.Dir {
			return a.Dir
		}
		la, lb := strings.ToLower(a.Name), strings.ToLower(b.Name)
		if la != lb {
			return la < lb
		}
		return a.Name < b.Name
	})

	for _, name := range s.gitIgnored(dir, out.Entries) {
		for i := range out.Entries {
			if out.Entries[i].Name == name {
				out.Entries[i].Ignored = true
			}
		}
	}
	return out, nil
}

// gitIgnored asks git which entries its ignore rules exclude. Outside a
// repository, or without git, nothing is ignored.
func (s *Service) gitIgnored(dir string, entries []Entry) []string {
	git := s.gitPath()
	if git == "" || len(entries) == 0 {
		return nil
	}
	var in bytes.Buffer
	for _, e := range entries {
		in.WriteString(e.Name)
		in.WriteByte(0)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, git, "-C", dir, "check-ignore", "-z", "--stdin")
	cmd.Stdin = &in
	out, _ := cmd.Output() // exit 1: nothing ignored; 128: not a repository
	var names []string
	for _, n := range bytes.Split(out, []byte{0}) {
		if len(n) > 0 {
			names = append(names, string(n))
		}
	}
	return names
}

// gitPath finds a git that can run. On macOS /usr/bin/git is a stub that
// pops up the developer tools installer when they're missing, so only use
// it when those tools are really installed.
func (s *Service) gitPath() string {
	s.gitOnce.Do(func() {
		path, err := exec.LookPath("git")
		if err != nil {
			return
		}
		if runtime.GOOS == "darwin" && path == "/usr/bin/git" {
			if exec.Command("/usr/bin/xcode-select", "-p").Run() != nil {
				return
			}
		}
		s.git = path
	})
	return s.git
}

// Open opens a file or folder with its default application.
func (s *Service) Open(path string) error {
	if err := exists(path); err != nil {
		return err
	}
	if runtime.GOOS == "darwin" {
		return exec.Command("/usr/bin/open", path).Start()
	}
	return exec.Command("xdg-open", path).Start()
}

// ResolveFile turns a file name printed in a terminal (absolute, ~/…, or
// relative to the shell's folder) into the absolute path of a regular file
// on this machine, or "" when there's no such file — so output from another
// host, or a host:port that looks like a file:line, gets no link.
func (s *Service) ResolveFile(dir, name string) string {
	switch {
	case name == "":
		return ""
	case name == "~" || strings.HasPrefix(name, "~/"):
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		name = filepath.Join(home, name[1:])
	case !filepath.IsAbs(name):
		if !filepath.IsAbs(dir) {
			return ""
		}
		name = filepath.Join(dir, name)
	}
	name = filepath.Clean(name)
	if fi, err := os.Stat(name); err != nil || !fi.Mode().IsRegular() {
		return ""
	}
	return name
}

// SaveText writes text the user chose to save (a terminal's output, a
// transcript) to the file they picked. New files are 0600: output can
// hold secrets.
func (s *Service) SaveText(path, text string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("not an absolute path: %q", path)
	}
	return os.WriteFile(path, []byte(text), 0o600)
}

// Reveal shows a file in Finder (or the file manager, on Linux).
func (s *Service) Reveal(path string) error {
	if err := exists(path); err != nil {
		return err
	}
	if runtime.GOOS == "darwin" {
		return exec.Command("/usr/bin/open", "-R", path).Start()
	}
	// Most Linux file managers can't select a file; open its folder.
	return exec.Command("xdg-open", filepath.Dir(path)).Start()
}

func exists(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("not an absolute path: %q", path)
	}
	_, err := os.Lstat(path)
	return friendly(err)
}

func friendly(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, os.ErrPermission):
		return errors.New("permission denied")
	case errors.Is(err, os.ErrNotExist):
		return errors.New("no longer exists")
	default:
		return err
	}
}

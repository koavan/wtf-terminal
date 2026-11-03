package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/dayanraj/wtf/internal/activity"
	"github.com/dayanraj/wtf/internal/events"
)

const (
	EventChanged = "config:changed"
	pollInterval = 750 * time.Millisecond
)

// Resolved is the config as the frontend uses it.
type Resolved struct {
	Config Config `json:"config"`
	Dark   Theme  `json:"dark"`
	Light  Theme  `json:"light"`
	// Problems found while loading; the frontend shows them.
	Problems []string `json:"problems"`
	Path     string   `json:"path"`
}

// Service loads the config, watches it, and pushes changes to the frontend.
type Service struct {
	dir string
	// Watching pauses while no window is in front; nil never pauses.
	active *activity.Gate

	mu       sync.Mutex
	current  Resolved
	themes   []Theme
	snapshot string // fingerprint of the files on disk
}

func NewService(dir string, active *activity.Gate) *Service {
	s := &Service{dir: dir, active: active}
	s.reload()
	return s
}

func (s *Service) path() string      { return filepath.Join(s.dir, "config.toml") }
func (s *Service) themesDir() string { return filepath.Join(s.dir, "themes") }

func (s *Service) startup(ctx context.Context) error {
	go s.watch(ctx)
	return nil
}

// Current returns the loaded config.
func (s *Service) Current() Resolved {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current
}

// Themes lists every available theme, built-in ones first.
func (s *Service) Themes() []Theme {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.themes
}

// SetTheme writes the theme setting into the config file.
func (s *Service) SetTheme(spec string) error {
	return s.set("theme", fmt.Sprintf("%q", spec))
}

// SetSidebarAutoRefresh turns the side pane's background updates on or off.
func (s *Service) SetSidebarAutoRefresh(on bool) error {
	return s.set("sidebar-auto-refresh", strconv.FormatBool(on))
}

// SetRedactSecrets sets whether Hide Secrets is on when wtf starts.
func (s *Service) SetRedactSecrets(on bool) error {
	return s.set("redact-secrets", strconv.FormatBool(on))
}

// SetConfirmPaste sets whether text with more than one line or control
// characters is shown before it's pasted.
func (s *Service) SetConfirmPaste(on bool) error {
	return s.set("confirm-paste", strconv.FormatBool(on))
}

// SetClaudeAutoMode lets Claude Code's own permission mode decide which
// commands in a Claude pane run without asking (off: wtf asks every time).
func (s *Service) SetClaudeAutoMode(on bool) error {
	return s.set("claude-auto-mode", strconv.FormatBool(on))
}

// SetShowUsage shows or hides wtf's CPU and memory in the address bar.
func (s *Service) SetShowUsage(on bool) error {
	return s.set("show-usage", strconv.FormatBool(on))
}

// set writes one setting into the config file, creating it if needed.
func (s *Service) set(key, value string) error {
	return s.edit(func(data string) (string, error) { return withSetting(data, key, value), nil })
}

// edit rewrites the config file (the template when there's none yet), then
// reloads it.
func (s *Service) edit(change func(string) (string, error)) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	data, err := os.ReadFile(s.path())
	if errors.Is(err, os.ErrNotExist) {
		data, err = []byte(template), nil
	}
	if err != nil {
		return err
	}
	out, err := change(string(data))
	if err != nil {
		return err
	}
	if err := writeAtomic(s.path(), []byte(out)); err != nil {
		return err
	}
	s.reloadAndNotify()
	return nil
}

// OpenConfigFile opens the config file in the default text editor,
// creating it with every setting documented if it doesn't exist yet.
func (s *Service) OpenConfigFile() error {
	if err := os.MkdirAll(s.themesDir(), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(s.path()); errors.Is(err, os.ErrNotExist) {
		if err := writeAtomic(s.path(), []byte(template)); err != nil {
			return err
		}
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("/usr/bin/open", "-t", s.path())
	} else {
		cmd = exec.Command("xdg-open", s.path())
	}
	return cmd.Start()
}

// OpenThemesFolder reveals the folder user themes are loaded from.
func (s *Service) OpenThemesFolder() error {
	if err := os.MkdirAll(s.themesDir(), 0o755); err != nil {
		return err
	}
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "/usr/bin/open"
	}
	return exec.Command(opener, s.themesDir()).Start()
}

func (s *Service) watch(ctx context.Context) {
	t := time.NewTicker(pollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		// In the background, wait; coming back, check at once.
		if !s.active.Wait(ctx) {
			return
		}
		s.mu.Lock()
		changed := s.fingerprint() != s.snapshot
		s.mu.Unlock()
		if changed {
			s.reloadAndNotify()
		}
	}
}

func (s *Service) reloadAndNotify() {
	r := s.reload()
	events.Emit(EventChanged, r)
}

func (s *Service) reload() Resolved {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot = s.fingerprint()

	problems := []string{} // never null in JSON
	cfg := Defaults()
	data, err := os.ReadFile(s.path())
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		problems = append(problems, err.Error())
	default:
		parsed, warnings, perr := Parse(string(data))
		if perr != nil {
			// Keep running with the last good settings until it's fixed.
			problems = append(problems, fmt.Sprintf("config.toml %v", perr))
			if s.current.Path != "" {
				cfg = s.current.Config
			}
		} else {
			cfg = parsed
			problems = append(problems, warnings...)
		}
	}

	themes, themeProblems := Themes(s.themesDir())
	problems = append(problems, themeProblems...)
	dark, light, pairProblems := ThemePair(cfg.Theme, themes)
	problems = append(problems, pairProblems...)

	s.themes = themes
	s.current = Resolved{Config: cfg, Dark: dark, Light: light, Problems: problems, Path: s.path()}
	return s.current
}

// fingerprint changes whenever the config file or any theme file does.
func (s *Service) fingerprint() string {
	fp := ""
	if fi, err := os.Stat(s.path()); err == nil {
		fp += fmt.Sprintf("%d/%d;", fi.ModTime().UnixNano(), fi.Size())
	}
	entries, _ := os.ReadDir(s.themesDir())
	for _, e := range entries {
		if fi, err := e.Info(); err == nil {
			fp += fmt.Sprintf("%s:%d/%d;", e.Name(), fi.ModTime().UnixNano(), fi.Size())
		}
	}
	return fp
}

func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config.*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

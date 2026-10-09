// Package terminal owns shell sessions and exposes them to the frontend:
// control calls go through Wails bindings, the byte stream through a
// loopback WebSocket (see stream.go).
package terminal

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const (
	EventExit = "terminal:exit"
	EventInfo = "terminal:info"

	infoPollInterval = 700 * time.Millisecond
)

// ExitEvent is emitted when a session's shell has exited.
type ExitEvent struct {
	ID   string `json:"id"`
	Code int    `json:"code"`
}

func init() {
	application.RegisterEvent[ExitEvent](EventExit)
	application.RegisterEvent[Info](EventInfo)
}

// Endpoint tells the frontend where to stream session I/O.
type Endpoint struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

// Settings reports the user's shell preferences; read for each new session.
type Settings func() (shell string, integration bool)

type Service struct {
	version  string
	settings Settings

	mu       sync.Mutex
	sessions map[string]*Session
	lastInfo map[string]Info
	nextID   atomic.Uint64

	stream *streamServer
	shell  string
	home   string
	// Where the shell integration scripts live; "" disables integration.
	integrationDir string

	// Set while quitting: shells killed by shutdown must not be reported as
	// exits, or the frontend would close their tabs and save an empty session.
	shuttingDown atomic.Bool
}

func NewService(version string, settings Settings) *Service {
	return &Service{
		version:  version,
		settings: settings,
		sessions: map[string]*Session{},
		lastInfo: map[string]Info{},
	}
}

func (s *Service) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	sv, err := newStreamServer(s.get)
	if err != nil {
		return fmt.Errorf("starting terminal stream server: %w", err)
	}
	s.stream = sv
	s.shell = resolveShell()
	s.home, _ = os.UserHomeDir()
	if cache, err := os.UserCacheDir(); err == nil {
		dir := filepath.Join(cache, "wtf", "shell-integration")
		if err := installIntegration(dir); err == nil {
			s.integrationDir = dir
		} else {
			log.Printf("shell integration disabled: %v", err)
		}
	}
	go s.pollInfo(ctx)
	return nil
}

func (s *Service) ServiceShutdown() error {
	s.shuttingDown.Store(true)
	s.mu.Lock()
	all := make([]*Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		all = append(all, sess)
	}
	s.mu.Unlock()
	for _, sess := range all {
		sess.Close()
	}
	if s.stream != nil {
		s.stream.close()
	}
	return nil
}

// Endpoint returns the WebSocket address and auth token for session streams.
func (s *Service) Endpoint() Endpoint {
	return Endpoint{URL: s.stream.url(), Token: s.stream.token}
}

// Home returns the user's home directory, for prettifying paths.
func (s *Service) Home() string { return s.home }

// Create starts a new shell for a window. An empty or invalid cwd falls
// back to $HOME.
// ShellInfo is the shell new tabs start (the configured one, else the
// login shell) and whether shell integration loads into it — for About.
func (s *Service) ShellInfo() (shell string, integration bool) {
	shell, integration = s.shell, s.integrationDir != ""
	if s.settings != nil {
		custom, on := s.settings()
		if custom != "" {
			if path, err := exec.LookPath(custom); err == nil && isExecutable(path) {
				shell = path
			}
		}
		integration = integration && on
	}
	base := filepath.Base(shell)
	return shell, integration && (base == "bash" || base == "zsh")
}

func (s *Service) Create(window string, cols, rows int, cwd string) (string, error) {
	if fi, err := os.Stat(cwd); cwd == "" || err != nil || !fi.IsDir() {
		cwd = s.home
	}
	id := fmt.Sprintf("s%d", s.nextID.Add(1))
	shell, integrationDir := s.shell, s.integrationDir
	if s.settings != nil {
		custom, integration := s.settings()
		if custom != "" {
			if path, err := exec.LookPath(custom); err == nil && isExecutable(path) {
				shell = path
			} else {
				log.Printf("shell %q not found; using %s", custom, shell)
			}
		}
		if !integration {
			integrationDir = ""
		}
	}
	args, extraEnv := integrate(integrationDir, shell)
	sess, err := startSession(id, spawnOptions{
		Shell: shell,
		Args:  args,
		Cwd:   cwd,
		Cols:  clampDim(cols),
		Rows:  clampDim(rows),
		Env:   append(s.env(), extraEnv...),
	})
	if err != nil {
		return "", fmt.Errorf("starting %s: %w", shell, err)
	}

	s.mu.Lock()
	sess.window = window
	s.sessions[id] = sess
	s.mu.Unlock()

	go func() {
		<-sess.Done
		sess.Close()
		s.mu.Lock()
		delete(s.sessions, id)
		delete(s.lastInfo, id)
		s.mu.Unlock()
		if !s.shuttingDown.Load() {
			application.Get().Event.Emit(EventExit, ExitEvent{ID: id, Code: sess.ExitCode()})
		}
	}()
	return id, nil
}

// Adopt hands a running session to another window (a tab moved there).
func (s *Service) Adopt(id, window string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessions[id]
	if sess == nil {
		return fmt.Errorf("no session %q", id)
	}
	sess.window = window
	return nil
}

// CloseWindow hangs up every session shown in a window that went away.
func (s *Service) CloseWindow(window string) {
	s.mu.Lock()
	var doomed []*Session
	for _, sess := range s.sessions {
		if sess.window == window {
			doomed = append(doomed, sess)
		}
	}
	s.mu.Unlock()
	for _, sess := range doomed {
		sess.Close()
	}
}

// ShellPIDs maps each live session's shell process to its session ID, so
// other services can tell which terminal a process belongs to.
func (s *Service) ShellPIDs() map[int]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[int]string, len(s.sessions))
	for id, sess := range s.sessions {
		if sess.cmd.Process != nil && !sess.closed.Load() {
			out[sess.cmd.Process.Pid] = id
		}
	}
	return out
}

// Running describes a program other than the shell running in a session.
type Running struct {
	Window  string `json:"window"`
	Process string `json:"process"`
	Cwd     string `json:"cwd"`
}

// RunningProcesses lists sessions that are busy with something other than
// their shell, so quitting can ask first.
func (s *Service) RunningProcesses() []Running {
	s.mu.Lock()
	all := make([]*Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		if !sess.closed.Load() {
			all = append(all, sess)
		}
	}
	s.mu.Unlock()

	out := []Running{}
	for _, sess := range all {
		info, err := sess.info()
		if err != nil || info.IsShell || info.Process == "" {
			continue
		}
		s.mu.Lock()
		window := sess.window
		s.mu.Unlock()
		out = append(out, Running{Window: window, Process: info.Process, Cwd: info.Cwd})
	}
	return out
}

func (s *Service) Resize(id string, cols, rows int) error {
	sess := s.get(id)
	if sess == nil {
		return fmt.Errorf("no session %q", id)
	}
	return sess.Resize(clampDim(cols), clampDim(rows))
}

// Close hangs up a session. The exit event follows once the shell is gone.
func (s *Service) Close(id string) {
	if sess := s.get(id); sess != nil {
		sess.Close()
	}
}

// Info is what a session runs and where, right now. The poller only sends
// changes, and one sent before the frontend linked the session to its pane
// is lost (a slow first launch), so a pane asks once it's linked.
func (s *Service) Info(id string) (Info, error) {
	sess := s.get(id)
	if sess == nil {
		return Info{}, fmt.Errorf("no session %q", id)
	}
	return sess.info()
}

func (s *Service) get(id string) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions[id]
}

// pollInfo watches each session's foreground process and working directory,
// and tells the frontend when either changes (tab titles, address bar).
func (s *Service) pollInfo(ctx context.Context) {
	t := time.NewTicker(infoPollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		s.mu.Lock()
		all := make([]*Session, 0, len(s.sessions))
		for _, sess := range s.sessions {
			all = append(all, sess)
		}
		s.mu.Unlock()

		for _, sess := range all {
			info, err := sess.info()
			if err != nil || info.Process == "" {
				continue
			}
			s.mu.Lock()
			prev, seen := s.lastInfo[sess.ID]
			_, alive := s.sessions[sess.ID]
			changed := alive && (!seen || prev != info)
			if changed {
				s.lastInfo[sess.ID] = info
			}
			s.mu.Unlock()
			if changed {
				application.Get().Event.Emit(EventInfo, info)
			}
		}
	}
}

func (s *Service) env() []string {
	drop := map[string]bool{
		"TERM": true, "COLORTERM": true, "TERM_PROGRAM": true,
		"TERM_PROGRAM_VERSION": true, "TERM_SESSION_ID": true,
		"WTF_SHELL_INTEGRATION": true, "WTF_ORIG_ZDOTDIR": true,
	}
	env := make([]string, 0, len(os.Environ())+5)
	hasLang := false
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if drop[k] || strings.HasPrefix(k, "ITERM_") {
			continue
		}
		if k == "LANG" {
			hasLang = true
		}
		env = append(env, kv)
	}
	env = append(env,
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"TERM_PROGRAM=wtf",
		"TERM_PROGRAM_VERSION="+s.version,
	)
	// Apps launched from Finder get no LANG, which breaks UTF-8 in most shells.
	if !hasLang {
		env = append(env, "LANG=en_US.UTF-8")
	}
	return env
}

func resolveShell() string {
	if sh := os.Getenv("SHELL"); isExecutable(sh) {
		return sh
	}
	if u, err := user.Current(); err == nil {
		if sh := loginShell(u.Username); isExecutable(sh) {
			return sh
		}
	}
	return fallbackShell
}

func isExecutable(path string) bool {
	if path == "" {
		return false
	}
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0
}

func clampDim(n int) uint16 {
	return uint16(max(1, min(n, 1000)))
}

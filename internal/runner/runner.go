// Package runner keeps wtf's agent runners (#41): remote Linux machines,
// reached over SSH, where Claude Code agents run in tmux under a
// wtf-runner daemon. Agents there use the Claude account you sign in to
// on the runner (`claude auth login`), never an API key.
//
// Phase 1: adding a runner from an SSH host, checking it (facts only:
// OS, arch, tmux, Claude Code and its sign-in, wtf-runner), installing
// wtf-runner, and the commands the UIs run in a tab for what needs you
// (installing Claude Code or tmux, signing in, first-run setup).
//
// Nothing here changes a runner without the UI asking you first: Install
// and Uninstall are called only after you confirm, and the rest runs in a
// tab where you see every step.
package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/dayanraj/wtf/internal/bridge"
	"github.com/dayanraj/wtf/internal/runnerd"
	"github.com/dayanraj/wtf/internal/uicore"
)

// Dir is where wtf-runner lives on a runner, under the user's home.
const Dir = ".local/share/wtf-runner"

// Protocol is what this wtf and wtf-runner speak; a runner with another
// one needs updating.
const Protocol = runnerd.Protocol

const (
	checkTimeout   = 30 * time.Second
	installTimeout = 2 * time.Minute
)

// Runner is one saved runner: an SSH host, by its alias (and port for a
// known_hosts entry, which has no config block).
type Runner struct {
	ID    string    `json:"id"`
	Alias string    `json:"alias"`
	Port  string    `json:"port,omitempty"`
	Name  string    `json:"name"`
	Added time.Time `json:"added"`
}

// Status is a runner with what its last check found.
type Status struct {
	Runner
	Facts   *Facts    `json:"facts,omitempty"`
	Checked time.Time `json:"checked,omitzero"`
	// What stops agents running there, in the order to fix them.
	Problems []string `json:"problems"`
	// Its wtf-runner isn't this wtf's (version or protocol).
	Outdated bool `json:"outdated"`
}

// runSSH runs ssh with args, stdin as its input; tests replace it.
type runSSH func(ctx context.Context, args []string, stdin io.Reader) (stdout, stderr []byte, err error)

type Service struct {
	file    string
	version string
	// The folder holding linux-amd64/ and linux-arm64/, each with
	// wtf-runner and wtf-mcp ("" when this build has none).
	payload string
	// Claude Code settings with wtf's status hooks (the plugin's
	// status-settings.json), installed next to them.
	StatusSettings func() string
	ssh            runSSH

	// wtf's bridge: runners' agents are mirrored there (phase 5).
	Bridge *bridge.Service
	// Tests run the stream's ssh another way.
	sshCommand func(ctx context.Context, args []string) *exec.Cmd

	mu      sync.Mutex
	conns   map[string]*conn
	runners []Runner
	loaded  bool
	facts   map[string]Facts
	checked map[string]time.Time
}

// NewService keeps runners in stateDir/runners.json.
func NewService(stateDir, version string) *Service {
	s := &Service{file: filepath.Join(stateDir, "runners.json"), version: version, ssh: execSSH,
		facts: map[string]Facts{}, checked: map[string]time.Time{}}
	if exe, err := os.Executable(); err == nil {
		s.payload = findPayload(filepath.Dir(exe))
	}
	return s
}

// findPayload looks for the runner binaries next to the app (a dev
// build), in the macOS bundle's Resources, or where the Linux packages put
// them (/usr/bin/wtf → /usr/lib/wtf/runner).
func findPayload(exeDir string) string {
	for _, d := range []string{
		filepath.Join(exeDir, "runner"),
		filepath.Join(exeDir, "..", "Resources", "runner"),
		filepath.Join(exeDir, "..", "lib", "wtf", "runner"),
	} {
		for _, a := range []string{"amd64", "arm64"} {
			if fi, err := os.Stat(filepath.Join(d, "linux-"+a, "wtf-runner")); err == nil && fi.Mode().IsRegular() {
				return filepath.Clean(d)
			}
		}
	}
	return ""
}

func execSSH(ctx context.Context, args []string, stdin io.Reader) ([]byte, []byte, error) {
	var out, errb bytes.Buffer
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, &out, &errb
	err := cmd.Run()
	return out.Bytes(), errb.Bytes(), err
}

// ID of a runner for an SSH host.
func ID(alias, port string) string { return alias + ":" + port }

// sshArgs connects to r without asking anything: no password or passphrase
// prompt and no unknown host key (connect once in a tab to accept it).
func sshArgs(r Runner, remote string) []string {
	args := []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-o", "ServerAliveInterval=15"}
	if r.Port != "" && r.Port != "22" {
		args = append(args, "-p", r.Port)
	}
	return append(args, "--", r.Alias, remote)
}

// TabSSH is the ssh command a tab runs to do remote on r, where you see it
// and can answer it (a password, a sign-in code).
func TabSSH(r Runner, remote string) string {
	port := ""
	if r.Port != "" && r.Port != "22" {
		port = "-p " + uicore.ShellQuote(r.Port) + " "
	}
	// A host that doesn't answer says so in 10 s rather than a blank tab.
	return "ssh -t -o ConnectTimeout=10 " + port + uicore.ShellQuote(r.Alias) + " " + uicore.ShellQuote(remote)
}

func (s *Service) load() {
	if s.loaded {
		return
	}
	s.loaded = true
	data, err := os.ReadFile(s.file)
	if err == nil {
		_ = json.Unmarshal(data, &s.runners)
	}
}

func (s *Service) save() error {
	data, err := json.MarshalIndent(s.runners, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.file), 0o700); err != nil {
		return err
	}
	tmp := s.file + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.file)
}

func (s *Service) find(id string) (Runner, bool) {
	s.load()
	i := slices.IndexFunc(s.runners, func(r Runner) bool { return r.ID == id })
	if i < 0 {
		return Runner{}, false
	}
	return s.runners[i], true
}

// List is every runner, in the order added, with its last check.
func (s *Service) List() []Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	out := make([]Status, 0, len(s.runners))
	for _, r := range s.runners {
		out = append(out, s.status(r))
	}
	return out
}

func (s *Service) status(r Runner) Status {
	st := Status{Runner: r, Problems: []string{}}
	if f, ok := s.facts[r.ID]; ok {
		st.Facts, st.Checked = &f, s.checked[r.ID]
		st.Problems = f.Problems()
		st.Outdated = f.RunnerVersion != "" && (f.RunnerVersion != s.version || f.RunnerProtocol != Protocol)
	}
	return st
}

// Add saves an SSH host as a runner; adding one twice is an error. It
// doesn't connect: Check does.
func (s *Service) Add(alias, port, name string) (Status, error) {
	alias, port, name = strings.TrimSpace(alias), strings.TrimSpace(port), strings.TrimSpace(name)
	if alias == "" || strings.HasPrefix(alias, "-") || strings.ContainsAny(alias, " \t\n") {
		return Status{}, fmt.Errorf("%q isn't an SSH host", alias)
	}
	if name == "" {
		name = alias
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	id := ID(alias, port)
	if _, ok := s.find(id); ok {
		return Status{}, fmt.Errorf("%s is already a runner", alias)
	}
	r := Runner{ID: id, Alias: alias, Port: port, Name: name, Added: time.Now()}
	s.runners = append(s.runners, r)
	if err := s.save(); err != nil {
		s.runners = s.runners[:len(s.runners)-1]
		return Status{}, err
	}
	return s.status(r), nil
}

// Rename changes the name wtf shows for a runner.
func (s *Service) Rename(id, name string) error {
	name = strings.TrimSpace(name)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	i := slices.IndexFunc(s.runners, func(r Runner) bool { return r.ID == id })
	if i < 0 {
		return fmt.Errorf("no runner %q", id)
	}
	if name == "" {
		name = s.runners[i].Alias
	}
	s.runners[i].Name = name
	return s.save()
}

// Remove forgets a runner in wtf. It leaves the runner itself alone
// (Uninstall removes wtf-runner there).
func (s *Service) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	i := slices.IndexFunc(s.runners, func(r Runner) bool { return r.ID == id })
	if i < 0 {
		return fmt.Errorf("no runner %q", id)
	}
	s.runners = slices.Delete(s.runners, i, i+1)
	delete(s.facts, id)
	delete(s.checked, id)
	if c := s.conns[id]; c != nil {
		delete(s.conns, id)
		c.cancel()
		go c.unmirrorAll()
	}
	return s.save()
}

func (s *Service) runner(id string) (Runner, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.find(id)
	if !ok {
		return Runner{}, fmt.Errorf("no runner %q", id)
	}
	return r, nil
}

// Check connects to a runner and reads its facts; it changes nothing
// there. It takes up to 30 s, so UIs call it off the main thread.
func (s *Service) Check(id string) (Status, error) {
	r, err := s.runner(id)
	if err != nil {
		return Status{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()
	out, errOut, err := s.ssh(ctx, sshArgs(r, "sh -s"), strings.NewReader(probeScript))
	f := parseProbe(out)
	if !f.Reachable {
		f.Error = sshError(ctx, err, errOut)
	}
	s.mu.Lock()
	s.facts[id], s.checked[id] = f, time.Now()
	r, ok := s.find(id)
	var st Status
	if ok {
		st = s.status(r)
	}
	s.mu.Unlock()
	if !ok {
		return Status{}, fmt.Errorf("no runner %q", id)
	}
	// Its agents stream in once wtf-runner is there.
	if f.Reachable && f.RunnerVersion != "" && f.RunnerProtocol == Protocol {
		s.connect(r)
	}
	return st, nil
}

// sshError says why ssh failed, from its last line of stderr.
func sshError(ctx context.Context, err error, stderr []byte) string {
	if ctx.Err() != nil {
		return "it didn't answer in time"
	}
	lines := strings.Split(strings.TrimSpace(string(stderr)), "\n")
	if last := strings.TrimSpace(lines[len(lines)-1]); last != "" {
		if strings.Contains(last, "Host key verification failed") {
			return last + " (connect once in a tab to check and accept its host key)"
		}
		return last
	}
	if err != nil {
		return err.Error()
	}
	return "the check stopped early"
}

// Install copies this wtf's wtf-runner, wtf-mcp and status hooks to the
// runner's ~/.local/share/wtf-runner (replacing older ones), then checks
// it again. Call it only after you confirm.
func (s *Service) Install(id string) (Status, error) {
	r, err := s.runner(id)
	if err != nil {
		return Status{}, err
	}
	s.mu.Lock()
	f, checked := s.facts[id]
	s.mu.Unlock()
	if !checked || !f.Reachable {
		return Status{}, errors.New("check the runner first")
	}
	if f.OS != "Linux" || f.GoArch == "" {
		return Status{}, fmt.Errorf("wtf has no runner for %s %s", f.OS, f.Arch)
	}
	files, err := s.payloadFiles(f.GoArch)
	if err != nil {
		return Status{}, err
	}
	archive, err := tarOf(files)
	if err != nil {
		return Status{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), installTimeout)
	defer cancel()
	if _, errOut, err := s.ssh(ctx, sshArgs(r, installScript), bytes.NewReader(archive)); err != nil {
		return Status{}, fmt.Errorf("installing on %s: %s", r.Name, sshError(ctx, err, errOut))
	}
	return s.Check(id)
}

// installScript unpacks the archive on stdin into a temporary folder next
// to wtf-runner's, then moves each file into place, so a failed copy
// leaves the old ones working.
const installScript = `sh -c 'set -e
d="$HOME/` + Dir + `"
mkdir -p "$d"
chmod 700 "$d"
t=$(mktemp -d "$d/.install.XXXXXX")
trap "rm -rf \"$t\"" EXIT
tar -xf - -C "$t"
for f in wtf-runner wtf-mcp status-settings.json; do mv -f "$t/$f" "$d/$f"; done
"$d/wtf-runner" stop >/dev/null 2>&1 || true'`

// Uninstall removes ~/.local/share/wtf-runner from the runner (not Claude
// Code, its sign-in or tmux). Call it only after you confirm.
func (s *Service) Uninstall(id string) error {
	r, err := s.runner(id)
	if err != nil {
		return err
	}
	s.disconnect(id)
	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()
	if _, errOut, err := s.ssh(ctx, sshArgs(r, `sh -c '"$HOME/`+Dir+`/wtf-runner" stop >/dev/null 2>&1; rm -rf "$HOME/`+Dir+`"'`), nil); err != nil {
		return fmt.Errorf("removing wtf-runner from %s: %s", r.Name, sshError(ctx, err, errOut))
	}
	s.mu.Lock()
	if f, ok := s.facts[id]; ok {
		f.RunnerVersion, f.RunnerProtocol = "", ""
		s.facts[id] = f
	}
	s.mu.Unlock()
	return nil
}

type file struct {
	name string
	data []byte
	mode int64
}

func (s *Service) payloadFiles(goarch string) ([]file, error) {
	if s.payload == "" {
		return nil, errors.New("this build of wtf has no runner binaries")
	}
	var files []file
	for _, n := range []string{"wtf-runner", "wtf-mcp"} {
		data, err := os.ReadFile(filepath.Join(s.payload, "linux-"+goarch, n))
		if err != nil {
			return nil, fmt.Errorf("this build of wtf has no %s for linux-%s", n, goarch)
		}
		files = append(files, file{n, data, 0o755})
	}
	settings := ""
	if s.StatusSettings != nil {
		settings = s.StatusSettings()
	}
	if settings == "" {
		return nil, errors.New("this build of wtf has no status hooks for Claude Code")
	}
	data, err := os.ReadFile(settings)
	if err != nil {
		return nil, err
	}
	return append(files, file{"status-settings.json", data, 0o644}), nil
}

// Tab commands: what needs you runs in a tab.
const (
	TabSignIn        = "signin"
	TabSetup         = "setup"
	TabInstallClaude = "install-claude"
	TabInstallTmux   = "install-tmux"
	TabShell         = "shell"
)

var tmuxInstall = map[string]string{
	"apt-get": "sudo apt-get update && sudo apt-get install tmux",
	"dnf":     "sudo dnf install tmux",
	"yum":     "sudo yum install tmux",
	"pacman":  "sudo pacman -S --needed tmux",
	"zypper":  "sudo zypper install tmux",
	"apk":     "sudo apk add tmux",
}

// TabCommand is the command a tab runs on a runner for what: sign in to
// Claude Code, finish its first-run setup, install Claude Code or tmux, or
// a plain shell there. It uses the runner's last check.
func (s *Service) TabCommand(id, what string) (string, error) {
	r, err := s.runner(id)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	f := s.facts[id]
	s.mu.Unlock()
	claude := f.Claude
	if claude == "" {
		claude = "$HOME/.local/bin/claude"
	}
	// An API key in the environment would take over from your account.
	claude = `env -u ANTHROPIC_API_KEY "` + claude + `"`
	var remote string
	switch what {
	case TabSignIn:
		remote = claude + " auth login --claudeai"
	case TabSetup:
		// Claude Code's first-run screens, in wtf-runner's own folder so
		// the folder it asks you to trust is one wtf made.
		remote = `mkdir -p "$HOME/` + Dir + `" && cd "$HOME/` + Dir + `" && ` + claude
	case TabInstallClaude:
		remote = "curl -fsSL https://claude.ai/install.sh | bash"
	case TabInstallTmux:
		cmd, ok := tmuxInstall[f.PackageManager]
		if !ok {
			return "", fmt.Errorf("no package manager found on %s to install tmux with", r.Name)
		}
		remote = cmd
	case TabShell:
		return TabSSH(r, "exec \"${SHELL:-sh}\" -l"), nil
	default:
		return "", fmt.Errorf("no tab command %q", what)
	}
	return TabSSH(r, remote), nil
}

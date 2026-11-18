package runner

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dayanraj/wtf/internal/bridge"
)

const signedIn = `os=Linux
arch=aarch64
home=/home/dev
tmux=tmux 3.3a
pm=apt-get
claude=/home/dev/.local/bin/claude
claudeVersion=2.1.327 (Claude Code)
auth={  "loggedIn": true,  "authMethod": "claude.ai",  "email": "me@example.com",  "orgName": "Org",  "subscriptionType": "team",  "loginExpiresAt": "2026-12-17T21:44:05.396Z"}
onboarded=1
runner=1.12.0 protocol=1
end=1
`

// noStream stands in for the agents stream's ssh: it fails at once.
func noStream(ctx context.Context, _ []string) *exec.Cmd { return exec.CommandContext(ctx, "false") }

func TestParseProbe(t *testing.T) {
	f := parseProbe([]byte(signedIn))
	if !f.Reachable || f.GoArch != "arm64" || f.ClaudeVersion != "2.1.327" || !f.SignedIn || f.AuthMethod != "claude.ai" ||
		f.Account != "me@example.com" || f.Plan != "team" || !f.Onboarded || f.RunnerVersion != "1.12.0" || f.RunnerProtocol != "1" || f.APIKeySet {
		t.Fatalf("facts = %+v", f)
	}
	if p := f.Problems(); len(p) != 0 {
		t.Fatalf("problems = %q", p)
	}

	// Cut short: not reachable, whatever came before.
	if f := parseProbe([]byte("os=Linux\n")); f.Reachable || f.Problems()[0] != "can't reach it over SSH" {
		t.Fatalf("cut short: %+v", f)
	}

	f = parseProbe([]byte("os=Linux\narch=x86_64\napiKey=1\nclaude=/usr/bin/claude\nauth={\"loggedIn\":false,\"authMethod\":\"none\"}\nend=1\n"))
	want := []string{"tmux isn't installed", "Claude Code isn't signed in", "wtf-runner isn't installed"}
	if !f.APIKeySet || f.GoArch != "amd64" || !slices.Equal(f.Problems(), want) {
		t.Fatalf("facts = %+v, problems %q", f, f.Problems())
	}

	f = parseProbe([]byte("os=Linux\narch=riscv64\ntmux=tmux 3.4\nclaude=/c\nauth={\"loggedIn\":true,\"authMethod\":\"console\"}\nend=1\n"))
	want = []string{"wtf has no runner for riscv64", "Claude Code is signed in with console, not a Claude account", "wtf-runner isn't installed"}
	if !slices.Equal(f.Problems(), want) {
		t.Fatalf("problems = %q", f.Problems())
	}
}

type fakeSSH struct {
	calls [][]string
	stdin [][]byte
	out   string
	err   error
}

func (f *fakeSSH) run(_ context.Context, args []string, in io.Reader) ([]byte, []byte, error) {
	f.calls = append(f.calls, args)
	var b []byte
	if in != nil {
		b, _ = io.ReadAll(in)
	}
	f.stdin = append(f.stdin, b)
	if f.err != nil {
		return nil, []byte("ssh: connect to host box port 22: Connection refused\n"), f.err
	}
	return []byte(f.out), nil, nil
}

func newTest(t *testing.T) (*Service, *fakeSSH) {
	s := NewService(t.TempDir(), "1.12.0")
	f := &fakeSSH{out: signedIn}
	s.ssh = f.run
	s.sshCommand = noStream
	return s, f
}

func TestRunners(t *testing.T) {
	s, f := newTest(t)
	if _, err := s.Add("-oProxyCommand=x", "", ""); err == nil {
		t.Fatal("an option was taken for a host")
	}
	st, err := s.Add("box", "2222", "")
	if err != nil || st.Name != "box" || st.ID != "box:2222" || st.Facts != nil {
		t.Fatalf("Add = %+v, %v", st, err)
	}
	if _, err := s.Add("box", "2222", "again"); err == nil {
		t.Fatal("added twice")
	}
	if st, err = s.Check("box:2222"); err != nil || st.Facts == nil || !st.Facts.SignedIn || st.Outdated || len(st.Problems) != 0 {
		t.Fatalf("Check = %+v, %v", st, err)
	}
	args := f.calls[0]
	if !slices.Contains(args, "BatchMode=yes") || !slices.Equal(args[len(args)-5:], []string{"-p", "2222", "--", "box", "sh -s"}) ||
		string(f.stdin[0]) != probeScript {
		t.Fatalf("ssh %q", args)
	}

	// Saved and read back by a new service; facts are only in memory.
	if err := s.Rename("box:2222", "Build box"); err != nil {
		t.Fatal(err)
	}
	s2 := NewService(filepath.Dir(s.file), "1.13.0")
	s2.ssh, s2.sshCommand = f.run, noStream
	if l := s2.List(); len(l) != 1 || l[0].Name != "Build box" || l[0].Facts != nil {
		t.Fatalf("List = %+v", l)
	}
	if st, _ := s2.Check("box:2222"); !st.Outdated {
		t.Fatal("a runner from another version isn't outdated")
	}

	f.err = errors.New("exit status 255")
	if st, _ = s.Check("box:2222"); st.Facts.Reachable || st.Facts.Error != "ssh: connect to host box port 22: Connection refused" {
		t.Fatalf("unreachable: %+v", st.Facts)
	}
	if _, err := s.Install("box:2222"); err == nil {
		t.Fatal("installed on an unreachable runner")
	}

	if err := s.Remove("box:2222"); err != nil || len(s.List()) != 0 {
		t.Fatalf("Remove: %v %+v", err, s.List())
	}
}

func TestInstall(t *testing.T) {
	s, f := newTest(t)
	s.payload = t.TempDir()
	for _, n := range []string{"wtf-runner", "wtf-mcp"} {
		os.MkdirAll(filepath.Join(s.payload, "linux-arm64"), 0o755)
		os.WriteFile(filepath.Join(s.payload, "linux-arm64", n), []byte("#!/bin/sh\n"), 0o755)
	}
	settings := filepath.Join(t.TempDir(), "status-settings.json")
	os.WriteFile(settings, []byte("{}"), 0o644)
	s.StatusSettings = func() string { return settings }
	s.Add("box", "", "")
	if _, err := s.Install("box:"); err == nil || !strings.Contains(err.Error(), "check") {
		t.Fatalf("installed before a check: %v", err)
	}
	s.Check("box:")
	if _, err := s.Install("box:"); err != nil {
		t.Fatal(err)
	}
	if last := f.calls[1]; last[len(last)-1] != installScript || last[len(last)-2] != "box" {
		t.Fatalf("install ssh %q", last)
	}
	// The archive unpacks to the three files.
	dir := t.TempDir()
	cmd := exec.Command("tar", "-xf", "-", "-C", dir)
	cmd.Stdin = strings.NewReader(string(f.stdin[1]))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tar: %v %s", err, out)
	}
	for _, n := range []string{"wtf-runner", "wtf-mcp", "status-settings.json"} {
		if _, err := os.Stat(filepath.Join(dir, n)); err != nil {
			t.Fatal(err)
		}
	}
	if fi, _ := os.Stat(filepath.Join(dir, "wtf-runner")); fi.Mode()&0o111 == 0 {
		t.Fatal("wtf-runner isn't executable")
	}
}

func TestTabCommand(t *testing.T) {
	s, _ := newTest(t)
	s.Add("box", "", "")
	s.Check("box:")
	for what, want := range map[string]string{
		TabSignIn:        `ssh -t -o ConnectTimeout=10 box 'env -u ANTHROPIC_API_KEY "/home/dev/.local/bin/claude" auth login --claudeai'`,
		TabInstallClaude: `ssh -t -o ConnectTimeout=10 box 'curl -fsSL https://claude.ai/install.sh | bash'`,
		TabInstallTmux:   `ssh -t -o ConnectTimeout=10 box 'sudo apt-get update && sudo apt-get install tmux'`,
		TabSetup:         `ssh -t -o ConnectTimeout=10 box 'mkdir -p "$HOME/.local/share/wtf-runner" && cd "$HOME/.local/share/wtf-runner" && env -u ANTHROPIC_API_KEY "/home/dev/.local/bin/claude"'`,
	} {
		if got, err := s.TabCommand("box:", what); err != nil || got != want {
			t.Errorf("%s: %s, %v\n want %s", what, got, err, want)
		}
	}
}

// TestLive checks a real runner: WTF_RUNNER_TEST_SSH_CONFIG is an ssh
// config with a Host "runner" (a throwaway container, never your own
// machines). It installs a stand-in payload there.
func TestLive(t *testing.T) {
	cfg := os.Getenv("WTF_RUNNER_TEST_SSH_CONFIG")
	if cfg == "" {
		t.Skip("WTF_RUNNER_TEST_SSH_CONFIG not set")
	}
	s := NewService(t.TempDir(), "test")
	s.ssh = func(ctx context.Context, args []string, in io.Reader) ([]byte, []byte, error) {
		return execSSH(ctx, append([]string{"-F", cfg}, args...), in)
	}
	s.Add("runner", "", "")
	st, err := s.Check("runner:")
	if err != nil || !st.Facts.Reachable {
		t.Fatalf("Check: %+v %v", st.Facts, err)
	}
	t.Logf("facts: %+v problems: %q", *st.Facts, st.Problems)
	if p := os.Getenv("WTF_RUNNER_TEST_PAYLOAD"); p != "" {
		s.payload = p
		s.StatusSettings = func() string { return "../../claude-plugin/status-settings.json" }
		if st, err = s.Install("runner:"); err != nil || st.Facts.RunnerVersion == "" {
			t.Fatalf("Install: %+v %v", st.Facts, err)
		}
		t.Logf("installed: %s protocol %s, outdated %v", st.Facts.RunnerVersion, st.Facts.RunnerProtocol, st.Outdated)
	}
	s.sshCommand = func(ctx context.Context, args []string) *exec.Cmd {
		return exec.CommandContext(ctx, "ssh", append([]string{"-F", cfg}, args...)...)
	}
	s.Bridge = bridge.NewService(nil)
	// The agents stream: up after a check, then an agent in default mode
	// asks before running its command.
	waitFor := func(what string, ok func(Agents) bool) Agents {
		for range 120 {
			if a := s.Agents("runner:"); ok(a) {
				return a
			}
			time.Sleep(500 * time.Millisecond)
		}
		t.Fatalf("no %s: %+v", what, s.Agents("runner:"))
		return Agents{}
	}
	s.Check("runner:")
	waitFor("connection", func(a Agents) bool { return a.Connected })
	dir := os.Getenv("WTF_RUNNER_TEST_DIR")
	if dir == "" {
		return
	}
	ag, err := s.Start("runner:", dir, "Run this exact Bash command: date > live-test.txt — then reply done.", "default", "live test")
	if err != nil {
		t.Fatal(err)
	}
	got := waitFor("approval", func(a Agents) bool {
		for _, x := range a.Agents {
			if x.ID == ag.ID && x.Status != nil && x.Status.State == bridge.StateWaiting {
				return true
			}
		}
		return false
	})
	t.Logf("waiting: %+v", got.Agents[len(got.Agents)-1].Status.Message)
	// Still alive after the daemon has looked at its tmux sessions a few
	// times (every 3 s).
	time.Sleep(7 * time.Second)
	for _, x := range s.Agents("runner:").Agents {
		if x.ID == ag.ID && !x.Alive {
			t.Fatalf("a running agent counts as ended: %+v", x)
		}
	}
	// Phase 5: mirrored in wtf's bridge, and allowed from there.
	session := Session("runner:", ag.ID)
	var apID string
	for _, st := range s.Bridge.Agents() {
		if st.Session == session && len(st.Approvals) > 0 {
			apID = st.Approvals[0].ID
		}
	}
	if apID == "" {
		t.Fatalf("not mirrored with its approval: %+v", s.Bridge.Agents())
	}
	if err := s.Bridge.Answer(apID, bridge.Allow); err != nil {
		t.Fatal(err)
	}
	idle := func(a Agents) bool {
		for _, x := range a.Agents {
			if x.ID == ag.ID && x.Status != nil && x.Status.State == bridge.StateIdle {
				return true
			}
		}
		return false
	}
	waitFor("done after allowing", idle)
	// Phase 4: peek, and a reply as the next prompt.
	screen, err := s.Peek("runner:", ag.ID)
	if err != nil || !strings.Contains(screen, "live-test.txt") {
		t.Fatalf("peek: %v\n%s", err, screen)
	}
	if err := s.Reply("runner:", ag.ID, "Reply with just the word pong."); err != nil {
		t.Fatal(err)
	}
	waitFor("the reply's turn", func(a Agents) bool {
		for _, x := range a.Agents {
			if x.ID == ag.ID && x.Status != nil && strings.Contains(x.Status.Prompt, "pong") && x.Status.State == bridge.StateIdle {
				return true
			}
		}
		return false
	})
	if cmd, _ := s.OpenCommand("runner:", ag.ID); !strings.Contains(cmd, `tmux attach -t '\''=wtf-`+ag.ID+`'\''`) {
		t.Fatalf("open: %s", cmd)
	}
	if err := s.EndAgent("runner:", ag.ID); err != nil {
		t.Fatal(err)
	}
	waitFor("end", func(a Agents) bool {
		for _, x := range a.Agents {
			if x.ID == ag.ID {
				return !x.Alive
			}
		}
		return false
	})
	if err := s.ForgetAgent("runner:", ag.ID); err != nil {
		t.Fatal(err)
	}
}

// Package bridge lets the wtf plugin's MCP helper (cmd/wtf-mcp), started by
// Claude Code inside a wtf shell, reach the running app (#32). The app
// listens on a Unix socket in a private folder; every shell it starts gets
// WTF_PANE (its session), WTF_BRIDGE (the socket), WTF_BRIDGE_TOKEN (a
// token good for that session only) and WTF_MCP (the helper). The helper
// lists the tools the app defines here and relays calls to them.
//
// The tools, the read-only allowlist, the parsers and the redaction all
// live here, in Go. The UIs answer four plain requests about a shell —
// what it is (pane), its recent command blocks (blocks), type text at its
// prompt (type), and run a line and wait for its block (run) — through the
// EventRequest event and Reply. Nothing here asks the user anything:
// Claude Code's own permission prompt, forced by the plugin's PreToolUse
// hook, is the approval.
package bridge

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dayanraj/wtf/internal/bridge/wire"
	"github.com/dayanraj/wtf/internal/events"
	"github.com/dayanraj/wtf/internal/infra"
)

const (
	// EventRequest asks the UI about a shell; it answers with Reply.
	EventRequest = "bridge:request"
	// EventStatus tells the UI what Claude Code in a shell is doing, from
	// the plugin's hooks (payload Status).
	EventStatus = "bridge:status"

	maxRequest = 1 << 20
	ioTimeout  = 10 * time.Second
	// How long a UI request may take: a run waits for the command (the UI
	// interrupts it at its own limit), the rest answer at once.
	runTimeout   = 3 * time.Minute
	queryTimeout = 10 * time.Second
)

// Request kinds.
const (
	KindPane   = "pane"
	KindBlocks = "blocks"
	KindType   = "type"
	KindRun    = "run"
)

// UIRequest is EventRequest's payload.
type UIRequest struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	// The backend session of the shell it's about ("s3").
	Session string `json:"session"`
	// blocks: how many, newest last.
	Count int `json:"count"`
	// type: the text (no Enter); run: the line (the UI adds Enter).
	Text string `json:"text"`
}

// Claude Code states, from its hooks.
const (
	StateIdle    = "idle"    // SessionStart, Stop: waiting for a prompt
	StateBusy    = "busy"    // UserPromptSubmit: working
	StateWaiting = "waiting" // Notification: needs the user (a permission prompt)
	StateEnded   = "ended"   // SessionEnd
)

// Status is EventStatus's payload.
type Status struct {
	// The session of the shell Claude Code runs in.
	Session string `json:"session"`
	State   string `json:"state"`
	// Notification's text ("Claude needs your permission to use Bash").
	Message string `json:"message"`
}

var hookStates = map[string]string{
	"SessionStart": StateIdle, "Stop": StateIdle, "UserPromptSubmit": StateBusy, "PostToolUse": StateBusy,
	"Notification": StateWaiting, "SessionEnd": StateEnded,
}

// EnvAuto, set on a Claude pane's claude when claude-auto-mode is on, makes
// the plugin's PreToolUse hook leave commands to Claude Code's own
// permission mode.
const EnvAuto = "WTF_CLAUDE_AUTO"

// Pane is what the UI knows about a shell.
type Pane struct {
	// Tab title, as shown.
	Title string `json:"title"`
	Cwd   string `json:"cwd"`
	// The shell's variables at its last prompt; empty without shell
	// integration.
	Env map[string]string `json:"env"`
	// A program runs in it (it isn't at its prompt).
	Running bool   `json:"running"`
	Process string `json:"process"`
	// Shell integration seen, so command blocks are known.
	Integrated bool `json:"integrated"`
}

// Block is a command block.
type Block struct {
	ID      int    `json:"id"`
	Command string `json:"command"`
	// -1 while it runs or when unknown.
	ExitCode int `json:"exitCode"`
	// Empty once it left the scrollback.
	Output     string  `json:"output"`
	DurationMs float64 `json:"durationMs"`
	// run: it didn't finish in time and was interrupted.
	TimedOut bool `json:"timedOut"`
}

// Reply is the UI's answer to a UIRequest. Error set means it failed, with
// a message for the model ("The shell is busy…").
type Reply struct {
	Pane   *Pane   `json:"pane"`
	Blocks []Block `json:"blocks"`
	Block  *Block  `json:"block"`
	Error  string  `json:"error"`
}

type Service struct {
	infra *infra.Service
	// The helper's path; "" when it isn't installed next to the app.
	helper string
	// The bundled Claude Code plugin's folder (claude-plugin/); "" without.
	plugin string

	key  []byte
	dir  string
	sock string
	ln   net.Listener

	seq     atomic.Uint64
	mu      sync.Mutex
	pending map[string]chan Reply
	// Claude pane session → the shell it works on.
	targets map[string]string
	// Claude pane session → its last state.
	states map[string]string

	// claude-auto-mode (nil: off).
	AutoMode func() bool
	// The claude to run (nil or "": claude from the shell's PATH); the
	// self-checks give a stand-in.
	Program func() string
}

func NewService(in *infra.Service) *Service {
	s := &Service{infra: in, pending: map[string]chan Reply{}, targets: map[string]string{}, states: map[string]string{}}
	// Next to the app (macOS: Contents/MacOS; a Linux dev build), or where
	// the Linux packages put it (/usr/bin/wtf → /usr/lib/wtf/wtf-mcp).
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		for _, h := range []string{filepath.Join(dir, "wtf-mcp"), filepath.Join(dir, "..", "lib", "wtf", "wtf-mcp")} {
			if fi, err := os.Stat(h); err == nil && fi.Mode().IsRegular() && fi.Mode()&0o111 != 0 {
				s.helper = filepath.Clean(h)
				break
			}
		}
		s.plugin = findPlugin(dir)
	}
	return s
}

// findPlugin looks for the plugin next to the app (a Linux dev build), in
// the macOS bundle's Resources, or where the Linux packages put it
// (/usr/bin/wtf → /usr/share/wtf/claude-plugin).
func findPlugin(exeDir string) string {
	for _, d := range []string{
		filepath.Join(exeDir, "claude-plugin"),
		filepath.Join(exeDir, "..", "Resources", "claude-plugin"),
		filepath.Join(exeDir, "..", "share", "wtf", "claude-plugin"),
	} {
		if fi, err := os.Stat(filepath.Join(d, ".claude-plugin", "plugin.json")); err == nil && fi.Mode().IsRegular() {
			return filepath.Clean(d)
		}
	}
	return ""
}

func (s *Service) startup() error {
	s.key = make([]byte, 32)
	if _, err := rand.Read(s.key); err != nil {
		return err
	}
	// MkdirTemp makes it 0700: only this user reaches the socket.
	dir, err := os.MkdirTemp("", "wtf-bridge-")
	if err != nil {
		return err
	}
	sock := filepath.Join(dir, "sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		os.RemoveAll(dir)
		return err
	}
	s.dir, s.sock, s.ln = dir, sock, ln
	go s.serve(ln)
	return nil
}

func (s *Service) shutdown() {
	if s.ln != nil {
		s.ln.Close()
	}
	if s.dir != "" {
		os.RemoveAll(s.dir)
	}
}

// shellEnv is what a new shell's environment gets for session id: nothing when
// the bridge isn't running.
func (s *Service) shellEnv(session string) []string {
	if s.sock == "" {
		return nil
	}
	env := []string{wire.EnvPane + "=" + session, wire.EnvBridge + "=" + s.sock, wire.EnvToken + "=" + s.token(session)}
	if s.helper != "" {
		env = append(env, wire.EnvMCP+"="+s.helper)
	}
	return env
}

func (s *Service) token(session string) string {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte(session))
	return hex.EncodeToString(m.Sum(nil))
}

// SetTarget points a Claude pane's tools at another shell ("" forgets it).
func (s *Service) SetTarget(session, target string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if target == "" {
		delete(s.targets, session)
		delete(s.states, session)
	} else {
		s.targets[session] = target
	}
}

func (s *Service) target(session string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t := s.targets[session]; t != "" {
		return t
	}
	return session
}

// setState records a Claude pane's state from a hook, and says whether the
// UI should hear of it: only changes. A Notification counts only while
// Claude Code works (a permission prompt); one while it's idle is its
// reminder that it's waiting for a prompt, which changes nothing.
func (s *Service) setState(session, state string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.states[session]
	if state == StateWaiting && old != StateBusy {
		return false
	}
	if state == StateEnded {
		delete(s.states, session)
		return old != ""
	}
	s.states[session] = state
	return old != state
}

// ClaudeCommand is the command line a Claude pane's shell runs: the user's
// own claude with the bundled plugin.
func (s *Service) ClaudeCommand() (string, error) {
	switch {
	case s.sock == "":
		return "", errors.New("wtf's bridge for Claude Code isn't running")
	case s.helper == "" || s.plugin == "":
		return "", errors.New("this wtf was built without its Claude Code plugin (wtf-mcp and claude-plugin aren't next to the app)")
	}
	prog := "claude"
	if s.Program != nil && s.Program() != "" {
		prog = shellQuote(s.Program())
	}
	cmd := prog + " --plugin-dir " + shellQuote(s.plugin)
	if s.AutoMode != nil && s.AutoMode() {
		cmd = EnvAuto + "=1 " + cmd
	}
	return cmd, nil
}

// shellQuote single-quotes for bash, zsh and fish.
func shellQuote(v string) string { return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'" }

// Reply answers a UIRequest. Late or unknown ones are dropped.
func (s *Service) Reply(id string, r Reply) {
	s.mu.Lock()
	ch := s.pending[id]
	delete(s.pending, id)
	s.mu.Unlock()
	if ch != nil {
		ch <- r
	}
}

// ask sends a request to the UI and waits for its reply.
func (s *Service) ask(ctx context.Context, req UIRequest) (Reply, error) {
	req.ID = "b" + strconv.FormatUint(s.seq.Add(1), 10)
	ch := make(chan Reply, 1)
	s.mu.Lock()
	s.pending[req.ID] = ch
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.pending, req.ID)
		s.mu.Unlock()
	}()
	limit := queryTimeout
	if req.Kind == KindRun {
		limit = runTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	events.Emit(EventRequest, req)
	select {
	case r := <-ch:
		if r.Error != "" {
			return r, errors.New(r.Error)
		}
		return r, nil
	case <-ctx.Done():
		return Reply{}, errors.New("wtf didn't answer in time (is the window still open?)")
	}
}

// ── Socket (see package wire) ────────────────────────────────────────────

func (s *Service) serve(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				log.Printf("bridge: %v", err)
			}
			return
		}
		go s.handle(c)
	}
}

func (s *Service) handle(c net.Conn) {
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(ioTimeout))
	line, err := bufio.NewReaderSize(io.LimitReader(c, maxRequest), 64*1024).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return
	}
	var req wire.Request
	var resp wire.Response
	if err := json.Unmarshal(line, &req); err != nil {
		resp.Error = "bad request"
	} else {
		c.SetReadDeadline(time.Time{})
		resp = s.do(context.Background(), req)
	}
	out, _ := json.Marshal(resp)
	c.SetWriteDeadline(time.Now().Add(ioTimeout))
	c.Write(append(out, '\n'))
}

// do answers one request: what the socket does, callable from tests.
func (s *Service) do(ctx context.Context, req wire.Request) wire.Response {
	if req.V != wire.Version {
		return wire.Response{Error: fmt.Sprintf("wtf-mcp speaks protocol %d, this wtf speaks %d: use the helper that came with this wtf", req.V, wire.Version)}
	}
	if req.Pane == "" || !hmac.Equal([]byte(req.Token), []byte(s.token(req.Pane))) {
		return wire.Response{Error: "not a wtf shell's token"}
	}
	switch req.Op {
	case "list":
		return wire.Response{Tools: Tools}
	case "call":
		text, err := s.call(ctx, s.target(req.Pane), req.Tool, req.Args)
		if err != nil {
			return wire.Response{Text: err.Error(), IsError: true}
		}
		return wire.Response{Text: text}
	case "hook":
		state := hookStates[req.Event]
		if state == "" {
			return wire.Response{Error: "unknown hook event " + strconv.Quote(req.Event)}
		}
		if s.setState(req.Pane, state) {
			events.Emit(EventStatus, Status{Session: req.Pane, State: state, Message: req.Message})
		}
		return wire.Response{}
	}
	return wire.Response{Error: "unknown op " + strconv.Quote(req.Op)}
}

// ServiceStartup starts listening (native UIs; the Wails build doesn't
// offer the bridge).
func (s *Service) ServiceStartup(context.Context) error { return s.startup() }

func (s *Service) ServiceShutdown() error {
	s.shutdown()
	return nil
}

// ShellEnv gives the terminal service, for each new shell, its WTF_PANE,
// WTF_BRIDGE, WTF_BRIDGE_TOKEN and WTF_MCP (a function, so it isn't one of
// the service's methods the UIs can call).
func ShellEnv(s *Service) func(session string) []string { return s.shellEnv }

// PluginDir is the bundled Claude Code plugin's folder, for
// `claude --plugin-dir`; "" when it isn't installed with the app.
func PluginDir(s *Service) string { return s.plugin }

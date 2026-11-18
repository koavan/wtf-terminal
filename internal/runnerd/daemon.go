package runnerd

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/dayanraj/wtf/internal/bridge"
	"github.com/dayanraj/wtf/internal/events"
	"github.com/dayanraj/wtf/internal/infra"
)

// Paths under the daemon's folder (~/.local/share/wtf-runner).
const (
	RunDir      = "run"
	ControlSock = "control.sock"
	HooksSock   = "hooks.sock"
	keyFile     = "key"
	lockFile    = "daemon.lock"
	agentsFile  = "agents.json"
	LogFile     = "daemon.log"
	// How often it looks at its tmux sessions (here, on the runner).
	watchEvery = 3 * time.Second
	// A client that can't keep up is dropped (it reconnects).
	clientQueue = 256
)

// Daemon is one runner's daemon.
type Daemon struct {
	Base    string // ~/.local/share/wtf-runner
	Version string
	// tmux, and how to run Claude Code (tests use stand-ins).
	Tmux   string
	Claude func() string

	br      *bridge.Service
	ctl     net.Listener
	mu      sync.Mutex
	agents  []Agent
	clients map[*client]struct{}
	quit    chan struct{}
}

type client struct {
	out  chan []byte
	conn net.Conn
}

func (d *Daemon) run(p string) string { return filepath.Join(d.Base, RunDir, p) }

// Serve runs the daemon until it's told to quit. Only one runs: a second
// one exits with an error.
func (d *Daemon) Serve() error {
	if d.Tmux == "" {
		d.Tmux = "tmux"
	}
	if d.Claude == nil {
		d.Claude = findClaude
	}
	if err := os.MkdirAll(d.run(""), 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(d.run(lockFile), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errors.New("wtf-runner is already running")
	}
	key, err := loadKey(d.run(keyFile))
	if err != nil {
		return err
	}
	d.clients, d.quit = map[*client]struct{}{}, make(chan struct{})
	d.load()

	d.br = bridge.NewService(infra.NewService())
	d.br.PaneOf = d.paneOf
	events.SetEmitter(func(name string, data any) {
		if st, ok := data.(bridge.Status); ok && name == bridge.EventStatus {
			d.broadcast(Message{Type: "status", Status: &st})
		}
	})
	if err := d.br.ListenAt(d.run(HooksSock), key); err != nil {
		return err
	}
	defer d.br.ServiceShutdown()
	os.Remove(d.run(ControlSock))
	d.ctl, err = net.Listen("unix", d.run(ControlSock))
	if err != nil {
		return err
	}
	os.Chmod(d.run(ControlSock), 0o600)
	defer d.ctl.Close()
	go d.accept()
	log.Printf("wtf-runner %s serving (%d agents)", d.Version, len(d.agents))

	t := time.NewTicker(watchEvery)
	defer t.Stop()
	d.watch()
	for {
		select {
		case <-d.quit:
			log.Print("quitting")
			return nil
		case <-t.C:
			d.watch()
		}
	}
}

func loadKey(path string) ([]byte, error) {
	if b, err := os.ReadFile(path); err == nil {
		if k, err := hex.DecodeString(strings.TrimSpace(string(b))); err == nil && len(k) >= 32 {
			return k, nil
		}
	}
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		return nil, err
	}
	return k, os.WriteFile(path, []byte(hex.EncodeToString(k)+"\n"), 0o600)
}

func (d *Daemon) load() {
	if b, err := os.ReadFile(d.run(agentsFile)); err == nil {
		_ = json.Unmarshal(b, &d.agents)
	}
}

// save writes the agents; d.mu is held.
func (d *Daemon) save() {
	b, _ := json.MarshalIndent(d.agents, "", "  ")
	tmp := d.run(agentsFile + ".tmp")
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err == nil {
		os.Rename(tmp, d.run(agentsFile))
	}
}

// paneOf tells the bridge an agent's folder and environment, for an
// approval's context facts: those of this machine.
func (d *Daemon) paneOf(session string) (bridge.Pane, bool) {
	d.mu.Lock()
	i := slices.IndexFunc(d.agents, func(a Agent) bool { return a.ID == session })
	var dir string
	if i >= 0 {
		dir = d.agents[i].Dir
	}
	d.mu.Unlock()
	if i < 0 {
		return bridge.Pane{}, false
	}
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	return bridge.Pane{Cwd: dir, Env: env, Running: true, Process: "claude"}, true
}

// watch reads which of its tmux sessions still run; one that stopped
// ends its agent.
func (d *Daemon) watch() {
	out, err := exec.Command(d.Tmux, "list-panes", "-a", "-F", "#{session_name} #{pane_dead} #{pane_dead_status}").Output()
	if err != nil {
		out = nil // no tmux server: none running
	}
	alive, exit := parsePanes(string(out))
	var ended []string
	d.mu.Lock()
	changed := false
	for i := range d.agents {
		a := &d.agents[i]
		now := alive[a.ID]
		if now == a.Alive {
			continue
		}
		changed = true
		a.Alive, a.Exit = now, ""
		if !now {
			a.Exit = exit[a.ID]
			if a.Exit == "" {
				a.Exit = "gone"
			}
			ended = append(ended, a.ID)
		}
	}
	if changed {
		d.save()
	}
	d.mu.Unlock()
	for _, id := range ended {
		d.br.ShellClosed(id)
	}
	if changed {
		d.sendAgents()
	}
}

// parsePanes reads `tmux list-panes -a -F '#{session_name} #{pane_dead}
// #{pane_dead_status}'`: which wtf-<id> sessions run, and how the others
// exited. Spaces, not tabs: tmux prints control characters as "_". A
// running pane has no exit status, so it has two fields.
func parsePanes(out string) (alive map[string]bool, exit map[string]string) {
	alive, exit = map[string]bool{}, map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		id, ok := strings.CutPrefix(f[0], "wtf-")
		if !ok {
			continue
		}
		alive[id] = f[1] != "1"
		if f[1] == "1" && len(f) > 2 {
			exit[id] = "exited " + f[2]
		}
	}
	return alive, exit
}

func (d *Daemon) sendAgents() {
	d.mu.Lock()
	m := Message{Type: "agents", Agents: slices.Clone(d.agents)}
	d.mu.Unlock()
	d.broadcast(m)
}

func (d *Daemon) broadcast(m Message) {
	b, err := json.Marshal(m)
	if err != nil {
		return
	}
	b = append(b, '\n')
	d.mu.Lock()
	defer d.mu.Unlock()
	for c := range d.clients {
		select {
		case c.out <- b:
		default:
			c.conn.Close() // too slow: it reconnects
		}
	}
}

func (d *Daemon) accept() {
	for {
		conn, err := d.ctl.Accept()
		if err != nil {
			return
		}
		go d.serveClient(conn)
	}
}

func (d *Daemon) serveClient(conn net.Conn) {
	c := &client{out: make(chan []byte, clientQueue), conn: conn}
	d.mu.Lock()
	hello := Message{Type: "hello", Version: d.Version, Protocol: Protocol, Agents: slices.Clone(d.agents)}
	d.clients[c] = struct{}{}
	d.mu.Unlock()
	hello.Statuses = d.br.Agents()
	b, _ := json.Marshal(hello)
	c.out <- append(b, '\n')
	done := make(chan struct{})
	go func() {
		defer close(done)
		for b := range c.out {
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if _, err := conn.Write(b); err != nil {
				conn.Close()
				return
			}
		}
	}()
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var cmd Command
		r := Message{Type: "reply"}
		if err := json.Unmarshal(sc.Bytes(), &cmd); err != nil {
			r.Error = "bad command"
		} else {
			r.ID = cmd.ID
			res, err := d.do(cmd)
			if err != nil {
				r.Error = err.Error()
			} else if res != nil {
				r.Result, _ = json.Marshal(res)
			}
		}
		b, _ := json.Marshal(r)
		d.mu.Lock()
		if _, ok := d.clients[c]; ok {
			select {
			case c.out <- append(b, '\n'):
			default:
			}
		}
		d.mu.Unlock()
		if cmd.Op == OpQuit && r.Error == "" {
			time.Sleep(100 * time.Millisecond)
			close(d.quit)
		}
	}
	d.mu.Lock()
	delete(d.clients, c)
	close(c.out)
	d.mu.Unlock()
	<-done
	conn.Close()
}

func (d *Daemon) find(id string) (int, error) {
	i := slices.IndexFunc(d.agents, func(a Agent) bool { return a.ID == id })
	if i < 0 {
		return -1, fmt.Errorf("no agent %q", id)
	}
	return i, nil
}

// do runs one command.
func (d *Daemon) do(cmd Command) (any, error) {
	switch cmd.Op {
	case OpStart:
		return d.start(cmd)
	case OpSeen:
		d.br.Seen(cmd.Agent)
		return nil, nil
	case OpRename:
		d.mu.Lock()
		i, err := d.find(cmd.Agent)
		if err == nil {
			d.agents[i].Name = strings.TrimSpace(cmd.Name)
			d.save()
		}
		d.mu.Unlock()
		if err != nil {
			return nil, err
		}
		d.br.Rename(cmd.Agent, strings.TrimSpace(cmd.Name))
		d.sendAgents()
		return nil, nil
	case OpEnd:
		d.mu.Lock()
		_, err := d.find(cmd.Agent)
		d.mu.Unlock()
		if err != nil {
			return nil, err
		}
		if out, err := exec.Command(d.Tmux, "kill-session", "-t", "=wtf-"+cmd.Agent).CombinedOutput(); err != nil {
			return nil, fmt.Errorf("tmux: %s", strings.TrimSpace(string(out)))
		}
		d.watch()
		return nil, nil
	case OpForget:
		d.mu.Lock()
		i, err := d.find(cmd.Agent)
		if err == nil && d.agents[i].Alive {
			err = errors.New("it's still running: end it first")
		}
		if err == nil {
			d.agents = slices.Delete(d.agents, i, i+1)
			d.save()
		}
		d.mu.Unlock()
		if err != nil {
			return nil, err
		}
		exec.Command(d.Tmux, "kill-session", "-t", "=wtf-"+cmd.Agent).Run() // its dead pane
		d.br.ShellClosed(cmd.Agent)
		d.sendAgents()
		return nil, nil
	case OpQuit:
		return nil, nil
	}
	return nil, fmt.Errorf("unknown command %q", cmd.Op)
}

// findClaude is Claude Code's native install, else claude from PATH (the
// login shell's, as the agent's shell starts).
func findClaude() string {
	home, _ := os.UserHomeDir()
	if p := filepath.Join(home, ".local", "bin", "claude"); isExec(p) {
		return p
	}
	return "claude"
}

func isExec(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular() && fi.Mode()&0o111 != 0
}

// agentScript runs in the agent's tmux session, in the user's login shell
// (their PATH and tools), as `$SHELL -lc agentScript settings claude
// [args…]`: Claude Code with wtf's status hooks and without
// ANTHROPIC_API_KEY, so it always uses the account you signed in to.
const agentScript = `s="$0"; c="$1"; shift; exec env -u ANTHROPIC_API_KEY "$c" --settings "$s" "$@"`

func (d *Daemon) start(cmd Command) (Agent, error) {
	dir := strings.TrimSpace(cmd.Dir)
	if home, _ := os.UserHomeDir(); dir == "~" || strings.HasPrefix(dir, "~/") {
		dir = filepath.Join(home, strings.TrimPrefix(dir, "~"))
	}
	if !filepath.IsAbs(dir) {
		return Agent{}, fmt.Errorf("%q isn't a full path", cmd.Dir)
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return Agent{}, fmt.Errorf("no folder %s here", dir)
	}
	if cmd.Mode != "" && !slices.Contains(Modes, cmd.Mode) {
		return Agent{}, fmt.Errorf("unknown permission mode %q", cmd.Mode)
	}
	idb := make([]byte, 4)
	rand.Read(idb)
	a := Agent{ID: "r" + hex.EncodeToString(idb), Name: strings.TrimSpace(cmd.Name), Dir: filepath.Clean(dir),
		Prompt: cmd.Prompt, Mode: cmd.Mode, Created: time.Now().UnixMilli(), Alive: true}

	shell := os.Getenv("SHELL")
	if b := filepath.Base(shell); b != "bash" && b != "zsh" && b != "sh" && b != "dash" && b != "ksh" {
		shell = "/bin/sh" // fish and others take other syntax
	}
	args := []string{"new-session", "-d", "-s", "wtf-" + a.ID, "-x", "200", "-y", "50", "-c", a.Dir,
		"-e", "WTF_PANE=" + a.ID, "-e", "WTF_BRIDGE=" + d.run(HooksSock), "-e", "WTF_BRIDGE_TOKEN=" + d.br.Token(a.ID),
		"-e", "WTF_MCP=" + filepath.Join(d.Base, "wtf-mcp"),
		"--", shell, "-lc", agentScript, filepath.Join(d.Base, "status-settings.json"), d.Claude()}
	if a.Mode != "" {
		args = append(args, "--permission-mode", a.Mode)
	}
	if strings.TrimSpace(a.Prompt) != "" {
		args = append(args, "--", a.Prompt)
	}
	// Its pane stays after Claude Code quits, so how it ended can be seen.
	// (A window option: its target is the session's window.)
	args = append(args, ";", "set-option", "-w", "-t", "=wtf-"+a.ID+":", "remain-on-exit", "on")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, d.Tmux, args...).CombinedOutput(); err != nil {
		return Agent{}, fmt.Errorf("tmux: %s", strings.TrimSpace(string(out)+" "+err.Error()))
	}
	d.mu.Lock()
	d.agents = append(d.agents, a)
	d.save()
	d.mu.Unlock()
	if a.Name != "" {
		d.br.Rename(a.ID, a.Name)
	}
	d.sendAgents()
	return a, nil
}

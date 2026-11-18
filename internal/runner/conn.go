package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/dayanraj/wtf/internal/bridge"
	"github.com/dayanraj/wtf/internal/events"
	"github.com/dayanraj/wtf/internal/runnerd"
)

// EventAgents carries a runner's agents (payload Agents) each time they
// or the connection change.
const EventAgents = "runner:agents"

// RemoteAgent is an agent on a runner with what it last reported (nil
// until its first hook: Claude Code may be on its first-run screens).
type RemoteAgent struct {
	runnerd.Agent
	Status *bridge.Status `json:"status,omitempty"`
}

// Agents is a runner's agents and its connection.
type Agents struct {
	Runner string `json:"runner"`
	// Connected: the stream is up. Error: why it isn't (it retries).
	Connected bool          `json:"connected"`
	Error     string        `json:"error,omitempty"`
	Agents    []RemoteAgent `json:"agents"`
}

const (
	commandTimeout = 20 * time.Second
	maxBackoff     = 30 * time.Second
)

// attachCommand runs on the runner: wtf-runner's stream, starting its
// daemon if it isn't running.
const attachCommand = `exec "$HOME/` + Dir + `/wtf-runner" attach`

// conn is the stream to one runner's daemon, reconnecting until stopped.
type conn struct {
	s *Service
	r Runner

	mu        sync.Mutex
	stdin     io.WriteCloser
	pending   map[int]chan runnerd.Message
	next      int
	connected bool
	err       string
	agents    []runnerd.Agent
	statuses  map[string]bridge.Status
	cancel    context.CancelFunc
}

// connect starts the stream to a ready runner, if it isn't running.
func (s *Service) connect(r Runner) {
	s.mu.Lock()
	if s.conns == nil {
		s.conns = map[string]*conn{}
	}
	if _, ok := s.conns[r.ID]; ok {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &conn{s: s, r: r, pending: map[int]chan runnerd.Message{}, statuses: map[string]bridge.Status{}, cancel: cancel}
	s.conns[r.ID] = c
	s.mu.Unlock()
	go c.loop(ctx)
}

// disconnect stops a runner's stream.
func (s *Service) disconnect(id string) {
	s.mu.Lock()
	c := s.conns[id]
	delete(s.conns, id)
	s.mu.Unlock()
	if c != nil {
		c.cancel()
	}
}

func (s *Service) conn(id string) (*conn, error) {
	s.mu.Lock()
	c := s.conns[id]
	s.mu.Unlock()
	if c == nil {
		return nil, errors.New("not connected to that runner")
	}
	return c, nil
}

func (c *conn) loop(ctx context.Context) {
	wait := 2 * time.Second
	for ctx.Err() == nil {
		start := time.Now()
		err := c.once(ctx)
		if ctx.Err() != nil {
			return
		}
		c.mu.Lock()
		c.connected, c.err = false, err.Error()
		for id, ch := range c.pending {
			close(ch)
			delete(c.pending, id)
		}
		c.mu.Unlock()
		c.emit()
		if time.Since(start) > time.Minute {
			wait = 2 * time.Second // it was up a while: try again soon
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = min(wait*2, maxBackoff)
	}
}

// once runs one ssh stream until it ends.
func (c *conn) once(ctx context.Context) error {
	args := sshArgs(c.r, attachCommand)
	// Notice a dead connection within ~45 s.
	args = append([]string{"-o", "ServerAliveCountMax=3"}, args...)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	if c.s.sshCommand != nil {
		cmd = c.s.sshCommand(ctx, args)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr strings.Builder
	cmd.Stderr = &limitWriter{w: &stderr, n: 4096}
	if err := cmd.Start(); err != nil {
		return err
	}
	defer cmd.Wait()
	defer stdin.Close()
	c.mu.Lock()
	c.stdin = stdin
	c.mu.Unlock()

	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64<<10), 8<<20)
	hello := false
	for sc.Scan() {
		var m runnerd.Message
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			continue
		}
		switch m.Type {
		case "hello":
			if m.Protocol != runnerd.Protocol {
				cmd.Process.Kill()
				return fmt.Errorf("its wtf-runner speaks protocol %s, this wtf %s: update it", m.Protocol, runnerd.Protocol)
			}
			hello = true
			c.mu.Lock()
			c.connected, c.err = true, ""
			c.agents = m.Agents
			c.statuses = map[string]bridge.Status{}
			for _, st := range m.Statuses {
				c.statuses[st.Session] = st
			}
			c.mu.Unlock()
			c.emit()
		case "agents":
			c.mu.Lock()
			c.agents = m.Agents
			c.mu.Unlock()
			c.emit()
		case "status":
			if m.Status == nil {
				continue
			}
			c.mu.Lock()
			if m.Status.State == bridge.StateEnded {
				delete(c.statuses, m.Status.Session)
			} else {
				c.statuses[m.Status.Session] = *m.Status
			}
			c.mu.Unlock()
			c.emit()
		case "reply":
			c.mu.Lock()
			ch := c.pending[m.ID]
			delete(c.pending, m.ID)
			c.mu.Unlock()
			if ch != nil {
				ch <- m
			}
		}
	}
	err = errors.New("the connection closed")
	if line := lastLine(stderr.String()); line != "" {
		err = errors.New(line)
	} else if !hello {
		err = errors.New("wtf-runner didn't answer")
	}
	return err
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

type limitWriter struct {
	w io.Writer
	n int
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if l.n > 0 {
		q := p[:min(len(p), l.n)]
		l.n -= len(q)
		l.w.Write(q)
	}
	return len(p), nil
}

// snapshot is the runner's agents now, in the order they started.
func (c *conn) snapshot() Agents {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := Agents{Runner: c.r.ID, Connected: c.connected, Error: c.err, Agents: []RemoteAgent{}}
	for _, a := range c.agents {
		ra := RemoteAgent{Agent: a}
		if st, ok := c.statuses[a.ID]; ok && a.Alive {
			ra.Status = &st
		}
		out.Agents = append(out.Agents, ra)
	}
	return out
}

func (c *conn) emit() { events.Emit(EventAgents, c.snapshot()) }

// do sends a command and waits for its reply.
func (c *conn) do(cmd runnerd.Command) (json.RawMessage, error) {
	c.mu.Lock()
	if !c.connected || c.stdin == nil {
		c.mu.Unlock()
		return nil, errors.New("not connected to that runner")
	}
	c.next++
	cmd.ID = c.next
	ch := make(chan runnerd.Message, 1)
	c.pending[cmd.ID] = ch
	b, _ := json.Marshal(cmd)
	_, err := c.stdin.Write(append(b, '\n'))
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	select {
	case m, ok := <-ch:
		if !ok {
			return nil, errors.New("the connection closed")
		}
		if m.Error != "" {
			return nil, errors.New(m.Error)
		}
		return m.Result, nil
	case <-time.After(commandTimeout):
		c.mu.Lock()
		delete(c.pending, cmd.ID)
		c.mu.Unlock()
		return nil, errors.New("the runner didn't answer in time")
	}
}

// Agents is a runner's agents and its connection right now (the
// EventAgents payload).
func (s *Service) Agents(id string) Agents {
	c, err := s.conn(id)
	if err != nil {
		return Agents{Runner: id, Agents: []RemoteAgent{}}
	}
	return c.snapshot()
}

// Start starts Claude Code in dir on a runner, with an optional first
// prompt, permission mode ("" for its default) and name.
func (s *Service) Start(id, dir, prompt, mode, name string) (runnerd.Agent, error) {
	c, err := s.conn(id)
	if err != nil {
		return runnerd.Agent{}, err
	}
	if mode != "" && !slices.Contains(runnerd.Modes, mode) {
		return runnerd.Agent{}, fmt.Errorf("unknown permission mode %q", mode)
	}
	res, err := c.do(runnerd.Command{Op: runnerd.OpStart, Dir: dir, Prompt: prompt, Mode: mode, Name: name})
	if err != nil {
		return runnerd.Agent{}, err
	}
	var a runnerd.Agent
	return a, json.Unmarshal(res, &a)
}

func (s *Service) agentOp(id string, cmd runnerd.Command) error {
	c, err := s.conn(id)
	if err != nil {
		return err
	}
	_, err = c.do(cmd)
	return err
}

// SeenAgent: you looked at it (done → idle).
func (s *Service) SeenAgent(id, agent string) error {
	return s.agentOp(id, runnerd.Command{Op: runnerd.OpSeen, Agent: agent})
}

// RenameAgent names an agent on a runner.
func (s *Service) RenameAgent(id, agent, name string) error {
	return s.agentOp(id, runnerd.Command{Op: runnerd.OpRename, Agent: agent, Name: name})
}

// EndAgent ends an agent's tmux session. Call it only after you confirm.
func (s *Service) EndAgent(id, agent string) error {
	return s.agentOp(id, runnerd.Command{Op: runnerd.OpEnd, Agent: agent})
}

// ForgetAgent drops an agent that ended.
func (s *Service) ForgetAgent(id, agent string) error {
	return s.agentOp(id, runnerd.Command{Op: runnerd.OpForget, Agent: agent})
}

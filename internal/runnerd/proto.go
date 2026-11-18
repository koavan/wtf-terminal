// Package runnerd is wtf-runner's daemon (#41), on a remote Linux machine:
// it starts Claude Code agents in tmux sessions, runs a headless bridge
// (internal/bridge) their hooks report to, and streams what they do to wtf
// over `ssh <host> wtf-runner attach`. It keeps running while no wtf is
// attached, so agents keep working and approvals keep waiting.
//
// The stream is JSON lines. The daemon sends a hello (its version and
// every agent with its status), then agents and status messages as they
// change, and a reply to each command. wtf sends commands with an id.
package runnerd

import (
	"encoding/json"

	"github.com/dayanraj/wtf/internal/bridge"
)

// Protocol is the stream's version; it's wtf-runner's protocol too.
const Protocol = "1"

// Agent is an agent this daemon started: a tmux session wtf-<ID>.
type Agent struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// The folder it runs in, its first prompt and permission mode.
	Dir    string `json:"dir"`
	Prompt string `json:"prompt"`
	Mode   string `json:"mode"`
	// When it started (Unix milliseconds).
	Created int64 `json:"created"`
	// Its tmux pane is running; when it isn't, how it exited.
	Alive bool   `json:"alive"`
	Exit  string `json:"exit,omitempty"`
	// When it ended (Unix milliseconds); ended agents are dropped after
	// KeepEnded.
	Ended int64 `json:"ended,omitempty"`
}

// Message is what the daemon sends.
type Message struct {
	// "hello", "agents", "status" or "reply".
	Type string `json:"type"`
	// hello: the daemon's version and protocol.
	Version  string `json:"version,omitempty"`
	Protocol string `json:"protocol,omitempty"`
	// hello and agents: every agent it started.
	Agents []Agent `json:"agents,omitempty"`
	// hello: what each agent last reported (bridge statuses).
	Statuses []bridge.Status `json:"statuses,omitempty"`
	// status: one agent's new status.
	Status *bridge.Status `json:"status,omitempty"`
	// reply: to the command with this id.
	ID     int             `json:"id,omitempty"`
	Error  string          `json:"error,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
}

// Command is what wtf sends; the daemon replies with its ID.
type Command struct {
	ID int    `json:"id"`
	Op string `json:"op"`
	// The agent it's about.
	Agent string `json:"agent,omitempty"`
	// start: where, the first prompt (optional), the permission mode
	// ("" for Claude Code's default) and a name (optional).
	Dir    string `json:"dir,omitempty"`
	Prompt string `json:"prompt,omitempty"`
	Mode   string `json:"mode,omitempty"`
	Name   string `json:"name,omitempty"`
	// answer: the approval, the bridge method that answers it (Answer,
	// AnswerQuestion, AllowAlways, DenyWith, AnswerText, AnswerAll) and
	// its arguments after the id, as JSON.
	Approval string          `json:"approval,omitempty"`
	Method   string          `json:"method,omitempty"`
	Args     json.RawMessage `json:"args,omitempty"`
	// type: what to type into its pane (then Enter).
	Text string `json:"text,omitempty"`
}

// Commands.
const (
	OpStart  = "start"  // → result: the Agent
	OpSeen   = "seen"   // you looked at it: done → idle
	OpRename = "rename" // Name
	OpEnd    = "end"    // kill its tmux session
	OpForget = "forget" // drop an agent that ended
	OpQuit   = "quit"   // the daemon exits (agents keep running in tmux)
	OpAnswer = "answer" // answer one of its approvals
	OpPeek   = "peek"   // → result: the end of its screen, as text
	OpType   = "type"   // type Text into its pane and press Enter
)

// Modes Claude Code's --permission-mode takes.
var Modes = []string{"default", "acceptEdits", "plan", "auto", "bypassPermissions"}

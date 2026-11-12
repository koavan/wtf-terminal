package bridge

import (
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dayanraj/wtf/internal/bridge/wire"
)

// Subagent is one sub-agent a Claude Code session started with its Agent
// tool, from Claude Code's hooks: the Agent call (its description and the
// agentId its response names), SubagentStart and SubagentStop, and the tool
// events inside it (they carry its agent_id).
type Subagent struct {
	ID string `json:"id"`
	// Its agent type ("Explore", "general-purpose").
	Type string `json:"type"`
	// The Agent call's description: what it was asked to do, redacted.
	Task string `json:"task"`
	// While it runs: its latest step ("Read bridge.go", "Bash: go test"),
	// redacted.
	Doing string `json:"doing"`
	// Once done: its final report's first line, redacted.
	Result  string `json:"result"`
	Running bool   `json:"running"`
	// When it started and, once done, ended (Unix milliseconds).
	Since int64 `json:"since"`
	Ended int64 `json:"ended"`
}

const (
	// How many sub-agents a session keeps: the oldest finished go first.
	maxSubagents = 20
	// How much of a step, task or result the Agents tab shows.
	maxStep = 120
)

// subagentEvents are the hooks only sub-agents send.
var subagentEvents = map[string]bool{"SubagentStart": true, "SubagentStop": true}

// equal: Status has a slice, so it isn't comparable with ==.
func (a Status) equal(b Status) bool {
	return a.Session == b.Session && a.State == b.State && a.Message == b.Message && a.Prompt == b.Prompt &&
		a.Since == b.Since && a.Unseen == b.Unseen && a.Name == b.Name && a.Started == b.Started &&
		a.Folder == b.Folder && a.Git == b.Git &&
		a.ClaudeSession == b.ClaudeSession && a.Project == b.Project && a.Transcript == b.Transcript &&
		slices.Equal(a.Subagents, b.Subagents)
}

func (a Status) clone() Status {
	a.Subagents = slices.Clone(a.Subagents)
	return a
}

// subagentHook records what a hook says about a session's sub-agents, and
// returns the session's status when that changed. Nothing here changes the
// session's own state: a background sub-agent works while it's idle.
func (s *Service) subagentHook(session string, req wire.Request) (Status, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.agents[session]
	in := req.Input
	if in == nil {
		in = &wire.ToolInput{}
	}
	id := req.AgentID
	if id == "" {
		id = req.Spawned
	}
	// SubagentStop with no type is one of Claude Code's own (prompt
	// suggestions, /btw); a sub-agent wtf never heard of starting is too.
	if id == "" || (req.AgentType == "" && req.Spawned == "" && (a == nil || a.subagent(id) < 0)) {
		return Status{}, false
	}
	if a == nil {
		a = s.newAgent(session)
		a.State = StateBusy
	}
	old := a.clone()
	i := a.subagent(id)
	if i < 0 {
		if req.Event == "SubagentStop" {
			return Status{}, false
		}
		a.addSubagent(Subagent{ID: id, Running: true, Since: time.Now().UnixMilli()})
		i = len(a.Subagents) - 1
	}
	sa := &a.Subagents[i]
	if sa.Type == "" {
		sa.Type = req.AgentType
	}
	switch {
	case req.Spawned != "":
		// The Agent call that started it: for one in the background this
		// comes before SubagentStart, for one in front once it's done.
		if t := shortLine(in.Description, maxStep); t != "" {
			sa.Task = t
		}
		if sa.Type == "" {
			sa.Type = in.SubagentType
		}
		// A sub-agent in front is done when its call returns, whether or
		// not it sent SubagentStop (web-fetch sends neither Start nor Stop).
		if req.SpawnedDone && sa.Running {
			sa.Running, sa.Doing, sa.Ended = false, "", time.Now().UnixMilli()
		}
	case req.Event == "SubagentStart":
		// Started, or resumed with another message.
		if !sa.Running {
			sa.Running, sa.Since, sa.Ended, sa.Result = true, time.Now().UnixMilli(), 0, ""
		}
	case req.Event == "SubagentStop":
		sa.Running, sa.Doing, sa.Ended = false, "", time.Now().UnixMilli()
		if r := shortLine(req.Message, maxStep); r != "" {
			sa.Result = r
		}
	case req.Tool == "SubagentHandback":
		sa.Result = shortLine(in.Message, maxStep)
	case req.Event == "PostToolUse" && sa.Running:
		sa.Doing = step(req.Tool, in)
	}
	return a.clone(), !a.equal(old)
}

func (a *Status) subagent(id string) int {
	return slices.IndexFunc(a.Subagents, func(s Subagent) bool { return s.ID == id })
}

// addSubagent keeps at most maxSubagents: the oldest finished go first.
func (a *Status) addSubagent(sa Subagent) {
	a.Subagents = append(a.Subagents, sa)
	for len(a.Subagents) > maxSubagents {
		i := slices.IndexFunc(a.Subagents, func(s Subagent) bool { return !s.Running })
		if i < 0 {
			i = 0
		}
		a.Subagents = slices.Delete(a.Subagents, i, i+1)
	}
}

// dropFinished forgets the sub-agents that are done: a new prompt starts
// a new list (running ones stay).
func (a *Status) dropFinished() {
	a.Subagents = slices.DeleteFunc(a.Subagents, func(s Subagent) bool { return !s.Running })
	if len(a.Subagents) == 0 {
		a.Subagents = nil
	}
}

// step says what a tool call did, short: "Bash: go test ./...",
// "Read bridge.go", "Grep TODO".
func step(tool string, in *wire.ToolInput) string {
	if i := strings.LastIndex(tool, "__"); i >= 0 && strings.HasPrefix(tool, "mcp__") {
		tool = tool[i+2:]
	}
	var what string
	switch {
	case in.Command != "":
		return shortLine(tool+": "+firstLine(in.Command), maxStep)
	case in.FilePath != "":
		what = filepath.Base(in.FilePath)
	case in.Pattern != "":
		what = in.Pattern
	case in.URL != "":
		if u, err := url.Parse(in.URL); err == nil && u.Host != "" {
			what = u.Host
		}
	case in.Query != "":
		what = in.Query
	case in.Path != "":
		what = filepath.Base(in.Path)
	}
	if what = firstLine(what); what != "" {
		tool += " " + what
	}
	return shortLine(tool, maxStep)
}

func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

// Package wire is what the app's bridge (internal/bridge) and the plugin's
// MCP helper (cmd/wtf-mcp) say to each other over the bridge's Unix
// socket: one JSON request per connection, one JSON response, each a line.
// It has no dependencies, so the helper stays small.
package wire

import "encoding/json"

// Version of the protocol; a helper from another wtf is turned away.
const Version = 1

// Environment variables every wtf shell gets.
const (
	EnvPane   = "WTF_PANE"
	EnvBridge = "WTF_BRIDGE"
	EnvToken  = "WTF_BRIDGE_TOKEN"
	EnvMCP    = "WTF_MCP"
	// The status-only Claude Code settings (agent-status): wtf's shell
	// integration runs claude with --settings "$WTF_AGENT_SETTINGS".
	EnvAgentSettings = "WTF_AGENT_SETTINGS"
)

// Request is what the helper sends.
type Request struct {
	V     int    `json:"v"`
	Pane  string `json:"pane"`
	Token string `json:"token"`
	// "list", "call" or "hook".
	Op string `json:"op"`
	// call: the MCP tool; hook: the tool a tool event is about.
	Tool string          `json:"tool,omitempty"`
	Args json.RawMessage `json:"args,omitempty"`
	// hook: Claude Code's hook_event_name, a Notification's message and a
	// UserPromptSubmit's prompt.
	Event   string `json:"event,omitempty"`
	Message string `json:"message,omitempty"`
	Prompt  string `json:"prompt,omitempty"`
	// hook: the folder Claude Code works in (its cwd), its session id and
	// transcript, to resume it after a restart (#38).
	Cwd           string `json:"cwd,omitempty"`
	ClaudeSession string `json:"claudeSession,omitempty"`
	Transcript    string `json:"transcript,omitempty"`
	// Sub-agents: the sub-agent a tool event or SubagentStart/Stop is
	// about (Claude Code's agent_id and agent_type; "" for the main
	// conversation), the few tool_input fields wtf shows (Tool is the
	// tool's name), and the agentId an Agent tool call's response names.
	AgentID   string     `json:"agentId,omitempty"`
	AgentType string     `json:"agentType,omitempty"`
	Input     *ToolInput `json:"input,omitempty"`
	Spawned   string     `json:"spawned,omitempty"`
	// The Agent call returned with the sub-agent finished (its response's
	// status isn't "async_launched"): some sub-agents (web-fetch) send no
	// SubagentStart or SubagentStop, so this is the only end wtf hears of.
	SpawnedDone bool `json:"spawnedDone,omitempty"`
}

// ToolInput is what wtf shows of a tool call's input: an Agent call's
// description and subagent_type, and what a step works on.
type ToolInput struct {
	Description  string `json:"description,omitempty"`
	SubagentType string `json:"subagent_type,omitempty"`
	Command      string `json:"command,omitempty"`
	FilePath     string `json:"file_path,omitempty"`
	Path         string `json:"path,omitempty"`
	Pattern      string `json:"pattern,omitempty"`
	URL          string `json:"url,omitempty"`
	Query        string `json:"query,omitempty"`
	// SubagentHandback: a sub-agent's final report.
	Message string `json:"message,omitempty"`
}

// Response is the app's answer.
type Response struct {
	Tools []Tool `json:"tools,omitempty"`
	// A call's result text; IsError marks a tool error the model should see.
	Text    string `json:"text,omitempty"`
	IsError bool   `json:"isError,omitempty"`
	// The request itself failed (bad token, unknown op).
	Error string `json:"error,omitempty"`
}

// Tool is one MCP tool as the helper registers it.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

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
)

// Request is what the helper sends.
type Request struct {
	V     int    `json:"v"`
	Pane  string `json:"pane"`
	Token string `json:"token"`
	// "list", "call" or "hook".
	Op   string          `json:"op"`
	Tool string          `json:"tool,omitempty"`
	Args json.RawMessage `json:"args,omitempty"`
	// hook: Claude Code's hook_event_name, and a Notification's message.
	Event   string `json:"event,omitempty"`
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

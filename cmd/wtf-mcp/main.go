// Command wtf-mcp is the wtf plugin's MCP server (#32). Claude Code starts
// it, over stdio, inside a wtf shell; it reads WTF_PANE, WTF_BRIDGE and
// WTF_BRIDGE_TOKEN from the environment, asks the running app for its tool
// list, and relays every call to the app over the bridge's Unix socket
// (internal/bridge; the messages are in internal/bridge/wire). It holds no
// logic of its own, so the tools always match the app that's running.
//
// "wtf-mcp hook" is the plugin's hook command: it reads a Claude Code hook
// event on stdin. For PreToolUse it answers "ask", so every command shows
// Claude Code's own prompt even if wtf is gone (the app only adds, within
// two seconds, where a command would act); the other events tell the app
// whether Claude Code is busy, idle or waiting for the user. With
// WTF_CLAUDE_AUTO=1 (claude-auto-mode, set by the app when it starts
// claude) PreToolUse answers nothing, so Claude Code's own permission mode
// decides. A PermissionRequest waits for your answer in wtf's Approvals
// inbox (#39) while Claude Code shows its own dialog; answered in the pane,
// it says nothing.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dayanraj/wtf/internal/bridge/wire"
)

// Set by the build (-ldflags "-X main.version=…").
var version = "dev"

// A call can wait for a command to finish in the shell (the app limits it).
const callTimeout = 4 * time.Minute

// How long a runner's permission prompt waits for its daemon to come back
// (0.5 + 1 + … + 32 s, about a minute).
const runnerRetry = 60 * time.Second

type client struct{ sock, pane, token string }

func (c client) do(ctx context.Context, req wire.Request) (wire.Response, error) {
	req.V, req.Pane, req.Token = wire.Version, c.pane, c.token
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", c.sock)
	if err != nil {
		return wire.Response{}, errors.New("can't reach wtf (was it quit, or is this shell from an older wtf?)")
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		conn.SetDeadline(dl)
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	line, _ := json.Marshal(req)
	if _, err := conn.Write(append(line, '\n')); err != nil {
		return wire.Response{}, err
	}
	out, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return wire.Response{}, fmt.Errorf("wtf closed the connection: %w", err)
	}
	var resp wire.Response
	if err := json.Unmarshal(out, &resp); err != nil {
		return resp, err
	}
	if resp.Error != "" {
		return resp, errors.New(resp.Error)
	}
	return resp, nil
}

func main() {
	log.SetFlags(0)
	log.SetPrefix("wtf-mcp: ")
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println(version)
		return
	}
	c := client{os.Getenv(wire.EnvBridge), os.Getenv(wire.EnvPane), os.Getenv(wire.EnvToken)}
	if len(os.Args) > 1 && (os.Args[1] == "hook" || os.Args[1] == "codex-hook") {
		provider := "claude"
		if os.Args[1] == "codex-hook" {
			provider = "codex"
		}
		os.Exit(hook(c, provider, os.Stdin, os.Stdout))
	}
	if c.sock == "" || c.pane == "" || c.token == "" {
		log.Fatal("not running in a wtf shell (WTF_PANE, WTF_BRIDGE and WTF_BRIDGE_TOKEN are unset)")
	}
	ctx := context.Background()
	lctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	list, err := c.do(lctx, wire.Request{Op: "list"})
	cancel()
	if err != nil {
		log.Fatal(err)
	}

	s := mcp.NewServer(&mcp.Implementation{Name: "wtf", Version: version}, &mcp.ServerOptions{
		Instructions: "Tools for the user's shell in wtf, the terminal they're working in. They read what wtf already knows about the shell " +
			"(its context, recent commands and output), run allowlisted read-only commands where the user sees them, and type commands " +
			"at the prompt for the user to run. Secrets are redacted from everything they return.",
	})
	for _, t := range list.Tools {
		name := t.Name
		s.AddTool(&mcp.Tool{Name: name, Description: t.Description, InputSchema: t.InputSchema},
			func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				ctx, cancel := context.WithTimeout(ctx, callTimeout)
				defer cancel()
				resp, err := c.do(ctx, wire.Request{Op: "call", Tool: name, Args: req.Params.Arguments})
				if err != nil {
					return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil
				}
				return &mcp.CallToolResult{IsError: resp.IsError, Content: []mcp.Content{&mcp.TextContent{Text: resp.Text}}}, nil
			})
	}
	if err := s.Run(ctx, &mcp.StdioTransport{}); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}

// askReason is shown in Claude Code's permission prompt.
const askReason = "wtf asks before every command Claude Code runs or types in your shell, and every file it changes."

// hook handles one hook event and returns the exit code. A PreToolUse it
// can't read exits 2, which makes Claude Code refuse the tool call.
func hook(c client, provider string, in io.Reader, out io.Writer) int {
	var ev struct {
		Event   string `json:"hook_event_name"`
		Message string `json:"message"`
		Prompt  string `json:"prompt"`
		Cwd     string `json:"cwd"`
		// Claude Code's session, to resume it after a restart.
		SessionID  string `json:"session_id"`
		Transcript string `json:"transcript_path"`
		// Sub-agents (SubagentStart/Stop, and tool events from inside one).
		AgentID   string          `json:"agent_id"`
		AgentType string          `json:"agent_type"`
		Last      string          `json:"last_assistant_message"`
		Tool      string          `json:"tool_name"`
		Input     *wire.ToolInput `json:"tool_input"`
		Response  json.RawMessage `json:"tool_response"`
		// PermissionRequest: what "don't ask again" would save.
		Suggestions []wire.Suggestion `json:"permission_suggestions"`
		// Stop: the session's background work in flight.
		Background []struct {
			Status string `json:"status"`
		} `json:"background_tasks"`
	}
	data, err := io.ReadAll(io.LimitReader(in, 16<<20))
	if err == nil {
		err = json.Unmarshal(data, &ev)
	}
	if err != nil || ev.Event == "" {
		log.Printf("can't read the hook event: %v", err)
		return 2
	}
	if ev.Event == "PreToolUse" && provider == "claude" {
		// claude-auto-mode: Claude Code's own permission mode decides.
		if os.Getenv("WTF_CLAUDE_AUTO") == "1" {
			return 0
		}
		reason := askReason
		if facts := toolFacts(c, ev.Tool, ev.Input, ev.Cwd); facts != "" {
			reason += "\n\nWhere it acts:\n" + facts
		}
		answer, _ := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{
			"hookEventName": "PreToolUse", "permissionDecision": "ask", "permissionDecisionReason": reason,
		}})
		if _, err := fmt.Fprintf(out, "%s\n", answer); err != nil {
			return 2
		}
		return 0
	}
	if c.sock == "" || c.pane == "" || c.token == "" {
		return 0
	}
	if ev.Event != "PermissionRequest" && ev.Input != nil {
		// Only a permission prompt shows the change.
		ev.Input.OldString, ev.Input.NewString, ev.Input.Content, ev.Input.Edits = "", "", "", nil
	}
	// Best effort: a status the app misses changes nothing. A permission
	// prompt waits as long as Claude Code lets the hook run.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	if ev.Event == "PermissionRequest" && provider == "claude" {
		cancel()
		ctx, cancel = context.WithCancel(context.Background())
		clipEdit(ev.Input)
	}
	defer cancel()
	req := wire.Request{Op: "hook", Provider: provider, Event: ev.Event, Message: ev.Message, Prompt: ev.Prompt, Cwd: ev.Cwd,
		ClaudeSession: ev.SessionID, Transcript: ev.Transcript,
		AgentID: ev.AgentID, AgentType: ev.AgentType, Tool: ev.Tool, Input: ev.Input}
	if ev.Event == "Stop" {
		for _, t := range ev.Background {
			if t.Status == "running" || t.Status == "pending" {
				req.Background++
			}
		}
	}
	if ev.Event == "SubagentStop" && req.Message == "" {
		req.Message = ev.Last
	}
	if ev.Event == "PermissionRequest" && provider == "claude" {
		req.Suggestions = ev.Suggestions
	}
	if ev.Event == "PermissionRequest" && req.Message == "" && ev.Input != nil {
		req.Message = ev.Input.Description
	}
	// An Agent call's response names the sub-agent it started.
	if ev.Tool == "Agent" || ev.Tool == "Task" {
		var r struct {
			AgentID string `json:"agentId"`
			// "async_launched" in the background, "completed" once done.
			Status string `json:"status"`
		}
		if json.Unmarshal(ev.Response, &r) == nil {
			req.Spawned = r.AgentID
			req.SpawnedDone = r.Status != "" && r.Status != "async_launched"
		}
	}
	resp, err := c.do(ctx, req)
	// On a runner (#41), a permission prompt outlives wtf-runner's daemon
	// restarting (an update): ask the new one, for as long as it takes
	// to come back. Claude Code's own dialog stays open meanwhile.
	if err != nil && ev.Event == "PermissionRequest" && provider == "claude" && os.Getenv(wire.EnvRunner) != "" {
		for wait := 500 * time.Millisecond; err != nil && wait < runnerRetry; wait *= 2 {
			time.Sleep(wait)
			resp, err = c.do(ctx, req)
		}
	}
	if err != nil {
		log.Print(err)
		return 0
	}
	if ev.Event == "PermissionRequest" && provider == "claude" && (resp.Decision == "allow" || resp.Decision == "deny") {
		decision := map[string]any{"behavior": resp.Decision}
		// A question answered in wtf: its input as Claude Code sent it,
		// with the answers.
		if len(resp.Answers) > 0 {
			var raw struct {
				Input map[string]any `json:"tool_input"`
			}
			if json.Unmarshal(data, &raw) == nil && raw.Input != nil {
				raw.Input["answers"] = resp.Answers
				decision["updatedInput"] = raw.Input
			}
		}
		if resp.Decision == "allow" && len(resp.Permissions) > 0 {
			decision["updatedPermissions"] = resp.Permissions
		}
		switch {
		case resp.Decision == "deny" && strings.TrimSpace(resp.Message) != "":
			// Denied with what to do instead: Claude carries on with it.
			decision["message"] = tellMessage + resp.Message
		case resp.Decision == "deny":
			decision["message"] = denyMessage
			decision["interrupt"] = true
		}
		answer, _ := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{
			"hookEventName": "PermissionRequest", "decision": decision,
		}})
		if _, err := fmt.Fprintf(out, "%s\n", answer); err != nil {
			return 1
		}
	}
	return 0
}

// denyMessage tells Claude why its tool call was refused.
const denyMessage = "The user denied this in wtf's Approvals inbox. Wait for their next instruction."

// tellMessage comes before what you told Claude to do instead.
const tellMessage = "The user denied this in wtf's Approvals inbox and said: "

// maxEdit is how much of an edit the helper sends; the inbox shows less.
const maxEdit = 64 << 10

func clipEdit(in *wire.ToolInput) {
	if in == nil {
		return
	}
	for _, p := range []*string{&in.OldString, &in.NewString, &in.Content} {
		if len(*p) > maxEdit {
			*p = (*p)[:maxEdit]
		}
	}
	if len(in.Edits) > 50 {
		in.Edits = in.Edits[:50]
	}
}

// toolFacts asks the app where a command would act (#40), for Claude Code's
// own dialog; "" when it isn't a command, or the app doesn't answer within
// two seconds.
func toolFacts(c client, tool string, in *wire.ToolInput, cwd string) string {
	if c.sock == "" || c.pane == "" || c.token == "" || in == nil || (in.Command == "" && len(in.Argv) == 0) {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := c.do(ctx, wire.Request{Op: "facts", Tool: tool, Input: &wire.ToolInput{Command: in.Command, Argv: in.Argv}, Cwd: cwd})
	if err != nil {
		return ""
	}
	return resp.Text
}

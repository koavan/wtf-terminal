// Command wtf-mcp is the wtf plugin's MCP server (#32). Claude Code starts
// it, over stdio, inside a wtf shell; it reads WTF_PANE, WTF_BRIDGE and
// WTF_BRIDGE_TOKEN from the environment, asks the running app for its tool
// list, and relays every call to the app over the bridge's Unix socket
// (internal/bridge; the messages are in internal/bridge/wire). It holds no
// logic of its own, so the tools always match the app that's running.
//
// "wtf-mcp hook" is the plugin's hook command: it reads a Claude Code hook
// event on stdin. For PreToolUse it answers "ask" without asking the app,
// so every command shows Claude Code's own prompt even if wtf is gone; the
// other events tell the app whether Claude Code is busy, idle or waiting
// for the user. With WTF_CLAUDE_AUTO=1 (claude-auto-mode, set by the app
// when it starts claude) PreToolUse answers nothing, so Claude Code's own
// permission mode decides.
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
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dayanraj/wtf/internal/bridge/wire"
)

// Set by the build (-ldflags "-X main.version=…").
var version = "dev"

// A call can wait for a command to finish in the shell (the app limits it).
const callTimeout = 4 * time.Minute

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
	if len(os.Args) > 1 && os.Args[1] == "hook" {
		os.Exit(hook(c, os.Stdin, os.Stdout))
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
func hook(c client, in io.Reader, out io.Writer) int {
	var ev struct {
		Event   string `json:"hook_event_name"`
		Message string `json:"message"`
		Prompt  string `json:"prompt"`
		// Sub-agents (SubagentStart/Stop, and tool events from inside one).
		AgentID   string          `json:"agent_id"`
		AgentType string          `json:"agent_type"`
		Last      string          `json:"last_assistant_message"`
		Tool      string          `json:"tool_name"`
		Input     *wire.ToolInput `json:"tool_input"`
		Response  json.RawMessage `json:"tool_response"`
	}
	data, err := io.ReadAll(io.LimitReader(in, 1<<20))
	if err == nil {
		err = json.Unmarshal(data, &ev)
	}
	if err != nil || ev.Event == "" {
		log.Printf("can't read the hook event: %v", err)
		return 2
	}
	if ev.Event == "PreToolUse" {
		// claude-auto-mode: Claude Code's own permission mode decides.
		if os.Getenv("WTF_CLAUDE_AUTO") == "1" {
			return 0
		}
		answer, _ := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{
			"hookEventName": "PreToolUse", "permissionDecision": "ask", "permissionDecisionReason": askReason,
		}})
		if _, err := fmt.Fprintf(out, "%s\n", answer); err != nil {
			return 2
		}
		return 0
	}
	if c.sock == "" || c.pane == "" || c.token == "" {
		return 0
	}
	// Best effort: a status the app misses changes nothing.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req := wire.Request{Op: "hook", Event: ev.Event, Message: ev.Message, Prompt: ev.Prompt,
		AgentID: ev.AgentID, AgentType: ev.AgentType, Tool: ev.Tool, Input: ev.Input}
	if ev.Event == "SubagentStop" && req.Message == "" {
		req.Message = ev.Last
	}
	// An Agent call's response names the sub-agent it started.
	if ev.Tool == "Agent" || ev.Tool == "Task" {
		var r struct {
			AgentID string `json:"agentId"`
		}
		if json.Unmarshal(ev.Response, &r) == nil {
			req.Spawned = r.AgentID
		}
	}
	if _, err := c.do(ctx, req); err != nil {
		log.Print(err)
	}
	return 0
}

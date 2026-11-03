// Command wtf-mcp is the wtf plugin's MCP server (#32). Claude Code starts
// it, over stdio, inside a wtf shell; it reads WTF_PANE, WTF_BRIDGE and
// WTF_BRIDGE_TOKEN from the environment, asks the running app for its tool
// list, and relays every call to the app over the bridge's Unix socket
// (internal/bridge; the messages are in internal/bridge/wire). It holds no
// logic of its own, so the tools always match the app that's running.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

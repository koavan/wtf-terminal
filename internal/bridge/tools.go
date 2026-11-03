package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dayanraj/wtf/internal/bridge/wire"
	"github.com/dayanraj/wtf/internal/explain"
	"github.com/dayanraj/wtf/internal/investigate"
	"github.com/dayanraj/wtf/internal/review"
	"github.com/dayanraj/wtf/internal/uicore"
)

const (
	// Of a command's output, the model sees the start and the end.
	headLines   = 60
	tailLines   = 140
	outputBytes = 12000
	// recent_commands: how many blocks, and the end of each one's output.
	defaultRecent = 5
	maxRecent     = 20
	recentLines   = 40
	recentBytes   = 4000
	// review_changes looks this far back for a preview.
	reviewLookback = 20
)

// Tools are what the plugin's MCP server offers. Every result is passed
// through Hide Secrets' redaction.
var Tools = []wire.Tool{
	{
		Name: "pane_context",
		Description: "What the user's wtf shell is set up for: its folder, system, Kubernetes context and namespace, AWS profile and region, " +
			"Google Cloud project, Terraform workspace and git branch (read from files, never asked of a cloud), whether a program is running in it, " +
			"and which well-known tools are on its PATH. Call it first to know where commands would run.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
	},
	{
		Name: "recent_commands",
		Description: "The last commands the user ran in their wtf shell, oldest first, each with its exit code and the end of its output. " +
			"Use it to see what failed or what the user is working on.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"count":{"type":"integer","minimum":1,"maximum":20,"description":"How many commands (default 5)."}},"additionalProperties":false}`),
	},
	{
		Name: "review_changes",
		Description: "Reads an infrastructure change preview the user ran in their wtf shell — terraform/tofu/terragrunt plan, kubectl diff or helm diff — " +
			"and lists what it will do as parsed facts: each resource created, changed, replaced or destroyed, what forces a replacement, and which " +
			"changes touch IAM, security groups, RBAC, network policies or secrets. Without block_id it reads the latest such command. " +
			"With plan_file it reads a saved Terraform plan instead (terraform show -json, which changes nothing).",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` +
			`"block_id":{"type":"integer","description":"A command block id from recent_commands."},` +
			`"plan_file":{"type":"string","description":"A saved plan file, relative to the shell's folder or absolute."}},"additionalProperties":false}`),
	},
	{
		Name: "run_readonly",
		Description: "Runs one read-only command in the user's wtf shell, where they can see it, as a normal command block, and returns its exit code and output. " +
			"Give the program and its arguments as argv (no shell syntax: no pipes, redirects or variables). Only this allowlist runs; anything else is refused with the reason:\n- " +
			strings.Join(investigate.Allowed, "\n- ") +
			"\nThe user approves every command. To suggest a command that changes something, use type_at_prompt instead.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` +
			`"argv":{"type":"array","items":{"type":"string"},"minItems":1,"description":"Program and arguments, e.g. [\"kubectl\",\"get\",\"pods\",\"-n\",\"shop\"]."},` +
			`"purpose":{"type":"string","description":"Why, in a few words, shown to the user."}},"required":["argv"],"additionalProperties":false}`),
	},
	{
		Name: "type_at_prompt",
		Description: "Types a command at the user's wtf shell prompt without pressing Enter, replacing what's typed there, for the user to read, edit and run themselves. " +
			"Use it for fixes and anything that changes state. Fails when a program is running in the shell.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","description":"The command line."}},"required":["command"],"additionalProperties":false}`),
	},
}

func (s *Service) call(ctx context.Context, session, tool string, raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		raw = json.RawMessage("{}")
	}
	var text string
	var err error
	switch tool {
	case "pane_context":
		text, err = s.paneContext(ctx, session)
	case "recent_commands":
		var a struct {
			Count int `json:"count"`
		}
		if err = json.Unmarshal(raw, &a); err == nil {
			text, err = s.recentCommands(ctx, session, a.Count)
		}
	case "review_changes":
		var a struct {
			BlockID  *int   `json:"block_id"`
			PlanFile string `json:"plan_file"`
		}
		if err = json.Unmarshal(raw, &a); err == nil {
			text, err = s.reviewChanges(ctx, session, a.BlockID, a.PlanFile)
		}
	case "run_readonly":
		var a struct {
			Argv    []string `json:"argv"`
			Purpose string   `json:"purpose"`
		}
		if err = json.Unmarshal(raw, &a); err == nil {
			text, err = s.runReadonly(ctx, session, a.Argv)
		}
	case "type_at_prompt":
		var a struct {
			Command string `json:"command"`
		}
		if err = json.Unmarshal(raw, &a); err == nil {
			text, err = s.typeAtPrompt(ctx, session, a.Command)
		}
	default:
		return "", fmt.Errorf("no tool %q", tool)
	}
	if err != nil {
		return "", fmt.Errorf("%s", uicore.Redact(err.Error()))
	}
	return uicore.Redact(text), nil
}

func (s *Service) pane(ctx context.Context, session string) (Pane, error) {
	r, err := s.ask(ctx, UIRequest{Kind: KindPane, Session: session})
	if err != nil {
		return Pane{}, err
	}
	if r.Pane == nil {
		return Pane{}, fmt.Errorf("that shell is gone")
	}
	return *r.Pane, nil
}

func (s *Service) paneContext(ctx context.Context, session string) (string, error) {
	p, err := s.pane(ctx, session)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if p.Title != "" {
		fmt.Fprintf(&b, "Tab: %s\n", p.Title)
	}
	b.WriteString(explain.ShellFacts(s.infra, p.Env, p.Cwd))
	switch {
	case p.Running && p.Process != "":
		fmt.Fprintf(&b, "State: running %s (not at its prompt)\n", p.Process)
	case p.Running:
		b.WriteString("State: a program is running (not at its prompt)\n")
	default:
		b.WriteString("State: at its prompt\n")
	}
	if !p.Integrated {
		b.WriteString("Shell integration: off, so wtf can't tell commands apart: recent_commands, review_changes and run_readonly don't work here\n")
	}
	if p.Env != nil {
		if t := explain.ToolsOnPath(p.Env); len(t) > 0 {
			fmt.Fprintf(&b, "Tools on PATH: %s\n", strings.Join(t, ", "))
		}
	}
	return b.String(), nil
}

func (s *Service) blocks(ctx context.Context, session string, n int) ([]Block, error) {
	r, err := s.ask(ctx, UIRequest{Kind: KindBlocks, Session: session, Count: n})
	return r.Blocks, err
}

func (s *Service) recentCommands(ctx context.Context, session string, n int) (string, error) {
	if n <= 0 {
		n = defaultRecent
	}
	n = min(n, maxRecent)
	list, err := s.blocks(ctx, session, n)
	if err != nil {
		return "", err
	}
	if len(list) == 0 {
		return "No commands yet in this shell (or it has no shell integration).", nil
	}
	var b strings.Builder
	for i, bl := range list {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "[block %d] $ %s\n%s\n", bl.ID, strings.TrimSpace(bl.Command), exitLine(bl))
		out, dropped := tail(bl.Output, recentLines, recentBytes)
		switch {
		case strings.TrimSpace(out) == "":
			b.WriteString("Output: none (or no longer in the scrollback)\n")
		case dropped > 0:
			fmt.Fprintf(&b, "Output (last lines; %d earlier left out):\n%s\n", dropped, out)
		default:
			fmt.Fprintf(&b, "Output:\n%s\n", out)
		}
	}
	return b.String(), nil
}

func (s *Service) reviewChanges(ctx context.Context, session string, id *int, planFile string) (string, error) {
	var r review.Review
	if planFile != "" {
		p, err := s.pane(ctx, session)
		if err != nil {
			return "", err
		}
		path := planFile
		if !strings.HasPrefix(path, "/") {
			dir := p.Cwd
			if pwd := p.Env["PWD"]; pwd != "" {
				dir = pwd
			}
			path = dir + "/" + path
		}
		if r, err = review.NewService(s.infra).ParseFile(path, p.Env); err != nil {
			return "", err
		}
	} else {
		list, err := s.blocks(ctx, session, reviewLookback)
		if err != nil {
			return "", err
		}
		var found *Block
		for i := len(list) - 1; i >= 0; i-- {
			bl := &list[i]
			if (id != nil && bl.ID == *id) || (id == nil && review.Reviewable(bl.Command)) {
				found = bl
				break
			}
		}
		switch {
		case found == nil && id != nil:
			return "", fmt.Errorf("no block %d among the last %d commands", *id, reviewLookback)
		case found == nil:
			return "", fmt.Errorf("none of the last %d commands is a terraform plan, kubectl diff or helm diff", reviewLookback)
		case found.Output == "":
			return "", fmt.Errorf("the output of `%s` has left the scrollback", strings.TrimSpace(found.Command))
		}
		r = review.NewService(s.infra).Parse(found.Command, found.Output)
	}
	if r.Tool == "" {
		return "", fmt.Errorf("that isn't the output of a terraform plan, kubectl diff or helm diff")
	}
	return review.Facts(r) + "\nExcerpt of the output:\n" + r.Excerpt + "\n", nil
}

func (s *Service) runReadonly(ctx context.Context, session string, argv []string) (string, error) {
	cmd, err := investigate.Check(argv)
	if err != nil {
		return "", fmt.Errorf("refused: %w", err)
	}
	r, err := s.ask(ctx, UIRequest{Kind: KindRun, Session: session, Text: cmd.Line})
	if err != nil {
		return "", err
	}
	if r.Block == nil {
		return "", fmt.Errorf("the command's block wasn't found")
	}
	bl := *r.Block
	var b strings.Builder
	fmt.Fprintf(&b, "[block %d] $ %s\n%s\n", bl.ID, cmd.Line, exitLine(bl))
	if bl.TimedOut {
		b.WriteString("It didn't finish in time and was interrupted (Ctrl-C).\n")
	}
	if out := clip(bl.Output); strings.TrimSpace(out) == "" {
		b.WriteString("Output: none\n")
	} else {
		fmt.Fprintf(&b, "Output:\n%s\n", out)
	}
	return b.String(), nil
}

func (s *Service) typeAtPrompt(ctx context.Context, session, command string) (string, error) {
	command = strings.TrimRight(command, "\r\n")
	if strings.TrimSpace(command) == "" {
		return "", fmt.Errorf("no command")
	}
	if strings.ContainsFunc(command, func(r rune) bool { return (r < 0x20 && r != '\n' && r != '\t') || r == 0x7f }) {
		return "", fmt.Errorf("the command can't hold control characters")
	}
	if _, err := s.ask(ctx, UIRequest{Kind: KindType, Session: session, Text: command}); err != nil {
		return "", err
	}
	return "Typed at the prompt. Nothing ran: the user reads it and presses Enter if they want it.", nil
}

func exitLine(b Block) string {
	if b.ExitCode < 0 {
		return "Exit code: unknown (still running?)"
	}
	return fmt.Sprintf("Exit code: %d", b.ExitCode)
}

// clip keeps the start and the end of a command's output.
func clip(out string) string {
	out = strings.TrimRight(out, "\n")
	lines := strings.Split(out, "\n")
	if len(lines) > headLines+tailLines {
		cut := len(lines) - headLines - tailLines
		lines = append(append(lines[:headLines:headLines], fmt.Sprintf("… (%d lines left out) …", cut)), lines[len(lines)-tailLines:]...)
	}
	s := strings.Join(lines, "\n")
	if len(s) > outputBytes {
		half := outputBytes / 2
		s = s[:half] + "\n… (cut) …\n" + s[len(s)-half:]
	}
	return strings.ToValidUTF8(s, "")
}

// tail is the last lines of out, and how many lines were left out.
func tail(out string, maxLines, maxBytes int) (string, int) {
	out = strings.TrimRight(out, "\n")
	if out == "" {
		return "", 0
	}
	lines := strings.Split(out, "\n")
	dropped := 0
	if len(lines) > maxLines {
		dropped = len(lines) - maxLines
		lines = lines[dropped:]
	}
	for len(lines) > 1 && len(strings.Join(lines, "\n")) > maxBytes {
		lines = lines[1:]
		dropped++
	}
	s := strings.Join(lines, "\n")
	if len(s) > maxBytes {
		s = s[len(s)-maxBytes:]
	}
	return strings.ToValidUTF8(s, ""), dropped
}

// Package investigate is the incident investigator (#27): the user asks
// why something is broken ("why is checkout-api crashlooping in
// staging?"), and the AI investigates with read-only commands, run one at a
// time in the user's own shell as normal command blocks, until it can give
// a finding that cites them. The model only ever names a program and its
// arguments: Check (allow.go) accepts it or says why not; the UI shows it
// and runs nothing until the user approves that command (Run) or declines
// it (Skip), then types the quoted line into the shell and sends back the
// block's output. A run
// stops at a step limit, a token limit, or Stop. A fix that changes
// anything is never run: it comes back as a command to type at the prompt.
package investigate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/dayanraj/wtf/internal/ai"
	"github.com/dayanraj/wtf/internal/events"
	"github.com/dayanraj/wtf/internal/explain"
	"github.com/dayanraj/wtf/internal/infra"
)

const (
	// The model's text as it streams.
	EventText = "investigate:text"
	// Ask the user to run a command (or skip it), then send back Result.
	EventRun = "investigate:run"
	// A command the model asked for was refused.
	EventRefused = "investigate:refused"
	// The investigation ended.
	EventDone = "investigate:done"

	// MaxSteps is how many commands one investigation runs at most.
	MaxSteps = 12
	// MaxTokens is the input and output tokens one investigation may use,
	// as far as the provider counts them.
	MaxTokens = 250_000
	// A step left unanswered this long ends the investigation (the user
	// approves each command first, and the UI interrupts a slow one).
	stepTimeout = 30 * time.Minute
	turnTokens  = 2048
	// Of each command's output, the model sees the start and the end.
	headLines   = 60
	tailLines   = 140
	outputBytes = 12000
	// Text streams to the UI at most this often.
	flushEvery = 80 * time.Millisecond
)

// Question is what the user asks, and the shell it's about.
type Question struct {
	Text string            `json:"text"`
	Env  map[string]string `json:"env"`
	Cwd  string            `json:"cwd"`
	// The shell's last few finished commands, oldest first: often what the
	// question is about.
	Recent []Recent `json:"recent"`
}

// Recent is a command the user ran before asking.
type Recent struct {
	Command  string `json:"command"`
	ExitCode int    `json:"exitCode"`
	Output   string `json:"output"`
}

const (
	// Of the recent commands, the model sees the last few and the end of
	// each one's output.
	recentCommands = 3
	recentLines    = 30
	recentBytes    = 3000
)

// Text is EventText's payload.
type Text struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// Run is EventRun's payload: ask the user, and when they approve, type
// Line into the shell; then send Result.
type Run struct {
	ID   string `json:"id"`
	Step int    `json:"step"`
	Line string `json:"line"`
	// Why, as the model said.
	Purpose string `json:"purpose"`
}

// Refusal is EventRefused's payload.
type Refusal struct {
	ID      string   `json:"id"`
	Argv    []string `json:"argv"`
	Reason  string   `json:"reason"`
	Purpose string   `json:"purpose"`
}

// StepResult is how a command went, from the UI.
type StepResult struct {
	ExitCode int    `json:"exitCode"`
	Output   string `json:"output"`
	// It didn't finish in time and was interrupted.
	TimedOut bool `json:"timedOut"`
	// The user chose not to run it.
	Declined bool `json:"declined"`
	// It couldn't run (the shell was busy or closed); the run ends.
	Error string `json:"error"`
	// The block, for linking a step to it.
	Block int `json:"block"`
}

// Done is EventDone's payload.
type Done struct {
	ID string `json:"id"`
	// The finding, as the model wrote it.
	Finding string `json:"finding"`
	// A command to type at the prompt (never run), when it gave one.
	Fix string `json:"fix"`
	// Commands asked for (approved or not), and how many the user declined.
	Steps    int `json:"steps"`
	Declined int `json:"declined"`
	// Why it stopped early: "steps" or "tokens", else "".
	Limit     string   `json:"limit"`
	Usage     ai.Usage `json:"usage"`
	Provider  string   `json:"provider"`
	Error     string   `json:"error"`
	Cancelled bool     `json:"cancelled"`
	Millis    int64    `json:"millis"`
}

type run struct {
	cancel  context.CancelFunc
	pending map[int]chan StepResult
}

type Service struct {
	ai    *ai.Service
	infra *infra.Service

	mu   sync.Mutex
	runs map[string]*run
}

func NewService(a *ai.Service, in *infra.Service) *Service {
	return &Service{ai: a, infra: in, runs: map[string]*run{}}
}

const system = `You investigate a problem in the user's infrastructure from their terminal, like a careful on-call engineer.

First decide whether the question needs commands at all. A general question (whether something is possible, how to do something, what a tool or error means) needs none: answer it directly from what you know, without calling the tool, in plain text. Use the tool only to look at the current state of the user's systems when the question is about them and the facts don't already answer it.

You have one tool, run, which asks the user to run a read-only command in their own shell. They see each command and why you want it, and approve or decline it; a declined command is answered "The user declined", and then you must not ask for it or something like it again, but continue with what you have or finish. It uses the shell's Kubernetes context and namespace, AWS profile and region (given in the facts); don't switch them. Each command shows in their terminal as you run it. Allowed:
%s
Anything else is refused and you're told why. Give argv as separate arguments, program first: no shell syntax (pipes, redirects, globs, $variables, &&), since every argument is passed as is. Use -o wide, label selectors, --sort-by and --tail to keep output short.

The facts end with the commands the user ran last in this shell and their output. When the question is about one of them (a command that failed, an error they just saw) and that output answers it, answer from it right away without running anything; run commands only for what it doesn't show.

Work step by step: start broad (what's failing, recent events), then narrow down, one command at a time; say in a sentence what you're checking before each. You can run at most %d commands; stop as soon as the evidence answers the question.

Never try to change anything. When a fix needs a change (rollback, restart, scale, edit), propose it at the end; the user runs it.

When done, reply without calling the tool, in plain text without Markdown headings:
Finding: what is wrong, in one or two sentences.
Evidence: one line per fact, citing the step that showed it as [1], [2], quoting the telling output.
Next: what to do about it. If one command fixes or mitigates it, give it alone in a fenced block marked sh; it's typed at the user's prompt for them to review, not run.
If the evidence isn't enough to be sure, say what's missing.`

var runTool = ai.Tool{
	Name:        "run",
	Description: "Runs one read-only command in the user's shell and returns its exit code and output (long output keeps its start and end).",
	Schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"argv": map[string]any{"type": "array", "items": map[string]any{"type": "string"},
				"description": `The program and its arguments, one per item, e.g. ["kubectl","get","pods","-n","staging"]`},
			"purpose": map[string]any{"type": "string", "description": "What this checks, in a few words"},
		},
		"required": []string{"argv"},
	},
}

// Request is the investigation's opening request, for the first-send
// preview. It reads the shell's context files, so call it off the main
// thread.
func (s *Service) Request(q Question) (ai.Request, error) {
	text := strings.TrimSpace(q.Text)
	if text == "" {
		return ai.Request{}, errors.New("ask a question")
	}
	return ai.Request{System: systemText(), Messages: []ai.Message{{Role: "user", Content: s.opening(q, text)}}, Env: q.Env}, nil
}

func systemText() string {
	return fmt.Sprintf(system, "- "+strings.Join(Allowed, "\n- "), MaxSteps)
}

func (s *Service) opening(q Question, text string) string {
	cwd := q.Cwd
	if pwd := q.Env["PWD"]; pwd != "" {
		cwd = pwd
	}
	facts := explain.ShellFacts(s.infra, q.Env, cwd)
	if t := explain.ToolsOnPath(q.Env); len(t) > 0 {
		facts += "Installed: " + strings.Join(t, ", ") + "\n"
	}
	if r := recentText(q.Recent); r != "" {
		facts += "\nThe last commands in this shell, oldest first:\n" + r
	}
	return facts + "\nQuestion: " + text + "\n"
}

// recentText is the last few commands with their exit codes and the end of
// their output.
func recentText(list []Recent) string {
	if len(list) > recentCommands {
		list = list[len(list)-recentCommands:]
	}
	var b strings.Builder
	for _, r := range list {
		if strings.TrimSpace(r.Command) == "" {
			continue
		}
		fmt.Fprintf(&b, "$ %s   (exit %d)\n", strings.TrimSpace(r.Command), r.ExitCode)
		out := strings.TrimRight(r.Output, "\n ")
		lines := strings.Split(out, "\n")
		if len(lines) > recentLines {
			lines = append([]string{fmt.Sprintf("… (%d earlier lines)", len(lines)-recentLines)}, lines[len(lines)-recentLines:]...)
		}
		out = strings.Join(lines, "\n")
		if len(out) > recentBytes {
			out = "…" + out[len(out)-recentBytes:]
		}
		if strings.TrimSpace(out) == "" {
			out = "(no output)"
		}
		b.WriteString(out + "\n")
	}
	return b.String()
}

// Start begins an investigation and returns at once; it reports through
// events and ends with EventDone. shown says the user saw Request's preview
// (needed before the first request to a provider).
func (s *Service) Start(id string, q Question, shown bool) error {
	first, err := s.Request(q)
	if err != nil {
		return err
	}
	sess, err := ai.NewToolSession(s.ai, false, q.Env, first, shown)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	if _, busy := s.runs[id]; busy {
		s.mu.Unlock()
		cancel()
		return fmt.Errorf("investigation %s is already running", id)
	}
	r := &run{cancel: cancel, pending: map[int]chan StepResult{}}
	s.runs[id] = r
	s.mu.Unlock()
	go func() {
		d := s.loop(ctx, id, r, sess, first)
		s.mu.Lock()
		delete(s.runs, id)
		s.mu.Unlock()
		cancel()
		events.Emit(EventDone, d)
	}()
	return nil
}

// Result reports how step's command went.
func (s *Service) Result(id string, step int, res StepResult) error {
	s.mu.Lock()
	var ch chan StepResult
	if r := s.runs[id]; r != nil {
		ch = r.pending[step]
		delete(r.pending, step)
	}
	s.mu.Unlock()
	if ch == nil {
		return fmt.Errorf("investigation %s isn't waiting for step %d", id, step)
	}
	ch <- res
	return nil
}

// Stop ends an investigation; its EventDone says Cancelled.
func (s *Service) Stop(id string) {
	s.mu.Lock()
	r := s.runs[id]
	s.mu.Unlock()
	if r != nil {
		r.cancel()
	}
}

// ServiceShutdown stops every investigation.
func (s *Service) ServiceShutdown() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.runs {
		r.cancel()
	}
	return nil
}

func (s *Service) loop(ctx context.Context, id string, r *run, sess *ai.ToolSession, first ai.Request) Done {
	start := time.Now()
	d := Done{ID: id, Provider: sess.Info.Name}
	defer func() { d.Millis = time.Since(start).Milliseconds() }()
	turns := []ai.Turn{{Role: "user", Text: first.Messages[0].Content}}
	tools := []ai.Tool{runTool}

	var pending strings.Builder
	last := time.Now()
	flush := func() {
		if pending.Len() > 0 {
			events.Emit(EventText, Text{ID: id, Text: pending.String()})
			pending.Reset()
		}
		last = time.Now()
	}
	out := func(t string) {
		pending.WriteString(t)
		if time.Since(last) >= flushEvery {
			flush()
		}
	}

	finalAsked := false
	for {
		reply, u, err := sess.Send(ctx, first.System, turns, tools, turnTokens, out)
		flush()
		d.Usage.Counted = d.Usage.Counted || u.Counted
		d.Usage.InputTokens += u.InputTokens
		d.Usage.OutputTokens += u.OutputTokens
		d.Usage.Cost += u.Cost
		switch {
		case ctx.Err() != nil:
			d.Cancelled = true
			return d
		case err != nil:
			d.Error = err.Error()
			return d
		}
		turns = append(turns, reply)
		if len(reply.Calls) == 0 || finalAsked {
			d.Finding = strings.TrimSpace(reply.Text)
			_, d.Fix = explain.SplitCommand(reply.Text)
			return d
		}
		if d.Usage.InputTokens+d.Usage.OutputTokens >= MaxTokens {
			d.Limit = "tokens"
		}

		results := make([]ai.ToolResult, 0, len(reply.Calls))
		for _, call := range reply.Calls {
			res := ai.ToolResult{CallID: call.ID}
			switch {
			case d.Limit != "":
				res.Content, res.IsError = "Not run: the investigation's limit is reached.", true
			case d.Steps >= MaxSteps:
				d.Limit = "steps"
				res.Content, res.IsError = "Not run: the investigation's limit is reached.", true
			default:
				content, isErr, stop := s.step(ctx, id, r, &d, call)
				if stop != "" {
					d.Error = stop
					return d
				}
				if ctx.Err() != nil {
					d.Cancelled = true
					return d
				}
				res.Content, res.IsError = content, isErr
			}
			results = append(results, res)
		}
		next := ai.Turn{Role: "user", Results: results}
		if d.Limit != "" {
			what := map[string]string{"steps": fmt.Sprintf("%d commands", MaxSteps), "tokens": "token budget"}[d.Limit]
			next.Text = "The investigation's limit (" + what + ") is reached: no more commands. Give your finding now from the evidence so far, in the format asked, and say what's still unknown."
			finalAsked = true
		}
		turns = append(turns, next)
	}
}

// step checks and runs one tool call. stop ends the run (the shell can't
// take commands).
func (s *Service) step(ctx context.Context, id string, r *run, d *Done, call ai.ToolCall) (content string, isErr bool, stop string) {
	if call.Name != runTool.Name {
		return fmt.Sprintf("There is no tool %q; use run.", call.Name), true, ""
	}
	var in struct {
		Argv    []string `json:"argv"`
		Purpose string   `json:"purpose"`
	}
	if err := json.Unmarshal(call.Input, &in); err != nil {
		return "The input must be {\"argv\": [\"program\", \"arg\", …], \"purpose\": \"…\"}.", true, ""
	}
	cmd, err := Check(in.Argv)
	if err != nil {
		events.Emit(EventRefused, Refusal{ID: id, Argv: in.Argv, Reason: err.Error(), Purpose: in.Purpose})
		return "Refused: " + err.Error(), true, ""
	}
	d.Steps++
	n := d.Steps
	ch := make(chan StepResult, 1)
	s.mu.Lock()
	r.pending[n] = ch
	s.mu.Unlock()
	events.Emit(EventRun, Run{ID: id, Step: n, Line: cmd.Line, Purpose: in.Purpose})
	var res StepResult
	select {
	case res = <-ch:
	case <-ctx.Done():
		return "", false, ""
	case <-time.After(stepTimeout):
		res = StepResult{TimedOut: true, ExitCode: -1}
	}
	if res.Error != "" {
		return "", false, res.Error
	}
	if res.Declined {
		d.Declined++
		return fmt.Sprintf("$ %s\nThe user declined to run this command.", cmd.Line), true, ""
	}
	return describe(n, cmd.Line, res), false, ""
}

// describe is a step's result as the model sees it.
func describe(n int, line string, r StepResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[%d] $ %s\n", n, line)
	if r.TimedOut {
		b.WriteString("It didn't finish in time and was interrupted. Output so far:\n")
	} else {
		fmt.Fprintf(&b, "Exit code %d. Output:\n", r.ExitCode)
	}
	out := strings.TrimRight(r.Output, "\n ")
	if out == "" {
		out = "(none)"
	}
	b.WriteString(clip(out))
	return b.String()
}

// clip keeps the start and end of long output.
func clip(out string) string {
	lines := strings.Split(out, "\n")
	if len(lines) > headLines+tailLines {
		cut := len(lines) - headLines - tailLines
		lines = append(append(lines[:headLines:headLines], fmt.Sprintf("… (%d lines left out) …", cut)), lines[len(lines)-tailLines:]...)
	}
	s := strings.Join(lines, "\n")
	if len(s) > outputBytes {
		s = s[:outputBytes/3] + "\n… (cut) …\n" + s[len(s)-outputBytes*2/3:]
	}
	return s
}

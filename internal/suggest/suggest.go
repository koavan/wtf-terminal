// Package suggest builds "plain English to command" requests: what the user
// asked for, with the facts of the focused shell (folder, system, shell,
// Kubernetes and AWS context, the well-known tools on its PATH), and reads
// the reply back as one command and a sentence about it. The command is only
// ever typed at the prompt, never run. On request, a follow-up explains each
// part of the command.
package suggest

import (
	"errors"
	"strings"

	"github.com/dayanraj/wtf/internal/ai"
	"github.com/dayanraj/wtf/internal/explain"
	"github.com/dayanraj/wtf/internal/infra"
)

const (
	maxTokens      = 800
	flagsMaxTokens = 1200
	// Longer requests are cut: this is a prompt bar, not a document.
	maxAsk = 2000
)

// Ask is what the user typed, and the shell it's for.
type Ask struct {
	Text string `json:"text"`
	// The shell's variables at its last prompt (PWD, PATH, KUBECONFIG…).
	Env map[string]string `json:"env"`
	// The folder, when Env has no PWD.
	Cwd string `json:"cwd"`
}

// Suggestion is a finished reply, split for the UI.
type Suggestion struct {
	// The command to type at the prompt; empty when the model gave none.
	Command string `json:"command"`
	// What it does, as the model put it (or why there's no command).
	Note string `json:"note"`
}

type Service struct {
	infra *infra.Service
}

func NewService(in *infra.Service) *Service { return &Service{infra: in} }

const system = `You turn a request written in plain English into one shell command for the user's terminal. The facts give their system, shell, folder, the Kubernetes and AWS context their shell uses, and the tools installed.

Reply with the command alone in a fenced block marked sh, then one short sentence saying what it does. Rules:
- One command line. Join steps with pipes or && when the request needs more than one.
- Write it for the shell, system and tools given (e.g. BSD or GNU flags as the system has them); prefer the tools listed as installed.
- The shell already uses the Kubernetes context and namespace and the AWS profile and region given: don't repeat them as flags unless the request names another one.
- Write <placeholder> for any name or value you'd have to guess.
- If the request isn't something a command can do, reply with one sentence and no block.

The command is typed at the user's prompt for them to read, edit and run; nothing runs it for them.`

const flagsAsk = `Explain each part of that command: every program, subcommand, flag and argument, in order, one per line as "part — what it does". Plain text only, no Markdown, no introduction.`

// Request is the AI request for a. It reads the shell's context files, so
// call it off the main thread.
func (s *Service) Request(a Ask) (ai.Request, error) {
	text := strings.TrimSpace(a.Text)
	if text == "" {
		return ai.Request{}, errors.New("describe the command you want")
	}
	if len(text) > maxAsk {
		text = text[:maxAsk]
	}
	return ai.Request{
		Fast:      true,
		System:    system,
		Messages:  []ai.Message{{Role: "user", Content: s.facts(a) + "\nRequest: " + text + "\n"}},
		MaxTokens: maxTokens,
		Env:       a.Env,
	}, nil
}

// FlagsRequest continues req, whose reply was reply: explain the command's
// parts.
func (s *Service) FlagsRequest(req ai.Request, reply string) ai.Request {
	req.Messages = append(append([]ai.Message{}, req.Messages...),
		ai.Message{Role: "assistant", Content: reply},
		ai.Message{Role: "user", Content: flagsAsk})
	req.MaxTokens = flagsMaxTokens
	return req
}

// Parse splits a finished reply into the command and its note.
func (s *Service) Parse(text string) Suggestion {
	note, cmd := explain.SplitCommand(text)
	return Suggestion{Command: cmd, Note: note}
}

func (s *Service) facts(a Ask) string {
	cwd := a.Cwd
	if pwd := a.Env["PWD"]; pwd != "" {
		cwd = pwd
	}
	f := explain.ShellFacts(s.infra, a.Env, cwd)
	if t := explain.ToolsOnPath(a.Env); len(t) > 0 {
		f += "Installed: " + strings.Join(t, ", ") + "\n"
	}
	return f
}

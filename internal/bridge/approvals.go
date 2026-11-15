package bridge

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dayanraj/wtf/internal/agentctx"
	"github.com/dayanraj/wtf/internal/bridge/wire"
	"github.com/dayanraj/wtf/internal/events"
	"github.com/dayanraj/wtf/internal/uicore"
)

// Approvals inbox (#39): every permission prompt Claude Code shows, listed
// on its agent's status, answerable from the Agents tab. Claude Code's
// PermissionRequest hook fires only when it would ask you anyway, so the
// permission mode of a claude you start yourself stays its own. The hook
// (wtf-mcp hook) waits on the bridge's socket for the answer while Claude
// Code shows its own dialog: whichever you answer first applies.
//
// Checked with Claude Code 2.1.325: the dialog shows while the hook runs,
// and a hook's later allow or deny closes it. Answered in the pane, a deny
// interrupts the turn and kills the hook (its connection closes); an allow
// leaves the hook running, so the approval is cleared when the tool's
// PostToolUse (or PostToolUseFailure) comes, or the turn ends.

// Approval is one permission prompt waiting for you.
type Approval struct {
	ID string `json:"id"`
	// The tool asking ("Bash", "Edit", "mcp__plugin_wtf_wtf__type_at_prompt").
	Tool string `json:"tool"`
	// The command it would run, or the file it would change, redacted.
	What string `json:"what"`
	// Claude Code's description of a command, redacted.
	Description string `json:"description"`
	// An edit: the lines it removes ("- ") and adds ("+ "), redacted and
	// cut at maxDiffLines.
	Diff string `json:"diff"`
	// Where the command would act (#40), one line per program, e.g.
	// "kubectl: context prod-eks (from --context), namespace payments".
	Facts []string `json:"facts"`
	// When it asked (Unix milliseconds).
	Since int64 `json:"since"`
	// Asked from inside a sub-agent: its type ("" for the agent itself).
	Subagent string `json:"subagent"`
}

const (
	maxDiffLines = 40
	maxWhat      = 2000
	// How long working out the facts may wait for the UI's pane request.
	factsTimeout = 3 * time.Second
)

// Answers from the inbox.
const (
	Allow = "allow"
	Deny  = "deny"
)

// waiter is a hook connection waiting for its answer.
type waiter struct {
	session, agentID, tool, key string
	ch                          chan string
}

// approvalKey is what tells a tool call apart from another of the same
// tool: its command or file.
func approvalKey(tool string, in *wire.ToolInput) string {
	if in == nil {
		return ""
	}
	switch {
	case in.Command != "":
		return in.Command
	case in.FilePath != "":
		return in.FilePath
	case len(in.Argv) > 0:
		return strings.Join(in.Argv, "\x00")
	}
	return in.Path
}

// awaitApproval lists a PermissionRequest in the inbox and waits until you
// answer it there (the decision), it's answered in the pane, or the hook
// goes (ctx: Claude Code killed it, or it timed out).
func (s *Service) awaitApproval(ctx context.Context, req wire.Request) wire.Response {
	ap := s.approvalOf(ctx, req)
	w := &waiter{session: req.Pane, agentID: req.AgentID, tool: req.Tool, key: approvalKey(req.Tool, req.Input), ch: make(chan string, 1)}
	msg := firstOf(strings.TrimSpace(req.Message), req.Tool)
	if req.Input != nil && req.Input.Command != "" {
		msg = req.Tool + ": " + req.Input.Command
	}
	// Waiting, with the approval, in one status: its notification can say
	// what it asks and where it would act.
	s.setState(req.Pane, req.Provider, StateWaiting, shortLine(msg, maxPrompt), "", false, 0)
	s.mu.Lock()
	a := s.agents[req.Pane]
	if a == nil || a.State != StateWaiting {
		s.mu.Unlock()
		return wire.Response{}
	}
	ap.ID = "a" + strconv.FormatUint(s.seq.Add(1), 10)
	s.waits[ap.ID] = w
	a.Approvals = append(a.Approvals, ap)
	st := a.clone()
	s.mu.Unlock()
	events.Emit(EventStatus, st)

	var decision string
	gone := false
	select {
	case decision = <-w.ch:
	case <-ctx.Done():
		gone = true
	}
	s.dropApproval(ap.ID)
	// What it does next, when no hook says: allowed, it works; denied in
	// the inbox or the pane (which kills the hook), the turn stops there
	// and no Stop hook comes.
	switch {
	case decision == Allow:
		s.afterApproval(req.Pane, StateBusy)
	case decision == Deny || gone:
		s.afterApproval(req.Pane, StateIdle)
	}
	return wire.Response{Decision: decision}
}

// afterApproval moves an agent on from waiting once nothing is left to
// approve.
func (s *Service) afterApproval(session, state string) {
	s.mu.Lock()
	a := s.agents[session]
	if a == nil || a.State != StateWaiting || len(a.Approvals) > 0 {
		s.mu.Unlock()
		return
	}
	a.State, a.Message, a.Since = state, "", time.Now().UnixMilli()
	st := a.clone()
	s.mu.Unlock()
	events.Emit(EventStatus, st)
}

// approvalOf is what the inbox shows of a request: the command or file,
// the change, and where a command would act.
func (s *Service) approvalOf(ctx context.Context, req wire.Request) Approval {
	ap := Approval{Tool: req.Tool, Since: time.Now().UnixMilli(), Subagent: req.AgentType}
	if req.AgentID != "" && ap.Subagent == "" {
		ap.Subagent = "sub-agent"
	}
	in := req.Input
	if in == nil {
		return ap
	}
	command := in.Command
	if len(in.Argv) > 0 {
		command = joinArgv(in.Argv)
	}
	ap.What = clipText(uicore.Redact(firstOf(command, in.FilePath, in.Path, in.URL, in.Query, in.Pattern)), maxWhat)
	ap.Description = shortLine(in.Description, maxPrompt)
	ap.Diff = diffOf(in)
	if command != "" {
		ap.Facts = s.factsOf(ctx, req, command)
	}
	return ap
}

// factsOf works out where command would act (#40). Claude Code's own Bash
// runs with the environment its shell gave it, in the folder it works in;
// wtf's type_at_prompt and run_readonly run in the shell an Ask Claude
// pane works on.
func (s *Service) factsOf(ctx context.Context, req wire.Request, command string) []string {
	session, cwd := req.Pane, req.Cwd
	if strings.HasPrefix(req.Tool, "mcp__") {
		session, cwd = s.target(req.Pane), ""
	}
	ctx, cancel := context.WithTimeout(ctx, factsTimeout)
	defer cancel()
	p, err := s.pane(ctx, session)
	if err != nil {
		return []string{"wtf can't read this shell's context: " + err.Error()}
	}
	if cwd == "" {
		cwd = p.Cwd
	}
	var r agentctx.Resolver
	if s.infra != nil {
		r = s.infra
	} else {
		return nil
	}
	lines := agentctx.Lines(agentctx.Of(command, agentctx.Shell{Env: p.Env, Cwd: cwd}, r))
	for i, l := range lines {
		lines[i] = uicore.Redact(l)
	}
	return lines
}

// Facts answers the "facts" op: where a command would act, as lines, for
// the reason Ask Claude's PreToolUse hook shows in Claude Code's dialog.
func (s *Service) facts(ctx context.Context, req wire.Request) string {
	if req.Input == nil {
		return ""
	}
	command := req.Input.Command
	if len(req.Input.Argv) > 0 {
		command = joinArgv(req.Input.Argv)
	}
	if command == "" {
		return ""
	}
	return strings.Join(s.factsOf(ctx, req, command), "\n")
}

// dropApproval takes an approval off its agent's status.
func (s *Service) dropApproval(id string) {
	s.mu.Lock()
	w := s.waits[id]
	delete(s.waits, id)
	if w == nil {
		s.mu.Unlock()
		return
	}
	a := s.agents[w.session]
	if a == nil {
		s.mu.Unlock()
		return
	}
	i := slices.IndexFunc(a.Approvals, func(ap Approval) bool { return ap.ID == id })
	if i < 0 {
		s.mu.Unlock()
		return
	}
	a.Approvals = slices.Delete(a.Approvals, i, i+1)
	st := a.clone()
	s.mu.Unlock()
	events.Emit(EventStatus, st)
}

// settleApprovals clears the approvals a hook event shows were answered in
// the pane: the tool ran (PostToolUse, PostToolUseFailure) or the turn
// ended, which leaves sub-agents' own prompts be; SessionEnd clears all.
func (s *Service) settleApprovals(req wire.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := approvalKey(req.Tool, req.Input)
	for _, w := range s.waits {
		if w.session != req.Pane {
			continue
		}
		var done bool
		switch req.Event {
		case "PostToolUse", "PostToolUseFailure":
			done = w.agentID == req.AgentID && w.tool == req.Tool && w.key == key
		case "Stop", "UserPromptSubmit", "SessionStart":
			done = w.agentID == ""
		case "SessionEnd":
			done = true
		}
		if done {
			select {
			case w.ch <- "":
			default:
			}
		}
	}
}

// Answer answers an approval from the inbox: Allow or Deny. It fails when
// it was answered already (in the pane, or it's gone).
func (s *Service) Answer(id, decision string) error {
	if decision != Allow && decision != Deny {
		return errors.New("an approval is answered allow or deny")
	}
	s.mu.Lock()
	w := s.waits[id]
	s.mu.Unlock()
	if w == nil {
		return errors.New("it was answered already")
	}
	select {
	case w.ch <- decision:
		return nil
	default:
		return errors.New("it was answered already")
	}
}

// diffOf is an edit's change: the lines removed and added.
func diffOf(in *wire.ToolInput) string {
	var lines []string
	add := func(prefix, text string) {
		if text == "" {
			return
		}
		for _, l := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
			lines = append(lines, prefix+l)
		}
	}
	add("- ", in.OldString)
	add("+ ", in.NewString)
	add("+ ", in.Content)
	for _, e := range in.Edits {
		add("- ", e.OldString)
		add("+ ", e.NewString)
	}
	if len(lines) == 0 {
		return ""
	}
	if len(lines) > maxDiffLines {
		more := len(lines) - maxDiffLines
		lines = append(lines[:maxDiffLines], "… "+strconv.Itoa(more)+" more lines")
	}
	return uicore.Redact(strings.Join(lines, "\n"))
}

// joinArgv shows argv as a command line, quoting what needs it.
func joinArgv(argv []string) string {
	out := make([]string, len(argv))
	for i, a := range argv {
		if a == "" || strings.ContainsAny(a, " \t\n'\"\\$`|&;<>()*?[]{}~#!") {
			out[i] = shellQuote(a)
		} else {
			out[i] = a
		}
	}
	return strings.Join(out, " ")
}

func clipText(s string, max int) string {
	if r := []rune(s); len(r) > max {
		return string(r[:max-1]) + "…"
	}
	return s
}

func firstOf(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

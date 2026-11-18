package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dayanraj/wtf/internal/agentctx"
	"github.com/dayanraj/wtf/internal/bridge/wire"
	"github.com/dayanraj/wtf/internal/events"
	"github.com/dayanraj/wtf/internal/infra"
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
// leaves the hook running, so the approval would stay until the tool's
// PostToolUse (or PostToolUseFailure) came, or the turn ended. So the key
// that answers its dialog, typed in that pane, takes it off the list at
// once (Typed); its hook waits on, so a No still ends the turn.

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
	// An AskUserQuestion: what Claude asks you, answered with an option
	// (AnswerQuestion) rather than Allow or Deny.
	Questions []Question `json:"questions"`
	// "Yes, and don't ask again": what each choice would save, e.g.
	// "Always allow Bash(npm test:*) in this project" (AllowAlways).
	Always []string `json:"always"`
	// Claude's questions: each can be answered in your own words too
	// (AnswerText for one, AnswerAll for several).
	FreeText bool `json:"freeText"`
	// Several questions, or several choices: answered all at once with a
	// form on the card (AnswerAll).
	Form bool `json:"form"`
}

// Pick is your answer to one of a request's questions in its form: the
// options chosen (one, unless it takes several), or your own words.
type Pick struct {
	Options []string `json:"options"`
	Text    string   `json:"text"`
}

// Question is one of the questions Claude asks you.
type Question struct {
	Question string   `json:"question"`
	Header   string   `json:"header"`
	Options  []string `json:"options"`
	// More than one option may be chosen: wtf leaves that to the pane.
	Multi bool `json:"multi"`
}

// Clickable: every question can be answered with one option click (one
// question, one choice); otherwise it's answered in the pane.
func (ap Approval) Clickable() bool {
	return len(ap.Questions) == 1 && !ap.Questions[0].Multi && len(ap.Questions[0].Options) > 0
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

// waiter is a hook connection waiting for its answer. A Codex approval
// has none: Codex shows its own prompt only once the hook has returned, so
// its hook doesn't wait, and the inbox answers by pressing the prompt's
// keys in its pane (y, Esc).
type waiter struct {
	session, agentID, tool, key string
	ch                          chan answer
	// An AskUserQuestion's questions and options as Claude Code sent them
	// (the inbox shows them redacted): what its answers must name.
	questions []wire.Question
	// The permission_suggestions behind each of the Approval's Always (for
	// Codex: none, its key is a).
	always []wire.Suggestion
	codex  bool
	// Answered by a key typed in its pane: off the list, but the hook
	// still waits (Claude Code kills it on No).
	inPane bool
}

// answer is what the inbox tells a waiting hook: "" (answered in the
// pane), allow or deny, and a question's answers.
type answer struct {
	decision string
	answers  map[string]string
	// Codex: the keys that answer its question (each option's number).
	keys []string
	// Allowed for good: the suggestions Claude Code saves.
	permissions []wire.Suggestion
	// Denied with what to do instead.
	message string
}

// Codex's approval prompt: y is "Yes, proceed", Esc is "No, and tell Codex
// what to do differently" (Codex 0.167).
const (
	codexAllowKey = "y"
	codexDenyKey  = "\x1b"
)

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
	w := &waiter{session: req.Pane, agentID: req.AgentID, tool: req.Tool, key: approvalKey(req.Tool, req.Input), ch: make(chan answer, 1),
		codex: req.Provider == "codex"}
	if isQuestion(req.Tool) && req.Input != nil {
		w.questions = req.Input.Questions
	}
	switch {
	case w.codex && !isQuestion(req.Tool):
		ap.Always = []string{codexAlways(req.Tool)}
	case !w.codex:
		for _, sg := range req.Suggestions {
			if label := suggestionLabel(sg); label != "" {
				ap.Always = append(ap.Always, label)
				w.always = append(w.always, sg)
			}
		}
		ap.FreeText = len(ap.Questions) > 0
	}
	ap.Form = formable(ap, w.codex)
	msg := firstOf(strings.TrimSpace(req.Message), req.Tool)
	if req.Input != nil && req.Input.Command != "" {
		msg = req.Tool + ": " + req.Input.Command
	}
	if len(ap.Questions) > 0 {
		msg = questionLine(ap.Questions)
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
	s.asking.Add(1)
	a.Approvals = append(a.Approvals, ap)
	st := a.clone()
	s.mu.Unlock()
	events.Emit(EventStatus, st)
	if pendingAccount(ap.Facts) {
		go s.refreshFacts(ap.ID, req)
	}
	if w.codex {
		// Codex's prompt shows once this returns; its events clear it.
		return wire.Response{}
	}

	var got answer
	gone := false
	select {
	case got = <-w.ch:
	case <-ctx.Done():
		gone = true
	}
	decision := got.decision
	s.mu.Lock()
	inPane := w.inPane
	s.mu.Unlock()
	s.dropApproval(ap.ID)
	// What it does next, when no hook says: allowed, it works; denied in
	// the inbox or the pane (which kills the hook), the turn stops there
	// and no Stop hook comes, even after a key in the pane showed it working.
	switch {
	case decision == Allow:
		s.afterApproval(req.Pane, StateBusy, false)
	case decision == Deny || gone:
		s.afterApproval(req.Pane, StateIdle, inPane)
	}
	return wire.Response{Decision: decision, Answers: got.answers, Permissions: got.permissions, Message: got.message}
}

// Claude Code's multiple-choice question to you comes through the
// PermissionRequest hook too. Allow alone doesn't answer it: the hook
// hands back the option chosen (checked with Claude Code 2.1.326).
const askUserQuestion = "AskUserQuestion"

// Codex's question to you (request_user_input) has no permission hook: its
// PreToolUse (scoped to it by the shell integration) lists it, and the
// inbox answers by pressing the option's number in its pane, which picks
// it and moves on to the next question (checked in Codex's source,
// 2026-11-16).
const codexQuestion = "request_user_input"

func isQuestion(tool string) bool { return tool == askUserQuestion || tool == codexQuestion }

// questionsOf: what an AskUserQuestion asks, redacted.
func questionsOf(in *wire.ToolInput) []Question {
	var qs []Question
	for _, q := range in.Questions {
		x := Question{Question: shortLine(uicore.Redact(q.Question), maxWhat), Header: uicore.Redact(q.Header), Multi: q.MultiSelect}
		for _, o := range q.Options {
			x.Options = append(x.Options, o.Label)
		}
		qs = append(qs, x)
	}
	return qs
}

// questionLine is the agent's message while it asks: its first question.
func questionLine(qs []Question) string {
	msg := qs[0].Question
	if n := len(qs) - 1; n > 0 {
		msg += " (and " + strconv.Itoa(n) + " more)"
	}
	return shortLine(msg, maxPrompt)
}

// afterApproval moves an agent on from waiting once nothing is left to
// approve (or from working, after a key in its pane answered it).
func (s *Service) afterApproval(session, state string, fromBusy bool) {
	s.mu.Lock()
	a := s.agents[session]
	if a == nil || len(a.Approvals) > 0 || !(a.State == StateWaiting || fromBusy && a.State == StateBusy) {
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
	if isQuestion(req.Tool) {
		ap.Questions = questionsOf(in)
		return ap
	}
	command := in.Command
	if len(in.Argv) > 0 {
		command = joinArgv(in.Argv)
	}
	ap.What = clipText(uicore.Redact(firstOf(command, in.FilePath, in.Path, in.URL, in.Query, in.Pattern)), maxWhat)
	ap.Description = shortLine(in.Description, maxPrompt)
	ap.Diff = diffOf(in)
	if req.Tool == "apply_patch" {
		// Codex's file edits: the patch is the command.
		ap.What, ap.Diff = patchOf(in.Command)
		return ap
	}
	if command != "" {
		ap.Facts = s.factsOf(ctx, req, command)
	}
	return ap
}

// patchOf reads a Codex apply_patch: the files it touches, and its lines
// removed and added.
func patchOf(patch string) (files, diff string) {
	var names, lines []string
	for _, l := range strings.Split(patch, "\n") {
		for _, p := range []string{"*** Add File: ", "*** Update File: ", "*** Delete File: ", "*** Move to: "} {
			if f, ok := strings.CutPrefix(l, p); ok {
				names = append(names, strings.TrimSpace(f))
			}
		}
		switch {
		case strings.HasPrefix(l, "***") || strings.HasPrefix(l, "@@"):
		case strings.HasPrefix(l, "+"):
			lines = append(lines, "+ "+l[1:])
		case strings.HasPrefix(l, "-"):
			lines = append(lines, "- "+l[1:])
		}
	}
	if len(lines) > maxDiffLines {
		more := len(lines) - maxDiffLines
		lines = append(lines[:maxDiffLines], "… "+strconv.Itoa(more)+" more lines")
	}
	return uicore.Redact(strings.Join(names, ", ")), uicore.Redact(strings.Join(lines, "\n"))
}

// pendingAccount: a fact waits on the AWS account lookup the badges share.
func pendingAccount(facts []string) bool {
	for _, f := range facts {
		if strings.Contains(f, infra.AccountPending) {
			return true
		}
	}
	return false
}

// refreshFacts works an approval's facts out again while the AWS account
// lookup they wait on runs (it's in the background, at most a few seconds),
// until it's answered or the approval goes.
func (s *Service) refreshFacts(id string, req wire.Request) {
	for i := 0; i < 40; i++ {
		time.Sleep(500 * time.Millisecond)
		s.mu.Lock()
		_, ok := s.waits[id]
		s.mu.Unlock()
		if !ok {
			return
		}
		command := req.Input.Command
		if len(req.Input.Argv) > 0 {
			command = joinArgv(req.Input.Argv)
		}
		facts := s.factsOf(context.Background(), req, command)
		if pendingAccount(facts) {
			continue
		}
		s.mu.Lock()
		w := s.waits[id]
		if w == nil || s.agents[w.session] == nil {
			s.mu.Unlock()
			return
		}
		a := s.agents[w.session]
		at := slices.IndexFunc(a.Approvals, func(ap Approval) bool { return ap.ID == id })
		if at < 0 {
			s.mu.Unlock()
			return
		}
		a.Approvals[at].Facts = facts
		st := a.clone()
		s.mu.Unlock()
		events.Emit(EventStatus, st)
		return
	}
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
	if w == nil {
		s.mu.Unlock()
		return
	}
	delete(s.waits, id)
	s.asking.Add(-1)
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
// ended (Codex: Interrupt, its No), which leaves sub-agents' own prompts
// be; SessionEnd clears all.
func (s *Service) settleApprovals(req wire.Request) {
	s.mu.Lock()
	key := approvalKey(req.Tool, req.Input)
	var drop []string
	defer func() {
		s.mu.Unlock()
		for _, id := range drop {
			s.dropApproval(id)
		}
	}()
	for id, w := range s.waits {
		if w.session != req.Pane {
			continue
		}
		var done bool
		switch req.Event {
		case "PostToolUse", "PostToolUseFailure":
			done = w.agentID == req.AgentID && w.tool == req.Tool && w.key == key
		case "Stop", "UserPromptSubmit", "SessionStart", "Interrupt":
			done = w.agentID == ""
		case "SessionEnd":
			done = true
		}
		if done && w.codex {
			drop = append(drop, id)
		} else if done {
			select {
			case w.ch <- answer{}:
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
	question := w != nil && len(w.questions) > 0
	s.mu.Unlock()
	if question && decision == Allow {
		// Allow alone leaves Claude Code's question waiting.
		return errors.New("a question is answered with one of its options")
	}
	return s.answer(id, answer{decision: decision})
}

// AnswerQuestion answers a question Claude asks (an AskUserQuestion) from
// the inbox with an option, one for each of its questions, in order. It
// fails when it was answered already, or an option isn't one it offers.
func (s *Service) AnswerQuestion(id string, options []string) error {
	s.mu.Lock()
	w := s.waits[id]
	if w == nil || w.inPane || len(w.questions) == 0 {
		s.mu.Unlock()
		return errors.New("it was answered already")
	}
	qs := w.questions
	s.mu.Unlock()
	if len(options) != len(qs) {
		return errors.New("one option for each question")
	}
	answers := map[string]string{}
	var keys []string
	for i, q := range qs {
		at := slices.IndexFunc(q.Options, func(o wire.Option) bool { return o.Label == options[i] })
		if at < 0 {
			return fmt.Errorf("%q isn't one of its options", options[i])
		}
		answers[q.Question] = options[i]
		keys = append(keys, strconv.Itoa(at+1))
	}
	if w.codex {
		return s.answer(id, answer{decision: Allow, keys: keys})
	}
	return s.answer(id, answer{decision: Allow, answers: answers})
}

// AllowAlways allows an approval and saves its choice'th "don't ask again"
// (one of the Approval's Always); for Codex, it presses a.
func (s *Service) AllowAlways(id string, choice int) error {
	s.mu.Lock()
	w := s.waits[id]
	var sg wire.Suggestion
	ok := w != nil && (w.codex && choice == 0 && len(w.questions) == 0 || choice >= 0 && choice < len(w.always))
	if ok && !w.codex {
		sg = w.always[choice]
	}
	s.mu.Unlock()
	switch {
	case w == nil:
		return errors.New("it was answered already")
	case !ok:
		return errors.New("it has no such choice")
	case w.codex:
		return s.answer(id, answer{decision: Allow, keys: []string{codexAlwaysKey}})
	}
	return s.answer(id, answer{decision: Allow, permissions: []wire.Suggestion{sg}})
}

// DenyWith denies an approval and tells the agent what to do instead,
// which it carries on with: Claude gets it as the reason; Codex's No (Esc)
// ends its turn, so the text is typed into its prompt and sent.
func (s *Service) DenyWith(id, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("say what to do instead")
	}
	if len(text) > maxTold {
		return errors.New("that's too long")
	}
	s.mu.Lock()
	w := s.waits[id]
	codex := w != nil && w.codex
	s.mu.Unlock()
	if codex {
		// One line: Enter in Codex's prompt sends it.
		line := strings.Join(strings.Fields(text), " ")
		return s.answer(id, answer{decision: Deny, keys: []string{codexDenyKey, pause, line, pause, "\r"}})
	}
	return s.answer(id, answer{decision: Deny, message: text})
}

// AnswerText answers a question Claude asks in your own words (its
// "Type something"), when the Approval says FreeText.
func (s *Service) AnswerText(id, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("type an answer")
	}
	if len(text) > maxTold {
		return errors.New("that's too long")
	}
	s.mu.Lock()
	w := s.waits[id]
	var q string
	free := w != nil && !w.codex && !w.inPane && len(w.questions) == 1 && !w.questions[0].MultiSelect
	if free {
		q = w.questions[0].Question
	}
	s.mu.Unlock()
	if w == nil {
		return errors.New("it was answered already")
	}
	if !free {
		return errors.New("this question is answered in the agent's pane")
	}
	return s.answer(id, answer{decision: Allow, answers: map[string]string{q: text}})
}

// formable: questions the card's form answers, all at once, that one
// click can't: several, or several choices. Codex picks an option per
// question by its number, so each needs options, one choice.
func formable(ap Approval, codex bool) bool {
	if len(ap.Questions) == 0 || ap.Clickable() {
		return false
	}
	if codex {
		for _, q := range ap.Questions {
			if q.Multi || len(q.Options) == 0 {
				return false
			}
		}
	}
	return true
}

// AnswerAll answers a request's questions all at once (its form): a Pick
// for each, in order. Several choices go to Claude as one answer, "Apple,
// Banana", as its own dialog sends them (checked with Claude Code 2.1.326).
func (s *Service) AnswerAll(id string, picks []Pick) error {
	s.mu.Lock()
	w := s.waits[id]
	if w == nil || w.inPane || len(w.questions) == 0 {
		s.mu.Unlock()
		return errors.New("it was answered already")
	}
	qs, codex := w.questions, w.codex
	s.mu.Unlock()
	if len(picks) != len(qs) {
		return errors.New("one answer for each question")
	}
	answers := map[string]string{}
	var keys []string
	for i, q := range qs {
		p := picks[i]
		if text := strings.TrimSpace(p.Text); text != "" {
			if codex {
				return errors.New("Codex's questions are answered with their options")
			}
			if len(text) > maxTold {
				return errors.New("that's too long")
			}
			answers[q.Question] = text
			continue
		}
		if len(p.Options) == 0 || len(p.Options) > 1 && !q.MultiSelect {
			return fmt.Errorf("answer %q", firstOf(q.Header, q.Question))
		}
		for _, o := range p.Options {
			at := slices.IndexFunc(q.Options, func(x wire.Option) bool { return x.Label == o })
			if at < 0 {
				return fmt.Errorf("%q isn't one of its options", o)
			}
			keys = append(keys, strconv.Itoa(at+1))
		}
		answers[q.Question] = strings.Join(p.Options, ", ")
	}
	if codex {
		return s.answer(id, answer{decision: Allow, keys: keys})
	}
	return s.answer(id, answer{decision: Allow, answers: answers})
}

// maxTold is how long what you tell an agent may be.
const maxTold = 4000

// pause, among an answer's keys, waits a moment: Codex's prompt shows
// once its approval closes, and text typed all at once reads as a paste
// that an Enter right after would only add a line to.
const pause = "\x00pause"

// Codex's "Yes, and don't ask again for this command in this session" (for
// an edit: these files), 0.167.
const codexAlwaysKey = "a"

func codexAlways(tool string) string {
	if tool == "apply_patch" {
		return "Don't ask again for these files"
	}
	return "Don't ask again for this command this session"
}

// suggestionLabel says what a permission_suggestion saves, "" for one
// that isn't an allow (wtf offers only those).
func suggestionLabel(sg wire.Suggestion) string {
	var v struct {
		Type  string `json:"type"`
		Rules []struct {
			Tool    string `json:"toolName"`
			Content string `json:"ruleContent"`
		} `json:"rules"`
		Behavior    string   `json:"behavior"`
		Mode        string   `json:"mode"`
		Directories []string `json:"directories"`
		Destination string   `json:"destination"`
	}
	if json.Unmarshal(sg, &v) != nil {
		return ""
	}
	where := map[string]string{"localSettings": " in this project", "projectSettings": " in this project, for everyone",
		"userSettings": " in every project", "session": " this session", "cliArg": " this session"}[v.Destination]
	var label string
	switch v.Type {
	case "addRules", "replaceRules":
		if v.Behavior != "allow" || len(v.Rules) == 0 {
			return ""
		}
		var rules []string
		for _, r := range v.Rules {
			if r.Content == "" {
				rules = append(rules, r.Tool)
			} else {
				rules = append(rules, r.Tool+"("+r.Content+")")
			}
		}
		label = "Always allow " + strings.Join(rules, ", ") + where
	case "setMode":
		switch v.Mode {
		case "acceptEdits":
			label = "Accept all edits" + where
		case "":
			return ""
		default:
			label = "Switch to " + v.Mode + " mode" + where
		}
	case "addDirectories":
		if len(v.Directories) == 0 {
			return ""
		}
		label = "Always allow access to " + strings.Join(v.Directories, ", ") + where
	default:
		return ""
	}
	return shortLine(uicore.Redact(label), maxPrompt)
}

func (s *Service) answer(id string, a answer) error {
	decision := a.decision
	s.mu.Lock()
	w := s.waits[id]
	answered := w == nil || w.inPane
	s.mu.Unlock()
	if answered {
		return errors.New("it was answered already")
	}
	if w.codex {
		// Codex's own prompt is showing: press its keys, one at a time.
		keys := a.keys
		if len(keys) == 0 {
			keys = []string{codexAllowKey}
			if decision == Deny {
				keys = []string{codexDenyKey}
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
		defer cancel()
		for _, k := range keys {
			if k == pause {
				time.Sleep(300 * time.Millisecond)
				continue
			}
			if _, err := s.ask(ctx, UIRequest{Kind: KindKeys, Session: w.session, Text: k}); err != nil {
				return err
			}
		}
		s.dropApproval(id)
		if decision == Allow {
			s.afterApproval(w.session, StateBusy, false)
		}
		return nil
	}
	select {
	case w.ch <- a:
		return nil
	default:
		return errors.New("it was answered already")
	}
}

// Typed hears every write to a shell (the terminal service's OnInput). A
// key that answers the agent's own permission dialog there takes the
// request the dialog shows (the oldest) off the list, so the inbox and its
// notification let go now rather than when the tool finishes.
func (s *Service) Typed(session string, p []byte) {
	if s.asking.Load() == 0 || len(p) != 1 {
		return
	}
	k := p[0]
	s.mu.Lock()
	a := s.agents[session]
	if a == nil || len(a.Approvals) == 0 {
		s.mu.Unlock()
		return
	}
	id := a.Approvals[0].ID
	w := s.waits[id]
	// Codex's letters answer its approval prompt; on its question screen
	// they start a note.
	codexKeys := w != nil && w.codex && len(w.questions) == 0
	if w == nil || !answersDialog(k, codexKeys) {
		s.mu.Unlock()
		return
	}
	// Esc (and Codex's n) is No: the turn ends on its own event.
	no := k == 0x1b || codexKeys && k == 'n'
	if w.codex {
		s.mu.Unlock()
		s.dropApproval(id)
		if !no {
			s.afterApproval(session, StateBusy, false)
		}
		return
	}
	w.inPane = true
	a.Approvals = slices.Delete(a.Approvals, 0, 1)
	if len(a.Approvals) == 0 && a.State == StateWaiting && !no {
		a.State, a.Message, a.Since = StateBusy, "", time.Now().UnixMilli()
	}
	st := a.clone()
	s.mu.Unlock()
	events.Emit(EventStatus, st)
}

// answersDialog: a key that answers an agent's permission dialog or
// question: Enter (its highlighted choice), a choice's number, or Esc (No);
// Codex's approval prompt also takes y, a and n (0.167).
func answersDialog(k byte, codex bool) bool {
	switch {
	case k == '\r' || k == '\n' || k == 0x1b || k >= '1' && k <= '9':
		return true
	case codex:
		return k == 'y' || k == 'a' || k == 'n'
	}
	return false
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

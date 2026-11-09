package bridge

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dayanraj/wtf/internal/bridge/wire"
	"github.com/dayanraj/wtf/internal/cmdhistory"
	"github.com/dayanraj/wtf/internal/events"
	"github.com/dayanraj/wtf/internal/snippets"
)

// fakeUI answers the bridge's requests the way the native UIs do, for one
// shell, and records what was typed into it.
type fakeUI struct {
	mu      sync.Mutex
	s       *Service
	pane    Pane
	blocks  []Block
	typed   []string
	ran     []string
	gone    bool
	runOut  string
	nextBlk int
	status  []Status
	drafts  []snippets.Snippet
}

func newFake(t *testing.T) *fakeUI {
	t.Helper()
	s := NewService(nil)
	if err := s.startup(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.shutdown)
	f := &fakeUI{s: s, nextBlk: 100,
		pane: Pane{Title: "shop", Cwd: "/tmp", Env: map[string]string{"PWD": "/tmp", "PATH": "/usr/bin:/bin"}, Integrated: true}}
	events.SetEmitter(func(name string, data any) {
		if name == EventStatus {
			f.mu.Lock()
			f.status = append(f.status, data.(Status))
			f.mu.Unlock()
		}
		if name != EventRequest {
			return
		}
		req := data.(UIRequest)
		go f.s.Reply(req.ID, f.answer(req))
	})
	t.Cleanup(func() { events.SetEmitter(nil) })
	return f
}

func (f *fakeUI) answer(req UIRequest) Reply {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.gone || req.Session != "s1" {
		return Reply{Error: "that shell is gone"}
	}
	switch req.Kind {
	case KindPane:
		p := f.pane
		return Reply{Pane: &p}
	case KindBlocks:
		n := min(req.Count, len(f.blocks))
		return Reply{Blocks: slices.Clone(f.blocks[len(f.blocks)-n:])}
	case KindType:
		if f.pane.Running {
			return Reply{Error: "The shell is busy"}
		}
		f.typed = append(f.typed, req.Text)
		return Reply{}
	case KindRun:
		f.ran = append(f.ran, req.Text)
		f.nextBlk++
		b := Block{ID: f.nextBlk, Command: req.Text, Output: f.runOut}
		f.blocks = append(f.blocks, b)
		return Reply{Block: &b}
	case KindSnippet:
		f.drafts = append(f.drafts, *req.Snippet)
		return Reply{}
	}
	return Reply{Error: "unknown"}
}

func call(t *testing.T, s *Service, pane, token, tool string, args any) wire.Response {
	t.Helper()
	raw, _ := json.Marshal(args)
	return s.do(context.Background(), wire.Request{V: wire.Version, Pane: pane, Token: token, Op: "call", Tool: tool, Args: raw})
}

func TestTokens(t *testing.T) {
	f := newFake(t)
	s := f.s
	env := map[string]string{}
	for _, kv := range s.shellEnv("s1") {
		k, v, _ := strings.Cut(kv, "=")
		env[k] = v
	}
	if env[wire.EnvPane] != "s1" || env[wire.EnvBridge] == "" || env[wire.EnvToken] == "" {
		t.Fatalf("env = %v", env)
	}
	if fi, err := os.Stat(filepath.Dir(env[wire.EnvBridge])); err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("socket folder: %v %v", fi.Mode(), err)
	}
	ok := s.do(context.Background(), wire.Request{V: wire.Version, Pane: "s1", Token: env[wire.EnvToken], Op: "list"})
	if ok.Error != "" || len(ok.Tools) != len(Tools) {
		t.Fatalf("list = %+v", ok)
	}
	// Another shell's token, a wrong one, none, an old helper.
	for _, req := range []wire.Request{
		{V: wire.Version, Pane: "s2", Token: env[wire.EnvToken], Op: "list"},
		{V: wire.Version, Pane: "s1", Token: strings.Repeat("0", 64), Op: "list"},
		{V: wire.Version, Pane: "s1", Op: "list"},
		{V: wire.Version + 1, Pane: "s1", Token: env[wire.EnvToken], Op: "list"},
	} {
		if r := s.do(context.Background(), req); r.Error == "" {
			t.Errorf("%+v accepted", req)
		}
	}
}

func TestTools(t *testing.T) {
	f := newFake(t)
	s := f.s
	tok := s.token("s1")
	secret := "AKIAIOSFODNN7EXAMPLE"
	plan, _ := os.ReadFile("../review/testdata/terraform-plan.txt")
	code0, code1 := 0, 1
	f.blocks = []Block{
		{ID: 1, Command: "terraform plan", ExitCode: code0, Output: string(plan)},
		{ID: 2, Command: "kubectl get pods", ExitCode: code1, Output: "error: aws_access_key_id=" + secret},
	}

	r := call(t, s, "s1", tok, "pane_context", nil)
	if r.IsError || !strings.Contains(r.Text, "Folder: /tmp") || !strings.Contains(r.Text, "at its prompt") {
		t.Errorf("pane_context = %+v", r)
	}

	r = call(t, s, "s1", tok, "recent_commands", map[string]int{"count": 5})
	if r.IsError || !strings.Contains(r.Text, "[block 2] $ kubectl get pods") || !strings.Contains(r.Text, "Exit code: 1") {
		t.Errorf("recent_commands = %+v", r)
	}
	if strings.Contains(r.Text, secret) {
		t.Errorf("recent_commands leaked a secret: %s", r.Text)
	}

	r = call(t, s, "s1", tok, "review_changes", map[string]any{})
	if r.IsError || !strings.Contains(r.Text, "Tool: terraform") {
		t.Errorf("review_changes = %+v", r)
	}
	r = call(t, s, "s1", tok, "review_changes", map[string]any{"block_id": 2})
	if !r.IsError {
		t.Errorf("review_changes of kubectl get = %+v", r)
	}

	// log_patterns: the latest block with enough lines; secrets redacted.
	r = call(t, s, "s1", tok, "log_patterns", map[string]any{})
	if r.IsError || !strings.Contains(r.Text, "[block 1] $ terraform plan") || !strings.Contains(r.Text, "patterns (numbers, ids") {
		t.Errorf("log_patterns = %+v", r)
	}
	f.blocks = append(f.blocks, Block{ID: 3, Command: "kubectl logs api", ExitCode: -1,
		Output: strings.Repeat("2026-11-03T10:00:01Z ERROR token="+secret+" rejected for 10.0.0.1\n", 12)})
	r = call(t, s, "s1", tok, "log_patterns", map[string]any{})
	if r.IsError || !strings.Contains(r.Text, "[block 3] $ kubectl logs api") || !strings.Contains(r.Text, "still running") ||
		!strings.Contains(r.Text, "1. ×12 [error]") || strings.Contains(r.Text, secret) {
		t.Errorf("log_patterns of a running block = %+v", r)
	}
	if r = call(t, s, "s1", tok, "log_patterns", map[string]any{"block_id": 99}); !r.IsError {
		t.Errorf("log_patterns of a missing block = %+v", r)
	}
	f.blocks = f.blocks[:2]

	// session_commands: the last ones, or a range, with start times.
	f.blocks[0].StartedAt, f.blocks[0].DurationMs = "2026-11-03T10:00:00Z", 1500
	r = call(t, s, "s1", tok, "session_commands", map[string]any{})
	if r.IsError || !strings.Contains(r.Text, "[block 1] ") || !strings.Contains(r.Text, "(took 1.5s) $ terraform plan") ||
		!strings.Contains(r.Text, "first and last 12 lines") || !strings.Contains(r.Text, "[block 2] $ kubectl get pods") || strings.Contains(r.Text, secret) {
		t.Errorf("session_commands = %+v", r)
	}
	r = call(t, s, "s1", tok, "session_commands", map[string]any{"from_block": 2})
	if r.IsError || strings.Contains(r.Text, "[block 1]") || !strings.Contains(r.Text, "[block 2]") {
		t.Errorf("session_commands from 2 = %+v", r)
	}
	if r = call(t, s, "s1", tok, "session_commands", map[string]any{"from_block": 9}); !r.IsError {
		t.Errorf("session_commands from a missing block = %+v", r)
	}

	// open_snippet_editor: checked, then handed to the UI; nothing saved.
	for _, bad := range []map[string]any{{"command": "ls"}, {"name": "x"}, {"name": "x", "command": "ls", "steps": []map[string]string{{"command": "ls"}}},
		{"name": "x", "steps": []map[string]string{{"title": "empty"}}}} {
		if r = call(t, s, "s1", tok, "open_snippet_editor", bad); !r.IsError {
			t.Errorf("open_snippet_editor %v = %+v", bad, r)
		}
	}
	r = call(t, s, "s1", tok, "open_snippet_editor", map[string]any{"name": " Restart ", "description": "Rolls the pods",
		"steps":  []map[string]string{{"title": "Restart", "command": "kubectl -n {{ns=$kube_namespace}} rollout restart deploy/{{name}}"}},
		"params": []map[string]any{{"name": "name", "description": "The deployment"}}})
	if r.IsError || !strings.Contains(r.Text, "Nothing is saved") || len(f.drafts) != 1 || f.drafts[0].Name != "Restart" ||
		len(f.drafts[0].Steps) != 1 || len(f.drafts[0].Params) != 1 || f.drafts[0].Params[0].Description != "The deployment" {
		t.Errorf("open_snippet_editor = %+v, drafts %+v", r, f.drafts)
	}

	// Refused by the allowlist: nothing reaches the shell.
	r = call(t, s, "s1", tok, "run_readonly", map[string]any{"argv": []string{"kubectl", "delete", "pod", "x"}})
	if !r.IsError || !strings.Contains(r.Text, "refused") || len(f.ran) != 0 {
		t.Errorf("run_readonly delete = %+v, ran %v", r, f.ran)
	}
	r = call(t, s, "s1", tok, "run_readonly", map[string]any{"argv": []string{"sh", "-c", "rm -rf /"}})
	if !r.IsError || len(f.ran) != 0 {
		t.Errorf("run_readonly sh = %+v", r)
	}
	f.runOut = "NAME READY\nweb 1/1"
	r = call(t, s, "s1", tok, "run_readonly", map[string]any{"argv": []string{"kubectl", "logs", "web"}, "purpose": "see"})
	if r.IsError || len(f.ran) != 1 || !strings.Contains(f.ran[0], "--tail=200") || !strings.Contains(r.Text, "web 1/1") {
		t.Errorf("run_readonly logs = %+v, ran %v", r, f.ran)
	}

	r = call(t, s, "s1", tok, "type_at_prompt", map[string]string{"command": "kubectl rollout restart deploy/web\n"})
	if r.IsError || len(f.typed) != 1 || f.typed[0] != "kubectl rollout restart deploy/web" {
		t.Errorf("type_at_prompt = %+v, typed %q", r, f.typed)
	}
	r = call(t, s, "s1", tok, "type_at_prompt", map[string]string{"command": "ls\x1b[31m"})
	if !r.IsError {
		t.Errorf("type_at_prompt with escape = %+v", r)
	}
	f.pane.Running = true
	if r = call(t, s, "s1", tok, "type_at_prompt", map[string]string{"command": "ls"}); !r.IsError {
		t.Errorf("type_at_prompt while busy = %+v", r)
	}

	if r = call(t, s, "s1", tok, "nope", nil); !r.IsError {
		t.Errorf("unknown tool = %+v", r)
	}
}

func TestTarget(t *testing.T) {
	f := newFake(t)
	s := f.s
	// A Claude pane (s9) works on s1.
	if r := call(t, s, "s9", s.token("s9"), "pane_context", nil); !r.IsError {
		t.Fatalf("s9 without a target reached s1: %+v", r)
	}
	s.SetTarget("s9", "s1")
	if r := call(t, s, "s9", s.token("s9"), "pane_context", nil); r.IsError {
		t.Fatalf("s9 → s1 = %+v", r)
	}
	s.SetTarget("s9", "")
	if r := call(t, s, "s9", s.token("s9"), "pane_context", nil); !r.IsError {
		t.Fatalf("forgotten target still used: %+v", r)
	}
}

// TestHelper builds wtf-mcp and drives it as Claude Code does: over stdio
// with an MCP client, its environment pointing at the bridge.
func TestHelper(t *testing.T) {
	if testing.Short() {
		t.Skip("builds wtf-mcp")
	}
	f := newFake(t)
	f.runOut = "pong"
	bin := filepath.Join(t.TempDir(), "wtf-mcp")
	if out, err := exec.Command("go", "build", "-o", bin, "../../cmd/wtf-mcp").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), f.s.shellEnv("s1")...)
	ctx := context.Background()
	c := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	cs, err := c.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	list, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tl := range list.Tools {
		names = append(names, tl.Name)
	}
	if len(names) != len(Tools) || !slices.Contains(names, "run_readonly") {
		t.Fatalf("tools = %v", names)
	}
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "run_readonly", Arguments: map[string]any{"argv": []string{"ping", "example.com"}}})
	if err != nil {
		t.Fatal(err)
	}
	text := res.Content[0].(*mcp.TextContent).Text
	if res.IsError || !strings.Contains(text, "pong") || len(f.ran) != 1 || f.ran[0] != "ping -c 3 example.com" {
		t.Fatalf("run_readonly = %v %q, ran %v", res.IsError, text, f.ran)
	}
	res, err = cs.CallTool(ctx, &mcp.CallToolParams{Name: "run_readonly", Arguments: map[string]any{"argv": []string{"rm", "-rf", "/"}}})
	if err != nil || !res.IsError {
		t.Fatalf("rm = %v %v", res, err)
	}

	// Outside a wtf shell it says so and exits.
	out, err := exec.Command(bin).CombinedOutput()
	if err == nil || !strings.Contains(string(out), "not running in a wtf shell") {
		t.Fatalf("no env: %v %s", err, out)
	}
}

// TestFindings: the Findings tab's SARIF for the shell's repository, most
// severe first, with the code around each finding and filters.
func TestFindings(t *testing.T) {
	f := newFake(t)
	repo := t.TempDir()
	os.Mkdir(filepath.Join(repo, ".git"), 0o755)
	os.WriteFile(filepath.Join(repo, "db.go"), []byte("package db\n\nfunc q(id string) {\n\tdb.Query(\"select * from t where id=\" + id)\n}\n"), 0o644)
	sarif := `{"runs":[{"tool":{"driver":{"name":"Semgrep OSS","semanticVersion":"1.90.0","rules":[
	  {"id":"go.sqli","shortDescription":{"text":"SQL built from user input"},"defaultConfiguration":{"level":"error"}},
	  {"id":"go.style","shortDescription":{"text":"Odd name"},"defaultConfiguration":{"level":"note"}}]}},
	  "results":[
	    {"ruleId":"go.style","message":{"text":"rename q"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"db.go"},"region":{"startLine":3}}}]},
	    {"ruleId":"go.sqli","message":{"text":"Use a parameterized query"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"db.go"},"region":{"startLine":4,"startColumn":2}}}]}]}]}`
	os.WriteFile(filepath.Join(repo, "semgrep.sarif"), []byte(sarif), 0o644)
	sub := filepath.Join(repo, "sub")
	os.Mkdir(sub, 0o755)
	f.pane.Cwd, f.pane.Env["PWD"] = sub, sub
	tok := f.s.token("s1")

	r := call(t, f.s, "s1", tok, "findings", map[string]any{})
	sqli, style := strings.Index(r.Text, "#2 [error] Semgrep OSS go.sqli — SQL built from user input"), strings.Index(r.Text, "#1 [note]")
	if r.IsError || sqli < 0 || style < sqli || !strings.Contains(r.Text, "at db.go:4:2") ||
		!strings.Contains(r.Text, ">    4 | \tdb.Query(") || !strings.Contains(r.Text, "Semgrep OSS 1.90.0: 2 findings, in the folder") ||
		!strings.Contains(r.Text, "All: 2 (1 error, 1 note). Matching: 2.") {
		t.Fatalf("findings = %+v", r)
	}
	r = call(t, f.s, "s1", tok, "findings", map[string]any{"severity": "high"})
	if r.IsError || strings.Contains(r.Text, "go.style") || !strings.Contains(r.Text, "Matching: 1.") {
		t.Errorf("severity high = %+v", r)
	}
	r = call(t, f.s, "s1", tok, "findings", map[string]any{"rule": "STYLE", "path": "db"})
	if r.IsError || strings.Contains(r.Text, "go.sqli —") || !strings.Contains(r.Text, "#1 [note]") {
		t.Errorf("rule filter = %+v", r)
	}
	if r = call(t, f.s, "s1", tok, "findings", map[string]any{"severity": "bad"}); !r.IsError {
		t.Errorf("bad severity = %+v", r)
	}
	f.pane.Cwd, f.pane.Env["PWD"] = t.TempDir(), ""
	f.pane.Env["PWD"] = f.pane.Cwd
	if r = call(t, f.s, "s1", tok, "findings", map[string]any{}); r.IsError || !strings.Contains(r.Text, "No SARIF reports") {
		t.Errorf("no reports = %+v", r)
	}
}

// TestSearchHistory: the saved history, with its contexts, for the model.
func TestSearchHistory(t *testing.T) {
	f := newFake(t)
	tok := f.s.token("s1")
	if r := call(t, f.s, "s1", tok, "search_history", map[string]any{}); !r.IsError {
		t.Errorf("without a store = %+v", r)
	}
	h := cmdhistory.NewService(t.TempDir(), nil)
	f.s.History = h
	now := time.Now().UTC()
	for i, c := range []string{"aws rds modify-db-instance --db-instance-identifier main --ca-certificate-identifier rds-ca-rsa2048-g1", "ls", "aws rds describe-db-instances"} {
		if err := h.Record(cmdhistory.Record{Session: "s1", Command: c, Cwd: "/w", StartedAt: now.Add(time.Duration(i-40*24) * time.Hour).Format(time.RFC3339), DurationMs: 900}); err != nil {
			t.Fatal(err)
		}
	}
	r := call(t, f.s, "s1", tok, "search_history", map[string]any{"text": "rds cert"})
	if r.IsError || !strings.Contains(r.Text, "1 matching") || !strings.Contains(r.Text, "$ aws rds modify-db-instance") || !strings.Contains(r.Text, "keeps 90 days") {
		t.Errorf("search = %+v", r)
	}
	r = call(t, f.s, "s1", tok, "search_history", map[string]any{"text": "rds", "days": 7})
	if r.IsError || !strings.Contains(r.Text, "0 matching") {
		t.Errorf("last 7 days = %+v", r)
	}
	r = call(t, f.s, "s1", tok, "search_history", map[string]any{"limit": 1})
	if r.IsError || !strings.Contains(r.Text, "3 matching, the newest 1 shown") || !strings.Contains(r.Text, "describe-db-instances") {
		t.Errorf("limit = %+v", r)
	}
}

// TestAgents: the registry behind the Agents tab and the tab badges.
func TestAgents(t *testing.T) {
	f := newFake(t)
	s := f.s
	hook := func(pane, event, message, prompt string) {
		t.Helper()
		r := s.do(context.Background(), wire.Request{V: wire.Version, Pane: pane, Token: s.token(pane), Op: "hook", Event: event, Message: message, Prompt: prompt})
		if r.Error != "" {
			t.Fatal(r.Error)
		}
	}
	last := func() Status {
		t.Helper()
		f.mu.Lock()
		defer f.mu.Unlock()
		if len(f.status) == 0 {
			t.Fatal("no status")
		}
		return f.status[len(f.status)-1]
	}
	count := func() int {
		f.mu.Lock()
		defer f.mu.Unlock()
		return len(f.status)
	}

	// A Notification before it works (its idle reminder) adds nothing.
	hook("s2", "Notification", "Claude is waiting for your input", "")
	if count() != 0 || len(s.Agents()) != 0 {
		t.Fatalf("idle reminder made an agent: %v", s.Agents())
	}
	hook("s2", "SessionStart", "", "")
	if st := last(); st.State != StateIdle || st.Unseen || st.Since == 0 {
		t.Fatalf("start = %+v", st)
	}
	// The prompt: first line, redacted, shortened.
	secret := "AKIA" + "IOSFODNN7EXAMPLE"
	hook("s2", "UserPromptSubmit", "", "\n  fix the   deploy with "+secret+"\nmore lines")
	st := last()
	if st.State != StateBusy || strings.Contains(st.Prompt, secret) || !strings.HasPrefix(st.Prompt, "fix the deploy with ") || strings.Contains(st.Prompt, "more") {
		t.Fatalf("busy = %+v", st)
	}
	hook("s2", "UserPromptSubmit", "", strings.Repeat("x", 500))
	if p := []rune(last().Prompt); len(p) != maxPrompt || p[len(p)-1] != '…' {
		t.Fatalf("long prompt = %d runes", len(p))
	}
	n := count()
	hook("s2", "PostToolUse", "", "")
	if count() != n {
		t.Fatal("PostToolUse while busy sent a change")
	}
	hook("s2", "Notification", "Claude needs your permission to use Bash", "")
	if st := last(); st.State != StateWaiting || st.Message != "Claude needs your permission to use Bash" {
		t.Fatalf("waiting = %+v", st)
	}
	hook("s2", "PostToolUse", "", "")
	if st := last(); st.State != StateBusy || st.Message != "" || st.Prompt == "" {
		t.Fatalf("approved = %+v", st)
	}
	// Finished: done until seen.
	hook("s2", "Stop", "", "")
	if st := last(); st.State != StateIdle || !st.Unseen {
		t.Fatalf("done = %+v", st)
	}
	if a := s.Agents(); len(a) != 1 || !a[0].Unseen || a[0].Session != "s2" {
		t.Fatalf("agents = %+v", a)
	}
	s.Seen("s2")
	if st := last(); st.State != StateIdle || st.Unseen {
		t.Fatalf("seen = %+v", st)
	}
	n = count()
	s.Seen("s2")
	s.Seen("s7")
	if count() != n {
		t.Fatal("Seen without a change sent one")
	}
	// A new prompt clears done; another session is listed apart.
	hook("s3", "SessionStart", "", "")
	hook("s3", "UserPromptSubmit", "", "hello")
	hook("s3", "Stop", "", "")
	hook("s3", "UserPromptSubmit", "", "again")
	if st := last(); st.Unseen || st.Prompt != "again" {
		t.Fatalf("new prompt = %+v", st)
	}
	if a := s.Agents(); len(a) != 2 || a[0].Session != "s2" || a[1].Session != "s3" {
		t.Fatalf("agents = %+v", a)
	}
	// Forget (shell closed or back at its prompt) and SessionEnd end it once.
	s.Forget("s3")
	if st := last(); st.Session != "s3" || st.State != StateEnded {
		t.Fatalf("forget = %+v", st)
	}
	n = count()
	s.Forget("s3")
	hook("s3", "SessionEnd", "", "")
	if count() != n {
		t.Fatal("ended twice")
	}
	hook("s2", "SessionEnd", "", "")
	if len(s.Agents()) != 0 {
		t.Fatalf("agents = %+v", s.Agents())
	}
}

// New shells get WTF_AGENT_SETTINGS while agent-status is on and the
// plugin has its settings file.
func TestAgentSettingsEnv(t *testing.T) {
	s := NewService(nil)
	if err := s.startup(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.shutdown)
	s.helper, s.statusSettings = "/x/wtf-mcp", "/x/claude-plugin/"+StatusSettingsFile
	has := func() bool {
		for _, kv := range s.shellEnv("s1") {
			if kv == wire.EnvAgentSettings+"="+s.statusSettings {
				return true
			}
		}
		return false
	}
	if !has() {
		t.Error("missing by default")
	}
	on := false
	s.AgentStatus = func() bool { return on }
	if has() {
		t.Error("set while off")
	}
	on = true
	if !has() {
		t.Error("missing while on")
	}
	s.statusSettings = ""
	if has() {
		t.Error("set without the file")
	}
}

// Sub-agents, in the order Claude Code sends their hooks: a background
// one's Agent call names it before SubagentStart; one in front is named
// when it's done. Their tool events don't change their parent's state.
func TestSubagents(t *testing.T) {
	f := newFake(t)
	s := f.s
	hook := func(req wire.Request) {
		t.Helper()
		req.V, req.Pane, req.Token, req.Op = wire.Version, "s1", s.token("s1"), "hook"
		if r := s.do(context.Background(), req); r.Error != "" {
			t.Fatal(r.Error)
		}
	}
	last := func() Status {
		t.Helper()
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.status[len(f.status)-1]
	}
	hook(wire.Request{Event: "SessionStart"})
	hook(wire.Request{Event: "UserPromptSubmit", Prompt: "look around"})
	// Background: the Agent call, then SubagentStart.
	hook(wire.Request{Event: "PostToolUse", Tool: "Agent", Spawned: "a1", Input: &wire.ToolInput{Description: "Find the endpoints", SubagentType: "Explore"}})
	hook(wire.Request{Event: "SubagentStart", AgentID: "a1", AgentType: "Explore"})
	st := last()
	if st.State != StateBusy || len(st.Subagents) != 1 {
		t.Fatalf("started = %+v", st)
	}
	if sa := st.Subagents[0]; sa.ID != "a1" || sa.Type != "Explore" || sa.Task != "Find the endpoints" || !sa.Running || sa.Since == 0 {
		t.Fatalf("sub-agent = %+v", sa)
	}
	// The parent stops; its sub-agent works on, and its steps show.
	hook(wire.Request{Event: "Stop"})
	secret := "AKIA" + "IOSFODNN7EXAMPLE"
	hook(wire.Request{Event: "PostToolUse", AgentID: "a1", AgentType: "Explore", Tool: "Bash", Input: &wire.ToolInput{Command: "\n  aws s3 ls --key " + secret + "\nmore"}})
	st = last()
	if st.State != StateIdle || !st.Unseen {
		t.Fatalf("a sub-agent's step changed its parent: %+v", st)
	}
	if d := st.Subagents[0].Doing; !strings.HasPrefix(d, "Bash: aws s3 ls --key ") || strings.Contains(d, secret) || strings.Contains(d, "more") {
		t.Fatalf("doing = %q", d)
	}
	hook(wire.Request{Event: "PostToolUse", AgentID: "a1", AgentType: "Explore", Tool: "Read", Input: &wire.ToolInput{FilePath: "/src/bridge.go"}})
	if d := last().Subagents[0].Doing; d != "Read bridge.go" {
		t.Fatalf("doing = %q", d)
	}
	hook(wire.Request{Event: "SubagentStop", AgentID: "a1", AgentType: "Explore", Message: "\nFound 3 endpoints.\nDetails…"})
	if sa := last().Subagents[0]; sa.Running || sa.Doing != "" || sa.Result != "Found 3 endpoints." || sa.Ended == 0 {
		t.Fatalf("stopped = %+v", sa)
	}

	// In front: SubagentStart first, named by its Agent call when done.
	hook(wire.Request{Event: "UserPromptSubmit", Prompt: "again"})
	if st := last(); len(st.Subagents) != 0 {
		t.Fatalf("a new prompt kept finished sub-agents: %+v", st.Subagents)
	}
	hook(wire.Request{Event: "SubagentStart", AgentID: "a2", AgentType: "Plan"})
	hook(wire.Request{Event: "PostToolUse", AgentID: "a2", AgentType: "Plan", Tool: "mcp__plugin_wtf_wtf__pane_context", Input: &wire.ToolInput{}})
	if sa := last().Subagents[0]; sa.Task != "" || sa.Type != "Plan" || sa.Doing != "pane_context" {
		t.Fatalf("front = %+v", sa)
	}
	hook(wire.Request{Event: "PostToolUse", AgentID: "a2", AgentType: "Plan", Tool: "SubagentHandback", Input: &wire.ToolInput{Message: "Plan ready."}})
	hook(wire.Request{Event: "SubagentStop", AgentID: "a2", AgentType: "Plan"})
	hook(wire.Request{Event: "PostToolUse", Tool: "Agent", Spawned: "a2", Input: &wire.ToolInput{Description: "Plan it"}})
	if sa := last().Subagents[0]; sa.Running || sa.Task != "Plan it" || sa.Result != "Plan ready." {
		t.Fatalf("front done = %+v", sa)
	}

	// Claude Code's own agents (no type) and unknown stops are ignored.
	n := len(f.status)
	hook(wire.Request{Event: "SubagentStop", AgentID: "x"})
	hook(wire.Request{Event: "SubagentStop", AgentID: "y", AgentType: "Explore"})
	hook(wire.Request{Event: "SubagentStart", AgentID: "z"})
	if len(f.status) != n || len(s.Agents()[0].Subagents) != 1 {
		t.Fatalf("ignored events changed it: %+v", s.Agents())
	}

	// At most maxSubagents: the oldest finished go first.
	for i := range maxSubagents + 3 {
		hook(wire.Request{Event: "SubagentStart", AgentID: "r" + strconv.Itoa(i), AgentType: "Explore"})
	}
	subs := s.Agents()[0].Subagents
	if len(subs) != maxSubagents || subs[0].ID != "r3" {
		t.Fatalf("kept %d, first %s", len(subs), subs[0].ID)
	}
	// Agents() hands out copies.
	subs[0].ID = "changed"
	if s.Agents()[0].Subagents[0].ID == "changed" {
		t.Fatal("Agents shares its sub-agents")
	}
}

func TestStep(t *testing.T) {
	for _, c := range []struct {
		tool string
		in   wire.ToolInput
		want string
	}{
		{"Grep", wire.ToolInput{Pattern: "TODO", Path: "/src"}, "Grep TODO"},
		{"Glob", wire.ToolInput{Pattern: "**/*.go"}, "Glob **/*.go"},
		{"WebFetch", wire.ToolInput{URL: "https://code.claude.com/docs/en/hooks"}, "WebFetch code.claude.com"},
		{"WebSearch", wire.ToolInput{Query: "gotk4 listview"}, "WebSearch gotk4 listview"},
		{"LS", wire.ToolInput{Path: "/src/internal"}, "LS internal"},
		{"TodoWrite", wire.ToolInput{}, "TodoWrite"},
		{"Bash", wire.ToolInput{Command: strings.Repeat("y", 300)}, "Bash: " + strings.Repeat("y", maxStep-7) + "…"},
	} {
		if got := step(c.tool, &c.in); got != c.want {
			t.Errorf("%s %+v = %q, want %q", c.tool, c.in, got, c.want)
		}
	}
}

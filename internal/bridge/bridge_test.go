package bridge

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dayanraj/wtf/internal/bridge/wire"
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

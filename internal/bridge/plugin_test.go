package bridge

import (
	"bytes"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dayanraj/wtf/internal/bridge/wire"
)

// The plugin's folder in the repo, as bundled with the apps.
const pluginDir = "../../claude-plugin"

func readJSON(t *testing.T, name string, v any) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(pluginDir, name))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

type hookFile struct {
	Hooks map[string][]struct {
		Matcher string `json:"matcher"`
		Hooks   []struct {
			Type    string `json:"type"`
			Command string `json:"command"`
			Timeout int    `json:"timeout"`
		} `json:"hooks"`
	} `json:"hooks"`
}

// The plugin and server names make the MCP tools mcp__plugin_wtf_wtf__*,
// which the PreToolUse matcher names.
const toolPrefix = "mcp__plugin_wtf_wtf__"

func TestPluginFiles(t *testing.T) {
	var manifest struct{ Name string }
	readJSON(t, ".claude-plugin/plugin.json", &manifest)
	var mcpFile struct {
		MCPServers map[string]struct {
			Command string            `json:"command"`
			Env     map[string]string `json:"env"`
		} `json:"mcpServers"`
	}
	readJSON(t, ".mcp.json", &mcpFile)
	srv, ok := mcpFile.MCPServers["wtf"]
	if manifest.Name != "wtf" || !ok || len(mcpFile.MCPServers) != 1 {
		t.Fatalf("plugin %q, servers %v: the tool prefix isn't %s", manifest.Name, mcpFile.MCPServers, toolPrefix)
	}
	if srv.Command != "${WTF_MCP}" || len(srv.Env) != 3 {
		t.Fatalf("server = %+v", srv)
	}

	var hooks hookFile
	readJSON(t, "hooks/hooks.json", &hooks)
	for event, groups := range hooks.Hooks {
		if event != "PreToolUse" && hookStates[event] == "" && !subagentEvents[event] {
			t.Errorf("hook %s: the bridge doesn't know it", event)
		}
		for _, g := range groups {
			for _, h := range g.Hooks {
				// Quoted: the macOS app's path can have a space.
				if h.Type != "command" || !strings.HasPrefix(h.Command, `"${WTF_MCP}" hook`) {
					t.Errorf("%s: %+v", event, h)
				}
				// PreToolUse fails closed (exit 2 refuses the call).
				if event == "PreToolUse" && !strings.HasSuffix(h.Command, "|| exit 2") {
					t.Errorf("PreToolUse doesn't fail closed: %q", h.Command)
				}
			}
		}
	}
	for event := range maps.Keys(hookStates) {
		if codexOnlyHookEvents[event] {
			continue
		}
		if _, ok := hooks.Hooks[event]; !ok {
			t.Errorf("no %s hook", event)
		}
	}
	for event := range subagentEvents {
		if _, ok := hooks.Hooks[event]; !ok {
			t.Errorf("no %s hook", event)
		}
	}

	// Every command that runs or types something asks; the read-only
	// context tools don't.
	pre := hooks.Hooks["PreToolUse"]
	if len(pre) != 1 {
		t.Fatalf("PreToolUse groups = %d", len(pre))
	}
	re := regexp.MustCompile(pre[0].Matcher)
	for _, name := range []string{"Bash", "Monitor", "Edit", "MultiEdit", "Write", "NotebookEdit", toolPrefix + "run_readonly", toolPrefix + "type_at_prompt"} {
		if !re.MatchString(name) {
			t.Errorf("%s doesn't ask", name)
		}
	}
	for _, name := range []string{toolPrefix + "pane_context", toolPrefix + "recent_commands", toolPrefix + "review_changes", toolPrefix + "log_patterns", "Read", "Grep"} {
		if re.MatchString(name) {
			t.Errorf("%s asks", name)
		}
	}

	// The slash commands use only tools (and arguments) the bridge has.
	var names []string
	for _, tl := range Tools {
		names = append(names, tl.Name)
		var schema struct{ Properties map[string]any }
		json.Unmarshal(tl.InputSchema, &schema)
		for p := range schema.Properties {
			names = append(names, p)
		}
	}
	cmds, _ := filepath.Glob(filepath.Join(pluginDir, "commands", "*.md"))
	var seen []string
	for _, p := range cmds {
		seen = append(seen, strings.TrimSuffix(filepath.Base(p), ".md"))
		data, _ := os.ReadFile(p)
		if !bytes.HasPrefix(data, []byte("---\ndescription: ")) || !bytes.Contains(data, []byte("$ARGUMENTS")) {
			t.Errorf("%s: no description front matter or $ARGUMENTS", p)
		}
		for _, m := range regexp.MustCompile("`([a-z]+_[a-z_]+)`").FindAllStringSubmatch(string(data), -1) {
			if !slices.Contains(names, m[1]) {
				t.Errorf("%s: unknown tool or argument %s", p, m[1])
			}
		}
	}
	slices.Sort(seen)
	if !slices.Equal(seen, []string{"cmd", "explain", "history", "investigate", "logs", "postmortem", "review", "runbook", "triage"}) {
		t.Errorf("commands = %v", seen)
	}
}

func TestFindPlugin(t *testing.T) {
	root := t.TempDir()
	if findPlugin(filepath.Join(root, "bin")) != "" {
		t.Fatal("found a plugin in an empty folder")
	}
	for _, rel := range []string{"bin/claude-plugin", "Contents/Resources/claude-plugin", "usr/share/wtf/claude-plugin"} {
		dir := filepath.Join(root, rel)
		os.MkdirAll(filepath.Join(dir, ".claude-plugin"), 0o755)
		os.WriteFile(filepath.Join(dir, ".claude-plugin", "plugin.json"), []byte("{}"), 0o644)
	}
	for exe, want := range map[string]string{
		"bin": "bin/claude-plugin", "Contents/MacOS": "Contents/Resources/claude-plugin", "usr/bin": "usr/share/wtf/claude-plugin",
	} {
		if got := findPlugin(filepath.Join(root, exe)); got != filepath.Join(root, want) {
			t.Errorf("from %s: %q", exe, got)
		}
	}
}

// TestHook runs the hook commands as Claude Code does: through sh, with the
// event on stdin.
func TestHook(t *testing.T) {
	if testing.Short() {
		t.Skip("builds wtf-mcp")
	}
	f := newFake(t)
	bin := filepath.Join(t.TempDir(), "wtf mcp") // a space, as in Application Support
	if out, err := exec.Command("go", "build", "-o", bin, "../../cmd/wtf-mcp").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	var hooks hookFile
	readJSON(t, "hooks/hooks.json", &hooks)
	run := func(event string, env []string, stdin string) (string, int) {
		t.Helper()
		cmd := exec.Command("sh", "-c", hooks.Hooks[event][0].Hooks[0].Command)
		cmd.Env, cmd.Stdin = env, strings.NewReader(stdin)
		out, err := cmd.Output()
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return string(out), ee.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		return string(out), 0
	}
	env := append(os.Environ(), f.s.shellEnv("s1")...)
	env = append(env, "WTF_MCP="+bin)
	// This test can itself be run from a wtf shell. Cases that simulate wtf
	// being gone must not inherit that live pane's bridge credentials, or a
	// synthetic hook event would appear in the running app's Agents tab.
	withoutBridge := slices.DeleteFunc(slices.Clone(os.Environ()), func(kv string) bool {
		for _, name := range []string{wire.EnvBridge, wire.EnvPane, wire.EnvToken} {
			if strings.HasPrefix(kv, name+"=") {
				return true
			}
		}
		return false
	})

	out, code := run("PreToolUse", env, `{"hook_event_name":"PreToolUse","tool_name":"Bash","permission_mode":"auto"}`)
	var raw map[string]map[string]string
	json.Unmarshal([]byte(out), &raw)
	if code != 0 || raw["hookSpecificOutput"]["permissionDecision"] != "ask" || raw["hookSpecificOutput"]["hookEventName"] != "PreToolUse" {
		t.Fatalf("PreToolUse = %d %q", code, out)
	}
	// Fails closed: unreadable input, or no helper.
	if _, code := run("PreToolUse", env, "nonsense"); code != 2 {
		t.Errorf("bad input exits %d", code)
	}
	if _, code := run("PreToolUse", append(withoutBridge, "WTF_MCP="), `{"hook_event_name":"PreToolUse"}`); code != 2 {
		t.Errorf("no helper exits %d", code)
	}
	// It doesn't need the app.
	if out, code := run("PreToolUse", append(withoutBridge, "WTF_MCP="+bin), `{"hook_event_name":"PreToolUse"}`); code != 0 || !strings.Contains(out, `"ask"`) {
		t.Errorf("without the bridge: %d %q", code, out)
	}

	// claude-auto-mode: no decision, so Claude Code's own mode applies.
	if out, code := run("PreToolUse", append(env, "WTF_CLAUDE_AUTO=1"), `{"hook_event_name":"PreToolUse","tool_name":"Bash"}`); code != 0 || out != "" {
		t.Errorf("auto mode: %d %q", code, out)
	}

	// The second Notification comes while idle (Claude Code's "waiting for
	// your input" reminder) and changes nothing; PostToolUse repeats busy.
	for _, ev := range []string{"SessionStart", "UserPromptSubmit", "Notification", "PostToolUse", "PostToolUse", "Stop", "Notification", "SessionEnd"} {
		if out, code := run(ev, env, `{"hook_event_name":"`+ev+`","message":"Claude needs your permission to use Bash","prompt":"tidy the logs"}`); code != 0 || out != "" {
			t.Errorf("%s = %d %q", ev, code, out)
		}
	}
	// Status events don't fail a session when wtf is gone.
	if _, code := run("Stop", append(withoutBridge, "WTF_MCP="+bin), `{"hook_event_name":"Stop"}`); code != 0 {
		t.Errorf("Stop without the bridge exits %d", code)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		f.mu.Lock()
		got := slices.Clone(f.status)
		f.mu.Unlock()
		var states []string
		for _, st := range got {
			if st.Session != "s1" {
				t.Fatalf("status for %s", st.Session)
			}
			states = append(states, st.State)
		}
		want := []string{StateIdle, StateBusy, StateWaiting, StateBusy, StateIdle, StateEnded}
		if slices.Equal(states, want) {
			if got[2].Message != "Claude needs your permission to use Bash" {
				t.Errorf("message = %q", got[2].Message)
			}
			if got[1].Prompt != "tidy the logs" {
				t.Errorf("prompt = %q", got[1].Prompt)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("states = %v, want %v", states, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestHookSubagents feeds the helper the hook payloads Claude Code 2.1
// sent for a background Explore sub-agent (paths shortened).
func TestHookSubagents(t *testing.T) {
	if testing.Short() {
		t.Skip("builds wtf-mcp")
	}
	f := newFake(t)
	bin := filepath.Join(t.TempDir(), "wtf-mcp")
	if out, err := exec.Command("go", "build", "-o", bin, "../../cmd/wtf-mcp").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	var hooks hookFile
	readJSON(t, StatusSettingsFile, &hooks)
	env := append(append(os.Environ(), f.s.shellEnv("s1")...), "WTF_MCP="+bin)
	for _, ev := range []string{
		`{"hook_event_name":"UserPromptSubmit","prompt":"read a.txt"}`,
		`{"hook_event_name":"PostToolUse","tool_name":"Agent","tool_input":{"description":"Read a.txt","prompt":"Read the file a.txt and report it.","subagent_type":"Explore"},"tool_response":{"isAsync":true,"status":"async_launched","agentId":"aac03e4bdb4890928","description":"Read a.txt"},"tool_use_id":"toolu_1"}`,
		`{"agent_id":"aac03e4bdb4890928","agent_type":"Explore","hook_event_name":"SubagentStart"}`,
		`{"agent_id":"aac03e4bdb4890928","agent_type":"Explore","hook_event_name":"PostToolUse","tool_name":"Read","tool_input":{"file_path":"/tmp/work/a.txt"},"tool_response":{"type":"text","file":{"content":"hello\n"}},"tool_use_id":"toolu_2"}`,
		// Its turn ends with the sub-agent and a shell still going (as
		// Claude Code 2.1.325 sends it): it's still working.
		`{"hook_event_name":"Stop","stop_hook_active":false,"last_assistant_message":"started","background_tasks":[{"id":"aac03e4bdb4890928","type":"subagent","status":"running","description":"Read a.txt","agent_type":"Explore"},{"id":"b67oxxhdr","type":"shell","status":"running","description":"Sleep","command":"sleep 15"},{"id":"b2","type":"shell","status":"completed","description":"Done"}],"session_crons":[]}`,
	} {
		var e struct {
			Event string `json:"hook_event_name"`
		}
		json.Unmarshal([]byte(ev), &e)
		cmd := exec.Command("sh", "-c", hooks.Hooks[e.Event][0].Hooks[0].Command)
		cmd.Env, cmd.Stdin = env, strings.NewReader(ev)
		if out, err := cmd.CombinedOutput(); err != nil || len(out) != 0 {
			t.Fatalf("%s: %v %q", e.Event, err, out)
		}
	}
	a := f.s.Agents()
	if len(a) != 1 || len(a[0].Subagents) != 1 {
		t.Fatalf("agents = %+v", a)
	}
	want := Subagent{ID: "aac03e4bdb4890928", Type: "Explore", Task: "Read a.txt", Doing: "Read a.txt", Running: true, Since: a[0].Subagents[0].Since}
	if a[0].State != StateBusy || a[0].Background != 2 || a[0].Subagents[0] != want {
		t.Fatalf("got %+v, want %+v", a[0], want)
	}
}

// TestPluginValidates runs Claude Code's own check when it's installed.
func TestPluginValidates(t *testing.T) {
	claude, err := exec.LookPath("claude")
	if err != nil || testing.Short() {
		t.Skip("claude isn't installed")
	}
	if out, err := exec.Command(claude, "plugin", "validate", pluginDir).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestClaudeCommand(t *testing.T) {
	s := NewService(nil)
	if _, err := s.ClaudeCommand(); err == nil {
		t.Fatal("a command without the bridge running")
	}
	if err := s.startup(); err != nil {
		t.Fatal(err)
	}
	defer s.shutdown()
	s.helper, s.plugin = "/x/wtf-mcp", "/Applications/wtf.app/Contents/Resources/it's here"
	auto := false
	s.AutoMode = func() bool { return auto }
	got, err := s.ClaudeCommand()
	if want := `claude --plugin-dir '/Applications/wtf.app/Contents/Resources/it'\''s here'`; err != nil || got != want {
		t.Fatalf("command = %q %v, want %q", got, err, want)
	}
	auto = true
	if got, _ := s.ClaudeCommand(); !strings.HasPrefix(got, "WTF_CLAUDE_AUTO=1 claude --plugin-dir ") {
		t.Fatalf("auto = %q", got)
	}
	s.plugin = ""
	if _, err := s.ClaudeCommand(); err == nil {
		t.Fatal("a command without the plugin")
	}
}

// status-settings.json (agent-status) has the plugin's status hooks and
// nothing else: no PreToolUse (your own claude keeps its permission mode),
// no permissions, MCP servers or other settings.
func TestStatusSettings(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(pluginDir, StatusSettingsFile))
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		t.Fatal(err)
	}
	if len(top) != 1 || top["hooks"] == nil {
		t.Fatalf("keys = %v, want only hooks", slices.Collect(maps.Keys(top)))
	}
	var status, plugin hookFile
	readJSON(t, StatusSettingsFile, &status)
	readJSON(t, "hooks/hooks.json", &plugin)
	var got, want []string
	for ev := range status.Hooks {
		got = append(got, ev)
	}
	for ev := range plugin.Hooks {
		if ev != "PreToolUse" {
			want = append(want, ev)
		}
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	for ev, groups := range status.Hooks {
		if len(groups) != 1 || len(groups[0].Hooks) != 1 || groups[0].Matcher != "" {
			t.Fatalf("%s: %+v", ev, groups)
		}
		// Never fails claude: a status wtf misses changes nothing.
		if h := groups[0].Hooks[0]; h.Type != "command" || h.Command != `"${WTF_MCP}" hook || true` {
			t.Errorf("%s: %+v", ev, h)
		}
	}
}

// TestHookApprovals drives the helper's PermissionRequest hook as Claude
// Code 2.1.325 does (#39): it waits for an answer from the inbox, says
// nothing when it was answered in the pane, and goes when it's killed.
func TestHookApprovals(t *testing.T) {
	if testing.Short() {
		t.Skip("builds wtf-mcp")
	}
	f := newFake(t)
	bin := filepath.Join(t.TempDir(), "wtf-mcp")
	if out, err := exec.Command("go", "build", "-o", bin, "../../cmd/wtf-mcp").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	var hooks hookFile
	readJSON(t, StatusSettingsFile, &hooks)
	if h := hooks.Hooks["PermissionRequest"]; len(h) != 1 || h[0].Hooks[0].Timeout < 3600 {
		t.Fatalf("PermissionRequest hook = %+v: it must be allowed to wait", h)
	}
	env := append(append(os.Environ(), f.s.shellEnv("s1")...), "WTF_MCP="+bin)
	start := func(ev string) (*exec.Cmd, *strings.Builder) {
		var e struct {
			Event string `json:"hook_event_name"`
		}
		json.Unmarshal([]byte(ev), &e)
		cmd := exec.Command(bin, "hook")
		var out strings.Builder
		cmd.Env, cmd.Stdin, cmd.Stdout = env, strings.NewReader(ev), &out
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		return cmd, &out
	}
	run := func(ev string) {
		cmd, out := start(ev)
		if err := cmd.Wait(); err != nil || out.Len() != 0 {
			t.Fatalf("%s: %v %q", ev, err, out.String())
		}
	}
	// waitFor polls the agent until cond holds.
	waitFor := func(what string, cond func(Status) bool) Status {
		t.Helper()
		for i := 0; i < 200; i++ {
			if a := f.s.Agents(); len(a) == 1 && cond(a[0]) {
				return a[0]
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("never %s: %+v", what, f.s.Agents())
		return Status{}
	}
	asking := func(st Status) bool { return len(st.Approvals) == 1 && st.State == StateWaiting }
	run(`{"hook_event_name":"UserPromptSubmit","prompt":"restart the api"}`)

	// Allowed in the inbox.
	cmd, out := start(`{"hook_event_name":"PermissionRequest","cwd":"/tmp","tool_name":"Bash","tool_input":{"command":"kubectl -n shop rollout restart deploy/api","description":"Restart the api"},"permission_suggestions":[]}`)
	st := waitFor("asking", asking)
	ap := st.Approvals[0]
	if ap.Tool != "Bash" || ap.What != "kubectl -n shop rollout restart deploy/api" || ap.Description != "Restart the api" || st.Message != "Bash: kubectl -n shop rollout restart deploy/api" {
		t.Fatalf("approval = %+v, message %q", ap, st.Message)
	}
	if err := f.s.Answer(ap.ID, "maybe"); err == nil {
		t.Fatal("answered maybe")
	}
	if err := f.s.Answer(ap.ID, Allow); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if want := `{"hookSpecificOutput":{"decision":{"behavior":"allow"},"hookEventName":"PermissionRequest"}}` + "\n"; out.String() != want {
		t.Fatalf("allow: %q", out.String())
	}
	waitFor("working after allow", func(st Status) bool { return len(st.Approvals) == 0 && st.State == StateBusy })
	if err := f.s.Answer(ap.ID, Allow); err == nil {
		t.Fatal("answered twice")
	}

	// Denied in the inbox: the edit shows as a diff, and the turn stops.
	cmd, out = start(`{"hook_event_name":"PermissionRequest","tool_name":"Edit","tool_input":{"file_path":"/tmp/a.go","old_string":"x := 1\n","new_string":"x := 2\ny := 3\n"}}`)
	st = waitFor("asking about the edit", asking)
	if ap := st.Approvals[0]; ap.What != "/tmp/a.go" || ap.Diff != "- x := 1\n+ x := 2\n+ y := 3" || len(ap.Facts) != 0 {
		t.Fatalf("edit approval = %+v", ap)
	}
	f.s.Answer(st.Approvals[0].ID, Deny)
	cmd.Wait()
	if !strings.Contains(out.String(), `"behavior":"deny"`) || !strings.Contains(out.String(), `"interrupt":true`) {
		t.Fatalf("deny: %q", out.String())
	}
	waitFor("idle after deny", func(st Status) bool { return len(st.Approvals) == 0 && st.State == StateIdle })

	// Allowed in the pane: the tool's PostToolUse ends the wait, silently.
	run(`{"hook_event_name":"UserPromptSubmit","prompt":"again"}`)
	cmd, out = start(`{"hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"make deploy"}}`)
	waitFor("asking about make", asking)
	run(`{"hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"make other"},"tool_response":{}}`)
	if st := f.s.Agents()[0]; len(st.Approvals) != 1 {
		t.Fatalf("another command's PostToolUse cleared it: %+v", st)
	}
	run(`{"hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"make deploy"},"tool_response":{}}`)
	if err := cmd.Wait(); err != nil || out.Len() != 0 {
		t.Fatalf("answered in the pane: %v %q", err, out.String())
	}
	waitFor("working after the pane's allow", func(st Status) bool { return len(st.Approvals) == 0 && st.State == StateBusy })

	// A question Claude asks: answered with an option, which the hook hands
	// back as the tool's answers, with its input as Claude Code sent it.
	cmd, out = start(`{"hook_event_name":"PermissionRequest","tool_name":"AskUserQuestion","tool_input":{"questions":[{"question":"Which database?","header":"DB","options":[{"label":"Postgres","description":"SQL"},{"label":"Redis"}],"multiSelect":false}]}}`)
	st = waitFor("asking a question", asking)
	if ap := st.Approvals[0]; !ap.Clickable() || ap.Questions[0].Question != "Which database?" || !slices.Equal(ap.Questions[0].Options, []string{"Postgres", "Redis"}) || st.Message != "Which database?" {
		t.Fatalf("question = %+v, message %q", ap, st.Message)
	}
	if err := f.s.Answer(st.Approvals[0].ID, Allow); err == nil {
		t.Fatal("a question answered Allow")
	}
	if err := f.s.AnswerQuestion(st.Approvals[0].ID, []string{"MySQL"}); err == nil {
		t.Fatal("answered with an option it doesn't offer")
	}
	if err := f.s.AnswerQuestion(st.Approvals[0].ID, []string{"Redis"}); err != nil {
		t.Fatal(err)
	}
	cmd.Wait()
	if want := `{"hookSpecificOutput":{"decision":{"behavior":"allow","updatedInput":{"answers":{"Which database?":"Redis"},"questions":[{"header":"DB","multiSelect":false,"options":[{"description":"SQL","label":"Postgres"},{"label":"Redis"}],"question":"Which database?"}]}},"hookEventName":"PermissionRequest"}}` + "\n"; out.String() != want {
		t.Fatalf("answered question: %q", out.String())
	}
	waitFor("working after the answer", func(st Status) bool { return len(st.Approvals) == 0 && st.State == StateBusy })

	// A question answered in your own words.
	cmd, out = start(`{"hook_event_name":"PermissionRequest","tool_name":"AskUserQuestion","tool_input":{"questions":[{"question":"Color?","options":[{"label":"Red"}]}]}}`)
	st = waitFor("asking a question", asking)
	if !st.Approvals[0].FreeText {
		t.Fatalf("one question, not free text: %+v", st.Approvals[0])
	}
	if err := f.s.AnswerText(st.Approvals[0].ID, "  "); err == nil {
		t.Fatal("answered with nothing")
	}
	if err := f.s.AnswerText(st.Approvals[0].ID, " Purple "); err != nil {
		t.Fatal(err)
	}
	cmd.Wait()
	if want := `{"hookSpecificOutput":{"decision":{"behavior":"allow","updatedInput":{"answers":{"Color?":"Purple"},"questions":[{"options":[{"label":"Red"}],"question":"Color?"}]}},"hookEventName":"PermissionRequest"}}` + "\n"; out.String() != want {
		t.Fatalf("free-text answer: %q", out.String())
	}

	// "Yes, and don't ask again": the suggestion goes back as Claude Code
	// sent it; ones that aren't an allow aren't offered.
	cmd, out = start(`{"hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"npm test"},"permission_suggestions":[` +
		`{"type":"addRules","rules":[{"toolName":"Bash","ruleContent":"npm test:*"}],"behavior":"allow","destination":"localSettings"},` +
		`{"type":"addRules","rules":[{"toolName":"Bash"}],"behavior":"deny","destination":"localSettings"},` +
		`{"type":"setMode","mode":"acceptEdits","destination":"session"},` +
		`{"type":"addDirectories","directories":["/srv"],"destination":"session"}]}`)
	st = waitFor("asking about npm test", asking)
	if want := []string{"Always allow Bash(npm test:*) in this project", "Accept all edits this session", "Always allow access to /srv this session"}; !slices.Equal(st.Approvals[0].Always, want) || st.Approvals[0].FreeText {
		t.Fatalf("always = %q", st.Approvals[0].Always)
	}
	if err := f.s.AllowAlways(st.Approvals[0].ID, 3); err == nil {
		t.Fatal("a choice it doesn't have")
	}
	if err := f.s.AllowAlways(st.Approvals[0].ID, 1); err != nil {
		t.Fatal(err)
	}
	cmd.Wait()
	if want := `{"hookSpecificOutput":{"decision":{"behavior":"allow","updatedPermissions":[{"type":"setMode","mode":"acceptEdits","destination":"session"}]},"hookEventName":"PermissionRequest"}}` + "\n"; out.String() != want {
		t.Fatalf("always: %q", out.String())
	}
	waitFor("working after always", func(st Status) bool { return len(st.Approvals) == 0 && st.State == StateBusy })

	// Denied with what to do instead: Claude carries on (no interrupt).
	cmd, out = start(`{"hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"npm i"}}`)
	st = waitFor("asking about npm i", asking)
	if err := f.s.AnswerText(st.Approvals[0].ID, "x"); err == nil {
		t.Fatal("a command answered with text")
	}
	if err := f.s.DenyWith(st.Approvals[0].ID, "Use pnpm instead"); err != nil {
		t.Fatal(err)
	}
	cmd.Wait()
	if want := `{"hookSpecificOutput":{"decision":{"behavior":"deny","message":"The user denied this in wtf's Approvals inbox and said: Use pnpm instead"},"hookEventName":"PermissionRequest"}}` + "\n"; out.String() != want {
		t.Fatalf("deny with: %q", out.String())
	}
	run(`{"hook_event_name":"UserPromptSubmit","prompt":"go on"}`)

	// Several questions, several choices: answered all at once with the
	// card's form, a choice or your own words for each.
	cmd, out = start(`{"hook_event_name":"PermissionRequest","tool_name":"AskUserQuestion","tool_input":{"questions":[{"question":"Fruits?","options":[{"label":"Apple"},{"label":"Banana"},{"label":"Cherry"}],"multiSelect":true},{"question":"Size?","options":[{"label":"S"},{"label":"L"}]},{"question":"Name?","options":[{"label":"x"}]}]}}`)
	st = waitFor("asking three questions", asking)
	if ap := st.Approvals[0]; !ap.Form || !ap.FreeText || ap.Clickable() {
		t.Fatalf("form: %+v", ap)
	}
	for _, bad := range [][]Pick{
		{{Options: []string{"Apple"}}, {Options: []string{"S"}}},
		{{Options: []string{"Apple"}}, {Options: []string{"S", "L"}}, {Text: "Bo"}},
		{{Options: []string{"Apple"}}, {}, {Text: "Bo"}},
		{{Options: []string{"Kiwi"}}, {Options: []string{"S"}}, {Text: "Bo"}},
	} {
		if err := f.s.AnswerAll(st.Approvals[0].ID, bad); err == nil {
			t.Fatalf("answered with %+v", bad)
		}
	}
	if err := f.s.AnswerAll(st.Approvals[0].ID, []Pick{{Options: []string{"Apple", "Cherry"}}, {Options: []string{"L"}}, {Text: " Bo "}}); err != nil {
		t.Fatal(err)
	}
	cmd.Wait()
	if !strings.Contains(out.String(), `"answers":{"Fruits?":"Apple, Cherry","Name?":"Bo","Size?":"L"}`) {
		t.Fatalf("form answers: %q", out.String())
	}
	run(`{"hook_event_name":"UserPromptSubmit","prompt":"go on"}`)

	// Or in the pane.
	cmd, _ = start(`{"hook_event_name":"PermissionRequest","tool_name":"AskUserQuestion","tool_input":{"questions":[{"question":"Which?","options":[{"label":"a"}]},{"question":"Port?","options":[{"label":"1"}]}]}}`)
	st = waitFor("asking two questions", asking)
	if st.Approvals[0].Clickable() || st.Message != "Which? (and 1 more)" {
		t.Fatalf("two questions: %+v %q", st.Approvals[0], st.Message)
	}
	f.s.Typed("s1", []byte("\r"))
	run(`{"hook_event_name":"PostToolUse","tool_name":"AskUserQuestion","tool_input":{},"tool_response":{}}`)
	cmd.Wait()

	// Allowed with a key in the pane: off the list at once, before the
	// tool finishes; typing elsewhere, or other keys, leave it.
	cmd, out = start(`{"hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"make test"}}`)
	st = waitFor("asking about make test", asking)
	f.s.Typed("s2", []byte("\r"))
	f.s.Typed("s1", []byte("\x1b[B"))
	f.s.Typed("s1", []byte("x"))
	if st := f.s.Agents()[0]; len(st.Approvals) != 1 {
		t.Fatalf("a key that doesn't answer cleared it: %+v", st)
	}
	f.s.Typed("s1", []byte("1"))
	if st := f.s.Agents()[0]; len(st.Approvals) != 0 || st.State != StateBusy {
		t.Fatalf("after 1 in the pane: %+v", st)
	}
	if err := f.s.Answer(st.Approvals[0].ID, Allow); err == nil {
		t.Fatal("answered from the inbox after the pane")
	}
	run(`{"hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"make test"},"tool_response":{}}`)
	if err := cmd.Wait(); err != nil || out.Len() != 0 {
		t.Fatalf("answered with a key: %v %q", err, out.String())
	}

	// No with a key in the pane: its hook killed, the turn stops.
	cmd, _ = start(`{"hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"rm -rf build"}}`)
	waitFor("asking about rm", asking)
	f.s.Typed("s1", []byte("3"))
	if st := f.s.Agents()[0]; len(st.Approvals) != 0 {
		t.Fatalf("after 3 in the pane: %+v", st)
	}
	cmd.Process.Signal(os.Interrupt)
	cmd.Wait()
	waitFor("idle after the key's No", func(st Status) bool { return len(st.Approvals) == 0 && st.State == StateIdle })
	run(`{"hook_event_name":"UserPromptSubmit","prompt":"go on"}`)

	// Denied in the pane: Claude Code kills the hook.
	cmd, _ = start(`{"hook_event_name":"PermissionRequest","tool_name":"Write","tool_input":{"file_path":"/tmp/b","content":"hi\n"}}`)
	st = waitFor("asking about the write", asking)
	if st.Approvals[0].Diff != "+ hi" {
		t.Fatalf("write diff %q", st.Approvals[0].Diff)
	}
	cmd.Process.Signal(os.Interrupt)
	cmd.Wait()
	waitFor("idle after the hook went", func(st Status) bool { return len(st.Approvals) == 0 && st.State == StateIdle })

	// A sub-agent's prompt outlives its parent's Stop; SessionEnd ends it.
	run(`{"hook_event_name":"UserPromptSubmit","prompt":"explore"}`)
	cmd, out = start(`{"hook_event_name":"PermissionRequest","agent_id":"a1","agent_type":"Explore","tool_name":"Bash","tool_input":{"command":"ls /"}}`)
	st = waitFor("asking for the sub-agent", asking)
	if st.Approvals[0].Subagent != "Explore" {
		t.Fatalf("sub-agent approval %+v", st.Approvals[0])
	}
	run(`{"hook_event_name":"Stop","background_tasks":[{"status":"running"}]}`)
	if st := f.s.Agents()[0]; len(st.Approvals) != 1 {
		t.Fatalf("Stop cleared a sub-agent's approval: %+v", st)
	}
	run(`{"hook_event_name":"SessionEnd"}`)
	if err := cmd.Wait(); err != nil || out.Len() != 0 {
		t.Fatalf("after SessionEnd: %v %q", err, out.String())
	}
}

func TestJoinArgv(t *testing.T) {
	if got := joinArgv([]string{"kubectl", "get", "pods", "-l", "app=api web", "it's"}); got != `kubectl get pods -l 'app=api web' 'it'\''s'` {
		t.Fatalf("joinArgv = %s", got)
	}
}

// TestHookCodexApprovals: Codex shows its own prompt only once the hook
// returns (0.167), so its PermissionRequest is listed without waiting; the
// inbox answers with the prompt's keys, and Codex's events clear it.
func TestHookCodexApprovals(t *testing.T) {
	if testing.Short() {
		t.Skip("builds wtf-mcp")
	}
	f := newFake(t)
	bin := filepath.Join(t.TempDir(), "wtf-mcp")
	if out, err := exec.Command("go", "build", "-o", bin, "../../cmd/wtf-mcp").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	env := append(append(os.Environ(), f.s.shellEnv("s1")...), "WTF_MCP="+bin)
	run := func(ev string) {
		t.Helper()
		cmd := exec.Command(bin, "codex-hook")
		var out strings.Builder
		cmd.Env, cmd.Stdin, cmd.Stdout = env, strings.NewReader(ev), &out
		done := make(chan error, 1)
		go func() { done <- cmd.Run() }()
		select {
		case err := <-done:
			if err != nil || out.Len() != 0 {
				t.Fatalf("%s: %v %q", ev, err, out.String())
			}
		case <-time.After(5 * time.Second):
			cmd.Process.Kill()
			t.Fatalf("%s: the hook waited (Codex wouldn't show its prompt)", ev)
		}
	}
	agent := func() Status {
		a := f.s.Agents()
		if len(a) != 1 {
			t.Fatalf("agents = %+v", a)
		}
		return a[0]
	}
	run(`{"hook_event_name":"SessionStart","cwd":"/tmp"}`)
	run(`{"hook_event_name":"UserPromptSubmit","prompt":"make the file"}`)
	// A sandboxed attempt's PostToolUse comes before the request: it mustn't
	// clear one made after it.
	run(`{"hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"touch /x"}}`)
	run(`{"hook_event_name":"PermissionRequest","turn_id":"t1","cwd":"/tmp","tool_name":"Bash","tool_input":{"command":"touch /x","description":"Create /x"}}`)
	st := agent()
	if st.Provider != "codex" || st.State != StateWaiting || len(st.Approvals) != 1 || st.Approvals[0].What != "touch /x" {
		t.Fatalf("codex approval: %+v", st)
	}
	if err := f.s.Answer(st.Approvals[0].ID, Allow); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	keys := slices.Clone(f.keys)
	f.mu.Unlock()
	if !slices.Equal(keys, []string{"y"}) || len(agent().Approvals) != 0 {
		t.Fatalf("allow pressed %q, approvals %+v", keys, agent().Approvals)
	}
	run(`{"hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"touch /x"}}`)
	if agent().State != StateBusy {
		t.Fatalf("after it ran: %+v", agent())
	}

	// Its Yes typed in the pane: off the list, working.
	run(`{"hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"touch /z"}}`)
	f.s.Typed("s1", []byte("y"))
	if st := agent(); len(st.Approvals) != 0 || st.State != StateBusy {
		t.Fatalf("after y in the pane: %+v", st)
	}

	// Its No in the pane: Interrupt.
	run(`{"hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"rm -rf /y"}}`)
	run(`{"hook_event_name":"Interrupt"}`)
	if st := agent(); len(st.Approvals) != 0 || st.State != StateIdle {
		t.Fatalf("after Interrupt: %+v", st)
	}

	// Deny from the inbox presses Esc; an edit is an apply_patch.
	run(`{"hook_event_name":"UserPromptSubmit","prompt":"edit"}`)
	run(`{"hook_event_name":"PermissionRequest","tool_name":"apply_patch","tool_input":{"command":"*** Begin Patch\n*** Update File: a.go\n@@\n-x := 1\n+x := 2\n*** End Patch\n"}}`)
	st = agent()
	if len(st.Approvals) != 1 || st.Approvals[0].What != "a.go" || st.Approvals[0].Diff != "- x := 1\n+ x := 2" || len(st.Approvals[0].Facts) != 0 {
		t.Fatalf("apply_patch approval: %+v", st.Approvals)
	}
	f.s.Answer(st.Approvals[0].ID, Deny)
	f.mu.Lock()
	keys = slices.Clone(f.keys)
	f.mu.Unlock()
	if !slices.Equal(keys, []string{"y", "\x1b"}) {
		t.Fatalf("deny pressed %q", keys)
	}
	if err := f.s.Answer(st.Approvals[0].ID, Deny); err == nil {
		t.Fatal("answered twice")
	}

	// Always presses a; denied with words, Esc then the words, sent.
	run(`{"hook_event_name":"UserPromptSubmit","prompt":"more"}`)
	run(`{"hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"ls"}}`)
	st = agent()
	if !slices.Equal(st.Approvals[0].Always, []string{"Don't ask again for this command this session"}) {
		t.Fatalf("codex always = %q", st.Approvals[0].Always)
	}
	f.mu.Lock()
	f.keys = nil
	f.mu.Unlock()
	if err := f.s.AllowAlways(st.Approvals[0].ID, 0); err != nil {
		t.Fatal(err)
	}
	run(`{"hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"rm x"}}`)
	if err := f.s.DenyWith(agent().Approvals[0].ID, "keep x,\n  remove y"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	keys = slices.Clone(f.keys)
	f.mu.Unlock()
	if !slices.Equal(keys, []string{"a", "\x1b", "keep x, remove y", "\r"}) || len(agent().Approvals) != 0 {
		t.Fatalf("always and deny-with pressed %q", keys)
	}

	// A question (request_user_input, through its PreToolUse): answered by
	// pressing each option's number in its pane.
	run(`{"hook_event_name":"UserPromptSubmit","prompt":"plan"}`)
	run(`{"hook_event_name":"PreToolUse","tool_name":"request_user_input","tool_input":{"questions":[{"id":"db","header":"DB","question":"Which database?","options":[{"label":"Postgres (Recommended)","description":"SQL"},{"label":"Redis","description":"KV"}]}]}}`)
	st = agent()
	if len(st.Approvals) != 1 || !st.Approvals[0].Clickable() || st.State != StateWaiting || st.Message != "Which database?" {
		t.Fatalf("codex question: %+v", st)
	}
	if err := f.s.Answer(st.Approvals[0].ID, Allow); err == nil {
		t.Fatal("a codex question answered y")
	}
	f.s.Typed("s1", []byte("y")) // starts a note there: still asking
	if len(agent().Approvals) != 1 {
		t.Fatal("y cleared a codex question")
	}
	f.mu.Lock()
	f.keys = nil
	f.mu.Unlock()
	if err := f.s.AnswerQuestion(st.Approvals[0].ID, []string{"Redis"}); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	keys = slices.Clone(f.keys)
	f.mu.Unlock()
	if st := agent(); !slices.Equal(keys, []string{"2"}) || len(st.Approvals) != 0 || st.State != StateBusy {
		t.Fatalf("answered with %q: %+v", keys, st)
	}
	run(`{"hook_event_name":"PostToolUse","tool_name":"request_user_input","tool_input":{"questions":[]}}`)

	// Several questions: a number for each, in order; or answered in the
	// pane, where its PostToolUse clears it.
	run(`{"hook_event_name":"PreToolUse","tool_name":"request_user_input","tool_input":{"questions":[{"id":"a","question":"A?","options":[{"label":"x"},{"label":"y"}]},{"id":"b","question":"B?","options":[{"label":"p"},{"label":"q"}]}]}}`)
	st = agent()
	if st.Approvals[0].Clickable() || !st.Approvals[0].Form || st.Approvals[0].FreeText {
		t.Fatalf("two codex questions: %+v", st.Approvals[0])
	}
	if err := f.s.AnswerAll(st.Approvals[0].ID, []Pick{{Options: []string{"y"}}, {Text: "z"}}); err == nil {
		t.Fatal("a codex question answered in words")
	}
	f.mu.Lock()
	f.keys = nil
	f.mu.Unlock()
	if err := f.s.AnswerAll(st.Approvals[0].ID, []Pick{{Options: []string{"y"}}, {Options: []string{"p"}}}); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	keys = slices.Clone(f.keys)
	f.mu.Unlock()
	if !slices.Equal(keys, []string{"2", "1"}) {
		t.Fatalf("two answers pressed %q", keys)
	}
	run(`{"hook_event_name":"PreToolUse","tool_name":"request_user_input","tool_input":{"questions":[{"id":"c","question":"C?","options":[{"label":"x"}]}]}}`)
	run(`{"hook_event_name":"PostToolUse","tool_name":"request_user_input","tool_input":{"questions":[]}}`)
	if st := agent(); len(st.Approvals) != 0 {
		t.Fatalf("answered in the pane: %+v", st)
	}

	// Its PreToolUse for anything else says nothing and lists nothing.
	run(`{"hook_event_name":"PreToolUse","tool_name":"exec_command","tool_input":{"cmd":"ls"}}`)
	if st := agent(); len(st.Approvals) != 0 {
		t.Fatalf("another tool's PreToolUse: %+v", st)
	}
}

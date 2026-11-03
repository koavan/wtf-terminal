package bridge

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
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
		if event != "PreToolUse" && hookStates[event] == "" {
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
	for event := range hookStates {
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
	if !slices.Equal(seen, []string{"cmd", "explain", "investigate", "logs", "postmortem", "review", "runbook"}) {
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
	if _, code := run("PreToolUse", append(os.Environ(), "WTF_MCP="), `{"hook_event_name":"PreToolUse"}`); code != 2 {
		t.Errorf("no helper exits %d", code)
	}
	// It doesn't need the app.
	if out, code := run("PreToolUse", append(os.Environ(), "WTF_MCP="+bin), `{"hook_event_name":"PreToolUse"}`); code != 0 || !strings.Contains(out, `"ask"`) {
		t.Errorf("without the bridge: %d %q", code, out)
	}

	// claude-auto-mode: no decision, so Claude Code's own mode applies.
	if out, code := run("PreToolUse", append(env, "WTF_CLAUDE_AUTO=1"), `{"hook_event_name":"PreToolUse","tool_name":"Bash"}`); code != 0 || out != "" {
		t.Errorf("auto mode: %d %q", code, out)
	}

	// The second Notification comes while idle (Claude Code's "waiting for
	// your input" reminder) and changes nothing; PostToolUse repeats busy.
	for _, ev := range []string{"SessionStart", "UserPromptSubmit", "Notification", "PostToolUse", "PostToolUse", "Stop", "Notification", "SessionEnd"} {
		if out, code := run(ev, env, `{"hook_event_name":"`+ev+`","message":"Claude needs your permission to use Bash"}`); code != 0 || out != "" {
			t.Errorf("%s = %d %q", ev, code, out)
		}
	}
	// Status events don't fail a session when wtf is gone.
	if _, code := run("Stop", append(os.Environ(), "WTF_MCP="+bin), `{"hook_event_name":"Stop"}`); code != 0 {
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
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("states = %v, want %v", states, want)
		}
		time.Sleep(20 * time.Millisecond)
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

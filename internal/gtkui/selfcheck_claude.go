//go:build linux

package gtkui

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/dayanraj/wtf/internal/bridge"
	"github.com/dayanraj/wtf/internal/uicore"
)

// standInClaude plays Claude Code in a Claude pane: it notes how it was
// started, runs the plugin's hooks the way Claude Code does, and asks the
// plugin's MCP server for the target's recent commands, then takes prompts
// line by line. Files in dir mark each step; the check creates "go" and
// "release" to move it on.
const standInClaude = `#!/bin/sh
d=%DIR%
echo "$*" > $d/args
h() { printf '%s' "$1" | "$WTF_MCP" hook; }
h '{"hook_event_name":"SessionStart"}'
h '{"hook_event_name":"PreToolUse","tool_name":"Bash"}' > $d/pre.tmp && mv $d/pre.tmp $d/pre
(printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"check","version":"1"}}}' \
	'{"jsonrpc":"2.0","method":"notifications/initialized"}' \
	'{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"recent_commands","arguments":{"count":5}}}'; sleep 3) | "$WTF_MCP" > $d/mcp.out 2> $d/mcp.err
h '{"hook_event_name":"UserPromptSubmit"}'
h '{"hook_event_name":"Notification","message":"Claude needs your permission to use Bash"}'
touch $d/waiting
while [ ! -e $d/go ]; do sleep 0.1; done
h '{"hook_event_name":"Stop"}'
touch $d/idle
# Prompts, as Claude Code takes them: each is busy, then idle; "hold" stays
# busy until the check creates "release"; "quit" ends it.
while IFS= read -r line; do
	echo "$line" >> $d/typed
	[ "$line" = quit ] && break
	h '{"hook_event_name":"UserPromptSubmit"}'
	case $line in *hold) while [ ! -e $d/release ]; do sleep 0.1; done ;; esac
	h '{"hook_event_name":"Stop"}'
done
h '{"hook_event_name":"SessionEnd"}'
`

// checkClaude (WTF_NATIVE_DEBUG=claude): Ask Claude (#32) with a stand-in
// claude (WTF_CLAUDE_STANDIN). It opens a split beside the shell running claude with
// the bundled plugin, points the plugin's tools at that shell, shows the
// hooks' states on the tab, goes back to the same Claude pane when asked
// again, and forgets it when it closes. Needs bin/gtk/wtf-mcp and
// bin/gtk/claude-plugin beside the app (linux/build.sh).
func (w *Window) checkClaude() {
	a := w.app
	dir := "/tmp/clbin"
	_ = os.RemoveAll(dir)
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(dir+"/claude", []byte(strings.ReplaceAll(standInClaude, "%DIR%", dir)), 0o755)
	// The backend runs it instead of claude in this check (backend.go).
	_ = os.Setenv("WTF_CLAUDE_STANDIN", dir+"/claude")
	exists := func(name string) bool { _, err := os.Stat(dir + "/" + name); return err == nil }
	read := func(name string) string { b, _ := os.ReadFile(dir + "/" + name); return string(b) }

	target := w.active()
	check(waitUntil(5*time.Second, func() bool { return target != nil && len(target.tracker.Blocks(0, 1<<30)) > 0 }), "shell integration in the shell")
	if target == nil {
		w.finish()
		return
	}
	target.write("echo hello-claude-target\r")
	check(waitUntil(5*time.Second, func() bool {
		id, ok := target.tracker.LastFinished()
		b, _ := target.tracker.Info(id)
		return ok && b.Text == "echo hello-claude-target"
	}), "the shell ran echo")

	var targetSession string
	onMain(func() {
		targetSession = w.model.Panes[target.id].SessionID
		w.askClaude()
	})
	var claude *Pane
	check(waitUntil(5*time.Second, func() bool { claude = w.active(); return claude != nil && claude != target && claude.sess != nil }),
		"Ask Claude splits beside the shell")
	if claude == nil || claude == target {
		w.finish()
		return
	}
	var claudeSession string
	var panes int
	onMain(func() {
		claudeSession = w.model.Panes[claude.id].SessionID
		panes = len(w.model.Panes)
	})
	check(waitUntil(15*time.Second, func() bool { return exists("args") }), "claude starts in the split")
	args := read("args")
	check(strings.HasPrefix(args, "--plugin-dir /") && strings.HasSuffix(strings.TrimSpace(args), "/claude-plugin"),
		"with the bundled plugin: %q", args)
	var mapped string
	onMain(func() { mapped = a.claude.targets[claudeSession] })
	check(mapped == targetSession && targetSession != "", "its tools work on the shell it came from (%s → %s)", claudeSession, mapped)
	check(waitUntil(10*time.Second, func() bool { return exists("pre") }) && strings.Contains(read("pre"), `"permissionDecision":"ask"`),
		"the plugin asks before Bash: %q", read("pre"))

	check(waitUntil(20*time.Second, func() bool { return exists("waiting") }), "the hooks ran")
	out := read("mcp.out")
	check(strings.Contains(out, "echo hello-claude-target") && !strings.Contains(out, "plugin-dir"),
		"recent_commands reads the target, not the Claude pane: %.200q (stderr %q)", out, read("mcp.err"))
	// On the main loop (waitUntil's conditions are).
	state := func() string {
		if t, ok := w.model.TabOf(claude.id); ok {
			return w.claudeState(t)
		}
		return ""
	}
	var st string
	check(waitUntil(5*time.Second, func() bool { st = state(); return st == bridge.StateWaiting }), "the tab shows Claude Code needs you: %q", st)
	w.shot("claude-waiting")
	_ = os.WriteFile(dir+"/go", nil, 0o644)
	check(waitUntil(5*time.Second, func() bool { st = state(); return exists("idle") && st == bridge.StateIdle }), "then idle: %q", st)

	// Asked again from the shell: back to the same Claude pane, nothing typed.
	onMain(func() {
		w.model.FocusPane(target.id)
		w.askClaude()
	})
	var focused, count int
	onMain(func() {
		if ps := w.model.ActivePane(); ps != nil {
			focused = ps.ID
		}
		count = len(w.model.Panes)
	})
	check(focused == claude.id && count == panes, "asking again goes to the same Claude pane (focused %d, %d panes)", focused, count)
	// From the Claude pane itself: no new split.
	onMain(w.askClaude)
	onMain(func() { count = len(w.model.Panes) })
	check(count == panes, "a Claude pane doesn't get its own Claude pane (%d panes)", count)

	// #24–#27 through the Claude pane: what each one types there.
	typed := func(line string) bool { return slices.Contains(strings.Split(read("typed"), "\n"), line) }
	target.write("ls /nonexistent-wtf\r")
	var failed int
	check(waitUntil(5*time.Second, func() bool {
		id, ok := target.tracker.LastFinished()
		b, _ := target.tracker.Info(id)
		failed = id
		return ok && b.Text == "ls /nonexistent-wtf" && b.ExitCode != nil && *b.ExitCode != 0
	}), "a command fails in the shell")
	var labels []string
	onMain(func() {
		for _, it := range target.blockItems(failed) {
			if it != nil {
				labels = append(labels, it.label)
			}
		}
	})
	check(slices.Contains(labels, "Explain Failure… (Claude)"), "its block menu offers Explain Failure: %q", labels)
	onMain(func() { w.explainBlock(target, failed) })
	explain := fmt.Sprintf("/wtf:explain block %d", failed)
	check(waitUntil(5*time.Second, func() bool { return typed(explain) }), "Explain Failure types %q in the Claude pane, with Enter (got %q)", explain, read("typed"))
	check(waitUntil(5*time.Second, func() bool { return state() == bridge.StateIdle }), "and Claude Code is idle again")

	// Summarize Logs: a command with enough lines offers it.
	target.write("seq 12\r")
	var seqID int
	check(waitUntil(5*time.Second, func() bool {
		id, ok := target.tracker.LastFinished()
		b, _ := target.tracker.Info(id)
		seqID = id
		return ok && b.Text == "seq 12"
	}), "the shell ran seq 12")
	labels = nil
	onMain(func() {
		for _, it := range target.blockItems(seqID) {
			if it != nil {
				labels = append(labels, it.label)
			}
		}
	})
	check(slices.Contains(labels, "Summarize Logs… (Claude)"), "its block menu offers Summarize Logs: %q", labels)
	onMain(func() { w.model.FocusPane(target.id); w.claudeLogs(seqID) })
	logs := fmt.Sprintf("/wtf:logs block %d", seqID)
	check(waitUntil(5*time.Second, func() bool { return typed(logs) }), "it types %q, with Enter", logs)
	check(waitUntil(5*time.Second, func() bool { return state() == bridge.StateIdle }), "and Claude Code is idle again")

	onMain(func() { w.model.FocusPane(target.id); w.claudeCommand() })
	time.Sleep(500 * time.Millisecond)
	var screen string
	onMain(func() {
		top, _ := claude.top()
		for r := top; r < top+claude.term.Rows(); r++ {
			screen += claude.term.Row(r) + "\n"
		}
	})
	check(strings.Contains(screen, "/wtf:cmd") && !strings.Contains(read("typed"), "/wtf:cmd"),
		"Command from Plain English types /wtf:cmd for you to finish, without Enter")
	claude.write("hold\r")
	check(waitUntil(5*time.Second, func() bool { return typed("/wtf:cmd hold") && state() == bridge.StateBusy }), "you finish it: busy (%q)", state())

	// While it works, a review waits for its next prompt.
	onMain(func() { w.model.FocusPane(target.id); w.reviewFile("/src/internal/review/testdata/plan.json") })
	check(waitUntil(5*time.Second, func() bool {
		return reviewShown != nil && reviewShown.review != nil && reviewShown.summarize.Sensitive()
	}),
		"a plan file's review offers Summarize with Claude")
	onMain(func() { reviewShown.summarize.Activate() })
	review := "/wtf:review plan file /src/internal/review/testdata/plan.json"
	time.Sleep(700 * time.Millisecond)
	check(!typed(review) && reviewShown == nil, "the dialog closes, and nothing is typed while Claude Code works")
	_ = os.WriteFile(dir+"/release", nil, 0o644)
	check(waitUntil(5*time.Second, func() bool { return typed(review) }), "then it's typed: %q", read("typed"))
	check(waitUntil(5*time.Second, func() bool { return state() == bridge.StateIdle }), "and Claude Code is idle again")

	onMain(func() { w.model.FocusPane(target.id); w.claudeInvestigate() })
	time.Sleep(500 * time.Millisecond)
	screen = ""
	onMain(func() {
		top, _ := claude.top()
		for r := top; r < top+claude.term.Rows(); r++ {
			screen += claude.term.Row(r) + "\n"
		}
	})
	check(strings.Contains(screen, "/wtf:investigate"), "Investigate types /wtf:investigate for you to finish")
	claude.write(uicore.ClearLine + "quit\r")
	check(waitUntil(10*time.Second, func() bool { ps := w.model.Panes[claude.id]; return ps != nil && !ps.Running }), "claude exits")
	onMain(func() { w.model.ClosePane(claude.id) })
	check(waitUntil(5*time.Second, func() bool { return len(a.claude.targets) == 0 }), "closing the Claude pane forgets it")
	var badge string
	onMain(func() {
		if t, ok := w.model.TabOf(target.id); ok {
			badge = w.claudeState(t)
		}
	})
	check(badge == "", "and the tab's badge goes: %q", badge)
	w.finish()
}

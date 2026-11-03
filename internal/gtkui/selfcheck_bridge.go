//go:build linux

package gtkui

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/dayanraj/wtf/internal/uicore"
)

// checkBridge (WTF_NATIVE_DEBUG=bridge): the bridge to the plugin's MCP
// server (#32), driven the way Claude Code drives it. One shell (the
// "Claude" one) starts $WTF_MCP with its own environment and speaks MCP to
// it over stdio; its tools point at a second shell (the target), which has
// a stand-in dig on its PATH. The helper lists the tools, reads the
// target's recent commands, runs an allowed command there as a block,
// refuses one that isn't allowed without typing it, and types a command at
// the target's prompt without running it. Needs bin/gtk/wtf-mcp beside
// the app (linux/build.sh).
func (w *Window) checkBridge() {
	a := w.app
	claude := w.active()
	check(waitUntil(5*time.Second, func() bool { return claude != nil && len(claude.tracker.Blocks(0, 1<<30)) > 0 }), "shell integration in the first shell")
	onMain(func() { w.model.Split(uicore.SplitRow, nil, "") })
	var target *Pane
	check(waitUntil(5*time.Second, func() bool { target = w.active(); return target != nil && target != claude && target.sess != nil }), "a second shell split beside it")
	if target == nil || target == claude {
		w.finish()
		return
	}
	check(waitUntil(5*time.Second, func() bool { return len(target.tracker.Blocks(0, 1<<30)) > 0 }), "shell integration in the target")

	var claudeSession, targetSession string
	onMain(func() {
		claudeSession = w.model.Panes[claude.id].SessionID
		targetSession = w.model.Panes[target.id].SessionID
	})
	a.be.Bridge.SetTarget(claudeSession, targetSession)

	// The target: a stand-in dig, and a command to read back.
	_ = os.RemoveAll("/tmp/brbin")
	target.write("mkdir -p /tmp/brbin && printf '#!/bin/sh\\necho \"$*\" >> /tmp/brbin/calls\\necho \"ANSWER example.com 93.184.216.34\"\\n' > /tmp/brbin/dig && chmod +x /tmp/brbin/dig && export PATH=/tmp/brbin:$PATH\r")
	check(waitUntil(5*time.Second, func() bool {
		ps := w.model.Panes[target.id]
		return ps != nil && strings.HasPrefix(ps.Env["PATH"], "/tmp/brbin") && !ps.Running
	}), "the target reports the stand-in's PATH")
	// Its own line: bash reports only the first command of an && list.
	target.write("echo hello-bridge\r")
	check(waitUntil(5*time.Second, func() bool {
		id, ok := target.tracker.LastFinished()
		b, _ := target.tracker.Info(id)
		return ok && b.Text == "echo hello-bridge"
	}), "the target ran echo")

	// What Claude Code sends: initialize, then requests, one JSON per line.
	// stdin stays open (sleep) until the answers are in.
	session := func(name string, calls ...string) {
		msgs := []string{
			`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"check","version":"1"}}}`,
			`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		}
		msgs = append(msgs, calls...)
		_ = os.WriteFile("/tmp/mcp-"+name+".in", []byte(strings.Join(msgs, "\n")+"\n"), 0o644)
		_ = os.Remove("/tmp/mcp-" + name + ".out")
		claude.write(`(cat /tmp/mcp-` + name + `.in; sleep 6) | "$WTF_MCP" > /tmp/mcp-` + name + `.out 2>/tmp/mcp-` + name + ".err\r")
	}
	answers := func(name string, ids ...int) map[int]json.RawMessage {
		got := map[int]json.RawMessage{}
		waitUntil(20*time.Second, func() bool {
			data, _ := os.ReadFile("/tmp/mcp-" + name + ".out")
			for _, line := range strings.Split(string(data), "\n") {
				var m struct {
					ID     int             `json:"id"`
					Result json.RawMessage `json:"result"`
					Error  json.RawMessage `json:"error"`
				}
				if json.Unmarshal([]byte(line), &m) == nil && m.ID > 0 {
					if m.Result == nil {
						m.Result = m.Error
					}
					got[m.ID] = m.Result
				}
			}
			for _, id := range ids {
				if got[id] == nil {
					return false
				}
			}
			return true
		})
		return got
	}
	type result struct {
		IsError bool `json:"isError"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	text := func(raw json.RawMessage) (string, bool) {
		var r result
		_ = json.Unmarshal(raw, &r)
		if len(r.Content) == 0 {
			return string(raw), true
		}
		return r.Content[0].Text, r.IsError
	}

	session("one",
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"recent_commands","arguments":{"count":3}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"run_readonly","arguments":{"argv":["rm","-rf","/tmp/brbin"],"purpose":"check"}}}`,
	)
	got := answers("one", 2, 3, 4)
	var list struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	_ = json.Unmarshal(got[2], &list)
	var names []string
	for _, t := range list.Tools {
		names = append(names, t.Name)
	}
	slices.Sort(names)
	errText, _ := os.ReadFile("/tmp/mcp-one.err")
	check(slices.Equal(names, []string{"pane_context", "recent_commands", "review_changes", "run_readonly", "type_at_prompt"}),
		"wtf-mcp lists the app's tools: %v (stderr %q)", names, errText)
	recent, isErr := text(got[3])
	check(!isErr && strings.Contains(recent, "echo hello-bridge") && strings.Contains(recent, "hello-bridge\n"),
		"recent_commands reads the target's blocks: %q", recent)
	refused, isErr := text(got[4])
	_, statErr := os.Stat("/tmp/brbin/dig")
	check(isErr && strings.Contains(refused, "isn't on the read-only list") && statErr == nil, "rm is refused and never typed: %q", refused)

	// One call at a time from here: a run, then typing, in the same shell.
	check(waitUntil(15*time.Second, func() bool { ps := w.model.Panes[claude.id]; return ps != nil && !ps.Running }), "the first helper exits")
	session("two", `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"run_readonly","arguments":{"argv":["dig","example.com"],"purpose":"check"}}}`)
	got = answers("two", 2)
	ran, isErr := text(got[2])
	calls, _ := os.ReadFile("/tmp/brbin/calls")
	check(!isErr && strings.Contains(ran, "$ dig example.com") && strings.Contains(ran, "Exit code: 0") && strings.Contains(ran, "93.184.216.34"),
		"run_readonly runs dig in the target and returns its output: %q", ran)
	check(string(calls) == "example.com\n", "the target ran only dig: %q", calls)
	var last string
	onMain(func() {
		if id, ok := target.tracker.LastFinished(); ok {
			b, _ := target.tracker.Info(id)
			last = b.Text
		}
	})
	check(last == "dig example.com", "it's a command block in the target: %q", last)
	w.shot("bridge-run")

	check(waitUntil(15*time.Second, func() bool { ps := w.model.Panes[claude.id]; return ps != nil && !ps.Running }), "the second helper exits")
	session("three", `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"type_at_prompt","arguments":{"command":"echo typed-not-run"}}}`)
	got = answers("three", 2)
	typedText, isErr := text(got[2])
	time.Sleep(300 * time.Millisecond)
	var screen string
	onMain(func() {
		top, _ := target.top()
		for r := top; r < top+target.term.Rows(); r++ {
			screen += target.term.Row(r) + "\n"
		}
	})
	check(!isErr && strings.Contains(typedText, "Nothing ran") && strings.Contains(screen, "echo typed-not-run") &&
		!strings.Contains(screen, "\ntyped-not-run"), "type_at_prompt types without Enter: %q", typedText)
	w.shot("bridge-typed")
	a.be.Bridge.SetTarget(claudeSession, "")
	w.finish()
}

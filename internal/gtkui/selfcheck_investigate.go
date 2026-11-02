//go:build linux

package gtkui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/dayanraj/wtf/internal/uicore"
)

// checkInvestigate (WTF_NATIVE_DEBUG=investigate): the incident
// investigator against a fake Claude API in this process. It asks for a
// read-only command, which waits for Run (nothing runs before) and is then
// typed into the shell (a stand-in kubectl answers); a second one is
// skipped and never typed; one that isn't read-only is refused without
// asking; then it gives a finding with a fix;
// the first request shows what's sent; the fix is typed at the prompt and
// not run. Like the ai check, it writes config.toml, so it runs only with
// XDG_CONFIG_HOME set to a throwaway folder, and puts the file back.
func (w *Window) checkInvestigate() {
	a := w.app
	info := a.be.About.Info()
	if !strings.HasSuffix(info.StateDir, "wtf-native") || os.Getenv("XDG_CONFIG_HOME") == "" || !strings.HasPrefix(info.ConfigFile, os.Getenv("XDG_CONFIG_HOME")) {
		check(false, "the investigate check needs a separate profile and XDG_CONFIG_HOME set to a throwaway folder (config %s)", info.ConfigFile)
		w.finish()
		return
	}
	path := info.ConfigFile
	orig, err := os.ReadFile(path)
	had := err == nil
	defer func() {
		if had {
			_ = os.WriteFile(path, orig, 0o644)
		} else {
			_ = os.Remove(path)
		}
	}()

	// The fake Claude API: each request gets the next reply.
	const fix = "kubectl rollout undo deploy/checkout -n staging"
	replies := []string{
		`{"text":"Listing the pods.","argv":["kubectl","get","pods","-n","staging"]}`,
		`{"text":"Reading its logs.","argv":["kubectl","logs","checkout-1","-n","staging"]}`,
		`{"text":"Restarting it.","argv":["kubectl","delete","pod","checkout-1","-n","staging"]}`,
		`{"text":"Finding: checkout-1 is crashlooping.\nEvidence: [1] shows CrashLoopBackOff.\nNext:\n` + "```sh\\n" + fix + "\\n```" + `"}`,
	}
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		sent, _ := json.Marshal(body["messages"])
		mu.Lock()
		bodies = append(bodies, string(sent))
		n := len(bodies) - 1
		mu.Unlock()
		reply := `{"text":"Done."}`
		if n < len(replies) {
			reply = replies[n]
		}
		var m struct {
			Text string          `json:"text"`
			Argv json.RawMessage `json:"argv"`
		}
		_ = json.Unmarshal([]byte(reply), &m)
		fmt.Fprint(rw, "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":500,\"output_tokens\":0}}}\n\n")
		t, _ := json.Marshal(m.Text)
		fmt.Fprintf(rw, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":%s}}\n\n", t)
		if m.Argv != nil {
			in, _ := json.Marshal(`{"argv":` + string(m.Argv) + `,"purpose":"check"}`)
			fmt.Fprintf(rw, "data: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"c%d\",\"name\":\"run\"}}\n\n", n)
			fmt.Fprintf(rw, "data: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":%s}}\n\n", in)
		}
		fmt.Fprint(rw, "data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":40}}\n\n")
	}))
	defer srv.Close()
	_ = os.Setenv("WTF_CHECK_AI_KEY", "test-key")
	providers := fmt.Sprintf("\n[ai.providers.claude]\nkind = \"anthropic\"\nmodel = \"m\"\nbase-url = %q\napi-key-env = \"WTF_CHECK_AI_KEY\"\n", srv.URL)
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	err = os.WriteFile(path, append(slices.Clone(orig), providers...), 0o644)
	check(err == nil, "wrote a provider to config.toml (%v)", err)
	cfg := a.be.Config
	_ = cfg.SetConfirmPaste(cfg.Current().Config.ConfirmPaste)
	_ = cfg.SetAIProvider(false, "claude")
	check(waitUntil(3*time.Second, func() bool { return a.settings.config().AIDefault == "claude" && w.aiEnabled() }),
		"the provider is loaded: %v", cfg.Current().Problems)

	// A stand-in kubectl on the shell's PATH.
	p := w.active()
	check(waitUntil(5*time.Second, func() bool { return len(p.tracker.Blocks(0, 1<<30)) > 0 }), "shell integration: first prompt marked")
	p.write("mkdir -p /tmp/invbin && printf '#!/bin/sh\\necho \"$*\" >> /tmp/invbin/calls\\necho \"NAME READY STATUS RESTARTS\"\\necho \"checkout-1 0/1 CrashLoopBackOff 7\"\\n' > /tmp/invbin/kubectl && chmod +x /tmp/invbin/kubectl && export PATH=/tmp/invbin:$PATH\r")
	check(waitUntil(5*time.Second, func() bool {
		ps := w.model.Panes[p.id]
		return ps != nil && strings.HasPrefix(ps.Env["PATH"], "/tmp/invbin") && !ps.Running
	}), "the shell reports the stand-in's PATH")

	// The rail shows Investigate once an AI is set up; the command opens it.
	onMain(func() { w.win.ActivateAction("ai.investigate", nil) })
	var tab *investigateTab
	check(waitUntil(3*time.Second, func() bool {
		tab, _ = w.sidePane().(*investigateTab)
		return tab != nil && w.sidebar.buttons[uicore.SidebarInvestigate].IsVisible()
	}), "Investigate (AI)… opens the tab")
	if tab == nil {
		w.finish()
		return
	}
	onMain(func() {
		tab.entry.SetText("why is checkout crashlooping in staging?")
		tab.goClicked()
	})
	var inv *investigation
	check(waitUntil(5*time.Second, func() bool {
		inv = investigations[w]
		return inv != nil && inv.preview != nil
	}), "the first request shows what's sent")
	if inv == nil || inv.preview == nil {
		w.finish()
		return
	}
	time.Sleep(300 * time.Millisecond)
	shotOf(&inv.preview.win.Widget, "investigate-preview")
	onMain(func() { inv.preview.send.Activate() })

	// Each command waits for the user.
	waitStep := func(n int) bool {
		return waitUntil(10*time.Second, func() bool { return inv.pending != nil && inv.pending.Step == n })
	}
	check(waitStep(1), "step 1 waits for Run")
	time.Sleep(500 * time.Millisecond)
	_, err = os.Stat("/tmp/invbin/calls")
	var runShown bool
	onMain(func() { runShown = tab.run.IsVisible() && tab.skip.IsVisible() })
	check(os.IsNotExist(err) && runShown, "nothing ran before Run (%v), Run and Skip are shown", err)
	time.Sleep(200 * time.Millisecond)
	w.shot("investigate-approve")
	onMain(func() { tab.run.Activate() })
	check(waitStep(2), "step 2 waits for Run")
	onMain(func() { tab.skip.Activate() })

	check(waitUntil(20*time.Second, func() bool { return inv.done != nil }), "the investigation ends")
	var d struct {
		finding, fix, err string
		steps, declined   int
	}
	var kinds []string
	var steps []invEntry
	onMain(func() {
		d.finding, d.fix, d.err, d.steps, d.declined = inv.done.Finding, inv.done.Fix, inv.done.Error, inv.done.Steps, inv.done.Declined
		for _, e := range inv.entries {
			kinds = append(kinds, e.kind)
			if e.kind == "step" {
				steps = append(steps, e)
			}
		}
	})
	check(d.err == "" && d.steps == 2 && d.declined == 1 && d.fix == fix, "two steps, one skipped, the fix %q (error %q)", d.fix, d.err)
	check(strings.HasPrefix(d.finding, "Finding: checkout-1 is crashlooping."), "the finding: %q", d.finding)
	check(slices.Contains(kinds, "refused") && len(steps) == 2 && steps[0].line == "kubectl get pods -n staging" &&
		steps[0].state == invFinished && steps[0].block > 0 && steps[1].state == invDeclined, "kinds %q, steps %+v", kinds, steps)
	calls, _ := os.ReadFile("/tmp/invbin/calls")
	check(string(calls) == "get pods -n staging\n", "the shell ran only the allowed command: %q", calls)
	mu.Lock()
	last := ""
	if len(bodies) > 0 {
		last = bodies[len(bodies)-1]
	}
	mu.Unlock()
	check(strings.Contains(last, "CrashLoopBackOff") && strings.Contains(last, "Refused: kubectl delete isn't read-only") &&
		strings.Contains(last, "The user declined to run this command."), "the model got the output, the skip and the refusal")
	mu.Lock()
	first := bodies[0]
	mu.Unlock()
	check(strings.Contains(first, "The last commands in this shell") && strings.Contains(first, "$ mkdir -p /tmp/invbin   (exit 0)"),
		"the question went with the shell's last commands")
	time.Sleep(300 * time.Millisecond)
	w.shot("investigate")

	// The fix is typed at the prompt, not run.
	onMain(func() { tab.fix.Activate() })
	screen := func() string {
		var s string
		onMain(func() {
			top, _ := p.top()
			for r := top; r < top+p.term.Rows(); r++ {
				s += p.term.Row(r) + "\n"
			}
		})
		return s
	}
	// Rows joined: the line wraps at the pane's width.
	joined := func() string { return strings.ReplaceAll(screen(), "\n", "") }
	check(waitUntil(3*time.Second, func() bool { return strings.Contains(joined(), "# "+fix) || strings.Contains(joined(), "$ "+fix) }),
		"the fix is at the prompt:\n%s", screen())
	time.Sleep(500 * time.Millisecond)
	calls, _ = os.ReadFile("/tmp/invbin/calls")
	check(!strings.Contains(string(calls), "rollout"), "and nothing ran it: %q", calls)
	p.write(uicore.ClearLine)
	w.finish()
}

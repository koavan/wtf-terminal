//go:build linux

package gtkui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dayanraj/wtf/internal/uicore"
)

// checkExplain (WTF_NATIVE_DEBUG=explain): AI features on a command line.
// A command fails in the shell;
// its block menu offers Explain Failure…, and the palette command opens the
// dialog. The facts sent hold the command, exit code and output; the first
// send waits for the preview; a command provider answers with a cause and a
// fix in a sh block; Type Fix at Prompt puts the fix at the prompt and runs
// nothing. Then Command from Plain English: the bar asks with the shell's
// facts, Explain Flags follows up, and Enter types the suggestion without
// running it. It writes config.toml, so like the ai check it runs only with
// XDG_CONFIG_HOME set to a throwaway folder, and puts the file back.
func (w *Window) checkExplain() {
	a := w.app
	info := a.be.About.Info()
	if !strings.HasSuffix(info.StateDir, "wtf-native") || os.Getenv("XDG_CONFIG_HOME") == "" || !strings.HasPrefix(info.ConfigFile, os.Getenv("XDG_CONFIG_HOME")) {
		check(false, "the explain check needs a separate profile and XDG_CONFIG_HOME set to a throwaway folder (config %s)", info.ConfigFile)
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
	const fix = "mkdir -p /tmp/wtf-explain-fix"
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.RemoveAll("/tmp/wtf-explain-fix")
	// The provider: a script that answers with a cause and a fix.
	script := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "fixer.sh")
	err = os.WriteFile(script, []byte("cat >/dev/null\nprintf 'The folder is missing.\\n\\n```sh\\n"+fix+"\\n```\\n'\n"), 0o755)
	check(err == nil, "wrote the provider's script (%v)", err)
	providers := "\n[ai.providers.fixer]\nkind = \"command\"\ncommand = [\"sh\", \"" + script + "\"]\n"
	err = os.WriteFile(path, append(slices.Clone(orig), providers...), 0o644)
	check(err == nil, "wrote a provider to config.toml (%v)", err)
	cfg := a.be.Config
	_ = cfg.SetConfirmPaste(cfg.Current().Config.ConfirmPaste)
	err = cfg.SetAIProvider(false, "fixer")
	check(err == nil, "ai-default = fixer (%v)", err)
	check(waitUntil(3*time.Second, func() bool { return a.settings.config().AIDefault == "fixer" && w.aiEnabled() }),
		"the provider is loaded: %v", cfg.Current().Problems)

	p := w.active()
	check(waitUntil(5*time.Second, func() bool { return len(p.tracker.Blocks(0, 1<<30)) > 0 }), "shell integration: first prompt marked")
	p.write("ls /nonexistent-wtf\r")
	var id int
	check(waitUntil(5*time.Second, func() bool {
		var ok bool
		id, ok = p.tracker.LastFinished()
		b, _ := p.tracker.Info(id)
		return ok && b.ExitCode != nil && *b.ExitCode != 0 && !p.feed.busy()
	}), "the command failed")

	var labels []string
	onMain(func() {
		for _, it := range p.blockItems(id) {
			if it != nil {
				labels = append(labels, it.label)
			}
		}
	})
	check(slices.Contains(labels, "Explain Failure…"), "the block menu offers Explain Failure…: %q", labels)

	onMain(func() { w.win.ActivateAction("ai.explain", nil) })
	var d *explainDialog
	check(waitUntil(5*time.Second, func() bool {
		d = explainShown
		return d != nil && d.call != nil && d.call.preview != nil
	}), "the palette command opens it, and the first send shows what's sent")
	if d == nil || d.call == nil || d.call.preview == nil {
		w.finish()
		return
	}
	var facts string
	onMain(func() { facts = d.req.Messages[0].Content })
	for _, want := range []string{"Command: ls /nonexistent-wtf\n", "Exit code: 2\n", "No such file or directory", "System: Linux"} {
		check(strings.Contains(facts, want), "the facts hold %q", want)
	}
	time.Sleep(300 * time.Millisecond)
	shotOf(&d.call.preview.win.Widget, "explain-preview")
	onMain(func() { d.call.preview.send.Activate() })

	check(waitUntil(5*time.Second, func() bool { return d.done != nil }), "the reply arrives")
	var gotFix, shown string
	var canType bool
	onMain(func() {
		gotFix, shown, canType = d.fix, d.shownText(), d.typeFix.Sensitive()
	})
	check(gotFix == fix && canType, "the fix is %q (Type Fix enabled: %v)", gotFix, canType)
	check(shown == "The folder is missing.", "the cause is shown without the fix block: %q", shown)
	time.Sleep(300 * time.Millisecond)
	shotOf(&d.s.win.Widget, "explain")

	onMain(d.typeAtPrompt)
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
	check(waitUntil(3*time.Second, func() bool { return strings.Contains(screen(), "$ "+fix) || strings.Contains(screen(), "# "+fix) }),
		"the fix is at the prompt:\n%s", screen())
	time.Sleep(500 * time.Millisecond)
	_, err = os.Stat("/tmp/wtf-explain-fix")
	var running bool
	onMain(func() { _, running = p.tracker.Running() })
	check(os.IsNotExist(err) && !running, "and nothing ran it (%v, running %v)", err, running)
	var closed bool
	onMain(func() { closed = explainShown == nil })
	check(closed, "the dialog closed")
	p.write(uicore.ClearLine)
	time.Sleep(300 * time.Millisecond)

	// Plain English to command: the bar asks with the shell's facts; the
	// same provider answers, so its fix is the suggestion.
	onMain(func() { w.win.ActivateAction("ai.ask", nil) })
	var b *askBar
	check(waitUntil(3*time.Second, func() bool { b = askBars[w]; return b != nil }), "Command from Plain English opens the bar")
	if b == nil {
		w.finish()
		return
	}
	onMain(func() {
		b.entry.SetText("make the folder")
		b.enter()
	})
	answered := waitUntil(5*time.Second, func() bool { return b.done != nil && b.suggestion != nil })
	check(answered, "the bar gets an answer")
	if !answered {
		w.finish()
		return
	}
	var cmd, sent string
	var previewed bool
	onMain(func() {
		cmd, sent = b.suggestion.Command, b.req.Messages[0].Content
		previewed = b.call != nil && b.call.preview != nil
	})
	check(cmd == fix, "the suggestion is %q", cmd)
	check(strings.Contains(sent, "\nRequest: make the folder\n") && strings.Contains(sent, "System: Linux") && b.req.Fast,
		"the quick AI gets the request with the shell's facts:\n%s", sent)
	check(!previewed, "no preview the second time")
	time.Sleep(300 * time.Millisecond)
	w.shot("ask")

	onMain(b.askFlags)
	check(waitUntil(5*time.Second, func() bool { return b.call == nil && b.flags.Text() != "" }), "Explain Flags answers")

	_ = os.RemoveAll("/tmp/wtf-explain-fix")
	onMain(b.enter) // same text: Enter inserts
	check(waitUntil(3*time.Second, func() bool { return strings.Contains(screen(), "$ "+fix) || strings.Contains(screen(), "# "+fix) }),
		"Enter types the command at the prompt:\n%s", screen())
	time.Sleep(500 * time.Millisecond)
	_, err = os.Stat("/tmp/wtf-explain-fix")
	onMain(func() { _, running = p.tracker.Running(); closed = askBars[w] == nil })
	check(os.IsNotExist(err) && !running && closed, "nothing ran it, and the bar closed (%v, running %v)", err, running)
	p.write(uicore.ClearLine)
	w.finish()
}

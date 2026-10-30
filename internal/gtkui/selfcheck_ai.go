//go:build linux

package gtkui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dayanraj/wtf/internal/ai"
	"github.com/dayanraj/wtf/internal/config"
)

// checkAI (WTF_NATIVE_DEBUG=ai): Settings → AI with two command providers,
// cat (which sends the prompt back) and one that never answers: the list,
// the palette's entries, Test's preview the first time only, the streamed
// reply, Stop, choosing the default, and adding, renaming and deleting one
// through the form. It writes config.toml, so it runs
// only in a separate profile with XDG_CONFIG_HOME set to a throwaway folder
// (linux/check.sh does), and puts the file back.
func (w *Window) checkAI() {
	a := w.app
	info := a.be.About.Info()
	if !strings.HasSuffix(info.StateDir, "wtf-native") || !strings.HasPrefix(info.ConfigFile, os.Getenv("XDG_CONFIG_HOME")) || os.Getenv("XDG_CONFIG_HOME") == "" {
		check(false, "the ai check needs a separate profile and XDG_CONFIG_HOME set to a throwaway folder (config %s)", info.ConfigFile)
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
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	providers := "\n[ai.providers.echo]\nkind = \"command\"\ncommand = [\"cat\"]\n" +
		"\n[ai.providers.slow]\nkind = \"command\"\ncommand = [\"sh\", \"-c\", \"sleep 30\"]\n"
	err = os.WriteFile(path, append(slices.Clone(orig), providers...), 0o644)
	check(err == nil, "wrote two providers to config.toml (%v)", err)
	// Writing a setting reloads the file at once.
	cfg := a.be.Config
	_ = cfg.SetConfirmPaste(cfg.Current().Config.ConfirmPaste)
	err = cfg.SetAIProvider(false, "echo")
	check(err == nil, "ai-default = echo (%v)", err)
	check(waitUntil(3*time.Second, func() bool {
		c := a.settings.config()
		return len(c.AI.Providers) == 2 && c.AIDefault == "echo"
	}), "the providers are loaded: %v", cfg.Current().Problems)

	// The palette offers the one not in use.
	var labels []string
	onMain(func() {
		for _, e := range w.aiEntries() {
			labels = append(labels, e.label)
		}
	})
	check(slices.Equal(labels, []string{"AI: Use slow as Default", "AI: Use slow for Quick Features"}), "palette entries %q", labels)

	// AI Providers opens Settings at the AI page.
	onMain(func() { w.win.ActivateAction("ai.providers", nil) })
	var d *settingsWindow
	check(waitUntil(3*time.Second, func() bool {
		d = settingsWindows[a]
		return d != nil && d.status != nil
	}), "AI Providers opens Settings and lists")
	if d == nil || d.status == nil {
		w.finish()
		return
	}
	var page string
	onMain(func() { page = d.win.VisiblePageName() })
	check(page == "ai", "at the AI page (%q)", page)
	var names []string
	var echoDefault, echoFast bool
	onMain(func() {
		for _, p := range d.status.Providers {
			names = append(names, p.Name)
			if p.Name == "echo" {
				echoDefault, echoFast = p.Default, p.Fast
				key, _ := aiKeyStatus(p)
				check(key == "Uses the tool's own login" && p.Destination == "cat", "echo: %q, runs %q", key, p.Destination)
			}
		}
	})
	check(slices.Equal(names, []string{"echo", "slow"}), "both providers listed: %q", names)
	check(echoDefault && echoFast, "echo is the default and answers quick features")
	time.Sleep(300 * time.Millisecond)
	shotOf(&d.win.Widget, "settings-ai")

	// The first Test shows what's sent; Send sends it.
	test := func() *aiTest {
		var t *aiTest
		onMain(func() { t = d.tests["echo"] })
		return t
	}
	onMain(func() { d.test("echo") })
	var preview *aiPreview
	check(waitUntil(3*time.Second, func() bool {
		if t := d.tests["echo"]; t != nil && t.call != nil {
			preview = t.call.preview
		}
		return preview != nil
	}), "the first request shows what's sent")
	if preview != nil {
		time.Sleep(300 * time.Millisecond)
		shotOf(&preview.win.Widget, "ai-preview")
		onMain(func() { preview.send.Activate() })
	}
	reply := func() (text, shown string, ok bool) {
		onMain(func() {
			if t := d.tests["echo"]; t != nil && t.done != nil {
				text, ok = t.text, t.done.Error == "" && !t.done.Cancelled
				if t.reply != nil {
					shown = t.reply.Text()
				}
			}
		})
		return
	}
	check(waitUntil(5*time.Second, func() bool { return test() != nil && test().done != nil }), "Test ends")
	text, shown, ok := reply()
	check(ok && strings.Contains(text, "You check that a connection works.") && strings.HasSuffix(text, "Reply with the single word OK."),
		"cat sends the prompt back: %q", text)
	check(shown == text, "the reply is shown (%q)", shown)
	shotOf(&d.win.Widget, "settings-ai-tested")

	// Later requests go straight through.
	onMain(func() { d.test("echo") })
	check(waitUntil(5*time.Second, func() bool { t := test(); return t != nil && t.done != nil }), "a second Test ends")
	var previewed bool
	onMain(func() { previewed = test().call.preview != nil })
	_, _, ok = reply()
	check(ok && !previewed, "without a preview")

	// Stop ends a request that doesn't answer.
	onMain(func() { d.test("slow") })
	preview = nil
	waitUntil(3*time.Second, func() bool {
		if t := d.tests["slow"]; t != nil && t.call != nil {
			preview = t.call.preview
		}
		return preview != nil
	})
	if preview != nil {
		onMain(func() { preview.send.Activate() })
	}
	var slow *aiTest
	check(waitUntil(3*time.Second, func() bool { slow = d.tests["slow"]; return slow != nil && slow.call.sent }), "the slow one is sent")
	time.Sleep(300 * time.Millisecond)
	onMain(func() { slow.call.cancel() })
	var cancelled bool
	check(waitUntil(5*time.Second, func() bool {
		cancelled = slow.done != nil && slow.done.Cancelled
		return slow.done != nil
	}) && cancelled, "Stop ends it, cancelled")

	// Use as Default writes ai-default, and the list follows.
	onMain(func() { d.choose(false, "slow") })
	check(waitUntil(5*time.Second, func() bool {
		if d.status == nil {
			return false
		}
		for _, p := range d.status.Providers {
			if p.Name == "slow" {
				return p.Default
			}
		}
		return false
	}), "Use as Default makes slow the default")
	check(a.settings.config().AIDefault == "slow", "ai-default = %q", a.settings.config().AIDefault)

	// Add Provider: the presets, then the form, whose fields follow the kind.
	onMain(d.choosePreset)
	time.Sleep(500 * time.Millisecond)
	shotOf(&d.win.Widget, "settings-presets")
	var custom ai.Preset
	for _, p := range a.be.AI.Presets() {
		if p.ID == "custom-command" {
			custom = p
		}
	}
	check(custom.Kind == "command", "a custom command preset (%q)", custom.Label)
	onMain(func() { d.openForm("", custom.Name, configOf(custom), custom.Help, false) })
	var f *aiForm
	check(waitUntil(2*time.Second, func() bool { f = d.form; return f != nil }), "the form opens")
	if f == nil {
		w.finish()
		return
	}
	fields := func() (api, cmd bool) {
		onMain(func() { api, cmd = f.api.Visible(), f.cmd.Visible() })
		return
	}
	api, cmd := fields()
	check(!api && cmd, "a command shows the Command field only")

	// An OpenAI-compatible server lists its models.
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/v1/models" {
			rw.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("X-Workspace") != "ws-1" {
			rw.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(rw, `{"error":{"message":"this request must include the x-workspace header"}}`)
			return
		}
		_, _ = io.WriteString(rw, `{"data":[{"id":"b-model"},{"id":"a-model"}]}`)
	}))
	defer srv.Close()
	onMain(func() {
		f.kind.SetSelected(1)
		f.baseURL.SetText(srv.URL + "/v1")
	})
	api, cmd = fields()
	check(api && !cmd, "openai shows the connection fields only")
	// Without the header the server says no; with it, it lists.
	onMain(f.fetchModels)
	var note string
	check(waitUntil(5*time.Second, func() bool {
		note = f.modelNote.Text()
		return f.modelNote.HasCSSClass("error") && f.fetch.Sensitive()
	}), "without the extra header, Fetch Models fails inline: %q", note)
	onMain(func() {
		f.headersRow.SetExpanded(true)
		f.headers.Buffer().SetText("X-Workspace: ws-1\n")
	})
	onMain(f.fetchModels)
	var models []string
	check(waitUntil(5*time.Second, func() bool { models = f.fetched; return len(models) > 0 }) &&
		slices.Equal(models, []string{"a-model", "b-model"}), "with it, Fetch Models lists the server's models: %q", models)
	time.Sleep(300 * time.Millisecond)
	shotOf(&d.win.Widget, "settings-form-models")
	saved := func(name string) (config.AIProvider, bool) {
		p, ok := a.settings.config().AI.Providers[name]
		return p, ok
	}

	// An openai provider keeps its headers through Save and Edit.
	onMain(func() {
		f.models.Popdown()
		f.name.SetText("hdr")
		f.model.SetText("a-model")
		f.submit()
	})
	check(waitUntil(5*time.Second, func() bool {
		p, ok := saved("hdr")
		return ok && p.Headers["X-Workspace"] == "ws-1" && d.form == nil
	}), "Save stores the extra header (%q)", f.err.Text())
	check(waitUntil(3*time.Second, func() bool {
		for _, p := range d.status.Providers {
			if p.Name == "hdr" {
				d.edit(p)
				return true
			}
		}
		return false
	}), "and it's listed")
	var hdrText string
	onMain(func() { f = d.form; hdrText = bufferText(f.headers.Buffer()) })
	check(f != nil && strings.TrimSpace(hdrText) == "X-Workspace: ws-1", "Edit shows it (%q)", hdrText)
	onMain(func() {
		for d.win.PopSubpage() {
		}
		d.delete("hdr", "openai")
	})
	check(waitUntil(5*time.Second, func() bool { _, ok := saved("hdr"); return !ok }), "and it's deleted")

	// A command, from the preset again.
	onMain(func() { d.openForm("", custom.Name, configOf(custom), custom.Help, false) })
	check(waitUntil(2*time.Second, func() bool { f = d.form; return f != nil && f.cmd.Visible() }), "the form opens again")
	onMain(func() {
		f.name.SetText("added")
		f.command.SetText("sh -c 'cat -'")
	})
	time.Sleep(300 * time.Millisecond)
	shotOf(&d.win.Widget, "settings-form")
	onMain(f.submit)
	check(waitUntil(5*time.Second, func() bool { _, ok := saved("added"); return ok && d.form == nil }), "Save adds it and closes the form (%q)", f.err.Text())
	p, _ := saved("added")
	check(slices.Equal(p.Command, []string{"sh", "-c", "cat -"}), "the command is split as a shell would: %q", p.Command)

	// Edit: renamed.
	var added ai.ProviderInfo
	check(waitUntil(3*time.Second, func() bool {
		if d.status != nil {
			for _, p := range d.status.Providers {
				if p.Name == "added" {
					added = p
					return true
				}
			}
		}
		return false
	}), "the list shows it")
	onMain(func() { d.edit(added) })
	check(waitUntil(2*time.Second, func() bool { f = d.form; return f != nil }), "Edit opens the form")
	var line string
	onMain(func() { line = f.command.Text() })
	args, _ := config.SplitCommand(line)
	check(slices.Equal(args, p.Command), "with its command (%q)", line)
	onMain(func() {
		f.name.SetText("renamed")
		f.submit()
	})
	check(waitUntil(5*time.Second, func() bool {
		_, old := saved("added")
		_, renamed := saved("renamed")
		return renamed && !old && d.form == nil
	}), "Save renames it")

	// Delete (the part after the confirmation).
	onMain(func() { d.delete("renamed", "command") })
	check(waitUntil(5*time.Second, func() bool { _, ok := saved("renamed"); return !ok }), "Delete removes it")

	onMain(func() { d.win.SetVisiblePageName("general") })
	time.Sleep(300 * time.Millisecond)
	shotOf(&d.win.Widget, "settings-general")

	onMain(func() { d.win.Close() })
	check(waitUntil(2*time.Second, func() bool { return settingsWindows[a] == nil }), "Settings closes")
	w.finish()
}

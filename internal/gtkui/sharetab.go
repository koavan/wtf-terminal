//go:build linux

package gtkui

import (
	"path/filepath"
	"slices"
	"time"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/dayanraj/wtf/internal/uicore"
)

func init() {
	registerSidebarTab(sidebarTabInfo{tab: uicore.SidebarShare, label: "Share & Record", icon: "media-record-symbolic",
		cmd: "sidebar.share", make: func(w *Window) sidePaneTab { return newShareTab(w) }})
}

// shareTab is the Share & Record tab: Hide Secrets, recording the focused
// terminal, saving its output as text or Markdown, and what was saved from
// this window. Like ShareView.svelte and the macOS app's ShareView.
type shareTab struct {
	w     *Window
	root  *gtk.Box
	form  *gtk.Box
	clock coreglib.SourceHandle
	offs  []func()
}

func newShareTab(w *Window) *shareTab {
	t := &shareTab{w: w}
	header := newPaneHeader("Share & Record")
	t.form = gtk.NewBox(gtk.OrientationVertical, 6)
	t.form.AddCSSClass("wtf-share")
	scroll := gtk.NewScrolledWindow()
	scroll.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	scroll.SetVExpand(true)
	scroll.SetChild(t.form)
	t.root = gtk.NewBox(gtk.OrientationVertical, 4)
	t.root.Append(header.root)
	t.root.Append(scroll)
	t.offs = append(t.offs, w.records.observe(t.rebuild), w.app.settings.observe(t.rebuild))
	t.rebuild()
	return t
}

func (t *shareTab) widget() gtk.Widgetter { return t.root }
func (t *shareTab) paneChanged()          { t.rebuild() }
func (t *shareTab) commandFinished()      {}

func (t *shareTab) close() {
	for _, off := range t.offs {
		off()
	}
	if t.clock != 0 {
		coreglib.SourceRemove(t.clock)
		t.clock = 0
	}
}

func shareHeading(icon, text string) *gtk.Box {
	b := gtk.NewBox(gtk.OrientationHorizontal, 5)
	b.AddCSSClass("wtf-share-heading")
	b.Append(gtk.NewImageFromIconName(icon))
	b.Append(sideLabel(text, ""))
	return b
}

func shareButton(label string, run func()) *gtk.Button {
	b := gtk.NewButtonWithLabel(label)
	b.SetHAlign(gtk.AlignStart)
	b.SetFocusOnClick(false)
	b.ConnectClicked(run)
	return b
}

func (t *shareTab) rebuild() {
	w, s, r := t.w, t.w.app.settings, t.w.records
	for c := t.form.FirstChild(); c != nil; c = t.form.FirstChild() {
		t.form.Remove(c)
	}
	add := func(widget gtk.Widgetter) { t.form.Append(widget) }

	// Hide Secrets
	add(shareHeading("security-high-symbolic", "Hide Secrets"))
	sw := gtk.NewSwitch()
	sw.SetActive(s.hide)
	sw.SetVAlign(gtk.AlignCenter)
	sw.NotifyProperty("active", func() {
		if sw.Active() != s.hide {
			if p := w.active(); p != nil {
				p.toggleSecrets()
			} else {
				s.setHide(sw.Active())
			}
		}
	})
	state := "Secrets are shown"
	sub := "Turn on before you share your screen"
	if s.hide {
		state, sub = "Secrets are hidden", "Covered on screen, [REDACTED] when copied or saved"
	}
	texts := gtk.NewBox(gtk.OrientationVertical, 1)
	stateLabel := sideLabel(state, "wtf-share-state")
	if s.hide {
		stateLabel.AddCSSClass("git-added")
	}
	texts.Append(stateLabel)
	texts.Append(wrapLabel(sub, "dim-label"))
	row := gtk.NewBox(gtk.OrientationHorizontal, 8)
	row.Append(sw)
	row.Append(texts)
	add(row)
	startup := gtk.NewCheckButtonWithLabel("Turn on whenever wtf starts")
	startup.SetActive(s.config().RedactSecrets)
	startup.ConnectToggled(func() {
		v, cfg := startup.Active(), w.app.be.Config
		go func() {
			if err := cfg.SetRedactSecrets(v); err != nil {
				onMainAsync(func() { w.toast("Couldn't save the setting: " + err.Error()) })
			}
		}()
	})
	add(startup)
	add(wrapLabel("Access keys, tokens, JWTs, passwords and private keys in every terminal. Best effort — it can miss a secret "+
		"it doesn't recognize. Ctrl+Alt+Shift+R", "wtf-share-note"))

	// Record this terminal
	add(shareHeading("media-record-symbolic", "Record this terminal"))
	p, ps := w.active(), w.model.ActivePane()
	if p != nil && ps != nil {
		target := sideLabel(w.label(p.id), "wtf-share-state")
		target.SetTooltipText(uicore.PrettyPath(ps.Cwd, w.app.home))
		add(target)
		if rec, ok := r.recording[p.id]; ok {
			add(sideLabel("● Recording · "+uicore.ExportElapsed(int(time.Since(rec.since).Seconds())), "git-deleted"))
			path := sideLabel(uicore.PrettyPath(rec.path, w.app.home), "dim-label")
			path.SetTooltipText(rec.path)
			add(path)
			stop := shareButton("Stop Recording", func() { w.stopRecording(p.id) })
			stop.AddCSSClass("destructive-action")
			add(stop)
		} else {
			start := shareButton("● Start Recording…", func() { w.toggleRecording(p) })
			start.AddCSSClass("suggested-action")
			add(start)
		}
		add(wrapLabel("An asciinema (.cast) file of what the terminal shows from now on, with its timing — not what you type. "+
			"Kept as is: Hide Secrets doesn't apply to it. Ctrl+Alt+Shift+S", "wtf-share-note"))
		add(shareHeading("document-save-symbolic", "Save what's there"))
		add(shareButton("Save Output as Text…", func() { w.saveOutputAsText(p) }))
		md := shareButton("Save Commands as Markdown…", func() { w.saveTranscript(p) })
		md.SetSensitive(ps.Integrated)
		if !ps.Integrated {
			md.SetTooltipText("Needs shell integration (bash or zsh)")
		}
		add(md)
		add(wrapLabel("Markdown: each command with its exit status, start time, duration and output — for a postmortem or a ticket.", "wtf-share-note"))
	} else {
		add(wrapLabel("Open a terminal to record it.", "wtf-share-note"))
	}

	// Also recording
	var others []int
	for pane := range r.recording {
		if p == nil || pane != p.id {
			others = append(others, pane)
		}
	}
	slices.Sort(others)
	if len(others) > 0 {
		add(shareHeading("media-record-symbolic", "Also recording"))
		for _, pane := range others {
			rec := r.recording[pane]
			line := gtk.NewBox(gtk.OrientationHorizontal, 4)
			goTo := gtk.NewButtonWithLabel(w.label(pane) + " — " + uicore.ExportElapsed(int(time.Since(rec.since).Seconds())) + " · " + filepath.Base(rec.path))
			goTo.AddCSSClass("flat")
			goTo.SetTooltipText("Show this terminal")
			id := pane
			goTo.ConnectClicked(func() { w.showPane(id) })
			stop := gtk.NewButtonFromIconName("media-playback-stop-symbolic")
			stop.AddCSSClass("flat")
			stop.SetTooltipText("Stop recording")
			stop.ConnectClicked(func() { w.stopRecording(id) })
			line.Append(goTo)
			line.Append(stop)
			add(line)
		}
	}

	// Saved in this window
	if len(r.saved) > 0 {
		add(shareHeading("folder-symbolic", "Saved in this window"))
		for _, f := range r.saved {
			line := gtk.NewBox(gtk.OrientationHorizontal, 4)
			open := gtk.NewButton()
			open.AddCSSClass("flat")
			inner := gtk.NewBox(gtk.OrientationHorizontal, 5)
			icon := "text-x-generic-symbolic"
			if f.kind == savedRecording {
				icon = "media-playback-start-symbolic"
			}
			inner.Append(gtk.NewImageFromIconName(icon))
			inner.Append(sideLabel(filepath.Base(f.path)+" — "+string(f.kind)+" · "+f.at.Format("15:04"), ""))
			open.SetChild(inner)
			open.SetTooltipText(f.path + "\nShow in Files")
			path := f.path
			open.ConnectClicked(func() {
				files := w.app.be.Files
				go func() {
					if err := files.Reveal(path); err != nil {
						onMainAsync(func() { w.toast(uicore.PrettyPath(path, w.app.home) + " isn't there any more") })
					}
				}()
			})
			copyPath := gtk.NewButtonFromIconName("edit-copy-symbolic")
			copyPath.AddCSSClass("flat")
			copyPath.SetTooltipText("Copy path")
			copyPath.ConnectClicked(func() {
				w.win.Clipboard().SetText(path)
				w.toast("Copied path")
			})
			line.Append(open)
			line.Append(copyPath)
			add(line)
		}
	}

	// The elapsed clocks tick only while something records.
	switch recording := len(r.recording) > 0; {
	case recording && t.clock == 0:
		t.clock = coreglib.TimeoutAdd(1000, func() bool {
			t.rebuild()
			return true
		})
	case !recording && t.clock != 0:
		coreglib.SourceRemove(t.clock)
		t.clock = 0
	}
}

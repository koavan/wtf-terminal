//go:build linux

package gtkui

import (
	"fmt"
	"strings"
	"time"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"

	"github.com/dayanraj/wtf/internal/infra"
	"github.com/dayanraj/wtf/internal/uicore"
)

// addressBar is the bar under the tabs: what the focused pane runs, where,
// its context badges (cluster, cloud account, git, credentials expiring)
// and how the last command went. Clicking it never takes the keyboard from
// the terminal. Like AddressBar.svelte and the macOS app's AddressBar.
type addressBar struct {
	w     *Window
	root  *gtk.Box
	box   *gtk.Box
	shape string
	// The expiry clocks move on their own: redrawn every 15s while shown.
	clock coreglib.SourceHandle
	// CPU and memory, when show-usage is on (it shows and hides itself).
	usage *usageReadout
}

func newAddressBar(w *Window) *addressBar {
	a := &addressBar{w: w}
	a.box = gtk.NewBox(gtk.OrientationHorizontal, 8)
	a.box.AddCSSClass("wtf-omnibox")
	a.root = gtk.NewBox(gtk.OrientationHorizontal, 0)
	a.root.AddCSSClass("wtf-address")
	a.root.Append(a.box)
	a.box.SetHExpand(true)
	// Outside the box: refresh empties the box.
	a.usage = newUsageReadout(w)
	a.root.Append(a.usage.widget())
	a.root.SetFocusable(false)
	a.root.SetCanFocus(false)
	return a
}

// refresh shows the active pane, when anything shown changed.
func (a *addressBar) refresh() {
	w := a.w
	m := w.model
	ps := m.ActivePane()
	s := w.app.settings
	var group *uicore.TabGroup
	if t, ok := m.Active(); ok && t.GroupID != 0 {
		if g, ok := m.Group(t.GroupID); ok {
			group = &g
		}
	}
	var badges []uicore.Badge
	var ctx *infra.Context
	if ps != nil && s.config().ContextBadges {
		ctx = w.infra[ps.ID]
		if ctx != nil {
			badges = uicore.BadgesOf(*ctx, time.Now())
		}
	}
	shape := fmt.Sprintf("%v|%v|%v|%v|%v", ps != nil, group, badges, s.hide, s.theme().Dark)
	if ps != nil {
		if rec, ok := w.records.recording[ps.ID]; ok {
			shape += fmt.Sprintf("|rec %d", int(time.Since(rec.since).Seconds()))
		}
	}
	if ps != nil {
		shape += fmt.Sprintf("|%s|%s|%v|%s|%v|%s", ps.Process, ps.Cwd, ps.IsShell, ps.Command, ps.Running, s.text(ps.Command))
		if l := ps.LastCommand; l != nil {
			shape += fmt.Sprintf("|%s|%d|%v", s.text(l.Text), l.ExitCode, l.Duration.Round(100*time.Millisecond))
		}
	}
	if shape == a.shape {
		return
	}
	a.shape = shape

	for c := a.box.FirstChild(); c != nil; c = a.box.FirstChild() {
		a.box.Remove(c)
	}
	if a.clock != 0 {
		glib.SourceRemove(a.clock)
		a.clock = 0
	}
	if ps == nil {
		return
	}
	a.box.SetTooltipText(ps.Cwd)
	if group != nil {
		name := group.Name
		if name == "" {
			name = "Group"
		}
		chip := pill(name, "wtf-pill-group")
		chip.Prepend(dot("group-dot-" + string(group.Color)))
		a.box.Append(chip)
	}
	process := ps.Process
	if process == "" {
		process = "shell"
	}
	class := "wtf-pill-shell"
	if !ps.IsShell {
		class = "wtf-pill-busy"
	}
	a.box.Append(pill(process, class))
	a.box.Append(crumbs(uicore.PrettyPath(ps.Cwd, w.app.home)))
	spacer := gtk.NewBox(gtk.OrientationHorizontal, 0)
	spacer.SetHExpand(true)
	a.box.Append(spacer)

	for _, b := range badges {
		a.box.Append(badgePill(b))
	}
	if ctx != nil && len(ctx.Expiries) > 0 {
		a.clock = coreglib.TimeoutAdd(15000, func() bool {
			a.shape = ""
			a.clock = 0
			a.refresh()
			return false
		})
	}
	if st := a.status(ps); st != nil {
		a.box.Append(st)
	}
	if rec, ok := w.records.recording[ps.ID]; ok {
		b := gtk.NewButton()
		b.SetChild(pill("● REC "+uicore.ExportElapsed(int(time.Since(rec.since).Seconds())), "wtf-pill-fail"))
		b.AddCSSClass("flat")
		b.AddCSSClass("wtf-pill-button")
		b.SetFocusOnClick(false)
		b.SetTooltipText("Recording to " + uicore.PrettyPath(rec.path, w.app.home) + ". Click to stop (Ctrl+Alt+Shift+S)")
		id := ps.ID
		b.ConnectClicked(func() { w.stopRecording(id) })
		a.box.Append(b)
	}
	if s.hide {
		b := gtk.NewButton()
		b.SetChild(pill("Secrets hidden", "wtf-pill-ok"))
		b.AddCSSClass("flat")
		b.AddCSSClass("wtf-pill-button")
		b.SetFocusOnClick(false)
		b.SetTooltipText("Secrets in the output are covered, and replaced with [REDACTED] when copied. Click to show them (Ctrl+Alt+Shift+R)")
		b.ConnectClicked(func() { s.setHide(false) })
		a.box.Append(b)
	}
}

// status is the running command, or how the last one went.
func (a *addressBar) status(ps *uicore.PaneState) gtk.Widgetter {
	s := a.w.app.settings
	if ps.Running && ps.Command != "" {
		p := pill(s.text(ps.Command), "wtf-pill-running")
		p.Prepend(dot("wtf-running-dot"))
		p.SetTooltipText(s.text(ps.Command))
		return p
	}
	last := ps.LastCommand
	if last == nil {
		return nil
	}
	dur := uicore.Duration(last.Duration)
	if last.ExitCode == 0 {
		p := pill("✓ "+dur, "wtf-pill-ok")
		p.SetTooltipText(s.text(last.Text) + " — succeeded")
		return p
	}
	p := pill(fmt.Sprintf("✗ exit %d  %s", last.ExitCode, dur), "wtf-pill-fail")
	p.SetTooltipText(fmt.Sprintf("%s — exit code %d", s.text(last.Text), last.ExitCode))
	return p
}

// pill is a rounded label.
func pill(text, class string) *gtk.Box {
	b := gtk.NewBox(gtk.OrientationHorizontal, 5)
	b.AddCSSClass("wtf-pill")
	b.AddCSSClass(class)
	l := gtk.NewLabel(text)
	l.SetEllipsize(pango.EllipsizeEnd)
	l.SetMaxWidthChars(48)
	b.Append(l)
	return b
}

func dot(class string) *gtk.Box {
	d := gtk.NewBox(gtk.OrientationHorizontal, 0)
	d.AddCSSClass("wtf-dot")
	d.AddCSSClass(class)
	d.SetVAlign(gtk.AlignCenter)
	return d
}

// crumbs shows a path as its folders, the last one stressed.
func crumbs(path string) *gtk.Label {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if strings.HasPrefix(path, "/") {
		parts = append([]string{"/"}, parts...)
	}
	var b strings.Builder
	for i, p := range parts {
		if p == "" {
			continue
		}
		if i > 0 && parts[i-1] != "/" {
			b.WriteString(`<span alpha="45%"> › </span>`)
		} else if i > 0 {
			b.WriteString(" ")
		}
		if i == len(parts)-1 {
			fmt.Fprintf(&b, "<b>%s</b>", escapeMarkup(p))
		} else {
			fmt.Fprintf(&b, `<span alpha="65%%">%s</span>`, escapeMarkup(p))
		}
	}
	l := gtk.NewLabel("")
	l.SetMarkup(b.String())
	l.SetEllipsize(pango.EllipsizeStart)
	l.AddCSSClass("wtf-crumbs")
	return l
}

// badgePill is a context badge: its kind, its value, and the clock of
// credentials that expire.
func badgePill(b uicore.Badge) *gtk.Box {
	p := pill(b.Label, "wtf-badge")
	kind := gtk.NewLabel(b.KindLabel())
	kind.AddCSSClass("wtf-badge-kind")
	p.Prepend(kind)
	p.SetTooltipText(b.Title)
	switch b.Clock.State {
	case uicore.ClockOK:
		p.Append(pill("⏱ "+b.Clock.Text, "wtf-clock"))
	case uicore.ClockSoon:
		p.Append(pill("⏱ "+b.Clock.Text, "wtf-clock-soon"))
	case uicore.ClockExpired:
		p.Append(pill("⏱ expired", "wtf-clock-expired"))
	}
	return p
}

// Contexts

// resolveContext works out a pane's context from what its shell reported:
// off the main loop, the newest request per pane wins, a failure keeps the
// last answer. Like resolveContext in lib/infra.ts.
func (w *Window) resolveContext(paneID int) {
	ps := w.model.Panes[paneID]
	if ps == nil || ps.Env == nil || !w.app.settings.config().ContextBadges {
		return
	}
	w.infraSeq++
	mine := w.infraSeq
	w.infraLatest[paneID] = mine
	env := ps.Env
	resolver := w.app.be.Infra
	go func() {
		ctx := resolver.Resolve(env)
		onMainAsync(func() {
			if w.infraLatest[paneID] != mine {
				return
			}
			delete(w.infraLatest, paneID)
			if w.model.Panes[paneID] == nil {
				return
			}
			w.infra[paneID] = &ctx
			w.address.refresh()
			if sp := w.sidePane(); sp != nil {
				sp.paneChanged()
			}
		})
	}()
}

// refreshContexts resolves every pane's context again (an account lookup
// finished, or badges were turned on).
func (w *Window) refreshContexts() {
	for id := range w.model.Panes {
		if !w.app.settings.config().ContextBadges {
			delete(w.infra, id)
		} else {
			w.resolveContext(id)
		}
	}
	w.address.refresh()
}

//go:build linux

package gtkui

import (
	"context"
	"fmt"
	"strings"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/dayanraj/wtf/internal/review"
	"github.com/dayanraj/wtf/internal/uicore"
)

// menuItem is one entry of a popup menu; a nil run makes a heading (shown
// dimmed), and a nil item starts a new section.
type menuItem struct {
	label string
	run   func()
	// A submenu instead of run.
	sub []*menuItem
}

var menuSection *menuItem

func item(label string, run func()) *menuItem { return &menuItem{label: label, run: run} }

// submenu is an item that opens items.
func submenu(label string, items []*menuItem) *menuItem { return &menuItem{label: label, sub: items} }

// menuPopover is a widget's popup menu: one popover, kept, whose items
// change each time it opens (a popover unparented after closing crashed).
type menuPopover struct {
	at      *gtk.Widget
	pop     *gtk.PopoverMenu
	onClose func()
}

func newMenuPopover(at gtk.Widgetter) *menuPopover {
	m := &menuPopover{at: gtk.BaseWidget(at)}
	m.pop = gtk.NewPopoverMenuFromModel(nil)
	m.pop.SetParent(m.at)
	m.pop.SetHasArrow(false)
	m.pop.ConnectClosed(func() {
		if f := m.onClose; f != nil {
			m.onClose = nil
			f()
		}
	})
	return m
}

// show shows items at a point in the widget. onClose runs when it's gone.
func (m *menuPopover) show(x, y float64, items []*menuItem, onClose func()) {
	group := gio.NewSimpleActionGroup()
	root := buildMenu(group, "a", items)
	// Replaces the group the last menu used.
	m.at.InsertActionGroup("menu", group)
	m.pop.SetMenuModel(root)
	rect := gdk.NewRectangle(int(x), int(y), 1, 1)
	m.pop.SetPointingTo(&rect)
	m.onClose = onClose
	m.pop.Popup()
}

// buildMenu makes items a menu model, adding their actions to group
// (named prefix + index, so submenus' don't clash).
func buildMenu(group *gio.SimpleActionGroup, prefix string, items []*menuItem) *gio.Menu {
	root := gio.NewMenu()
	section := gio.NewMenu()
	flush := func() {
		if section.NItems() > 0 {
			root.AppendSection("", section)
		}
		section = gio.NewMenu()
	}
	for i, it := range items {
		if it == nil {
			flush()
			continue
		}
		name := fmt.Sprintf("%s%d", prefix, i)
		if it.sub != nil {
			section.AppendSubmenu(it.label, buildMenu(group, name+"_", it.sub))
			continue
		}
		act := gio.NewSimpleAction(name, nil)
		if it.run == nil {
			act.SetEnabled(false)
		} else {
			run := it.run
			act.ConnectActivate(func(*glib.Variant) { run() })
		}
		group.AddAction(act)
		section.Append(it.label, "menu."+name)
	}
	flush()
	return root
}

// installMouse adds the pane's clicks: the gutter marks and the right-click
// menu, Ctrl-click on links, middle-click paste through the paste checks.
// They run before VTE sees the click.
func (p *Pane) installMouse() {
	click := gtk.NewGestureClick()
	click.SetButton(0)
	click.SetPropagationPhase(gtk.PhaseCapture)
	click.ConnectPressed(func(n int, x, y float64) {
		button := click.CurrentButton()
		mods := click.CurrentEventState()
		claim := func() { click.SetState(gtk.EventSequenceClaimed) }
		// A program that reads the mouse gets its clicks; Shift gets them to
		// wtf anyway.
		ours := !p.feed.mouse || mods&gdk.ShiftMask != 0
		switch {
		case button == gdk.BUTTON_PRIMARY && n == 1:
			if id, ok := p.over.markAt(x, y); ok {
				claim()
				p.blockMenu(id, x, y)
			} else if mods&gdk.ControlMask != 0 {
				if link := p.term.LinkAt(x, y); link != "" {
					claim()
					p.openLink(link)
				} else if p.tokenClick(x, y) {
					claim()
				}
			}
		case button == gdk.BUTTON_MIDDLE && ours:
			claim()
			p.pasteFrom(p.term.PrimaryClipboard())
		case button == gdk.BUTTON_SECONDARY && ours:
			claim()
			p.contextMenu(x, y)
		}
	})
	p.term.AddController(click)

	// A path dragged from the Files tab is typed at the prompt, quoted.
	drop := gtk.NewDropTarget(coreglib.TypeString, gdk.ActionCopy)
	drop.ConnectDrop(func(v *coreglib.Value, _, _ float64) bool {
		text, _ := v.GoValue().(string)
		path, ok := strings.CutPrefix(text, pathDragPrefix)
		if !ok {
			return false
		}
		p.term.GrabFocus()
		p.term.PasteText(uicore.ShellQuote(path) + " ")
		return true
	})
	p.term.AddController(drop)

	p.term.SetHasTooltip(true)
	p.term.ConnectQueryTooltip(func(x, y int, keyboard bool, tip *gtk.Tooltip) bool {
		if keyboard {
			return false
		}
		if id, ok := p.over.markAt(float64(x), float64(y)); ok {
			tip.SetText(p.over.markTooltip(id))
			return true
		}
		return false
	})
}

// openLink opens web and file links with the desktop's handler.
func (p *Pane) openLink(link string) {
	scheme, _, _ := strings.Cut(link, ":")
	switch strings.ToLower(scheme) {
	case "http", "https", "mailto", "file", "ftp":
	default:
		return
	}
	gtk.NewURILauncher(link).Launch(context.Background(), &p.win.win.Window, nil)
}

// summary is "kubectl get pods · ✗ exit 1 · 3.2s": what the command was and
// how it went.
func summary(b uicore.Block) string {
	text := b.Text
	switch {
	case text == "":
		text = "command"
	case len([]rune(text)) > 48:
		text = string([]rune(text)[:47]) + "…"
	}
	if b.ExitCode == nil {
		return text + " · running"
	}
	parts := []string{text, "✓"}
	if *b.ExitCode != 0 {
		parts[1] = fmt.Sprintf("✗ exit %d", *b.ExitCode)
	}
	return strings.Join(append(parts, uicore.Duration(b.Duration)), " · ")
}

// blockItems are a command block's actions.
func (p *Pane) blockItems(id int) []*menuItem {
	b, ok := p.tracker.Info(id)
	if !ok {
		return nil
	}
	items := []*menuItem{item(p.win.app.settings.text(summary(b)), nil)}
	if b.Text != "" {
		items = append(items, item("Copy Command", func() { p.copyText(b.Text, "command") }))
	}
	if b.HasOutput {
		items = append(items, item("Copy Output", func() {
			out, _ := p.tracker.Output(id)
			p.copyText(out, "output")
		}))
		if b.Text != "" {
			items = append(items, item("Copy Command and Output", func() {
				out, _ := p.tracker.Output(id)
				text := "$ " + b.Text
				if out != "" {
					text += "\n" + out
				}
				p.copyText(text, "command and output")
			}))
		}
	}
	if b.HasOutput {
		label := "Pin Output"
		if b.Running() {
			label = "Pin Output (follows it)"
		}
		items = append(items, item(label, func() { p.win.pinBlock(p, id) }))
	}
	if !b.Running() && b.HasOutput && review.Reviewable(b.Text) {
		items = append(items, menuSection, item("Review Changes…", func() { p.win.reviewBlock(p, id) }))
	}
	if out, _ := p.tracker.Output(id); b.HasOutput && len(strings.FieldsFunc(out, func(r rune) bool { return r == '\n' })) >= 10 {
		items = append(items, menuSection, item("Summarize Logs… (Claude)", func() { p.win.model.FocusPane(p.id); p.win.claudeLogs(id) }))
	}
	if b.ExitCode != nil && *b.ExitCode != 0 {
		items = append(items, menuSection, item("Explain Failure… (Claude)", func() { p.win.explainBlock(p, id) }))
	}
	if b.Text != "" && !b.Running() {
		items = append(items, menuSection, item("Run Again", func() { p.runAgain(b.Text) }),
			item("Runbook from Here… (Claude)", func() { p.win.model.FocusPane(p.id); p.win.claudeRunbook(id) }))
	}
	if b.Text != "" {
		items = append(items, item("Save as Snippet…", func() { p.win.saveAsSnippet(b.Text) }))
	}
	return items
}

func (p *Pane) blockMenu(id int, x, y float64) {
	items := p.blockItems(id)
	if len(items) == 0 {
		return
	}
	p.over.setHighlight(id)
	p.menu.show(x, y, items, p.over.clearHighlight)
}

// contextMenu is the right-click menu: the command block under the mouse,
// then copy, paste and the rest.
func (p *Pane) contextMenu(x, y float64) {
	var items []*menuItem
	if inspect := p.inspectItems(x, y); len(inspect) > 0 {
		items = append(inspect, menuSection)
	}
	if link := p.term.LinkAt(x, y); link != "" {
		items = append(items, item("Open Link", func() { p.openLink(link) }),
			item("Copy Link", func() { p.copyText(link, "link") }), menuSection)
	}
	highlighted := false
	if top, ok := p.top(); ok && !p.feed.alternate {
		_, ch := p.term.CharSize()
		row := top + max(0, int(y)-padTop)/max(1, ch)
		if id, ok := p.tracker.BlockAt(row); ok {
			items = append(append(items, p.blockItems(id)...), menuSection)
			p.over.setHighlight(id)
			highlighted = true
		}
	}
	if p.term.HasSelection() {
		sel := p.term.Selection()
		items = append(items, item("Copy", p.copySelection), item("Pin Selection", func() { p.win.pinSelection(p, sel) }))
	}
	hide := "Hide Secrets"
	if p.win.app.settings.hide {
		hide = "Show Secrets"
	}
	items = append(items,
		item("Paste", p.paste),
		item("Select All", p.term.SelectAll),
		menuSection,
		item("Find…", p.showFind),
		item("Clear", p.clearBuffer),
		menuSection)
	if ps := p.win.model.Panes[p.id]; ps != nil && !p.win.app.claude.isClaude(ps) {
		on := func(f func()) func() { return func() { p.win.model.FocusPane(p.id); f() } }
		items = append(items, item("Ask Claude", on(p.win.askClaude)))
		if !p.feed.alternate {
			items = append(items, item("Command from Plain English…", on(p.win.claudeCommand)), item("Investigate…", on(p.win.claudeInvestigate)))
		}
	}
	items = append(items,
		item(hide, p.toggleSecrets),
		item(map[bool]string{true: "Stop Recording", false: "Start Recording…"}[p.win.isRecording(p.id)],
			func() { p.win.toggleRecording(p) }),
	)
	onClose := func() {}
	if highlighted {
		onClose = p.over.clearHighlight
	}
	p.menu.show(x, y, items, onClose)
}

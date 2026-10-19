//go:build linux

package gtkui

import (
	"time"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/dayanraj/wtf/internal/uicore"
)

// findBar finds text in the terminal's output: as you type, Enter for the
// next match, Shift+Enter for the previous, Escape to close. The overlay
// highlights the matches on screen and outlines the current one. Like
// SearchBar.svelte and the macOS app's SearchBar.
type findBar struct {
	p      *Pane
	box    *gtk.Box
	entry  *gtk.SearchEntry
	status *gtk.Label
	cased  *gtk.ToggleButton

	// Every match in the scrollback (absolute rows), and the selected one.
	found   []uicore.Match
	current int
	// Output arrived: search again a moment later, once however much
	// arrives meanwhile.
	refreshing bool
}

// Rows searched at most, from the bottom.
const findLimit = 50_000

func (p *Pane) showFind() {
	if p.find != nil {
		p.find.entry.GrabFocus()
		return
	}
	f := &findBar{p: p, current: -1}
	f.entry = gtk.NewSearchEntry()
	f.entry.SetPlaceholderText("Find")
	f.entry.SetSizeRequest(200, -1)
	f.status = gtk.NewLabel("")
	f.status.AddCSSClass("dim-label")
	f.cased = gtk.NewToggleButtonWithLabel("Aa")
	f.cased.SetTooltipText("Match case")
	f.cased.AddCSSClass("flat")
	button := func(icon, tip string, run func()) *gtk.Button {
		b := gtk.NewButtonFromIconName(icon)
		b.SetTooltipText(tip)
		b.AddCSSClass("flat")
		b.ConnectClicked(run)
		return b
	}
	f.box = gtk.NewBox(gtk.OrientationHorizontal, 4)
	f.box.AddCSSClass("find-bar")
	f.box.SetHAlign(gtk.AlignEnd)
	f.box.SetVAlign(gtk.AlignStart)
	f.box.SetMarginTop(8)
	f.box.SetMarginEnd(16)
	for _, w := range []gtk.Widgetter{f.entry, f.status, f.cased,
		button("go-up-symbolic", "Previous (Shift+Enter, Ctrl+Shift+H)", f.previous),
		button("go-down-symbolic", "Next (Enter, Ctrl+Shift+G)", f.next),
		button("window-close-symbolic", "Close (Esc)", f.close)} {
		f.box.Append(w)
	}

	f.entry.ConnectSearchChanged(func() { f.search(true) })
	f.cased.ConnectToggled(func() { f.search(true) })
	f.entry.ConnectStopSearch(f.close)
	f.entry.ConnectActivate(f.next)
	keys := gtk.NewEventControllerKey()
	keys.ConnectKeyPressed(func(keyval, _ uint, state gdk.ModifierType) bool {
		if (keyval == gdk.KEY_Return || keyval == gdk.KEY_KP_Enter) && state&gdk.ShiftMask != 0 {
			f.previous()
			return true
		}
		return false
	})
	f.entry.AddController(keys)

	p.find = f
	p.root.AddOverlay(f.box)
	f.entry.GrabFocus()
}

func (f *findBar) query() string       { return f.entry.Text() }
func (f *findBar) caseSensitive() bool { return f.cased.Active() }

func (f *findBar) currentMatch() (uicore.Match, bool) {
	if f.current < 0 || f.current >= len(f.found) {
		return uicore.Match{}, false
	}
	return f.found[f.current], true
}

// search finds every match. Typing more refines the search from the match
// at or after the current one rather than jumping back.
func (f *findBar) search(scroll bool) {
	p := f.p
	from, had := f.currentMatch()
	q := f.query()
	top, known := p.top()
	if q == "" || !known {
		f.found, f.current = nil, -1
		f.status.SetText("")
		p.refresh()
		return
	}
	_, upper, _, _ := p.term.View()
	first := p.firstRow(upper)
	last := p.feed.screenTop + p.term.Rows() - 1
	f.found = uicore.FindIn(p.term, max(first, last-findLimit), last, q, f.caseSensitive())
	switch {
	case len(f.found) == 0:
		f.current = -1
	case had:
		f.current = 0
		for i, m := range f.found {
			if m.Row > from.Row || m.Row == from.Row && m.Col >= from.Col {
				f.current = i
				break
			}
		}
	default:
		// The first match from the bottom of the view up, as a terminal
		// search reads.
		f.current = 0
		bottom := top + p.term.Rows()
		for i, m := range f.found {
			if m.Row < bottom {
				f.current = i
			}
		}
	}
	f.show(scroll)
}

func (f *findBar) next()     { f.step(1) }
func (f *findBar) previous() { f.step(-1) }

func (f *findBar) step(d int) {
	if f.query() == "" {
		return
	}
	if len(f.found) == 0 {
		f.search(true)
	}
	if n := len(f.found); n > 0 {
		cur := f.current
		if cur < 0 {
			cur = -d
		}
		f.current = ((cur+d)%n + n) % n
		f.show(true)
	}
}

// show says where the current match is and scrolls it into view.
func (f *findBar) show(scroll bool) {
	p := f.p
	f.status.SetText(uicore.FindStatus(f.found, f.current))
	if len(f.found) == 0 {
		f.status.AddCSSClass("warning")
	} else {
		f.status.RemoveCSSClass("warning")
	}
	if m, ok := f.currentMatch(); ok && scroll {
		if top, ok := p.top(); ok && (m.Row < top || m.Row >= top+p.term.Rows()) {
			p.scrollToRow(max(0, m.Row-p.term.Rows()/2))
		}
	}
	p.refresh()
}

// outputArrived searches again 300ms after output, keeping the selected
// match and the scroll position.
func (f *findBar) outputArrived() {
	if f.refreshing || f.query() == "" {
		return
	}
	f.refreshing = true
	time.AfterFunc(300*time.Millisecond, func() {
		coreglib.IdleAdd(func() {
			f.refreshing = false
			if f.p.find == f {
				f.search(false)
			}
		})
	})
}

func (f *findBar) close() {
	p := f.p
	p.root.RemoveOverlay(f.box)
	p.find = nil
	p.refresh()
	p.term.GrabFocus()
}

func (p *Pane) findNext() {
	if p.find == nil {
		p.showFind()
		return
	}
	p.find.next()
}

func (p *Pane) findPrevious() {
	if p.find == nil {
		p.showFind()
		return
	}
	p.find.previous()
}

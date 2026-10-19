//go:build linux

package gtkui

import (
	"fmt"
	"strings"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"

	"github.com/dayanraj/wtf/internal/uicore"
)

// overview is Show All Tabs (Ctrl+Shift+\): every tab as a card with the
// last lines of its screen, in strip order, searchable. Enter or a click
// opens one; Backspace with an empty search closes the selected tab. Like
// TabOverview.svelte and the macOS app's TabOverview.
type overview struct {
	w      *Window
	root   *gtk.Box
	search *gtk.SearchEntry
	flow   *gtk.FlowBox
	// Previews are taken once, when the overview opens.
	previews map[int]string
	shown    []int // tab ids, as the cards are
}

func (w *Window) toggleOverview() {
	if o := w.overviewShown; o != nil {
		o.dismiss()
		return
	}
	if w.palette != nil {
		w.palette.dismiss()
	}
	o := &overview{w: w, previews: map[int]string{}}
	w.overviewShown = o
	for _, t := range w.model.Tabs() {
		if p := w.panes[t.FocusedPaneID]; p != nil {
			o.previews[t.ID] = w.app.settings.text(strings.Join(p.lastLines(10), "\n"))
		}
	}
	o.search = gtk.NewSearchEntry()
	o.search.SetPlaceholderText(fmt.Sprintf("Search %d tabs", len(w.model.Tabs())))
	o.search.SetHExpand(true)
	done := gtk.NewButtonWithLabel("Done")
	done.ConnectClicked(o.dismiss)
	header := gtk.NewBox(gtk.OrientationHorizontal, 8)
	header.Append(o.search)
	header.Append(done)

	o.flow = gtk.NewFlowBox()
	o.flow.SetSelectionMode(gtk.SelectionBrowse)
	o.flow.SetMaxChildrenPerLine(4)
	o.flow.SetMinChildrenPerLine(1)
	o.flow.SetColumnSpacing(12)
	o.flow.SetRowSpacing(12)
	o.flow.SetHomogeneous(true)
	o.flow.ConnectChildActivated(func(c *gtk.FlowBoxChild) { o.open(c.Index()) })
	scroll := gtk.NewScrolledWindow()
	scroll.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	scroll.SetVExpand(true)
	scroll.SetChild(o.flow)

	o.root = gtk.NewBox(gtk.OrientationVertical, 12)
	o.root.AddCSSClass("wtf-overview")
	o.root.Append(header)
	o.root.Append(scroll)
	o.search.ConnectSearchChanged(o.fill)
	o.search.ConnectActivate(func() {
		if sel := o.flow.SelectedChildren(); len(sel) > 0 {
			o.open(sel[0].Index())
		}
	})
	o.search.ConnectStopSearch(o.dismiss)
	keys := gtk.NewEventControllerKey()
	keys.SetPropagationPhase(gtk.PhaseCapture)
	keys.ConnectKeyPressed(func(keyval, _ uint, _ gdk.ModifierType) bool {
		switch keyval {
		case gdk.KEY_Escape:
			o.dismiss()
		case gdk.KEY_BackSpace:
			if o.search.Text() != "" {
				return false
			}
			if sel := o.flow.SelectedChildren(); len(sel) > 0 {
				o.w.closeTabs([]int{o.shown[sel[0].Index()]})
				onMainAsync(o.refill)
			}
		case gdk.KEY_Right, gdk.KEY_Left, gdk.KEY_Up, gdk.KEY_Down:
			if sel := o.flow.SelectedChildren(); len(sel) > 0 {
				step := map[uint]int{gdk.KEY_Right: 1, gdk.KEY_Left: -1, gdk.KEY_Down: 4, gdk.KEY_Up: -4}[keyval]
				o.selectIndex(sel[0].Index() + step)
			}
		default:
			return false
		}
		return true
	})
	o.root.AddController(keys)
	o.fill()
	w.overlay.AddOverlay(o.root)
	o.search.GrabFocus()
}

// refill shows the cards again, after a tab closed.
func (o *overview) refill() {
	if o.w.overviewShown == o {
		o.fill()
	}
}

// fill shows the tabs matching the search, best first; all, in strip
// order, when it's empty.
func (o *overview) fill() {
	m := o.w.model
	tabs := m.Tabs()
	if q := strings.TrimSpace(o.search.Text()); q != "" {
		ranked := uicore.FuzzyRank(q, tabs, func(t uicore.Tab) string { return o.w.app.settings.text(m.Title(t)) + " " + o.detail(t) })
		tabs = tabs[:0:0]
		for _, r := range ranked {
			tabs = append(tabs, r.Item)
		}
	}
	for c := o.flow.FirstChild(); c != nil; c = o.flow.FirstChild() {
		o.flow.Remove(c)
	}
	o.shown = o.shown[:0]
	active := 0
	for i, t := range tabs {
		o.flow.Append(o.card(t))
		o.shown = append(o.shown, t.ID)
		if t.ID == m.ActiveID() {
			active = i
		}
	}
	o.selectIndex(active)
}

func (o *overview) detail(t uicore.Tab) string {
	ps := o.w.model.FocusedPane(t)
	if ps == nil {
		return ""
	}
	return uicore.PrettyPath(ps.Cwd, o.w.app.home)
}

func (o *overview) card(t uicore.Tab) *gtk.Box {
	m := o.w.model
	card := gtk.NewBox(gtk.OrientationVertical, 6)
	card.AddCSSClass("wtf-card")
	if g, ok := m.Group(t.GroupID); ok && t.GroupID != 0 {
		card.AddCSSClass("group-" + string(g.Color))
	}
	title := gtk.NewLabel(o.w.app.settings.text(m.Title(t)))
	title.AddCSSClass("wtf-card-title")
	title.SetXAlign(0)
	title.SetEllipsize(pango.EllipsizeEnd)
	detail := gtk.NewLabel(o.detail(t))
	detail.AddCSSClass("dim-label")
	detail.SetXAlign(0)
	detail.SetEllipsize(pango.EllipsizeMiddle)
	preview := gtk.NewLabel(o.previews[t.ID])
	preview.AddCSSClass("wtf-card-preview")
	preview.SetXAlign(0)
	preview.SetYAlign(1)
	preview.SetVExpand(true)
	preview.SetEllipsize(pango.EllipsizeEnd)
	preview.SetLines(10)
	preview.SetSizeRequest(-1, 150)
	card.Append(title)
	card.Append(detail)
	card.Append(preview)
	return card
}

func (o *overview) selectIndex(i int) {
	n := len(o.shown)
	if n == 0 {
		return
	}
	i = max(0, min(n-1, i))
	if c := o.flow.ChildAtIndex(i); c != nil {
		o.flow.SelectChild(c)
	}
}

func (o *overview) open(i int) {
	if i < 0 || i >= len(o.shown) {
		return
	}
	id := o.shown[i]
	o.dismiss()
	o.w.model.Activate(id)
}

func (o *overview) dismiss() {
	w := o.w
	if w.overviewShown != o {
		return
	}
	w.overviewShown = nil
	w.overlay.RemoveOverlay(o.root)
	w.focusActive()
}

// lastLines are the last rows on screen with text, for previews.
func (p *Pane) lastLines(n int) []string {
	top, ok := p.top()
	if !ok {
		return nil
	}
	_, cursor := p.term.Cursor()
	end := max(cursor, top+p.term.Rows()-1)
	for end > top && strings.TrimSpace(p.term.Row(end)) == "" {
		end--
	}
	var out []string
	for r := max(top, end-n+1); r <= end; r++ {
		out = append(out, strings.TrimRight(p.term.Row(r), " "))
	}
	return out
}

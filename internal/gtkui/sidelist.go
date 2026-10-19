//go:build linux

package gtkui

import (
	"strings"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/graphene"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"
)

// sideList is the side pane's lists: sections of rows with an icon, a
// title, a detail line, pills and an optional trailing button; single and
// multiple selection, activation (double-click, Enter), right-click menus,
// and drops of paths from the Files tab. Like the macOS app's SideList.

type sidePill struct {
	text, class, tip string
}

type sideRow struct {
	id    string
	icon  string // a symbolic icon name; "" for none
	dot   string // a CSS class for a colored dot instead of an icon
	title string
	mono  bool
	// A second line, and a second line in red (an error) or a class.
	detail, alert, alertClass string
	monoDetail                bool
	pills                     []sidePill
	tip                       string
	muted, highlighted        bool
	// Indent for trees.
	depth int
	// A trailing button.
	button     string
	buttonTip  string
	buttonRun  func()
	titleClass string // e.g. a git status color
	italic     bool
	strike     bool
	// A letter at the end (git status), or a dot there (a CSS class).
	letter   string
	trailDot string
}

type sideSection struct {
	title string
	tip   string
	rows  []sideRow
	// A collapsible section: its heading toggles it and shows a count.
	toggle func()
	open   bool
	badge  string
	// A note shown in place of rows (empty states).
	empty string
	// A heading shown even with no rows.
	always bool
	// A small group label above the heading.
	group string
}

type sideList struct {
	root   *gtk.ScrolledWindow
	list   *gtk.ListBox
	ids    []string // per list row; "" for headings and notes
	toggle map[int]func()

	onActivate  func(id string)
	onClick     func(id string)
	onMenu      func(ids []string, x, y float64)
	onSelection func(ids []string)
	onDrop      func(id, path string)
	// dragData is the path a row's drag carries ("" for none).
	dragData func(id string) string
	menu     *menuPopover
}

func newSideList(multiple bool) *sideList {
	l := &sideList{toggle: map[int]func(){}}
	l.list = gtk.NewListBox()
	l.list.AddCSSClass("wtf-side-list")
	if multiple {
		l.list.SetSelectionMode(gtk.SelectionMultiple)
	} else {
		l.list.SetSelectionMode(gtk.SelectionSingle)
	}
	l.list.SetActivateOnSingleClick(false)
	l.list.ConnectRowActivated(func(r *gtk.ListBoxRow) {
		i := r.Index()
		if f := l.toggle[i]; f != nil {
			f()
			return
		}
		if id := l.idAt(i); id != "" && l.onActivate != nil {
			l.onActivate(id)
		}
	})
	l.list.ConnectSelectedRowsChanged(func() {
		if l.onSelection != nil {
			l.onSelection(l.selected())
		}
	})
	l.root = gtk.NewScrolledWindow()
	l.root.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	l.root.SetVExpand(true)
	l.root.SetChild(l.list)
	// Not on the list itself: clearing a list box removes every child.
	l.menu = newMenuPopover(l.root)

	click := gtk.NewGestureClick()
	click.SetButton(0)
	click.ConnectPressed(func(n int, x, y float64) {
		row := l.list.RowAtY(int(y))
		if row == nil {
			return
		}
		i := row.Index()
		switch click.CurrentButton() {
		case gdk.BUTTON_PRIMARY:
			if f := l.toggle[i]; f != nil && n == 1 {
				click.SetState(gtk.EventSequenceClaimed)
				f()
				return
			}
			if id := l.idAt(i); id != "" && n == 1 && l.onClick != nil {
				l.onClick(id)
			}
		case gdk.BUTTON_SECONDARY:
			id := l.idAt(i)
			if id == "" || l.onMenu == nil {
				return
			}
			// The right-clicked row joins the selection, or replaces it.
			if !row.IsSelected() {
				l.list.UnselectAll()
				l.list.SelectRow(row)
			}
			// In the menu's coordinates (the scrolled window).
			if pt, ok := l.list.ComputePoint(l.root, graphene.NewPointAlloc().Init(float32(x), float32(y))); ok {
				x, y = float64(pt.X()), float64(pt.Y())
			}
			l.onMenu(l.selected(), x, y)
		}
	})
	l.list.AddController(click)

	drag := gtk.NewDragSource()
	drag.SetActions(gdk.ActionCopy)
	drag.ConnectPrepare(func(_, y float64) *gdk.ContentProvider {
		if l.dragData == nil {
			return nil
		}
		row := l.list.RowAtY(int(y))
		if row == nil {
			return nil
		}
		path := l.dragData(l.idAt(row.Index()))
		if path == "" {
			return nil
		}
		return gdk.NewContentProviderForValue(coreglib.NewValue(pathDragPrefix + path))
	})
	l.list.AddController(drag)

	drop := gtk.NewDropTarget(coreglib.TypeString, gdk.ActionCopy)
	drop.ConnectDrop(func(v *coreglib.Value, _, y float64) bool {
		row := l.list.RowAtY(int(y))
		text, _ := v.GoValue().(string)
		path, ok := strings.CutPrefix(text, pathDragPrefix)
		if row == nil || !ok || l.onDrop == nil {
			return false
		}
		if id := l.idAt(row.Index()); id != "" {
			l.onDrop(id, path)
			return true
		}
		return false
	})
	l.list.AddController(drop)
	return l
}

func (l *sideList) idAt(i int) string {
	if i < 0 || i >= len(l.ids) {
		return ""
	}
	return l.ids[i]
}

func (l *sideList) selected() []string {
	var out []string
	for _, r := range l.list.SelectedRows() {
		if id := l.idAt(r.Index()); id != "" {
			out = append(out, id)
		}
	}
	return out
}

// set shows sections, keeping the selection by id.
func (l *sideList) set(sections []sideSection) {
	keep := map[string]bool{}
	for _, id := range l.selected() {
		keep[id] = true
	}
	l.list.RemoveAll()
	l.ids = l.ids[:0]
	l.toggle = map[int]func(){}
	add := func(w gtk.Widgetter, id string, selectable bool) *gtk.ListBoxRow {
		row := gtk.NewListBoxRow()
		row.SetChild(w)
		row.SetSelectable(selectable)
		row.SetActivatable(true)
		l.list.Append(row)
		l.ids = append(l.ids, id)
		return row
	}
	for _, s := range sections {
		if s.group != "" {
			add(sideLabel(s.group, "wtf-side-group"), "", false)
		}
		switch {
		case s.toggle != nil:
			arrow := "pan-end-symbolic"
			if s.open {
				arrow = "pan-down-symbolic"
			}
			h := gtk.NewBox(gtk.OrientationHorizontal, 4)
			h.AddCSSClass("wtf-side-heading")
			h.Append(gtk.NewImageFromIconName(arrow))
			t := sideLabel(s.title, "")
			t.SetHExpand(true)
			h.Append(t)
			if s.badge != "" {
				h.Append(sideLabel(s.badge, "dim-label"))
			}
			h.SetTooltipText(s.tip)
			add(h, "", false)
			l.toggle[len(l.ids)-1] = s.toggle
			if !s.open {
				continue
			}
		case s.title != "" && (len(s.rows) > 0 || s.always):
			h := sideLabel(s.title, "wtf-side-heading")
			h.SetTooltipText(s.tip)
			add(h, "", false)
		}
		for _, r := range s.rows {
			row := add(sideRowWidget(r), r.id, true)
			if keep[r.id] {
				l.list.SelectRow(row)
			}
		}
		if len(s.rows) == 0 && s.empty != "" {
			add(wrapLabel(s.empty, "wtf-side-note"), "", false)
		}
	}
}

func sideLabel(text, class string) *gtk.Label {
	l := gtk.NewLabel(text)
	l.SetXAlign(0)
	l.SetEllipsize(pango.EllipsizeEnd)
	if class != "" {
		l.AddCSSClass(class)
	}
	return l
}

// wrapLabel is text that wraps to the pane's width instead of being cut.
func wrapLabel(text, class string) *gtk.Label {
	l := sideLabel(text, class)
	l.SetEllipsize(pango.EllipsizeNone)
	l.SetWrap(true)
	l.SetWrapMode(pango.WrapWordChar)
	return l
}

func sideRowWidget(r sideRow) *gtk.Box {
	box := gtk.NewBox(gtk.OrientationHorizontal, 6)
	box.AddCSSClass("wtf-side-row")
	if r.highlighted {
		box.AddCSSClass("highlighted")
	}
	box.SetMarginStart(r.depth * 14)
	switch {
	case r.dot != "":
		box.Append(dot(r.dot))
	case r.icon != "":
		img := gtk.NewImageFromIconName(r.icon)
		img.AddCSSClass("wtf-side-icon")
		box.Append(img)
	}
	lines := gtk.NewBox(gtk.OrientationVertical, 0)
	lines.SetHExpand(true)
	title := sideLabel(r.title, "wtf-side-title")
	if r.mono {
		title.AddCSSClass("monospace")
	}
	if r.muted {
		title.AddCSSClass("dim-label")
	}
	if r.titleClass != "" {
		title.AddCSSClass(r.titleClass)
	}
	if r.italic || r.strike {
		attrs := pango.NewAttrList()
		if r.italic {
			attrs.Insert(pango.NewAttrStyle(pango.StyleItalic))
		}
		if r.strike {
			attrs.Insert(pango.NewAttrStrikethrough(true))
		}
		title.SetAttributes(attrs)
	}
	lines.Append(title)
	if r.detail != "" {
		d := sideLabel(r.detail, "dim-label")
		d.AddCSSClass("wtf-side-detail")
		if r.monoDetail {
			d.AddCSSClass("monospace")
		}
		d.SetEllipsize(pango.EllipsizeMiddle)
		lines.Append(d)
	}
	if r.alert != "" {
		class := r.alertClass
		if class == "" {
			class = "error"
		}
		a := wrapLabel(r.alert, class)
		a.AddCSSClass("wtf-side-detail")
		lines.Append(a)
	}
	box.Append(lines)
	for _, p := range r.pills {
		pl := pill(p.text, "wtf-side-pill")
		if p.class != "" {
			pl.AddCSSClass(p.class)
		}
		pl.SetTooltipText(p.tip)
		pl.SetVAlign(gtk.AlignCenter)
		box.Append(pl)
	}
	if r.trailDot != "" {
		box.Append(dot(r.trailDot))
	}
	if r.letter != "" {
		letter := sideLabel(r.letter, "wtf-side-letter")
		if r.titleClass != "" {
			letter.AddCSSClass(r.titleClass)
		}
		box.Append(letter)
	}
	if r.button != "" && r.buttonRun != nil {
		b := gtk.NewButtonFromIconName(r.button)
		b.AddCSSClass("flat")
		b.AddCSSClass("wtf-side-button")
		b.SetTooltipText(r.buttonTip)
		b.SetFocusOnClick(false)
		b.SetVAlign(gtk.AlignCenter)
		b.ConnectClicked(r.buttonRun)
		box.Append(b)
	}
	box.SetTooltipText(r.tip)
	return box
}

// paneHeader is a side-pane tab's header: a title and small tool buttons.
type paneHeader struct {
	root  *gtk.Box
	title *gtk.Label
	tools *gtk.Box
}

func newPaneHeader(text string) *paneHeader {
	h := &paneHeader{root: gtk.NewBox(gtk.OrientationHorizontal, 2)}
	h.root.AddCSSClass("wtf-pane-header")
	h.title = sideLabel(strings.ToUpper(text), "wtf-pane-title")
	h.title.SetHExpand(true)
	h.title.SetEllipsize(pango.EllipsizeMiddle)
	h.tools = gtk.NewBox(gtk.OrientationHorizontal, 0)
	h.root.Append(h.title)
	h.root.Append(h.tools)
	return h
}

func (h *paneHeader) setTitle(text, tip string) {
	h.title.SetText(strings.ToUpper(text))
	h.title.SetTooltipText(tip)
}

// tool adds a small icon button.
func (h *paneHeader) tool(icon, tip string, run func()) *gtk.Button {
	b := gtk.NewButtonFromIconName(icon)
	b.AddCSSClass("flat")
	b.AddCSSClass("wtf-tool")
	b.SetTooltipText(tip)
	b.SetFocusOnClick(false)
	b.ConnectClicked(run)
	h.tools.Append(b)
	return b
}

// What a path dragged from the Files tab carries.
const pathDragPrefix = "wtf-path "

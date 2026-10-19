//go:build linux

package gtkui

import (
	"fmt"
	"strings"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/dayanraj/wtf/internal/uicore"
)

// tabPage is a tab's panes: its split tree as nested GtkPaned, or just the
// zoomed pane. It's rebuilt when the tree's shape changes; a ratio change
// only moves a divider.
type tabPage struct {
	w     *Window
	tab   int
	root  *gtk.Box
	shape string
	// The dividers, by their path in the tree.
	dividers map[string]*divider
}

// divider is a split's GtkPaned, kept at its ratio as the page resizes.
type divider struct {
	paned *gtk.Paned
	path  []bool
	ratio float64
	// Setting the position ourselves, not a drag.
	applying bool
}

func newTabPage(w *Window, tab int) *tabPage {
	pg := &tabPage{w: w, tab: tab, root: gtk.NewBox(gtk.OrientationVertical, 0), dividers: map[string]*divider{}}
	pg.root.SetHExpand(true)
	pg.root.SetVExpand(true)
	return pg
}

// shapeOf is a tree's shape: its splits and panes, not its ratios.
func shapeOf(l *uicore.Layout) string {
	if !l.IsSplit() {
		return fmt.Sprint(l.Pane)
	}
	return fmt.Sprintf("%s(%s,%s)", l.Dir, shapeOf(l.A), shapeOf(l.B))
}

func pathKey(path []bool) string {
	var b strings.Builder
	for _, p := range path {
		b.WriteByte("ab"[map[bool]int{false: 0, true: 1}[p]])
	}
	return b.String()
}

func (pg *tabPage) update(t uicore.Tab) {
	shape := shapeOf(t.Root)
	if t.ZoomedPaneID != 0 {
		shape = fmt.Sprintf("zoom %d", t.ZoomedPaneID)
	}
	if shape != pg.shape {
		pg.shape = shape
		pg.rebuild(t)
	}
	for _, s := range t.Root.Splits() {
		if d := pg.dividers[pathKey(s.Path)]; d != nil && d.ratio != s.Ratio {
			d.ratio = s.Ratio
			d.apply()
		}
	}
	// Panes other than the focused one are dimmed while the tab is split.
	dim := pg.w.app.settings.config().UnfocusedSplitDim
	for _, id := range t.Root.Leaves() {
		if p := pg.w.panes[id]; p != nil {
			p.setDimmed(t.Root.IsSplit() && t.ZoomedPaneID == 0 && id != t.FocusedPaneID, dim)
		}
	}
}

// rebuild lays the tree out again with the panes' existing terminals.
func (pg *tabPage) rebuild(t uicore.Tab) {
	for _, id := range t.Root.Leaves() {
		if p := pg.w.panes[id]; p != nil {
			detachWidget(&p.root.Widget)
		}
	}
	if child := pg.root.FirstChild(); child != nil {
		pg.root.Remove(child)
	}
	pg.dividers = map[string]*divider{}
	var w gtk.Widgetter
	if t.ZoomedPaneID != 0 {
		w = pg.leaf(t.ZoomedPaneID)
	} else {
		w = pg.build(t.Root, nil)
	}
	if w != nil {
		pg.root.Append(w)
	}
}

func (pg *tabPage) leaf(id int) gtk.Widgetter {
	p := pg.w.panes[id]
	if p == nil {
		return gtk.NewBox(gtk.OrientationVertical, 0)
	}
	return p.root
}

func (pg *tabPage) build(l *uicore.Layout, path []bool) gtk.Widgetter {
	if !l.IsSplit() {
		return pg.leaf(l.Pane)
	}
	orient := gtk.OrientationHorizontal
	if l.Dir == uicore.SplitColumn {
		orient = gtk.OrientationVertical
	}
	paned := gtk.NewPaned(orient)
	paned.SetResizeStartChild(true)
	paned.SetResizeEndChild(true)
	paned.SetShrinkStartChild(false)
	paned.SetShrinkEndChild(false)
	paned.SetStartChild(pg.build(l.A, append(append([]bool{}, path...), false)))
	paned.SetEndChild(pg.build(l.B, append(append([]bool{}, path...), true)))
	d := &divider{paned: paned, path: append([]bool{}, path...), ratio: l.Ratio}
	pg.dividers[pathKey(path)] = d
	// The size is known once allocated (max-position changes then).
	paned.NotifyProperty("max-position", d.apply)
	paned.NotifyProperty("position", func() {
		if d.applying || !paned.Mapped() {
			return
		}
		if max := paned.ObjectProperty("max-position").(int); max > 0 {
			r := uicore.ClampRatio(float64(paned.Position()) / float64(max))
			if diff := r - d.ratio; diff > 0.002 || diff < -0.002 {
				d.ratio = r
				pg.w.model.SetRatio(pg.tab, d.path, r)
			}
		}
	})
	return paned
}

// apply moves the divider to its ratio.
func (d *divider) apply() {
	max := d.paned.ObjectProperty("max-position").(int)
	if max <= 0 {
		return
	}
	d.applying = true
	d.paned.SetPosition(int(d.ratio*float64(max) + 0.5))
	d.applying = false
}

func (pg *tabPage) destroy() {
	pg.w.stack.Remove(pg.root)
}

// detachWidget takes a widget out of whatever holds it, so it can go
// elsewhere.
func detachWidget(w *gtk.Widget) {
	parent := w.Parent()
	if parent == nil {
		return
	}
	switch p := parent.(type) {
	case *gtk.Paned:
		if p.StartChild() != nil && sameWidget(p.StartChild(), w) {
			p.SetStartChild(nil)
		} else {
			p.SetEndChild(nil)
		}
	case *gtk.Box:
		p.Remove(w)
	case *gtk.Stack:
		p.Remove(w)
	default:
		w.Unparent()
	}
}

func sameWidget(a gtk.Widgetter, b *gtk.Widget) bool {
	return gtk.BaseWidget(a).Object.Native() == b.Object.Native()
}

//go:build linux

package gtkui

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/diamondburned/gotk4/pkg/cairo"
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pangocairo"

	"github.com/dayanraj/wtf/internal/config"
	"github.com/dayanraj/wtf/internal/uicore"
)

// overlay draws over a terminal: each command's mark in the left gutter
// (click it for the command's actions), how long slow commands took, the
// block a menu is about, Find's matches, and covers over secrets while
// Hide Secrets is on. It takes no clicks itself (the pane's gesture finds
// marks), and it's hidden while it has nothing to draw, so streaming output
// costs it one scan per frame and no drawing.
type overlay struct {
	p         *Pane
	area      *gtk.DrawingArea
	scheduled bool

	// What's on screen, rows counted from the top of the view.
	marks     []uicore.RowBlock
	covers    []uicore.Cell
	found     []uicore.Match
	current   *uicore.Match
	highlight *[2]int // absolute rows of the block a menu is about

	cover, accent uicore.RGB
	dark          bool
}

func newOverlay(p *Pane) *overlay {
	o := &overlay{p: p, area: gtk.NewDrawingArea()}
	o.area.SetCanTarget(false)
	o.area.SetHExpand(true)
	o.area.SetVExpand(true)
	o.area.SetVisible(false)
	o.area.SetDrawFunc(o.draw)
	return o
}

func (o *overlay) setTheme(th config.Theme) {
	o.cover = uicore.CoverColor(th.Foreground, th.Background)
	o.accent = uicore.Accent(th.Cursor, th.Background, th.Palette[:], th.Dark)
	o.dark = th.Dark
	o.area.QueueDraw()
}

// schedule updates the overlay once per pass of the main loop.
func (o *overlay) schedule() {
	if o.scheduled {
		return
	}
	o.scheduled = true
	coreglib.IdleAdd(func() {
		o.scheduled = false
		o.update()
	})
}

func (o *overlay) update() {
	p := o.p
	top, ok := p.top()
	if !ok {
		return
	}
	rows := p.term.Rows()
	var marks []uicore.RowBlock
	if !p.feed.alternate {
		for _, m := range p.tracker.Blocks(top, top+rows-1) {
			m.Row -= top
			marks = append(marks, m)
		}
	}
	var covers []uicore.Cell
	if p.win.app.settings.hide {
		covers = uicore.SecretCells(p.term, top, rows)
	}
	var found []uicore.Match
	var current *uicore.Match
	if f := p.find; f != nil {
		found = uicore.FindIn(p.term, top, top+rows-1, f.query(), f.caseSensitive())
		for i := range found {
			found[i].Row -= top
		}
		if c, ok := f.currentMatch(); ok && c.Row >= top && c.Row < top+rows {
			c.Row -= top
			current = &c
		}
	}
	changed := !slices.EqualFunc(marks, o.marks, sameMark) || !slices.Equal(covers, o.covers) ||
		!slices.Equal(found, o.found) || (current == nil) != (o.current == nil) ||
		current != nil && *current != *o.current
	o.marks, o.covers, o.found, o.current = marks, covers, found, current
	if changed {
		o.redraw()
	}
}

func sameMark(a, b uicore.RowBlock) bool {
	ea, eb := a.Block.ExitCode, b.Block.ExitCode
	return a.Row == b.Row && a.Block.ID == b.Block.ID && (ea == nil) == (eb == nil) &&
		(ea == nil || *ea == *eb) && a.Block.Duration == b.Block.Duration
}

func (o *overlay) redraw() {
	o.area.SetVisible(len(o.marks) > 0 || len(o.covers) > 0 || len(o.found) > 0 || o.highlight != nil)
	o.area.QueueDraw()
}

func (o *overlay) setHighlight(id int) {
	if first, last, ok := o.p.tracker.BlockRows(id); ok {
		o.highlight = &[2]int{first, last}
		o.redraw()
	}
}

func (o *overlay) clearHighlight() {
	if o.highlight != nil {
		o.highlight = nil
		o.redraw()
	}
}

// cell is the rectangle of a run of cells on screen, in the overlay.
func (o *overlay) cell(row, col, width int) (x, y, w, h float64) {
	cw, ch := o.p.term.CharSize()
	return float64(padLeft + col*cw), float64(padTop + row*ch), float64(width * cw), float64(ch)
}

// markAt is the command whose mark is at a point in the gutter.
func (o *overlay) markAt(x, y float64) (int, bool) {
	if x >= padLeft {
		return 0, false
	}
	for _, m := range o.marks {
		_, my, _, mh := o.cell(m.Row, 0, 1)
		if m.Block.ExitCode != nil && y >= my && y < my+mh {
			return m.Block.ID, true
		}
	}
	return 0, false
}

// markTooltip is a mark's tooltip, like the other builds'.
func (o *overlay) markTooltip(id int) string {
	b, ok := o.p.tracker.Info(id)
	if !ok || b.ExitCode == nil {
		return ""
	}
	s := "Succeeded"
	if *b.ExitCode != 0 {
		s = fmt.Sprintf("Failed with exit code %d", *b.ExitCode)
	}
	s += " · took " + uicore.Duration(b.Duration)
	s += " · finished " + b.FinishedAt.Format("15:04:05")
	return s + "\nClick for this command's actions"
}

func setRGBA(cr *cairo.Context, c uicore.RGB, alpha float64) {
	cr.SetSourceRGBA(c.R/255, c.G/255, c.B/255, alpha)
}

func roundedRect(cr *cairo.Context, x, y, w, h, r float64) {
	r = min(r, w/2, h/2)
	cr.NewPath()
	cr.Arc(x+w-r, y+r, r, -math.Pi/2, 0)
	cr.Arc(x+w-r, y+h-r, r, 0, math.Pi/2)
	cr.Arc(x+r, y+h-r, r, math.Pi/2, math.Pi)
	cr.Arc(x+r, y+r, r, math.Pi, 3*math.Pi/2)
	cr.ClosePath()
}

func (o *overlay) draw(_ *gtk.DrawingArea, cr *cairo.Context, _, _ int) {
	p := o.p
	cols := p.term.Columns()
	_, ch := p.term.CharSize()
	if h := o.highlight; h != nil {
		if top, ok := p.top(); ok {
			from, to := max(0, h[0]-top), min(p.term.Rows()-1, h[1]-top)
			if from <= to {
				x, y, w, _ := o.cell(from, 0, cols)
				roundedRect(cr, x, y, w, float64((to-from+1)*ch), 4)
				setRGBA(cr, o.accent, map[bool]float64{true: 0.16, false: 0.12}[o.dark])
				cr.Fill()
			}
		}
	}

	ok := uicore.RGB{R: 0x2f, G: 0x9e, B: 0x6a}
	fail := uicore.RGB{R: 0xd9, G: 0x30, B: 0x36}
	muted := uicore.RGB{R: 61, G: 61, B: 61}
	mutedAlpha := 0.5
	if o.dark {
		ok, fail = uicore.RGB{R: 0x5b, G: 0xb9, B: 0x8c}, uicore.RGB{R: 0xe5, G: 0x48, B: 0x4d}
		muted, mutedAlpha = uicore.RGB{R: 235, G: 235, B: 235}, 0.45
	}
	top, _ := p.top()
	for _, m := range o.marks {
		code := m.Block.ExitCode
		if code == nil {
			continue
		}
		_, y, _, h := o.cell(m.Row, 0, 1)
		roundedRect(cr, 4, y+h*0.22, 3, h*0.56, 1.5)
		if *code == 0 {
			setRGBA(cr, ok, 0.75)
		} else {
			setRGBA(cr, fail, 0.95)
		}
		cr.Fill()
		// How long a slow command took, at the end of its prompt line, when
		// there's room.
		if m.Block.Duration < uicore.DurationLabel {
			continue
		}
		label := uicore.Duration(m.Block.Duration)
		width := utf8.RuneCountInString(label) + 2
		used := utf8.RuneCountInString(strings.TrimRight(p.term.Row(top+m.Row), " "))
		if used+width+2 > cols {
			continue
		}
		layout := o.area.CreatePangoLayout("")
		layout.SetMarkup(fmt.Sprintf(`<span size="smaller">%s</span>`, label))
		lw, lh := layout.PixelSize()
		x, _, w, _ := o.cell(m.Row, cols-width, width)
		cw, _ := p.term.CharSize()
		cr.MoveTo(x+w-float64(lw)-float64(cw), y+(h-float64(lh))/2)
		setRGBA(cr, muted, mutedAlpha)
		pangocairo.ShowLayout(cr, layout)
	}

	// Every match on screen, the selected one outlined.
	for _, f := range o.found {
		x, y, w, h := o.cell(f.Row, f.Col, f.Width)
		roundedRect(cr, x, y, w, h, 2)
		setRGBA(cr, o.accent, map[bool]float64{true: 0.28, false: 0.22}[o.dark])
		cr.Fill()
	}
	if c := o.current; c != nil {
		x, y, w, h := o.cell(c.Row, c.Col, c.Width)
		roundedRect(cr, x+0.75, y+0.75, w-1.5, h-1.5, 2)
		setRGBA(cr, o.accent, 1)
		cr.SetLineWidth(1.5)
		cr.Stroke()
	}

	for _, c := range o.covers {
		x, y, w, h := o.cell(c.Row, c.Col, c.Width)
		// A pixel more each side: anti-aliased glyphs spill out of their cells.
		cr.Rectangle(x-1, y, w+2, h)
		setRGBA(cr, o.cover, 1)
		cr.Fill()
	}
}

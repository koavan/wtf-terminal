//go:build linux

// Package vte wraps VTE's GTK 4 terminal widget for gotk4, which ships no
// VTE bindings. Only what wtf uses is wrapped.
package vte

// #cgo pkg-config: vte-2.91-gtk4
// #include "shim.h"
// #include <stdlib.h>
import "C"

import (
	"runtime"
	"unsafe"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// Terminal is a VteTerminal (our WtfTerm subclass).
type Terminal struct {
	*gtk.Widget
	t *C.VteTerminal
}

// New creates a terminal widget.
func New() *Terminal {
	p := C.wtf_term_new()
	obj := coreglib.Take(unsafe.Pointer(p))
	w := &gtk.Widget{
		InitiallyUnowned: coreglib.InitiallyUnowned{Object: obj},
		Object:           obj,
		Accessible:       gtk.Accessible{Object: obj},
		Buildable:        gtk.Buildable{Object: obj},
		ConstraintTarget: gtk.ConstraintTarget{Object: obj},
	}
	return &Terminal{Widget: w, t: (*C.VteTerminal)(unsafe.Pointer(p))}
}

// Feed hands output to the terminal, as if it came from the PTY. VTE only
// queues it: the bytes are parsed in the next frame clock "update" phase.
func (t *Terminal) Feed(b []byte) {
	if len(b) == 0 {
		return
	}
	C.vte_terminal_feed(t.t, (*C.char)(unsafe.Pointer(&b[0])), C.gssize(len(b)))
	runtime.KeepAlive(b)
}

// Cursor is the cursor's column and absolute row (scrollback included).
func (t *Terminal) Cursor() (col, row int) {
	var c, r C.glong
	C.vte_terminal_get_cursor_position(t.t, &c, &r)
	return int(c), int(r)
}

func (t *Terminal) Columns() int { return int(C.vte_terminal_get_column_count(t.t)) }
func (t *Terminal) Rows() int    { return int(C.vte_terminal_get_row_count(t.t)) }

// Text returns the text from (startRow, startCol) up to, not including,
// (endRow, endCol). Rows are absolute. VTE ends a row with '\n' only when
// the next row is in the range and the row isn't soft-wrapped into it.
func (t *Terminal) Text(startRow, startCol, endRow, endCol int) string {
	p := C.wtf_term_text(t.t, C.glong(startRow), C.glong(startCol), C.glong(endRow), C.glong(endCol))
	if p == nil {
		return ""
	}
	defer C.g_free(C.gpointer(p))
	return C.GoString(p)
}

// Row is the text of one row, without a line end.
func (t *Terminal) Row(row int) string { return t.Text(row, 0, row, t.Columns()) }

// EndsLine reports whether row ends a line: it isn't soft-wrapped into the
// next one. (Rows that left the scrollback count as line ends.)
func (t *Terminal) EndsLine(row int) bool {
	s := t.Text(row, 0, row+1, 0)
	return len(s) > 0 && s[len(s)-1] == '\n'
}

func (t *Terminal) SetFont(desc string) {
	cs := C.CString(desc)
	defer C.free(unsafe.Pointer(cs))
	fd := C.pango_font_description_from_string(cs)
	defer C.pango_font_description_free(fd)
	C.vte_terminal_set_font(t.t, fd)
}

func (t *Terminal) SetColors(fg, bg string) {
	cf, cb := C.CString(fg), C.CString(bg)
	defer C.free(unsafe.Pointer(cf))
	defer C.free(unsafe.Pointer(cb))
	C.wtf_term_set_colors(t.t, cf, cb)
}

func (t *Terminal) SetScrollback(lines int) {
	C.vte_terminal_set_scrollback_lines(t.t, C.glong(lines))
}

// SetSize asks for a grid of cols × rows (sets the natural size).
func (t *Terminal) SetSize(cols, rows int) {
	C.vte_terminal_set_size(t.t, C.glong(cols), C.glong(rows))
}

// OnCommit is called with the bytes for the child: keys typed, or a paste.
func (t *Terminal) OnCommit(f func(text string)) {
	t.Object.Connect("commit", func(text string, _ uint) { f(text) })
}

// OnGridChanged is called after the grid's column or row count changed.
func (t *Terminal) OnGridChanged(f func(cols, rows int)) {
	t.Object.Connect("grid-changed", func(cols, rows int) { f(cols, rows) })
}

// OnBeforeAllocate is called before each size allocation, while the rows
// are still wrapped for the old width.
func (t *Terminal) OnBeforeAllocate(f func(width int)) {
	t.Object.Connect("before-allocate", func(width int) { f(width) })
}

// OnTitle is called when a program sets the window title.
func (t *Terminal) OnTitle(f func(title string)) {
	t.Object.Connect("window-title-changed", func() {
		p := C.vte_terminal_get_window_title(t.t)
		if p == nil {
			f("")
			return
		}
		f(C.GoString(p))
	})
}

// Copy puts the selection on the clipboard; Paste sends the clipboard to the
// child (through "commit", bracketed when the program asked for it).
func (t *Terminal) Copy()  { C.vte_terminal_copy_clipboard_format(t.t, C.VTE_FORMAT_TEXT) }
func (t *Terminal) Paste() { C.vte_terminal_paste_clipboard(t.t) }

func (t *Terminal) SelectAll() { C.vte_terminal_select_all(t.t) }

// FontScale is the zoom: 1 is the font's own size.
func (t *Terminal) FontScale() float64         { return float64(C.vte_terminal_get_font_scale(t.t)) }
func (t *Terminal) SetFontScale(scale float64) { C.vte_terminal_set_font_scale(t.t, C.double(scale)) }

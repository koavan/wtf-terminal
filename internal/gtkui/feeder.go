//go:build linux

package gtkui

import (
	"bytes"
	"sync"
	"time"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"

	"github.com/dayanraj/wtf/internal/gtkui/vte"
)

// feeder moves PTY output into VTE in step with the frame clock.
//
// vte_terminal_feed only queues bytes; VTE parses its whole queue in the
// frame clock's "update" phase. So output is fed in "before-paint" (same
// frame, no added latency), at most one budget's worth per frame, which is
// also the backpressure: the reader blocks while too much is waiting, and
// the shell stalls on a full PTY. A feed is cut right after an OSC 133
// mark, so that in the next frame, before anything else is fed, the cursor
// is exactly where the mark was.
type feeder struct {
	term  *vte.Terminal
	marks *marks

	mu        sync.Mutex
	drained   *sync.Cond
	pending   []byte
	scheduled bool // the main loop has been told there's output

	// Main loop only.
	clock    *gdk.FrameClock
	updating bool
	budget   int
	awaiting byte // the kind of the mark fed last frame, 0 if none
	started  time.Time
}

const (
	minBudget  = 16 << 10
	maxBudget  = 4 << 20
	maxPending = 8 << 20
	// The time a frame may spend on output (parsing and painting it).
	frameBudget = 20 * time.Millisecond
)

func newFeeder(term *vte.Terminal, m *marks) *feeder {
	f := &feeder{term: term, marks: m, budget: 256 << 10}
	f.drained = sync.NewCond(&f.mu)
	term.ConnectRealize(func() {
		f.clock = gdk.BaseFrameClock(term.FrameClock())
		f.clock.ConnectBeforePaint(f.beforePaint)
		f.clock.ConnectAfterPaint(f.afterPaint)
		f.mu.Lock()
		has := len(f.pending) > 0
		f.mu.Unlock()
		if has {
			f.start()
		}
	})
	return f
}

// push is called from the reader goroutine; it blocks while the terminal
// is behind.
func (f *feeder) push(chunk []byte) {
	f.mu.Lock()
	for len(f.pending) > maxPending {
		f.drained.Wait()
	}
	f.pending = append(f.pending, chunk...)
	schedule := !f.scheduled
	f.scheduled = true
	f.mu.Unlock()
	if schedule {
		coreglib.IdleAdd(f.start)
	}
}

func (f *feeder) start() {
	if f.clock == nil || f.updating {
		return
	}
	f.updating = true
	f.clock.BeginUpdating()
}

func (f *feeder) beforePaint() {
	if !f.updating {
		return
	}
	f.started = time.Now()
	if f.awaiting != 0 {
		col, row := f.term.Cursor()
		f.marks.add(f.awaiting, row, col)
		f.awaiting = 0
	}

	f.mu.Lock()
	n := min(len(f.pending), f.budget)
	part := f.pending[:n]
	kind := byte(0)
	switch start, end, k, ok := nextMark(f.pending, n); {
	case ok:
		part, kind = f.pending[:end], k
	case start >= 0 && len(f.pending)-start < 1024:
		// A mark that hasn't fully arrived: feed up to it and wait.
		part = f.pending[:start]
	}
	// No copy: the reader only appends past the end of pending, and VTE
	// copies what it's fed.
	out := part
	f.pending = f.pending[len(part):]
	if len(f.pending) == 0 {
		f.pending = nil
	}
	idle := len(f.pending) == 0 && kind == 0
	if idle {
		f.scheduled = false
	}
	f.drained.Broadcast()
	f.mu.Unlock()

	f.term.Feed(out)
	f.awaiting = kind
	if idle {
		// The last bytes are queued; VTE parses them this frame.
		f.updating = false
		f.clock.EndUpdating()
	}
}

// afterPaint adapts the budget so a frame spends about frameBudget on output.
func (f *feeder) afterPaint() {
	if f.started.IsZero() {
		return
	}
	took := time.Since(f.started)
	f.started = time.Time{}
	if took <= 0 {
		return
	}
	target := int(float64(f.budget) * float64(frameBudget) / float64(took))
	f.budget = max(minBudget, min(maxBudget, (f.budget+target)/2))
}

// nextMark finds the first OSC 133 sequence that starts within b[:limit].
// It returns where it starts, where it ends (just past its terminator) and
// its kind (A, B, C or D). ok is false when there's none, or when it hasn't
// fully arrived: then start is its index (or -1 when there's none).
func nextMark(b []byte, limit int) (start, end int, kind byte, ok bool) {
	const intro = "\x1b]133;"
	i := bytes.Index(b[:min(len(b), limit+len(intro))], []byte(intro))
	if i < 0 || i >= limit {
		return -1, 0, 0, false
	}
	rest := b[i+len(intro):]
	if len(rest) == 0 {
		return i, 0, 0, false
	}
	for j := 0; j < len(rest); j++ {
		switch rest[j] {
		case 0x07:
			return i, i + len(intro) + j + 1, rest[0], true
		case 0x1b:
			if j+1 < len(rest) {
				return i, i + len(intro) + j + 2, rest[0], true
			}
			return i, 0, 0, false
		}
	}
	return i, 0, 0, false
}

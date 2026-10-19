//go:build linux

package gtkui

import (
	"bytes"
	"regexp"
	"strconv"
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
// the shell stalls on a full PTY.
//
// Shell integration rides along:
//   - a feed is cut right after an OSC 133 mark, so that in the next frame,
//     before anything else is fed, the cursor is exactly where the mark was;
//   - OSC 633 (the command line, the shell's context) is handed over as it's
//     fed: it needs no position, and comes before the mark that uses it.
//
// And where the screen is: VTE doesn't say which absolute row its view
// starts at once old rows are trimmed, so after each feed a cursor position
// report (CSI 6n) is fed too. VTE answers through "commit", the cursor's row
// on screen, which with its absolute row gives the screen's first row. The
// answer never reaches the shell.
type feeder struct {
	term *vte.Terminal
	// onMark gets an OSC 133 mark (what follows "133;") with the cursor
	// where it was; on633 what follows "633;"; onView a new screen origin.
	onMark func(data string, col, row int)
	on633  func(data string)
	onView func()

	mu        sync.Mutex
	drained   *sync.Cond
	pending   []byte
	scheduled bool // the main loop has been told there's output

	// Main loop only.
	clock    *gdk.FrameClock
	updating bool
	budget   int
	awaiting string // the mark fed last frame, "" if none
	started  time.Time
	// The cursor position report in flight: how many reports the program
	// asked for first (theirs come back first), and the screen's first row
	// once known.
	probing, wantView bool
	skipReports       int
	probeAge          int
	screenTop         int
	viewKnown         bool
	// Modes programs set: a full-screen program (vim, less) has the
	// alternate screen; mouse reporting; bracketed paste.
	alternate, mouse, bracketed bool
}

const (
	minBudget  = 16 << 10
	maxBudget  = 4 << 20
	maxPending = 8 << 20
	// The time a frame may spend on output (parsing and painting it).
	frameBudget = 20 * time.Millisecond
	// The cursor position report.
	reportRequest = "\x1b[6n"
)

func newFeeder(term *vte.Terminal) *feeder {
	f := &feeder{term: term, budget: 256 << 10}
	f.drained = sync.NewCond(&f.mu)
	term.ConnectRealize(func() {
		f.clock = gdk.BaseFrameClock(term.FrameClock())
		f.clock.ConnectBeforePaint(f.beforePaint)
		f.clock.ConnectAfterPaint(f.afterPaint)
		f.mu.Lock()
		has := len(f.pending) > 0
		f.mu.Unlock()
		if has || f.wantView {
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

// requestView finds the screen's first row again (after a resize).
func (f *feeder) requestView() {
	f.wantView = true
	f.start()
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
	if f.awaiting != "" {
		col, row := f.term.Cursor()
		mark := f.awaiting
		f.awaiting = ""
		if f.onMark != nil {
			f.onMark(mark, col, row)
		}
	}

	f.mu.Lock()
	part, mark, osc633 := cut(f.pending, min(len(f.pending), f.budget))
	// No copy: the reader only appends past the end of pending, and VTE
	// copies what it's fed.
	out := part
	f.pending = f.pending[len(part):]
	if len(f.pending) == 0 {
		f.pending = nil
	}
	idle := len(f.pending) == 0 && mark == ""
	if idle {
		f.scheduled = false
	}
	f.drained.Broadcast()
	f.mu.Unlock()

	for _, d := range osc633 {
		if f.on633 != nil {
			f.on633(d)
		}
	}
	f.term.Feed(out)
	f.trackModes(out)
	f.awaiting = mark
	if f.probing {
		// An answer that never came (VTE dropped it): ask again.
		if f.probeAge++; f.probeAge > 30 {
			f.probing = false
		}
	}
	if (len(out) > 0 || f.wantView) && !f.probing && safeToReport(out) {
		f.probeAge = 0
		f.probing, f.wantView = true, false
		f.skipReports = bytes.Count(out, []byte(reportRequest))
		f.term.Feed([]byte(reportRequest))
	}
	if idle {
		// The last bytes are queued; VTE parses them this frame.
		f.updating = false
		f.clock.EndUpdating()
	}
}

// cut takes the part of b to feed this frame, at most about limit bytes:
// up to and including the first OSC 133 mark (returned, without "133;"),
// or up to a mark or 633 that hasn't fully arrived. It also returns the
// 633 sequences in the part, in order.
func cut(b []byte, limit int) (part []byte, mark string, osc633 []string) {
	part = b[:limit]
	for from := 0; from < len(part); {
		start, end, code, data, ok := nextOSC(b, from, len(part))
		switch {
		case start < 0:
			return part, "", osc633
		case !ok && len(b)-start < 1024:
			// Not fully here: feed up to it and wait.
			return b[:start], "", osc633
		case !ok:
			// Too long to be a mark: let VTE have it.
			from = start + 1
		case code == "633":
			osc633 = append(osc633, data)
			if end > len(part) {
				part = b[:end]
			}
			from = end
		default:
			return b[:end], data, osc633
		}
	}
	return part, "", osc633
}

// nextOSC finds the first OSC 133 or 633 sequence starting in b[from:limit].
// It returns where it starts and ends (just past its terminator), its code
// and what follows "<code>;". ok is false when there's none (start -1) or
// it hasn't fully arrived.
func nextOSC(b []byte, from, limit int) (start, end int, code, data string, ok bool) {
	for i := from; i < limit; {
		j := bytes.Index(b[i:min(len(b), limit+6)], []byte("\x1b]"))
		if j < 0 || i+j >= limit {
			return -1, 0, "", "", false
		}
		start = i + j
		rest := b[start+2:]
		if len(rest) < 4 {
			return start, 0, "", "", false
		}
		switch c := string(rest[:4]); c {
		case "133;", "633;":
			code = c[:3]
		default:
			i = start + 2
			continue
		}
		body := rest[4:]
		for k := 0; k < len(body); k++ {
			switch body[k] {
			case 0x07:
				return start, start + 6 + k + 1, code, string(body[:k]), true
			case 0x1b:
				if k+1 < len(body) {
					return start, start + 6 + k + 2, code, string(body[:k]), true
				}
				return start, 0, code, "", false
			}
		}
		return start, 0, code, "", false
	}
	return -1, 0, "", "", false
}

var modeSwitch = regexp.MustCompile(`\x1b\[\?(1049|1047|47|1000|1002|1003|2004)([hl])`)

// trackModes follows the modes programs set in b (DECSET/DECRST, one mode
// per sequence, as programs send them). A reset clears them all.
func (f *feeder) trackModes(b []byte) {
	if bytes.Contains(b, []byte("\x1bc")) {
		f.alternate, f.mouse, f.bracketed = false, false, false
	}
	if !bytes.Contains(b, []byte("\x1b[?")) {
		return
	}
	for _, m := range modeSwitch.FindAllSubmatch(b, -1) {
		on := m[2][0] == 'h'
		switch string(m[1]) {
		case "1049", "1047", "47":
			f.alternate = on
		case "1000", "1002", "1003":
			f.mouse = on
		case "2004":
			f.bracketed = on
		}
	}
}

// safeToReport: a request split at the end of the part would be counted
// wrong, so no report follows a part ending in a partial escape.
func safeToReport(out []byte) bool {
	return bytes.IndexByte(out[max(0, len(out)-5):], 0x1b) < 0
}

var reportAnswer = regexp.MustCompile(`^\x1b\[(\d+);(\d+)R$`)

// takeReport looks at what VTE sends the child: true when it's the answer
// to our report request, which is used here and not sent on.
func (f *feeder) takeReport(text string) bool {
	if !f.probing || len(text) > 16 {
		return false
	}
	m := reportAnswer.FindStringSubmatch(text)
	if m == nil {
		return false
	}
	if f.skipReports > 0 {
		f.skipReports--
		return false
	}
	f.probing = false
	row, _ := strconv.Atoi(m[1])
	_, cursor := f.term.Cursor()
	f.screenTop, f.viewKnown = cursor-(row-1), true
	if f.onView != nil {
		f.onView()
	}
	return true
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

// busy says output is waiting or being fed.
func (f *feeder) busy() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.pending) > 0 || f.updating
}

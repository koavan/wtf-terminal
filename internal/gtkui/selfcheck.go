//go:build linux

package gtkui

// Self-checks, run headless (Xvfb) with WTF_NATIVE_DEBUG:
//
//	app       the backend runs, events arrive on the main loop, shortcuts
//	          reach their commands before VTE sees the keys (needs xdotool)
//	snapshot  saves each window as a PNG in $WTF_SNAPSHOT_DIR (default
//	          bin/gtk/snapshots), to check layouts without a desktop
//	marks     OSC 133 marks stay on their prompts across feeds, rewraps
//	          and scrollback trimming (no shell; synthetic output)
//	signals   the shell's signal mask is empty; Ctrl-C and resizes reach
//	          programs. Prints the pid of a `sleep` left running, so the
//	          caller can check that quitting killed it.
//
// Each prints PASS/FAIL lines and quits; the exit status is 1 on a failure.

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/diamondburned/gotk4/pkg/glib/v2"

	"github.com/dayanraj/wtf/internal/events"
)

// selfCheck runs the check WTF_NATIVE_DEBUG names, on its own goroutine.
func (a *App) selfCheck(w *Window) {
	switch a.debug {
	case "app":
		w.checkApp()
	case "snapshot":
		w.checkSnapshot()
	case "marks":
		w.checkMarks()
	case "signals":
		w.checkSignals()
	default:
		fmt.Printf("unknown WTF_NATIVE_DEBUG=%q\n", a.debug)
		failed = true
		w.finish()
	}
}

// waitUntil polls cond on the main loop.
func waitUntil(d time.Duration, cond func() bool) bool {
	for end := time.Now().Add(d); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		var ok bool
		onMain(func() { ok = cond() })
		if ok {
			return true
		}
	}
	return false
}

// xkey types keys into the focused window, as a person would.
func xkey(keys ...string) error {
	return exec.Command("xdotool", append([]string{"key", "--clearmodifiers"}, keys...)...).Run()
}

func (w *Window) checkApp() {
	a := w.app
	info := a.be.About.Info()
	check(info.Version == a.build.Version, "backend answers: version %q", info.Version)
	check(strings.HasSuffix(info.StateDir, "wtf-native"), "separate profile: state in %s", info.StateDir)

	// Events emitted on other goroutines arrive in order, on the main loop.
	var got []any
	var onLoop bool
	onMain(func() {
		a.events.on("selfcheck", func(data any) {
			got = append(got, data)
			onLoop = onLoop || glib.MainContextDefault().IsOwner()
		})
	})
	for i := range 100 {
		events.Emit("selfcheck", i)
	}
	ok := waitUntil(2*time.Second, func() bool { return len(got) == 100 })
	inOrder := true
	for i, v := range got {
		inOrder = inOrder && v == i
	}
	check(ok && inOrder && onLoop, "100 events delivered in order on the main loop (%d)", len(got))

	// The shell is up; shortcuts beat VTE to the keys.
	check(waitUntil(5*time.Second, func() bool { return w.pane.sess != nil && len(a.be.Terminal.ShellPIDs()) == 1 }), "shell started")
	time.Sleep(time.Second)
	var scale float64
	err := xkey("ctrl+plus")
	check(err == nil && waitUntil(2*time.Second, func() bool { scale = w.pane.term.FontScale(); return scale > 1 }),
		"Ctrl+Plus zooms in (scale %.1f, %v)", scale, err)
	_ = xkey("ctrl+0")
	check(waitUntil(2*time.Second, func() bool { return w.pane.term.FontScale() == 1 }), "Ctrl+0 resets the zoom")
	_ = xkey("ctrl+shift+n")
	check(waitUntil(3*time.Second, func() bool { return len(a.windows) == 2 }), "Ctrl+Shift+N opens a window")
	check(waitUntil(3*time.Second, func() bool { return len(a.be.Terminal.ShellPIDs()) == 2 }), "with its own shell")
	_ = xkey("ctrl+shift+w")
	check(waitUntil(3*time.Second, func() bool { return len(a.windows) == 1 }), "Ctrl+Shift+W closes it")
	check(waitUntil(3*time.Second, func() bool { return len(a.be.Terminal.ShellPIDs()) == 1 }), "and its shell")

	// Typing still reaches the shell: Ctrl+letter is the shell's.
	var mu sync.Mutex
	var out bytes.Buffer
	w.pane.tap = func(b []byte) {
		mu.Lock()
		out.Write(b)
		mu.Unlock()
	}
	_ = xkey("e", "c", "h", "o", "space", "w", "t", "f", "minus", "o", "k", "Return")
	seen := func(s string) bool {
		mu.Lock()
		defer mu.Unlock()
		return bytes.Contains(out.Bytes(), []byte(s))
	}
	check(waitUntil(3*time.Second, func() bool { return seen("wtf-ok\r\n") }), "typed command ran")
	_ = xkey("ctrl+d")
	check(waitUntil(3*time.Second, func() bool { return len(a.windows) == 0 }), "Ctrl+D ends the shell and closes the window")
	w.finish()
}

func (w *Window) checkSnapshot() {
	dir := os.Getenv("WTF_SNAPSHOT_DIR")
	if dir == "" {
		dir = "bin/gtk/snapshots"
	}
	_ = os.MkdirAll(dir, 0o755)
	time.Sleep(2 * time.Second) // the shell's prompt
	var path string
	var err error
	onMain(func() { path, err = w.snapshot(filepath.Join(dir, w.name+".png")) })
	check(err == nil, "snapshot %s (%v)", path, err)
	w.finish()
}

var failed bool

func check(ok bool, format string, args ...any) {
	status := "PASS"
	if !ok {
		status, failed = "FAIL", true
	}
	fmt.Printf("%s %s\n", status, fmt.Sprintf(format, args...))
}

func (w *Window) finish() {
	onMain(w.app.gtk.Quit)
	if failed {
		os.Exit(1)
	}
}

// settle waits until all output is fed and parsed.
func (w *Window) settle() {
	for {
		var busy bool
		onMain(func() {
			w.pane.feed.mu.Lock()
			busy = len(w.pane.feed.pending) > 0 || w.pane.feed.updating
			w.pane.feed.mu.Unlock()
		})
		if !busy {
			time.Sleep(100 * time.Millisecond) // VTE parses the last feed this frame
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func promptBlock(k int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\x1b]133;A\a$ cmd-%d\x1b]133;B\a\r\n\x1b]133;C\a", k)
	for j := 0; j < 3; j++ {
		// 150 characters: wraps at 80 columns.
		fmt.Fprintf(&b, "%-150s\r\n", strings.Repeat(fmt.Sprintf("out-%d-%d ", k, j), 25)[:150])
	}
	b.WriteString("\x1b]133;D;0\a")
	return b.String()
}

// verify checks that every prompt mark sits on its "$ cmd-k" line.
func (w *Window) verify(stage string) {
	var bad []string
	var n int
	onMain(func() {
		k := -1
		for _, mk := range w.pane.marks.list {
			if mk.kind != 'A' {
				continue
			}
			n++
			line := w.pane.term.Row(mk.row)
			// The prompt number: marks are in order, but trimming drops
			// the oldest, so read it from the first mark.
			var got int
			if _, err := fmt.Sscanf(line, "$ cmd-%d", &got); err != nil {
				bad = append(bad, fmt.Sprintf("row %d: %q", mk.row, strings.TrimSpace(line)))
				continue
			}
			if k >= 0 && got != k+1 {
				bad = append(bad, fmt.Sprintf("row %d: cmd-%d after cmd-%d", mk.row, got, k))
			}
			k = got
		}
	})
	check(len(bad) == 0 && n > 0, "%s: %d prompt marks on their lines %v", stage, n, bad)
}

func (w *Window) resizeTo(width int) (cols int, took time.Duration) {
	var before int
	onMain(func() { before = w.pane.term.Columns() })
	start := time.Now()
	onMain(func() { w.win.SetDefaultSize(width, 600) })
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		onMain(func() { cols = w.pane.term.Columns() })
		if cols != before {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cols, time.Since(start)
}

func (w *Window) checkMarks() {
	time.Sleep(500 * time.Millisecond) // mapped and allocated
	var cols int
	onMain(func() { cols = w.pane.term.Columns() })
	fmt.Printf("grid %d columns\n", cols)

	const prompts = 40
	var all bytes.Buffer
	for k := 0; k < prompts; k++ {
		all.WriteString(promptBlock(k))
	}
	// One push, so the feeder has to cut it at every mark.
	w.pane.feed.push(all.Bytes())
	w.settle()
	var n int
	onMain(func() { n = len(w.pane.marks.list) })
	check(n == prompts*4, "recorded %d marks for %d prompts", n, prompts)
	w.verify("after feeding")

	c, took := w.resizeTo(480)
	w.settle()
	check(c < cols, "narrower: %d → %d columns in %v", cols, c, took.Round(time.Millisecond))
	w.verify("after rewrap to narrower")

	c2, took := w.resizeTo(1200)
	w.settle()
	check(c2 > c, "wider: %d → %d columns in %v", c, c2, took.Round(time.Millisecond))
	w.verify("after rewrap to wider")

	// Scrollback trimming: a small scrollback, then lots of output.
	onMain(func() { w.pane.term.SetScrollback(300) })
	var more bytes.Buffer
	for k := prompts; k < prompts+60; k++ {
		more.WriteString(promptBlock(k))
	}
	w.pane.feed.push(more.Bytes())
	w.settle()
	var row0 string
	var total int
	onMain(func() {
		row0 = w.pane.term.Row(0)
		total = len(w.pane.marks.list)
		w.pane.marks.prune()
	})
	check(row0 == "", "scrollback trimmed: row 0 is gone (%q)", row0)
	w.verify("after trimming")
	var kept int
	onMain(func() { kept = len(w.pane.marks.list) })
	fmt.Printf("marks kept %d of %d\n", kept, total)

	c3, took := w.resizeTo(600)
	w.settle()
	check(c3 < c2, "rewrap after trimming: %d → %d columns in %v", c2, c3, took.Round(time.Millisecond))
	w.verify("after trimming and rewrap")
	w.finish()
}

func procStatus(pid int, field string) string {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, field+":"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// childNamed finds a process with the given name whose parent is ppid.
func childNamed(ppid int, name string) int {
	entries, _ := os.ReadDir("/proc")
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		if procStatus(pid, "Name") == name && procStatus(pid, "PPid") == strconv.Itoa(ppid) {
			return pid
		}
	}
	return 0
}

func (w *Window) checkSignals() {
	var mu sync.Mutex
	var out bytes.Buffer
	w.pane.tap = func(b []byte) {
		mu.Lock()
		out.Write(b)
		mu.Unlock()
	}
	waitFor := func(s string, d time.Duration) bool {
		for end := time.Now().Add(d); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
			mu.Lock()
			ok := bytes.Contains(out.Bytes(), []byte(s))
			mu.Unlock()
			if ok {
				return true
			}
		}
		return false
	}
	time.Sleep(1500 * time.Millisecond) // the shell's startup files
	var shell int
	for pid := range w.app.be.Terminal.ShellPIDs() {
		shell = pid
	}
	blk, ign := procStatus(shell, "SigBlk"), procStatus(shell, "SigIgn")
	check(strings.Trim(blk, "0") == "", "shell %d signal mask empty (SigBlk %s)", shell, blk)
	fmt.Printf("shell SigIgn %s\n", ign)

	// Ctrl-C stops a foreground program.
	_, _ = w.pane.sess.Write([]byte("sleep 300\r"))
	time.Sleep(500 * time.Millisecond)
	sleeper := childNamed(shell, "sleep")
	check(sleeper > 0, "sleep 300 running (pid %d)", sleeper)
	check(strings.Trim(procStatus(sleeper, "SigBlk"), "0") == "", "its signal mask is empty")
	_, _ = w.pane.sess.Write([]byte{0x03})
	time.Sleep(300 * time.Millisecond)
	check(procStatus(sleeper, "Name") == "", "Ctrl-C stopped it")

	// A resize reaches the program in the foreground.
	_, _ = w.pane.sess.Write([]byte(`sh -c 'trap "echo GOT-WINCH-\$(tput cols)" WINCH; while :; do sleep 0.1; done'` + "\r"))
	time.Sleep(500 * time.Millisecond)
	onMain(func() { w.win.SetDefaultSize(600, 500) })
	check(waitFor("GOT-WINCH-", 3*time.Second), "SIGWINCH reached the program")
	_, _ = w.pane.sess.Write([]byte{0x03})
	time.Sleep(300 * time.Millisecond)

	// Left running: quitting must kill it (the caller checks).
	_, _ = w.pane.sess.Write([]byte("sleep 777\r"))
	time.Sleep(500 * time.Millisecond)
	fmt.Printf("left running: sleep pid %d\n", childNamed(shell, "sleep"))
	w.finish()
}

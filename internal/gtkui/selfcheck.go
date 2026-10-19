//go:build linux

package gtkui

// Self-checks, run headless (Xvfb) with WTF_NATIVE_DEBUG:
//
//	app       the backend runs, events arrive on the main loop, shortcuts
//	          reach their commands before VTE sees the keys (needs xdotool)
//	snapshot  saves each window as a PNG in $WTF_SNAPSHOT_DIR (default
//	          bin/gtk/snapshots), to check layouts without a desktop
//	terminal  command blocks, copy last output, Hide Secrets covers, find,
//	          paste confirmation, clear, prompt jumps; snapshots of each
//	view      the overlay's rows match VTE's view once old rows are trimmed
//	marks     OSC 133 marks stay on their prompts across feeds, rewraps
//	          and scrollback trimming (no shell; synthetic output)
//	signals   the shell's signal mask is empty; Ctrl-C and resizes reach
//	          programs. Prints the pid of a `sleep` left running, so the
//	          caller can check that quitting killed it.
//
// Each prints PASS/FAIL lines and quits; the exit status is 1 on a failure.

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/diamondburned/gotk4/pkg/glib/v2"

	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/graphene"

	"github.com/dayanraj/wtf/internal/events"
	"github.com/dayanraj/wtf/internal/uicore"
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
	case "view":
		w.checkView()
	case "terminal":
		w.checkTerminal()
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

// xclick clicks a mouse button at a point in this app's window.
func xclick(x, y, button int) {
	ids, _ := exec.Command("xdotool", "search", "--onlyvisible", "--pid", strconv.Itoa(os.Getpid())).Output()
	if f := strings.Fields(string(ids)); len(f) > 0 {
		_ = exec.Command("xdotool", "mousemove", "--window", f[0], strconv.Itoa(x), strconv.Itoa(y),
			"click", strconv.Itoa(button)).Run()
	}
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
	var size float64
	base := a.settings.config().FontSize
	err := xkey("ctrl+plus")
	check(err == nil && waitUntil(2*time.Second, func() bool { size = a.settings.fontSize; return size == base+1 }),
		"Ctrl+Plus makes the text bigger (%g → %g, %v)", base, size, err)
	_ = xkey("ctrl+0")
	check(waitUntil(2*time.Second, func() bool { return a.settings.fontSize == base }), "Ctrl+0 resets the size")
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
		onMain(func() { busy = w.pane.feed.busy() })
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
		for _, mk := range w.pane.tracker.Blocks(0, 1<<30) {
			n++
			line := w.pane.term.Row(mk.Row)
			// The prompt number: marks are in order, but trimming drops
			// the oldest, so read it from the first mark.
			var got int
			if _, err := fmt.Sscanf(line, "$ cmd-%d", &got); err != nil {
				bad = append(bad, fmt.Sprintf("row %d: %q", mk.Row, strings.TrimSpace(line)))
				continue
			}
			if k >= 0 && got != k+1 {
				bad = append(bad, fmt.Sprintf("row %d: cmd-%d after cmd-%d", mk.Row, got, k))
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
	onMain(func() { n = len(w.pane.lines.Markers()) })
	check(n == prompts*3, "recorded %d marks for %d prompts", n, prompts)
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
		total = len(w.pane.lines.Markers())
		w.pane.lines.Prune()
	})
	check(row0 == "", "scrollback trimmed: row 0 is gone (%q)", row0)
	w.verify("after trimming")
	var kept int
	onMain(func() { kept = len(w.pane.lines.Markers()) })
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

// checkView: the overlay's rows match VTE's view once the scrollback is
// full and trimming (no shell; synthetic output).
func (w *Window) checkView() {
	p := w.pane
	time.Sleep(500 * time.Millisecond)
	onMain(func() { p.term.SetScrollback(100) })
	var b strings.Builder
	for i := range 1000 {
		fmt.Fprintf(&b, "line-%d\r\n", i)
	}
	p.feed.push([]byte(b.String()))
	w.settle()
	var rows, top, upper int
	var ok bool
	var bottom, first string
	onMain(func() {
		rows = p.term.Rows()
		top, ok = p.top()
		_, u, _, _ := p.term.View()
		upper = int(u)
		bottom = p.term.Row(top + rows - 2)
		first = p.term.Row(p.firstRow(u))
	})
	check(ok && bottom == "line-999", "screen found after trimming: row above the cursor is %q (top %d)", bottom, top)
	check(upper == 100 && first == "line-901", "oldest row kept is %q (of 100)", first)
	var shown string
	onMain(func() {
		p.scrollToRow(top - 30)
		t, _ := p.top()
		shown = p.term.Row(t)
	})
	want := fmt.Sprintf("line-%d", 1000-rows+1-30)
	check(shown == want, "scrolled 30 rows up: top row %q, want %q", shown, want)
	w.finish()
}

// shot saves a snapshot of the window for looking at (not a check).
func (w *Window) shot(name string) {
	dir := os.Getenv("WTF_SNAPSHOT_DIR")
	if dir == "" {
		dir = "bin/gtk/snapshots"
	}
	_ = os.MkdirAll(dir, 0o755)
	onMain(func() {
		path, err := w.snapshot(filepath.Join(dir, name+".png"))
		fmt.Printf("snapshot %s %v\n", path, err)
	})
}

func (w *Window) checkTerminal() {
	a, p := w.app, w.pane
	var mu sync.Mutex
	var out bytes.Buffer
	p.tap = func(b []byte) {
		mu.Lock()
		out.Write(b)
		mu.Unlock()
	}
	seen := func(s string) bool {
		mu.Lock()
		defer mu.Unlock()
		return bytes.Contains(out.Bytes(), []byte(s))
	}
	blocks := func() []uicore.RowBlock {
		var b []uicore.RowBlock
		onMain(func() { b = p.tracker.Blocks(0, 1<<30) })
		return b
	}
	run := func(cmd, marker string) bool {
		p.write(cmd + "\r")
		return waitUntil(5*time.Second, func() bool { return seen(marker) }) &&
			waitUntil(3*time.Second, func() bool {
				_, running := p.tracker.Running()
				return !running && !p.feed.busy()
			})
	}
	check(waitUntil(5*time.Second, func() bool { return len(p.tracker.Blocks(0, 1<<30)) > 0 }), "shell integration: first prompt marked")

	// Command blocks: text, exit code, duration, output.
	check(run("echo one; echo two", "two\r\n"), "ran echo")
	check(run("sleep 1.2; false", "\x1b]133;D;1"), "ran a failing command")
	b := blocks()
	check(len(b) == 3, "%d blocks for 2 commands and the open prompt", len(b))
	if len(b) >= 3 {
		first, second := b[0].Block, b[1].Block
		// Bash reports the first simple command of a line.
		check(first.Text == "echo one" && first.ExitCode != nil && *first.ExitCode == 0,
			"first block: %q", first.Text)
		check(second.ExitCode != nil && *second.ExitCode == 1 && second.Duration >= time.Second,
			"second block: exit 1 after %v", second.Duration)
		var output string
		onMain(func() { output, _ = p.tracker.Output(first.ID) })
		check(output == "one\ntwo", "output of the first: %q", output)
	}
	time.Sleep(300 * time.Millisecond)
	var marks int
	onMain(func() { marks = len(p.over.marks) })
	check(marks == 3, "overlay shows %d marks", marks)
	w.shot("terminal-blocks")

	// Copy last output, through the clipboard.
	check(run("echo copied-out", "copied-out\r\n"), "ran echo copied-out")
	_ = xkey("ctrl+alt+shift+c")
	time.Sleep(300 * time.Millisecond)
	clip := make(chan string, 1)
	onMain(func() {
		cb := p.term.Clipboard()
		cb.ReadTextAsync(context.Background(), func(res gio.AsyncResulter) {
			text, _ := cb.ReadTextFinish(res)
			clip <- text
		})
	})
	select {
	case got := <-clip:
		check(got == "copied-out", "Ctrl+Alt+Shift+C copies the last output: %q", got)
	case <-time.After(3 * time.Second):
		check(false, "clipboard read timed out")
	}

	// Hide Secrets: the token is covered, on the right cells.
	check(run("echo export GITHUB_TOKEN=abc123def456", "abc123def456\r\n"), "printed a secret")
	_ = xkey("ctrl+alt+shift+r")
	check(waitUntil(2*time.Second, func() bool { return a.settings.hide }), "Ctrl+Alt+Shift+R hides secrets")
	var covers []uicore.Cell
	check(waitUntil(2*time.Second, func() bool { covers = p.over.covers; return len(covers) >= 2 }),
		"secrets covered: %v", covers)
	ok := false
	onMain(func() {
		top, _ := p.top()
		for _, c := range covers {
			row := p.term.Row(top + c.Row)
			if r := []rune(row); c.Col+c.Width <= len(r) && string(r[c.Col:c.Col+c.Width]) == "abc123def456" {
				ok = true
			}
		}
	})
	check(ok, "a cover sits exactly on the output's token")
	w.shot("terminal-secrets")
	_ = xkey("ctrl+alt+shift+r")
	check(waitUntil(2*time.Second, func() bool { return !a.settings.hide && len(p.over.covers) == 0 }), "shown again")

	// Links: VTE reports the URL under the pointer; the right-click menu
	// on a command highlights its block.
	check(run("echo see https://example.com/docs?x=1.", "x=1.\r\n"), "printed a link")
	time.Sleep(300 * time.Millisecond)
	var link string
	var winX, winY int
	onMain(func() {
		top, _ := p.top()
		cw, ch := p.term.CharSize()
		for r := 0; r < p.term.Rows(); r++ {
			if row := p.term.Row(top + r); strings.HasPrefix(row, "see https") {
				x, y := float64(padLeft+6*cw+cw/2), float64(padTop+r*ch+ch/2)
				link = p.term.LinkAt(x, y)
				// The block's prompt row, in the window, for the click below.
				pt, _ := p.term.ComputePoint(w.win, graphene.NewPointAlloc().Init(float32(x), float32(y-float64(ch))))
				// The X window also holds the surface's shadow margin.
				tx, ty := w.pane.term.Native().SurfaceTransform()
				winX, winY = int(float64(pt.X())+tx), int(float64(pt.Y())+ty)
			}
		}
	})
	check(link == "https://example.com/docs?x=1", "the link under the pointer is %q", link)
	xclick(winX, winY, 3)
	check(waitUntil(2*time.Second, func() bool { return p.over.highlight != nil }), "right-click on a command opens its menu")
	w.shot("terminal-menu")
	_ = xkey("Escape")
	check(waitUntil(2*time.Second, func() bool { return p.over.highlight == nil }), "Escape closes it")

	// Find: count and highlights.
	_ = xkey("ctrl+shift+f")
	check(waitUntil(2*time.Second, func() bool { return p.find != nil }), "Ctrl+Shift+F opens Find")
	_ = exec.Command("xdotool", "type", "--delay", "30", "echo").Run()
	var status string
	check(waitUntil(3*time.Second, func() bool {
		status = p.find.status.Text()
		return strings.HasSuffix(status, " of 5")
	}), "Find counts the matches: %q", status)
	var found int
	onMain(func() { found = len(p.over.found) })
	check(found >= 4, "%d matches highlighted on screen", found)
	w.shot("terminal-find")
	_ = xkey("Escape")
	check(waitUntil(2*time.Second, func() bool { return p.find == nil && len(p.over.found) == 0 }), "Escape closes Find")

	// Prompt jumps: up to an earlier prompt, back down to the bottom.
	for i := range 40 {
		p.write(fmt.Sprintf("echo filler-%d\r", i))
	}
	check(waitUntil(10*time.Second, func() bool { return seen("filler-39\r\n") }), "filled the screen")
	time.Sleep(500 * time.Millisecond)
	w.settle()
	// Find left the view on a match; typing would bring it down.
	onMain(p.scrollToBottom)
	time.Sleep(200 * time.Millisecond)
	var before, after, back int
	onMain(func() { before, _ = p.top() })
	_ = xkey("ctrl+shift+Up")
	time.Sleep(300 * time.Millisecond)
	onMain(func() { after, _ = p.top() })
	_ = xkey("ctrl+shift+Down")
	time.Sleep(300 * time.Millisecond)
	_ = xkey("ctrl+shift+Down")
	time.Sleep(300 * time.Millisecond)
	_ = xkey("ctrl+shift+Down")
	time.Sleep(300 * time.Millisecond)
	onMain(func() { back, _ = p.top() })
	check(after < before, "Previous Prompt scrolls up (%d → %d)", before, after)
	check(back == before, "Next Prompt comes back to the bottom (%d)", back)

	// Clear: scrollback gone, the prompt kept, old marks dropped.
	_ = xkey("ctrl+shift+k")
	var upper float64
	check(waitUntil(3*time.Second, func() bool {
		_, upper, _, _ = p.term.View()
		return int(upper) == p.term.Rows() && len(p.tracker.Blocks(0, 1<<30)) <= 1
	}), "Ctrl+Shift+K clears (upper %.0f, %d blocks left)", upper, len(blocks()))
	w.shot("terminal-cleared")

	// Paste confirmation: two lines are shown first. Last: Xvfb has no
	// window manager to give the window the keyboard back after a dialog.
	onMain(func() { p.term.Clipboard().SetText("echo pasted-one\necho pasted-two\n") })
	time.Sleep(200 * time.Millisecond)
	_ = xkey("ctrl+shift+v")
	check(waitUntil(3*time.Second, func() bool { return p.pasteDialog != nil }), "a two-line paste asks first")
	time.Sleep(300 * time.Millisecond)
	check(!seen("pasted-one"), "nothing reached the shell yet")
	// Xvfb has no window manager to focus the dialog: answer it directly.
	onMain(func() {
		if p.pasteDialog != nil {
			p.pasteDialog.Response("paste")
		}
	})
	// Bash takes a bracketed paste as typed text: Enter runs it.
	check(waitUntil(3*time.Second, func() bool { return seen("pasted-one") }), "Paste sends it")
	p.write("\r")
	check(waitUntil(5*time.Second, func() bool { return seen("pasted-two\r\n") }), "and Enter runs it")
	check(waitUntil(5*time.Second, func() bool { _, r := p.tracker.Running(); return !r && !p.feed.busy() }), "to the end")

	w.finish()
}

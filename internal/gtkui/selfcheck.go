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
//	tabs      tabs, splits, focus, zoom, groups, a busy background tab,
//	          moving a tab to a new window; quits saving the session
//	restore   run after tabs: the saved windows come back
//	sidebar   the side pane: Files (tree, git, insert, cd), SSH, Kubernetes
//	panes     the other side-pane tabs: History, Pins, Share, Network,
//	          Processes, AWS, Generate, Findings, Snippets, Workspaces
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
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/diamondburned/gotk4/pkg/glib/v2"

	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/graphene"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/dayanraj/wtf/internal/events"
	"github.com/dayanraj/wtf/internal/files"
	"github.com/dayanraj/wtf/internal/sshhosts"
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
	case "tabs":
		w.checkTabs()
	case "restore":
		w.checkRestore()
	case "sidebar":
		w.checkSidebar()
	case "panes":
		w.checkPanes()
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
	check(waitUntil(5*time.Second, func() bool { return w.active().sess != nil && len(a.be.Terminal.ShellPIDs()) == 1 }), "shell started")
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
	w.active().tap = func(b []byte) {
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
		onMain(func() { busy = w.active().feed.busy() })
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
		for _, mk := range w.active().tracker.Blocks(0, 1<<30) {
			n++
			line := w.active().term.Row(mk.Row)
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
	onMain(func() { before = w.active().term.Columns() })
	start := time.Now()
	onMain(func() { w.win.SetDefaultSize(width, 600) })
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		onMain(func() { cols = w.active().term.Columns() })
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
	onMain(func() { cols = w.active().term.Columns() })
	fmt.Printf("grid %d columns\n", cols)

	const prompts = 40
	var all bytes.Buffer
	for k := 0; k < prompts; k++ {
		all.WriteString(promptBlock(k))
	}
	// One push, so the feeder has to cut it at every mark.
	w.active().feed.push(all.Bytes())
	w.settle()
	var n int
	onMain(func() { n = len(w.active().lines.Markers()) })
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
	onMain(func() { w.active().term.SetScrollback(300) })
	var more bytes.Buffer
	for k := prompts; k < prompts+60; k++ {
		more.WriteString(promptBlock(k))
	}
	w.active().feed.push(more.Bytes())
	w.settle()
	var row0 string
	var total int
	onMain(func() {
		row0 = w.active().term.Row(0)
		total = len(w.active().lines.Markers())
		w.active().lines.Prune()
	})
	check(row0 == "", "scrollback trimmed: row 0 is gone (%q)", row0)
	w.verify("after trimming")
	var kept int
	onMain(func() { kept = len(w.active().lines.Markers()) })
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
	w.active().tap = func(b []byte) {
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
	_, _ = w.active().sess.Write([]byte("sleep 300\r"))
	time.Sleep(500 * time.Millisecond)
	sleeper := childNamed(shell, "sleep")
	check(sleeper > 0, "sleep 300 running (pid %d)", sleeper)
	check(strings.Trim(procStatus(sleeper, "SigBlk"), "0") == "", "its signal mask is empty")
	_, _ = w.active().sess.Write([]byte{0x03})
	time.Sleep(300 * time.Millisecond)
	check(procStatus(sleeper, "Name") == "", "Ctrl-C stopped it")

	// A resize reaches the program in the foreground.
	_, _ = w.active().sess.Write([]byte(`sh -c 'trap "echo GOT-WINCH-\$(tput cols)" WINCH; while :; do sleep 0.1; done'` + "\r"))
	time.Sleep(500 * time.Millisecond)
	onMain(func() { w.win.SetDefaultSize(600, 500) })
	check(waitFor("GOT-WINCH-", 3*time.Second), "SIGWINCH reached the program")
	_, _ = w.active().sess.Write([]byte{0x03})
	time.Sleep(300 * time.Millisecond)

	// Left running: quitting must kill it (the caller checks).
	_, _ = w.active().sess.Write([]byte("sleep 777\r"))
	time.Sleep(500 * time.Millisecond)
	fmt.Printf("left running: sleep pid %d\n", childNamed(shell, "sleep"))
	w.finish()
}

// checkView: the overlay's rows match VTE's view once the scrollback is
// full and trimming (no shell; synthetic output).
func (w *Window) checkView() {
	p := w.active()
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
	// A frame being drawn can make the renderer refuse: try again.
	for try := 0; try < 5; try++ {
		var err error
		var path string
		onMain(func() { path, err = w.snapshot(filepath.Join(dir, name+".png")) })
		if err == nil || try == 4 {
			fmt.Printf("snapshot %s %v\n", path, err)
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func (w *Window) checkTerminal() {
	a, p := w.app, w.active()
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

	// The address bar: a git repository's badge, the last command's result.
	check(run("mkdir -p /tmp/repo && cd /tmp/repo && git init -q -b trunk && echo in-repo", "in-repo\r\n"), "made a git repository")
	var badges []uicore.Badge
	check(waitUntil(5*time.Second, func() bool {
		if ctx := w.infra[p.id]; ctx != nil {
			badges = uicore.BadgesOf(*ctx, time.Now())
		}
		for _, b := range badges {
			if b.Kind == "git" && strings.Contains(b.Label, "trunk") {
				return true
			}
		}
		return false
	}), "the address bar shows the git branch: %+v", badges)
	var cwd string
	var last *uicore.LastCommand
	onMain(func() {
		if ps := w.model.Panes[p.id]; ps != nil {
			cwd, last = ps.Cwd, ps.LastCommand
		}
	})
	check(cwd == "/tmp/repo" && last != nil && last.ExitCode == 0, "and the folder (%s) and a successful last command", cwd)
	w.shot("terminal-address")

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
				tx, ty := w.active().term.Native().SurfaceTransform()
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
		var cur, total int
		_, err := fmt.Sscanf(status, "%d of %d", &cur, &total)
		return err == nil && total >= 5
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

// paneReady says a pane's shell has shown a prompt. Main loop only.
func paneReady(p *Pane) bool {
	return p != nil && p.session != "" && len(p.tracker.Blocks(0, 1<<30)) > 0
}

func (w *Window) checkTabs() {
	a, m := w.app, w.model
	count := func() (tabs, panes int) {
		onMain(func() { tabs, panes = len(m.Tabs()), len(m.Panes) })
		return
	}
	check(waitUntil(5*time.Second, func() bool { return paneReady(w.active()) }), "first shell ready")

	_ = xkey("ctrl+shift+t")
	_ = xkey("ctrl+shift+t")
	check(waitUntil(3*time.Second, func() bool { t, _ := count(); return t == 3 }), "Ctrl+Shift+T twice: 3 tabs")
	_ = xkey("ctrl+shift+d")
	_ = xkey("ctrl+alt+shift+d")
	check(waitUntil(3*time.Second, func() bool { _, p := count(); return p == 5 }), "split right and down: 5 panes")
	var leaves []int
	onMain(func() { t, _ := m.Active(); leaves = t.Root.Leaves() })
	check(len(leaves) == 3, "the active tab has 3 panes: %v", leaves)
	check(waitUntil(5*time.Second, func() bool {
		ok := true
		onMain(func() {
			for _, id := range leaves {
				ok = ok && w.panes[id] != nil && w.panes[id].session != ""
			}
		})
		return ok
	}), "each pane has a shell")
	time.Sleep(500 * time.Millisecond)
	w.shot("tabs-splits")

	// Focus moves between panes; the focused one has the keyboard.
	var focused int
	onMain(func() { focused = m.ActivePane().ID })
	check(focused == leaves[2], "the newest pane has the focus")
	_ = xkey("alt+Left")
	check(waitUntil(2*time.Second, func() bool {
		onMain(func() { focused = m.ActivePane().ID })
		return focused == leaves[0]
	}), "Alt+Left focuses the left pane (%d)", focused)
	var hasKeys bool
	onMain(func() { hasKeys = w.panes[focused].term.HasFocus() })
	check(hasKeys, "and gives it the keyboard")
	_ = xkey("ctrl+shift+Return")
	check(waitUntil(2*time.Second, func() bool {
		var z int
		onMain(func() { t, _ := m.Active(); z = t.ZoomedPaneID })
		return z == leaves[0]
	}), "Ctrl+Shift+Enter zooms it")
	_ = xkey("ctrl+shift+Return")

	// Groups: a group with two tabs, folded and unfolded.
	var g int
	onMain(func() {
		tabs := m.Tabs()
		g = m.CreateGroup(tabs[0].ID)
		m.AddToGroup(tabs[1].ID, g)
		name := "api"
		m.SetGroup(g, &name, nil)
	})
	time.Sleep(300 * time.Millisecond)
	w.shot("tabs-group")
	var chips int
	onMain(func() {
		for c := w.strip.tabs.FirstChild(); c != nil; c = gtk.BaseWidget(c).NextSibling() {
			if gtk.BaseWidget(c).HasCSSClass("wtf-chip") {
				chips++
			}
		}
	})
	check(chips == 1, "the strip shows the group's chip")

	// The palette: type a command's name, Enter runs it.
	var panesBefore int
	onMain(func() { panesBefore = len(m.Panes) })
	_ = xkey("ctrl+shift+p")
	check(waitUntil(2*time.Second, func() bool { return w.palette != nil }), "Ctrl+Shift+P opens the palette")
	_ = exec.Command("xdotool", "type", "--delay", "20", "split right").Run()
	time.Sleep(300 * time.Millisecond)
	var top string
	onMain(func() {
		if w.palette != nil && len(w.palette.shown) > 0 {
			top = w.palette.shown[0].label
		}
	})
	check(top == "Split Right", "the best match is %q", top)
	w.shot("tabs-palette")
	_ = xkey("Return")
	check(waitUntil(3*time.Second, func() bool { return w.palette == nil && len(m.Panes) == panesBefore+1 }), "Enter runs it")
	// Themes preview as you move, and Escape puts the theme back.
	onMain(func() { w.togglePalette(paletteThemes) })
	time.Sleep(200 * time.Millisecond)
	_ = xkey("Down")
	_ = xkey("Down")
	var previewing string
	check(waitUntil(2*time.Second, func() bool {
		if pv := a.settings.previewing; pv != nil {
			previewing = pv.Name
		}
		return a.settings.previewing != nil
	}), "moving through themes previews them (%s)", previewing)
	w.shot("tabs-theme-preview")
	_ = xkey("Escape")
	check(waitUntil(2*time.Second, func() bool { return w.palette == nil && a.settings.previewing == nil }), "Escape puts the theme back")

	// Broadcast: typing in one split pane reaches the others.
	onMain(func() { m.Activate(m.Tabs()[2].ID) })
	time.Sleep(300 * time.Millisecond)
	var targets []int
	_ = xkey("ctrl+alt+shift+b")
	check(waitUntil(2*time.Second, func() bool {
		targets = w.broadcastTargets(w.active().id)
		return len(targets) > 0
	}), "Ctrl+Alt+Shift+B broadcasts to the tab's other panes (%v)", targets)
	_ = exec.Command("xdotool", "type", "--delay", "20", "echo broadcast-ok").Run()
	_ = xkey("Return")
	reached := 0
	check(waitUntil(5*time.Second, func() bool {
		reached = 0
		t, _ := m.Active()
		for _, id := range t.Root.Leaves() {
			if p := w.panes[id]; p != nil {
				top, _ := p.top()
				for r := top; r < top+p.term.Rows(); r++ {
					if strings.TrimSpace(p.term.Row(r)) == "broadcast-ok" {
						reached++
						break
					}
				}
			}
		}
		return reached == len(t.Root.Leaves())
	}), "the command ran in every pane of the tab (%d)", reached)
	w.shot("tabs-broadcast")
	_ = xkey("ctrl+alt+shift+b")
	check(waitUntil(2*time.Second, func() bool { return !w.broadcast.on() }), "and stops")

	// A background tab keeps its shell busy: output and prompt marks arrive
	// while it's hidden.
	onMain(func() { m.Activate(m.Tabs()[0].ID) })
	time.Sleep(300 * time.Millisecond)
	bg := w.active()
	var before int
	onMain(func() { before = len(bg.tracker.Blocks(0, 1<<30)) })
	bg.write("sleep 1; for i in $(seq 1 2000); do echo background-$i; done\r")
	onMain(func() { m.Activate(m.Tabs()[2].ID) })
	var mapped bool
	onMain(func() { mapped = bg.term.Mapped() })
	check(!mapped, "the first tab is hidden")
	check(waitUntil(15*time.Second, func() bool {
		var n int
		onMain(func() { n = len(bg.tracker.Blocks(0, 1<<30)) })
		return n >= before+1 && !bg.feed.busy()
	}), "its command finished while hidden, with its marks")
	var activity bool
	onMain(func() { t := m.Tabs()[0]; activity = t.Activity || t.Finished != nil })
	check(activity, "its tab shows there was output")
	var out string
	onMain(func() {
		for _, b := range bg.tracker.Blocks(0, 1<<30) {
			if strings.HasPrefix(b.Block.Text, "sleep") {
				out, _ = bg.tracker.Output(b.Block.ID)
			}
		}
	})
	check(strings.HasSuffix(out, "background-2000"), "all of its output arrived (%d bytes)", len(out))
	w.shot("tabs-activity")

	// Show All Tabs: a card per tab; Enter opens the one searched for.
	_ = xkey("ctrl+bar")
	var cards int
	check(waitUntil(2*time.Second, func() bool {
		if w.overviewShown != nil {
			cards = len(w.overviewShown.shown)
		}
		return cards == len(m.Tabs())
	}), "Ctrl+Shift+\\ shows %d cards", cards)
	w.shot("tabs-overview")
	_ = xkey("Escape")
	check(waitUntil(2*time.Second, func() bool { return w.overviewShown == nil }), "Escape closes it")
	check(waitUntil(2*time.Second, func() bool { p := w.active(); return p != nil && p.term.HasFocus() }),
		"and the terminal has the keyboard again")

	// The shortcuts window lists the keymap.
	_ = xkey("ctrl+question")
	check(waitUntil(3*time.Second, func() bool { return w.shortcuts != nil && w.shortcuts.IsVisible() }),
		"Ctrl+? shows the keyboard shortcuts")
	w.shot("tabs-shortcuts")
	onMain(func() {
		if w.shortcuts != nil {
			w.shortcuts.Close()
		}
	})

	// A tab moves to a new window with its shell.
	var shell string
	onMain(func() { shell = w.active().session })
	_ = xkey("ctrl+shift+n")
	check(waitUntil(3*time.Second, func() bool { return len(a.windows) == 2 }), "a second window")
	onMain(func() { w.win.Present() })
	time.Sleep(300 * time.Millisecond)
	onMain(func() { w.moveToNewWindow(m.ActiveID()) })
	check(waitUntil(3*time.Second, func() bool { return len(a.windows) == 3 }), "Move to New Window opens a window")
	var moved *Window
	onMain(func() { moved = a.windows[2] })
	var same bool
	onMain(func() {
		p := moved.active()
		same = p != nil && p.session == shell && moved.owns(shell) && !w.owns(shell)
	})
	check(same, "with the same shell (%s)", shell)
	t, _ := count()
	check(t == 2, "the first window has %d tabs left", t)

	// A workspace from the snippets file opens here, its placeholder filled.
	// Late: Xvfb has no window manager to give the keyboard back after its
	// dialog.
	cfg, _ := os.UserConfigDir()
	_ = os.MkdirAll(filepath.Join(cfg, "wtf"), 0o755)
	_ = os.WriteFile(filepath.Join(cfg, "wtf", "snippets.toml"), []byte(`
[[workspace]]
name = "Checks"
[[workspace.tab]]
title = "logs"
group = "ws"
color = "green"
command = "echo ws-{{word}}"
[[workspace.tab]]
layout = "column"
[[workspace.tab.pane]]
command = "echo pane-one"
[[workspace.tab.pane]]
command = "echo pane-two"
[workspace.params.word]
default = "hello"
`), 0o644)
	tabsBefore := len(m.Tabs())
	_ = xkey("Escape")
	onMain(func() { w.togglePalette(paletteWorkspaces) })
	var found string
	check(waitUntil(2*time.Second, func() bool {
		if w.palette != nil && len(w.palette.shown) > 0 {
			found = w.palette.shown[0].label
		}
		return found == "Checks"
	}), "the workspaces palette lists it (%q)", found)
	// Another window has the keyboard by now: pick it as Enter would.
	onMain(func() {
		if w.palette != nil {
			w.palette.run(0)
		}
	})
	check(waitUntil(2*time.Second, func() bool { return w.workspaceDialog != nil }), "choosing it asks for its values")
	w.shot("tabs-workspace")
	onMain(func() {
		if w.workspaceDialog != nil {
			w.workspaceDialog.Response("here")
		}
	})
	check(waitUntil(5*time.Second, func() bool { return len(m.Tabs()) == tabsBefore+2 }), "Open Here adds its 2 tabs")
	var wsGroup string
	var wsPanes int
	onMain(func() {
		tabs := m.Tabs()
		if len(tabs) >= 2 {
			if g, ok := m.Group(tabs[len(tabs)-2].GroupID); ok {
				wsGroup = g.Name + " " + string(g.Color)
			}
			wsPanes = len(tabs[len(tabs)-1].Root.Leaves())
		}
	})
	check(wsGroup == "ws green" && wsPanes == 2, "in its group (%q), the second split in 2 (%d)", wsGroup, wsPanes)
	check(waitUntil(8*time.Second, func() bool {
		tabs := m.Tabs()
		p := w.panes[tabs[len(tabs)-2].FocusedPaneID]
		if p == nil {
			return false
		}
		top, _ := p.top()
		for r := top; r < top+p.term.Rows(); r++ {
			if strings.TrimSpace(p.term.Row(r)) == "ws-hello" {
				return true
			}
		}
		return false
	}), "its startup command ran with the default value")
	onMain(func() {
		tabs := m.Tabs()
		m.CloseMany([]int{tabs[len(tabs)-2].ID, tabs[len(tabs)-1].ID})
	})

	// Quit saves every window's session.
	w.quitAndFinish()
}

func (w *Window) checkRestore() {
	a := w.app
	check(waitUntil(5*time.Second, func() bool { return len(a.windows) == 3 }), "3 windows restored (%d)", len(a.windows))
	var tabs, panes int
	var group string
	onMain(func() {
		main := a.window("main")
		if main == nil {
			return
		}
		tabs, panes = len(main.model.Tabs()), len(main.model.Panes)
		if gs := main.model.Groups(); len(gs) == 1 {
			group = gs[0].Name
		}
	})
	check(tabs == 2 && panes == 2, "the main window has its tabs back (%d tabs, %d panes)", tabs, panes)
	var split int
	onMain(func() {
		for _, x := range a.windows {
			if t, ok := x.model.Active(); ok && len(t.Root.Leaves()) == 4 {
				split = len(x.model.Tabs())
			}
		}
	})
	check(split == 1, "the moved tab is back in its own window, split in 4")
	check(group == "api", "and its group (%q)", group)
	time.Sleep(time.Second)
	w.shot("restored")
	w.quitAndFinish()
}

// quitAndFinish quits as the Quit command does (saving the windows), with
// the checks' status.
func (w *Window) quitAndFinish() {
	onMain(w.app.quitNow)
	if failed {
		os.Exit(1)
	}
}

func (w *Window) checkSidebar() {
	p := w.active()
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
	check(waitUntil(5*time.Second, func() bool { return paneReady(p) }), "shell ready")
	p.write("mkdir -p /tmp/proj/src && cd /tmp/proj && git init -q -b main && echo a > tracked.txt && git add . && " +
		"git -c user.email=x@y -c user.name=x commit -qm one && echo b >> tracked.txt && echo new > src/new.go && echo ready-proj\r")
	check(waitUntil(8*time.Second, func() bool { return seen("ready-proj\r\n") }), "made a repository with changes")
	check(waitUntil(5*time.Second, func() bool { ps := w.model.ActivePane(); return ps != nil && ps.Cwd == "/tmp/proj" }), "the pane is in it")

	_ = xkey("ctrl+alt+e")
	var f *filesTab
	check(waitUntil(3*time.Second, func() bool {
		if sp, ok := w.sidePane().(*filesTab); ok {
			f = sp
		}
		return f != nil
	}), "Ctrl+Alt+E opens the Files tab")
	rows := func() []string {
		var ids []string
		for _, id := range f.list.ids {
			if id != "" {
				ids = append(ids, id)
			}
		}
		return ids
	}
	check(waitUntil(5*time.Second, func() bool {
		ids := rows()
		return slices.Contains(ids, "d:/tmp/proj/src") && slices.Contains(ids, "f:/tmp/proj/tracked.txt") && f.git != nil
	}), "it lists the folder: %v", rows())
	var letter string
	var dotted bool
	onMain(func() {
		r := f.entryRow(uicore.FileRow{Entry: files.Entry{Name: "tracked.txt", Path: "/tmp/proj/tracked.txt"}})
		letter = r.letter
		d := f.entryRow(uicore.FileRow{Entry: files.Entry{Name: "src", Path: "/tmp/proj/src", Dir: true}})
		dotted = d.trailDot != ""
	})
	check(letter == "M" && dotted, "git marks: tracked.txt %q, src has changes %v", letter, dotted)
	var branch string
	onMain(func() { branch = f.gitText.Text() })
	check(strings.Contains(branch, "main"), "the branch shows (%q)", branch)
	w.shot("sidebar-files")

	// A click on a folder opens it; on a file types its path.
	onMain(func() { f.clicked("d:/tmp/proj/src") })
	check(waitUntil(3*time.Second, func() bool { return slices.Contains(rows(), "f:/tmp/proj/src/new.go") }), "a click opens a folder")
	onMain(func() { f.clicked("f:/tmp/proj/src/new.go") })
	check(waitUntil(3*time.Second, func() bool { return seen("src/new.go ") }), "a click on a file types its path at the prompt")
	p.write(uicore.ClearLine)

	// The changes, on their own.
	onMain(func() { f.gitBtn.Activate() })
	check(waitUntil(2*time.Second, func() bool {
		ids := rows()
		return len(ids) == 2 && strings.HasPrefix(ids[0], "c:")
	}), "the changes view lists the 2 changes: %v", rows())
	w.shot("sidebar-changes")

	// The side pane is saved with the window.
	var saved string
	onMain(func() { saved = string(w.sidebarState.JSON()) })
	check(strings.Contains(saved, `"open":true`) && strings.Contains(saved, `"tab":"files"`), "its state is saved: %s", saved)
	// SSH: hosts from ~/.ssh/config (the check's own home, never the real
	// one) and a key made with ssh-keygen.
	home, _ := os.UserHomeDir()
	_ = os.MkdirAll(filepath.Join(home, ".ssh"), 0o700)
	_ = os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte(`Host web1
  HostName 192.0.2.10
  User deploy
Host db
  HostName 192.0.2.20
  Port 2222
`), 0o600)
	p.write("ssh-keygen -q -t ed25519 -N '' -C check -f ~/.ssh/id_check && echo key-made\r")
	check(waitUntil(8*time.Second, func() bool { return seen("key-made\r\n") }), "made a key")
	_ = xkey("ctrl+alt+s")
	var t *sshTab
	check(waitUntil(3*time.Second, func() bool {
		if sp, ok := w.sidePane().(*sshTab); ok {
			t = sp
		}
		return t != nil
	}), "Ctrl+Alt+S opens the SSH tab")
	var hosts []string
	check(waitUntil(5*time.Second, func() bool {
		hosts = hosts[:0]
		for _, h := range t.list.Hosts {
			hosts = append(hosts, h.Alias)
		}
		return t.hostsLoaded && slices.Contains(hosts, "web1") && slices.Contains(hosts, "db")
	}), "it lists the config's hosts: %v", hosts)
	w.shot("sidebar-ssh")
	// Connecting types ssh into the idle shell (it fails: no such machine).
	onMain(func() {
		for _, h := range t.list.Hosts {
			if h.Alias == "db" {
				t.openHosts([]sshhosts.Host{h}, sshOpenHere)
			}
		}
	})
	check(waitUntil(3*time.Second, func() bool { return seen("ssh db") || seen("ssh -p 2222") }), "connecting types the ssh command into the shell")
	p.write("\x03")
	time.Sleep(500 * time.Millisecond)
	onMain(func() { t.show("keys") })
	var keys []string
	check(waitUntil(5*time.Second, func() bool {
		keys = keys[:0]
		for _, k := range t.keys.Keys {
			keys = append(keys, filepath.Base(k.Path))
		}
		return t.keysLoaded && slices.Contains(keys, "id_check")
	}), "the Keys section lists the new key: %v", keys)
	w.shot("sidebar-ssh-keys")
	onMain(func() { t.show("tunnels") })
	check(waitUntil(3*time.Second, func() bool { return t.tunnelsLoaded }), "the Tunnels section loads")
	onMain(func() { t.show("hosts") })

	// Kubernetes: a kubeconfig with two contexts (no cluster to reach).
	_ = os.MkdirAll(filepath.Join(home, ".kube"), 0o755)
	_ = os.WriteFile(filepath.Join(home, ".kube", "config"), []byte(`apiVersion: v1
kind: Config
current-context: dev
clusters:
- name: dev-cluster
  cluster: {server: "https://127.0.0.1:6443"}
- name: prod-cluster
  cluster: {server: "https://10.0.0.1:6443"}
users:
- name: me
  user: {token: not-a-real-token}
contexts:
- name: dev
  context: {cluster: dev-cluster, user: me, namespace: web}
- name: prod
  context: {cluster: prod-cluster, user: me}
`), 0o600)
	_ = xkey("ctrl+alt+k")
	var k *kubeTab
	check(waitUntil(3*time.Second, func() bool {
		if sp, ok := w.sidePane().(*kubeTab); ok {
			k = sp
		}
		return k != nil
	}), "Ctrl+Alt+K opens the Kubernetes tab")
	time.Sleep(time.Second)
	w.shot("sidebar-kube")
	onMain(func() { k.choose("contexts") })
	var contexts []string
	var dflt bool
	check(waitUntil(5*time.Second, func() bool {
		c, ok := k.body.(*kubeContexts)
		if !ok || !c.loaded {
			return false
		}
		contexts = contexts[:0]
		for _, x := range c.configs.Contexts {
			contexts = append(contexts, x.Name)
			if x.Name == "dev" {
				dflt = x.Current
			}
		}
		return len(contexts) == 2
	}), "the Contexts view lists the kubeconfig's contexts: %v", contexts)
	check(dflt, "dev is the default")
	w.shot("sidebar-kube-contexts")

	_ = xkey("ctrl+alt+k")
	check(waitUntil(2*time.Second, func() bool { return w.sidebar == nil }), "Ctrl+Alt+K again closes the pane")
	w.finish()
}

func (w *Window) checkPanes() {
	p := w.active()
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
	check(waitUntil(5*time.Second, func() bool { return paneReady(p) }), "shell ready")
	home, _ := os.UserHomeDir()
	_ = os.MkdirAll(filepath.Join(home, ".aws"), 0o700)
	_ = os.WriteFile(filepath.Join(home, ".aws", "config"), []byte("[profile dev]\nregion = eu-west-1\n[profile ops]\nregion = us-east-1\nsso_start_url = https://example.awsapps.com/start\n"), 0o600)
	p.write("echo first-cmd; sleep 0.3; echo history-ok\r")
	check(waitUntil(5*time.Second, func() bool { return seen("history-ok\r\n") }), "ran a command")
	time.Sleep(500 * time.Millisecond)

	open := func(key string, want func(sidePaneTab) bool, name string) sidePaneTab {
		_ = xkey(key)
		var tab sidePaneTab
		check(waitUntil(3*time.Second, func() bool {
			tab = w.sidePane()
			return tab != nil && want(tab)
		}), "%s opens with %s", name, key)
		return tab
	}

	// History: the command, with how it went.
	open("ctrl+alt+h", func(t sidePaneTab) bool { _, ok := t.(*historyTab); return ok }, "History")
	var cmds []string
	check(waitUntil(3*time.Second, func() bool {
		cmds = cmds[:0]
		for _, e := range w.records.history.Of(p.id) {
			cmds = append(cmds, e.Command)
		}
		return len(cmds) > 0 && strings.Contains(cmds[len(cmds)-1], "echo first-cmd")
	}), "it lists the command: %q", cmds)
	w.shot("panes-history")

	// Pin the last output: the Pinned tab opens with it.
	_ = xkey("ctrl+alt+i")
	time.Sleep(300 * time.Millisecond)
	onMain(w.pinLastOutput)
	var pins []uicore.Pin
	check(waitUntil(3*time.Second, func() bool {
		pins = w.records.pins.List()
		_, shown := w.sidePane().(*pinsTab)
		return len(pins) == 1 && shown
	}), "Pin Last Command Output pins it and shows the Pinned tab")
	if len(pins) == 1 {
		check(strings.Contains(pins[0].Text, "history-ok"), "with its output (%q)", pins[0].Text)
	}
	w.shot("panes-pins")

	open("ctrl+alt+j", func(t sidePaneTab) bool { _, ok := t.(*shareTab); return ok }, "Share & Record")
	w.shot("panes-share")

	open("ctrl+alt+o", func(t sidePaneTab) bool { _, ok := t.(*networkTab); return ok }, "Network")
	check(waitUntil(5*time.Second, func() bool { s := netStoreOf(w); return s.loaded || s.err != "" }), "it reads the connections (%s)", netStoreOf(w).err)
	w.shot("panes-network")

	open("ctrl+alt+a", func(t sidePaneTab) bool { _, ok := t.(*processTab); return ok }, "Processes")
	check(waitUntil(5*time.Second, func() bool { s := procStoreOf(w); return s.loaded && len(s.list) > 0 }), "it lists processes (%d)", len(procStoreOf(w).list))
	w.shot("panes-processes")

	open("ctrl+alt+u", func(t sidePaneTab) bool { _, ok := t.(*awsTab); return ok }, "AWS Profiles")
	var profiles []string
	check(waitUntil(5*time.Second, func() bool {
		profiles = profiles[:0]
		for _, x := range awsStore.list.Profiles {
			profiles = append(profiles, x.Name)
		}
		return awsStore.loaded && slices.Contains(profiles, "dev") && slices.Contains(profiles, "ops")
	}), "it lists the profiles: %v", profiles)
	w.shot("panes-aws")

	g := open("ctrl+alt+g", func(t sidePaneTab) bool { _, ok := t.(*generateTab); return ok }, "Generate")
	if gt, ok := g.(*generateTab); ok {
		onMain(func() { w.sidebarState.GenerateSection = "password"; gt.show() })
		check(waitUntil(2*time.Second, func() bool { return gt.pass != nil && len(gt.pass.password) >= 16 }), "it makes a password")
	}
	w.shot("panes-generate")

	open("ctrl+alt+f", func(t sidePaneTab) bool { _, ok := t.(*findingsTab); return ok }, "Findings")
	check(waitUntil(5*time.Second, func() bool {
		f, ok := w.sidePane().(*findingsTab)
		return ok && !f.loading && f.folder != ""
	}), "it reads the findings for the folder")
	w.shot("panes-findings")

	// Snippets and Workspaces, from the user's snippets file.
	cfg, _ := os.UserConfigDir()
	_ = os.MkdirAll(filepath.Join(cfg, "wtf"), 0o755)
	_ = os.WriteFile(filepath.Join(cfg, "wtf", "snippets.toml"), []byte(`
[[snippet]]
name = "Disk usage"
command = "du -sh {{dir}}"
[[workspace]]
name = "Logs"
[[workspace.tab]]
command = "echo logs"
`), 0o644)
	open("ctrl+alt+y", func(t sidePaneTab) bool { _, ok := t.(*snippetsTab); return ok }, "Snippets")
	var names []string
	check(waitUntil(5*time.Second, func() bool {
		names = names[:0]
		for _, x := range snippetsShared.list.Snippets {
			names = append(names, x.Name)
		}
		return snippetsShared.loaded && slices.Contains(names, "Disk usage")
	}), "it lists the snippet: %v", names)
	w.shot("panes-snippets")
	open("ctrl+alt+w", func(t sidePaneTab) bool { _, ok := t.(*workspacesTab); return ok }, "Workspaces")
	var listed int
	check(waitUntil(5*time.Second, func() bool {
		t, ok := w.sidePane().(*workspacesTab)
		listed = 0
		if ok {
			for _, id := range t.list.ids {
				if id != "" {
					listed++
				}
			}
		}
		return listed >= 1
	}), "it lists the workspace (%d)", listed)
	w.shot("panes-workspaces")
	w.finish()
}

//go:build linux

package main

// Self-checks, run headless (Xvfb) with WTF_NATIVE_DEBUG:
//
//	marks    OSC 133 marks stay on their prompts across feeds, rewraps
//	         and scrollback trimming (no shell; synthetic output)
//	signals  the shell's signal mask is empty; Ctrl-C and resizes reach
//	         programs. Prints the pid of a `sleep` left running, so the
//	         caller can check that quitting killed it.
//
// Each prints PASS/FAIL lines and quits; the exit status is 1 on a failure.

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

var failed bool

func check(ok bool, format string, args ...any) {
	status := "PASS"
	if !ok {
		status, failed = "FAIL", true
	}
	fmt.Printf("%s %s\n", status, fmt.Sprintf(format, args...))
}

func (a *app) finish() {
	onMain(a.gtk.Quit)
	if failed {
		os.Exit(1)
	}
}

// settle waits until all output is fed and parsed.
func (a *app) settle() {
	for {
		var busy bool
		onMain(func() {
			a.feed.mu.Lock()
			busy = len(a.feed.pending) > 0 || a.feed.updating
			a.feed.mu.Unlock()
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
func (a *app) verify(stage string) {
	var bad []string
	var n int
	onMain(func() {
		k := -1
		for _, mk := range a.marks.list {
			if mk.kind != 'A' {
				continue
			}
			n++
			line := a.term.Row(mk.row)
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

func (a *app) resizeTo(width int) (cols int, took time.Duration) {
	var before int
	onMain(func() { before = a.term.Columns() })
	start := time.Now()
	onMain(func() { a.win.SetDefaultSize(width, 600) })
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		onMain(func() { cols = a.term.Columns() })
		if cols != before {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cols, time.Since(start)
}

func (a *app) checkMarks() {
	time.Sleep(500 * time.Millisecond) // mapped and allocated
	var cols int
	onMain(func() { cols = a.term.Columns() })
	fmt.Printf("grid %d columns\n", cols)

	const prompts = 40
	var all bytes.Buffer
	for k := 0; k < prompts; k++ {
		all.WriteString(promptBlock(k))
	}
	// One push, so the feeder has to cut it at every mark.
	a.feed.push(all.Bytes())
	a.settle()
	var n int
	onMain(func() { n = len(a.marks.list) })
	check(n == prompts*4, "recorded %d marks for %d prompts", n, prompts)
	a.verify("after feeding")

	c, took := a.resizeTo(480)
	a.settle()
	check(c < cols, "narrower: %d → %d columns in %v", cols, c, took.Round(time.Millisecond))
	a.verify("after rewrap to narrower")

	c2, took := a.resizeTo(1200)
	a.settle()
	check(c2 > c, "wider: %d → %d columns in %v", c, c2, took.Round(time.Millisecond))
	a.verify("after rewrap to wider")

	// Scrollback trimming: a small scrollback, then lots of output.
	onMain(func() { a.term.SetScrollback(300) })
	var more bytes.Buffer
	for k := prompts; k < prompts+60; k++ {
		more.WriteString(promptBlock(k))
	}
	a.feed.push(more.Bytes())
	a.settle()
	var row0 string
	var total int
	onMain(func() {
		row0 = a.term.Row(0)
		total = len(a.marks.list)
		a.marks.prune()
	})
	check(row0 == "", "scrollback trimmed: row 0 is gone (%q)", row0)
	a.verify("after trimming")
	var kept int
	onMain(func() { kept = len(a.marks.list) })
	fmt.Printf("marks kept %d of %d\n", kept, total)

	c3, took := a.resizeTo(600)
	a.settle()
	check(c3 < c2, "rewrap after trimming: %d → %d columns in %v", c2, c3, took.Round(time.Millisecond))
	a.verify("after trimming and rewrap")
	a.finish()
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

func (a *app) checkSignals() {
	var mu sync.Mutex
	var out bytes.Buffer
	a.tap = func(b []byte) {
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
	for pid := range a.svc.ShellPIDs() {
		shell = pid
	}
	blk, ign := procStatus(shell, "SigBlk"), procStatus(shell, "SigIgn")
	check(strings.Trim(blk, "0") == "", "shell %d signal mask empty (SigBlk %s)", shell, blk)
	fmt.Printf("shell SigIgn %s\n", ign)

	// Ctrl-C stops a foreground program.
	_, _ = a.sess.Write([]byte("sleep 300\r"))
	time.Sleep(500 * time.Millisecond)
	sleeper := childNamed(shell, "sleep")
	check(sleeper > 0, "sleep 300 running (pid %d)", sleeper)
	check(strings.Trim(procStatus(sleeper, "SigBlk"), "0") == "", "its signal mask is empty")
	_, _ = a.sess.Write([]byte{0x03})
	time.Sleep(300 * time.Millisecond)
	check(procStatus(sleeper, "Name") == "", "Ctrl-C stopped it")

	// A resize reaches the program in the foreground.
	_, _ = a.sess.Write([]byte(`sh -c 'trap "echo GOT-WINCH-\$(tput cols)" WINCH; while :; do sleep 0.1; done'` + "\r"))
	time.Sleep(500 * time.Millisecond)
	onMain(func() { a.win.SetDefaultSize(600, 500) })
	check(waitFor("GOT-WINCH-", 3*time.Second), "SIGWINCH reached the program")
	_, _ = a.sess.Write([]byte{0x03})
	time.Sleep(300 * time.Millisecond)

	// Left running: quitting must kill it (the caller checks).
	_, _ = a.sess.Write([]byte("sleep 777\r"))
	time.Sleep(500 * time.Millisecond)
	fmt.Printf("left running: sleep pid %d\n", childNamed(shell, "sleep"))
	a.finish()
}

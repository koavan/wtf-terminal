//go:build linux

package gtkui

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/dayanraj/wtf/internal/uicore"
)

// checkPerf measures wtf at rest with each side-pane tab shown (the
// roadmap's "every side-pane tab 0% over 30 s"): CPU seconds over the
// period and memory (PSS) after it, as rows of linux/bench/results.csv
// labelled $WTF_NATIVE_BENCH_LABEL (default "gtk").
func (w *Window) checkPerf() {
	defer w.finish()
	check(waitUntil(5*time.Second, func() bool { return paneReady(w.active()) }), "shell ready")
	label := os.Getenv("WTF_NATIVE_BENCH_LABEL")
	if label == "" {
		label = "gtk"
	}
	seconds := 30
	if n, err := strconv.Atoi(os.Getenv("WTF_PERF_SECONDS")); err == nil && n > 0 {
		seconds = n
	}
	record := func(phase string) {
		a := cpuSeconds()
		time.Sleep(time.Duration(seconds) * time.Second)
		used := cpuSeconds() - a
		line := fmt.Sprintf("%s,%s,%d,%.2f,%.1f,%d", label, phase, seconds, used, 100*used/float64(seconds), pssMiB())
		fmt.Println("perf " + line)
		if f, err := os.OpenFile("linux/bench/results.csv", os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644); err == nil {
			fmt.Fprintln(f, line)
			f.Close()
		}
	}
	time.Sleep(3 * time.Second)
	record("idle-no-pane")
	for _, t := range sidebarTabs {
		tab := t.tab
		onMain(func() {
			w.sidebarState.Tab, w.sidebarState.Open = tab, true
			w.layoutSidebar()
		})
		time.Sleep(3 * time.Second) // its first load
		record("pane-" + string(tab))
	}
	onMain(func() {
		w.sidebarState.Open = false
		w.layoutSidebar()
		w.app.showManual(uicore.ManualHelpTopic(w.sidebarState))
	})
	time.Sleep(2 * time.Second)
	record("manual-open")
	record("manual-open-again")
}

// cpuSeconds is this process's user and system CPU time so far.
func cpuSeconds() float64 {
	b, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return 0
	}
	// Fields after the command name, which is in parentheses.
	f := strings.Fields(string(b[strings.LastIndexByte(string(b), ')')+1:]))
	if len(f) < 13 {
		return 0
	}
	utime, _ := strconv.ParseFloat(f[11], 64)
	stime, _ := strconv.ParseFloat(f[12], 64)
	return (utime + stime) / 100 // USER_HZ
}

// pssMiB is this process's proportional memory use.
func pssMiB() int {
	f, err := os.Open("/proc/self/smaps_rollup")
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if fields := strings.Fields(sc.Text()); len(fields) >= 2 && fields[0] == "Pss:" {
			kb, _ := strconv.Atoi(fields[1])
			return kb / 1024
		}
	}
	return 0
}

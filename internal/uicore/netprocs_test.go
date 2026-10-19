package uicore

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/dayanraj/wtf/internal/netconn"
	"github.com/dayanraj/wtf/internal/procs"
	"github.com/dayanraj/wtf/internal/usage"
)

func TestAddresses(t *testing.T) {
	for a, want := range map[string][2]string{"[::1]:3000": {"::1", "3000"}, "*:5432": {"*", "5432"}, "plain": {"plain", ""}} {
		if h, p := NetSplitAddress(a); h != want[0] || p != want[1] {
			t.Errorf("%s: %q %q", a, h, p)
		}
	}
	if NetIsExposed("127.0.0.1") || NetIsExposed("::1") || !NetIsExposed("0.0.0.0") || !NetIsExposed("*") {
		t.Error("exposure")
	}
	a := netconn.Conn{PID: 1, Process: "node", Protocol: "TCP", Local: "127.0.0.1:3000", State: "LISTEN"}
	b := netconn.Conn{PID: 1, Process: "node", Protocol: "TCP", Local: "[::1]:3000", State: "LISTEN"}
	c := netconn.Conn{PID: 1, Process: "node", Protocol: "TCP", Local: "*:3000", State: "LISTEN"}
	var locals []string
	for _, x := range NetMergeListeners([]netconn.Conn{a, b, c}) {
		locals = append(locals, x.Local)
	}
	if !reflect.DeepEqual(locals, []string{"127.0.0.1:3000", "*:3000"}) {
		t.Errorf("merged %v", locals)
	}
	if NetServiceName("5432") != "postgres" || NetServiceName("1") != "" {
		t.Error("service names")
	}
	if NetStateLabel("CLOSE_WAIT") != "close wait" || NetStateLabel("ESTABLISHED") != "" {
		t.Error("state labels")
	}
}

func pids(cs []netconn.Conn) []int {
	var out []int
	for _, c := range cs {
		out = append(out, c.PID)
	}
	return out
}

func TestWhichConnectionsShow(t *testing.T) {
	mine := netconn.Conn{PID: 10, Process: "ssh", Protocol: "TCP", Local: "10.0.0.2:5555", Remote: "1.2.3.4:22", State: "ESTABLISHED", Session: "s1"}
	other := netconn.Conn{PID: 20, Process: "Slack", Protocol: "TCP", Local: "10.0.0.2:6000", Remote: "5.6.7.8:443", State: "ESTABLISHED"}
	forward := netconn.Conn{PID: 30, Process: "ssh", Protocol: "TCP", Local: "127.0.0.1:5432", State: "LISTEN"}
	all := []netconn.Conn{mine, other, forward}
	if got := pids(NetVisible(all, true, "", map[string]bool{"5432": true})); !reflect.DeepEqual(got, []int{10, 30}) {
		t.Errorf("terminals %v", got)
	}
	if got := pids(NetVisible(all, false, "slack", nil)); !reflect.DeepEqual(got, []int{20}) {
		t.Errorf("filter %v", got)
	}
	if got := pids(NetVisible(all, true, "pid: 20", nil)); !reflect.DeepEqual(got, []int{20}) {
		t.Errorf("pid filter %v", got)
	}
}

func TestProcesses(t *testing.T) {
	expectText(t, ProcCPU(0.44), "0.4%")
	expectText(t, ProcCPU(23.4), "23%")
	expectText(t, ProcBytes(500), "1 KB")
	expectText(t, ProcBytes(52_428_800), "50 MB")
	expectText(t, ProcBytes(1_610_612_736), "1.5 GB")
	expectText(t, ProcElapsed(45), "45s")
	expectText(t, ProcElapsed(725), "12m")
	expectText(t, ProcElapsed(11_100), "3h 5m")
	expectText(t, ProcElapsed(187_200), "2d 4h")
	sh := procs.Proc{PID: 100, PPID: 1, Name: "zsh", CPU: 0, Memory: 10, Session: "s1", Shell: true}
	vim := procs.Proc{PID: 120, PPID: 100, Name: "vim", CPU: 5, Memory: 50, Session: "s1"}
	node := procs.Proc{PID: 110, PPID: 100, Name: "node", CPU: 30, Memory: 40, Session: "s1"}
	child := procs.Proc{PID: 130, PPID: 110, Name: "esbuild", CPU: 1, Memory: 5, Session: "s1"}
	var tree []string
	for _, n := range ProcTree([]procs.Proc{child, vim, node, sh}) {
		tree = append(tree, fmt.Sprintf("%s:%d", n.Proc.Name, n.Depth))
	}
	if !reflect.DeepEqual(tree, []string{"zsh:0", "node:1", "esbuild:2", "vim:1"}) {
		t.Errorf("tree %v", tree)
	}
	names := func(ps []procs.Proc) []string {
		var out []string
		for _, p := range ps {
			out = append(out, p.Name)
		}
		return out
	}
	if got := names(ProcSorted([]procs.Proc{sh, vim, node}, ProcSortCPU)); !reflect.DeepEqual(got, []string{"node", "vim", "zsh"}) {
		t.Errorf("by cpu %v", got)
	}
	if got := names(ProcSorted([]procs.Proc{sh, vim, node}, ProcSortName)); !reflect.DeepEqual(got, []string{"node", "vim", "zsh"}) {
		t.Errorf("by name %v", got)
	}
	if got := names(ProcSorted([]procs.Proc{sh, vim, node}, ProcSortMemory)); !reflect.DeepEqual(got, []string{"vim", "node", "zsh"}) {
		t.Errorf("by memory %v", got)
	}
	if !ProcMatches(vim, "120") || ProcMatches(vim, "emacs") {
		t.Error("matches")
	}
}

func TestUsageFormats(t *testing.T) {
	expectText(t, UsageCPU(-1), "…")
	expectText(t, UsageCPU(0.44), "0.4%")
	expectText(t, UsageCPU(12.4), "12%")
	expectText(t, UsageBytes(512), "512 B")
	expectText(t, UsageBytes(860_160), "840 KB")
	expectText(t, UsageBytes(432_013_312), "412 MB")
	expectText(t, UsageBytes(1_288_490_189), "1.2 GB")
	u := usage.Usage{App: usage.Group{CPU: 1, Memory: 100}, Window: usage.Group{CPU: 2, Memory: 50}, Programs: usage.Group{CPU: 50, Memory: 999}}
	if cpu, mem := UsageTotal(u); cpu != -1 || mem != 150 {
		t.Errorf("total %v %v", cpu, mem)
	}
	u.Interval = 2
	if cpu, _ := UsageTotal(u); cpu != 3 {
		t.Errorf("total cpu %v", cpu)
	}
}

package uicore

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestHistoryStore(t *testing.T) {
	var h History
	t0 := time.Unix(1000, 0)
	h.Started(1, 1, "ls", "/a", t0)
	h.Finished(1, 0, 12*time.Millisecond, t0.Add(time.Second))
	h.Finished(1, 9, 99*time.Millisecond, time.Now())
	h.Started(1, 2, "   ", "/a", time.Now())
	h.Finished(2, 1, 5*time.Millisecond, time.Now())
	if l := h.Of(1); len(l) != 1 || *l[0].ExitCode != 0 || l[0].Duration != 12*time.Millisecond || l[0].Running() {
		t.Errorf("pane 1 %+v", l)
	}
	for i := range 1005 {
		h.Started(3, i, fmt.Sprintf("c%d", i), "", t0.Add(time.Duration(i)*time.Second))
	}
	if l := h.Of(3); len(l) != 1000 || l[0].Command != "c5" {
		t.Errorf("pane 3 kept %d from %q", len(l), l[0].Command)
	}
	h.Started(4, 1, "early", "", t0.Add(-5*time.Second))
	var merged []string
	for _, e := range h.Merged([]int{1, 4}) {
		merged = append(merged, e.Command)
	}
	if !reflect.DeepEqual(merged, []string{"early", "ls"}) {
		t.Errorf("merged %v", merged)
	}
	h.Clear(3)
	if len(h.Of(3)) != 0 {
		t.Error("cleared")
	}
	e := HistoryEntry{Command: "kubectl get pods", Cwd: "/srv/Shop"}
	if !HistoryMatches(e, " shop ") || HistoryMatches(e, "helm") {
		t.Error("matches")
	}
}

func TestPinStore(t *testing.T) {
	var p Pins
	block := 7
	p.Add(Pin{Title: "make", Text: "a", PaneID: 1, BlockID: &block, Cwd: "/", Running: true})
	p.Add(Pin{Title: "Selection", Text: "sel", PaneID: 1, Cwd: "/"})
	p.Add(Pin{Title: "make", Text: "b", PaneID: 1, BlockID: &block, Cwd: "/", Running: true})
	if l := p.List(); len(l) != 2 || l[1].Text != "b" || l[1].ID != 1 {
		t.Fatalf("list %+v", l)
	}
	p.Update(1, 7, "done", &PinDone{ExitCode: 0, Duration: 50 * time.Millisecond})
	p.Update(1, 7, "later", nil)
	if l := p.List(); l[1].Text != "done" || l[1].Running || *l[1].ExitCode != 0 || *l[1].Duration != 50*time.Millisecond || len(p.Live()) != 0 {
		t.Errorf("finished %+v", l[1])
	}
	one := 1
	p.Add(Pin{Title: "big", Text: strings.Repeat("x", 200_005), PaneID: 2, BlockID: &one, Cwd: "/"})
	if l := p.List(); !l[0].Truncated || len(l[0].Text) != 200_000 {
		t.Errorf("clip %v %d", l[0].Truncated, len(l[0].Text))
	}
	for i := range 40 {
		p.Add(Pin{Title: fmt.Sprintf("s%d", i), PaneID: 3, Cwd: "/"})
	}
	if l := p.List(); len(l) != 30 || l[0].Title != "s39" {
		t.Errorf("kept %d, first %q", len(l), l[0].Title)
	}
	first := p.List()[0].ID
	p.Toggle(first)
	if !p.List()[0].Collapsed {
		t.Error("toggle")
	}
	p.Remove(first)
	if len(p.List()) != 29 {
		t.Error("remove")
	}
	p.Add(Pin{Title: "tail", PaneID: 5, BlockID: &one, Running: true})
	p.PaneGone(5)
	if len(p.Live()) != 0 {
		t.Error("pane gone")
	}
	p.Clear()
	if len(p.List()) != 0 {
		t.Error("clear")
	}
}

// More cases of the Svelte build's pins tests.

func TestPinsFollowARunningCommandAndKeepTheEndOfLongOutput(t *testing.T) {
	var p Pins
	block := 7
	p.Add(Pin{Title: "make", Text: "old", PaneID: 1, BlockID: &block, Cwd: "/w", Running: true})
	if got := p.Add(Pin{Title: "make", Text: "a", PaneID: 1, BlockID: &block, Cwd: "/w", Running: true}); got.Text != "a" || len(p.List()) != 1 {
		t.Errorf("re-pinned %+v, %d pins", got, len(p.List()))
	}
	p.Update(1, 7, "a\nb", nil)
	if l := p.List(); l[0].Text != "a\nb" || !l[0].Running || len(p.Live()) != 1 {
		t.Errorf("following %+v", l[0])
	}
	p.PaneGone(1)
	if l := p.List(); len(l) != 1 || l[0].Running {
		t.Errorf("pane gone %+v", l)
	}
	other := 8
	long := p.Add(Pin{Title: "log", Text: strings.Repeat("x", PinsMaxChars) + "END", PaneID: 1, BlockID: &other})
	if !long.Truncated || len(long.Text) != PinsMaxChars || !strings.HasSuffix(long.Text, "END") {
		t.Errorf("long pin: truncated %v, %d characters, ends %q", long.Truncated, len(long.Text), long.Text[len(long.Text)-3:])
	}
}

package uicore

import (
	"slices"
	"sort"
	"strings"
	"time"
)

// What the History and Pinned tabs keep, in memory per window: commands
// run, and pinned output. A port of
// macos/Sources/WtfCore/History.swift (its Export part is in export.go).

// HistoryEntry is one command a pane ran.
type HistoryEntry struct {
	// The command block's id in its terminal.
	ID        int
	PaneID    int
	Command   string
	Cwd       string
	StartedAt time.Time
	// Zero while it runs; Duration and ExitCode are set with it.
	FinishedAt time.Time
	Duration   time.Duration
	ExitCode   *int
}

func (e HistoryEntry) Running() bool { return e.FinishedAt.IsZero() }

const HistoryMaxEntries = 1000

// History is the commands each pane ran, from shell integration. The zero
// value is ready to use.
type History struct {
	panes map[int][]HistoryEntry
}

// Started records a command; blank ones are skipped, and each pane keeps
// its last HistoryMaxEntries.
func (h *History) Started(pane, id int, command, cwd string, at time.Time) {
	if strings.TrimSpace(command) == "" {
		return
	}
	if h.panes == nil {
		h.panes = map[int][]HistoryEntry{}
	}
	list := append(h.panes[pane], HistoryEntry{ID: id, PaneID: pane, Command: command, Cwd: cwd, StartedAt: at})
	if len(list) > HistoryMaxEntries {
		list = append([]HistoryEntry(nil), list[len(list)-HistoryMaxEntries:]...)
	}
	h.panes[pane] = list
}

// Finished finishes the pane's last command, if it's still running;
// strays are ignored.
func (h *History) Finished(pane, exitCode int, d time.Duration, at time.Time) {
	list := h.panes[pane]
	if len(list) == 0 || !list[len(list)-1].Running() {
		return
	}
	last := &list[len(list)-1]
	last.FinishedAt = at
	last.Duration = d
	last.ExitCode = &exitCode
}

// Of is a copy of a pane's commands, oldest first.
func (h *History) Of(pane int) []HistoryEntry { return slices.Clone(h.panes[pane]) }

func (h *History) Clear(pane int) { delete(h.panes, pane) }

// Merged is several panes' commands in the order they started.
func (h *History) Merged(ids []int) []HistoryEntry {
	var out []HistoryEntry
	for _, id := range ids {
		out = append(out, h.panes[id]...)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
	return out
}

func HistoryMatches(e HistoryEntry, query string) bool {
	q := strings.ToLower(trimBlanks(query))
	return q == "" || strings.Contains(strings.ToLower(e.Command+" "+e.Cwd), q)
}

// Pin is output kept in view: a command's (following it while it runs) or
// a selection.
type Pin struct {
	// Set by Pins.Add.
	ID        int
	Title     string
	Text      string
	Truncated bool
	PaneID    int
	// nil for a selection.
	BlockID *int
	Cwd     string
	// Add sets it to now when zero.
	PinnedAt  time.Time
	Running   bool
	ExitCode  *int
	Duration  *time.Duration
	Collapsed bool
}

const (
	PinsMax      = 30
	PinsMaxChars = 200_000
)

// Pins is the Pinned tab's list, newest first. The zero value is ready to
// use.
type Pins struct {
	list   []Pin
	nextID int
}

// pinClip is the last 200,000 characters, and whether some were cut.
func pinClip(s string) (string, bool) {
	r := []rune(s)
	if len(r) > PinsMaxChars {
		return string(r[len(r)-PinsMaxChars:]), true
	}
	return s, false
}

// List is a copy of the pins, newest first.
func (p *Pins) List() []Pin { return slices.Clone(p.list) }

// Add pins a draft; pinning the same command again updates its pin in place.
func (p *Pins) Add(draft Pin) Pin {
	n := draft
	n.Text, n.Truncated = pinClip(draft.Text)
	n.Collapsed = false
	if n.PinnedAt.IsZero() {
		n.PinnedAt = time.Now()
	}
	if draft.BlockID != nil {
		for i, old := range p.list {
			if old.PaneID == draft.PaneID && old.BlockID != nil && *old.BlockID == *draft.BlockID {
				n.ID = old.ID
				p.list[i] = n
				return n
			}
		}
	}
	p.nextID++
	n.ID = p.nextID
	p.list = append([]Pin{n}, p.list...)
	if len(p.list) > PinsMax {
		p.list = p.list[:PinsMax]
	}
	return n
}

func (p *Pins) Remove(id int) {
	out := p.list[:0]
	for _, x := range p.list {
		if x.ID != id {
			out = append(out, x)
		}
	}
	p.list = out
}

func (p *Pins) Clear() { p.list = nil }

func (p *Pins) Toggle(id int) {
	for i := range p.list {
		if p.list[i].ID == id {
			p.list[i].Collapsed = !p.list[i].Collapsed
			return
		}
	}
}

// Live is the pins still following a running command.
func (p *Pins) Live() []Pin {
	var out []Pin
	for _, x := range p.list {
		if x.Running {
			out = append(out, x)
		}
	}
	return out
}

// PinDone is how a pinned command finished.
type PinDone struct {
	ExitCode int
	Duration time.Duration
}

// Update is new output for a running pin; done (when not nil) finishes it
// for good.
func (p *Pins) Update(pane, block int, text string, done *PinDone) {
	for i := range p.list {
		x := &p.list[i]
		if x.PaneID != pane || x.BlockID == nil || *x.BlockID != block || !x.Running {
			continue
		}
		x.Text, x.Truncated = pinClip(text)
		if done != nil {
			x.Running = false
			code, d := done.ExitCode, done.Duration
			x.ExitCode, x.Duration = &code, &d
		}
		return
	}
}

// PaneGone stops the pane's pins following it.
func (p *Pins) PaneGone(pane int) {
	for i := range p.list {
		if p.list[i].PaneID == pane {
			p.list[i].Running = false
		}
	}
}

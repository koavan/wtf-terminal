// Package activity tracks whether one of wtf's windows is in front, so
// background polling can stop while none is.
package activity

import (
	"context"
	"sync"
)

// Gate is open while at least one window has focus.
type Gate struct {
	mu      sync.Mutex
	focused map[string]bool
	// Closed and replaced each time the gate opens, waking every Wait.
	opened chan struct{}
}

// New returns an open gate: the first window opens in front.
func New() *Gate {
	return &Gate{focused: map[string]bool{"": true}, opened: make(chan struct{})}
}

// SetFocused records whether the named window has focus. The first report
// from any window replaces New's assumption.
func (g *Gate) SetFocused(window string, focused bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	wasOpen := len(g.focused) > 0
	delete(g.focused, "")
	if focused {
		g.focused[window] = true
	} else {
		delete(g.focused, window)
	}
	if !wasOpen && len(g.focused) > 0 {
		close(g.opened)
		g.opened = make(chan struct{})
	}
}

// Forget drops a closed window.
func (g *Gate) Forget(window string) { g.SetFocused(window, false) }

// Open reports whether a window is in front.
func (g *Gate) Open() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.focused) > 0
}

// Wait returns at once while the gate is open, otherwise blocks until it
// opens (true) or ctx ends (false). A nil gate is always open.
func (g *Gate) Wait(ctx context.Context) bool {
	if g == nil {
		return ctx.Err() == nil
	}
	g.mu.Lock()
	if len(g.focused) > 0 {
		g.mu.Unlock()
		return true
	}
	ch := g.opened
	g.mu.Unlock()
	select {
	case <-ch:
		return true
	case <-ctx.Done():
		return false
	}
}

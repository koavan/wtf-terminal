//go:build linux

package gtkui

import (
	"time"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
)

// poller refreshes something now, then every interval while its window (or
// any wtf window, for shared samplers) is in front, and again as soon as it
// comes back to the front. An interval of 0 refreshes only once. Like the
// macOS app's Poller.
type poller struct {
	a        *App
	w        *Window // nil: any window in front counts
	interval time.Duration
	refresh  func()
	timer    coreglib.SourceHandle
	focus    coreglib.SignalHandle
	stopped  bool
}

// sidebarInterval is the side pane's interval: the setting while
// auto-refresh is on, else 0.
func (a *App) sidebarInterval() time.Duration {
	c := a.settings.config()
	if !c.SidebarAutoRefresh {
		return 0
	}
	return time.Duration(max(1, c.SidebarRefreshInterval) * float64(time.Second))
}

func newPoller(a *App, w *Window, interval time.Duration, refresh func()) *poller {
	p := &poller{a: a, w: w, interval: interval, refresh: refresh}
	p.start()
	return p
}

func (p *poller) inFront() bool {
	if p.w != nil {
		return p.w.win.IsActive() && p.w.win.Mapped()
	}
	return p.a.front() != nil && p.a.front().win.IsActive()
}

func (p *poller) start() {
	p.stopped = false
	p.refresh()
	if p.interval <= 0 {
		return
	}
	p.timer = coreglib.TimeoutAdd(uint(p.interval/time.Millisecond), func() bool {
		if p.stopped {
			return false
		}
		if p.inFront() {
			p.refresh()
		}
		return true
	})
	if p.w != nil {
		p.focus = p.w.win.NotifyProperty("is-active", func() {
			if !p.stopped && p.w.win.IsActive() {
				p.refresh()
			}
		})
	}
}

func (p *poller) stop() {
	p.stopped = true
	if p.timer != 0 {
		coreglib.SourceRemove(p.timer)
		p.timer = 0
	}
	if p.focus != 0 {
		p.w.win.HandlerDisconnect(p.focus)
		p.focus = 0
	}
}

// update restarts with a new interval (refreshing now) only if it changed.
func (p *poller) update(interval time.Duration) {
	if interval == p.interval {
		return
	}
	p.stop()
	p.interval = interval
	p.start()
}

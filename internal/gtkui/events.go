//go:build linux

package gtkui

import (
	"sync"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"

	"github.com/dayanraj/wtf/internal/events"
)

// eventPump delivers backend events (internal/events) on the GTK main loop.
// Services emit from their own goroutines; events queue up and are handed
// out in one idle callback per batch, in order. Payloads are the services'
// own Go values: there's no JSON in between.
type eventPump struct {
	mu        sync.Mutex
	queue     []event
	scheduled bool

	// Main loop only.
	handlers map[string][]func(data any)
}

type event struct {
	name string
	data any
}

func newEventPump() *eventPump {
	p := &eventPump{handlers: map[string][]func(any){}}
	events.SetEmitter(p.emit)
	return p
}

// on calls f with every event of that name.
func (p *eventPump) on(name string, f func(data any)) {
	p.handlers[name] = append(p.handlers[name], f)
}

// emit runs on any goroutine.
func (p *eventPump) emit(name string, data any) {
	p.mu.Lock()
	p.queue = append(p.queue, event{name, data})
	schedule := !p.scheduled
	p.scheduled = true
	p.mu.Unlock()
	if schedule {
		coreglib.IdleAdd(p.flush)
	}
}

func (p *eventPump) flush() {
	p.mu.Lock()
	batch := p.queue
	p.queue, p.scheduled = nil, false
	p.mu.Unlock()
	for _, e := range batch {
		for _, f := range p.handlers[e.name] {
			f(e.data)
		}
	}
}

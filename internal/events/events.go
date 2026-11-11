// Package events delivers backend events to whichever UI is running: the
// macOS app (through cmd/libwtf) or the Linux app (internal/gtkui).
// Services emit through here so they don't depend on either.
package events

import "sync/atomic"

// Emitter sends one named event with a JSON-encodable payload to the UI.
type Emitter func(name string, data any)

var current atomic.Pointer[Emitter]

// SetEmitter installs the UI's emitter. Until then events are dropped.
func SetEmitter(fn Emitter) { current.Store(&fn) }

// Emit sends an event, if a UI is listening.
func Emit(name string, data any) {
	if fn := current.Load(); fn != nil && *fn != nil {
		(*fn)(name, data)
	}
}

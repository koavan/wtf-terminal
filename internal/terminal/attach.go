package terminal

import "fmt"

// Attach hands a session's output stream to a UI that doesn't use the
// WebSocket (the native macOS app, via cmd/libwtf). Like a stream
// connection, only one consumer may be attached at a time; call release
// when done. A package function rather than a method, so it isn't
// bound as a backend call (cmd/swiftgen binds the service's methods).
func Attach(s *Service, id string) (sess *Session, release func(), err error) {
	sess = s.get(id)
	if sess == nil {
		return nil, nil, fmt.Errorf("no session %q", id)
	}
	if !sess.attachMu.TryLock() {
		return nil, nil, fmt.Errorf("session %q is already attached", id)
	}
	return sess, sess.attachMu.Unlock, nil
}

// Lookup returns a running session, or nil.
func Lookup(s *Service, id string) *Session { return s.get(id) }

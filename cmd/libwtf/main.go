// Command libwtf builds the Go backend as a C archive for the native macOS
// UI (M0 spike: terminal sessions only).
//
//	go build -buildmode=c-archive -o bin/libwtf.a ./cmd/libwtf
package main

/*
#include <stdint.h>

typedef void (*wtf_output_fn)(void *ctx, int64_t id, const char *buf, int64_t len);
typedef void (*wtf_exit_fn)(void *ctx, int64_t id, int64_t code);

static inline void wtf_call_output(wtf_output_fn fn, void *ctx, int64_t id, const char *buf, int64_t len) {
	fn(ctx, id, buf, len);
}
static inline void wtf_call_exit(wtf_exit_fn fn, void *ctx, int64_t id, int64_t code) {
	fn(ctx, id, code);
}
*/
import "C"

import (
	"sync"
	"unsafe"

	"github.com/dayanraj/wtf/internal/terminal"
)

const (
	version = "native-spike"
	// Upper bound for coalescing queued PTY chunks into one callback, as in
	// the WebSocket stream (internal/terminal/stream.go).
	maxFrame = 256 * 1024
)

var (
	mu       sync.Mutex
	sessions = map[int64]*terminal.Session{}
	nextID   int64
)

// wtf_start starts a shell. Output is delivered on a Go-owned thread through
// onOutput; the buffer is only valid during the call, and the call blocks
// further output for this session until it returns (backpressure).
// Returns the session id, or -1 on failure.
//
//export wtf_start
func wtf_start(cols, rows C.int, onOutput C.wtf_output_fn, onExit C.wtf_exit_fn, ctx unsafe.Pointer) C.int64_t {
	sess, err := terminal.StartStandalone(version, int(cols), int(rows))
	if err != nil {
		return -1
	}
	mu.Lock()
	nextID++
	id := nextID
	sessions[id] = sess
	mu.Unlock()

	go func() {
		frame := make([]byte, 0, maxFrame)
		for chunk := range sess.Output {
			frame = append(frame[:0], chunk...)
		drain:
			for len(frame) < maxFrame {
				select {
				case more, ok := <-sess.Output:
					if !ok {
						break drain
					}
					frame = append(frame, more...)
				default:
					break drain
				}
			}
			C.wtf_call_output(onOutput, ctx, C.int64_t(id),
				(*C.char)(unsafe.Pointer(&frame[0])), C.int64_t(len(frame)))
		}
		<-sess.Done
		sess.Close()
		mu.Lock()
		delete(sessions, id)
		mu.Unlock()
		C.wtf_call_exit(onExit, ctx, C.int64_t(id), C.int64_t(sess.ExitCode()))
	}()
	return C.int64_t(id)
}

//export wtf_write
func wtf_write(id C.int64_t, buf *C.char, n C.int64_t) {
	if sess := get(id); sess != nil && n > 0 {
		_, _ = sess.Write(C.GoBytes(unsafe.Pointer(buf), C.int(n)))
	}
}

//export wtf_resize
func wtf_resize(id C.int64_t, cols, rows C.int) {
	if sess := get(id); sess != nil {
		_ = sess.Resize(uint16(max(1, cols)), uint16(max(1, rows)))
	}
}

//export wtf_close
func wtf_close(id C.int64_t) {
	if sess := get(id); sess != nil {
		sess.Close()
	}
}

func get(id C.int64_t) *terminal.Session {
	mu.Lock()
	defer mu.Unlock()
	return sessions[int64(id)]
}

func main() {}

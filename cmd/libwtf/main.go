// Command libwtf builds the Go backend as a C archive for the native macOS
// UI. Build it with the native tag, which leaves the Wails glue out:
//
//	go build -tags native -buildmode=c-archive -o bin/native/libwtf.a ./cmd/libwtf
//
// Service methods are called by name with JSON arguments (wtf_call), the
// same methods the Wails bindings expose. Backend events and PTY output come
// back through callbacks, on Go-owned threads.
package main

/*
#include <stdint.h>
#include <stdlib.h>

// Input strings are const, so Swift can pass its Strings directly.
typedef const char cstr_t;

typedef void (*wtf_event_fn)(void *ctx, const char *name, const char *json);
typedef void (*wtf_output_fn)(void *ctx, const char *buf, int64_t len);

static inline void wtf_call_event(wtf_event_fn fn, void *ctx, const char *name, const char *json) {
	fn(ctx, name, json);
}
static inline void wtf_call_output(wtf_output_fn fn, void *ctx, const char *buf, int64_t len) {
	fn(ctx, buf, len);
}
*/
import "C"

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"unsafe"

	"github.com/dayanraj/wtf/internal/backend"
	"github.com/dayanraj/wtf/internal/events"
	"github.com/dayanraj/wtf/internal/paths"
	"github.com/dayanraj/wtf/internal/state"
	"github.com/dayanraj/wtf/internal/terminal"
)

// Upper bound for coalescing queued PTY chunks into one callback, as in the
// WebSocket stream (internal/terminal/stream.go).
const maxFrame = 256 * 1024

var (
	mu       sync.Mutex
	be       *backend.Backend
	services map[string]any
	cancel   context.CancelFunc
)

// wtf_init builds and starts the services. Events go to onEvent from then
// on. stateDir and cacheDir replace the default directories when not empty,
// so a development build doesn't share files with the installed app.
// Returns NULL, or an error message to release with wtf_free.
//
//export wtf_init
func wtf_init(version, commit, buildDate, stateDir, cacheDir *C.cstr_t, onEvent C.wtf_event_fn, ctx unsafe.Pointer) *C.char {
	mu.Lock()
	defer mu.Unlock()
	if be != nil {
		return C.CString("already initialised")
	}
	if dir := goString(cacheDir); dir != "" {
		paths.SetCacheRoot(dir)
	}
	// Shells start through wtf-exec, next to the app's binary, which hands
	// them a clean signal mask (see macos/exec/wtf-exec.c).
	if exe, err := os.Executable(); err == nil {
		helper := filepath.Join(filepath.Dir(exe), "wtf-exec")
		if fi, err := os.Stat(helper); err == nil && fi.Mode()&0o111 != 0 {
			terminal.Launcher = func(path string, argv []string) (string, []string) {
				return helper, append([]string{helper, path}, argv...)
			}
		}
	}
	b, err := backend.New(goString(version), goString(commit), goString(buildDate), goString(stateDir))
	if err != nil {
		return C.CString(err.Error())
	}
	events.SetEmitter(func(name string, data any) {
		payload, err := json.Marshal(data)
		if err != nil {
			log.Printf("event %s: %v", name, err)
			return
		}
		cname, cjson := C.CString(name), C.CString(string(payload))
		defer C.free(unsafe.Pointer(cname))
		defer C.free(unsafe.Pointer(cjson))
		C.wtf_call_event(onEvent, ctx, cname, cjson)
	})
	runCtx, stop := context.WithCancel(context.Background())
	if err := b.Start(runCtx); err != nil {
		stop()
		return C.CString(err.Error())
	}
	services = map[string]any{}
	for _, n := range b.Services() {
		services[n.Name] = n.Service
	}
	// What the Wails window manager reads from the store itself.
	services["session"] = &session{b.Store}
	be, cancel = b, stop
	return nil
}

// session gives the native UI the saved windows to reopen at launch, and
// forgets a window closed for good. (Saving goes through state.SaveWindow,
// as in the Wails build.)
type session struct{ store *state.Store }

// Windows returns the saved windows in the order they were first saved.
func (s *session) Windows() []state.Window { return s.store.Windows() }

func (s *session) Forget(name string) error { return s.store.Forget(name) }

// wtf_shutdown stops the services, last started first.
//
//export wtf_shutdown
func wtf_shutdown() {
	mu.Lock()
	defer mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if be != nil {
		be.Shutdown()
	}
	be = nil
}

// wtf_call calls service.method with a JSON array of arguments and returns
// {"result": …} or {"error": "…"}, to release with wtf_free. It blocks for
// as long as the method does; call it off the main thread.
//
//export wtf_call
func wtf_call(service, method, args *C.cstr_t) *C.char {
	result, err := call(goString(service), goString(method), []byte(goString(args)))
	var out []byte
	if err != nil {
		out, _ = json.Marshal(map[string]string{"error": err.Error()})
	} else if out, err = json.Marshal(map[string]any{"result": result}); err != nil {
		out, _ = json.Marshal(map[string]string{"error": "encoding result: " + err.Error()})
	}
	return C.CString(string(out))
}

var errorType = reflect.TypeFor[error]()

func call(service, method string, args []byte) (result any, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%s.%s panicked: %v", service, method, r)
		}
	}()
	mu.Lock()
	svc := services[service]
	mu.Unlock()
	if svc == nil {
		return nil, fmt.Errorf("no service %q", service)
	}
	if method == "ServiceStartup" || method == "ServiceShutdown" {
		return nil, fmt.Errorf("%s.%s is not callable", service, method)
	}
	m := reflect.ValueOf(svc).MethodByName(method)
	if !m.IsValid() {
		return nil, fmt.Errorf("no method %s.%s", service, method)
	}
	var raw []json.RawMessage
	if len(args) > 0 {
		if err := json.Unmarshal(args, &raw); err != nil {
			return nil, fmt.Errorf("%s.%s: arguments must be a JSON array: %w", service, method, err)
		}
	}
	t := m.Type()
	if t.IsVariadic() || len(raw) != t.NumIn() {
		return nil, fmt.Errorf("%s.%s takes %d arguments, got %d", service, method, t.NumIn(), len(raw))
	}
	in := make([]reflect.Value, len(raw))
	for i, r := range raw {
		v := reflect.New(t.In(i))
		if err := json.Unmarshal(r, v.Interface()); err != nil {
			return nil, fmt.Errorf("%s.%s argument %d: %w", service, method, i+1, err)
		}
		in[i] = v.Elem()
	}
	out := m.Call(in)
	// As with Wails: a trailing error is the error; the rest is the result
	// (nothing, one value, or an array).
	if n := len(out); n > 0 && t.Out(n-1) == errorType {
		if e := out[n-1]; !e.IsNil() {
			return nil, e.Interface().(error)
		}
		out = out[:n-1]
	}
	switch len(out) {
	case 0:
		return nil, nil
	case 1:
		return out[0].Interface(), nil
	}
	values := make([]any, len(out))
	for i, v := range out {
		values[i] = v.Interface()
	}
	return values, nil
}

// wtf_attach streams a session's output (created with terminal.Create) to
// onOutput. The buffer is only valid during the call, and the call blocks
// further output for the session until it returns: that is the
// backpressure. A last call with len -1 says the stream ended, so ctx can be
// released. Exits arrive as the terminal:exit event. Returns NULL, or an
// error to release with wtf_free.
//
//export wtf_attach
func wtf_attach(id *C.cstr_t, onOutput C.wtf_output_fn, ctx unsafe.Pointer) *C.char {
	sess, release, err := attach(goString(id))
	if err != nil {
		return C.CString(err.Error())
	}
	go func() {
		defer release()
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
			C.wtf_call_output(onOutput, ctx, (*C.char)(unsafe.Pointer(&frame[0])), C.int64_t(len(frame)))
		}
		C.wtf_call_output(onOutput, ctx, nil, -1)
	}()
	return nil
}

func attach(id string) (*terminal.Session, func(), error) {
	mu.Lock()
	b := be
	mu.Unlock()
	if b == nil {
		return nil, nil, errors.New("not initialised")
	}
	return terminal.Attach(b.Terminal, id)
}

// wtf_write sends keyboard input to a session. It can block while the
// shell isn't reading; call it off the main thread.
//
//export wtf_write
func wtf_write(id, buf *C.cstr_t, n C.int64_t) {
	mu.Lock()
	b := be
	mu.Unlock()
	if b == nil || n <= 0 {
		return
	}
	if sess := terminal.Lookup(b.Terminal, goString(id)); sess != nil {
		_, _ = sess.Write(C.GoBytes(unsafe.Pointer(buf), C.int(n)))
	}
}

// wtf_set_focused tells the backend whether a window is in front; polling
// pauses while none is. gone=1 forgets a closed window.
//
//export wtf_set_focused
func wtf_set_focused(window *C.cstr_t, focused, gone C.int) {
	mu.Lock()
	b := be
	mu.Unlock()
	if b == nil {
		return
	}
	if gone != 0 {
		b.Active.Forget(goString(window))
		return
	}
	b.Active.SetFocused(goString(window), focused != 0)
}

func goString(p *C.cstr_t) string { return C.GoString((*C.char)(unsafe.Pointer(p))) }

// wtf_free releases a string returned by this library.
//
//export wtf_free
func wtf_free(p *C.char) { C.free(unsafe.Pointer(p)) }

func main() {}

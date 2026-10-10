package terminal

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"
)

// Recording a session to an asciicast v2 file (asciinema's format; play it
// with `asciinema play`, or upload it). The shell's output is written as it
// is read from the pty, with its timing, plus resizes. What you type isn't
// recorded — only what the terminal shows. Files are created 0600 since
// output can hold secrets.

// flushEvery bounds how much recorded output can sit in memory, unwritten.
const flushEvery = time.Second

// RecordingStatus describes a session's recording.
type RecordingStatus struct {
	Active bool   `json:"active"`
	Path   string `json:"path"`
	// Seconds since the recording started.
	Seconds float64 `json:"seconds"`
	// Bytes of output recorded.
	Bytes int64 `json:"bytes"`
}

type recorder struct {
	mu      sync.Mutex
	f       *os.File
	w       *bufio.Writer
	path    string
	start   time.Time
	bytes   int64
	pending []byte // an incomplete UTF-8 sequence at the end of the last chunk
	err     error  // the first write error; recording stops there
	done    chan struct{}
	closed  bool
}

type castHeader struct {
	Version   int               `json:"version"`
	Width     int               `json:"width"`
	Height    int               `json:"height"`
	Timestamp int64             `json:"timestamp"`
	Title     string            `json:"title,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
}

func newRecorder(path string, cols, rows uint16, title, shell string, now time.Time) (*recorder, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("not an absolute path: %q", path)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	r := &recorder{f: f, w: bufio.NewWriterSize(f, 64*1024), path: path, start: now, done: make(chan struct{})}
	head, _ := json.Marshal(castHeader{
		Version: 2, Width: int(cols), Height: int(rows), Timestamp: now.Unix(), Title: title,
		Env: map[string]string{"SHELL": shell, "TERM": "xterm-256color"},
	})
	r.w.Write(head)
	r.w.WriteByte('\n')
	go r.flusher()
	return r, nil
}

func (r *recorder) flusher() {
	t := time.NewTicker(flushEvery)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			r.mu.Lock()
			if !r.closed && r.err == nil {
				r.err = r.w.Flush()
			}
			r.mu.Unlock()
		case <-r.done:
			return
		}
	}
}

// event appends [seconds, kind, data]; the caller holds mu.
func (r *recorder) event(at time.Time, kind, data string) {
	if r.closed || r.err != nil {
		return
	}
	line, _ := json.Marshal([]any{json.RawMessage(strconv.FormatFloat(at.Sub(r.start).Seconds(), 'f', 6, 64)), kind, data})
	_, r.err = r.w.Write(append(line, '\n'))
}

// output records a chunk of pty output. A UTF-8 character split between
// chunks is held back until its end arrives, since events are strings.
func (r *recorder) output(p []byte, at time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bytes += int64(len(p))
	data := append(r.pending, p...)
	cut := len(data)
	// Back up over a trailing partial character (at most 3 bytes).
	for i := 1; i <= 3 && i <= len(data); i++ {
		b := data[len(data)-i]
		if b < 0x80 {
			break
		}
		if utf8.RuneStart(b) {
			if !utf8.FullRune(data[len(data)-i:]) {
				cut = len(data) - i
			}
			break
		}
	}
	r.pending = append([]byte(nil), data[cut:]...)
	if cut > 0 {
		r.event(at, "o", string(data[:cut]))
	}
}

func (r *recorder) resize(cols, rows uint16, at time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.event(at, "r", fmt.Sprintf("%dx%d", cols, rows))
}

func (r *recorder) status(now time.Time) RecordingStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	return RecordingStatus{Active: !r.closed, Path: r.path, Seconds: now.Sub(r.start).Seconds(), Bytes: r.bytes}
}

// close writes what's left and closes the file; safe to call twice.
func (r *recorder) close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return r.err
	}
	if len(r.pending) > 0 {
		r.event(time.Now(), "o", string(r.pending)) // invalid UTF-8 becomes U+FFFD
		r.pending = nil
	}
	r.closed = true
	close(r.done)
	err := errors.Join(r.err, r.w.Flush(), r.f.Close())
	r.err = err
	return err
}

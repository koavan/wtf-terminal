package terminal

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

const (
	// Stop sending once the client is this far behind on rendering. Keeps
	// `cat huge.log` from piling megabytes into xterm.js and freezing the UI.
	highWater = 1 << 20
	// Upper bound for coalescing queued PTY chunks into one frame.
	maxFrame = 256 * 1024
	// While output streams, gather it for up to batchWindow before sending.
	// Programs writing to a terminal flush every line, so `seq 1 1000000`
	// arrives as a million tiny reads; one frame each cost more CPU (Go,
	// WebKit's networking process, the page) than drawing them. Output after
	// a quiet spell of streamGap, such as a keystroke's echo, goes at once.
	batchWindow = 4 * time.Millisecond
	streamGap   = 10 * time.Millisecond
)

// Wire protocol, per session connection:
//
//	server → client  binary: raw PTY output
//	client → server  binary: raw keyboard input
//	client → server  text:   "a<n>" = client has rendered n bytes in total
type streamServer struct {
	token    string
	listener net.Listener
	server   *http.Server
	lookup   func(id string) *Session
}

func newStreamServer(lookup func(string) *Session) (*streamServer, error) {
	tok := make([]byte, 32)
	if _, err := rand.Read(tok); err != nil {
		return nil, err
	}
	// Loopback only, random port: nothing outside this machine can reach it,
	// and the token keeps other local processes out.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	sv := &streamServer{token: hex.EncodeToString(tok), listener: ln, lookup: lookup}
	mux := http.NewServeMux()
	mux.HandleFunc("/pty", sv.handle)
	sv.server = &http.Server{Handler: mux}
	go func() { _ = sv.server.Serve(ln) }()
	return sv, nil
}

func (sv *streamServer) url() string { return "ws://" + sv.listener.Addr().String() + "/pty" }

func (sv *streamServer) close() { _ = sv.server.Close() }

func (sv *streamServer) handle(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if subtle.ConstantTimeCompare([]byte(q.Get("token")), []byte(sv.token)) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	sess := sv.lookup(q.Get("id"))
	if sess == nil {
		http.Error(w, "no such session", http.StatusNotFound)
		return
	}
	if !sess.attachMu.TryLock() {
		http.Error(w, "session already attached", http.StatusConflict)
		return
	}
	defer sess.attachMu.Unlock()

	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// A client's origin can be anything (a web view's own scheme, a dev
		// server); the token is what authenticates the client.
		InsecureSkipVerify: true,
		CompressionMode:    websocket.CompressionDisabled,
	})
	if err != nil {
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(1 << 20)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	var acked atomic.Int64
	ackNotify := make(chan struct{}, 1)

	go func() {
		defer cancel()
		for {
			typ, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			switch typ {
			case websocket.MessageBinary:
				if _, err := sess.Write(data); err != nil {
					return
				}
			case websocket.MessageText:
				if len(data) > 1 && data[0] == 'a' {
					if n, err := strconv.ParseInt(string(data[1:]), 10, 64); err == nil {
						acked.Store(n)
						select {
						case ackNotify <- struct{}{}:
						default:
						}
					}
				}
			}
		}
	}()

	var sent int64
	var lastSend time.Time
	batch := time.NewTimer(time.Hour)
	batch.Stop()
	for {
		for sent-acked.Load() > highWater {
			select {
			case <-ackNotify:
			case <-ctx.Done():
				return
			}
		}

		var frame []byte
		var ok bool
		select {
		case frame, ok = <-sess.Output:
		case <-ctx.Done():
			return
		}
		if !ok {
			c.Close(websocket.StatusNormalClosure, "exited")
			return
		}

		// Coalesce whatever else is already queued into the same frame.
		closed := false
	coalesce:
		for len(frame) < maxFrame {
			select {
			case more, moreOK := <-sess.Output:
				if !moreOK {
					closed = true
					break coalesce
				}
				frame = append(frame, more...)
			default:
				break coalesce
			}
		}

		// Mid-stream, wait a moment for more rather than send a sliver.
		if !closed && len(frame) < maxFrame && time.Since(lastSend) < streamGap {
			batch.Reset(batchWindow)
		gather:
			for len(frame) < maxFrame {
				select {
				case more, moreOK := <-sess.Output:
					if !moreOK {
						closed = true
						break gather
					}
					frame = append(frame, more...)
				case <-batch.C:
					break gather
				case <-ctx.Done():
					return
				}
			}
			batch.Stop()
		}

		if err := c.Write(ctx, websocket.MessageBinary, frame); err != nil {
			return
		}
		lastSend = time.Now()
		sent += int64(len(frame))
		if closed {
			c.Close(websocket.StatusNormalClosure, "exited")
			return
		}
	}
}

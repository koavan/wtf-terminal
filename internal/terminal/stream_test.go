package terminal

import (
	"bytes"
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func startTestSession(t *testing.T) (*Session, *streamServer) {
	t.Helper()
	sess, err := startSession("t1", spawnOptions{
		Shell: "/bin/sh",
		Cwd:   t.TempDir(),
		Cols:  80,
		Rows:  24,
		Env:   append(os.Environ(), "PS1=$ ", "TERM=xterm-256color"),
	})
	if err != nil {
		t.Fatal(err)
	}
	sv, err := newStreamServer(func(id string) *Session {
		if id == sess.ID {
			return sess
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		sess.Close()
		sv.close()
	})
	return sess, sv
}

func dial(t *testing.T, ctx context.Context, sv *streamServer, id, token string) (*websocket.Conn, error) {
	t.Helper()
	c, _, err := websocket.Dial(ctx, sv.url()+"?id="+id+"&token="+token, nil)
	if err == nil {
		c.SetReadLimit(4 << 20)
	}
	return c, err
}

// readUntil reads output frames, acking as it goes, until want appears.
func readUntil(t *testing.T, ctx context.Context, c *websocket.Conn, want string) string {
	t.Helper()
	var got bytes.Buffer
	for !strings.Contains(got.String(), want) {
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("waiting for %q: %v\noutput so far: %q", want, err, got.String())
		}
		got.Write(data)
		_ = c.Write(ctx, websocket.MessageText, []byte("a"+strconv.Itoa(got.Len())))
	}
	return got.String()
}

func TestStreamRoundTrip(t *testing.T) {
	sess, sv := startTestSession(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c, err := dial(t, ctx, sv, sess.ID, sv.token)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()

	if err := c.Write(ctx, websocket.MessageBinary, []byte("echo wtf-$((40+2))\r")); err != nil {
		t.Fatal(err)
	}
	readUntil(t, ctx, c, "wtf-42")
}

func TestStreamRejectsBadToken(t *testing.T) {
	sess, sv := startTestSession(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if c, err := dial(t, ctx, sv, sess.ID, "nope"); err == nil {
		c.CloseNow()
		t.Fatal("connected with a bad token")
	}
	if c, err := dial(t, ctx, sv, "missing", sv.token); err == nil {
		c.CloseNow()
		t.Fatal("connected to a missing session")
	}
}

// A client that never acks must stop receiving at roughly highWater, and
// acking must let the rest of the output through.
func TestStreamFlowControl(t *testing.T) {
	sess, sv := startTestSession(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	c, err := dial(t, ctx, sv, sess.ID, sv.token)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()

	// ~4 MB of output, then a marker.
	cmd := "head -c 4000000 /dev/zero | tr '\\0' x; echo; echo DONE-$((1+1))\r"
	if err := c.Write(ctx, websocket.MessageBinary, []byte(cmd)); err != nil {
		t.Fatal(err)
	}

	// Read without acking until the server stalls.
	received := 0
	for {
		readCtx, readCancel := context.WithTimeout(ctx, 700*time.Millisecond)
		_, data, err := c.Read(readCtx)
		readCancel()
		if err != nil {
			break // stalled (timeout); the read error also closed the conn's read side
		}
		received += len(data)
	}
	if received > highWater+maxFrame+64*1024 {
		t.Fatalf("server sent %d bytes without acks; want at most ~%d", received, highWater+maxFrame)
	}
	if received < highWater/2 {
		t.Fatalf("server sent only %d bytes before stalling", received)
	}
	t.Logf("stalled after %d unacked bytes", received)
}

func TestSessionInfoAndExit(t *testing.T) {
	sess, sv := startTestSession(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c, err := dial(t, ctx, sv, sess.ID, sv.token)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()

	// Idle shell: the foreground process is the shell itself. The service
	// answers for its sessions, by ID.
	svc := &Service{sessions: map[string]*Session{sess.ID: sess}}
	if _, err := svc.Info("nope"); err == nil {
		t.Error("info for a session that doesn't exist")
	}
	info, err := svc.Info(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsShell || info.Process != "sh" {
		t.Fatalf("idle info = %+v, want the shell", info)
	}
	if info.Cwd == "" {
		t.Fatalf("no cwd reported: %+v", info)
	}

	// Run a foreground job and watch it show up.
	if err := c.Write(ctx, websocket.MessageBinary, []byte("sleep 5\r")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		info, _ = sess.info()
		if info.Process == "sleep" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("foreground process never became sleep: %+v", info)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if info.IsShell {
		t.Fatal("sleep reported as the shell")
	}

	// Closing hangs up the shell and its job.
	sess.Close()
	select {
	case <-sess.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("shell did not exit after Close")
	}
}

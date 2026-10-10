package terminal

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// readCast returns a cast file's header and events.
func readCast(t *testing.T, path string) (castHeader, [][]any) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	var head castHeader
	var events [][]any
	for i := 0; sc.Scan(); i++ {
		if i == 0 {
			if err := json.Unmarshal(sc.Bytes(), &head); err != nil {
				t.Fatalf("header: %v", err)
			}
			continue
		}
		var ev []any
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			t.Fatalf("event %d: %v (%s)", i, err, sc.Text())
		}
		events = append(events, ev)
	}
	return head, events
}

func TestRecorderWritesAsciicast(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.cast")
	start := time.Unix(1_800_000_000, 0)
	r, err := newRecorder(path, 120, 40, "deploy", "zsh", start)
	if err != nil {
		t.Fatal(err)
	}
	r.output([]byte("hello \xe2\x9c"), start.Add(500*time.Millisecond)) // "✓" split across chunks
	r.output([]byte("\x93\r\n"), start.Add(time.Second))
	r.resize(80, 24, start.Add(2*time.Second))
	if err := r.close(); err != nil {
		t.Fatal(err)
	}
	if err := r.close(); err != nil {
		t.Fatalf("second close: %v", err)
	}

	head, events := readCast(t, path)
	if head.Version != 2 || head.Width != 120 || head.Height != 40 || head.Title != "deploy" || head.Timestamp != start.Unix() {
		t.Errorf("header %+v", head)
	}
	want := [][]any{{0.5, "o", "hello "}, {1.0, "o", "✓\r\n"}, {2.0, "r", "80x24"}}
	if len(events) != len(want) {
		t.Fatalf("events %v, want %v", events, want)
	}
	for i, ev := range events {
		if ev[0].(float64) != want[i][0] || ev[1] != want[i][1] || ev[2] != want[i][2] {
			t.Errorf("event %d = %v, want %v", i, ev, want[i])
		}
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want 0600: recordings can hold secrets", fi.Mode().Perm())
	}
	if st := r.status(start); st.Bytes != 11 || st.Active {
		t.Errorf("status %+v", st)
	}
}

func TestRecorderNeedsAnAbsolutePath(t *testing.T) {
	if _, err := newRecorder("rel.cast", 80, 24, "", "sh", time.Now()); err == nil {
		t.Error("a relative path was accepted")
	}
}

func TestSessionRecording(t *testing.T) {
	sess, err := startSession("t1", spawnOptions{
		Shell: "/bin/sh", Args: []string{"sh", "-c", "sleep 0.3; printf recorded-output; sleep 0.2"},
		Cwd: t.TempDir(), Cols: 100, Rows: 30, Env: []string{"PATH=/usr/bin:/bin"},
	})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for range sess.Output {
		}
	}()
	path := filepath.Join(t.TempDir(), "rec.cast")
	if err := sess.StartRecording(path, "test"); err != nil {
		t.Fatal(err)
	}
	if err := sess.StartRecording(path, "again"); err == nil {
		t.Error("a second recording of the same session was started")
	}
	if !sess.Recording().Active {
		t.Error("not reported as recording")
	}
	if err := sess.Resize(90, 20); err != nil {
		t.Fatal(err)
	}
	<-sess.Done
	// The recording ends with the shell's output.
	deadline := time.Now().Add(2 * time.Second)
	for sess.Recording().Active && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if sess.Recording().Active {
		t.Fatal("recording still active after the shell exited")
	}
	head, events := readCast(t, path)
	if head.Width != 100 || head.Height != 30 {
		t.Errorf("header size %dx%d, want 100x30", head.Width, head.Height)
	}
	var out strings.Builder
	resized := false
	for _, ev := range events {
		switch ev[1] {
		case "o":
			out.WriteString(ev[2].(string))
		case "r":
			resized = ev[2] == "90x20"
		}
	}
	if !strings.Contains(out.String(), "recorded-output") {
		t.Errorf("output %q doesn't have the command's output", out.String())
	}
	if !resized {
		t.Error("the resize wasn't recorded")
	}
}

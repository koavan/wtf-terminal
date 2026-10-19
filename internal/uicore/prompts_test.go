package uicore

import (
	"testing"
	"time"
)

// fakeGrid is a terminal's rows, the ones listed in wrapped continuing on
// the next row.
type fakeGrid struct {
	rows    []string
	wrapped map[int]bool
	cursor  int
}

func (g *fakeGrid) Row(r int) string {
	if r < 0 || r >= len(g.rows) {
		return ""
	}
	return g.rows[r]
}
func (g *fakeGrid) EndsLine(r int) bool { return !g.wrapped[r] }
func (g *fakeGrid) Cursor() (int, int)  { return 0, g.cursor }

// print writes rows at the cursor and moves it below them.
func (g *fakeGrid) print(rows ...string) {
	for _, r := range rows {
		for len(g.rows) <= g.cursor {
			g.rows = append(g.rows, "")
		}
		g.rows[g.cursor] = r
		g.cursor++
	}
}

type session struct {
	g   *fakeGrid
	p   *PromptTracker
	clk time.Time
}

func newSession() *session {
	s := &session{g: &fakeGrid{wrapped: map[int]bool{}}, clk: time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC)}
	s.p = NewPromptTracker(s.g, NewLines(s.g))
	s.p.now = func() time.Time { return s.clk }
	return s
}

// run is one prompt, command and output, as the shell scripts report them.
func (s *session) run(cmd string, code string, took time.Duration, output ...string) {
	s.p.OSC133("A", 0, s.g.cursor)
	s.g.print("$ " + cmd)
	s.p.OSC633("E;" + cmd)
	s.p.OSC133("C", 0, s.g.cursor)
	s.g.print(output...)
	s.clk = s.clk.Add(took)
	s.p.OSC133("D;"+code, 0, s.g.cursor)
}

func TestPromptBlocks(t *testing.T) {
	s := newSession()
	var started []string
	var ended []int
	s.p.OnCommandStart = func(text string, _ int) { started = append(started, text) }
	s.p.OnCommandEnd = func(_, code int, _ time.Duration) { ended = append(ended, code) }
	s.run("ls", "0", 200*time.Millisecond, "a", "b")
	s.run("false", "1", 3*time.Second)
	s.p.OSC133("A", 0, s.g.cursor)
	s.g.print("$ ")

	if len(started) != 2 || started[0] != "ls" || started[1] != "false" || len(ended) != 2 || ended[1] != 1 {
		t.Fatalf("started %q ended %v", started, ended)
	}
	blocks := s.p.Blocks(0, 100)
	if len(blocks) != 3 || blocks[0].Row != 0 || blocks[1].Row != 3 || blocks[2].Row != 4 {
		t.Fatalf("blocks %+v", blocks)
	}
	b := blocks[1].Block
	if b.Text != "false" || b.Running() || *b.ExitCode != 1 || b.Duration != 3*time.Second {
		t.Errorf("block %+v", b)
	}
	if !blocks[2].Block.Running() {
		t.Error("the open prompt isn't running")
	}
	if out, ok := s.p.Output(blocks[0].Block.ID); !ok || out != "a\nb" {
		t.Errorf("output %q %v", out, ok)
	}
	if out, ok := s.p.Output(blocks[1].Block.ID); !ok || out != "" {
		t.Errorf("empty output %q %v", out, ok)
	}
	if id, ok := s.p.LastFinished(); !ok || id != blocks[1].Block.ID {
		t.Errorf("last finished %d %v", id, ok)
	}
	if id, ok := s.p.BlockAt(2); !ok || id != blocks[0].Block.ID {
		t.Errorf("block at row 2: %d %v", id, ok)
	}
	if _, ok := s.p.BlockAt(4); ok {
		t.Error("a prompt with no command has no block")
	}
	if f, l, ok := s.p.BlockRows(blocks[0].Block.ID); !ok || f != 0 || l != 2 {
		t.Errorf("block rows %d-%d %v", f, l, ok)
	}
	if r, ok := s.p.Jump(-1, 4); !ok || r != 3 {
		t.Errorf("previous prompt %d %v", r, ok)
	}
	if r, ok := s.p.Jump(1, 0); !ok || r != 3 {
		t.Errorf("next prompt %d %v", r, ok)
	}
	if _, ok := s.p.Jump(1, 4); ok {
		t.Error("no prompt after the last")
	}
}

func TestPromptRunningOutputFollowsTheCursor(t *testing.T) {
	s := newSession()
	s.p.OSC133("A", 0, 0)
	s.g.print("$ tail -f log")
	s.p.OSC633("E;tail -f log")
	s.p.OSC133("C", 0, s.g.cursor)
	s.g.print("one", "two")
	id, ok := s.p.Running()
	if !ok {
		t.Fatal("nothing running")
	}
	// The cursor sits on the row after the output.
	if out, _ := s.p.Output(id); out != "one\ntwo" {
		t.Errorf("output %q", out)
	}
}

func TestPromptAtOrAboveAnEarlierOneMeansCleared(t *testing.T) {
	s := newSession()
	s.run("ls", "0", 0, "a")
	s.run("ls", "0", 0, "a")
	// clear: the cursor goes home and the next prompt is drawn on row 0.
	s.g.rows, s.g.cursor = nil, 0
	s.p.OSC133("A", 0, 0)
	if b := s.p.Blocks(0, 100); len(b) != 1 {
		t.Errorf("blocks after clear: %+v", b)
	}
}

func TestPromptMarksDropWhenTheirLinesGo(t *testing.T) {
	s := newSession()
	s.run("echo hi", "0", 0, "hi")
	s.p.OSC133("A", 0, s.g.cursor)
	s.g.print("$ ")
	// The first rows leave the scrollback.
	s.g.rows[0], s.g.rows[1] = "", ""
	s.p.Cleared()
	if b := s.p.Blocks(0, 100); len(b) != 1 || b[0].Row != 2 {
		t.Errorf("blocks %+v", b)
	}
	if _, ok := s.p.LastFinished(); ok {
		t.Error("a finished command whose output left still counts")
	}
}

func TestTextOfJoinsWrappedRows(t *testing.T) {
	g := &fakeGrid{rows: []string{"abcd", "ef  ", "gh  "}, wrapped: map[int]bool{0: true}}
	if got := TextOf(g, 0, 2); got != "abcdef\ngh\n" {
		t.Errorf("got %q", got)
	}
}

func TestUnescape(t *testing.T) {
	if got := Unescape(`echo a\\b \x3b c\x0a`); got != "echo a\\b ; c\n" {
		t.Errorf("got %q", got)
	}
}

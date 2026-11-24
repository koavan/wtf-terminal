package snake

import (
	"strings"
	"testing"
)

func TestMovesAndEats(t *testing.T) {
	g := New(10, 8, 1)
	head := g.Snake[0]
	g.Food, g.BonusLeft = Point{head.X + 1, head.Y}, 0
	g.Step()
	if g.Snake[0] != (Point{head.X + 1, head.Y}) || g.Score != fruitScore || g.Eaten != 1 {
		t.Fatalf("after eating: head %v score %d eaten %d", g.Snake[0], g.Score, g.Eaten)
	}
	if g.Food == g.Snake[0] {
		t.Fatal("new fruit placed on the snake")
	}
	n := len(g.Snake)
	g.Food = Point{0, 0}
	g.Step()
	if len(g.Snake) != n+1 {
		t.Fatalf("grew to %d, want %d", len(g.Snake), n+1)
	}
}

func TestWallEndsTheRound(t *testing.T) {
	g := New(6, 4, 1)
	g.Food = Point{0, 0}
	for range g.W {
		g.Step()
	}
	if !g.Over {
		t.Fatal("ran through the right wall")
	}
}

func TestNoTurningBack(t *testing.T) {
	g := New(10, 8, 1)
	g.Turn(Left)
	g.Step()
	if g.Over || g.Heading() != Right {
		t.Fatalf("turned back on itself: over %v heading %v", g.Over, g.Heading())
	}
	// Two quick turns both happen, one per move.
	g.Turn(Up)
	g.Turn(Left)
	g.Step()
	g.Step()
	if g.Heading() != Left || g.Over {
		t.Fatalf("heading %v over %v, want left", g.Heading(), g.Over)
	}
}

func TestHitsItself(t *testing.T) {
	g := New(10, 10, 1)
	g.Food = Point{0, 0}
	g.Snake = []Point{{5, 5}, {4, 5}, {3, 5}, {3, 4}, {4, 4}, {5, 4}, {6, 4}}
	g.Turn(Up) // into {5,4}
	g.Step()
	if !g.Over {
		t.Fatal("ran through its own body")
	}
}

func TestChasesItsOwnTail(t *testing.T) {
	// A 2×2 loop: the head moves where the tail just left.
	g := New(10, 10, 1)
	g.Food = Point{9, 9}
	g.Snake = []Point{{5, 5}, {5, 6}, {6, 6}, {6, 5}}
	g.dir = Up
	g.Turn(Right)
	g.Step()
	if g.Over {
		t.Fatal("hit the tail that was moving away")
	}
}

func TestStarAfterFiveFruit(t *testing.T) {
	g := New(20, 10, 1)
	for i := range bonusEvery {
		h := g.Snake[0]
		g.Food = Point{h.X + 1, h.Y}
		if i == bonusEvery-1 && g.BonusLeft != 0 {
			t.Fatal("star before the fifth fruit")
		}
		g.Step()
	}
	if g.BonusLeft != bonusTicks {
		t.Fatalf("star left %d, want %d", g.BonusLeft, bonusTicks)
	}
	before := g.Score
	h := g.Snake[0]
	g.Bonus = Point{h.X + 1, h.Y}
	g.Food = Point{0, 0}
	g.Step()
	if g.BonusLeft != 0 || g.Score <= before {
		t.Fatalf("star not eaten: left %d score %d→%d", g.BonusLeft, before, g.Score)
	}
}

func TestSpeedsUp(t *testing.T) {
	g := New(10, 8, 1)
	slow := g.Interval()
	g.Eaten = 10
	if g.Interval() >= slow {
		t.Fatal("no quicker after ten fruit")
	}
	g.Eaten = 1000
	if g.Interval() < 55e6 {
		t.Fatal("too quick to play")
	}
}

func TestParseKeys(t *testing.T) {
	got := parseKeys([]byte("\x1b[A\x1bOBwasdp q\r\x03"))
	want := []key{keyUp, keyDown, keyUp, keyLeft, keyDown, keyRight, keyPause, keyPause, keyQuit, keyAgain, keyQuit}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	if k := parseKeys([]byte{0x1b}); len(k) != 1 || k[0] != keyQuit {
		t.Fatalf("Esc alone: %v", k)
	}
}

func TestFrameShowsTheBoard(t *testing.T) {
	g := New(12, 6, 1)
	f := frame(g, 30, false, 80, 24)
	for _, s := range []string{head, body, g.FoodEmoji, wall, "SNAKE", "🏆 30"} {
		if !strings.Contains(f, s) {
			t.Errorf("frame lacks %q", s)
		}
	}
	g.Over = true
	if f := frame(g, 30, false, 80, 24); !strings.Contains(f, crash) || !strings.Contains(f, "Game over") {
		t.Error("game over frame lacks the crash")
	}
}

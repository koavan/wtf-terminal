// Package snake is the classic snake game, played in a wtf tab: the
// command palette's Play Snake runs `wtf-mcp snake`, which draws it with
// emoji in the terminal. game.go is the rules; play.go the terminal.
package snake

import (
	"math/rand/v2"
	"time"
)

// Point is a cell: x across, y down, from the top left.
type Point struct{ X, Y int }

// Dir is where the snake is heading.
type Dir int

const (
	Up Dir = iota
	Down
	Left
	Right
)

func (d Dir) step() Point {
	switch d {
	case Up:
		return Point{0, -1}
	case Down:
		return Point{0, 1}
	case Left:
		return Point{-1, 0}
	}
	return Point{1, 0}
}

func (d Dir) opposite() Dir { return [...]Dir{Down, Up, Right, Left}[d] }

// Fruit are what the snake eats, one shown at a time.
var Fruit = []string{"🍎", "🍌", "🍇", "🍓", "🍒", "🍑", "🍍", "🥝"}

const (
	// A star shows after every bonusEvery fruit, for bonusTicks moves.
	bonusEvery = 5
	bonusTicks = 30
	fruitScore = 10
	// A star is worth more the sooner it's eaten: bonusScore at most.
	bonusScore = 50
)

// Game is one round on a w×h board walled in on every side.
type Game struct {
	W, H  int
	Snake []Point // the head first
	dir   Dir
	// turns are the turns typed since the last move, at most two, so a
	// quick ↑→ makes both.
	turns []Dir

	Food      Point
	FoodEmoji string
	// Bonus is the star's cell while BonusLeft > 0.
	Bonus     Point
	BonusLeft int

	Score, Eaten int
	Over         bool
	grow         int
	rnd          *rand.Rand
}

// New starts a round: a snake of three in the middle, heading right.
func New(w, h int, seed uint64) *Game {
	g := &Game{W: w, H: h, dir: Right, rnd: rand.New(rand.NewPCG(seed, seed^0x5eed))}
	x, y := w/2, h/2
	g.Snake = []Point{{x, y}, {x - 1, y}, {x - 2, y}}
	g.placeFood()
	return g
}

// Turn asks to head d at the next move. Turning back on itself, or the
// way it's already going, does nothing.
func (g *Game) Turn(d Dir) {
	last := g.dir
	if n := len(g.turns); n > 0 {
		last = g.turns[n-1]
	}
	if d == last || d == last.opposite() || len(g.turns) == 2 {
		return
	}
	g.turns = append(g.turns, d)
}

// Heading is the way the snake is going.
func (g *Game) Heading() Dir { return g.dir }

// Step moves the snake one cell: it eats what's there, or hits a wall or
// itself and the round is over.
func (g *Game) Step() {
	if g.Over {
		return
	}
	if len(g.turns) > 0 {
		g.dir, g.turns = g.turns[0], g.turns[1:]
	}
	d := g.dir.step()
	head := Point{g.Snake[0].X + d.X, g.Snake[0].Y + d.Y}
	if head.X < 0 || head.Y < 0 || head.X >= g.W || head.Y >= g.H {
		g.Over = true
		return
	}
	// The tail moves out of the way, unless the snake is growing.
	body := g.Snake
	if g.grow == 0 {
		body = body[:len(body)-1]
	}
	for _, p := range body {
		if p == head {
			g.Over = true
			return
		}
	}
	g.Snake = append([]Point{head}, body...)
	if g.grow > 0 {
		g.grow--
	}

	if g.BonusLeft > 0 {
		if head == g.Bonus {
			g.Score += bonusScore * g.BonusLeft / bonusTicks
			g.BonusLeft = 0
			g.grow += 2
		} else {
			g.BonusLeft--
		}
	}
	if head == g.Food {
		g.Eaten++
		g.Score += fruitScore
		g.grow++
		g.placeFood()
		if g.Eaten%bonusEvery == 0 {
			if p, ok := g.freeCell(); ok {
				g.Bonus, g.BonusLeft = p, bonusTicks
			}
		}
	}
}

// Interval is how long a move takes: quicker with each fruit eaten.
func (g *Game) Interval() time.Duration {
	ms := 140 - 4*g.Eaten
	return time.Duration(max(ms, 55)) * time.Millisecond
}

func (g *Game) placeFood() {
	if p, ok := g.freeCell(); ok {
		g.Food = p
		g.FoodEmoji = Fruit[g.rnd.IntN(len(Fruit))]
	} else {
		// The snake fills the board.
		g.Over = true
	}
}

// freeCell picks an empty cell at random.
func (g *Game) freeCell() (Point, bool) {
	taken := make(map[Point]bool, len(g.Snake)+2)
	for _, p := range g.Snake {
		taken[p] = true
	}
	taken[g.Food] = true
	if g.BonusLeft > 0 {
		taken[g.Bonus] = true
	}
	var free []Point
	for y := range g.H {
		for x := range g.W {
			if p := (Point{x, y}); !taken[p] {
				free = append(free, p)
			}
		}
	}
	if len(free) == 0 {
		return Point{}, false
	}
	return free[g.rnd.IntN(len(free))], true
}

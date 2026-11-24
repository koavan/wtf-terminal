package snake

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Emoji are two columns wide, so each cell is two columns.
const (
	wall  = "🧱"
	head  = "🐍"
	body  = "🟩"
	star  = "⭐"
	crash = "💥"
)

// The board's size: at most maxW×maxH cells, smaller in a small window.
const (
	maxW, maxH = 28, 18
	minW, minH = 10, 6
)

type key int

const (
	keyNone key = iota
	keyUp
	keyDown
	keyLeft
	keyRight
	keyPause
	keyQuit
	keyAgain
)

// Run plays in the terminal on stdin/stdout until q, Esc or Ctrl-C.
func Run() error {
	in, out := int(os.Stdin.Fd()), os.Stdout
	saved, err := unix.IoctlGetTermios(in, getTermios)
	if err != nil {
		return errors.New("snake needs a terminal")
	}
	raw := *saved
	raw.Lflag &^= unix.ICANON | unix.ECHO | unix.ISIG | unix.IEXTEN
	raw.Iflag &^= unix.IXON | unix.ICRNL
	raw.Cc[unix.VMIN], raw.Cc[unix.VTIME] = 1, 0
	if err := unix.IoctlSetTermios(in, setTermios, &raw); err != nil {
		return err
	}
	// The other screen, no cursor; both put back however it ends.
	fmt.Fprint(out, "\x1b[?1049h\x1b[?25l\x1b[2J")
	defer func() {
		fmt.Fprint(out, "\x1b[0m\x1b[?25h\x1b[?1049l")
		unix.IoctlSetTermios(in, setTermios, saved)
	}()

	keys := make(chan key, 8)
	go readKeys(os.Stdin, keys)
	resized := make(chan os.Signal, 1)
	signal.Notify(resized, syscall.SIGWINCH)
	defer signal.Stop(resized)
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(stop)

	best := loadBest()
	newGame := func() *Game {
		w, h := boardSize(in)
		return New(w, h, uint64(time.Now().UnixNano()))
	}
	g := newGame()
	paused := false
	tick := time.NewTimer(g.Interval())
	defer tick.Stop()
	draw := func() {
		cols, rows := termSize(in)
		out.WriteString(frame(g, best, paused, cols, rows))
	}
	draw()
	for {
		select {
		case <-stop:
			return nil
		case <-resized:
			out.WriteString("\x1b[2J")
			draw()
		case k := <-keys:
			switch k {
			case keyQuit:
				return nil
			case keyPause:
				if !g.Over {
					paused = !paused
					draw()
				}
			case keyAgain:
				if g.Over {
					g, paused = newGame(), false
					out.WriteString("\x1b[2J")
					tick.Reset(g.Interval())
					draw()
				}
			case keyUp, keyDown, keyLeft, keyRight:
				if !g.Over && !paused {
					g.Turn([...]Dir{keyUp: Up, keyDown: Down, keyLeft: Left, keyRight: Right}[k])
				}
			}
		case <-tick.C:
			if !g.Over && !paused {
				g.Step()
				if g.Over && g.Score > best {
					best = g.Score
					saveBest(best)
				}
				draw()
			}
			tick.Reset(g.Interval())
		}
	}
}

// readKeys turns what's typed into keys: arrows, WASD, hjkl, P or space,
// Q, Esc or Ctrl-C, R or Return.
func readKeys(f *os.File, keys chan<- key) {
	buf := make([]byte, 64)
	for {
		n, err := f.Read(buf)
		if err != nil {
			keys <- keyQuit
			return
		}
		for _, k := range parseKeys(buf[:n]) {
			keys <- k
		}
	}
}

func parseKeys(b []byte) (ks []key) {
	for i := 0; i < len(b); i++ {
		c := b[i]
		if c == 0x1b {
			// ESC [ A or ESC O A (arrows); ESC alone quits.
			if i+2 < len(b) && (b[i+1] == '[' || b[i+1] == 'O') {
				switch b[i+2] {
				case 'A':
					ks = append(ks, keyUp)
				case 'B':
					ks = append(ks, keyDown)
				case 'C':
					ks = append(ks, keyRight)
				case 'D':
					ks = append(ks, keyLeft)
				}
				i += 2
				continue
			}
			if i+1 == len(b) {
				ks = append(ks, keyQuit)
			}
			continue
		}
		switch c {
		case 'w', 'W', 'k', 'K':
			ks = append(ks, keyUp)
		case 's', 'S', 'j', 'J':
			ks = append(ks, keyDown)
		case 'a', 'A', 'h', 'H':
			ks = append(ks, keyLeft)
		case 'd', 'D', 'l', 'L':
			ks = append(ks, keyRight)
		case 'p', 'P', ' ':
			ks = append(ks, keyPause)
		case 'q', 'Q', 3: // Ctrl-C
			ks = append(ks, keyQuit)
		case 'r', 'R', '\r', '\n':
			ks = append(ks, keyAgain)
		}
	}
	return ks
}

// termSize is the window in columns and rows (80×24 if unknown).
func termSize(fd int) (cols, rows int) {
	if ws, err := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ); err == nil && ws.Col > 0 && ws.Row > 0 {
		return int(ws.Col), int(ws.Row)
	}
	return 80, 24
}

// boardSize fits the board, its walls and three lines of text in the window.
func boardSize(fd int) (w, h int) {
	cols, rows := termSize(fd)
	w = min(maxW, max(minW, cols/2-2))
	h = min(maxH, max(minH, rows-5))
	return w, h
}

// frame is the whole screen for one moment of the game, centered.
func frame(g *Game, best int, paused bool, cols, rows int) string {
	width := (g.W + 2) * 2
	left := max(0, (cols-width)/2)
	top := max(0, (rows-(g.H+5))/2)
	pad := strings.Repeat(" ", left)

	cells := make(map[Point]string, len(g.Snake)+2)
	cells[g.Food] = g.FoodEmoji
	if g.BonusLeft > 0 {
		cells[g.Bonus] = star
	}
	for i := len(g.Snake) - 1; i >= 0; i-- {
		cells[g.Snake[i]] = body
	}
	cells[g.Snake[0]] = head
	if g.Over {
		cells[g.Snake[0]] = crash
	}

	var b strings.Builder
	b.WriteString("\x1b[H")
	b.WriteString(strings.Repeat("\x1b[K\n", top))
	title := fmt.Sprintf("🐍 \x1b[1mSNAKE\x1b[0m   %s %d   🏆 %d", g.FoodEmoji, g.Score, best)
	if g.BonusLeft > 0 {
		title += fmt.Sprintf("   %s %d", star, g.BonusLeft)
	}
	b.WriteString(pad + title + "\x1b[K\n\x1b[K\n")
	walls := pad + strings.Repeat(wall, g.W+2) + "\x1b[K\n"
	b.WriteString(walls)
	for y := range g.H {
		b.WriteString(pad + wall)
		for x := range g.W {
			if c, ok := cells[Point{x, y}]; ok {
				b.WriteString(c)
			} else {
				b.WriteString("  ")
			}
		}
		b.WriteString(wall + "\x1b[K\n")
	}
	b.WriteString(walls)
	switch {
	case g.Over:
		msg := fmt.Sprintf("💀 Game over: %d points", g.Score)
		if g.Score > 0 && g.Score >= best {
			msg = fmt.Sprintf("🎉 New best: %d points!", g.Score)
		}
		b.WriteString(pad + msg + "   R again · Q quit\x1b[K")
	case paused:
		b.WriteString(pad + "⏸️  Paused   P carry on · Q quit\x1b[K")
	default:
		b.WriteString(pad + "\x1b[2m←↑↓→ or WASD move · P pause · Q quit\x1b[0m\x1b[K")
	}
	b.WriteString("\x1b[J")
	return b.String()
}

// The best score, kept in the user's cache folder.
func bestFile() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "wtf", "snake-best")
}

func loadBest() int {
	f := bestFile()
	if f == "" {
		return 0
	}
	b, _ := os.ReadFile(f)
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return n
}

func saveBest(n int) {
	if f := bestFile(); f != "" {
		os.MkdirAll(filepath.Dir(f), 0o755)
		os.WriteFile(f, []byte(strconv.Itoa(n)+"\n"), 0o644)
	}
}

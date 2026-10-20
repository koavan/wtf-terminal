package uicore

import (
	"strings"
	"unicode/utf8"
)

// Coloring the tokens on screen: which cells take which color. Only what's
// on screen, only text no program colored. Port of LogView.paint in
// lib/logview.ts (macos/Sources/WtfCore/LogTints.swift). Not for
// full-screen programs: the caller doesn't ask while the alternate screen
// is up.

// PlainGrid is a Grid that also says which characters of a row are in the
// terminal's default colors (no program colored them).
type PlainGrid interface {
	Grid
	// Plain has one entry per rune of Row(row); missing entries are plain.
	Plain(row int) []bool
}

// LogColors are the colors for the token kinds, from the theme: hex
// `#rrggbb`.
type LogColors struct {
	Error, Warn, Info, Debug, Timestamp, URL, IP, K8s string
}

// NewLogColors takes palette[1] red, [3] yellow, [2] green, [4] blue,
// [5] magenta, [6] cyan; faded is 45% of the way to the background.
func NewLogColors(palette []string, foreground, background string) LogColors {
	p := func(i int) string {
		if i < len(palette) {
			if c, ok := ParseRGB(palette[i]); ok {
				return c.Hex()
			}
		}
		return "#888888"
	}
	fg, bg := rgbOr(foreground, RGB{200, 200, 200, 1}), rgbOr(background, RGB{0, 0, 0, 1})
	muted := fg.Mix(bg, 0.45).Hex()
	return LogColors{Error: p(1), Warn: p(3), Info: p(2), Debug: muted, Timestamp: muted, URL: p(4), IP: p(5), K8s: p(6)}
}

// Color is a token's color; paths and JWTs have none.
func (c LogColors) Color(tok LogToken) (string, bool) {
	switch tok.Kind {
	case LogKindLevel:
		switch tok.Level {
		case LogError:
			return c.Error, true
		case LogWarn:
			return c.Warn, true
		case LogInfo:
			return c.Info, true
		}
		return c.Debug, true
	case LogKindTimestamp, LogKindTime:
		return c.Timestamp, true
	case LogKindURL:
		return c.URL, true
	case LogKindIP:
		return c.IP, true
	case LogKindK8s:
		return c.K8s, true
	}
	return "", false
}

// LogTint is a run of cells on screen to color: Row counts from the top of
// the view.
type LogTint struct {
	Row, Col, Width int
	Color           string
}

// LogMaxTints caps the tints for one screen.
const LogMaxTints = 600

// logPlain says, for each byte of a logical line's text, whether no program
// colored its character; nil when the grid can't tell (all plain).
func logPlain(g Grid, l logical) []bool {
	pg, ok := g.(PlainGrid)
	if !ok {
		return nil
	}
	out := make([]bool, 0, len(l.text))
	for r := l.first; r <= l.last && len(out) < len(l.text); r++ {
		text := g.Row(r)
		if g.EndsLine(r) {
			text = strings.TrimRight(text, " ")
		}
		flags := pg.Plain(r)
		i := 0
		for len(text) > 0 {
			_, n := utf8.DecodeRuneInString(text)
			p := i >= len(flags) || flags[i]
			for range n {
				out = append(out, p)
			}
			text = text[n:]
			i++
		}
	}
	return out
}

// LogTints is the tints for rows [top, top+rows) of the grid.
func LogTints(g Grid, top, rows int, colors LogColors) []LogTint {
	bottom := top + rows - 1
	var out []LogTint
	for y := top; y <= bottom && len(out) < LogMaxTints; {
		l := logicalLine(g, y)
		y = max(y+1, l.last+1)
		if strings.TrimSpace(l.text) == "" {
			continue
		}
		var plain []bool
		for _, tok := range LogScan(l.text) {
			color, ok := colors.Color(tok)
			if !ok || tok.End > len(l.rows) {
				continue
			}
			// A program's own colors win.
			if plain == nil {
				plain = logPlain(g, l)
				if plain == nil {
					plain = []bool{}
				}
			}
			if len(plain) > 0 && !allPlain(plain, tok.Start, tok.End) {
				continue
			}
			// One run per row a wrapped token spans.
			for i := tok.Start; i < tok.End; {
				row := l.rows[i]
				j := i
				for j+1 < tok.End && l.rows[j+1] == row {
					j++
				}
				if row >= top && row <= bottom {
					out = append(out, LogTint{row - top, l.cols[i], l.cols[j] - l.cols[i] + max(1, l.widths[j]), color})
				}
				i = j + 1
			}
			if len(out) >= LogMaxTints {
				break
			}
		}
	}
	return out
}

func allPlain(plain []bool, start, end int) bool {
	for i := start; i < end && i < len(plain); i++ {
		if !plain[i] {
			return false
		}
	}
	return true
}

// LogTokenAt is the token at a cell (absolute row), for Ctrl-click and
// right-click.
func LogTokenAt(g Grid, row, col int) (LogToken, bool) {
	l := logicalLine(g, row)
	for _, tok := range LogScan(l.text) {
		for i := tok.Start; i < min(tok.End, len(l.rows)); i++ {
			if l.rows[i] == row && col >= l.cols[i] && col < l.cols[i]+max(1, l.widths[i]) {
				return tok, true
			}
		}
	}
	return LogToken{}, false
}

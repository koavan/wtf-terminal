package uicore

import (
	"fmt"
	"regexp"
	"testing"
)

// plainGrid is a fakeGrid that knows which characters a program colored.
type plainGrid struct {
	fakeGrid
	colored map[int][]bool
}

func (g *plainGrid) Plain(row int) []bool {
	var out []bool
	for _, c := range g.colored[row] {
		out = append(out, !c)
	}
	return out
}

var testLogColors = NewLogColors([]string{"#000000", "#ff0000", "#00ff00", "#ffff00", "#0000ff", "#ff00ff", "#00ffff", "#ffffff"}, "#ffffff", "#000000")

func tintStrings(ts []LogTint, color bool) []string {
	var out []string
	for _, t := range ts {
		s := fmt.Sprintf("%d,%d,%d", t.Row, t.Col, t.Width)
		if color {
			s += "," + t.Color
		}
		out = append(out, s)
	}
	return out
}

func TestLogTintsWhatsOnScreen(t *testing.T) {
	rows := []string{"12:00:01 ERROR from 10.0.0.1", "WARN pod/a"}
	// WARN was colored by the program (yellow): its colors win.
	g := &plainGrid{fakeGrid{rows: rows, wrapped: map[int]bool{}}, map[int][]bool{1: {true, true, true, true}}}
	expectList(t, "tints", tintStrings(LogTints(g, 0, 6, testLogColors), true),
		[]string{"0,0,8," + testLogColors.Timestamp, "0,9,5,#ff0000", "0,20,8,#ff00ff", "1,5,5,#00ffff"})
	if testLogColors.Timestamp != "#8c8c8c" {
		t.Errorf("faded %s", testLogColors.Timestamp)
	}
	// A grid that can't tell colors has everything plain.
	plain := &fakeGrid{rows: rows, wrapped: map[int]bool{}}
	expectList(t, "plain", tintStrings(LogTints(plain, 0, 6, testLogColors), true),
		[]string{"0,0,8," + testLogColors.Timestamp, "0,9,5,#ff0000", "0,20,8,#ff00ff", "1,0,4,#ffff00", "1,5,5,#00ffff"})
	// Rows count from the top of the view.
	expectList(t, "scrolled", tintStrings(LogTints(g, 1, 1, testLogColors), false), []string{"0,5,5"})
}

func TestLogTintsFollowAWrap(t *testing.T) {
	g := &fakeGrid{rows: []string{"abcdefg 20", "26-10-09T1", "0:00:00Z"}, wrapped: map[int]bool{0: true, 1: true}}
	expectList(t, "wrapped", tintStrings(LogTints(g, 0, 6, testLogColors), false), []string{"0,8,2", "1,0,10", "2,0,8"})
	// A program's color anywhere in the token leaves all of it.
	pg := &plainGrid{*g, map[int][]bool{2: {false, true}}}
	if got := LogTints(pg, 0, 6, testLogColors); len(got) != 0 {
		t.Errorf("colored token tinted: %v", got)
	}
}

func TestLogTokenAtACell(t *testing.T) {
	g := &fakeGrid{rows: []string{"x pod/web-1 y"}, wrapped: map[int]bool{}}
	if tok, ok := LogTokenAt(g, 0, 4); !ok || tok.Kind != LogKindK8s || tok.Name != "web-1" {
		t.Errorf("pod: %+v %v", tok, ok)
	}
	if _, ok := LogTokenAt(g, 0, 0); ok {
		t.Error("token at x")
	}
	w := &fakeGrid{rows: []string{"日本 ERROR"}, wrapped: map[int]bool{}}
	if tok, ok := LogTokenAt(w, 0, 5); !ok || tok.Text != "ERROR" {
		t.Errorf("after wide characters: %+v %v", tok, ok)
	}
}

func TestLogColorsFromTheTheme(t *testing.T) {
	c := NewLogColors([]string{"#000000", "#ff5555", "#50fa7b", "#f1fa8c", "#bd93f9", "#ff79c6", "#8be9fd", "#ffffff"}, "#ffffff", "#000000")
	if c.Error != "#ff5555" || c.Warn != "#f1fa8c" {
		t.Errorf("%+v", c)
	}
	if !regexp.MustCompile(`^#[0-9a-f]{6}$`).MatchString(c.Timestamp) {
		t.Errorf("timestamp %q", c.Timestamp)
	}
}

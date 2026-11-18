package runnerd

import (
	"maps"
	"testing"
)

func TestParsePanes(t *testing.T) {
	// A running pane's exit status is empty, also on the last line.
	alive, exit := parsePanes("other 0 \nwtf-r1 1 3\nwtf-r2 0 \n")
	if !maps.Equal(alive, map[string]bool{"r1": false, "r2": true}) || !maps.Equal(exit, map[string]string{"r1": "exited 3"}) {
		t.Fatalf("alive %v exit %v", alive, exit)
	}
	alive, _ = parsePanes("wtf-r2 0 ")
	if !alive["r2"] {
		t.Fatal("the last running pane isn't alive")
	}
	if alive, _ := parsePanes(""); len(alive) != 0 {
		t.Fatal(alive)
	}
}

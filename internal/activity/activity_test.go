package activity

import (
	"context"
	"testing"
	"time"
)

func TestOpenUntilFirstReport(t *testing.T) {
	g := New()
	if !g.Open() {
		t.Fatal("a new gate should be open")
	}
	g.SetFocused("main", false)
	if g.Open() {
		t.Fatal("closed after the only window lost focus")
	}
}

func TestOpenWhileAnyWindowFocused(t *testing.T) {
	g := New()
	g.SetFocused("main", true)
	g.SetFocused("w2", true)
	g.SetFocused("main", false)
	if !g.Open() {
		t.Fatal("w2 still has focus")
	}
	g.Forget("w2")
	if g.Open() {
		t.Fatal("no window has focus")
	}
}

func TestWaitWakesWhenAWindowComesToFront(t *testing.T) {
	g := New()
	g.SetFocused("main", false)
	done := make(chan bool)
	go func() { done <- g.Wait(context.Background()) }()
	select {
	case <-done:
		t.Fatal("Wait returned while in the background")
	case <-time.After(20 * time.Millisecond):
	}
	g.SetFocused("main", true)
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("Wait = false, want true")
		}
	case <-time.After(time.Second):
		t.Fatal("Wait did not wake")
	}
}

func TestWaitEndsWithContext(t *testing.T) {
	g := New()
	g.SetFocused("main", false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if g.Wait(ctx) {
		t.Fatal("Wait = true after cancel")
	}
	var nilGate *Gate
	if !nilGate.Wait(context.Background()) {
		t.Fatal("a nil gate should never pause")
	}
}

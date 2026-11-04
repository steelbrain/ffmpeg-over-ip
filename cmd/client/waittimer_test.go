package main

import (
	"testing"
	"time"
)

func TestWaitTimerCountsOnlyWaiting(t *testing.T) {
	var w waitTimer
	now := time.Now()
	if w.waited(now) != 0 {
		t.Fatal("a fresh timer reports waiting")
	}
	w.start()
	if d := w.waited(now.Add(time.Minute)); d < 59*time.Second {
		t.Fatalf("waited %v, want about a minute", d)
	}
	// Busy (handling a message) for ten minutes: none of it counts.
	w.stop()
	if d := w.waited(now.Add(10 * time.Minute)); d != 0 {
		t.Fatalf("busy loop counted as waiting: %v", d)
	}
	// Waiting again starts from zero.
	w.start()
	if d := w.waited(time.Now()); d > time.Second {
		t.Fatalf("waiting restarted at %v, want about 0", d)
	}
}

package mpc

import (
	"testing"
	"time"
)

func TestNextReadyReregisterBackoff(t *testing.T) {
	got := []time.Duration{readyReregisterBackoffMin}
	for i := 0; i < 5; i++ {
		got = append(got, nextReadyReregisterBackoff(got[len(got)-1]))
	}

	want := []time.Duration{
		5 * time.Second,
		10 * time.Second,
		20 * time.Second,
		40 * time.Second,
		60 * time.Second,
		60 * time.Second,
	}
	if len(got) != len(want) {
		t.Fatalf("got %d steps, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("step %d: got %v, want %v", i, got[i], want[i])
		}
	}
}

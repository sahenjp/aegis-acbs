package search

import (
	"testing"
	"time"
)

func TestMeasuredNanosecondsHasPositiveFloor(t *testing.T) {
	for _, duration := range []time.Duration{-time.Nanosecond, 0, time.Nanosecond} {
		if got := measuredNanoseconds(duration); got < 1 {
			t.Fatalf("duration %v produced %d", duration, got)
		}
	}
	if got := measuredNanoseconds(7 * time.Nanosecond); got != 7 {
		t.Fatalf("positive duration changed to %d", got)
	}
}

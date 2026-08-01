package bench

import (
	"testing"
	"time"
)

func TestMeasuredStressNanosecondsHasPositiveFloor(t *testing.T) {
	for _, duration := range []time.Duration{-time.Nanosecond, 0, time.Nanosecond} {
		if got := measuredStressNanoseconds(duration); got < 1 {
			t.Fatalf("duration %v produced %d", duration, got)
		}
	}
	if got := measuredStressNanoseconds(11 * time.Nanosecond); got != 11 {
		t.Fatalf("positive duration changed to %d", got)
	}
}

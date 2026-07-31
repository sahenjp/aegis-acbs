#!/usr/bin/env python3
from __future__ import annotations

from pathlib import Path


def replace_once(text: str, old: str, new: str, label: str) -> str:
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{label}: expected one match, found {count}")
    return text.replace(old, new, 1)


search_path = Path("internal/search/search.go")
search = search_path.read_text()
search = replace_once(
    search,
    "\tr.Stats.DurationNS = time.Since(started).Nanoseconds()\n\treturn r, err\n}\n",
    "\tr.Stats.DurationNS = measuredNanoseconds(time.Since(started))\n\treturn r, err\n}\n\n"
    "func measuredNanoseconds(duration time.Duration) int64 {\n"
    "\tnanoseconds := duration.Nanoseconds()\n"
    "\tif nanoseconds < 1 {\n"
    "\t\treturn 1\n"
    "\t}\n"
    "\treturn nanoseconds\n"
    "}\n",
    "search duration",
)
search_path.write_text(search)

stress_path = Path("internal/bench/stress.go")
stress = stress_path.read_text()
stress = replace_once(
    stress,
    "\twg.Wait()\n\twall := time.Since(started)\n",
    "\twg.Wait()\n\twallNS := measuredStressNanoseconds(time.Since(started))\n",
    "stress wall measurement",
)
stress = replace_once(
    stress,
    "\t\tWallDurationNS: wall.Nanoseconds(), Memory: captureMemorySummary(),\n"
    "\t}\n"
    "\tif wall > 0 {\n"
    "\t\treport.ThroughputQPS = float64(report.Completed) / wall.Seconds()\n"
    "\t}\n",
    "\t\tWallDurationNS: wallNS, Memory: captureMemorySummary(),\n"
    "\t}\n"
    "\treport.ThroughputQPS = float64(report.Completed) * float64(time.Second) / float64(wallNS)\n",
    "stress report wall",
)
stress = replace_once(
    stress,
    "func WriteStressJSON(path string, report StressReport) error {\n",
    "func measuredStressNanoseconds(duration time.Duration) int64 {\n"
    "\tnanoseconds := duration.Nanoseconds()\n"
    "\tif nanoseconds < 1 {\n"
    "\t\treturn 1\n"
    "\t}\n"
    "\treturn nanoseconds\n"
    "}\n\n"
    "func WriteStressJSON(path string, report StressReport) error {\n",
    "stress duration helper",
)
stress_path.write_text(stress)

Path("internal/search/timing_test.go").write_text(
    '''package search

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
'''
)

Path("internal/bench/timing_test.go").write_text(
    '''package bench

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
'''
)

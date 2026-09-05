package maxsearch

import (
	"context"
	"testing"

	"github.com/lasder-ca/aegis-acbs/internal/search"
)

func TestRunBatch(t *testing.T) {
	g := fixtureGraph(t)
	queries := []BatchQuery{{Source: 0, Target: 1}, {Source: 0, Target: 2}}
	report, err := RunBatch(
		context.Background(),
		g,
		queries,
		Config{Mode: ModeEfficient, Verify: true},
		[]Runner{BuiltinRunner{Algorithm: search.Dijkstra}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.Queries != 2 || report.Summary.Reachable != 2 {
		t.Fatalf("summary=%+v", report.Summary)
	}
	if report.Summary.WinnerCounts[search.Dijkstra] != 2 {
		t.Fatalf("winnerCounts=%v", report.Summary.WinnerCounts)
	}
	if len(report.Samples) != 2 {
		t.Fatalf("samples=%d want=2", len(report.Samples))
	}
	if report.Samples[0].Distance != 3 || report.Samples[1].Distance != 7 {
		t.Fatalf("distances=%d,%d want=3,7", report.Samples[0].Distance, report.Samples[1].Distance)
	}
	if report.Summary.P50NS < 1 || report.Summary.P95NS < 1 || report.Summary.MaxNS < 1 {
		t.Fatalf("invalid duration summary: %+v", report.Summary)
	}
}

func TestRunBatchRejectsOutOfRangeQuery(t *testing.T) {
	g := fixtureGraph(t)
	_, err := RunBatch(
		context.Background(),
		g,
		[]BatchQuery{{Source: 0, Target: 99}},
		Config{Mode: ModeEfficient, Verify: true},
		[]Runner{BuiltinRunner{Algorithm: search.Dijkstra}},
	)
	if err == nil {
		t.Fatal("expected out-of-range query error")
	}
}

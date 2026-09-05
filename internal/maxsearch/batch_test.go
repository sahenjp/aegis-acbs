package maxsearch

import (
	"context"
	"errors"
	"testing"

	"github.com/lasder-ca/aegis-acbs/internal/graph"
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
	if report.Summary.Primary != search.Dijkstra || report.Summary.PrimaryWins != 2 || report.Summary.Fallbacks != 0 {
		t.Fatalf("primary/fallback summary=%+v", report.Summary)
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

func TestRunBatchCountsFallbackFromFirstRunner(t *testing.T) {
	g := fixtureGraph(t)
	primary := batchFailRunner{name: search.Algorithm("always-fails")}
	report, err := RunBatch(
		context.Background(),
		g,
		[]BatchQuery{{Source: 0, Target: 1}},
		Config{Mode: ModeEfficient, Verify: true},
		[]Runner{primary, BuiltinRunner{Algorithm: search.Dijkstra}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.Primary != primary.Name() || report.Summary.PrimaryWins != 0 || report.Summary.Fallbacks != 1 {
		t.Fatalf("summary=%+v", report.Summary)
	}
	if report.Summary.WinnerCounts[search.Dijkstra] != 1 {
		t.Fatalf("winnerCounts=%v", report.Summary.WinnerCounts)
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

type batchFailRunner struct{ name search.Algorithm }

func (r batchFailRunner) Name() search.Algorithm { return r.name }
func (r batchFailRunner) Run(context.Context, *graph.Graph, int, int) (search.Result, error) {
	return search.Result{}, errors.New("intentional test failure")
}

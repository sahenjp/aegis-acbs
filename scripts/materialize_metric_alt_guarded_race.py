#!/usr/bin/env python3
from pathlib import Path


def replace_once(text: str, old: str, new: str, label: str) -> str:
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{label}: expected one match, found {count}")
    return text.replace(old, new, 1)


Path("internal/search/metric_alt_race.go").write_text(r'''package search

import (
	"context"

	"github.com/lasder-ca/aegis-acbs/internal/graph"
)

const (
	metricALTRaceLocalRatio      = 0.05
	metricALTRaceRegionalRatio   = 0.25
	metricALTRaceLargeGraphRatio = 0.30
	metricALTRaceLargeGraphEdges = 1_000_000
)

type metricALTRaceResult struct {
	result Result
	err    error
}

// acbsMetricALTGuardedRace is an exact latency portfolio. It avoids goroutine
// overhead on tiny/local queries and on long regional queries where the prior
// race candidate showed tail regressions. For the remaining query region,
// production ACBS and four-active-landmark ACBS run concurrently; the first
// successful exact result cancels the other.
//
// The upper geometry guard is intentionally conservative: regional graphs use
// 0.25 of the graph diameter, while million-edge graphs use 0.30 so large-city
// workloads retain enough race coverage to amortize the portfolio overhead.
// This optimizes latency rather than total CPU work and is therefore exposed as
// an explicit research algorithm, never as the default production path.
func acbsMetricALTGuardedRace(
	ctx context.Context, g *graph.Graph, source, target int,
) (Result, error) {
	if g.EdgeCount < metricALTMinimumEdges {
		return metricALTRaceSingle(ctx, g, source, target, Aegis)
	}

	_, ratio, hasGeography := queryGeometry(g, source, target)
	if hasGeography && ratio < metricALTRaceLocalRatio {
		if g.Metric == graph.MetricTime {
			return metricALTRaceSingle(ctx, g, source, target, BiDijkstra)
		}
		return metricALTRaceSingle(ctx, g, source, target, AegisALTTop4)
	}
	if hasGeography && ratio > metricALTRaceUpperRatio(g) {
		return metricALTRaceSingle(ctx, g, source, target, Aegis)
	}

	if _, ok := metricALTForGraph(g); !ok {
		return Result{}, errMetricALTNotPrepared
	}
	return metricALTRacePair(ctx, g, source, target, Aegis, AegisALTTop4)
}

func metricALTRaceUpperRatio(g *graph.Graph) float64 {
	if g.EdgeCount >= metricALTRaceLargeGraphEdges {
		return metricALTRaceLargeGraphRatio
	}
	return metricALTRaceRegionalRatio
}

func metricALTRaceSingle(
	ctx context.Context, g *graph.Graph, source, target int, algorithm Algorithm,
) (Result, error) {
	result, err := Run(ctx, g, source, target, algorithm)
	if err != nil {
		return result, err
	}
	result.Stats.Selected = algorithm
	result.Stats.Algorithm = AegisALTGuardedRace
	return result, nil
}

func metricALTRacePair(
	ctx context.Context, g *graph.Graph, source, target int,
	firstAlgorithm, secondAlgorithm Algorithm,
) (Result, error) {
	runContext, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan metricALTRaceResult, 2)
	for _, algorithm := range []Algorithm{firstAlgorithm, secondAlgorithm} {
		algorithm := algorithm
		go func() {
			result, err := Run(runContext, g, source, target, algorithm)
			results <- metricALTRaceResult{result: result, err: err}
		}()
	}

	first := <-results
	if first.err == nil {
		cancel()
		winner := first.result.Stats.Algorithm
		first.result.Stats.Selected = winner
		first.result.Stats.Algorithm = AegisALTGuardedRace
		return first.result, nil
	}
	second := <-results
	if second.err != nil {
		return Result{}, first.err
	}
	winner := second.result.Stats.Algorithm
	second.result.Stats.Selected = winner
	second.result.Stats.Algorithm = AegisALTGuardedRace
	return second.result, nil
}
''')

path = Path("internal/search/search.go")
text = path.read_text()
text = replace_once(
    text,
    '\tAegisALT          Algorithm = "aegis-alt"\n',
    '\tAegisALT              Algorithm = "aegis-alt"\n'
    '\tAegisALTGuardedRace   Algorithm = "aegis-alt-guarded-race"\n',
    "algorithm constant",
)
text = replace_once(
    text,
    '''\tcase AegisALT:
\t\tr, err = acbsMetricALT(ctx, g, source, target)
''',
    '''\tcase AegisALT:
\t\tr, err = acbsMetricALT(ctx, g, source, target)
\tcase AegisALTGuardedRace:
\t\tr, err = acbsMetricALTGuardedRace(ctx, g, source, target)
''',
    "algorithm dispatch",
)
path.write_text(text)

Path("internal/search/metric_alt_race_test.go").write_text(r'''package search

import (
	"context"
	"testing"

	"github.com/lasder-ca/aegis-acbs/internal/graph"
)

func TestMetricALTGuardedRaceRemainsExact(t *testing.T) {
	g := gridGraph(t, 48, 48, true)
	g.EdgeCount = metricALTMinimumEdges
	g.Metric = graph.MetricTime
	g.DiameterMeters = 100_000
	if _, err := PrepareMetricALT(g, 8); err != nil {
		t.Fatal(err)
	}
	defer ReleaseMetricALT(g)

	for source := 0; source < len(g.Nodes); source += 293 {
		for target := 31; target < len(g.Nodes); target += 307 {
			want, err := Run(context.Background(), g, source, target, Dijkstra)
			if err != nil {
				t.Fatal(err)
			}
			got, err := Run(context.Background(), g, source, target, AegisALTGuardedRace)
			if err != nil {
				t.Fatal(err)
			}
			if got.Stats.Distance != want.Stats.Distance || got.Stats.OptimalityGap != 0 {
				t.Fatalf("%d -> %d: got=%+v want=%+v", source, target, got.Stats, want.Stats)
			}
			if got.Stats.Algorithm != AegisALTGuardedRace {
				t.Fatalf("unexpected algorithm: %+v", got.Stats)
			}
			if got.Stats.Selected != Aegis &&
				got.Stats.Selected != AegisALTTop4 &&
				got.Stats.Selected != BiDijkstra {
				t.Fatalf("unexpected winner: %+v", got.Stats)
			}
		}
	}
}

func TestMetricALTGuardedRaceTinyGraphUsesProduction(t *testing.T) {
	g := gridGraph(t, 12, 12, true)
	result, err := Run(context.Background(), g, 0, len(g.Nodes)-1, AegisALTGuardedRace)
	if err != nil {
		t.Fatal(err)
	}
	if result.Stats.Selected != Aegis {
		t.Fatalf("selected = %s, want %s", result.Stats.Selected, Aegis)
	}
}

func TestMetricALTRaceUpperRatio(t *testing.T) {
	g := &graph.Graph{EdgeCount: metricALTRaceLargeGraphEdges - 1}
	if got := metricALTRaceUpperRatio(g); got != metricALTRaceRegionalRatio {
		t.Fatalf("regional upper ratio = %v, want %v", got, metricALTRaceRegionalRatio)
	}
	g.EdgeCount = metricALTRaceLargeGraphEdges
	if got := metricALTRaceUpperRatio(g); got != metricALTRaceLargeGraphRatio {
		t.Fatalf("large-graph upper ratio = %v, want %v", got, metricALTRaceLargeGraphRatio)
	}
}
''')

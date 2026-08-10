#!/usr/bin/env python3
from pathlib import Path
import runpy


# Materialize the node-major metric ALT variants, including top1, first.
runpy.run_path("scripts/materialize_metric_alt_top1.py", run_name="__main__")


def replace_once(text: str, old: str, new: str, label: str) -> str:
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{label}: expected one match, found {count}")
    return text.replace(old, new, 1)


Path("internal/search/metric_alt_top1_guard.go").write_text(r'''package search

import (
    "context"
    "errors"

    "github.com/lasder-ca/aegis-acbs/internal/graph"
)

const (
    metricALTTop1GuardSmallEdges  = 100_000
    metricALTTop1GuardMediumEdges = 400_000
    metricALTTop1GuardLargeEdges  = 1_000_000

    metricALTTop1GuardSmallRatio  = 0.25
    metricALTTop1GuardMediumRatio = 0.15
    metricALTTop1GuardLargeRatio  = 0.50
    metricALTTop1GuardMetroRatio  = 0.25
)

// acbsMetricALTTop1GeometryGuard is an exact portfolio selected before search.
// It uses the one-active-landmark candidate only in geometry ranges where the
// paired top1 screening artifact showed a positive runtime margin without a
// p95/p99 regression; otherwise it falls back to production Aegis. No timed
// query runs more than one search algorithm.
func acbsMetricALTTop1GeometryGuard(
    ctx context.Context, g *graph.Graph, source, target int,
) (Result, error) {
    if g.EdgeCount < metricALTMinimumEdges {
        return metricALTTop1GuardSingle(ctx, g, source, target, Aegis)
    }

    _, ratio, hasGeography := queryGeometry(g, source, target)
    if !hasGeography || ratio > metricALTTop1GuardRatio(g) {
        return metricALTTop1GuardSingle(ctx, g, source, target, Aegis)
    }
    if _, ok := metricALTForGraph(g); !ok {
        return Result{}, errors.New("aegis-alt-top1 geometry guard requires PrepareMetricALT")
    }
    return metricALTTop1GuardSingle(ctx, g, source, target, AegisALTTop1)
}

func metricALTTop1GuardRatio(g *graph.Graph) float64 {
    switch {
    case g.EdgeCount < metricALTTop1GuardSmallEdges:
        return metricALTTop1GuardSmallRatio
    case g.EdgeCount < metricALTTop1GuardMediumEdges:
        return metricALTTop1GuardMediumRatio
    case g.EdgeCount < metricALTTop1GuardLargeEdges:
        return metricALTTop1GuardLargeRatio
    default:
        return metricALTTop1GuardMetroRatio
    }
}

func metricALTTop1GuardSingle(
    ctx context.Context, g *graph.Graph, source, target int, algorithm Algorithm,
) (Result, error) {
    result, err := Run(ctx, g, source, target, algorithm)
    if err != nil {
        return result, err
    }
    result.Stats.Selected = algorithm
    result.Stats.Algorithm = AegisALTTop1GeometryGuard
    return result, nil
}
''')

search_path = Path("internal/search/search.go")
search = search_path.read_text()
search = replace_once(
    search,
    '\tAegisALTTop1      Algorithm = "aegis-alt-top1"\n',
    '\tAegisALTTop1               Algorithm = "aegis-alt-top1"\n'
    '\tAegisALTTop1GeometryGuard  Algorithm = "aegis-alt-top1-geometry-guard"\n',
    "geometry guard algorithm constant",
)
search = replace_once(
    search,
    '''\tcase AegisALTTop1:
\t\tr, err = acbsMetricALTTop1(ctx, g, source, target)
''',
    '''\tcase AegisALTTop1:
\t\tr, err = acbsMetricALTTop1(ctx, g, source, target)
\tcase AegisALTTop1GeometryGuard:
\t\tr, err = acbsMetricALTTop1GeometryGuard(ctx, g, source, target)
''',
    "geometry guard algorithm dispatch",
)
search_path.write_text(search)

Path("internal/search/metric_alt_top1_guard_test.go").write_text(r'''package search

import (
    "context"
    "testing"

    "github.com/lasder-ca/aegis-acbs/internal/graph"
)

func TestMetricALTTop1GeometryGuardRemainsExact(t *testing.T) {
    g := gridGraph(t, 48, 48, true)
    g.EdgeCount = metricALTMinimumEdges
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
            got, err := Run(context.Background(), g, source, target, AegisALTTop1GeometryGuard)
            if err != nil {
                t.Fatal(err)
            }
            if got.Stats.Distance != want.Stats.Distance || got.Stats.OptimalityGap != 0 {
                t.Fatalf("%d -> %d: got=%+v want=%+v", source, target, got.Stats, want.Stats)
            }
            if got.Stats.Algorithm != AegisALTTop1GeometryGuard {
                t.Fatalf("unexpected algorithm: %+v", got.Stats)
            }
            if got.Stats.Selected != Aegis && got.Stats.Selected != AegisALTTop1 {
                t.Fatalf("unexpected selected algorithm: %+v", got.Stats)
            }
        }
    }
}

func TestMetricALTTop1GuardRatios(t *testing.T) {
    cases := []struct {
        edges int
        want  float64
    }{
        {metricALTTop1GuardSmallEdges - 1, metricALTTop1GuardSmallRatio},
        {metricALTTop1GuardSmallEdges, metricALTTop1GuardMediumRatio},
        {metricALTTop1GuardMediumEdges, metricALTTop1GuardLargeRatio},
        {metricALTTop1GuardLargeEdges, metricALTTop1GuardMetroRatio},
    }
    for _, tc := range cases {
        g := &graph.Graph{EdgeCount: tc.edges}
        if got := metricALTTop1GuardRatio(g); got != tc.want {
            t.Fatalf("edges=%d ratio=%v want=%v", tc.edges, got, tc.want)
        }
    }
}
''')

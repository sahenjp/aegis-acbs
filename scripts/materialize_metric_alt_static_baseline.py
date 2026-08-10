#!/usr/bin/env python3
from pathlib import Path


def replace_once(text: str, old: str, new: str, label: str) -> str:
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{label}: expected one match, found {count}")
    return text.replace(old, new, 1)


path = Path("internal/search/acbs.go")
text = path.read_text()
anchor = '''func acbsMetricALTTop2(ctx context.Context, g *graph.Graph, source, target int) (Result, error) {
\treturn acbsMetricALTWithLimit(ctx, g, source, target, AegisALTTop2, 2)
}

'''
addition = anchor + '''func acbsMetricALTStatic(ctx context.Context, g *graph.Graph, source, target int) (Result, error) {
\tif g.EdgeCount < metricALTMinimumEdges {
\t\treturn acbsWithOptions(ctx, g, source, target, acbsOptions{
\t\t\talgorithm: AegisALTStatic, adaptive: false, pruning: false,
\t\t})
\t}
\tif _, ok := metricALTForGraph(g); !ok {
\t\treturn Result{}, errors.New("aegis-alt requires PrepareMetricALT for this graph")
\t}
\treturn acbsWithOptions(ctx, g, source, target, acbsOptions{
\t\talgorithm: AegisALTStatic, adaptive: false, pruning: false,
\t\tmetricALT: true,
\t})
}

'''
text = replace_once(text, anchor, addition, "static entry point")
path.write_text(text)

path = Path("internal/search/search.go")
text = path.read_text()
text = replace_once(
    text,
    '\tAegisALTTop2      Algorithm = "aegis-alt-top2"\n',
    '\tAegisALTTop2      Algorithm = "aegis-alt-top2"\n'
    '\tAegisALTStatic    Algorithm = "aegis-alt-static"\n',
    "constant",
)
text = replace_once(
    text,
    '''\tcase AegisALTTop2:
\t\tr, err = acbsMetricALTTop2(ctx, g, source, target)
''',
    '''\tcase AegisALTTop2:
\t\tr, err = acbsMetricALTTop2(ctx, g, source, target)
\tcase AegisALTStatic:
\t\tr, err = acbsMetricALTStatic(ctx, g, source, target)
''',
    "dispatch",
)
path.write_text(text)

Path("internal/search/metric_alt_static_test.go").write_text(r'''package search

import (
	"context"
	"testing"
)

func TestMetricALTStaticSchedulerRemainsExact(t *testing.T) {
	g := gridGraph(t, 36, 36, true)
	g.EdgeCount = metricALTMinimumEdges
	if _, err := PrepareMetricALT(g, 8); err != nil {
		t.Fatal(err)
	}
	defer ReleaseMetricALT(g)

	for source := 0; source < len(g.Nodes); source += 211 {
		for target := 19; target < len(g.Nodes); target += 229 {
			want, err := Run(context.Background(), g, source, target, Dijkstra)
			if err != nil {
				t.Fatal(err)
			}
			got, err := Run(context.Background(), g, source, target, AegisALTStatic)
			if err != nil {
				t.Fatal(err)
			}
			if got.Stats.Distance != want.Stats.Distance || got.Stats.OptimalityGap != 0 {
				t.Fatalf("%d -> %d: got=%+v want=%+v", source, target, got.Stats, want.Stats)
			}
			if got.Stats.SchedulerVersion != "lower-key-static-v2" {
				t.Fatalf("unexpected scheduler: %+v", got.Stats)
			}
		}
	}
}
''')

#!/usr/bin/env python3
from pathlib import Path


def replace_once(text: str, old: str, new: str, label: str) -> str:
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{label}: expected one match, found {count}")
    return text.replace(old, new, 1)


path = Path("internal/search/acbs.go")
text = path.read_text()
anchor = '''func acbsMetricALTGate125(ctx context.Context, g *graph.Graph, source, target int) (Result, error) {
\treturn acbsMetricALTGate(ctx, g, source, target, AegisALTGate125, 5, 4)
}

'''
addition = anchor + '''func acbsMetricALTCombinedGate75(ctx context.Context, g *graph.Graph, source, target int) (Result, error) {
\treturn acbsMetricALTCombinedGate(ctx, g, source, target, AegisALTCombinedGate75, 3, 4)
}

func acbsMetricALTCombinedGate100(ctx context.Context, g *graph.Graph, source, target int) (Result, error) {
\treturn acbsMetricALTCombinedGate(ctx, g, source, target, AegisALTCombinedGate100, 1, 1)
}

func acbsMetricALTCombinedGate125(ctx context.Context, g *graph.Graph, source, target int) (Result, error) {
\treturn acbsMetricALTCombinedGate(ctx, g, source, target, AegisALTCombinedGate125, 5, 4)
}

'''
text = replace_once(text, anchor, addition, "combined gate entry points")

helper_anchor = '''func acbsMetricALTGate(
'''
helper = '''func acbsMetricALTCombinedGate(
\tctx context.Context, g *graph.Graph, source, target int,
\talgorithm Algorithm, numerator, denominator uint64,
) (Result, error) {
\tif g.EdgeCount < metricALTMinimumEdges {
\t\tresult, err := acbsWithOptions(ctx, g, source, target, acbsOptions{
\t\t\talgorithm: algorithm, adaptive: true, pruning: false,
\t\t})
\t\tresult.Stats.Selected = Aegis
\t\treturn result, err
\t}
\tindex, ok := metricALTForGraph(g)
\tif !ok {
\t\treturn Result{}, errors.New("aegis-alt requires PrepareMetricALT for this graph")
\t}
\tquery := index.query(source, target, 2)
\tlandmark, _ := query.bounds(source)
\tchord := heuristic(g, source, target, true)

\toptions := acbsOptions{
\t\talgorithm: algorithm, adaptive: true, pruning: false,
\t}
\tselected := Aegis
\tif ratioAtLeast(landmark, chord, numerator, denominator) {
\t\toptions.metricALT = true
\t\toptions.metricALTLimit = 2
\t\tselected = AegisALTTop2
\t}
\tresult, err := acbsWithOptions(ctx, g, source, target, options)
\tresult.Stats.Selected = selected
\treturn result, err
}

''' + helper_anchor
text = replace_once(text, helper_anchor, helper, "combined gate helper")
path.write_text(text)

path = Path("internal/search/search.go")
text = path.read_text()
text = replace_once(
    text,
    '\tAegisALTGate125   Algorithm = "aegis-alt-gate125"\n',
    '\tAegisALTGate125          Algorithm = "aegis-alt-gate125"\n'
    '\tAegisALTCombinedGate75  Algorithm = "aegis-alt-combined-gate75"\n'
    '\tAegisALTCombinedGate100 Algorithm = "aegis-alt-combined-gate100"\n'
    '\tAegisALTCombinedGate125 Algorithm = "aegis-alt-combined-gate125"\n',
    "constants",
)
text = replace_once(
    text,
    '''\tcase AegisALTGate125:
\t\tr, err = acbsMetricALTGate125(ctx, g, source, target)
''',
    '''\tcase AegisALTGate125:
\t\tr, err = acbsMetricALTGate125(ctx, g, source, target)
\tcase AegisALTCombinedGate75:
\t\tr, err = acbsMetricALTCombinedGate75(ctx, g, source, target)
\tcase AegisALTCombinedGate100:
\t\tr, err = acbsMetricALTCombinedGate100(ctx, g, source, target)
\tcase AegisALTCombinedGate125:
\t\tr, err = acbsMetricALTCombinedGate125(ctx, g, source, target)
''',
    "dispatch",
)
path.write_text(text)

Path("internal/search/metric_alt_combined_gate_test.go").write_text(r'''package search

import (
	"context"
	"testing"
)

func TestMetricALTCombinedGatesRemainExact(t *testing.T) {
	g := gridGraph(t, 40, 40, true)
	g.EdgeCount = metricALTMinimumEdges
	if _, err := PrepareMetricALT(g, 8); err != nil {
		t.Fatal(err)
	}
	defer ReleaseMetricALT(g)

	algorithms := []Algorithm{
		AegisALTCombinedGate75,
		AegisALTCombinedGate100,
		AegisALTCombinedGate125,
	}
	for source := 0; source < len(g.Nodes); source += 239 {
		for target := 23; target < len(g.Nodes); target += 263 {
			want, err := Run(context.Background(), g, source, target, Dijkstra)
			if err != nil {
				t.Fatal(err)
			}
			for _, algorithm := range algorithms {
				got, err := Run(context.Background(), g, source, target, algorithm)
				if err != nil {
					t.Fatal(err)
				}
				if got.Stats.Distance != want.Stats.Distance || got.Stats.OptimalityGap != 0 {
					t.Fatalf("%s %d -> %d: got=%+v want=%+v", algorithm, source, target, got.Stats, want.Stats)
				}
				if got.Stats.Selected != Aegis && got.Stats.Selected != AegisALTTop2 {
					t.Fatalf("unexpected selection: %+v", got.Stats)
				}
			}
		}
	}
}
''')

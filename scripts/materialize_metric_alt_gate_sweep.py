#!/usr/bin/env python3
from pathlib import Path


def replace_once(text: str, old: str, new: str, label: str) -> str:
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{label}: expected one match, found {count}")
    return text.replace(old, new, 1)


path = Path("internal/search/acbs.go")
text = path.read_text()
anchor = '''func acbsPureMetricALTTop2(ctx context.Context, g *graph.Graph, source, target int) (Result, error) {
\treturn acbsPureMetricALTWithLimit(ctx, g, source, target, AegisALTPureTop2, 2)
}

'''
addition = anchor + '''func acbsMetricALTGate50(ctx context.Context, g *graph.Graph, source, target int) (Result, error) {
\treturn acbsMetricALTGate(ctx, g, source, target, AegisALTGate50, 1, 2)
}

func acbsMetricALTGate75(ctx context.Context, g *graph.Graph, source, target int) (Result, error) {
\treturn acbsMetricALTGate(ctx, g, source, target, AegisALTGate75, 3, 4)
}

func acbsMetricALTGate100(ctx context.Context, g *graph.Graph, source, target int) (Result, error) {
\treturn acbsMetricALTGate(ctx, g, source, target, AegisALTGate100, 1, 1)
}

func acbsMetricALTGate125(ctx context.Context, g *graph.Graph, source, target int) (Result, error) {
\treturn acbsMetricALTGate(ctx, g, source, target, AegisALTGate125, 5, 4)
}

'''
text = replace_once(text, anchor, addition, "gate entry points")

helper_anchor = '''func acbsPureMetricALTWithLimit(
'''
helper = '''func acbsMetricALTGate(
\tctx context.Context, g *graph.Graph, source, target int,
\talgorithm Algorithm, numerator, denominator uint64,
) (Result, error) {
\tif g.EdgeCount < metricALTMinimumEdges {
\t\treturn acbsWithOptions(ctx, g, source, target, acbsOptions{
\t\t\talgorithm: algorithm, adaptive: true, pruning: false,
\t\t})
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
\t\toptions.metricALTOnly = true
\t\tselected = AegisALTPureTop2
\t}
\tresult, err := acbsWithOptions(ctx, g, source, target, options)
\tresult.Stats.Selected = selected
\treturn result, err
}

''' + helper_anchor
text = replace_once(text, helper_anchor, helper, "gate helper")
path.write_text(text)

path = Path("internal/search/metric_alt.go")
text = path.read_text()
insert_anchor = '''func absDiffUint64(a, b uint64) uint64 {
'''
insert = '''func ratioAtLeast(value, base, numerator, denominator uint64) bool {
\tif denominator == 0 {
\t\treturn false
\t}
\tif base == 0 {
\t\treturn value > 0
\t}
\tvalueHigh, valueLow := bits.Mul64(value, denominator)
\tbaseHigh, baseLow := bits.Mul64(base, numerator)
\treturn valueHigh > baseHigh || (valueHigh == baseHigh && valueLow >= baseLow)
}

''' + insert_anchor
text = replace_once(text, insert_anchor, insert, "ratio helper")
path.write_text(text)

path = Path("internal/search/search.go")
text = path.read_text()
text = replace_once(
    text,
    '\tAegisALTPureTop2  Algorithm = "aegis-alt-pure-top2"\n',
    '\tAegisALTPureTop2  Algorithm = "aegis-alt-pure-top2"\n'
    '\tAegisALTGate50    Algorithm = "aegis-alt-gate50"\n'
    '\tAegisALTGate75    Algorithm = "aegis-alt-gate75"\n'
    '\tAegisALTGate100   Algorithm = "aegis-alt-gate100"\n'
    '\tAegisALTGate125   Algorithm = "aegis-alt-gate125"\n',
    "constants",
)
text = replace_once(
    text,
    '''\tcase AegisALTPureTop2:
\t\tr, err = acbsPureMetricALTTop2(ctx, g, source, target)
''',
    '''\tcase AegisALTPureTop2:
\t\tr, err = acbsPureMetricALTTop2(ctx, g, source, target)
\tcase AegisALTGate50:
\t\tr, err = acbsMetricALTGate50(ctx, g, source, target)
\tcase AegisALTGate75:
\t\tr, err = acbsMetricALTGate75(ctx, g, source, target)
\tcase AegisALTGate100:
\t\tr, err = acbsMetricALTGate100(ctx, g, source, target)
\tcase AegisALTGate125:
\t\tr, err = acbsMetricALTGate125(ctx, g, source, target)
''',
    "dispatch",
)
path.write_text(text)

Path("internal/search/metric_alt_gate_test.go").write_text(r'''package search

import (
	"context"
	"math"
	"testing"
)

func TestRatioAtLeastUsesFullWidthProducts(t *testing.T) {
	if !ratioAtLeast(math.MaxUint64, math.MaxUint64, 1, 1) {
		t.Fatal("equal maximum values should satisfy ratio")
	}
	if ratioAtLeast(math.MaxUint64-1, math.MaxUint64, 1, 1) {
		t.Fatal("smaller value should not satisfy ratio")
	}
	if !ratioAtLeast(3, 4, 3, 4) {
		t.Fatal("3/4 threshold should be inclusive")
	}
	if ratioAtLeast(2, 4, 3, 4) {
		t.Fatal("1/2 should not satisfy 3/4")
	}
}

func TestMetricALTGateRemainsExact(t *testing.T) {
	g := gridGraph(t, 42, 42, true)
	g.EdgeCount = metricALTMinimumEdges
	if _, err := PrepareMetricALT(g, 8); err != nil {
		t.Fatal(err)
	}
	defer ReleaseMetricALT(g)

	algorithms := []Algorithm{
		AegisALTGate50, AegisALTGate75, AegisALTGate100, AegisALTGate125,
	}
	for source := 0; source < len(g.Nodes); source += 251 {
		for target := 13; target < len(g.Nodes); target += 277 {
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
				if got.Stats.Selected != Aegis && got.Stats.Selected != AegisALTPureTop2 {
					t.Fatalf("unexpected selection: %+v", got.Stats)
				}
			}
		}
	}
}
''')

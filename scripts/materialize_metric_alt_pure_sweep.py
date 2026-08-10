#!/usr/bin/env python3
from pathlib import Path


def replace_once(text: str, old: str, new: str, label: str) -> str:
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{label}: expected one match, found {count}")
    return text.replace(old, new, 1)


path = Path("internal/search/acbs.go")
text = path.read_text()
text = replace_once(
    text,
    '\tacbsMetricALTPotentialModel = "balanced-metric-alt-v2"\n',
    '\tacbsMetricALTPotentialModel     = "balanced-metric-alt-v2"\n'
    '\tacbsPureMetricALTPotentialModel = "balanced-pure-metric-alt-v1"\n',
    "model constant",
)
text = replace_once(
    text,
    '\tmetricALT      bool\n\tmetricALTLimit int\n\tguardMode      acbsGuardMode\n',
    '\tmetricALT      bool\n\tmetricALTLimit int\n\tmetricALTOnly  bool\n\tguardMode      acbsGuardMode\n',
    "options",
)
entry_anchor = '''func acbsMetricALTTop2(ctx context.Context, g *graph.Graph, source, target int) (Result, error) {
\treturn acbsMetricALTWithLimit(ctx, g, source, target, AegisALTTop2, 2)
}

'''
entry_add = entry_anchor + '''func acbsPureMetricALT(ctx context.Context, g *graph.Graph, source, target int) (Result, error) {
\treturn acbsPureMetricALTWithLimit(ctx, g, source, target, AegisALTPure, 0)
}

func acbsPureMetricALTTop2(ctx context.Context, g *graph.Graph, source, target int) (Result, error) {
\treturn acbsPureMetricALTWithLimit(ctx, g, source, target, AegisALTPureTop2, 2)
}

'''
text = replace_once(text, entry_anchor, entry_add, "pure entry points")
helper_anchor = '''func acbsLateGuard(ctx context.Context, g *graph.Graph, source, target int) (Result, error) {
'''
helper = '''func acbsPureMetricALTWithLimit(
\tctx context.Context, g *graph.Graph, source, target int, algorithm Algorithm, limit int,
) (Result, error) {
\tif g.EdgeCount < metricALTMinimumEdges {
\t\treturn acbsWithOptions(ctx, g, source, target, acbsOptions{
\t\t\talgorithm: algorithm, adaptive: true, pruning: false,
\t\t})
\t}
\tif _, ok := metricALTForGraph(g); !ok {
\t\treturn Result{}, errors.New("aegis-alt requires PrepareMetricALT for this graph")
\t}
\treturn acbsWithOptions(ctx, g, source, target, acbsOptions{
\t\talgorithm: algorithm, adaptive: true, pruning: false,
\t\tmetricALT: true, metricALTLimit: limit, metricALTOnly: true,
\t})
}

''' + helper_anchor
text = replace_once(text, helper_anchor, helper, "pure helper")
text = replace_once(
    text,
    '''\tif opts.projection {
\t\tmodelName = acbsProjectionModel
\t} else if opts.metricALT {
\t\tmodelName = acbsMetricALTPotentialModel
\t}
''',
    '''\tif opts.projection {
\t\tmodelName = acbsProjectionModel
\t} else if opts.metricALTOnly {
\t\tmodelName = acbsPureMetricALTPotentialModel
\t} else if opts.metricALT {
\t\tmodelName = acbsMetricALTPotentialModel
\t}
''',
    "model selection",
)
text = replace_once(
    text,
    '\t\tpotential = newACBSMetricALTPotential(g, source, target, index, opts.metricALTLimit)\n',
    '\t\tpotential = newACBSMetricALTPotential(g, source, target, index, opts.metricALTLimit, opts.metricALTOnly)\n',
    "constructor call",
)
text = replace_once(
    text,
    '\tmetricALT                 metricALTQuery\n\tenabled                   bool\n',
    '\tmetricALT                 metricALTQuery\n\tmetricALTOnly             bool\n\tenabled                   bool\n',
    "potential field",
)
old_ctor = '''func newACBSMetricALTPotential(
\tg *graph.Graph, source, target int, index *metricALTIndex, limit int,
) acbsPotential {
\tp := newACBSPotential(g, source, target, false)
\tp.metricALT = index.query(source, target, limit)
\tp.enabled = true
\treturn p
}
'''
new_ctor = '''func newACBSMetricALTPotential(
\tg *graph.Graph, source, target int, index *metricALTIndex, limit int, metricOnly bool,
) acbsPotential {
\tp := newACBSPotential(g, source, target, false)
\tp.metricALT = index.query(source, target, limit)
\tp.metricALTOnly = metricOnly
\tp.enabled = true
\treturn p
}
'''
text = replace_once(text, old_ctor, new_ctor, "constructor")
text = replace_once(
    text,
    '''\tif !p.enabled {
\t\treturn 0
\t}
\tx, y, z := g.UnitVector(v)
''',
    '''\tif !p.enabled {
\t\treturn 0
\t}
\tif p.metricALTOnly {
\t\tforward, backward := p.metricALT.bounds(v)
\t\treturn signedDifference(forward, backward)
\t}
\tx, y, z := g.UnitVector(v)
''',
    "pure phi",
)
text = replace_once(
    text,
    '''\tif !p.enabled {
\t\treturn 0, 0
\t}
\tx, y, z := g.UnitVector(v)
''',
    '''\tif !p.enabled {
\t\treturn 0, 0
\t}
\tif p.metricALTOnly {
\t\treturn p.metricALT.bounds(v)
\t}
\tx, y, z := g.UnitVector(v)
''',
    "pure bounds",
)
path.write_text(text)

path = Path("internal/search/search.go")
text = path.read_text()
text = replace_once(
    text,
    '\tAegisALTTop2      Algorithm = "aegis-alt-top2"\n',
    '\tAegisALTTop2      Algorithm = "aegis-alt-top2"\n'
    '\tAegisALTPure      Algorithm = "aegis-alt-pure"\n'
    '\tAegisALTPureTop2  Algorithm = "aegis-alt-pure-top2"\n',
    "constants",
)
text = replace_once(
    text,
    '''\tcase AegisALTTop2:
\t\tr, err = acbsMetricALTTop2(ctx, g, source, target)
''',
    '''\tcase AegisALTTop2:
\t\tr, err = acbsMetricALTTop2(ctx, g, source, target)
\tcase AegisALTPure:
\t\tr, err = acbsPureMetricALT(ctx, g, source, target)
\tcase AegisALTPureTop2:
\t\tr, err = acbsPureMetricALTTop2(ctx, g, source, target)
''',
    "dispatch",
)
path.write_text(text)

Path("internal/search/metric_alt_pure_test.go").write_text(r'''package search

import (
	"context"
	"testing"
)

func TestPureMetricALTIsExactAndFeasible(t *testing.T) {
	g := gridGraph(t, 36, 36, true)
	g.EdgeCount = metricALTMinimumEdges
	if _, err := PrepareMetricALT(g, 8); err != nil {
		t.Fatal(err)
	}
	defer ReleaseMetricALT(g)
	index, ok := metricALTForGraph(g)
	if !ok {
		t.Fatal("missing metric ALT index")
	}

	for source := 0; source < len(g.Nodes); source += 197 {
		for target := 11; target < len(g.Nodes); target += 223 {
			potential := newACBSMetricALTPotential(g, source, target, index, 2, true)
			for from := range g.Nodes {
				phiFrom := potential.phi(g, from)
				for _, edge := range g.OutEdges(from) {
					phiTo := potential.phi(g, edge.To)
					cost := int64(2 * edge.Cost)
					if cost+phiTo-phiFrom < 0 || cost+phiFrom-phiTo < 0 {
						t.Fatalf("infeasible pure potential on %d -> %d", from, edge.To)
					}
				}
			}

			want, err := Run(context.Background(), g, source, target, Dijkstra)
			if err != nil {
				t.Fatal(err)
			}
			for _, algorithm := range []Algorithm{AegisALTPure, AegisALTPureTop2} {
				got, err := Run(context.Background(), g, source, target, algorithm)
				if err != nil {
					t.Fatal(err)
				}
				if got.Stats.Distance != want.Stats.Distance || got.Stats.OptimalityGap != 0 {
					t.Fatalf("%s %d -> %d: got=%+v want=%+v", algorithm, source, target, got.Stats, want.Stats)
				}
				if got.Stats.PotentialModel != acbsPureMetricALTPotentialModel {
					t.Fatalf("unexpected potential model: %+v", got.Stats)
				}
			}
		}
	}
}
''')

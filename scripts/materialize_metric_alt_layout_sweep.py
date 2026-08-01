#!/usr/bin/env python3
from pathlib import Path


def replace_once(text: str, old: str, new: str, label: str) -> str:
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{label}: expected one match, found {count}")
    return text.replace(old, new, 1)


metric_alt = 'package search\n\nimport (\n\t"errors"\n\t"math/bits"\n\t"sync"\n\t"time"\n\n\t"github.com/lasder-ca/aegis-acbs/internal/graph"\n)\n\nconst (\n\tmetricALTMinimumEdges     = 10_000\n\tmetricALTLargeEdges       = 400_000\n\tmetricALTMaximumLandmarks = 16\n)\n\n// MetricALTPreparation describes graph-wide preprocessing performed outside\n// timed queries. The index is immutable after publication and safe for\n// concurrent searches.\ntype MetricALTPreparation struct {\n\tLandmarks int\n\tDuration  time.Duration\n\tBytes     uint64\n\tReused    bool\n}\n\n// metricALTIndex stores each node\'s landmark vector contiguously. The original\n// landmark-major layout reduced graph work but made every potential evaluation\n// jump between distant arrays. Node-major storage turns the hot query loop into\n// one short sequential scan.\ntype metricALTIndex struct {\n\tlandmarks []int\n\tstride    int\n\tdistances []uint64\n}\n\n// metricALTQuery is a query-local, allocation-free view of an index. It caches\n// source/target landmark distances and may keep only the strongest landmarks\n// for the source-target pair. Any subset remains admissible and 1-Lipschitz.\ntype metricALTQuery struct {\n\tindex       *metricALTIndex\n\tsource      int\n\ttarget      int\n\tactiveMask  uint16\n\tactiveCount uint8\n}\n\nvar (\n\tmetricALTIndexes   sync.Map // map[*graph.Graph]*metricALTIndex\n\tmetricALTPrepareMu sync.Mutex\n)\n\n// RecommendedMetricALTLandmarks selects the measured memory/runtime tradeoff.\n// Tiny graphs retain production ACBS, medium graphs use four landmarks, and\n// larger regional graphs use eight.\nfunc RecommendedMetricALTLandmarks(g *graph.Graph) int {\n\tif g == nil || g.EdgeCount < metricALTMinimumEdges {\n\t\treturn 0\n\t}\n\tif g.EdgeCount < metricALTLargeEdges {\n\t\treturn 4\n\t}\n\treturn 8\n}\n\n// PrepareMetricALT builds a metric landmark index over the undirected\n// relaxation of g. Every directed edge is inserted into the relaxation with\n// its original cost; therefore relaxation distance is a lower bound on every\n// directed path and remains 1-Lipschitz on every directed edge.\nfunc PrepareMetricALT(g *graph.Graph, count int) (MetricALTPreparation, error) {\n\tif g == nil || len(g.Nodes) == 0 {\n\t\treturn MetricALTPreparation{}, errors.New("metric ALT requires a non-empty graph")\n\t}\n\tif count < 0 {\n\t\treturn MetricALTPreparation{}, errors.New("metric ALT landmark count cannot be negative")\n\t}\n\tif count == 0 {\n\t\tReleaseMetricALT(g)\n\t\treturn MetricALTPreparation{}, nil\n\t}\n\tif count > metricALTMaximumLandmarks {\n\t\tcount = metricALTMaximumLandmarks\n\t}\n\tif count > len(g.Nodes) {\n\t\tcount = len(g.Nodes)\n\t}\n\n\tmetricALTPrepareMu.Lock()\n\tdefer metricALTPrepareMu.Unlock()\n\tif existing, ok := metricALTForGraph(g); ok && len(existing.landmarks) >= count {\n\t\treturn MetricALTPreparation{\n\t\t\tLandmarks: len(existing.landmarks),\n\t\t\tBytes:     existing.bytes(),\n\t\t\tReused:    true,\n\t\t}, nil\n\t}\n\n\tstarted := time.Now()\n\tindex := &metricALTIndex{\n\t\tstride:    count,\n\t\tdistances: make([]uint64, len(g.Nodes)*count),\n\t}\n\tselected := make([]bool, len(g.Nodes))\n\tnearest := make([]uint64, len(g.Nodes))\n\tfor i := range nearest {\n\t\tnearest[i] = inf\n\t}\n\n\tlandmark := highestDegreeNode(g, selected)\n\tfor len(index.landmarks) < count && landmark >= 0 {\n\t\tcolumn := len(index.landmarks)\n\t\tselected[landmark] = true\n\t\tdistances := metricALTUndirectedDistances(g, landmark)\n\t\tindex.landmarks = append(index.landmarks, landmark)\n\n\t\tfor v, distance := range distances {\n\t\t\tindex.distances[v*index.stride+column] = distance\n\t\t\tif distance < nearest[v] {\n\t\t\t\tnearest[v] = distance\n\t\t\t}\n\t\t}\n\t\tlandmark = nextMetricALTLandmark(g, selected, nearest)\n\t}\n\tif len(index.landmarks) == 0 {\n\t\treturn MetricALTPreparation{}, errors.New("metric ALT could not select a landmark")\n\t}\n\n\tmetricALTIndexes.Store(g, index)\n\treturn MetricALTPreparation{\n\t\tLandmarks: len(index.landmarks),\n\t\tDuration:  time.Since(started),\n\t\tBytes:     index.bytes(),\n\t}, nil\n}\n\n// ReleaseMetricALT removes the graph-to-index association. Searches that have\n// already captured the immutable index remain valid.\nfunc ReleaseMetricALT(g *graph.Graph) {\n\tif g != nil {\n\t\tmetricALTIndexes.Delete(g)\n\t}\n}\n\nfunc MetricALTPrepared(g *graph.Graph) bool {\n\t_, ok := metricALTForGraph(g)\n\treturn ok\n}\n\nfunc metricALTForGraph(g *graph.Graph) (*metricALTIndex, bool) {\n\tvalue, ok := metricALTIndexes.Load(g)\n\tif !ok {\n\t\treturn nil, false\n\t}\n\treturn value.(*metricALTIndex), true\n}\n\nfunc (index *metricALTIndex) bytes() uint64 {\n\tif index == nil {\n\t\treturn 0\n\t}\n\treturn uint64(len(index.distances)) * 8\n}\n\nfunc (index *metricALTIndex) row(v int) []uint64 {\n\tstart := v * index.stride\n\treturn index.distances[start : start+len(index.landmarks)]\n}\n\nfunc highestDegreeNode(g *graph.Graph, selected []bool) int {\n\tbest := -1\n\tbestDegree := -1\n\tfor v := range g.Nodes {\n\t\tif selected[v] {\n\t\t\tcontinue\n\t\t}\n\t\tdegree := g.OutDegree(v) + g.InDegree(v)\n\t\tif degree > bestDegree {\n\t\t\tbest = v\n\t\t\tbestDegree = degree\n\t\t}\n\t}\n\treturn best\n}\n\nfunc nextMetricALTLandmark(g *graph.Graph, selected []bool, nearest []uint64) int {\n\t// Cover disconnected components first; no finite landmark distance exists\n\t// there yet. Within covered components use deterministic farthest sampling.\n\tdisconnected := -1\n\tdisconnectedDegree := -1\n\tbest := -1\n\tbestDistance := uint64(0)\n\tfor v := range g.Nodes {\n\t\tif selected[v] {\n\t\t\tcontinue\n\t\t}\n\t\tif nearest[v] == inf {\n\t\t\tdegree := g.OutDegree(v) + g.InDegree(v)\n\t\t\tif degree > disconnectedDegree {\n\t\t\t\tdisconnected = v\n\t\t\t\tdisconnectedDegree = degree\n\t\t\t}\n\t\t\tcontinue\n\t\t}\n\t\tif best < 0 || nearest[v] > bestDistance {\n\t\t\tbest = v\n\t\t\tbestDistance = nearest[v]\n\t\t}\n\t}\n\tif disconnected >= 0 {\n\t\treturn disconnected\n\t}\n\treturn best\n}\n\nfunc metricALTUndirectedDistances(g *graph.Graph, source int) []uint64 {\n\tdist := make([]uint64, len(g.Nodes))\n\tfor i := range dist {\n\t\tdist[i] = inf\n\t}\n\tdist[source] = 0\n\tvar queue radixHeap\n\tradixPush(&queue, item{node: source, distance: 0, priority: 0})\n\tfor queue.Len() > 0 {\n\t\tcur := radixPop(&queue)\n\t\tif cur.distance != dist[cur.node] {\n\t\t\tcontinue\n\t\t}\n\t\trelaxMetricALTEdges(&queue, dist, cur, g.OutEdges(cur.node))\n\t\trelaxMetricALTEdges(&queue, dist, cur, g.InEdges(cur.node))\n\t}\n\treturn dist\n}\n\nfunc relaxMetricALTEdges(queue *radixHeap, dist []uint64, cur item, edges []graph.Edge) {\n\tfor _, edge := range edges {\n\t\tif cur.distance > inf-edge.Cost {\n\t\t\tcontinue\n\t\t}\n\t\tnext := cur.distance + edge.Cost\n\t\tif next >= dist[edge.To] {\n\t\t\tcontinue\n\t\t}\n\t\tdist[edge.To] = next\n\t\tradixPush(queue, item{node: edge.To, distance: next, priority: next})\n\t}\n}\n\nfunc (index *metricALTIndex) query(source, target, limit int) metricALTQuery {\n\tcount := len(index.landmarks)\n\tif limit <= 0 || limit > count {\n\t\tlimit = count\n\t}\n\n\ttype rankedLandmark struct {\n\t\tindex uint8\n\t\tscore uint64\n\t}\n\tvar ranked [metricALTMaximumLandmarks]rankedLandmark\n\trankedCount := 0\n\tsourceRow := index.row(source)\n\ttargetRow := index.row(target)\n\tfor i := 0; i < count; i++ {\n\t\tscore := uint64(0)\n\t\tif sourceRow[i] != inf && targetRow[i] != inf {\n\t\t\tscore = absDiffUint64(sourceRow[i], targetRow[i])\n\t\t}\n\t\tposition := rankedCount\n\t\tfor position > 0 {\n\t\t\tprevious := ranked[position-1]\n\t\t\tif previous.score > score || (previous.score == score && previous.index < uint8(i)) {\n\t\t\t\tbreak\n\t\t\t}\n\t\t\tranked[position] = previous\n\t\t\tposition--\n\t\t}\n\t\tranked[position] = rankedLandmark{index: uint8(i), score: score}\n\t\trankedCount++\n\t}\n\n\tquery := metricALTQuery{\n\t\tindex:       index,\n\t\tsource:      source,\n\t\ttarget:      target,\n\t\tactiveCount: uint8(limit),\n\t}\n\tfor i := 0; i < limit; i++ {\n\t\tquery.activeMask |= uint16(1) << ranked[i].index\n\t}\n\treturn query\n}\n\nfunc (query metricALTQuery) bounds(v int) (forward, backward uint64) {\n\trow := query.index.row(v)\n\tsourceRow := query.index.row(query.source)\n\ttargetRow := query.index.row(query.target)\n\tmask := query.activeMask\n\tfor mask != 0 {\n\t\tlandmark := bits.TrailingZeros16(mask)\n\t\tmask &= mask - 1\n\t\tvalue := row[landmark]\n\t\tif value == inf {\n\t\t\tcontinue\n\t\t}\n\t\tif target := targetRow[landmark]; target != inf {\n\t\t\tif bound := absDiffUint64(target, value); bound > forward {\n\t\t\t\tforward = bound\n\t\t\t}\n\t\t}\n\t\tif source := sourceRow[landmark]; source != inf {\n\t\t\tif bound := absDiffUint64(source, value); bound > backward {\n\t\t\t\tbackward = bound\n\t\t\t}\n\t\t}\n\t}\n\treturn forward, backward\n}\n\nfunc absDiffUint64(a, b uint64) uint64 {\n\tif a >= b {\n\t\treturn a - b\n\t}\n\treturn b - a\n}\n\nfunc validateMetricALTFeasibility(g *graph.Graph, source, target, limit int) bool {\n\tindex, ok := metricALTForGraph(g)\n\tif !ok {\n\t\treturn g.EdgeCount < metricALTMinimumEdges\n\t}\n\tp := newACBSMetricALTPotential(g, source, target, index, limit)\n\tfor from := range g.Nodes {\n\t\tphiFrom := p.phi(g, from)\n\t\tfor _, edge := range g.OutEdges(from) {\n\t\t\tphiTo := p.phi(g, edge.To)\n\t\t\tcost := int64(2 * edge.Cost)\n\t\t\tif cost+phiTo-phiFrom < 0 || cost+phiFrom-phiTo < 0 {\n\t\t\t\treturn false\n\t\t\t}\n\t\t}\n\t}\n\treturn true\n}\n'
Path('internal/search/metric_alt.go').write_text(metric_alt)

path = Path('internal/search/acbs.go')
text = path.read_text()
text = text.replace('acbsMetricALTPotentialModel = "balanced-metric-alt-v1"', 'acbsMetricALTPotentialModel = "balanced-metric-alt-v2"')
text = replace_once(
    text,
    '\tmetricALT  bool\n\tguardMode  acbsGuardMode\n',
    '\tmetricALT      bool\n\tmetricALTLimit int\n\tguardMode      acbsGuardMode\n',
    'options',
)
old = '''func acbsMetricALT(ctx context.Context, g *graph.Graph, source, target int) (Result, error) {
\tif g.EdgeCount < metricALTMinimumEdges {
\t\treturn acbsWithOptions(ctx, g, source, target, acbsOptions{
\t\t\talgorithm: AegisALT, adaptive: true, pruning: false,
\t\t})
\t}
\tif _, ok := metricALTForGraph(g); !ok {
\t\treturn Result{}, errors.New("aegis-alt requires PrepareMetricALT for this graph")
\t}
\treturn acbsWithOptions(ctx, g, source, target, acbsOptions{
\t\talgorithm: AegisALT, adaptive: true, pruning: false, metricALT: true,
\t})
}
'''
new = '''func acbsMetricALT(ctx context.Context, g *graph.Graph, source, target int) (Result, error) {
\treturn acbsMetricALTWithLimit(ctx, g, source, target, AegisALT, 0)
}

func acbsMetricALTTop4(ctx context.Context, g *graph.Graph, source, target int) (Result, error) {
\treturn acbsMetricALTWithLimit(ctx, g, source, target, AegisALTTop4, 4)
}

func acbsMetricALTTop2(ctx context.Context, g *graph.Graph, source, target int) (Result, error) {
\treturn acbsMetricALTWithLimit(ctx, g, source, target, AegisALTTop2, 2)
}

func acbsMetricALTWithLimit(
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
\t\tmetricALT: true, metricALTLimit: limit,
\t})
}
'''
text = replace_once(text, old, new, 'entry points')
text = replace_once(
    text,
    '\t\tpotential = newACBSMetricALTPotential(g, source, target, index)\n',
    '\t\tpotential = newACBSMetricALTPotential(g, source, target, index, opts.metricALTLimit)\n',
    'constructor call',
)
text = replace_once(
    text,
    '''\tif potential.metricALT != nil {
\t\tstats.PotentialLandmarks = len(potential.metricALT.landmarks)
\t\tstats.PotentialIndexBytes = potential.metricALT.bytes()
\t}
''',
    '''\tif potential.metricALT.activeCount > 0 {
\t\tstats.PotentialLandmarks = int(potential.metricALT.activeCount)
\t\tstats.PotentialIndexBytes = potential.metricALT.index.bytes()
\t}
''',
    'stats',
)
text = replace_once(
    text,
    '''\tprojection                bool
\tmetricALT                 *metricALTIndex
\tsource, target            int
\tenabled                   bool
''',
    '''\tprojection                bool
\tmetricALT                 metricALTQuery
\tenabled                   bool
''',
    'potential fields',
)
old = '''func newACBSMetricALTPotential(
\tg *graph.Graph, source, target int, index *metricALTIndex,
) acbsPotential {
\tp := newACBSPotential(g, source, target, false)
\tp.metricALT = index
\tp.source = source
\tp.target = target
\tp.enabled = true
\treturn p
}
'''
new = '''func newACBSMetricALTPotential(
\tg *graph.Graph, source, target int, index *metricALTIndex, limit int,
) acbsPotential {
\tp := newACBSPotential(g, source, target, false)
\tp.metricALT = index.query(source, target, limit)
\tp.enabled = true
\treturn p
}
'''
text = replace_once(text, old, new, 'potential constructor')
old_metric_bounds = '''\tif p.metricALT != nil {
\t\taltForward, altBackward := p.metricALT.bounds(p.source, p.target, v)
'''
new_metric_bounds = '''\tif p.metricALT.activeCount > 0 {
\t\taltForward, altBackward := p.metricALT.bounds(v)
'''
if text.count(old_metric_bounds) != 2:
    raise SystemExit(f"metric bounds: expected two matches, found {text.count(old_metric_bounds)}")
text = text.replace(old_metric_bounds, new_metric_bounds)
path.write_text(text)

path = Path('internal/search/search.go')
text = path.read_text()
text = replace_once(
    text,
    '\tAegisALT          Algorithm = "aegis-alt"\n',
    '\tAegisALT          Algorithm = "aegis-alt"\n\tAegisALTTop4      Algorithm = "aegis-alt-top4"\n\tAegisALTTop2      Algorithm = "aegis-alt-top2"\n',
    'algorithm constants',
)
text = replace_once(
    text,
    '''\tcase AegisALT:
\t\tr, err = acbsMetricALT(ctx, g, source, target)
''',
    '''\tcase AegisALT:
\t\tr, err = acbsMetricALT(ctx, g, source, target)
\tcase AegisALTTop4:
\t\tr, err = acbsMetricALTTop4(ctx, g, source, target)
\tcase AegisALTTop2:
\t\tr, err = acbsMetricALTTop2(ctx, g, source, target)
''',
    'algorithm dispatch',
)
path.write_text(text)

Path('internal/search/metric_alt_test.go').write_text('package search\n\nimport (\n\t"context"\n\t"math/bits"\n\t"testing"\n\n\t"github.com/lasder-ca/aegis-acbs/internal/graph"\n)\n\nfunc TestRecommendedMetricALTLandmarks(t *testing.T) {\n\tcases := []struct {\n\t\tedges int\n\t\twant  int\n\t}{{9_999, 0}, {10_000, 4}, {399_999, 4}, {400_000, 8}}\n\tfor _, tc := range cases {\n\t\tg := &graph.Graph{EdgeCount: tc.edges}\n\t\tif got := RecommendedMetricALTLandmarks(g); got != tc.want {\n\t\t\tt.Fatalf("edges=%d: got %d, want %d", tc.edges, got, tc.want)\n\t\t}\n\t}\n}\n\nfunc TestMetricALTPreparedCandidatesRemainExactAndFeasible(t *testing.T) {\n\tg := gridGraph(t, 40, 40, true)\n\tg.EdgeCount = metricALTMinimumEdges\n\tpreparation, err := PrepareMetricALT(g, 8)\n\tif err != nil {\n\t\tt.Fatal(err)\n\t}\n\tdefer ReleaseMetricALT(g)\n\tif preparation.Landmarks != 8 || preparation.Bytes == 0 || preparation.Reused {\n\t\tt.Fatalf("invalid preparation: %+v", preparation)\n\t}\n\treused, err := PrepareMetricALT(g, 4)\n\tif err != nil || !reused.Reused {\n\t\tt.Fatalf("reuse = %+v, %v", reused, err)\n\t}\n\talgorithms := []Algorithm{AegisALT, AegisALTTop4, AegisALTTop2}\n\tlimits := []int{0, 4, 2}\n\tfor source := 0; source < len(g.Nodes); source += 173 {\n\t\tfor target := 17; target < len(g.Nodes); target += 211 {\n\t\t\twant, err := Run(context.Background(), g, source, target, Dijkstra)\n\t\t\tif err != nil {\n\t\t\t\tt.Fatal(err)\n\t\t\t}\n\t\t\tfor i, algorithm := range algorithms {\n\t\t\t\tif !validateMetricALTFeasibility(g, source, target, limits[i]) {\n\t\t\t\t\tt.Fatalf("infeasible potential for %s %d -> %d", algorithm, source, target)\n\t\t\t\t}\n\t\t\t\tgot, err := Run(context.Background(), g, source, target, algorithm)\n\t\t\t\tif err != nil {\n\t\t\t\t\tt.Fatal(err)\n\t\t\t\t}\n\t\t\t\tif got.Stats.Distance != want.Stats.Distance || got.Stats.OptimalityGap != 0 {\n\t\t\t\t\tt.Fatalf("%s %d -> %d: got=%+v want=%+v", algorithm, source, target, got.Stats, want.Stats)\n\t\t\t\t}\n\t\t\t\tif got.Stats.PotentialModel != acbsMetricALTPotentialModel {\n\t\t\t\t\tt.Fatalf("unexpected potential stats: %+v", got.Stats)\n\t\t\t\t}\n\t\t\t}\n\t\t}\n\t}\n}\n\nfunc TestMetricALTQuerySelectsStrongestLandmarks(t *testing.T) {\n\tindex := metricALTIndex{\n\t\tlandmarks: []int{0, 1, 2, 3},\n\t\tstride:    4,\n\t\tdistances: []uint64{\n\t\t\t0, 10, 20, 30,\n\t\t\t4, 11, 27, 31,\n\t\t\t9, 30, 22, 70,\n\t\t},\n\t}\n\tquery := index.query(0, 2, 2)\n\tif query.activeCount != 2 || bits.OnesCount16(query.activeMask) != 2 {\n\t\tt.Fatalf("query = %+v", query)\n\t}\n\twant := uint16(1<<3 | 1<<1)\n\tif query.activeMask != want {\n\t\tt.Fatalf("mask = %04b, want %04b", query.activeMask, want)\n\t}\n\tforward, backward := query.bounds(1)\n\tif forward != 39 || backward != 1 {\n\t\tt.Fatalf("bounds = %d/%d, want 39/1", forward, backward)\n\t}\n}\n\nfunc TestMetricALTRequiresPreparationOnNonTinyGraph(t *testing.T) {\n\tg := gridGraph(t, 80, 80, true)\n\tg.EdgeCount = metricALTMinimumEdges\n\t_, err := Run(context.Background(), g, 0, len(g.Nodes)-1, AegisALT)\n\tif err == nil {\n\t\tt.Fatal("aegis-alt ran without preprocessing")\n\t}\n}\n\nfunc TestMetricALTPreparationCapsLandmarks(t *testing.T) {\n\tg := gridGraph(t, 8, 8, true)\n\tpreparation, err := PrepareMetricALT(g, metricALTMaximumLandmarks+10)\n\tif err != nil {\n\t\tt.Fatal(err)\n\t}\n\tdefer ReleaseMetricALT(g)\n\tif preparation.Landmarks != metricALTMaximumLandmarks {\n\t\tt.Fatalf("landmarks = %d", preparation.Landmarks)\n\t}\n}\n')

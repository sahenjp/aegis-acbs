#!/usr/bin/env python3
from __future__ import annotations

from pathlib import Path


def replace_once(text: str, old: str, new: str, label: str) -> str:
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{label}: expected one match, found {count}")
    return text.replace(old, new, 1)


def patch_acbs() -> None:
    path = Path("internal/search/acbs.go")
    text = path.read_text()
    text = replace_once(
        text,
        'import (\n\t"context"\n\t"math"\n',
        'import (\n\t"context"\n\t"errors"\n\t"math"\n',
        "acbs imports",
    )
    text = replace_once(
        text,
        '\tacbsChordPotentialModel     = "balanced-chord-v3"\n'
        '\tacbsProjectionModel         = "balanced-projection-v1"\n',
        '\tacbsChordPotentialModel     = "balanced-chord-v3"\n'
        '\tacbsMetricALTPotentialModel = "balanced-metric-alt-v1"\n'
        '\tacbsProjectionModel         = "balanced-projection-v1"\n',
        "potential model constant",
    )
    text = replace_once(
        text,
        "\tadaptive   bool\n\tpruning    bool\n\tprojection bool\n",
        "\tadaptive   bool\n\tpruning    bool\n\tprojection bool\n\tmetricALT  bool\n",
        "acbs option",
    )
    text = replace_once(
        text,
        "func acbsLateGuard(ctx context.Context, g *graph.Graph, source, target int) (Result, error) {\n",
        "func acbsMetricALT(ctx context.Context, g *graph.Graph, source, target int) (Result, error) {\n"
        "\tif g.EdgeCount < metricALTMinimumEdges {\n"
        "\t\treturn acbsWithOptions(ctx, g, source, target, acbsOptions{\n"
        "\t\t\talgorithm: AegisALT, adaptive: true, pruning: false,\n"
        "\t\t})\n"
        "\t}\n"
        "\tif _, ok := metricALTForGraph(g); !ok {\n"
        "\t\treturn Result{}, errors.New(\"aegis-alt requires PrepareMetricALT for this graph\")\n"
        "\t}\n"
        "\treturn acbsWithOptions(ctx, g, source, target, acbsOptions{\n"
        "\t\talgorithm: AegisALT, adaptive: true, pruning: false, metricALT: true,\n"
        "\t})\n"
        "}\n\n"
        "func acbsLateGuard(ctx context.Context, g *graph.Graph, source, target int) (Result, error) {\n",
        "metric ALT entry point",
    )
    text = replace_once(
        text,
        "\tmodelName := acbsChordPotentialModel\n\tif opts.projection {\n\t\tmodelName = acbsProjectionModel\n\t}\n",
        "\tmodelName := acbsChordPotentialModel\n\tif opts.projection {\n\t\tmodelName = acbsProjectionModel\n\t} else if opts.metricALT {\n\t\tmodelName = acbsMetricALTPotentialModel\n\t}\n",
        "model name",
    )
    text = replace_once(
        text,
        "\tpotential := newACBSPotential(g, source, target, opts.projection)\n",
        "\tpotential := newACBSPotential(g, source, target, opts.projection)\n"
        "\tif opts.metricALT {\n"
        "\t\tindex, ok := metricALTForGraph(g)\n"
        "\t\tif !ok {\n"
        "\t\t\treturn Result{}, errors.New(\"metric ALT index disappeared before search\")\n"
        "\t\t}\n"
        "\t\tpotential = newACBSMetricALTPotential(g, source, target, index)\n"
        "\t}\n",
        "potential selection",
    )
    text = replace_once(
        text,
        "\tstats := Stats{\n"
        "\t\tAlgorithm: opts.algorithm, QueuePushes: 2,\n"
        "\t\tSchedulerVersion: schedulerName(opts), PotentialModel: modelName,\n"
        "\t}\n",
        "\tstats := Stats{\n"
        "\t\tAlgorithm: opts.algorithm, QueuePushes: 2,\n"
        "\t\tSchedulerVersion: schedulerName(opts), PotentialModel: modelName,\n"
        "\t}\n"
        "\tif potential.metricALT != nil {\n"
        "\t\tstats.PotentialLandmarks = len(potential.metricALT.landmarks)\n"
        "\t\tstats.PotentialIndexBytes = potential.metricALT.bytes()\n"
        "\t}\n",
        "potential stats",
    )
    text = replace_once(
        text,
        "\tprojection                bool\n\tenabled                   bool\n",
        "\tprojection                bool\n\tmetricALT                 *metricALTIndex\n\tsource, target            int\n\tenabled                   bool\n",
        "potential fields",
    )
    text = replace_once(
        text,
        "func (p acbsPotential) phi(g *graph.Graph, v int) int64 {\n",
        "func newACBSMetricALTPotential(\n"
        "\tg *graph.Graph, source, target int, index *metricALTIndex,\n"
        ") acbsPotential {\n"
        "\tp := newACBSPotential(g, source, target, false)\n"
        "\tp.metricALT = index\n"
        "\tp.source = source\n"
        "\tp.target = target\n"
        "\tp.enabled = true\n"
        "\treturn p\n"
        "}\n\n"
        "func (p acbsPotential) phi(g *graph.Graph, v int) int64 {\n",
        "metric ALT potential constructor",
    )
    text = replace_once(
        text,
        "\tforward := lowerBoundCost(chordUnitMeters(x, y, z, p.targetX, p.targetY, p.targetZ), p.costPerMeter)\n"
        "\tbackward := lowerBoundCost(chordUnitMeters(x, y, z, p.sourceX, p.sourceY, p.sourceZ), p.costPerMeter)\n"
        "\treturn signedDifference(forward, backward)\n",
        "\tforward := lowerBoundCost(chordUnitMeters(x, y, z, p.targetX, p.targetY, p.targetZ), p.costPerMeter)\n"
        "\tbackward := lowerBoundCost(chordUnitMeters(x, y, z, p.sourceX, p.sourceY, p.sourceZ), p.costPerMeter)\n"
        "\tif p.metricALT != nil {\n"
        "\t\taltForward, altBackward := p.metricALT.bounds(p.source, p.target, v)\n"
        "\t\tif altForward > forward {\n"
        "\t\t\tforward = altForward\n"
        "\t\t}\n"
        "\t\tif altBackward > backward {\n"
        "\t\t\tbackward = altBackward\n"
        "\t\t}\n"
        "\t}\n"
        "\treturn signedDifference(forward, backward)\n",
        "phi merge",
    )
    text = replace_once(
        text,
        "\tforward = lowerBoundCost(chordUnitMeters(x, y, z, p.targetX, p.targetY, p.targetZ), p.costPerMeter)\n"
        "\tbackward = lowerBoundCost(chordUnitMeters(x, y, z, p.sourceX, p.sourceY, p.sourceZ), p.costPerMeter)\n"
        "\treturn forward, backward\n",
        "\tforward = lowerBoundCost(chordUnitMeters(x, y, z, p.targetX, p.targetY, p.targetZ), p.costPerMeter)\n"
        "\tbackward = lowerBoundCost(chordUnitMeters(x, y, z, p.sourceX, p.sourceY, p.sourceZ), p.costPerMeter)\n"
        "\tif p.metricALT != nil {\n"
        "\t\taltForward, altBackward := p.metricALT.bounds(p.source, p.target, v)\n"
        "\t\tif altForward > forward {\n"
        "\t\t\tforward = altForward\n"
        "\t\t}\n"
        "\t\tif altBackward > backward {\n"
        "\t\t\tbackward = altBackward\n"
        "\t\t}\n"
        "\t}\n"
        "\treturn forward, backward\n",
        "bound merge",
    )
    path.write_text(text)


def patch_search() -> None:
    path = Path("internal/search/search.go")
    text = path.read_text()
    text = replace_once(
        text,
        '\tAegis             Algorithm = "aegis"\n',
        '\tAegis             Algorithm = "aegis"\n'
        '\tAegisALT          Algorithm = "aegis-alt"\n',
        "algorithm constant",
    )
    text = replace_once(
        text,
        "\tPotentialModel             string    `json:\"potentialModel,omitempty\"`\n",
        "\tPotentialModel             string    `json:\"potentialModel,omitempty\"`\n"
        "\tPotentialLandmarks         int       `json:\"potentialLandmarks,omitempty\"`\n"
        "\tPotentialIndexBytes        uint64    `json:\"potentialIndexBytes,omitempty\"`\n",
        "stats fields",
    )
    text = replace_once(
        text,
        "\tcase Aegis:\n\t\tr, err = acbs(ctx, g, source, target)\n",
        "\tcase Aegis:\n\t\tr, err = acbs(ctx, g, source, target)\n"
        "\tcase AegisALT:\n\t\tr, err = acbsMetricALT(ctx, g, source, target)\n",
        "run case",
    )
    path.write_text(text)


def patch_main() -> None:
    path = Path("cmd/aegis/main.go")
    text = path.read_text()
    text = replace_once(
        text,
        "\tg, err := graph.Load(*path)\n"
        "\tif err != nil {\n"
        "\t\treturn err\n"
        "\t}\n"
        "\ts, err := resolve(g, *src)\n",
        "\tg, err := graph.Load(*path)\n"
        "\tif err != nil {\n"
        "\t\treturn err\n"
        "\t}\n"
        "\tcleanup, err := prepareSearchIndexes(g, []search.Algorithm{search.Algorithm(*alg)})\n"
        "\tif err != nil {\n"
        "\t\treturn err\n"
        "\t}\n"
        "\tdefer cleanup()\n"
        "\ts, err := resolve(g, *src)\n",
        "route preparation",
    )
    text = replace_once(
        text,
        "\treport, err := bench.Run(context.Background(), g, bench.Config{Queries: *queries, Seed: *seed, Algorithms: list, Warmup: 3, Repeats: *repeats, BatchSize: *batchSize, Order: *order, MeasureMemory: *measureMemory, Timeout: *timeout, Suite: *suite, PairMode: *pairMode})\n",
        "\tcleanup, err := prepareSearchIndexes(g, list)\n"
        "\tif err != nil {\n"
        "\t\treturn err\n"
        "\t}\n"
        "\tdefer cleanup()\n"
        "\treport, err := bench.Run(context.Background(), g, bench.Config{Queries: *queries, Seed: *seed, Algorithms: list, Warmup: 3, Repeats: *repeats, BatchSize: *batchSize, Order: *order, MeasureMemory: *measureMemory, Timeout: *timeout, Suite: *suite, PairMode: *pairMode})\n",
        "benchmark preparation",
    )
    stress_old = (
        "\tg, err := graph.Load(*path)\n"
        "\tif err != nil {\n"
        "\t\treturn err\n"
        "\t}\n"
        "\treport, err := bench.RunStress(context.Background(), g, bench.StressConfig{\n"
    )
    stress_new = (
        "\tg, err := graph.Load(*path)\n"
        "\tif err != nil {\n"
        "\t\treturn err\n"
        "\t}\n"
        "\tcleanup, err := prepareSearchIndexes(g, []search.Algorithm{search.Algorithm(*alg)})\n"
        "\tif err != nil {\n"
        "\t\treturn err\n"
        "\t}\n"
        "\tdefer cleanup()\n"
        "\treport, err := bench.RunStress(context.Background(), g, bench.StressConfig{\n"
    )
    text = replace_once(text, stress_old, stress_new, "stress preparation")
    helper = '''
func prepareSearchIndexes(g *graph.Graph, algorithms []search.Algorithm) (func(), error) {
	for _, algorithm := range algorithms {
		if algorithm != search.AegisALT {
			continue
		}
		landmarks := search.RecommendedMetricALTLandmarks(g)
		preparation, err := search.PrepareMetricALT(g, landmarks)
		if err != nil {
			return func() {}, err
		}
		if preparation.Landmarks > 0 {
			fmt.Fprintf(os.Stderr,
				"metric-alt preprocess: landmarks=%d duration=%.3fs memory=%.2fMiB reused=%t\\n",
				preparation.Landmarks,
				preparation.Duration.Seconds(),
				float64(preparation.Bytes)/(1024*1024),
				preparation.Reused,
			)
		}
		return func() { search.ReleaseMetricALT(g) }, nil
	}
	return func() {}, nil
}

'''
    text = replace_once(text, "func resolve(g *graph.Graph, value string) (int, error) {\n", helper + "func resolve(g *graph.Graph, value string) (int, error) {\n", "helper insertion")
    path.write_text(text)


def create_metric_alt() -> None:
    Path("internal/search/metric_alt.go").write_text(r'''package search

import (
	"errors"
	"sync"
	"time"

	"github.com/lasder-ca/aegis-acbs/internal/graph"
)

const (
	metricALTMinimumEdges = 10_000
	metricALTLargeEdges   = 400_000
)

// MetricALTPreparation describes graph-wide preprocessing performed outside
// timed queries. The index is immutable after publication and safe for
// concurrent searches.
type MetricALTPreparation struct {
	Landmarks int
	Duration  time.Duration
	Bytes     uint64
	Reused    bool
}

type metricALTIndex struct {
	landmarks []int
	distances [][]uint64
}

var (
	metricALTIndexes   sync.Map // map[*graph.Graph]*metricALTIndex
	metricALTPrepareMu sync.Mutex
)

// RecommendedMetricALTLandmarks selects the measured memory/runtime tradeoff.
// Tiny graphs retain production ACBS, medium graphs use four landmarks, and
// larger regional graphs use eight.
func RecommendedMetricALTLandmarks(g *graph.Graph) int {
	if g == nil || g.EdgeCount < metricALTMinimumEdges {
		return 0
	}
	if g.EdgeCount < metricALTLargeEdges {
		return 4
	}
	return 8
}

// PrepareMetricALT builds a metric landmark index over the undirected
// relaxation of g. Every directed edge is inserted into the relaxation with
// its original cost; therefore relaxation distance is a lower bound on every
// directed path and remains 1-Lipschitz on every directed edge.
func PrepareMetricALT(g *graph.Graph, count int) (MetricALTPreparation, error) {
	if g == nil || len(g.Nodes) == 0 {
		return MetricALTPreparation{}, errors.New("metric ALT requires a non-empty graph")
	}
	if count < 0 {
		return MetricALTPreparation{}, errors.New("metric ALT landmark count cannot be negative")
	}
	if count == 0 {
		ReleaseMetricALT(g)
		return MetricALTPreparation{}, nil
	}
	if count > len(g.Nodes) {
		count = len(g.Nodes)
	}

	metricALTPrepareMu.Lock()
	defer metricALTPrepareMu.Unlock()
	if existing, ok := metricALTForGraph(g); ok && len(existing.landmarks) >= count {
		return MetricALTPreparation{
			Landmarks: len(existing.landmarks),
			Bytes:     existing.bytes(),
			Reused:    true,
		}, nil
	}

	started := time.Now()
	index := &metricALTIndex{}
	selected := make([]bool, len(g.Nodes))
	nearest := make([]uint64, len(g.Nodes))
	for i := range nearest {
		nearest[i] = inf
	}

	landmark := highestDegreeNode(g, selected, false)
	for len(index.landmarks) < count && landmark >= 0 {
		selected[landmark] = true
		distances := metricALTUndirectedDistances(g, landmark)
		index.landmarks = append(index.landmarks, landmark)
		index.distances = append(index.distances, distances)

		for v, distance := range distances {
			if distance < nearest[v] {
				nearest[v] = distance
			}
		}
		landmark = nextMetricALTLandmark(g, selected, nearest)
	}
	if len(index.landmarks) == 0 {
		return MetricALTPreparation{}, errors.New("metric ALT could not select a landmark")
	}

	metricALTIndexes.Store(g, index)
	return MetricALTPreparation{
		Landmarks: len(index.landmarks),
		Duration:  time.Since(started),
		Bytes:     index.bytes(),
	}, nil
}

// ReleaseMetricALT removes the graph-to-index association. Searches that have
// already captured the immutable index remain valid.
func ReleaseMetricALT(g *graph.Graph) {
	if g != nil {
		metricALTIndexes.Delete(g)
	}
}

func MetricALTPrepared(g *graph.Graph) bool {
	_, ok := metricALTForGraph(g)
	return ok
}

func metricALTForGraph(g *graph.Graph) (*metricALTIndex, bool) {
	value, ok := metricALTIndexes.Load(g)
	if !ok {
		return nil, false
	}
	return value.(*metricALTIndex), true
}

func (index *metricALTIndex) bytes() uint64 {
	if index == nil || len(index.distances) == 0 {
		return 0
	}
	return uint64(len(index.distances)) * uint64(len(index.distances[0])) * 8
}

func highestDegreeNode(g *graph.Graph, selected []bool, requireUnreached bool) int {
	best := -1
	bestDegree := -1
	for v := range g.Nodes {
		if selected[v] {
			continue
		}
		_ = requireUnreached
		degree := g.OutDegree(v) + g.InDegree(v)
		if degree > bestDegree {
			best = v
			bestDegree = degree
		}
	}
	return best
}

func nextMetricALTLandmark(g *graph.Graph, selected []bool, nearest []uint64) int {
	// Cover disconnected components first; no finite landmark distance exists
	// there yet. Within covered components use farthest-point sampling.
	disconnected := -1
	disconnectedDegree := -1
	best := -1
	bestDistance := uint64(0)
	for v := range g.Nodes {
		if selected[v] {
			continue
		}
		if nearest[v] == inf {
			degree := g.OutDegree(v) + g.InDegree(v)
			if degree > disconnectedDegree {
				disconnected = v
				disconnectedDegree = degree
			}
			continue
		}
		if nearest[v] > bestDistance {
			best = v
			bestDistance = nearest[v]
		}
	}
	if disconnected >= 0 {
		return disconnected
	}
	return best
}

func metricALTUndirectedDistances(g *graph.Graph, source int) []uint64 {
	dist := make([]uint64, len(g.Nodes))
	for i := range dist {
		dist[i] = inf
	}
	dist[source] = 0
	var queue radixHeap
	radixPush(&queue, item{node: source, distance: 0, priority: 0})
	for queue.Len() > 0 {
		cur := radixPop(&queue)
		if cur.distance != dist[cur.node] {
			continue
		}
		relaxMetricALTEdges(&queue, dist, cur, g.OutEdges(cur.node))
		relaxMetricALTEdges(&queue, dist, cur, g.InEdges(cur.node))
	}
	return dist
}

func relaxMetricALTEdges(queue *radixHeap, dist []uint64, cur item, edges []graph.Edge) {
	for _, edge := range edges {
		if cur.distance > inf-edge.Cost {
			continue
		}
		next := cur.distance + edge.Cost
		if next >= dist[edge.To] {
			continue
		}
		dist[edge.To] = next
		radixPush(queue, item{node: edge.To, distance: next, priority: next})
	}
}

func (index *metricALTIndex) bounds(source, target, v int) (forward, backward uint64) {
	for _, distances := range index.distances {
		if distances[target] != inf && distances[v] != inf {
			if value := absDiffUint64(distances[target], distances[v]); value > forward {
				forward = value
			}
		}
		if distances[source] != inf && distances[v] != inf {
			if value := absDiffUint64(distances[source], distances[v]); value > backward {
				backward = value
			}
		}
	}
	return forward, backward
}

func absDiffUint64(a, b uint64) uint64 {
	if a >= b {
		return a - b
	}
	return b - a
}

func validateMetricALTFeasibility(g *graph.Graph, source, target int) bool {
	index, ok := metricALTForGraph(g)
	if !ok {
		return g.EdgeCount < metricALTMinimumEdges
	}
	p := newACBSMetricALTPotential(g, source, target, index)
	for from := range g.Nodes {
		phiFrom := p.phi(g, from)
		for _, edge := range g.OutEdges(from) {
			phiTo := p.phi(g, edge.To)
			cost := int64(2 * edge.Cost)
			if cost+phiTo-phiFrom < 0 || cost+phiFrom-phiTo < 0 {
				return false
			}
		}
	}
	return true
}
''')


def create_tests() -> None:
    Path("internal/search/metric_alt_test.go").write_text(r'''package search

import (
	"context"
	"testing"

	"github.com/lasder-ca/aegis-acbs/internal/graph"
)

func TestRecommendedMetricALTLandmarks(t *testing.T) {
	cases := []struct {
		edges int
		want  int
	}{{9_999, 0}, {10_000, 4}, {399_999, 4}, {400_000, 8}}
	for _, tc := range cases {
		g := &graph.Graph{EdgeCount: tc.edges}
		if got := RecommendedMetricALTLandmarks(g); got != tc.want {
			t.Fatalf("edges=%d: got %d, want %d", tc.edges, got, tc.want)
		}
	}
}

func TestMetricALTPreparedCandidateRemainsExactAndFeasible(t *testing.T) {
	g := gridGraph(t, 40, 40, true)
	preparation, err := PrepareMetricALT(g, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer ReleaseMetricALT(g)
	if preparation.Landmarks != 4 || preparation.Bytes == 0 || preparation.Reused {
		t.Fatalf("invalid preparation: %+v", preparation)
	}
	reused, err := PrepareMetricALT(g, 4)
	if err != nil || !reused.Reused {
		t.Fatalf("reuse = %+v, %v", reused, err)
	}
	for source := 0; source < len(g.Nodes); source += 89 {
		for target := 17; target < len(g.Nodes); target += 127 {
			if !validateMetricALTFeasibility(g, source, target) {
				t.Fatalf("infeasible potential for %d -> %d", source, target)
			}
			want, err := Run(context.Background(), g, source, target, Dijkstra)
			if err != nil {
				t.Fatal(err)
			}
			got, err := Run(context.Background(), g, source, target, AegisALT)
			if err != nil {
				t.Fatal(err)
			}
			if got.Stats.Distance != want.Stats.Distance || got.Stats.OptimalityGap != 0 {
				t.Fatalf("%d -> %d: got=%+v want=%+v", source, target, got.Stats, want.Stats)
			}
			if got.Stats.PotentialModel != acbsMetricALTPotentialModel || got.Stats.PotentialLandmarks != 4 {
				t.Fatalf("unexpected potential stats: %+v", got.Stats)
			}
		}
	}
}

func TestMetricALTRequiresPreparationOnNonTinyGraph(t *testing.T) {
	g := gridGraph(t, 80, 80, true)
	g.EdgeCount = metricALTMinimumEdges
	_, err := Run(context.Background(), g, 0, len(g.Nodes)-1, AegisALT)
	if err == nil {
		t.Fatal("aegis-alt ran without preprocessing")
	}
}

func TestMetricALTBoundsAreSymmetricLandmarkDifferences(t *testing.T) {
	index := metricALTIndex{distances: [][]uint64{{0, 4, 10}}}
	forward, backward := index.bounds(0, 2, 1)
	if forward != 6 || backward != 4 {
		t.Fatalf("bounds = %d/%d, want 6/4", forward, backward)
	}
}
''')


def main() -> None:
    patch_acbs()
    patch_search()
    patch_main()
    create_metric_alt()
    create_tests()


if __name__ == "__main__":
    main()

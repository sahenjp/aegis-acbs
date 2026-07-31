#!/usr/bin/env python3
from __future__ import annotations

import argparse
from pathlib import Path


def replace_once(text: str, old: str, new: str, label: str) -> str:
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{label}: expected one match, found {count}")
    return text.replace(old, new, 1)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--landmarks", type=int, required=True)
    args = parser.parse_args()
    if args.landmarks <= 0:
        raise SystemExit("--landmarks must be positive")

    acbs_path = Path("internal/search/acbs.go")
    acbs = acbs_path.read_text()
    acbs = replace_once(
        acbs,
        '\tacbsChordPotentialModel      = "balanced-chord-v3"\n'
        '\tacbsProjectionModel          = "balanced-projection-v1"\n',
        '\tacbsChordPotentialModel      = "balanced-chord-v3"\n'
        f'\tacbsALTPotentialModel        = "balanced-alt-v1-k{args.landmarks}"\n'
        '\tacbsProjectionModel          = "balanced-projection-v1"\n',
        "model constants",
    )
    acbs = replace_once(
        acbs,
        "\tentropic   bool\n\tpruning    bool\n\tprojection bool\n",
        "\tentropic   bool\n\tpruning    bool\n\tprojection bool\n\talt        bool\n",
        "options",
    )
    acbs = replace_once(
        acbs,
        "\t\talgorithm: AegisEntropic,\n\t\tadaptive:  true,\n\t\tentropic:  true,\n\t\tpruning:   false,\n",
        "\t\talgorithm: AegisEntropic,\n\t\tadaptive:  true,\n\t\talt:       true,\n\t\tpruning:   false,\n",
        "candidate options",
    )
    acbs = replace_once(
        acbs,
        "\tmodelName := acbsChordPotentialModel\n\tif opts.projection {\n\t\tmodelName = acbsProjectionModel\n\t}\n",
        "\tmodelName := acbsChordPotentialModel\n\tif opts.projection {\n\t\tmodelName = acbsProjectionModel\n\t} else if opts.alt {\n\t\tmodelName = acbsALTPotentialModel\n\t}\n",
        "model name",
    )
    acbs = replace_once(
        acbs,
        "\tpotential := newACBSPotential(g, source, target, opts.projection)\n",
        "\tpotential := newACBSPotentialMode(g, source, target, opts.projection, opts.alt)\n",
        "potential construction",
    )
    acbs = replace_once(
        acbs,
        "\tprojection                bool\n\tenabled                   bool\n",
        "\tprojection                bool\n\talt                       *acbsALTIndex\n\tsource, target            int\n\tenabled                   bool\n",
        "potential fields",
    )
    acbs = replace_once(
        acbs,
        "func newACBSPotential(g *graph.Graph, source, target int, projection bool) acbsPotential {\n"
        "\tif g.MinCostPerMeter <= 0 {\n"
        "\t\treturn acbsPotential{}\n"
        "\t}\n",
        "func newACBSPotential(g *graph.Graph, source, target int, projection bool) acbsPotential {\n"
        "\treturn newACBSPotentialMode(g, source, target, projection, false)\n"
        "}\n\n"
        "func newACBSPotentialMode(g *graph.Graph, source, target int, projection, useALT bool) acbsPotential {\n"
        "\tvar alt *acbsALTIndex\n"
        "\tif useALT {\n"
        "\t\talt = acbsALTForGraph(g)\n"
        "\t}\n"
        "\tif g.MinCostPerMeter <= 0 {\n"
        "\t\treturn acbsPotential{alt: alt, source: source, target: target, enabled: alt != nil}\n"
        "\t}\n",
        "potential constructors",
    )
    acbs = replace_once(
        acbs,
        "\t\tcostPerMeter: g.MinCostPerMeter * (1 - 1e-12),\n"
        "\t\tprojection:   projection,\n"
        "\t\tenabled:      true,\n",
        "\t\tcostPerMeter: g.MinCostPerMeter * (1 - 1e-12),\n"
        "\t\tprojection:   projection,\n"
        "\t\talt:          alt,\n"
        "\t\tsource:       source,\n"
        "\t\ttarget:       target,\n"
        "\t\tenabled:      true,\n",
        "potential init",
    )
    acbs = replace_once(
        acbs,
        "\tforward := lowerBoundCost(chordUnitMeters(x, y, z, p.targetX, p.targetY, p.targetZ), p.costPerMeter)\n"
        "\tbackward := lowerBoundCost(chordUnitMeters(x, y, z, p.sourceX, p.sourceY, p.sourceZ), p.costPerMeter)\n"
        "\treturn signedDifference(forward, backward)\n",
        "\tforward := lowerBoundCost(chordUnitMeters(x, y, z, p.targetX, p.targetY, p.targetZ), p.costPerMeter)\n"
        "\tbackward := lowerBoundCost(chordUnitMeters(x, y, z, p.sourceX, p.sourceY, p.sourceZ), p.costPerMeter)\n"
        "\tif p.alt != nil {\n"
        "\t\taltForward, altBackward := p.alt.bounds(p.source, p.target, v)\n"
        "\t\tif altForward > forward {\n"
        "\t\t\tforward = altForward\n"
        "\t\t}\n"
        "\t\tif altBackward > backward {\n"
        "\t\t\tbackward = altBackward\n"
        "\t\t}\n"
        "\t}\n"
        "\treturn signedDifference(forward, backward)\n",
        "phi ALT merge",
    )
    acbs = replace_once(
        acbs,
        "\tforward = lowerBoundCost(chordUnitMeters(x, y, z, p.targetX, p.targetY, p.targetZ), p.costPerMeter)\n"
        "\tbackward = lowerBoundCost(chordUnitMeters(x, y, z, p.sourceX, p.sourceY, p.sourceZ), p.costPerMeter)\n"
        "\treturn forward, backward\n",
        "\tforward = lowerBoundCost(chordUnitMeters(x, y, z, p.targetX, p.targetY, p.targetZ), p.costPerMeter)\n"
        "\tbackward = lowerBoundCost(chordUnitMeters(x, y, z, p.sourceX, p.sourceY, p.sourceZ), p.costPerMeter)\n"
        "\tif p.alt != nil {\n"
        "\t\taltForward, altBackward := p.alt.bounds(p.source, p.target, v)\n"
        "\t\tif altForward > forward {\n"
        "\t\t\tforward = altForward\n"
        "\t\t}\n"
        "\t\tif altBackward > backward {\n"
        "\t\t\tbackward = altBackward\n"
        "\t\t}\n"
        "\t}\n"
        "\treturn forward, backward\n",
        "bound ALT merge",
    )
    acbs = replace_once(
        acbs,
        'acbsEntropicSchedulerVersion = "entropic-proof-rate-v2"',
        f'acbsEntropicSchedulerVersion = "balanced-alt-v1-k{args.landmarks}"',
        "candidate version",
    )
    acbs = replace_once(
        acbs,
        "\tif opts.entropic {\n\t\treturn acbsEntropicSchedulerVersion\n\t}\n",
        "\tif opts.entropic || opts.alt {\n\t\treturn acbsEntropicSchedulerVersion\n\t}\n",
        "candidate identity",
    )
    acbs_path.write_text(acbs)

    Path("internal/search/alt_experiment.go").write_text(
        '''package search

import (
	"errors"
	"sync"
	"time"

	"github.com/lasder-ca/aegis-acbs/internal/graph"
)

type ALTPreparation struct {
	Landmarks int
	Duration  time.Duration
	Bytes     uint64
}

type acbsALTIndex struct {
	landmarks []int
	from      [][]uint64 // d(landmark, v)
	to        [][]uint64 // d(v, landmark)
}

var acbsALTIndexes sync.Map // map[*graph.Graph]*acbsALTIndex

func PrepareALT(g *graph.Graph, count int) (ALTPreparation, error) {
	if g == nil || len(g.Nodes) == 0 {
		return ALTPreparation{}, errors.New("ALT requires a non-empty graph")
	}
	if count <= 0 {
		return ALTPreparation{}, errors.New("ALT landmark count must be positive")
	}
	if count > len(g.Nodes) {
		count = len(g.Nodes)
	}
	started := time.Now()
	index := &acbsALTIndex{}
	selected := make([]bool, len(g.Nodes))
	nearest := make([]uint64, len(g.Nodes))
	for i := range nearest {
		nearest[i] = inf
	}

	landmark := 0
	bestDegree := -1
	for v := range g.Nodes {
		degree := g.OutDegree(v) + g.InDegree(v)
		if degree > bestDegree {
			bestDegree = degree
			landmark = v
		}
	}

	for len(index.landmarks) < count && !selected[landmark] {
		selected[landmark] = true
		from := acbsALTDistances(g, landmark, false)
		to := acbsALTDistances(g, landmark, true)
		index.landmarks = append(index.landmarks, landmark)
		index.from = append(index.from, from)
		index.to = append(index.to, to)

		next := -1
		bestDistance := uint64(0)
		for v := range g.Nodes {
			if selected[v] {
				continue
			}
			if from[v] == inf || to[v] == inf {
				if len(index.landmarks) == 1 {
					nearest[v] = 0
				}
				continue
			}
			d := from[v]
			if to[v] < d {
				d = to[v]
			}
			if d < nearest[v] {
				nearest[v] = d
			}
			if nearest[v] > bestDistance {
				bestDistance = nearest[v]
				next = v
			}
		}
		if next < 0 || bestDistance == 0 {
			break
		}
		landmark = next
	}
	if len(index.landmarks) == 0 {
		return ALTPreparation{}, errors.New("ALT could not select a landmark")
	}
	acbsALTIndexes.Store(g, index)
	bytes := uint64(len(index.landmarks)) * uint64(len(g.Nodes)) * 2 * 8
	return ALTPreparation{Landmarks: len(index.landmarks), Duration: time.Since(started), Bytes: bytes}, nil
}

func acbsALTForGraph(g *graph.Graph) *acbsALTIndex {
	value, ok := acbsALTIndexes.Load(g)
	if !ok {
		return nil
	}
	return value.(*acbsALTIndex)
}

func acbsALTDistances(g *graph.Graph, source int, reverse bool) []uint64 {
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
		edges := g.OutEdges(cur.node)
		if reverse {
			edges = g.InEdges(cur.node)
		}
		for _, edge := range edges {
			if cur.distance > inf-edge.Cost {
				continue
			}
			next := cur.distance + edge.Cost
			if next >= dist[edge.To] {
				continue
			}
			dist[edge.To] = next
			radixPush(&queue, item{node: edge.To, distance: next, priority: next})
		}
	}
	return dist
}

func (index *acbsALTIndex) bounds(source, target, v int) (forward, backward uint64) {
	for i := range index.landmarks {
		from := index.from[i]
		to := index.to[i]

		// Directed ALT lower bounds for d(v,target).
		if from[target] != inf && from[v] != inf && from[target] > from[v] {
			if value := from[target] - from[v]; value > forward {
				forward = value
			}
		}
		if to[v] != inf && to[target] != inf && to[v] > to[target] {
			if value := to[v] - to[target]; value > forward {
				forward = value
			}
		}

		// Directed ALT lower bounds for d(source,v).
		if from[v] != inf && from[source] != inf && from[v] > from[source] {
			if value := from[v] - from[source]; value > backward {
				backward = value
			}
		}
		if to[source] != inf && to[v] != inf && to[source] > to[v] {
			if value := to[source] - to[v]; value > backward {
				backward = value
			}
		}
	}
	return forward, backward
}
'''
    )

    main_path = Path("cmd/aegis/main.go")
    main_text = main_path.read_text()
    main_text = replace_once(
        main_text,
        "\treport, err := bench.Run(context.Background(), g, bench.Config{Queries: *queries, Seed: *seed, Algorithms: list, Warmup: 3, Repeats: *repeats, BatchSize: *batchSize, Order: *order, MeasureMemory: *measureMemory, Timeout: *timeout, Suite: *suite, PairMode: *pairMode})\n",
        "\tfor _, algorithm := range list {\n"
        "\t\tif algorithm != search.AegisEntropic {\n"
        "\t\t\tcontinue\n"
        "\t\t}\n"
        f"\t\tpreparation, prepErr := search.PrepareALT(g, {args.landmarks})\n"
        "\t\tif prepErr != nil {\n"
        "\t\t\treturn prepErr\n"
        "\t\t}\n"
        "\t\tfmt.Printf(\"alt-preprocess landmarks=%d duration=%.3fs memory=%.2fMiB\\n\", preparation.Landmarks, preparation.Duration.Seconds(), float64(preparation.Bytes)/(1024*1024))\n"
        "\t\tbreak\n"
        "\t}\n"
        "\treport, err := bench.Run(context.Background(), g, bench.Config{Queries: *queries, Seed: *seed, Algorithms: list, Warmup: 3, Repeats: *repeats, BatchSize: *batchSize, Order: *order, MeasureMemory: *measureMemory, Timeout: *timeout, Suite: *suite, PairMode: *pairMode})\n",
        "benchmark preprocessing",
    )
    main_path.write_text(main_text)

    Path("internal/search/alt_experiment_test.go").write_text(
        '''package search

import (
	"context"
	"testing"
)

func TestALTPreparedCandidateRemainsExact(t *testing.T) {
	g := gridGraph(t, 40, 40, true)
	preparation, err := PrepareALT(g, 4)
	if err != nil {
		t.Fatal(err)
	}
	if preparation.Landmarks == 0 || preparation.Bytes == 0 {
		t.Fatalf("invalid preparation: %+v", preparation)
	}
	for source := 0; source < len(g.Nodes); source += 89 {
		for target := 17; target < len(g.Nodes); target += 127 {
			want, err := Run(context.Background(), g, source, target, Dijkstra)
			if err != nil {
				t.Fatal(err)
			}
			got, err := Run(context.Background(), g, source, target, AegisEntropic)
			if err != nil {
				t.Fatal(err)
			}
			if got.Stats.Distance != want.Stats.Distance || got.Stats.OptimalityGap != 0 {
				t.Fatalf("%d -> %d: got=%+v want=%+v", source, target, got.Stats, want.Stats)
			}
		}
	}
}

func TestALTBoundsUseDirectedTriangleInequalities(t *testing.T) {
	index := acbsALTIndex{
		landmarks: []int{0},
		from: [][]uint64{{0, 4, 10}},
		to:   [][]uint64{{0, 7, 12}},
	}
	forward, backward := index.bounds(0, 2, 1)
	if forward != 6 {
		t.Fatalf("forward bound = %d, want 6", forward)
	}
	if backward != 4 {
		t.Fatalf("backward bound = %d, want 4", backward)
	}
}
'''
    )


if __name__ == "__main__":
    main()

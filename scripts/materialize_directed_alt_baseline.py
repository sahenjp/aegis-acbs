#!/usr/bin/env python3
from pathlib import Path


def replace_once(text: str, old: str, new: str, label: str) -> str:
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{label}: expected one match, found {count}")
    return text.replace(old, new, 1)


Path("internal/search/directed_alt.go").write_text(r'''package search

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/lasder-ca/aegis-acbs/internal/graph"
)

const aStarDirectedALTPotentialModel = "astar-directed-alt-v1"

type DirectedALTPreparation struct {
	Landmarks int
	Duration  time.Duration
	Bytes     uint64
	Reused    bool
}

type directedALTIndex struct {
	landmarks []int
	stride    int
	from      []uint64 // d(landmark, v)
	to        []uint64 // d(v, landmark)
}

var (
	directedALTIndexes   sync.Map // map[*graph.Graph]*directedALTIndex
	directedALTPrepareMu sync.Mutex
)

func PrepareDirectedALT(g *graph.Graph) (DirectedALTPreparation, error) {
	if g == nil || len(g.Nodes) == 0 {
		return DirectedALTPreparation{}, errors.New("directed ALT requires a non-empty graph")
	}
	metricIndex, ok := metricALTForGraph(g)
	if !ok || len(metricIndex.landmarks) == 0 {
		return DirectedALTPreparation{}, errors.New("directed ALT requires a prepared metric landmark index")
	}

	directedALTPrepareMu.Lock()
	defer directedALTPrepareMu.Unlock()
	if existing, ok := directedALTForGraph(g); ok &&
		len(existing.landmarks) == len(metricIndex.landmarks) {
		return DirectedALTPreparation{
			Landmarks: len(existing.landmarks),
			Bytes:     existing.bytes(),
			Reused:    true,
		}, nil
	}

	started := time.Now()
	count := len(metricIndex.landmarks)
	index := &directedALTIndex{
		landmarks: append([]int(nil), metricIndex.landmarks...),
		stride:    count,
		from:      make([]uint64, len(g.Nodes)*count),
		to:        make([]uint64, len(g.Nodes)*count),
	}
	for column, landmark := range index.landmarks {
		from := directedALTDistances(g, landmark, false)
		to := directedALTDistances(g, landmark, true)
		for node := range g.Nodes {
			offset := node*count + column
			index.from[offset] = from[node]
			index.to[offset] = to[node]
		}
	}
	directedALTIndexes.Store(g, index)
	return DirectedALTPreparation{
		Landmarks: count,
		Duration:  time.Since(started),
		Bytes:     index.bytes(),
	}, nil
}

func ReleaseDirectedALT(g *graph.Graph) {
	if g != nil {
		directedALTIndexes.Delete(g)
	}
}

func directedALTForGraph(g *graph.Graph) (*directedALTIndex, bool) {
	value, ok := directedALTIndexes.Load(g)
	if !ok {
		return nil, false
	}
	return value.(*directedALTIndex), true
}

func (index *directedALTIndex) bytes() uint64 {
	if index == nil {
		return 0
	}
	return uint64(len(index.from)+len(index.to)) * 8
}

func (index *directedALTIndex) row(data []uint64, node int) []uint64 {
	start := node * index.stride
	return data[start : start+len(index.landmarks)]
}

func directedALTDistances(g *graph.Graph, source int, reverse bool) []uint64 {
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
		relaxMetricALTEdges(&queue, dist, cur, edges)
	}
	return dist
}

func (index *directedALTIndex) lowerBound(node, target int) uint64 {
	fromNode := index.row(index.from, node)
	fromTarget := index.row(index.from, target)
	toNode := index.row(index.to, node)
	toTarget := index.row(index.to, target)
	bound := uint64(0)
	for i := range index.landmarks {
		if fromNode[i] != inf && fromTarget[i] != inf &&
			fromTarget[i] >= fromNode[i] {
			if value := fromTarget[i] - fromNode[i]; value > bound {
				bound = value
			}
		}
		if toNode[i] != inf && toTarget[i] != inf &&
			toNode[i] >= toTarget[i] {
			if value := toNode[i] - toTarget[i]; value > bound {
				bound = value
			}
		}
	}
	return bound
}

func aStarDirectedALT(
	ctx context.Context, g *graph.Graph, source, target int,
) (Result, error) {
	if g.EdgeCount < metricALTMinimumEdges {
		result, err := dijkstra(ctx, g, source, target, true)
		result.Stats.Algorithm = AStarDirectedALT
		return result, err
	}
	index, ok := directedALTForGraph(g)
	if !ok {
		return Result{}, errors.New("astar-directed-alt requires PrepareDirectedALT")
	}

	n := len(g.Nodes)
	w := acquireSingleWorkspace(n)
	defer releaseSingleWorkspace(w)
	dist, prev := w.dist, w.prev
	w.touch(source)
	dist[source] = 0
	q := &w.q
	push(q, item{
		node: source, distance: 0,
		priority: directedALTHeuristic(g, index, source, target),
	})
	stats := Stats{
		Algorithm:           AStarDirectedALT,
		QueuePushes:         1,
		PotentialModel:      aStarDirectedALTPotentialModel,
		PotentialLandmarks:  len(index.landmarks),
		PotentialIndexBytes: index.bytes(),
	}
	for q.Len() > 0 {
		if stats.Expanded&1023 == 0 {
			select {
			case <-ctx.Done():
				return Result{}, ctx.Err()
			default:
			}
		}
		cur := pop(q)
		stats.QueuePops++
		if cur.distance != dist[cur.node] {
			stats.StalePops++
			continue
		}
		stats.Expanded++
		if cur.node == target {
			break
		}
		for _, edge := range g.OutEdges(cur.node) {
			stats.Relaxed++
			if dist[cur.node] > inf-edge.Cost {
				continue
			}
			nextDistance := dist[cur.node] + edge.Cost
			if nextDistance >= dist[edge.To] {
				continue
			}
			w.touch(edge.To)
			dist[edge.To] = nextDistance
			prev[edge.To] = cur.node
			h := directedALTHeuristic(g, index, edge.To, target)
			priority := nextDistance + h
			if priority < nextDistance {
				priority = inf
			}
			push(q, item{
				node: edge.To, distance: nextDistance, priority: priority,
			})
			stats.QueuePushes++
		}
	}
	if dist[target] == inf {
		return Result{Stats: stats}, nil
	}
	path := reconstruct(prev, source, target)
	stats.Distance = dist[target]
	stats.Reachable = true
	stats.PathNodes = len(path)
	stats.UpperBound = dist[target]
	stats.LowerBound = dist[target]
	return Result{Path: path, Stats: stats}, nil
}

func directedALTHeuristic(
	g *graph.Graph, index *directedALTIndex, node, target int,
) uint64 {
	chord := heuristic(g, node, target, true)
	landmark := index.lowerBound(node, target)
	if landmark > chord {
		return landmark
	}
	return chord
}
''')

path = Path("internal/search/search.go")
text = path.read_text()
text = replace_once(
    text,
    '\tAStarALT          Algorithm = "astar-alt"\n',
    '\tAStarALT          Algorithm = "astar-alt"\n'
    '\tAStarDirectedALT  Algorithm = "astar-directed-alt"\n',
    "algorithm constant",
)
text = replace_once(
    text,
    '''\tcase AStarALT:
\t\tr, err = aStarMetricALT(ctx, g, source, target)
''',
    '''\tcase AStarALT:
\t\tr, err = aStarMetricALT(ctx, g, source, target)
\tcase AStarDirectedALT:
\t\tr, err = aStarDirectedALT(ctx, g, source, target)
''',
    "dispatch",
)
path.write_text(text)

path = Path("cmd/aegis/main.go")
text = path.read_text()
old = '''func prepareSearchIndexes(g *graph.Graph, algorithms []search.Algorithm) (func(), error) {
\tfor _, algorithm := range algorithms {
\t\tif algorithm != search.AegisALT {
\t\t\tcontinue
\t\t}
\t\tlandmarks := search.RecommendedMetricALTLandmarks(g)
\t\tpreparation, err := search.PrepareMetricALT(g, landmarks)
\t\tif err != nil {
\t\t\treturn func() {}, err
\t\t}
\t\tif preparation.Landmarks > 0 {
\t\t\tfmt.Fprintf(os.Stderr,
\t\t\t\t"metric-alt preprocess: landmarks=%d duration=%.3fs memory=%.2fMiB reused=%t\\n",
\t\t\t\tpreparation.Landmarks,
\t\t\t\tpreparation.Duration.Seconds(),
\t\t\t\tfloat64(preparation.Bytes)/(1024*1024),
\t\t\t\tpreparation.Reused,
\t\t\t)
\t\t}
\t\treturn func() { search.ReleaseMetricALT(g) }, nil
\t}
\treturn func() {}, nil
}
'''
new = '''func prepareSearchIndexes(g *graph.Graph, algorithms []search.Algorithm) (func(), error) {
\tneedMetric := false
\tneedDirected := false
\tfor _, algorithm := range algorithms {
\t\tswitch algorithm {
\t\tcase search.AegisALT, search.AegisALTTop4, search.AegisALTTop2,
\t\t\tsearch.AStarALT, search.AStarDirectedALT:
\t\t\tneedMetric = true
\t\t}
\t\tif algorithm == search.AStarDirectedALT {
\t\t\tneedDirected = true
\t\t}
\t}
\tif !needMetric {
\t\treturn func() {}, nil
\t}

\tlandmarks := search.RecommendedMetricALTLandmarks(g)
\tpreparation, err := search.PrepareMetricALT(g, landmarks)
\tif err != nil {
\t\treturn func() {}, err
\t}
\tif preparation.Landmarks > 0 {
\t\tfmt.Fprintf(os.Stderr,
\t\t\t"metric-alt preprocess: landmarks=%d duration=%.3fs memory=%.2fMiB reused=%t\\n",
\t\t\tpreparation.Landmarks,
\t\t\tpreparation.Duration.Seconds(),
\t\t\tfloat64(preparation.Bytes)/(1024*1024),
\t\t\tpreparation.Reused,
\t\t)
\t}
\tif needDirected && landmarks > 0 {
\t\tdirected, err := search.PrepareDirectedALT(g)
\t\tif err != nil {
\t\t\tsearch.ReleaseMetricALT(g)
\t\t\treturn func() {}, err
\t\t}
\t\tfmt.Fprintf(os.Stderr,
\t\t\t"directed-alt preprocess: landmarks=%d duration=%.3fs memory=%.2fMiB reused=%t\\n",
\t\t\tdirected.Landmarks,
\t\t\tdirected.Duration.Seconds(),
\t\t\tfloat64(directed.Bytes)/(1024*1024),
\t\t\tdirected.Reused,
\t\t)
\t}
\treturn func() {
\t\tsearch.ReleaseDirectedALT(g)
\t\tsearch.ReleaseMetricALT(g)
\t}, nil
}
'''
text = replace_once(text, old, new, "CLI preparation")
path.write_text(text)

Path("internal/search/directed_alt_test.go").write_text(r'''package search

import (
	"context"
	"testing"
)

func TestDirectedALTIsConsistentAndExact(t *testing.T) {
	g := gridGraph(t, 32, 32, true)
	g.EdgeCount = metricALTMinimumEdges
	if _, err := PrepareMetricALT(g, 8); err != nil {
		t.Fatal(err)
	}
	defer ReleaseMetricALT(g)
	preparation, err := PrepareDirectedALT(g)
	if err != nil {
		t.Fatal(err)
	}
	defer ReleaseDirectedALT(g)
	if preparation.Landmarks != 8 || preparation.Bytes == 0 {
		t.Fatalf("invalid preparation: %+v", preparation)
	}
	index, ok := directedALTForGraph(g)
	if !ok {
		t.Fatal("missing directed ALT index")
	}

	for source := 0; source < len(g.Nodes); source += 181 {
		for target := 7; target < len(g.Nodes); target += 193 {
			for from := range g.Nodes {
				hFrom := directedALTHeuristic(g, index, from, target)
				for _, edge := range g.OutEdges(from) {
					hTo := directedALTHeuristic(g, index, edge.To, target)
					if hFrom > edge.Cost && hFrom-edge.Cost > hTo {
						t.Fatalf("inconsistent heuristic on %d -> %d", from, edge.To)
					}
				}
			}
			want, err := Run(context.Background(), g, source, target, Dijkstra)
			if err != nil {
				t.Fatal(err)
			}
			got, err := Run(context.Background(), g, source, target, AStarDirectedALT)
			if err != nil {
				t.Fatal(err)
			}
			if got.Stats.Distance != want.Stats.Distance || got.Stats.OptimalityGap != 0 {
				t.Fatalf("%d -> %d: got=%+v want=%+v", source, target, got.Stats, want.Stats)
			}
		}
	}
}
''')

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
	"math/bits"
	"sync"
	"time"

	"github.com/lasder-ca/aegis-acbs/internal/graph"
)

const aStarDirectedALTPotentialModel = "astar-directed-alt-v2"

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

type directedALTQuery struct {
	index       *directedALTIndex
	target      int
	activeMask  uint16
	activeCount uint8
	fromTarget  [metricALTMaximumLandmarks]uint64
	toTarget    [metricALTMaximumLandmarks]uint64
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

func (index *directedALTIndex) query(source, target, limit int) directedALTQuery {
	count := len(index.landmarks)
	if limit <= 0 || limit > count {
		limit = count
	}
	type rankedLandmark struct {
		index uint8
		score uint64
	}
	var ranked [metricALTMaximumLandmarks]rankedLandmark
	rankedCount := 0
	fromSource := index.row(index.from, source)
	fromTarget := index.row(index.from, target)
	toSource := index.row(index.to, source)
	toTarget := index.row(index.to, target)
	query := directedALTQuery{
		index:       index,
		target:      target,
		activeCount: uint8(limit),
	}
	for i := 0; i < count; i++ {
		query.fromTarget[i] = fromTarget[i]
		query.toTarget[i] = toTarget[i]
		score := directedALTTerm(
			fromSource[i], fromTarget[i], toSource[i], toTarget[i],
		)
		position := rankedCount
		for position > 0 {
			previous := ranked[position-1]
			if previous.score > score ||
				(previous.score == score && previous.index < uint8(i)) {
				break
			}
			ranked[position] = previous
			position--
		}
		ranked[position] = rankedLandmark{index: uint8(i), score: score}
		rankedCount++
	}
	for i := 0; i < limit; i++ {
		query.activeMask |= uint16(1) << ranked[i].index
	}
	return query
}

func directedALTTerm(fromNode, fromTarget, toNode, toTarget uint64) uint64 {
	bound := uint64(0)
	if fromNode != inf && fromTarget != inf && fromTarget >= fromNode {
		bound = fromTarget - fromNode
	}
	if toNode != inf && toTarget != inf && toNode >= toTarget {
		if value := toNode - toTarget; value > bound {
			bound = value
		}
	}
	return bound
}

func (query directedALTQuery) lowerBound(node int) uint64 {
	fromNode := query.index.row(query.index.from, node)
	toNode := query.index.row(query.index.to, node)
	bound := uint64(0)
	mask := query.activeMask
	for mask != 0 {
		landmark := bits.TrailingZeros16(mask)
		mask &= mask - 1
		if value := directedALTTerm(
			fromNode[landmark],
			query.fromTarget[landmark],
			toNode[landmark],
			query.toTarget[landmark],
		); value > bound {
			bound = value
		}
	}
	return bound
}

func aStarDirectedALT(
	ctx context.Context, g *graph.Graph, source, target int,
) (Result, error) {
	return aStarDirectedALTWithLimit(
		ctx, g, source, target, AStarDirectedALT, 0,
	)
}

func aStarDirectedALTTop4(
	ctx context.Context, g *graph.Graph, source, target int,
) (Result, error) {
	return aStarDirectedALTWithLimit(
		ctx, g, source, target, AStarDirectedALTTop4, 4,
	)
}

func aStarDirectedALTTop2(
	ctx context.Context, g *graph.Graph, source, target int,
) (Result, error) {
	return aStarDirectedALTWithLimit(
		ctx, g, source, target, AStarDirectedALTTop2, 2,
	)
}

func aStarDirectedALTWithLimit(
	ctx context.Context, g *graph.Graph, source, target int,
	algorithm Algorithm, limit int,
) (Result, error) {
	if g.EdgeCount < metricALTMinimumEdges {
		result, err := dijkstra(ctx, g, source, target, true)
		result.Stats.Algorithm = algorithm
		return result, err
	}
	index, ok := directedALTForGraph(g)
	if !ok {
		return Result{}, errors.New("astar-directed-alt requires PrepareDirectedALT")
	}
	query := index.query(source, target, limit)

	n := len(g.Nodes)
	w := acquireSingleWorkspace(n)
	defer releaseSingleWorkspace(w)
	dist, prev := w.dist, w.prev
	w.touch(source)
	dist[source] = 0
	q := &w.q
	push(q, item{
		node: source, distance: 0,
		priority: directedALTHeuristic(g, query, source, target),
	})
	stats := Stats{
		Algorithm:           algorithm,
		QueuePushes:         1,
		PotentialModel:      aStarDirectedALTPotentialModel,
		PotentialLandmarks:  int(query.activeCount),
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
			h := directedALTHeuristic(g, query, edge.To, target)
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
	g *graph.Graph, query directedALTQuery, node, target int,
) uint64 {
	chord := heuristic(g, node, target, true)
	landmark := query.lowerBound(node)
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
    '\tAStarDirectedALT  Algorithm = "astar-directed-alt"\n',
    '\tAStarDirectedALT      Algorithm = "astar-directed-alt"\n'
    '\tAStarDirectedALTTop4  Algorithm = "astar-directed-alt-top4"\n'
    '\tAStarDirectedALTTop2  Algorithm = "astar-directed-alt-top2"\n',
    "constants",
)
text = replace_once(
    text,
    '''\tcase AStarDirectedALT:
\t\tr, err = aStarDirectedALT(ctx, g, source, target)
''',
    '''\tcase AStarDirectedALT:
\t\tr, err = aStarDirectedALT(ctx, g, source, target)
\tcase AStarDirectedALTTop4:
\t\tr, err = aStarDirectedALTTop4(ctx, g, source, target)
\tcase AStarDirectedALTTop2:
\t\tr, err = aStarDirectedALTTop2(ctx, g, source, target)
''',
    "dispatch",
)
path.write_text(text)

Path("internal/search/directed_alt_test.go").write_text(r'''package search

import (
	"context"
	"math/bits"
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

	algorithms := []Algorithm{
		AStarDirectedALT, AStarDirectedALTTop4, AStarDirectedALTTop2,
	}
	limits := []int{0, 4, 2}
	for source := 0; source < len(g.Nodes); source += 181 {
		for target := 7; target < len(g.Nodes); target += 193 {
			for i, algorithm := range algorithms {
				query := index.query(source, target, limits[i])
				if bits.OnesCount16(query.activeMask) != int(query.activeCount) {
					t.Fatalf("invalid active mask for %s", algorithm)
				}
				for from := range g.Nodes {
					hFrom := directedALTHeuristic(g, query, from, target)
					for _, edge := range g.OutEdges(from) {
						hTo := directedALTHeuristic(g, query, edge.To, target)
						if hFrom > edge.Cost && hFrom-edge.Cost > hTo {
							t.Fatalf("%s inconsistent on %d -> %d", algorithm, from, edge.To)
						}
					}
				}
			}

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
			}
		}
	}
}
''')

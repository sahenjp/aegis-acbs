package search

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

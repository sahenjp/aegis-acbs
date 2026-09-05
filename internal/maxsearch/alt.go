package maxsearch

import (
	"container/heap"
	"context"
	"errors"
	"math"
	"time"

	"github.com/lasder-ca/aegis-acbs/internal/graph"
	"github.com/lasder-ca/aegis-acbs/internal/search"
)

const ALT search.Algorithm = "alt"

const altInfinity = ^uint64(0)

type ALTRunner struct {
	graph        *graph.Graph
	landmarks    []int
	fromLandmark [][]uint64 // d(L, v)
	toLandmark   [][]uint64 // d(v, L), computed on the reverse graph
	preprocessNS int64
	tableBytes   uint64
}

// NewALTRunner builds exact directed ALT landmark tables. The preprocessing is
// graph-specific and reusable across arbitrarily many source/target queries.
// Landmark selection affects speed only; correctness comes from directed
// shortest-path triangle inequalities and does not depend on how landmarks are
// chosen.
func NewALTRunner(ctx context.Context, g *graph.Graph, landmarkCount int) (*ALTRunner, error) {
	if g == nil || len(g.Nodes) == 0 {
		return nil, errors.New("maxsearch: ALT requires a non-empty graph")
	}
	if landmarkCount <= 0 {
		return nil, errors.New("maxsearch: ALT landmark count must be positive")
	}
	if landmarkCount > 32 {
		return nil, errors.New("maxsearch: ALT landmark count is capped at 32")
	}
	if landmarkCount > len(g.Nodes) {
		landmarkCount = len(g.Nodes)
	}

	started := time.Now()
	landmarks := selectALTlandmarks(g, landmarkCount)
	from := make([][]uint64, len(landmarks))
	to := make([][]uint64, len(landmarks))
	for i, landmark := range landmarks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var err error
		from[i], err = altAllDistances(ctx, g, landmark, false)
		if err != nil {
			return nil, err
		}
		to[i], err = altAllDistances(ctx, g, landmark, true)
		if err != nil {
			return nil, err
		}
	}
	preprocessNS := time.Since(started).Nanoseconds()
	if preprocessNS < 1 {
		preprocessNS = 1
	}
	return &ALTRunner{
		graph:        g,
		landmarks:    landmarks,
		fromLandmark: from,
		toLandmark:   to,
		preprocessNS: preprocessNS,
		tableBytes:   uint64(len(landmarks)) * uint64(len(g.Nodes)) * 2 * 8,
	}, nil
}

func (r *ALTRunner) Name() search.Algorithm { return ALT }
func (r *ALTRunner) LandmarkCount() int      { return len(r.landmarks) }
func (r *ALTRunner) Landmarks() []int        { return append([]int(nil), r.landmarks...) }
func (r *ALTRunner) PreprocessDuration() time.Duration {
	return time.Duration(r.preprocessNS)
}
func (r *ALTRunner) DistanceTableBytes() uint64 { return r.tableBytes }

func (r *ALTRunner) Run(ctx context.Context, g *graph.Graph, source, target int) (search.Result, error) {
	if err := ctx.Err(); err != nil {
		return search.Result{}, err
	}
	if g != r.graph {
		return search.Result{}, errors.New("maxsearch: ALT runner used with a different graph instance")
	}
	if source < 0 || source >= len(g.Nodes) || target < 0 || target >= len(g.Nodes) {
		return search.Result{}, errors.New("maxsearch: source or target is out of range")
	}
	started := time.Now()
	if source == target {
		return search.Result{
			Path: []int{source},
			Stats: search.Stats{
				Algorithm: ALT, DurationNS: positiveDurationNS(started), Distance: 0,
				Reachable: true, PathNodes: 1,
			},
		}, nil
	}

	n := len(g.Nodes)
	dist := make([]uint64, n)
	prev := make([]int, n)
	for i := range dist {
		dist[i] = altInfinity
		prev[i] = -1
	}
	dist[source] = 0
	pq := &altPriorityQueue{}
	heap.Init(pq)
	heap.Push(pq, altQueueItem{node: source, distance: 0, priority: r.heuristic(source, target)})

	var expanded, relaxed, pushes, pops, stale uint64
	pushes = 1
	for pq.Len() > 0 {
		if expanded&1023 == 0 {
			if err := ctx.Err(); err != nil {
				return search.Result{}, err
			}
		}
		item := heap.Pop(pq).(altQueueItem)
		pops++
		if item.distance != dist[item.node] {
			stale++
			continue
		}
		expanded++
		if item.node == target {
			path := reconstructALTPath(prev, source, target)
			return search.Result{
				Path: path,
				Stats: search.Stats{
					Algorithm: ALT, DurationNS: positiveDurationNS(started),
					Expanded: expanded, Relaxed: relaxed, QueuePushes: pushes,
					QueuePops: pops, StalePops: stale, Distance: dist[target],
					Reachable: true, PathNodes: len(path),
				},
			}, nil
		}
		for _, edge := range g.OutEdges(item.node) {
			relaxed++
			candidate := altSaturatingAdd(item.distance, edge.Cost)
			if candidate >= dist[edge.To] {
				continue
			}
			dist[edge.To] = candidate
			prev[edge.To] = item.node
			priority := altSaturatingAdd(candidate, r.heuristic(edge.To, target))
			heap.Push(pq, altQueueItem{node: edge.To, distance: candidate, priority: priority})
			pushes++
		}
	}

	return search.Result{Stats: search.Stats{
		Algorithm: ALT, DurationNS: positiveDurationNS(started), Expanded: expanded,
		Relaxed: relaxed, QueuePushes: pushes, QueuePops: pops, StalePops: stale,
		Reachable: false,
	}}, nil
}

// heuristic is the directed ALT lower bound:
// max_L { d(L,t)-d(L,v), d(v,L)-d(t,L), 0 }.
func (r *ALTRunner) heuristic(v, target int) uint64 {
	var best uint64
	for i := range r.landmarks {
		from := r.fromLandmark[i]
		to := r.toLandmark[i]
		if from[target] != altInfinity && from[v] != altInfinity && from[target] >= from[v] {
			if bound := from[target] - from[v]; bound > best {
				best = bound
			}
		}
		if to[v] != altInfinity && to[target] != altInfinity && to[v] >= to[target] {
			if bound := to[v] - to[target]; bound > best {
				best = bound
			}
		}
	}
	return best
}

func selectALTlandmarks(g *graph.Graph, count int) []int {
	n := len(g.Nodes)
	if count >= n {
		out := make([]int, n)
		for i := range out {
			out[i] = i
		}
		return out
	}

	minLat, maxLat, minLon, maxLon := 0, 0, 0, 0
	for i := 1; i < n; i++ {
		node := g.Nodes[i]
		if node.Lat < g.Nodes[minLat].Lat {
			minLat = i
		}
		if node.Lat > g.Nodes[maxLat].Lat {
			maxLat = i
		}
		if node.Lon < g.Nodes[minLon].Lon {
			minLon = i
		}
		if node.Lon > g.Nodes[maxLon].Lon {
			maxLon = i
		}
	}

	out := make([]int, 0, count)
	seen := make(map[int]struct{}, count)
	add := func(v int) {
		if len(out) >= count || v < 0 || v >= n {
			return
		}
		if _, ok := seen[v]; ok {
			return
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	for _, v := range []int{minLat, maxLat, minLon, maxLon, 0, n - 1, n / 2} {
		add(v)
	}
	if count > 1 {
		for i := 0; i < count*2 && len(out) < count; i++ {
			add(int((uint64(i) * uint64(n-1)) / uint64(count*2-1)))
		}
	}
	for i := 0; len(out) < count; i++ {
		add(i)
	}
	return out
}

func altAllDistances(ctx context.Context, g *graph.Graph, source int, reverse bool) ([]uint64, error) {
	n := len(g.Nodes)
	dist := make([]uint64, n)
	for i := range dist {
		dist[i] = altInfinity
	}
	dist[source] = 0
	pq := &altPriorityQueue{}
	heap.Init(pq)
	heap.Push(pq, altQueueItem{node: source, distance: 0, priority: 0})
	var settled uint64
	for pq.Len() > 0 {
		if settled&4095 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		item := heap.Pop(pq).(altQueueItem)
		if item.distance != dist[item.node] {
			continue
		}
		settled++
		edges := g.OutEdges(item.node)
		if reverse {
			edges = g.InEdges(item.node)
		}
		for _, edge := range edges {
			candidate := altSaturatingAdd(item.distance, edge.Cost)
			if candidate >= dist[edge.To] {
				continue
			}
			dist[edge.To] = candidate
			heap.Push(pq, altQueueItem{node: edge.To, distance: candidate, priority: candidate})
		}
	}
	return dist, nil
}

func reconstructALTPath(prev []int, source, target int) []int {
	path := make([]int, 0, 16)
	for v := target; v != -1; v = prev[v] {
		path = append(path, v)
		if v == source {
			break
		}
	}
	if len(path) == 0 || path[len(path)-1] != source {
		return nil
	}
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path
}

func altSaturatingAdd(a, b uint64) uint64 {
	if a == altInfinity || b == altInfinity || a > math.MaxUint64-b {
		return altInfinity
	}
	return a + b
}

func positiveDurationNS(started time.Time) int64 {
	ns := time.Since(started).Nanoseconds()
	if ns < 1 {
		return 1
	}
	return ns
}

type altQueueItem struct {
	node     int
	distance uint64
	priority uint64
}

type altPriorityQueue []altQueueItem

func (p altPriorityQueue) Len() int { return len(p) }
func (p altPriorityQueue) Less(i, j int) bool {
	if p[i].priority == p[j].priority {
		if p[i].distance == p[j].distance {
			return p[i].node < p[j].node
		}
		return p[i].distance < p[j].distance
	}
	return p[i].priority < p[j].priority
}
func (p altPriorityQueue) Swap(i, j int) { p[i], p[j] = p[j], p[i] }
func (p *altPriorityQueue) Push(x any)   { *p = append(*p, x.(altQueueItem)) }
func (p *altPriorityQueue) Pop() any {
	old := *p
	n := len(old)
	x := old[n-1]
	*p = old[:n-1]
	return x
}

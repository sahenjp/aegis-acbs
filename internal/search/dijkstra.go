package search

import (
	"context"
	"math"

	"github.com/sahenjp/aegis-acbs/internal/graph"
)

const inf = ^uint64(0)

func dijkstra(ctx context.Context, g *graph.Graph, source, target int, useHeuristic bool) (Result, error) {
	n := len(g.Nodes)
	w := acquireSingleWorkspace(n)
	defer releaseSingleWorkspace(w)
	dist, prev := w.dist, w.prev

	w.touch(source)
	dist[source] = 0
	q := &w.q
	push(q, item{node: source, distance: 0, priority: heuristic(g, source, target, useHeuristic)})
	stats := Stats{Algorithm: Dijkstra, QueuePushes: 1}
	if useHeuristic {
		stats.Algorithm = AStar
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
		for _, e := range g.OutEdges(cur.node) {
			stats.Relaxed++
			if dist[cur.node] > inf-e.Cost {
				continue
			}
			nd := dist[cur.node] + e.Cost
			if nd < dist[e.To] {
				w.touch(e.To)
				dist[e.To] = nd
				prev[e.To] = cur.node
				priority := nd + heuristic(g, e.To, target, useHeuristic)
				if priority < nd {
					priority = inf
				}
				push(q, item{node: e.To, distance: nd, priority: priority})
				stats.QueuePushes++
			}
		}
	}
	if dist[target] == inf {
		return Result{Stats: stats}, nil
	}
	path := reconstruct(prev, source, target)
	stats.Distance, stats.Reachable, stats.PathNodes = dist[target], true, len(path)
	return Result{Path: path, Stats: stats}, nil
}

func heuristic(g *graph.Graph, node, target int, enabled bool) uint64 {
	if !enabled || g.MinCostPerMeter <= 0 {
		return 0
	}
	d := graph.HaversineMeters(g.Nodes[node].Lat, g.Nodes[node].Lon, g.Nodes[target].Lat, g.Nodes[target].Lon)
	v := d * g.MinCostPerMeter
	if v <= 0 {
		return 0
	}
	if v >= float64(math.MaxUint64) {
		return math.MaxUint64
	}
	// Floor preserves admissibility.
	return uint64(v)
}

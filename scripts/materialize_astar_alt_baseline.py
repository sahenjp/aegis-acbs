#!/usr/bin/env python3
from pathlib import Path


def replace_once(text: str, old: str, new: str, label: str) -> str:
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{label}: expected one match, found {count}")
    return text.replace(old, new, 1)


Path("internal/search/astar_alt.go").write_text(r'''package search

import (
	"context"

	"github.com/lasder-ca/aegis-acbs/internal/graph"
)

const aStarMetricALTPotentialModel = "astar-metric-alt-v1"

// aStarMetricALT is a reference implementation of the classical ALT idea:
// unidirectional A* guided by the maximum of the geographic lower bound and
// the undirected metric-landmark lower bound. It exists as an experimental
// baseline so ACBS-ALT is not credited for gains supplied by landmarks alone.
func aStarMetricALT(
	ctx context.Context, g *graph.Graph, source, target int,
) (Result, error) {
	if g.EdgeCount < metricALTMinimumEdges {
		result, err := dijkstra(ctx, g, source, target, true)
		result.Stats.Algorithm = AStarALT
		return result, err
	}
	index, ok := metricALTForGraph(g)
	if !ok {
		return Result{}, errMetricALTNotPrepared
	}
	query := index.query(source, target, 0)

	n := len(g.Nodes)
	w := acquireSingleWorkspace(n)
	defer releaseSingleWorkspace(w)
	dist, prev := w.dist, w.prev

	w.touch(source)
	dist[source] = 0
	q := &w.q
	push(q, item{
		node: source, distance: 0,
		priority: aStarALTHeuristic(g, query, source, target),
	})
	stats := Stats{
		Algorithm:           AStarALT,
		QueuePushes:         1,
		PotentialModel:      aStarMetricALTPotentialModel,
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
			h := aStarALTHeuristic(g, query, edge.To, target)
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
	stats.OptimalityGap = 0
	return Result{Path: path, Stats: stats}, nil
}

func aStarALTHeuristic(
	g *graph.Graph, query metricALTQuery, node, target int,
) uint64 {
	chord := heuristic(g, node, target, true)
	landmark, _ := query.bounds(node)
	if landmark > chord {
		return landmark
	}
	return chord
}
''')

path = Path("internal/search/metric_alt.go")
text = path.read_text()
text = replace_once(
    text,
    'const (\n\tmetricALTMinimumEdges',
    'var errMetricALTNotPrepared = errors.New("metric ALT index is not prepared")\n\nconst (\n\tmetricALTMinimumEdges',
    "shared error",
)
path.write_text(text)

path = Path("internal/search/acbs.go")
text = path.read_text()
text = replace_once(
    text,
    'return Result{}, errors.New("aegis-alt requires PrepareMetricALT for this graph")',
    'return Result{}, errMetricALTNotPrepared',
    "shared preparation error",
)
path.write_text(text)

path = Path("internal/search/search.go")
text = path.read_text()
text = replace_once(
    text,
    '\tAStar             Algorithm = "astar"\n',
    '\tAStar             Algorithm = "astar"\n\tAStarALT          Algorithm = "astar-alt"\n',
    "algorithm constant",
)
text = replace_once(
    text,
    '''\tcase AStar:
\t\tif g.MinCostPerMeter <= 0 {
\t\t\treturn Result{}, errors.New("A* requires coordinates and an admissible cost-per-meter bound")
\t\t}
\t\tr, err = dijkstra(ctx, g, source, target, true)
''',
    '''\tcase AStar:
\t\tif g.MinCostPerMeter <= 0 {
\t\t\treturn Result{}, errors.New("A* requires coordinates and an admissible cost-per-meter bound")
\t\t}
\t\tr, err = dijkstra(ctx, g, source, target, true)
\tcase AStarALT:
\t\tr, err = aStarMetricALT(ctx, g, source, target)
''',
    "dispatch",
)
path.write_text(text)

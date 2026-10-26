package search

import (
	"context"

	"github.com/sahenjp/aegis-acbs/internal/graph"
)

func bidirectionalDijkstra(ctx context.Context, g *graph.Graph, source, target int) (Result, error) {
	if source == target {
		return Result{Path: []int{source}, Stats: Stats{Algorithm: BiDijkstra, Reachable: true, PathNodes: 1}}, nil
	}
	n := len(g.Nodes)
	w := acquireBiWorkspace(n)
	defer releaseBiWorkspace(w)
	df, db := w.df, w.db
	pf, pb := w.pf, w.pb
	settledF, settledB := w.settledF, w.settledB

	w.touchForward(source)
	w.touchBackward(target)
	df[source], db[target] = 0, 0
	qf, qb := &w.qf, &w.qb
	radixPush(qf, item{node: source, distance: 0, priority: 0})
	radixPush(qb, item{node: target, distance: 0, priority: 0})
	best, meet := inf, -1
	stats := Stats{Algorithm: BiDijkstra, QueuePushes: 2}
	for qf.Len() > 0 && qb.Len() > 0 {
		if stats.Expanded&1023 == 0 {
			select {
			case <-ctx.Done():
				return Result{}, ctx.Err()
			default:
			}
		}
		frontF, _ := radixPeek(qf)
		frontB, _ := radixPeek(qb)
		minF := frontF.priority
		minB := frontB.priority
		if best != inf && minF <= best && minB <= best && minF >= best-minB {
			break
		}
		if minF <= minB {
			cur := radixPop(qf)
			stats.QueuePops++
			if cur.distance != df[cur.node] || settledF[cur.node] {
				stats.StalePops++
				continue
			}
			settledF[cur.node] = true
			stats.Expanded++
			if settledB[cur.node] && df[cur.node] <= inf-db[cur.node] && df[cur.node]+db[cur.node] < best {
				best = df[cur.node] + db[cur.node]
				meet = cur.node
			}
			for _, e := range g.OutEdges(cur.node) {
				stats.Relaxed++
				if df[cur.node] > inf-e.Cost {
					continue
				}
				nd := df[cur.node] + e.Cost
				if nd < df[e.To] {
					w.touchForward(e.To)
					df[e.To] = nd
					pf[e.To] = int32(cur.node)
					radixPush(qf, item{node: e.To, distance: nd, priority: nd})
					stats.QueuePushes++
				}
				if db[e.To] != inf && nd <= inf-db[e.To] && nd+db[e.To] < best {
					best = nd + db[e.To]
					meet = e.To
				}
			}
		} else {
			cur := radixPop(qb)
			stats.QueuePops++
			if cur.distance != db[cur.node] || settledB[cur.node] {
				stats.StalePops++
				continue
			}
			settledB[cur.node] = true
			stats.Expanded++
			if settledF[cur.node] && df[cur.node] <= inf-db[cur.node] && df[cur.node]+db[cur.node] < best {
				best = df[cur.node] + db[cur.node]
				meet = cur.node
			}
			for _, e := range g.InEdges(cur.node) {
				stats.Relaxed++
				if db[cur.node] > inf-e.Cost {
					continue
				}
				nd := db[cur.node] + e.Cost
				if nd < db[e.To] {
					w.touchBackward(e.To)
					db[e.To] = nd
					pb[e.To] = int32(cur.node)
					radixPush(qb, item{node: e.To, distance: nd, priority: nd})
					stats.QueuePushes++
				}
				if df[e.To] != inf && nd <= inf-df[e.To] && nd+df[e.To] < best {
					best = nd + df[e.To]
					meet = e.To
				}
			}
		}
	}
	if best == inf || meet < 0 {
		return Result{Stats: stats}, nil
	}
	path := reconstructBidirectional(pf, pb, source, meet, target)
	if len(path) == 0 {
		return Result{Stats: stats}, nil
	}
	stats.Distance = best
	stats.Reachable = true
	stats.PathNodes = len(path)
	return Result{Path: path, Stats: stats}, nil
}

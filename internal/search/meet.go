package search

import (
	"context"

	"github.com/sahenjp/aegis-acbs/internal/graph"
)

// Meet is the MEET (Meetable-Endpoints Exact Termination) algorithm: an exact
// bidirectional Dijkstra that always advances the frontier with the smaller
// lower bound and stops as soon as the two frontier lower bounds together
// reach the incumbent.
const Meet Algorithm = "meet"

// meet runs MEET, an exact bidirectional search.
//
// Compared with bidirectionalDijkstra it changes three things and keeps the same
// result:
//
//   - Scheduler. The frontier with the smaller next key is advanced. Each
//     direction still pops in non-decreasing key order (all edge costs are
//     non-negative, and both queues are radix heaps on the plain distance), so
//     no directional label is ever settled out of order. This alone is safe.
//   - Termination. The loop stops when mu <= kF + kB, where kF and kB are the
//     current minimum keys of the forward and backward frontiers. This is the
//     standard admissible bidirectional bound: any path of cost d* < mu must
//     contribute an edge settled by both directions, and such an overlap
//     yields a candidate <= d* that is recorded before the bound is reached.
//   - Pop cut-pruning. A settled label with g >= mu is not relaxed, because
//     every route through it already costs at least g >= mu and therefore
//     cannot beat the incumbent.
//
// Correctness note. An earlier revision also pruned relaxations using a single
// global "smallest overlap seen so far" value. That is NOT admissible: the
// minimum overlap may belong to a different node than the candidate, so
// subtracting it prunes routes that the candidate could still improve on, and
// the search returned over-estimates on a 4-node graph. The pruning was removed
// rather than patched; see TestMEETMatchesDijkstraExhaustiveSmallDirected,
// which reproduces that topology. Only mu >= achieved-upper-bound comparisons
// are used for pruning now.
//
// Every recorded candidate is a real path (a forward label plus a backward
// label over the same node), and every candidate is only recorded when it
// strictly improves mu, so the returned distance is an achievable path length
// that is never undercut by an unexamined route. That makes the result exactly
// equal to Dijkstra's.
func meet(ctx context.Context, g *graph.Graph, source, target int) (Result, error) {
	if source == target {
		return Result{Path: []int{source}, Stats: Stats{Algorithm: Meet, Reachable: true, PathNodes: 1}}, nil
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
	stats := Stats{Algorithm: Meet, QueuePushes: 2}
	lastDirection := byte('F')

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
		// Exact bidirectional termination: no route can still beat an incumbent
		// that is already bounded by the sum of the two frontier minima.
		if best != inf && minF >= best-minB {
			break
		}
		// Advance the smaller lower bound; on an exact tie keep the previous
		// direction so the scheduler does not oscillate on equal keys.
		forward := minF < minB || (minF == minB && lastDirection == 'F')
		if forward {
			lastDirection = 'F'
			cur := radixPop(qf)
			stats.QueuePops++
			if cur.distance != df[cur.node] || settledF[cur.node] {
				stats.StalePops++
				continue
			}
			settledF[cur.node] = true
			stats.Expanded++
			stats.ForwardExpanded++
			// Cut-pruning at pop: gF >= mu can only lead to routes that are no
			// better than the incumbent already achieved.
			if cur.distance >= best {
				stats.PrunedAtPop++
				continue
			}
			if other := db[cur.node]; other != inf && cur.distance <= inf-other {
				if candidate := cur.distance + other; candidate < best {
					best = candidate
					meet = cur.node
				}
			}
			for _, e := range g.OutEdges(cur.node) {
				stats.Relaxed++
				if cur.distance > inf-e.Cost {
					continue
				}
				nd := cur.distance + e.Cost
				if nd < df[e.To] {
					w.touchForward(e.To)
					df[e.To] = nd
					pf[e.To] = int32(cur.node)
					radixPush(qf, item{node: e.To, distance: nd, priority: nd})
					stats.QueuePushes++
				}
				if other := db[e.To]; other != inf && nd <= inf-other && nd+other < best {
					best = nd + other
					meet = e.To
				}
			}
		} else {
			lastDirection = 'B'
			cur := radixPop(qb)
			stats.QueuePops++
			if cur.distance != db[cur.node] || settledB[cur.node] {
				stats.StalePops++
				continue
			}
			settledB[cur.node] = true
			stats.Expanded++
			stats.BackwardExpanded++
			if cur.distance >= best {
				stats.PrunedAtPop++
				continue
			}
			if other := df[cur.node]; other != inf && cur.distance <= inf-other {
				if candidate := cur.distance + other; candidate < best {
					best = candidate
					meet = cur.node
				}
			}
			for _, e := range g.InEdges(cur.node) {
				stats.Relaxed++
				if cur.distance > inf-e.Cost {
					continue
				}
				nd := cur.distance + e.Cost
				if nd < db[e.To] {
					w.touchBackward(e.To)
					db[e.To] = nd
					pb[e.To] = int32(cur.node)
					radixPush(qb, item{node: e.To, distance: nd, priority: nd})
					stats.QueuePushes++
				}
				if other := df[e.To]; other != inf && nd <= inf-other && nd+other < best {
					best = nd + other
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
	stats.UpperBound = best
	return Result{Path: path, Stats: stats}, nil
}

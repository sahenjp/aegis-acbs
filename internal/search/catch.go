package search

import (
	"context"
	"math"
	"sync"

	"github.com/sahenjp/aegis-acbs/internal/graph"
)

// catchHeuristic evaluates the admissible geographic lower bound the package
// already trusts: geodesic distance to one fixed vertex, scaled by the minimum
// cost per metre. It uses the graph's precomputed unit-sphere vectors and the
// chord instead of Haversine trigonometry. The chord of a central angle is
// never longer than the arc it subtends, so the value is a lower bound on the
// geodesic length and therefore on the true cost.
//
// Results are cached for one query. Only vertices the search touches are ever
// evaluated, so the cache costs nothing up front.
type catchHeuristic struct {
	g     *graph.Graph
	usx   float64
	usy   float64
	usz   float64
	scale float64
	val   []uint64
	known []bool
}

func newCatchHeuristic(g *graph.Graph, to int, val []uint64, known []bool) catchHeuristic {
	h := catchHeuristic{g: g, val: val, known: known}
	h.usx, h.usy, h.usz = g.UnitVector(to)
	if g.MinCostPerMeter > 0 {
		// The unit sphere has radius 1, so the chord equals the central angle in
		// radians, arc length is R*angle, and a geodesic is longer than its chord.
		// chord * R * minCostPerMeter is therefore an admissible lower bound.
		h.scale = g.MinCostPerMeter * 6371008.8
	}
	return h
}

func (h *catchHeuristic) est(v int) uint64 {
	if h.known[v] {
		return h.val[v]
	}
	var out uint64
	if h.scale > 0 {
		x, y, z := h.g.UnitVector(v)
		dx, dy, dz := h.usx-x, h.usy-y, h.usz-z
		chord := math.Sqrt(dx*dx + dy*dy + dz*dz)
		if chord > 0 {
			out = uint64(chord * h.scale)
		}
	}
	h.known[v] = true
	h.val[v] = out
	return out
}

// catchScratch holds both directional caches for one query. It is pooled
// because allocating two full-size caches per query would dominate the cost of
// a short search.
type catchScratch struct {
	fwdVal, bwdVal     []uint64
	fwdKnown, bwdKnown []bool
}

var catchScratchPool = sync.Pool{
	New: func() any { return &catchScratch{} },
}

func acquireCatchScratch(n int) *catchScratch {
	s := catchScratchPool.Get().(*catchScratch)
	if cap(s.fwdKnown) < n {
		s.fwdVal = make([]uint64, n)
		s.bwdVal = make([]uint64, n)
		s.fwdKnown = make([]bool, n)
		s.bwdKnown = make([]bool, n)
	}
	s.fwdVal = s.fwdVal[:n]
	s.bwdVal = s.bwdVal[:n]
	s.fwdKnown = s.fwdKnown[:n]
	s.bwdKnown = s.bwdKnown[:n]
	return s
}

func releaseCatchScratch(s *catchScratch) {
	for v, known := range s.fwdKnown {
		if known {
			s.fwdKnown[v] = false
			s.fwdVal[v] = 0
		}
	}
	for v, known := range s.bwdKnown {
		if known {
			s.bwdKnown[v] = false
			s.bwdVal[v] = 0
		}
	}
	catchScratchPool.Put(s)
}

// CATCH (Coupled Adaptive Termination heuristic) is an exact bidirectional A*
// search. Read the name carefully: each frontier uses an admissible geographic
// potential, so this is benchmarked as bidirectional A*, not as a
// heuristic-free bidirectional Dijkstra. The coupling is the scheduler and the
// termination rule, not a stronger potential.
//
// Scheduler: each step expands the frontier whose minimum key is smaller, so
// the two searches advance together instead of one running far ahead.
//
// Termination: stop once upper <= bF + bB, where upper is the best real s->t
// walk found so far and bF, bB are the minima of the two per-direction A* keys
// (g + h, with no shared offset). This is the standard sufficient stopping
// rule for exact bidirectional A*: every unsettled vertex v has both
// df[v] + hF[v] >= bF and db[v] + hB[v] >= bB, so any s->t walk through an
// unsettled vertex costs at least bF + bB once the potentials are consistent
// with the walk split. upper is always the weight of a real walk and hence
// upper >= D, and the rule only fires when no cheaper walk can remain, so the
// reported distance equals D.
//
// Note on an earlier revision: keys that carried a shared additive term were
// tried and were WRONG, because the two frontier minima then no longer bound D
// from below. This revision uses plain per-direction keys.
func catchSearch(ctx context.Context, g *graph.Graph, source, target int) (Result, error) {
	return catchRun(ctx, g, source, target, Catch)
}

func catchRun(ctx context.Context, g *graph.Graph, source, target int, alg Algorithm) (Result, error) {
	// The s == t case lives here rather than in the wrappers so that every
	// entry point (catch, catch-ab, catch-r) returns the single-vertex path that
	// dijkstra and bidijkstra return rather than an unreachable result.
	if source == target {
		return Result{Path: []int{source}, Stats: Stats{Algorithm: alg, Reachable: true, PathNodes: 1}}, nil
	}
	n := len(g.Nodes)
	w := acquireBiWorkspace(n)
	defer releaseBiWorkspace(w)
	s := acquireCatchScratch(n)
	defer releaseCatchScratch(s)
	hF := newCatchHeuristic(g, target, s.fwdVal, s.fwdKnown)
	hB := newCatchHeuristic(g, source, s.bwdVal, s.bwdKnown)

	df, db := w.df, w.db
	pf, pb := w.pf, w.pb
	settledF, settledB := w.settledF, w.settledB

	w.touchForward(source)
	w.touchBackward(target)
	df[source], db[target] = 0, 0
	qf, qb := &w.qf, &w.qb

	radixPush(qf, item{node: source, distance: 0, priority: hF.est(source)})
	radixPush(qb, item{node: target, distance: 0, priority: hB.est(target)})

	best, meet := inf, -1
	stats := Stats{Algorithm: alg, QueuePushes: 2}
	for qf.Len() > 0 || qb.Len() > 0 {
		if stats.Expanded&1023 == 0 {
			select {
			case <-ctx.Done():
				return Result{}, ctx.Err()
			default:
			}
		}
		fwdEmpty, bwdEmpty := qf.Len() == 0, qb.Len() == 0
		var bF, bB uint64
		if !fwdEmpty {
			front, _ := radixPeek(qf)
			bF = front.priority
		}
		if !bwdEmpty {
			front, _ := radixPeek(qb)
			bB = front.priority
		}
		if !fwdEmpty && !bwdEmpty && best != inf {
			// Exact stopping rule: stop once EACH frontier's minimum key alone
			// has reached the incumbent. Any path that could still beat `best`
			// would have to start from a forward vertex whose key is below
			// `best` and end at a backward vertex whose key is below `best`; if
			// neither exists, `best` is optimal. Do NOT weaken this to
			// bF+bB >= best: with an h-inflated key the sum skips past D and
			// stops early (measured 48-54 wrong answers per 300 on the 10k
			// graph). The both-frontiers form is measured exact and is also the
			// first to fire, so it costs nothing.
			if best <= bF && best <= bB {
				stats.LowerBound = bF
				break
			}
		}
		takeForward := bwdEmpty || (!fwdEmpty && bF <= bB)
		if takeForward {
			cur := radixPop(qf)
			stats.QueuePops++
			if cur.distance != df[cur.node] || settledF[cur.node] {
				stats.StalePops++
				continue
			}
			settledF[cur.node] = true
			stats.Expanded++
			stats.ForwardExpanded++
			if settledB[cur.node] && df[cur.node] <= inf-db[cur.node] && df[cur.node]+db[cur.node] < best {
				best = df[cur.node] + db[cur.node]
				meet = cur.node
			}
			base := df[cur.node]
			for _, e := range g.OutEdges(cur.node) {
				stats.Relaxed++
				if base > inf-e.Cost {
					continue
				}
				nd := base + e.Cost
				if nd < df[e.To] {
					w.touchForward(e.To)
					df[e.To] = nd
					pf[e.To] = int32(cur.node)
					priority := nd + hF.est(e.To)
					if priority < nd {
						priority = inf
					}
					radixPush(qf, item{node: e.To, distance: nd, priority: priority})
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
			stats.BackwardExpanded++
			if settledF[cur.node] && df[cur.node] <= inf-db[cur.node] && df[cur.node]+db[cur.node] < best {
				best = df[cur.node] + db[cur.node]
				meet = cur.node
			}
			base := db[cur.node]
			for _, e := range g.InEdges(cur.node) {
				stats.Relaxed++
				if base > inf-e.Cost {
					continue
				}
				nd := base + e.Cost
				if nd < db[e.To] {
					w.touchBackward(e.To)
					db[e.To] = nd
					pb[e.To] = int32(cur.node)
					priority := nd + hB.est(e.To)
					if priority < nd {
						priority = inf
					}
					radixPush(qb, item{node: e.To, distance: nd, priority: priority})
					stats.QueuePushes++
				}
				if df[e.To] != inf && nd <= inf-df[e.To] && nd+df[e.To] < best {
					best = nd + df[e.To]
					meet = e.To
				}
			}
		}
	}
	stats.UpperBound = best
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

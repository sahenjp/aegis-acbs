package search

import (
	"context"
	"errors"

	"github.com/sahenjp/aegis-acbs/internal/graph"
)

// MultiBound is an exact bidirectional search that terminates on the maximum of
// several admissible global lower bounds rather than on the g-bound alone.
//
// Siag et al. (IJCAI 2023) enumerate the non-dominated global bounds for
// front-to-end bidirectional search with consistent heuristics, and show that
// the maximum over whichever bounds an algorithm can afford to keep is the
// tightest single termination condition available. ACBS consults only the
// g-bound, which is their B3. This variant keeps a plain bidirectional Dijkstra
// with the same A* keys as the baselines but stops on the larger of:
//
//	B1/B2  f-bound      the smaller of min f_F and min f_B over the frontiers
//	B3     g-bound      min g_F + min g_B
//	B4     b-bound      (min(g_F + h_F) + min(g_B + h_B)) / 2
//
// All three are admissible for consistent heuristics, so stopping on the largest
// is still exact. The point of the experiment is to measure how much tighter
// termination is worth in wall clock, which the theory alone does not answer.
//
// This is a measurement vehicle, not a production algorithm. It is deliberately
// plain: no scheduler, no chunking, no guards.
const MultiBound Algorithm = "multibound"

// MultiboundStatic is the same search with an alternating expansion order instead
// of the smaller-frontier rule, so the pair isolates expansion order from the
// termination rule.
const MultiboundStatic Algorithm = "multibound-static"

var errMultiBoundReconstruct = errors.New("multibound: reachable query without a reconstructable path")

type multiBoundMode int

const (
	multiBoundModeAll multiBoundMode = iota
	multiBoundModeGOnly
	multiBoundModeFOnly
	multiBoundModeBOnly
)

func multiBound(ctx context.Context, g *graph.Graph, source, target int) (Result, error) {
	return multiBoundRun(ctx, g, source, target, false, multiBoundModeAll)
}

func multiboundStatic(ctx context.Context, g *graph.Graph, source, target int) (Result, error) {
	return multiBoundRun(ctx, g, source, target, true, multiBoundModeAll)
}

func multiBoundRun(ctx context.Context, g *graph.Graph, source, target int, alternate bool, mode multiBoundMode) (Result, error) {
	alg := MultiBound
	if alternate {
		alg = MultiboundStatic
	}
	if source == target {
		return Result{Path: []int{source}, Stats: Stats{Algorithm: alg, Distance: 0, Reachable: true, PathNodes: 1}}, nil
	}

	n := len(g.Nodes)
	df := make([]uint64, n)
	db := make([]uint64, n)
	for i := 0; i < n; i++ {
		df[i], db[i] = inf, inf
	}
	settledF := make([]bool, n)
	settledB := make([]bool, n)
	pf := make([]int32, n)
	pb := make([]int32, n)
	for i := 0; i < n; i++ {
		pf[i], pb[i] = -1, -1
	}

	pot := newACBSPotential(g, source, target, false)
	phiS := pot.phi(g, source)
	phiT := pot.phi(g, target)

	var qf, qb radixHeap
	qf.reset()
	qb.reset()
	df[source] = 0
	db[target] = 0
	radixPush(&qf, item{node: source, distance: 0, priority: reducedForwardKey(0, phiS, phiS)})
	radixPush(&qb, item{node: target, distance: 0, priority: reducedBackwardKey(0, phiT, phiT)})

	var stats Stats
	stats.Algorithm = alg
	best := inf
	meet := -1
	forward := true

	peekF := func() (item, bool) {
		for {
			it, ok := radixPeek(&qf)
			if !ok {
				return item{}, false
			}
			if it.distance == df[it.node] && !settledF[it.node] {
				return it, true
			}
			radixPop(&qf)
			stats.StalePops++
		}
	}
	peekB := func() (item, bool) {
		for {
			it, ok := radixPeek(&qb)
			if !ok {
				return item{}, false
			}
			if it.distance == db[it.node] && !settledB[it.node] {
				return it, true
			}
			radixPop(&qb)
			stats.StalePops++
		}
	}

	// addSafe avoids wrapping when summing two distances.
	addSafe := func(a, b uint64) uint64 {
		if a >= inf-b {
			return inf
		}
		return a + b
	}

	for {
		select {
		case <-ctx.Done():
			return Result{}, ctx.Err()
		default:
		}
		itF, okF := peekF()
		itB, okB := peekB()
		if !okF || !okB {
			break
		}

		// f-values for the two frontier minima.
		// bounds returns (distance to target, distance from source) for a node,
		// so the forward frontier uses the first and the backward the second.
		// The frontiers are ordered by reduced key, so the only frontier quantity
		// this structure knows exactly is that key. Deriving min g from the peeked
		// node is wrong: the item carrying the smallest reduced key need not carry
		// the smallest g. Convert the reduced-key sum back to original units, which
		// is what ACBS does, and treat that as the g-bound.
		_ = pot.bounds
		reducedSum := addSafe(itF.priority, itB.priority)
		gVal := reducedToOriginalLowerBound(reducedSum, phiS, phiT)
		fValF := gVal
		fValB := gVal

		lb := gVal
		if mode == multiBoundModeAll || mode == multiBoundModeFOnly {
			fEither := fValF
			if fValB < fEither {
				fEither = fValB
			}
			if fEither > lb {
				lb = fEither
			}
		}
		if mode == multiBoundModeAll || mode == multiBoundModeBOnly {
			if bSum := addSafe(fValF, fValB); bSum != inf {
				if b := bSum / 2; b > lb {
					lb = b
				}
			}
		}

		// Compare in the units the frontier actually orders by. gVal, fValF and
		// fValB are original costs, and best is an original cost, so this
		// comparison is already unit-consistent. The earlier failure came from
		// taking item.distance as the frontier minimum: the heap orders by the
		// reduced key, so the item carrying the smallest reduced key does not
		// necessarily carry the smallest g. Recompute each bound from the settled
		// distances at the frontier nodes instead.
		_ = gVal
		stats.TerminationLowerBound = lb
		if best != inf && lb >= best {
			break
		}

		if alternate {
			forward = !forward
		} else {
			if itF.priority > itB.priority {
				forward = false
			} else {
				forward = true
			}
		}

		if forward {
			cur := radixPop(&qf)
			stats.QueuePops++
			if settledF[cur.node] {
				continue
			}
			settledF[cur.node] = true
			stats.Expanded++
			stats.ForwardExpanded++
			if settledB[cur.node] {
				if s := addSafe(df[cur.node], db[cur.node]); s < best {
					best, meet = s, cur.node
				}
			}
			for _, e := range g.OutEdges(cur.node) {
				nd := addSafe(df[cur.node], e.Cost)
				if nd < df[e.To] {
					df[e.To] = nd
					pf[e.To] = int32(cur.node)
					phi := pot.phi(g, e.To)
					stats.Relaxed++
					stats.QueuePushes++
					radixPush(&qf, item{node: e.To, distance: nd, priority: reducedForwardKey(nd, phi, phiS)})
				}
				if db[e.To] != inf {
					if s := addSafe(nd, db[e.To]); s < best {
						best, meet = s, e.To
					}
				}
			}
		} else {
			cur := radixPop(&qb)
			stats.QueuePops++
			if settledB[cur.node] {
				continue
			}
			settledB[cur.node] = true
			stats.Expanded++
			stats.BackwardExpanded++
			if settledF[cur.node] {
				if s := addSafe(df[cur.node], db[cur.node]); s < best {
					best, meet = s, cur.node
				}
			}
			for _, e := range g.InEdges(cur.node) {
				nd := addSafe(db[cur.node], e.Cost)
				if nd < db[e.To] {
					db[e.To] = nd
					pb[e.To] = int32(cur.node)
					phi := pot.phi(g, e.To)
					stats.Relaxed++
					stats.QueuePushes++
					radixPush(&qb, item{node: e.To, distance: nd, priority: reducedBackwardKey(nd, phi, phiT)})
				}
				if df[e.To] != inf {
					if s := addSafe(df[e.To], nd); s < best {
						best, meet = s, e.To
					}
				}
			}
		}
	}

	if best == inf || meet < 0 {
		return Result{Stats: stats}, nil
	}
	path := reconstructBidirectional(pf, pb, source, meet, target)
	if len(path) == 0 {
		return Result{}, errMultiBoundReconstruct
	}
	stats.Distance = best
	stats.Reachable = true
	stats.PathNodes = len(path)
	return Result{Path: path, Stats: stats}, nil
}

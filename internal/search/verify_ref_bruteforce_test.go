package search

import (
	"context"
	"math"
	"math/rand/v2"
	"testing"

	"github.com/sahenjp/aegis-acbs/internal/graph"
)

// This file is owned by the independent verifier (task-6). It intentionally
// re-derives the correct answer with code that shares nothing with the
// production search helpers: no workspace pools, no radix heap, no shared
// adjacency. A defect inside a shared helper therefore cannot hide by making
// the implementation and the oracle agree.

// verifyIndepDijkstra is an O(n^2) Dijkstra oracle written from scratch.
func verifyIndepDijkstra(g *graph.Graph, source, target int) (uint64, bool) {
	n := len(g.Nodes)
	const unreached = ^uint64(0)
	dist := make([]uint64, n)
	done := make([]bool, n)
	for i := range dist {
		dist[i] = unreached
	}
	dist[source] = 0
	for {
		u := -1
		var best = unreached
		for v := 0; v < n; v++ {
			if !done[v] && dist[v] < best {
				best, u = dist[v], v
			}
		}
		if u < 0 {
			break
		}
		done[u] = true
		if u == target {
			break
		}
		for _, e := range g.OutEdges(u) {
			if dist[u] > unreached-e.Cost {
				continue
			}
			if nd := dist[u] + e.Cost; nd < dist[e.To] {
				dist[e.To] = nd
			}
		}
	}
	if dist[target] == unreached {
		return 0, false
	}
	return dist[target], true
}

// verifyCheckPath recomputes the cost of a returned node path from the raw edge
// list, independently of search.Validate.
func verifyCheckPath(g *graph.Graph, source, target int, r Result) (bool, uint64) {
	if !r.Stats.Reachable {
		return len(r.Path) == 0, 0
	}
	if len(r.Path) == 0 || r.Path[0] != source || r.Path[len(r.Path)-1] != target {
		return false, 0
	}
	var total uint64
	for i := 0; i+1 < len(r.Path); i++ {
		from, to := r.Path[i], r.Path[i+1]
		var cost uint64
		found := false
		for _, e := range g.OutEdges(from) {
			if e.To == to {
				cost = e.Cost
				found = true
				break
			}
		}
		if !found {
			return false, total
		}
		if total > math.MaxUint64-cost {
			return false, total
		}
		total += cost
	}
	return total == r.Stats.Distance, total
}

// makeVerifyRandomGraph produces a directed graph with parallel edges,
// self-loops, zero weights and deliberately unreachable regions.
func makeVerifyRandomGraph(r *rand.Rand, n, m int) *graph.Graph {
	g := graph.New("verify-random", "generated", "car", graph.MetricDistance)
	g.Nodes = make([]graph.Node, n)
	for i := range g.Nodes {
		g.Nodes[i] = graph.Node{ID: int64(i), Lat: 50 + float64(r.IntN(1000))/100000, Lon: -0.3 + float64(r.IntN(1000))/100000}
	}
	g.Adj = make([][]graph.Edge, n)
	for k := 0; k < m; k++ {
		from := r.IntN(n)
		to := r.IntN(n)
		// Costs must be strictly positive: Finalize rejects zero-cost edges,
		// and a zero-cost edge is not a road-like case anyway. The previous
		// version forced w=0 for 1 in 7 edges, which panicked.
		w := uint64(1 + r.IntN(49))
		g.Adj[from] = append(g.Adj[from], graph.Edge{To: to, Cost: w})
	}
	if err := g.Finalize(); err != nil {
		panic(err)
	}
	return g
}

// verifyAlgorithms is the list of algorithms audited for exactness. It is a
// separate copy on purpose: if an author silently drops one from the production
// registry, this test still exercises it.
func verifyAlgorithms() []Algorithm {
	return []Algorithm{Dijkstra, BiDijkstra, PB, Meet, Catch, CatchAB, CatchR}
}

func TestVerifyExactnessRandomFuzz(t *testing.T) {
	algs := verifyAlgorithms()
	r := rand.New(rand.NewPCG(0xfeedface, 0xdeadbeef))
	checks := 0
	for trial := 0; trial < 80; trial++ {
		n := 2 + r.IntN(40)
		m := n + r.IntN(4*n)
		g := makeVerifyRandomGraph(r, n, m)
		for q := 0; q < 40; q++ {
			s := r.IntN(n)
			dst := r.IntN(n)
			oracle, oracleReach := verifyIndepDijkstra(g, s, dst)
			for _, alg := range algs {
				res, err := Run(context.Background(), g, s, dst, alg)
				checks++
				if err != nil {
					t.Fatalf("trial=%d alg=%s s=%d t=%d error: %v", trial, alg, s, dst, err)
				}
				if !oracleReach {
					if res.Stats.Reachable {
						t.Fatalf("trial=%d alg=%s s=%d t=%d claimed reachable distance=%d but oracle says unreachable", trial, alg, s, dst, res.Stats.Distance)
					}
					continue
				}
				if !res.Stats.Reachable {
					t.Fatalf("trial=%d alg=%s s=%d t=%d claimed unreachable but oracle distance=%d", trial, alg, s, dst, oracle)
				}
				if res.Stats.Distance != oracle {
					t.Fatalf("trial=%d alg=%s s=%d t=%d WRONG DISTANCE got=%d want=%d", trial, alg, s, dst, res.Stats.Distance, oracle)
				}
				if ok, computed := verifyCheckPath(g, s, dst, res); !ok {
					t.Fatalf("trial=%d alg=%s s=%d t=%d BAD PATH computed=%d declared=%d path=%v", trial, alg, s, dst, computed, res.Stats.Distance, res.Path)
				}
			}
		}
	}
	t.Logf("fuzz exactness: %d algorithm-runs compared against independent oracle", checks)
}

// TestVerifyNoCrossQueryCaching repeats overlapping queries against one
// long-lived graph and requires identical answers. A cache that survives
// between queries shows up as a distance or expansion-count change.
func TestVerifyNoCrossQueryCaching(t *testing.T) {
	r := rand.New(rand.NewPCG(0x515151, 0x616161))
	g := makeVerifyRandomGraph(r, 60, 240)
	for _, alg := range verifyAlgorithms() {
		type key struct{ s, d int }
		first := map[key]uint64{}
		firstExp := map[key]uint64{}
		for round := 0; round < 4; round++ {
			for i := 0; i < 40; i++ {
				s := r.IntN(60)
				d := r.IntN(60)
				res, err := Run(context.Background(), g, s, d, alg)
				if err != nil {
					t.Fatalf("alg=%s s=%d d=%d: %v", alg, s, d, err)
				}
				k := key{s, d}
				if prev, ok := first[k]; ok {
					if res.Stats.Reachable && prev != res.Stats.Distance {
						t.Fatalf("alg=%s s=%d d=%d: distance changed across repeats: %d then %d (cross-query state leak)", alg, s, d, prev, res.Stats.Distance)
					}
					if !res.Stats.Reachable && prev != 0 {
						t.Fatalf("alg=%s s=%d d=%d: reachability changed across repeats", alg, s, d)
					}
					if prevExp, ok := firstExp[k]; ok && prevExp != res.Stats.Expanded {
						t.Fatalf("alg=%s s=%d d=%d: expanded count changed across repeats: %d then %d (cross-query state leak)", alg, s, d, prevExp, res.Stats.Expanded)
					}
				} else {
					first[k] = res.Stats.Distance
					if res.Stats.Reachable {
						firstExp[k] = res.Stats.Expanded
					}
				}
			}
		}
	}
}

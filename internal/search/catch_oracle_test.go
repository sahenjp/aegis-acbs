package search

import (
	"context"
	"fmt"
	"testing"

	"github.com/lasder-ca/aegis-acbs/internal/graph"
)

// oracleGraph builds a small random digraph and its all-pairs distances by
// Floyd-Warshall, which is independent of every search algorithm in the
// package. Used as the ground truth for the CATCH exactness tests.
func oracleGraph(t *testing.T, r *catchRand, n int) (*graph.Graph, [][]uint64) {
	t.Helper()
	g := graph.New("oracle", "", "car", graph.MetricDistance)
	g.Nodes = make([]graph.Node, n)
	g.Adj = make([][]graph.Edge, n)
	for i := 0; i < n; i++ {
		g.Nodes[i] = graph.Node{ID: int64(i + 1), Lat: 35 + float64(i)*0.001, Lon: 139 + float64(r.intn(1000))*0.0001}
	}
	for u := 0; u < n; u++ {
		for v := 0; v < n; v++ {
			if u == v || r.intn(3) != 0 {
				continue
			}
			g.Adj[u] = append(g.Adj[u], graph.Edge{To: v, Cost: uint64(1 + r.intn(40))})
		}
	}
	if err := g.Finalize(); err != nil {
		t.Fatal(err)
	}
	dist := make([][]uint64, n)
	for i := range dist {
		dist[i] = make([]uint64, n)
		for j := range dist[i] {
			if i == j {
				dist[i][j] = 0
			} else {
				dist[i][j] = inf
			}
		}
	}
	for u := 0; u < n; u++ {
		for _, e := range g.OutEdges(u) {
			if e.Cost < dist[u][e.To] {
				dist[u][e.To] = e.Cost
			}
		}
	}
	for k := 0; k < n; k++ {
		for i := 0; i < n; i++ {
			if dist[i][k] == inf {
				continue
			}
			for j := 0; j < n; j++ {
				if dist[k][j] == inf {
					continue
				}
				if nd := dist[i][k] + dist[k][j]; nd < dist[i][j] {
					dist[i][j] = nd
				}
			}
		}
	}
	return g, dist
}

// TestCatchMatchesQuadraticOracle checks every ordered pair of small random
// digraphs (self-loops excluded by construction, unreachable sinks included)
// against an independent O(n^2) all-pairs oracle.
func TestCatchMatchesQuadraticOracle(t *testing.T) {
	ctx := context.Background()
	r := newCatchRand(999)
	checked := 0
	for trial := 0; trial < 300; trial++ {
		n := 2 + r.intn(9)
		g, want := oracleGraph(t, r, n)
		for s := 0; s < n; s++ {
			for d := 0; d < n; d++ {
				for _, alg := range []Algorithm{Catch, CatchAB, CatchR} {
					got, err := Run(ctx, g, s, d, alg)
					if err != nil {
						t.Fatalf("trial=%d alg=%s: %v", trial, alg, err)
					}
					if want[s][d] == inf {
						if got.Stats.Reachable {
							t.Fatalf("trial=%d alg=%s %d->%d: unreachable but got distance %d", trial, alg, s, d, got.Stats.Distance)
						}
						continue
					}
					if !got.Stats.Reachable || got.Stats.Distance != want[s][d] {
						t.Fatalf("trial=%d alg=%s %d->%d: oracle=%d got=%d reachable=%v", trial, alg, s, d, want[s][d], got.Stats.Distance, got.Stats.Reachable)
					}
					if !Validate(g, s, d, got) {
						t.Fatalf("trial=%d alg=%s %d->%d: reported path does not realise the distance", trial, alg, s, d)
					}
					if s == d && (!got.Stats.Reachable || got.Stats.Distance != 0 || len(got.Path) != 1 || got.Path[0] != s) {
						t.Fatalf("trial=%d alg=%s: s==t contract broken: %+v path=%v", trial, alg, got.Stats, got.Path)
					}
					checked++
				}
			}
		}
	}
	t.Logf("verified %d exact queries against the all-pairs oracle", checked)
}

// TestCatchDeterministicAcrossQueries pins that interleaving unrelated queries
// cannot change the result of a query, which would indicate leaked state in the
// pooled workspaces or heuristic caches.
func TestCatchDeterministicAcrossQueries(t *testing.T) {
	ctx := context.Background()
	r := newCatchRand(4242)
	for trial := 0; trial < 20; trial++ {
		g, _ := oracleGraph(t, r, 40)
		n := len(g.Nodes)
		type key struct {
			s, d int
			alg  Algorithm
		}
		first := map[key]Result{}
		var keys []key
		for q := 0; q < 40; q++ {
			s, d := r.intn(n), r.intn(n)
			for _, alg := range []Algorithm{Catch, CatchAB, CatchR} {
				res, err := Run(ctx, g, s, d, alg)
				if err != nil {
					t.Fatal(err)
				}
				k := key{s, d, alg}
				first[k] = res
				keys = append(keys, k)
			}
		}
		for _, k := range keys {
			again, err := Run(ctx, g, k.s, k.d, k.alg)
			if err != nil {
				t.Fatal(err)
			}
			want := first[k]
			if again.Stats.Distance != want.Stats.Distance || again.Stats.Expanded != want.Stats.Expanded ||
				again.Stats.Relaxed != want.Stats.Relaxed || again.Stats.QueuePushes != want.Stats.QueuePushes {
				t.Fatalf("trial=%d %s %d->%d not deterministic: first=%+v again=%+v", trial, k.alg, k.s, k.d, want.Stats, again.Stats)
			}
		}
	}
}

// TestCatchQueriesWithReversedOnlyEdges forces single-direction edges, which is
// what exposes a bidirectional search that mishandles the reverse CSR.
func TestCatchQueriesWithReversedOnlyEdges(t *testing.T) {
	ctx := context.Background()
	r := newCatchRand(31337)
	for trial := 0; trial < 200; trial++ {
		n := 2 + r.intn(10)
		g := graph.New("oneway", "", "car", graph.MetricDistance)
		g.Nodes = make([]graph.Node, n)
		g.Adj = make([][]graph.Edge, n)
		for i := 0; i < n; i++ {
			g.Nodes[i] = graph.Node{ID: int64(i + 1), Lat: 35 + float64(i)*0.002, Lon: 139 + float64(i)*0.003}
		}
		for u := 0; u < n; u++ {
			for v := u + 1; v < n; v++ {
				if r.intn(2) == 0 {
					g.Adj[u] = append(g.Adj[u], graph.Edge{To: v, Cost: uint64(1 + r.intn(50))})
				} else {
					g.Adj[v] = append(g.Adj[v], graph.Edge{To: u, Cost: uint64(1 + r.intn(50))})
				}
			}
		}
		if err := g.Finalize(); err != nil {
			t.Fatal(err)
		}
		for s := 0; s < n; s++ {
			for d := 0; d < n; d++ {
				want, err := Run(ctx, g, s, d, Dijkstra)
				if err != nil {
					t.Fatal(err)
				}
				for _, alg := range []Algorithm{Catch, CatchAB, CatchR} {
					got, err := Run(ctx, g, s, d, alg)
					if err != nil {
						t.Fatal(err)
					}
					if want.Stats.Reachable != got.Stats.Reachable || want.Stats.Distance != got.Stats.Distance {
						t.Fatalf("trial=%d alg=%s %d->%d: dijkstra=%v got=%v", trial, alg, s, d, want.Stats, got.Stats)
					}
				}
			}
		}
	}
	fmt.Println("one-way edge graphs verified")
}

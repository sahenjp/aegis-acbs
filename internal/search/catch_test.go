package search

import (
	"context"
	"math/rand"
	"testing"

	"github.com/sahenjp/aegis-acbs/internal/graph"
)

// TestCatchMatchesDijkstraFuzz fuzzes the CATCH variants against Dijkstra on a
// randomised small road-like graph. It checks three things per query: the same
// reachability, the same exact distance, and that the returned path really
// achieves that distance (Validate). A separate assertion pins the s == t
// contract, because an earlier revision returned unreachable for that case.
func TestCatchMatchesDijkstraFuzz(t *testing.T) {
	ctx := context.Background()
	rnd := rand.New(rand.NewSource(20261026))
	cases := 0
	for trial := 0; trial < 120; trial++ {
		n := 2 + rnd.Intn(60)
		g := graph.New("fuzz", "", "car", graph.MetricDistance)
		g.Nodes = make([]graph.Node, n)
		g.Adj = make([][]graph.Edge, n)
		for i := 0; i < n; i++ {
			g.Nodes[i] = graph.Node{ID: int64(i + 1), Lat: 35 + rnd.Float64()*0.2, Lon: 139 + rnd.Float64()*0.2}
		}
		for i := 0; i < n; i++ {
			for j := 0; j < n; j++ {
				if i == j || rnd.Float64() > 0.18 {
					continue
				}
				m := graph.HaversineMeters(g.Nodes[i].Lat, g.Nodes[i].Lon, g.Nodes[j].Lat, g.Nodes[j].Lon)
				// Mix geodesic-proportional costs with a deterministic jitter so
				// the heuristic stays admissible but not tight.
				cost := uint64(m*float64(900+rnd.Intn(400))) + uint64(rnd.Intn(2000))
				if cost == 0 {
					cost = 1
				}
				g.Adj[i] = append(g.Adj[i], graph.Edge{To: j, Cost: cost})
			}
		}
		if err := g.Finalize(); err != nil {
			t.Fatal(err)
		}
		for q := 0; q < 40; q++ {
			s, d := rnd.Intn(n), rnd.Intn(n)
			want, err := Run(ctx, g, s, d, Dijkstra)
			if err != nil {
				t.Fatal(err)
			}
			for _, alg := range []Algorithm{Catch, CatchAB, CatchR} {
				got, err := Run(ctx, g, s, d, alg)
				if err != nil {
					t.Fatalf("trial=%d alg=%s: %v", trial, alg, err)
				}
				if want.Stats.Reachable != got.Stats.Reachable {
					t.Fatalf("trial=%d alg=%s %d->%d reachable: dijkstra=%v got=%v", trial, alg, s, d, want.Stats.Reachable, got.Stats.Reachable)
				}
				if want.Stats.Distance != got.Stats.Distance {
					t.Fatalf("trial=%d alg=%s %d->%d distance: dijkstra=%d got=%d", trial, alg, s, d, want.Stats.Distance, got.Stats.Distance)
				}
				if got.Stats.Reachable && !Validate(g, s, d, got) {
					t.Fatalf("trial=%d alg=%s %d->%d path invalid: %v", trial, alg, s, d, got.Path)
				}
				if s == d {
					if !got.Stats.Reachable || got.Stats.Distance != 0 || len(got.Path) != 1 || got.Path[0] != s {
						t.Fatalf("trial=%d alg=%s s==t contract: %+v path=%v", trial, alg, got.Stats, got.Path)
					}
				}
				cases++
			}
		}
	}
	t.Logf("verified %d exact queries against Dijkstra", cases)
}

// TestCatchHeuristicAdmissible pins the potential itself: the cached geographic
// bound must never exceed the true distance to its target, on every node.
func TestCatchHeuristicAdmissible(t *testing.T) {
	ctx := context.Background()
	rnd := rand.New(rand.NewSource(7))
	for trial := 0; trial < 12; trial++ {
		n := 2 + rnd.Intn(50)
		g := graph.New("adm", "", "car", graph.MetricDistance)
		g.Nodes = make([]graph.Node, n)
		g.Adj = make([][]graph.Edge, n)
		for i := 0; i < n; i++ {
			g.Nodes[i] = graph.Node{ID: int64(i + 1), Lat: 35 + rnd.Float64()*0.1, Lon: 139 + rnd.Float64()*0.1}
		}
		for i := 0; i < n; i++ {
			for j := 0; j < n; j++ {
				if i != j && rnd.Float64() < 0.15 {
					m := graph.HaversineMeters(g.Nodes[i].Lat, g.Nodes[i].Lon, g.Nodes[j].Lat, g.Nodes[j].Lon)
					cost := uint64(m*1000) + uint64(rnd.Intn(3000)) + 1
					g.Adj[i] = append(g.Adj[i], graph.Edge{To: j, Cost: cost})
				}
			}
		}
		if err := g.Finalize(); err != nil {
			t.Fatal(err)
		}
		to := rnd.Intn(n)
		h := newCatchHeuristic(g, to, make([]uint64, n), make([]bool, n))
		for v := 0; v < n; v++ {
			ref, err := Run(ctx, g, v, to, Dijkstra)
			if err != nil {
				t.Fatal(err)
			}
			if !ref.Stats.Reachable {
				continue
			}
			if hv := h.est(v); hv > ref.Stats.Distance {
				t.Fatalf("trial=%d inadmissible: h(%d)=%d > dist=%d (target %d)", trial, v, hv, ref.Stats.Distance, to)
			}
		}
	}
}

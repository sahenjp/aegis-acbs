package search

import (
	"context"
	"math/rand"
	"testing"

	"github.com/sahenjp/aegis-acbs/internal/graph"
)

// TestMultiBoundMatchesDijkstra checks that stopping on the maximum of the
// f-, g- and b-bounds is still exact. A tighter termination rule is only worth
// having if it never truncates the search early, so this runs the same random
// digraphs used elsewhere, including self-loops, parallel edges and unreachable
// nodes.
func TestMultiBoundMatchesDijkstra(t *testing.T) {
	r := rand.New(rand.NewSource(20261027))
	for trial := 0; trial < 60; trial++ {
		n := 4 + r.Intn(9)
		g := &graph.Graph{Nodes: make([]graph.Node, n)}
		for i := range g.Nodes {
			g.Nodes[i] = graph.Node{
				ID:  int64(i),
				Lat: 50 + float64(r.Intn(1000))/100000,
				Lon: -0.3 + float64(r.Intn(1000))/100000,
			}
		}
		g.Adj = make([][]graph.Edge, n)
		m := n + r.Intn(3*n)
		for k := 0; k < m; k++ {
			from := r.Intn(n)
			to := r.Intn(n)
			w := uint64(1 + r.Intn(40))
			g.Adj[from] = append(g.Adj[from], graph.Edge{To: to, Cost: w})
		}
		if err := g.Finalize(); err != nil {
			t.Fatalf("trial %d: finalize: %v", trial, err)
		}
		for s := 0; s < n; s++ {
			for d := 0; d < n; d++ {
				want, err := Run(context.Background(), g, s, d, Dijkstra)
				if err != nil {
					t.Fatalf("dijkstra: %v", err)
				}
				for _, alg := range []Algorithm{MultiBound, MultiboundStatic} {
					got, err := Run(context.Background(), g, s, d, alg)
					if err != nil {
						t.Fatalf("trial %d %s %d->%d: %v", trial, alg, s, d, err)
					}
					if got.Stats.Reachable != want.Stats.Reachable {
						t.Fatalf("trial %d %s %d->%d: reachable %v want %v",
							trial, alg, s, d, got.Stats.Reachable, want.Stats.Reachable)
					}
					if want.Stats.Reachable && got.Stats.Distance != want.Stats.Distance {
						t.Fatalf("trial %d %s %d->%d: distance %d want %d",
							trial, alg, s, d, got.Stats.Distance, want.Stats.Distance)
					}
					if want.Stats.Reachable && len(got.Path) == 0 {
						t.Fatalf("trial %d %s %d->%d: reachable but empty path", trial, alg, s, d)
					}
				}
			}
		}
	}
}

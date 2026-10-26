package search

import (
	"context"
	"math/rand"
	"testing"

	"github.com/sahenjp/aegis-acbs/internal/graph"
)

// checkMEETAgainstDijkstra asserts that MEET returns exactly the same distance
// and reachability as Dijkstra, and that any reported path is valid.
func checkMEETAgainstDijkstra(t *testing.T, ctx context.Context, g *graph.Graph, s, d int) {
	t.Helper()
	want, err := Run(ctx, g, s, d, Dijkstra)
	if err != nil {
		t.Fatalf("dijkstra %d->%d: %v", s, d, err)
	}
	got, err := meet(ctx, g, s, d)
	if err != nil {
		t.Fatalf("meet %d->%d: %v", s, d, err)
	}
	if got.Stats.Algorithm != Meet {
		t.Fatalf("meet reported algorithm %q, want %q", got.Stats.Algorithm, Meet)
	}
	if want.Stats.Reachable != got.Stats.Reachable || want.Stats.Distance != got.Stats.Distance {
		t.Fatalf("%d->%d distance mismatch: dijkstra=%+v meet=%+v", s, d, want.Stats, got.Stats)
	}
	if got.Stats.Reachable && !Validate(g, s, d, got) {
		t.Fatalf("%d->%d produced an invalid path: %+v", s, d, got.Stats)
	}
}

// TestMEETMatchesDijkstraExhaustiveSmallDirected enumerates every directed graph
// on 2..4 nodes (with one deterministic weight per arc), so MEET is checked
// against Dijkstra on every topology, including unreachable and self-equal
// endpoint cases.
func TestMEETMatchesDijkstraExhaustiveSmallDirected(t *testing.T) {
	ctx := context.Background()
	checked := 0
	for n := 2; n <= 4; n++ {
		pairs := make([][2]int, 0, n*(n-1))
		for i := 0; i < n; i++ {
			for j := 0; j < n; j++ {
				if i != j {
					pairs = append(pairs, [2]int{i, j})
				}
			}
		}
		total := 1 << len(pairs)
		for mask := 0; mask < total; mask++ {
			g := graph.New("test", "", "", graph.MetricDistance)
			g.Nodes = make([]graph.Node, n)
			g.Adj = make([][]graph.Edge, n)
			for i := range g.Nodes {
				g.Nodes[i] = graph.Node{ID: int64(i + 1), Lat: 35 + float64(i)*.001, Lon: 139 + float64(i)*.001}
			}
			for bit, p := range pairs {
				if mask&(1<<bit) != 0 {
					g.Adj[p[0]] = append(g.Adj[p[0]], graph.Edge{To: p[1], Cost: uint64(1 + (bit % 7))})
				}
			}
			if err := g.Finalize(); err != nil {
				t.Fatal(err)
			}
			for s := 0; s < n; s++ {
				for d := 0; d < n; d++ {
					checkMEETAgainstDijkstra(t, ctx, g, s, d)
					checked++
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("exhaustive check covered no queries")
	}
	t.Logf("MEET exhaustive small-directed: %d (graph, source, target) checks matched Dijkstra", checked)
}

// TestMEETFuzzMatchesDijkstraRandomGraphs fuzzes MEET against Dijkstra with
// math/rand over sparse directed random graphs, including zero-cost-adjacent
// tiny weights and graphs with disconnected components.
func TestMEETFuzzMatchesDijkstraRandomGraphs(t *testing.T) {
	ctx := context.Background()
	rnd := rand.New(rand.NewSource(20231026))
	checked := 0
	for caseNo := 0; caseNo < 300; caseNo++ {
		n := 2 + rnd.Intn(60)
		g := graph.New("fuzz", "", "car", graph.MetricDistance)
		g.Nodes = make([]graph.Node, n)
		g.Adj = make([][]graph.Edge, n)
		for i := 0; i < n; i++ {
			g.Nodes[i] = graph.Node{ID: int64(i + 1), Lat: 35 + rnd.Float64()*.2, Lon: 139 + rnd.Float64()*.2}
		}
		density := rnd.Float64() * 0.35
		for i := 0; i < n; i++ {
			for j := 0; j < n; j++ {
				if i == j || rnd.Float64() >= density {
					continue
				}
				// Small colliding weights force many equal-distance keys, which
				// is exactly where the termination rule has to be careful.
				cost := uint64(1 + rnd.Intn(9))
				g.Adj[i] = append(g.Adj[i], graph.Edge{To: j, Cost: cost})
			}
		}
		if err := g.Finalize(); err != nil {
			t.Fatal(err)
		}
		for q := 0; q < 20; q++ {
			s, d := rnd.Intn(n), rnd.Intn(n)
			checkMEETAgainstDijkstra(t, ctx, g, s, d)
			checked++
		}
	}
	t.Logf("MEET fuzz random graphs: %d queries matched Dijkstra", checked)
}

// TestMEETFuzzMatchesDijkstraRoadLike covers the symmetric, geographically
// embedded shape the benchmark graphs actually use.
func TestMEETFuzzMatchesDijkstraRoadLike(t *testing.T) {
	rnd := rand.New(rand.NewSource(710110))
	ctx := context.Background()
	checked := 0
	for caseNo := 0; caseNo < 60; caseNo++ {
		n := 25 + rnd.Intn(50)
		g := graph.New("road", "", "car", graph.MetricDistance)
		g.Nodes = make([]graph.Node, n)
		g.Adj = make([][]graph.Edge, n)
		for i := 0; i < n; i++ {
			g.Nodes[i] = graph.Node{ID: int64(i + 1), Lat: 35 + rnd.Float64()*.1, Lon: 139 + rnd.Float64()*.1}
		}
		for i := 0; i < n; i++ {
			for j := i + 1; j < n; j++ {
				if rnd.Float64() < .08 {
					m := graph.HaversineMeters(g.Nodes[i].Lat, g.Nodes[i].Lon, g.Nodes[j].Lat, g.Nodes[j].Lon)
					cost := uint64(m*1000) + uint64(rnd.Intn(5000))
					g.Adj[i] = append(g.Adj[i], graph.Edge{To: j, Cost: cost})
					g.Adj[j] = append(g.Adj[j], graph.Edge{To: i, Cost: cost})
				}
			}
		}
		if err := g.Finalize(); err != nil {
			t.Fatal(err)
		}
		for q := 0; q < 30; q++ {
			s, d := rnd.Intn(n), rnd.Intn(n)
			checkMEETAgainstDijkstra(t, ctx, g, s, d)
			checked++
		}
	}
	t.Logf("MEET fuzz road-like graphs: %d queries matched Dijkstra", checked)
}

// TestMEETMatchesDijkstraOnGridAndWorkspaceReuse exercises the pooled
// bidirectional workspace across many repeated calls, which is where leaked
// dist/settled state would show up as a wrong answer.
func TestMEETMatchesDijkstraOnGridAndWorkspaceReuse(t *testing.T) {
	g := gridGraph(t, 40, 40, true)
	ctx := context.Background()
	pairs := [][2]int{{0, 1599}, {1599, 0}, {10, 11}, {50, 1350}, {800, 25}, {0, 0}, {1599, 1599}}
	for round := 0; round < 40; round++ {
		for _, pair := range pairs {
			checkMEETAgainstDijkstra(t, ctx, g, pair[0], pair[1])
		}
	}
}

// TestMEETZeroCostFreeGrid ensures MEET handles a graph whose weights are all
// equal (massive key collisions) and a graph with a single long chain.
func TestMEETEqualWeightGridAndChain(t *testing.T) {
	ctx := context.Background()
	g := gridGraph(t, 30, 30, true)
	for r := 0; r < 15; r++ {
		checkMEETAgainstDijkstra(t, ctx, g, r*30, (29-r)*30+29)
		checkMEETAgainstDijkstra(t, ctx, g, (29-r)*30+29, r*30)
	}

	const n = 400
	chain := graph.New("chain", "", "", graph.MetricDistance)
	chain.Nodes = make([]graph.Node, n)
	chain.Adj = make([][]graph.Edge, n)
	for i := 0; i < n; i++ {
		chain.Nodes[i] = graph.Node{ID: int64(i + 1), Lat: 35 + float64(i)*0.00001, Lon: 139}
	}
	for i := 0; i+1 < n; i++ {
		chain.Adj[i] = append(chain.Adj[i], graph.Edge{To: i + 1, Cost: uint64(1 + i%5)})
	}
	if err := chain.Finalize(); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]int{{0, n - 1}, {n - 1, 0}, {0, 1}, {100, 101}, {0, n - 1}} {
		checkMEETAgainstDijkstra(t, ctx, chain, pair[0], pair[1])
	}
}

// TestMEETUnreachableAndCancelled covers the no-path result shape and context
// cancellation propagation.
func TestMEETUnreachableAndCancelled(t *testing.T) {
	ctx := context.Background()
	g := graph.New("split", "", "", graph.MetricDistance)
	g.Nodes = []graph.Node{{ID: 1, Lat: 35, Lon: 139}, {ID: 2, Lat: 35.01, Lon: 139}, {ID: 3, Lat: 35.02, Lon: 139}}
	g.Adj = make([][]graph.Edge, 3)
	g.Adj[0] = append(g.Adj[0], graph.Edge{To: 1, Cost: 10})
	if err := g.Finalize(); err != nil {
		t.Fatal(err)
	}
	got, err := meet(ctx, g, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got.Stats.Reachable || got.Stats.Distance != 0 || len(got.Path) != 0 {
		t.Fatalf("expected unreachable result, got %+v", got)
	}
	want, err := Run(ctx, g, 0, 2, Dijkstra)
	if err != nil {
		t.Fatal(err)
	}
	if want.Stats.Reachable != got.Stats.Reachable {
		t.Fatalf("reachability mismatch: dijkstra=%+v meet=%+v", want.Stats, got.Stats)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := meet(cancelled, gridGraph(t, 60, 60, true), 0, 3599); err == nil {
		t.Fatal("cancelled context did not abort MEET")
	}
}

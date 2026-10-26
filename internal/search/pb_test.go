package search

import (
	"context"
	"math/rand"
	"path/filepath"
	"testing"

	"github.com/lasder-ca/aegis-acbs/internal/graph"
)

// TestPBBucketsMatchesDijkstraFuzz fuzzes PB against Dijkstra with math/rand on a
// small embedded road-like graph: random weights, random directedness and many
// random source/target pairs. It also re-runs every pair a second time to make
// sure pooled workspace state never leaks between queries.
func TestPBBucketsMatchesDijkstraFuzz(t *testing.T) {
	ctx := context.Background()
	rnd := rand.New(rand.NewSource(77031))
	const graphCases = 40
	const pairsPerCase = 150
	checked := 0
	for caseNo := 0; caseNo < graphCases; caseNo++ {
		n := 8 + rnd.Intn(13) // 8..20 nodes
		var g *graph.Graph
		for attempt := 0; ; attempt++ {
			g = graph.New("pb-fuzz", "", "car", graph.MetricDistance)
			g.Nodes = make([]graph.Node, n)
			g.Adj = make([][]graph.Edge, n)
			for i := 0; i < n; i++ {
				g.Nodes[i] = graph.Node{ID: int64(i + 1), Lat: 51.76 + rnd.Float64()*0.01, Lon: -0.24 + rnd.Float64()*0.015}
			}
			for i := 0; i < n; i++ {
				for j := i + 1; j < n; j++ {
					if rnd.Float64() < 0.18 {
						// Deliberately integer weights with duplicates and odd
						// magnitudes, the PB bucket edge cases.
						cost := uint64(1 + rnd.Intn(40))
						g.Adj[i] = append(g.Adj[i], graph.Edge{To: j, Cost: cost})
						if rnd.Float64() < 0.6 {
							cost2 := uint64(1 + rnd.Intn(40))
							g.Adj[j] = append(g.Adj[j], graph.Edge{To: i, Cost: cost2})
						}
					}
				}
			}
			err := g.Finalize()
			if err == nil && g.EdgeCount > 0 {
				break
			}
			if attempt > 20 {
				t.Fatalf("case %d: could not build a valid random graph: %v", caseNo, err)
			}
		}
		for q := 0; q < pairsPerCase; q++ {
			s, d := rnd.Intn(n), rnd.Intn(n)
			want, err := Run(ctx, g, s, d, Dijkstra)
			if err != nil {
				t.Fatalf("case %d %d->%d dijkstra: %v", caseNo, s, d, err)
			}
			for rep := 0; rep < 2; rep++ {
				got, err := Run(ctx, g, s, d, PB)
				if err != nil {
					t.Fatalf("case %d %d->%d pb: %v", caseNo, s, d, err)
				}
				if got.Stats.Algorithm != PB {
					t.Fatalf("case %d: algorithm label = %q, want %q", caseNo, got.Stats.Algorithm, PB)
				}
				if want.Stats.Reachable != got.Stats.Reachable {
					t.Fatalf("case %d %d->%d reachable mismatch: dijkstra=%v pb=%v", caseNo, s, d, want.Stats.Reachable, got.Stats.Reachable)
				}
				if want.Stats.Distance != got.Stats.Distance {
					t.Fatalf("case %d %d->%d distance mismatch: dijkstra=%d pb=%d", caseNo, s, d, want.Stats.Distance, got.Stats.Distance)
				}
				if !Validate(g, s, d, got) {
					t.Fatalf("case %d %d->%d pb path failed validation: %+v", caseNo, s, d, got.Stats)
				}
				checked++
			}
		}
	}
	if checked == 0 {
		t.Fatal("fuzz test did not execute any comparison")
	}
	t.Logf("PB-Buckets fuzz: %d graph cases, %d comparisons against Dijkstra", graphCases, checked)
}

// TestPBBucketsMatchesDijkstraExhaustiveTiny enumerates every directed edge
// subset of tiny graphs, which covers empty frontiers, one-way chains and
// unreachable pairs that random fuzzing reaches only rarely.
func TestPBBucketsMatchesDijkstraExhaustiveTiny(t *testing.T) {
	ctx := context.Background()
	for n := 2; n <= 4; n++ {
		pairs := make([][2]int, 0, n*(n-1))
		for i := 0; i < n; i++ {
			for j := 0; j < n; j++ {
				if i != j {
					pairs = append(pairs, [2]int{i, j})
				}
			}
		}
		for mask := 0; mask < 1<<len(pairs); mask++ {
			g := graph.New("tiny", "", "", graph.MetricDistance)
			g.Nodes = make([]graph.Node, n)
			g.Adj = make([][]graph.Edge, n)
			for i := range g.Nodes {
				g.Nodes[i] = graph.Node{ID: int64(i + 1), Lat: 35 + float64(i)*.001, Lon: 139 + float64(i)*.001}
			}
			for bit, p := range pairs {
				if mask&(1<<bit) != 0 {
					g.Adj[p[0]] = append(g.Adj[p[0]], graph.Edge{To: p[1], Cost: uint64(1 + (bit % 5))})
				}
			}
			if err := g.Finalize(); err != nil {
				t.Fatal(err)
			}
			for s := 0; s < n; s++ {
				for d := 0; d < n; d++ {
					a, err := Run(ctx, g, s, d, Dijkstra)
					if err != nil {
						t.Fatal(err)
					}
					b, err := Run(ctx, g, s, d, PB)
					if err != nil {
						t.Fatal(err)
					}
					if a.Stats.Reachable != b.Stats.Reachable || a.Stats.Distance != b.Stats.Distance || (b.Stats.Reachable && !Validate(g, s, d, b)) {
						t.Fatalf("n=%d mask=%d %d->%d dijkstra=%+v pb=%+v", n, mask, s, d, a.Stats, b.Stats)
					}
				}
			}
		}
	}
}

// TestPBBucketsMatchesDijkstraRoadLike uses larger road-like graphs where every
// query travels a realistic number of hops.
func TestPBBucketsMatchesDijkstraRoadLike(t *testing.T) {
	ctx := context.Background()
	rnd := rand.New(rand.NewSource(20261026))
	for caseNo := 0; caseNo < 8; caseNo++ {
		n := 120 + rnd.Intn(80)
		g := graph.New("pb-road", "", "car", graph.MetricDistance)
		g.Nodes = make([]graph.Node, n)
		g.Adj = make([][]graph.Edge, n)
		for i := 0; i < n; i++ {
			g.Nodes[i] = graph.Node{ID: int64(i + 1), Lat: 35 + rnd.Float64()*.1, Lon: 139 + rnd.Float64()*.1}
		}
		for i := 0; i < n; i++ {
			for j := i + 1; j < n; j++ {
				if rnd.Float64() < .05 {
					m := graph.HaversineMeters(g.Nodes[i].Lat, g.Nodes[i].Lon, g.Nodes[j].Lat, g.Nodes[j].Lon)
					g.Adj[i] = append(g.Adj[i], graph.Edge{To: j, Cost: uint64(m*1000) + uint64(rnd.Intn(5000))})
					g.Adj[j] = append(g.Adj[j], graph.Edge{To: i, Cost: uint64(m*1000) + uint64(rnd.Intn(5000))})
				}
			}
		}
		if err := g.Finalize(); err != nil {
			t.Fatal(err)
		}
		for q := 0; q < 60; q++ {
			s, d := rnd.Intn(n), rnd.Intn(n)
			a, err := Run(ctx, g, s, d, Dijkstra)
			if err != nil {
				t.Fatal(err)
			}
			b, err := Run(ctx, g, s, d, PB)
			if err != nil {
				t.Fatal(err)
			}
			if a.Stats.Reachable != b.Stats.Reachable || a.Stats.Distance != b.Stats.Distance || (b.Stats.Reachable && !Validate(g, s, d, b)) {
				t.Fatalf("case=%d %d->%d dijkstra=%+v pb=%+v", caseNo, s, d, a.Stats, b.Stats)
			}
		}
	}
}

// TestPBBucketsExpansionIsBidirectional checks that the work really is split
// across both frontiers instead of silently degenerating into one direction.
func TestPBBucketsExpansionIsBidirectional(t *testing.T) {
	g := gridGraph(t, 40, 40, true)
	r, err := Run(context.Background(), g, 0, len(g.Nodes)-1, PB)
	if err != nil || !r.Stats.Reachable {
		t.Fatalf("pb failed: %+v err=%v", r.Stats, err)
	}
	if r.Stats.ForwardExpanded == 0 || r.Stats.BackwardExpanded == 0 {
		t.Fatalf("expected both directions to expand: %+v", r.Stats)
	}
	d, err := Run(context.Background(), g, 0, len(g.Nodes)-1, Dijkstra)
	if err != nil {
		t.Fatal(err)
	}
	if d.Stats.Distance != r.Stats.Distance {
		t.Fatalf("distance mismatch on grid: dijkstra=%d pb=%d", d.Stats.Distance, r.Stats.Distance)
	}
}

// TestPBBucketsMatchesDijkstraRealGraph runs the differential check on the real
// Hatfield extract when the fixture is present, on 500 random pairs per graph.
func TestPBBucketsMatchesDijkstraRealGraph(t *testing.T) {
	ctx := context.Background()
	paths := []string{
		"../../artifacts/repro-graphs/hatfield-distance.aegis",
		"../../artifacts/repro-graphs/hatfield-time.aegis",
	}
	tested := 0
	for _, p := range paths {
		g, err := graph.Load(p)
		if err != nil {
			t.Logf("skipping %s: %v", p, err)
			continue
		}
		n := len(g.Nodes)
		rnd := rand.New(rand.NewSource(424242))
		compared := 0
		for i := 0; i < 500; i++ {
			s, d := rnd.Intn(n), rnd.Intn(n)
			want, err := Run(ctx, g, s, d, Dijkstra)
			if err != nil {
				t.Fatalf("%s dijkstra %d->%d: %v", p, s, d, err)
			}
			got, err := Run(ctx, g, s, d, PB)
			if err != nil {
				t.Fatalf("%s pb %d->%d: %v", p, s, d, err)
			}
			if want.Stats.Reachable != got.Stats.Reachable || want.Stats.Distance != got.Stats.Distance {
				t.Fatalf("%s %d->%d distance mismatch: dijkstra=%d pb=%d", p, s, d, want.Stats.Distance, got.Stats.Distance)
			}
			if got.Stats.Reachable && !Validate(g, s, d, got) {
				t.Fatalf("%s %d->%d path invalid: %+v", p, s, d, got.Stats)
			}
			compared++
		}
		t.Logf("%s: %d pairs match Dijkstra (nodes=%d edges=%d)", filepath.Base(p), compared, len(g.Nodes), g.EdgeCount)
		tested += compared
	}
	if tested == 0 {
		t.Skip("no real graph fixtures present")
	}
}

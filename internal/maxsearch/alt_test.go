package maxsearch

import (
	"context"
	"testing"

	"github.com/lasder-ca/aegis-acbs/internal/graph"
	"github.com/lasder-ca/aegis-acbs/internal/search"
)

func TestALTMatchesDijkstraOnDirectedGraph(t *testing.T) {
	g := altFixture(t)
	runner, err := NewALTRunner(context.Background(), g, 4)
	if err != nil {
		t.Fatal(err)
	}
	if runner.LandmarkCount() != 4 {
		t.Fatalf("landmarks=%d want=4", runner.LandmarkCount())
	}
	if runner.PreprocessDuration() <= 0 {
		t.Fatal("preprocess duration was not recorded")
	}
	if runner.DistanceTableBytes() != uint64(4*len(g.Nodes)*2*8) {
		t.Fatalf("table bytes=%d", runner.DistanceTableBytes())
	}

	for source := range g.Nodes {
		for target := range g.Nodes {
			want, err := search.Run(context.Background(), g, source, target, search.Dijkstra)
			if err != nil {
				t.Fatalf("dijkstra %d->%d: %v", source, target, err)
			}
			got, err := runner.Run(context.Background(), g, source, target)
			if err != nil {
				t.Fatalf("alt %d->%d: %v", source, target, err)
			}
			if got.Stats.Algorithm != ALT {
				t.Fatalf("%d->%d algorithm=%q", source, target, got.Stats.Algorithm)
			}
			if got.Stats.Reachable != want.Stats.Reachable {
				t.Fatalf("%d->%d reachable got=%v want=%v", source, target, got.Stats.Reachable, want.Stats.Reachable)
			}
			if got.Stats.Reachable && got.Stats.Distance != want.Stats.Distance {
				t.Fatalf("%d->%d distance got=%d want=%d", source, target, got.Stats.Distance, want.Stats.Distance)
			}
			if !search.Validate(g, source, target, got) {
				t.Fatalf("%d->%d invalid ALT path: %+v", source, target, got)
			}
		}
	}
}

func TestALTDirectedHeuristicIsAdmissible(t *testing.T) {
	g := altFixture(t)
	runner, err := NewALTRunner(context.Background(), g, 4)
	if err != nil {
		t.Fatal(err)
	}
	for target := range g.Nodes {
		for v := range g.Nodes {
			shortest, err := search.Run(context.Background(), g, v, target, search.Dijkstra)
			if err != nil {
				t.Fatal(err)
			}
			if !shortest.Stats.Reachable {
				continue
			}
			h := runner.heuristic(v, target)
			if h > shortest.Stats.Distance {
				t.Fatalf("heuristic overestimated %d->%d: h=%d shortest=%d", v, target, h, shortest.Stats.Distance)
			}
		}
	}
}

func TestALTRejectsDifferentGraphInstance(t *testing.T) {
	g := altFixture(t)
	runner, err := NewALTRunner(context.Background(), g, 2)
	if err != nil {
		t.Fatal(err)
	}
	other := altFixture(t)
	if _, err := runner.Run(context.Background(), other, 0, 1); err == nil {
		t.Fatal("ALT accepted a different graph instance")
	}
}

func TestALTSelectLandmarksDeterministicallyWithoutDuplicates(t *testing.T) {
	g := altFixture(t)
	a := selectALTlandmarks(g, 6)
	b := selectALTlandmarks(g, 6)
	if len(a) != 6 || len(b) != 6 {
		t.Fatalf("lengths=%d,%d", len(a), len(b))
	}
	seen := map[int]bool{}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("selection is not deterministic: %v vs %v", a, b)
		}
		if seen[a[i]] {
			t.Fatalf("duplicate landmark in %v", a)
		}
		seen[a[i]] = true
	}
}

func altFixture(t *testing.T) *graph.Graph {
	t.Helper()
	g := graph.New("alt-fixture", "test", "car", graph.MetricDistance)
	g.Nodes = []graph.Node{
		{ID: 1, Lat: 35.00, Lon: 139.00},
		{ID: 2, Lat: 35.01, Lon: 139.01},
		{ID: 3, Lat: 35.02, Lon: 139.03},
		{ID: 4, Lat: 34.99, Lon: 139.02},
		{ID: 5, Lat: 35.03, Lon: 139.04},
		{ID: 6, Lat: 35.04, Lon: 139.05},
		{ID: 7, Lat: 35.10, Lon: 139.10}, // isolated
	}
	g.Adj = make([][]graph.Edge, len(g.Nodes))
	add := func(from, to int, cost uint64) {
		g.Adj[from] = append(g.Adj[from], graph.Edge{To: to, Cost: cost})
	}
	add(0, 1, 2)
	add(0, 3, 10)
	add(1, 2, 2)
	add(1, 4, 10)
	add(2, 0, 8)
	add(2, 4, 3)
	add(3, 2, 1)
	add(4, 1, 4)
	add(4, 5, 1)
	add(5, 2, 6)
	if err := g.Finalize(); err != nil {
		t.Fatal(err)
	}
	return g
}

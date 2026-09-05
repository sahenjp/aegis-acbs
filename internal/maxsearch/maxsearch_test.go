package maxsearch

import (
	"context"
	"testing"

	"github.com/lasder-ca/aegis-acbs/internal/graph"
)

func fixtureGraph(t *testing.T) *graph.Graph {
	t.Helper()
	g := graph.New("fixture", "test", "car", graph.MetricDistance)
	g.Nodes = []graph.Node{{ID: 1}, {ID: 2}, {ID: 3}}
	g.Adj = [][]graph.Edge{
		{{To: 1, Cost: 3}, {To: 2, Cost: 10}},
		{{To: 2, Cost: 4}},
		{},
	}
	if err := g.Finalize(); err != nil {
		t.Fatal(err)
	}
	return g
}

func TestCandidatesAreUnique(t *testing.T) {
	got := Candidates(fixtureGraph(t), 0, 2)
	seen := map[string]bool{}
	for _, alg := range got {
		if seen[string(alg)] {
			t.Fatalf("duplicate %q", alg)
		}
		seen[string(alg)] = true
	}
}

func TestRun(t *testing.T) {
	out, err := Run(context.Background(), fixtureGraph(t), 0, 2, Config{MaxParallel: 1, Verify: true})
	if err != nil {
		t.Fatal(err)
	}
	if out.Winner == "" {
		t.Fatal("missing winner")
	}
	if out.Result.Stats.Distance != 7 {
		t.Fatalf("distance=%d want=7", out.Result.Stats.Distance)
	}
}

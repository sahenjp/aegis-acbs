package maxsearch

import (
	"testing"

	"github.com/lasder-ca/aegis-acbs/internal/graph"
)

func TestParseRoutingKitCHReachable(t *testing.T) {
	got, err := parseRoutingKitCHResponse("R 7 1234 3 0 1 2\n", 3)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Stats.Reachable || got.Stats.Distance != 7 || got.Stats.DurationNS != 1234 {
		t.Fatalf("stats=%+v", got.Stats)
	}
	if len(got.Path) != 3 || got.Path[0] != 0 || got.Path[2] != 2 {
		t.Fatalf("path=%v", got.Path)
	}
}

func TestParseRoutingKitCHUnreachable(t *testing.T) {
	got, err := parseRoutingKitCHResponse("U 99\n", 3)
	if err != nil {
		t.Fatal(err)
	}
	if got.Stats.Reachable || got.Stats.DurationNS != 99 || len(got.Path) != 0 {
		t.Fatalf("got=%+v", got)
	}
}

func TestParseRoutingKitCHRejectsBadPath(t *testing.T) {
	for _, response := range []string{
		"R 7 10 2 0\n",
		"R 7 10 1 99\n",
		"U 0\n",
		"E invalid-path\n",
	} {
		if _, err := parseRoutingKitCHResponse(response, 3); err == nil {
			t.Fatalf("accepted %q", response)
		}
	}
}

func TestRoutingKitGraphFingerprintIsStableAndWeightSensitive(t *testing.T) {
	build := func(cost uint64) *graph.Graph {
		g := graph.New("fixture", "test", "car", graph.MetricDistance)
		g.Nodes = []graph.Node{{ID: 1}, {ID: 2}}
		g.Adj = [][]graph.Edge{{{To: 1, Cost: cost}}, {}}
		if err := g.Finalize(); err != nil {
			t.Fatal(err)
		}
		return g
	}
	a := build(7)
	b := build(7)
	c := build(8)
	fa := RoutingKitGraphFingerprint(a)
	if len(fa) != 64 {
		t.Fatalf("fingerprint length=%d want=64", len(fa))
	}
	if fa != RoutingKitGraphFingerprint(b) {
		t.Fatal("equivalent graphs produced different fingerprints")
	}
	if fa == RoutingKitGraphFingerprint(c) {
		t.Fatal("edge-weight change did not change the fingerprint")
	}
}

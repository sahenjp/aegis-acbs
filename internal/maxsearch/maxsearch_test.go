package maxsearch

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lasder-ca/aegis-acbs/internal/graph"
	"github.com/lasder-ca/aegis-acbs/internal/search"
)

type fakeRunner struct {
	name      search.Algorithm
	delay     time.Duration
	distance  uint64
	reachable bool
	err       error
}

func (r fakeRunner) Name() search.Algorithm { return r.name }

func (r fakeRunner) Run(ctx context.Context, _ *graph.Graph, _, _ int) (search.Result, error) {
	select {
	case <-ctx.Done():
		return search.Result{}, ctx.Err()
	case <-time.After(r.delay):
	}
	if r.err != nil {
		return search.Result{}, r.err
	}
	return search.Result{Stats: search.Stats{Algorithm: r.name, Distance: r.distance, Reachable: r.reachable}}, nil
}

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

func TestBuildPlanDiverseAndUnique(t *testing.T) {
	plan, err := BuildPlan(fixtureGraph(t), 0, 2, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Candidates) < 3 {
		t.Fatalf("candidates=%v", plan.Candidates)
	}
	seen := map[search.Algorithm]bool{}
	for _, candidate := range plan.Candidates {
		if seen[candidate.Algorithm] {
			t.Fatalf("duplicate %q", candidate.Algorithm)
		}
		seen[candidate.Algorithm] = true
	}
	if plan.Candidates[0].Role != "primary" || plan.Candidates[1].Role != "diverse-hedge" {
		t.Fatalf("roles=%+v", plan.Candidates[:2])
	}
}

func TestRun(t *testing.T) {
	out, err := Run(context.Background(), fixtureGraph(t), 0, 2, Config{Mode: ModeEfficient, Verify: true})
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

func TestLatencyModeReturnsFastestRunner(t *testing.T) {
	runners := []Runner{
		fakeRunner{name: "slow", delay: 25 * time.Millisecond, distance: 7, reachable: true},
		fakeRunner{name: "fast", delay: time.Millisecond, distance: 7, reachable: true},
	}
	out, err := RunWithRunners(context.Background(), fixtureGraph(t), 0, 2, Config{Mode: ModeLatency, MaxParallel: 2}, runners)
	if err != nil {
		t.Fatal(err)
	}
	if out.Winner != "fast" {
		t.Fatalf("winner=%s", out.Winner)
	}
}

func TestEfficientModeDoesNotRace(t *testing.T) {
	runners := []Runner{
		fakeRunner{name: "first", delay: 5 * time.Millisecond, distance: 7, reachable: true},
		fakeRunner{name: "second", delay: time.Millisecond, distance: 7, reachable: true},
	}
	out, err := RunWithRunners(context.Background(), fixtureGraph(t), 0, 2, Config{Mode: ModeEfficient, MaxParallel: 8}, runners)
	if err != nil {
		t.Fatal(err)
	}
	if out.Winner != "first" || len(out.Attempts) != 1 {
		t.Fatalf("out=%+v", out)
	}
}

func TestConsensusAgreement(t *testing.T) {
	runners := []Runner{
		fakeRunner{name: "a", delay: time.Millisecond, distance: 7, reachable: true},
		fakeRunner{name: "b", delay: 2 * time.Millisecond, distance: 7, reachable: true},
	}
	out, err := RunWithRunners(context.Background(), fixtureGraph(t), 0, 2, Config{Mode: ModeLatency, MaxParallel: 2, Consensus: true}, runners)
	if err != nil {
		t.Fatal(err)
	}
	if !out.ConsensusReached {
		t.Fatal("consensus not reached")
	}
}

func TestConsensusMismatch(t *testing.T) {
	runners := []Runner{
		fakeRunner{name: "a", delay: time.Millisecond, distance: 7, reachable: true},
		fakeRunner{name: "b", delay: 2 * time.Millisecond, distance: 8, reachable: true},
	}
	_, err := RunWithRunners(context.Background(), fixtureGraph(t), 0, 2, Config{Mode: ModeLatency, MaxParallel: 2, Consensus: true}, runners)
	if !errors.Is(err, ErrConsensusMismatch) {
		t.Fatalf("err=%v", err)
	}
}

func TestBalancedRequiresDelay(t *testing.T) {
	_, err := BuildPlan(fixtureGraph(t), 0, 2, Config{Mode: ModeBalanced, MaxParallel: 2})
	if err == nil {
		t.Fatal("expected error")
	}
}

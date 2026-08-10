package search

import (
	"context"
	"testing"
)

func TestACBSEntropicSmallGraphUsesProductionTransitions(t *testing.T) {
	g := gridGraph(t, 24, 24, true)
	if g.EdgeCount >= aegisEntropicMinimumEdges {
		t.Fatalf("test graph has %d edges", g.EdgeCount)
	}

	base, err := Run(context.Background(), g, 0, len(g.Nodes)-1, Aegis)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := Run(context.Background(), g, 0, len(g.Nodes)-1, AegisEntropic)
	if err != nil {
		t.Fatal(err)
	}
	if !Validate(g, 0, len(g.Nodes)-1, candidate) {
		t.Fatalf("invalid candidate result: %+v", candidate.Stats)
	}
	if candidate.Stats.Algorithm != AegisEntropic {
		t.Fatalf("algorithm = %q", candidate.Stats.Algorithm)
	}
	if candidate.Stats.SchedulerVersion != acbsEntropicSchedulerVersion {
		t.Fatalf("scheduler version = %q", candidate.Stats.SchedulerVersion)
	}

	checks := []struct {
		name string
		base uint64
		got  uint64
	}{
		{"distance", base.Stats.Distance, candidate.Stats.Distance},
		{"expanded", base.Stats.Expanded, candidate.Stats.Expanded},
		{"relaxed", base.Stats.Relaxed, candidate.Stats.Relaxed},
		{"chunks", base.Stats.Chunks, candidate.Stats.Chunks},
		{"switches", base.Stats.DirectionSwitches, candidate.Stats.DirectionSwitches},
		{"first upper bound", base.Stats.FirstUpperBoundExpanded, candidate.Stats.FirstUpperBoundExpanded},
	}
	for _, check := range checks {
		if check.base != check.got {
			t.Fatalf("%s: production=%d candidate=%d", check.name, check.base, check.got)
		}
	}
}

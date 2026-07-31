package search

import (
	"context"
	"testing"

	"github.com/lasder-ca/aegis-acbs/internal/graph"
)

func TestRecommendedMetricALTLandmarks(t *testing.T) {
	cases := []struct {
		edges int
		want  int
	}{{9_999, 0}, {10_000, 4}, {399_999, 4}, {400_000, 8}}
	for _, tc := range cases {
		g := &graph.Graph{EdgeCount: tc.edges}
		if got := RecommendedMetricALTLandmarks(g); got != tc.want {
			t.Fatalf("edges=%d: got %d, want %d", tc.edges, got, tc.want)
		}
	}
}

func TestMetricALTPreparedCandidateRemainsExactAndFeasible(t *testing.T) {
	g := gridGraph(t, 40, 40, true)
	// Force the indexed execution tier without inflating this exhaustive fixture.
	g.EdgeCount = metricALTMinimumEdges
	preparation, err := PrepareMetricALT(g, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer ReleaseMetricALT(g)
	if preparation.Landmarks != 4 || preparation.Bytes == 0 || preparation.Reused {
		t.Fatalf("invalid preparation: %+v", preparation)
	}
	reused, err := PrepareMetricALT(g, 4)
	if err != nil || !reused.Reused {
		t.Fatalf("reuse = %+v, %v", reused, err)
	}
	for source := 0; source < len(g.Nodes); source += 89 {
		for target := 17; target < len(g.Nodes); target += 127 {
			if !validateMetricALTFeasibility(g, source, target) {
				t.Fatalf("infeasible potential for %d -> %d", source, target)
			}
			want, err := Run(context.Background(), g, source, target, Dijkstra)
			if err != nil {
				t.Fatal(err)
			}
			got, err := Run(context.Background(), g, source, target, AegisALT)
			if err != nil {
				t.Fatal(err)
			}
			if got.Stats.Distance != want.Stats.Distance || got.Stats.OptimalityGap != 0 {
				t.Fatalf("%d -> %d: got=%+v want=%+v", source, target, got.Stats, want.Stats)
			}
			if got.Stats.PotentialModel != acbsMetricALTPotentialModel || got.Stats.PotentialLandmarks != 4 {
				t.Fatalf("unexpected potential stats: %+v", got.Stats)
			}
		}
	}
}

func TestMetricALTRequiresPreparationOnNonTinyGraph(t *testing.T) {
	g := gridGraph(t, 80, 80, true)
	g.EdgeCount = metricALTMinimumEdges
	_, err := Run(context.Background(), g, 0, len(g.Nodes)-1, AegisALT)
	if err == nil {
		t.Fatal("aegis-alt ran without preprocessing")
	}
}

func TestMetricALTBoundsAreSymmetricLandmarkDifferences(t *testing.T) {
	index := metricALTIndex{distances: [][]uint64{{0, 4, 10}}}
	forward, backward := index.bounds(0, 2, 1)
	if forward != 6 || backward != 4 {
		t.Fatalf("bounds = %d/%d, want 6/4", forward, backward)
	}
}

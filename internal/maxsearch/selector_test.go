package maxsearch

import (
	"testing"

	"github.com/lasder-ca/aegis-acbs/internal/search"
)

func TestSelectSolverAmortizesCH(t *testing.T) {
	profiles := []SolverProfile{
		{Algorithm: search.Aegis, QueryNS: 892000},
		{Algorithm: RoutingKitCH, QueryNS: 173000, PreprocessNS: 66800000, UpdateNS: 66800000},
		{Algorithm: RoutingKitCCH, QueryNS: 188000, PreprocessNS: 457600000, UpdateNS: 25000000},
	}

	short, err := SelectSolver(profiles, WorkloadHorizon{Queries: 20})
	if err != nil {
		t.Fatal(err)
	}
	if short.Selected != search.Aegis {
		t.Fatalf("short workload selected %q, want aegis", short.Selected)
	}

	long, err := SelectSolver(profiles, WorkloadHorizon{Queries: 120})
	if err != nil {
		t.Fatal(err)
	}
	if long.Selected != RoutingKitCH {
		t.Fatalf("long workload selected %q, want routingkit-ch", long.Selected)
	}
}

func TestSelectSolverAccountsForUpdates(t *testing.T) {
	profiles := []SolverProfile{
		{Algorithm: RoutingKitCH, QueryNS: 100000, PreprocessNS: 100000000, UpdateNS: 100000000},
		{Algorithm: RoutingKitCCH, QueryNS: 130000, PreprocessNS: 180000000, UpdateNS: 5000000},
		{Algorithm: search.Aegis, QueryNS: 900000},
	}
	selection, err := SelectSolver(profiles, WorkloadHorizon{Queries: 1000, MetricUpdates: 8})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Selected != RoutingKitCCH {
		t.Fatalf("update-heavy workload selected %q, want routingkit-cch", selection.Selected)
	}
}

func TestSelectSolverRejectsInvalidHorizon(t *testing.T) {
	_, err := SelectSolver([]SolverProfile{{Algorithm: search.Aegis, QueryNS: 1}}, WorkloadHorizon{})
	if err == nil {
		t.Fatal("expected error for zero-query horizon")
	}
}

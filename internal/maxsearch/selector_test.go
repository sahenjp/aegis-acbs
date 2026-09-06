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
	if short.Statistic != SelectionMean {
		t.Fatalf("default statistic = %q, want mean", short.Statistic)
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

func TestSelectSolverTailStatisticCanChangeWinner(t *testing.T) {
	profiles := []SolverProfile{
		{Algorithm: search.Aegis, QueryNS: 100, QueryP95NS: 1000, QueryP99NS: 2000},
		{Algorithm: RoutingKitCH, QueryNS: 150, QueryP95NS: 160, QueryP99NS: 170},
	}
	horizon := WorkloadHorizon{Queries: 100}

	mean, err := SelectSolverByStatistic(profiles, horizon, SelectionMean)
	if err != nil {
		t.Fatal(err)
	}
	if mean.Selected != search.Aegis {
		t.Fatalf("mean selected %q, want aegis", mean.Selected)
	}

	p95, err := SelectSolverByStatistic(profiles, horizon, SelectionP95)
	if err != nil {
		t.Fatal(err)
	}
	if p95.Selected != RoutingKitCH || p95.Statistic != SelectionP95 {
		t.Fatalf("p95 selected %q with statistic %q, want routingkit-ch/p95", p95.Selected, p95.Statistic)
	}

	p99, err := SelectSolverByStatistic(profiles, horizon, SelectionP99)
	if err != nil {
		t.Fatal(err)
	}
	if p99.Selected != RoutingKitCH || p99.Statistic != SelectionP99 {
		t.Fatalf("p99 selected %q with statistic %q, want routingkit-ch/p99", p99.Selected, p99.Statistic)
	}
}

func TestSelectSolverTailStatisticRequiresEvidence(t *testing.T) {
	_, err := SelectSolverByStatistic(
		[]SolverProfile{{Algorithm: search.Aegis, QueryNS: 1}},
		WorkloadHorizon{Queries: 1},
		SelectionP95,
	)
	if err == nil {
		t.Fatal("expected error when p95 evidence is missing")
	}
}

func TestSelectSolverRejectsInvalidHorizon(t *testing.T) {
	_, err := SelectSolver([]SolverProfile{{Algorithm: search.Aegis, QueryNS: 1}}, WorkloadHorizon{})
	if err == nil {
		t.Fatal("expected error for zero-query horizon")
	}
}

package search

import (
	"context"
	"math"
	"testing"
)

func seededACBSEntropicScheduler(rateF, rateB float64, nF, nB uint64) acbsEntropicScheduler {
	return acbsEntropicScheduler{
		forward:  acbsProofRate{logMean: math.Log(rateF), samples: nF},
		backward: acbsProofRate{logMean: math.Log(rateB), samples: nB},
	}
}

func TestACBSEntropicSymmetricStateProducesHalfAllocation(t *testing.T) {
	s := seededACBSEntropicScheduler(2, 2, 20, 20)
	d := s.choose(item{priority: 100}, item{priority: 100}, 0, false)
	if math.Abs(d.forwardP-0.5) > 1e-12 {
		t.Fatalf("forward allocation = %f", d.forwardP)
	}
}

func TestACBSEntropicLowerPriorityFrontierGetsProofPressure(t *testing.T) {
	s := seededACBSEntropicScheduler(2, 2, 20, 20)
	d := s.choose(item{priority: 100}, item{priority: 200}, 0, false)
	if d.forwardP <= 0.5 {
		t.Fatalf("expected forward preference, allocation = %f", d.forwardP)
	}
}

func TestACBSEntropicHigherProofRateGetsPreference(t *testing.T) {
	s := seededACBSEntropicScheduler(4, 1, 20, 20)
	d := s.choose(item{priority: 100}, item{priority: 100}, 0, false)
	if d.forwardP <= 0.5 {
		t.Fatalf("expected forward preference, allocation = %f", d.forwardP)
	}
}

func TestACBSEntropicExplorationPrefersUnderSampledDirection(t *testing.T) {
	s := seededACBSEntropicScheduler(1, 1, 2, 100)
	d := s.choose(item{priority: 100}, item{priority: 100}, 0, false)
	if d.forwardP <= 0.5 {
		t.Fatalf("expected exploration preference, allocation = %f", d.forwardP)
	}
}

func TestACBSEntropicAllocationStaysInsideSimplexInterior(t *testing.T) {
	cases := []acbsEntropicScheduler{
		seededACBSEntropicScheduler(1e9, 1e-9, 100, 100),
		seededACBSEntropicScheduler(1e-9, 1e9, 100, 100),
	}
	for i := range cases {
		d := cases[i].choose(item{priority: 1}, item{priority: math.MaxUint64}, 0, false)
		if d.forwardP < acbsMinimumDirectionShare-1e-12 || d.forwardP > 1-acbsMinimumDirectionShare+1e-12 {
			t.Fatalf("allocation escaped interior: %f", d.forwardP)
		}
	}
}

func TestACBSEntropicDebtRoundingHasSubChunkPrefixDiscrepancy(t *testing.T) {
	s := seededACBSEntropicScheduler(3, 1, 50, 50)
	var expected float64
	actual := 0
	maxRun := 0
	run := 0
	last := byte(0)
	for k := 0; k < 10_000; k++ {
		d := s.choose(item{priority: 100}, item{priority: 100}, last, false)
		expected += d.forwardP
		if d.direction == 'F' {
			actual++
		}
		if d.direction == last {
			run++
		} else {
			last = d.direction
			run = 1
		}
		if run > maxRun {
			maxRun = run
		}
		if math.Abs(float64(actual)-expected) >= 1.0+1e-12 {
			t.Fatalf("prefix %d discrepancy = %f", k, float64(actual)-expected)
		}
	}
	if maxRun > 8 {
		t.Fatalf("minimum-share fairness failed, max run = %d", maxRun)
	}
}

func TestACBSEntropicProofRateUsesRobustLogUpdate(t *testing.T) {
	var r acbsProofRate
	r.update(99, 99)
	before := r.rate()
	r.update(math.MaxUint64/2, 1)
	after := r.rate()
	limit := before*math.Pow(4, acbsProofRateAlpha) + 1e-12
	if after > limit {
		t.Fatalf("outlier moved rate too far: before=%f after=%f limit=%f", before, after, limit)
	}
}

func TestACBSEntropicBudgetIsSmoothAndCapped(t *testing.T) {
	base := acbsBaseEdgeBudget(1_000_000)
	if got := acbsEntropicEdgeBudget(1_000_000, 0, false); got != base {
		t.Fatalf("maximum-entropy budget = %d", got)
	}
	mid := acbsEntropicEdgeBudget(1_000_000, 0.5, false)
	if mid <= base || mid >= 4*base {
		t.Fatalf("mid-certainty budget = %d", mid)
	}
	if got := acbsEntropicEdgeBudget(1_000_000, 1, false); got != 4*base {
		t.Fatalf("certain budget = %d", got)
	}
	if got := acbsEntropicEdgeBudget(1_000_000, 1, true); got != 2*base {
		t.Fatalf("incumbent budget = %d", got)
	}
}

func TestACBSEntropicDirectionalGainIgnoresOppositeQueueMovement(t *testing.T) {
	if got := acbsDirectionalGain('F', 10, 15, 20, 100); got != 5 {
		t.Fatalf("forward gain = %d", got)
	}
	if got := acbsDirectionalGain('B', 10, 100, 20, 27); got != 7 {
		t.Fatalf("backward gain = %d", got)
	}
}

func TestACBSEntropicVariantReportsCandidateVersion(t *testing.T) {
	g := gridGraph(t, 24, 24, true)
	r, err := Run(context.Background(), g, 0, len(g.Nodes)-1, AegisEntropic)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Stats.Reachable || !Validate(g, 0, len(g.Nodes)-1, r) {
		t.Fatalf("invalid result: %+v", r.Stats)
	}
	if r.Stats.SchedulerVersion != acbsEntropicSchedulerVersion {
		t.Fatalf("scheduler version = %q", r.Stats.SchedulerVersion)
	}
	if r.Stats.OptimalityGap != 0 {
		t.Fatalf("optimality gap = %d", r.Stats.OptimalityGap)
	}
}

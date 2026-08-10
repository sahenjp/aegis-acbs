package search

import (
	"context"
	"math/rand"
	"testing"
)

func TestMinHeapOrdering(t *testing.T) {
	rng := rand.New(rand.NewSource(5050))
	var h minHeap
	items := make([]item, 10_000)
	for i := range items {
		items[i] = item{
			node:     i,
			priority: uint64(rng.Intn(1_000)),
			distance: uint64(rng.Intn(1_000)),
		}
		push(&h, items[i])
	}
	previous := pop(&h)
	for h.Len() > 0 {
		current := pop(&h)
		if lessItem(current, previous) {
			t.Fatalf("heap order regressed: previous=%+v current=%+v", previous, current)
		}
		previous = current
	}
}

func TestACBSSteadyStateAllocationsRemainBounded(t *testing.T) {
	g := gridGraph(t, 120, 120, true)
	ctx := context.Background()
	if _, err := Run(ctx, g, 0, len(g.Nodes)-1, Aegis); err != nil {
		t.Fatal(err)
	}
	allocations := testing.AllocsPerRun(20, func() {
		r, err := Run(ctx, g, 0, len(g.Nodes)-1, Aegis)
		if err != nil || !r.Stats.Reachable {
			t.Fatalf("ACBS failed: reachable=%v err=%v", r.Stats.Reachable, err)
		}
	})
	// One exact-sized path slice is expected without instrumentation. The race
	// detector adds implementation- and architecture-dependent synchronization
	// allocations. Keep the normal-build ceiling strict; the wider race-only
	// ceiling prevents instrumentation variance from blocking correctness/race
	// evaluation on arm64 runners.
	limit := 8.0
	if testRaceEnabled {
		limit = 160
	}
	if allocations > limit {
		t.Fatalf("steady-state ACBS allocations too high: %.2f (limit %.0f, race=%v)", allocations, limit, testRaceEnabled)
	}
}

func BenchmarkACBSLargeGrid(b *testing.B) {
	g := gridGraph(b, 180, 180, true)
	ctx := context.Background()
	if _, err := Run(ctx, g, 0, len(g.Nodes)-1, Aegis); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r, err := Run(ctx, g, 0, len(g.Nodes)-1, Aegis)
		if err != nil || !r.Stats.Reachable {
			b.Fatalf("ACBS failed: reachable=%v err=%v", r.Stats.Reachable, err)
		}
	}
}

func BenchmarkACBSProjectionLargeGrid(b *testing.B) {
	g := gridGraph(b, 180, 180, true)
	ctx := context.Background()
	if _, err := Run(ctx, g, 0, len(g.Nodes)-1, AegisProjection); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r, err := Run(ctx, g, 0, len(g.Nodes)-1, AegisProjection)
		if err != nil || !r.Stats.Reachable {
			b.Fatalf("ACBS projection failed: reachable=%v err=%v", r.Stats.Reachable, err)
		}
	}
}

func TestRadixHeapMonotoneOrdering(t *testing.T) {
	rng := rand.New(rand.NewSource(6060))
	var h radixHeap
	for i := 0; i < 20_000; i++ {
		radixPush(&h, item{node: i, priority: uint64(rng.Intn(1_000_000)), distance: uint64(i)})
	}
	var previous uint64
	for h.Len() > 0 {
		current := radixPop(&h)
		if current.priority < previous {
			t.Fatalf("radix order regressed: previous=%d current=%d", previous, current.priority)
		}
		previous = current.priority
	}
}

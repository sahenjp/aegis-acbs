package search

import (
	"context"
	"testing"

	"github.com/sahenjp/aegis-acbs/internal/graph"
)

// TestSpecSection3WorkFormula checks the claim in Section 3.2 of docs/PAPER.md
// that work = relaxed + 4*expanded + 2*queueGrowth, using real traced chunks.
// A specification nobody can check against the code is worth little, so this
// recomputes the value the spec gives and compares it with the trace.
func TestSpecSection3WorkFormula(t *testing.T) {
	g, err := graph.Load("/tmp/osm/tokyo-distance.aegis")
	if err != nil {
		t.Skip("graph missing")
	}
	idx := map[int64]int{}
	for i, nd := range g.Nodes {
		idx[nd.ID] = i
	}
	pairs := [][2]int64{
		{5460554322, 1436554112},
		{1783241055, 764883454},
	}
	checked := 0
	for _, pr := range pairs {
		s, ok1 := idx[pr[0]]
		d, ok2 := idx[pr[1]]
		if !ok1 || !ok2 {
			continue
		}
		var events []ACBSTraceEvent
		ctx := WithACBSTrace(context.Background(), func(e ACBSTraceEvent) {
			events = append(events, e)
		})
		if _, err := Run(ctx, g, s, d, Aegis); err != nil {
			t.Fatalf("run: %v", err)
		}
		for _, e := range events {
			growth := (e.ForwardQueueAfter + e.BackwardQueueAfter) - (e.ForwardQueueBefore + e.BackwardQueueBefore)
			want := e.RelaxedDelta + 4*e.ExpandedDelta
			if growth > 0 {
				want += 2 * uint64(growth)
			}
			if want != e.Work {
				t.Fatalf("chunk %d: spec work=%d trace work=%d (relaxed=%d expanded=%d growth=%d)",
					e.Chunk, want, e.Work, e.RelaxedDelta, e.ExpandedDelta, growth)
			}
			// Section 3.4: the budget must be base, 2*base or 4*base.
			base := acbsBaseEdgeBudget(g.EdgeCount)
			if e.Budget != base && e.Budget != 2*base && e.Budget != 4*base {
				t.Fatalf("chunk %d: budget %d is not base=%d, 2x or 4x", e.Chunk, e.Budget, base)
			}
			checked++
		}
	}
	if checked == 0 {
		t.Skip("no traced chunks")
	}
	t.Logf("verified work formula and budget set on %d chunks", checked)
}

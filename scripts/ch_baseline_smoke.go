//go:build chbaseline

package main

import (
	"fmt"
	"math"
	"os"

	ch "github.com/LdDl/ch"
)

// This tiny directed graph checks the exact API assumptions used by
// ch_baseline.go: user labels survive mapping, edge direction matters, and CH
// preprocessing preserves the weighted shortest-path cost.
func main() {
	g := ch.NewGraph()
	for _, id := range []int64{0, 1, 2, 3} {
		if err := g.CreateVertex(id); err != nil {
			fatal(err)
		}
	}
	for _, edge := range []struct {
		from, to int64
		weight   float64
	}{
		{0, 1, 2},
		{1, 3, 3},
		{0, 2, 10},
		{2, 3, 1},
		{1, 2, 20},
	} {
		if err := g.AddEdge(edge.from, edge.to, edge.weight); err != nil {
			fatal(err)
		}
	}
	g.PrepareContractionHierarchies()

	cost, path := g.ShortestPath(0, 3)
	if math.Abs(cost-5) > 1e-9 {
		fatal(fmt.Errorf("0->3 cost=%v path=%v, want 5", cost, path))
	}
	reverseCost, _ := g.ShortestPath(3, 0)
	if reverseCost >= 0 {
		fatal(fmt.Errorf("3->0 unexpectedly reachable with cost %v", reverseCost))
	}
	fmt.Printf("CH smoke ok: cost=%.0f path=%v shortcuts=%d\n", cost, path, g.GetShortcutsNum())
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

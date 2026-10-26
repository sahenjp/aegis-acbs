package search

import (
	"context"

	"github.com/sahenjp/aegis-acbs/internal/graph"
)

type raceResult struct {
	result Result
	err    error
}

func race(ctx context.Context, g *graph.Graph, source, target int) (Result, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ch := make(chan raceResult, 2)
	algs := []Algorithm{BiDijkstra}
	if g.MinCostPerMeter > 0 {
		algs = append(algs, AStar)
	} else {
		algs = append(algs, Dijkstra)
	}
	for _, alg := range algs {
		a := alg
		go func() { r, e := Run(ctx, g, source, target, a); ch <- raceResult{r, e} }()
	}
	first := <-ch
	if first.err == nil {
		cancel()
		winner := first.result.Stats.Algorithm
		first.result.Stats.Algorithm = AegisRace
		first.result.Stats.Selected = winner
		return first.result, nil
	}
	second := <-ch
	if second.err != nil {
		return Result{}, first.err
	}
	winner := second.result.Stats.Algorithm
	second.result.Stats.Algorithm = AegisRace
	second.result.Stats.Selected = winner
	return second.result, nil
}

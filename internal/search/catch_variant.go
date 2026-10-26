package search

import (
	"context"

	"github.com/lasder-ca/aegis-acbs/internal/graph"
)

func catchAB(ctx context.Context, g *graph.Graph, source, target int) (Result, error) {
	return catchRun(ctx, g, source, target, CatchAB)
}

func catchR(ctx context.Context, g *graph.Graph, source, target int) (Result, error) {
	return catchRun(ctx, g, source, target, CatchR)
}

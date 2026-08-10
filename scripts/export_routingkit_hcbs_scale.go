//go:build routingkithcbs

package main

import (
	"bufio"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"os"
	"strconv"
	"strings"

	"github.com/lasder-ca/aegis-acbs/internal/graph"
)

type cell struct{ lat, lon int32 }

func main() {
	graphPath := flag.String("graph", "", "Aegis graph")
	outPath := flag.String("output", "routingkit-hcbs-v2.txt", "output file")
	queries := flag.Int("queries-per-seed", 10000, "queries per seed")
	seedsText := flag.String("seeds", "1010,424242,20260717", "comma-separated seeds")
	flag.Parse()
	if *graphPath == "" || *queries <= 0 {
		fatalf("--graph and positive --queries-per-seed are required")
	}
	g, err := graph.Load(*graphPath)
	if err != nil {
		fatalf("load graph: %v", err)
	}
	if len(g.Nodes) < 2 {
		fatalf("graph needs at least two nodes")
	}
	seeds, err := parseSeeds(*seedsText)
	if err != nil || len(seeds) == 0 {
		fatalf("invalid --seeds: %v", err)
	}
	local := buildBuckets(g, .01)
	regional := buildBuckets(g, .05)

	f, err := os.Create(*outPath)
	if err != nil {
		fatalf("create output: %v", err)
	}
	defer f.Close()
	w := bufio.NewWriterSize(f, 1<<20)
	defer w.Flush()
	fmt.Fprintln(w, "AEGIS_ROUTINGKIT_CH_V2")
	actualEdges := 0
	for from := range g.Nodes {
		actualEdges += len(g.OutEdges(from))
	}
	if actualEdges != g.EdgeCount {
		fatalf("edge count mismatch: iterated=%d metadata=%d", actualEdges, g.EdgeCount)
	}
	totalQueries := *queries * len(seeds)
	fmt.Fprintf(w, "%d %d %d\n", len(g.Nodes), actualEdges, totalQueries)
	for from := range g.Nodes {
		for _, edge := range g.OutEdges(from) {
			if edge.Cost >= 2147483647 {
				fatalf("edge %d->%d cost out of RoutingKit range", from, edge.To)
			}
			fmt.Fprintf(w, "%d %d %d\n", from, edge.To, edge.Cost)
		}
	}
	for _, seed := range seeds {
		rng := rand.New(rand.NewSource(seed))
		for i := 0; i < *queries; i++ {
			source := rng.Intn(len(g.Nodes))
			var target int
			switch i % 3 {
			case 0:
				target = randomOther(rng, len(g.Nodes), source)
			case 1:
				target = bucketOther(rng, g, local, .01, source)
			default:
				target = bucketOther(rng, g, regional, .05, source)
			}
			fmt.Fprintf(w, "%d %d\n", source, target)
		}
	}
	fmt.Printf("routingkit hcbs scale export: nodes=%d edges=%d queries=%d seeds=%v\n", len(g.Nodes), actualEdges, totalQueries, seeds)
}

func parseSeeds(text string) ([]int64, error) {
	parts := strings.Split(text, ",")
	out := make([]int64, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		v, err := strconv.ParseInt(part, 10, 64)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func buildBuckets(g *graph.Graph, size float64) map[cell][]int {
	buckets := make(map[cell][]int, len(g.Nodes)/8)
	for i, node := range g.Nodes {
		key := cellKey(node.Lat, node.Lon, size)
		buckets[key] = append(buckets[key], i)
	}
	return buckets
}

func cellKey(lat, lon, size float64) cell {
	return cell{
		lat: int32(math.Floor((lat + 90) / size)),
		lon: int32(math.Floor((lon + 180) / size)),
	}
}

func bucketOther(rng *rand.Rand, g *graph.Graph, buckets map[cell][]int, size float64, source int) int {
	node := g.Nodes[source]
	candidates := buckets[cellKey(node.Lat, node.Lon, size)]
	if len(candidates) > 1 {
		for attempt := 0; attempt < 8; attempt++ {
			target := candidates[rng.Intn(len(candidates))]
			if target != source {
				return target
			}
		}
	}
	return randomOther(rng, len(g.Nodes), source)
}

func randomOther(rng *rand.Rand, n, source int) int {
	target := rng.Intn(n - 1)
	if target >= source {
		target++
	}
	return target
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

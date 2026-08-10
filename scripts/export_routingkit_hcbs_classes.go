//go:build routingkithcbsclasses

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

type geoCell struct{ lat, lon int32 }

type queryClass uint8

const (
	classGlobal queryClass = iota
	classLocal
	classRegional
)

type classifiedPair struct {
	source int
	target int
	class  queryClass
}

func main() {
	graphPath := flag.String("graph", "", "Aegis graph")
	outPath := flag.String("output", "routingkit-hcbs-classes.txt", "output file")
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
	pairs := make([]classifiedPair, 0, *queries*len(seeds))
	for _, seed := range seeds {
		rng := rand.New(rand.NewSource(seed))
		for i := 0; i < *queries; i++ {
			source := rng.Intn(len(g.Nodes))
			q := classifiedPair{source: source}
			switch i % 3 {
			case 0:
				q.class = classGlobal
				q.target = randomOther(rng, len(g.Nodes), source)
			case 1:
				q.class = classLocal
				q.target = bucketOther(rng, g, local, .01, source)
			default:
				q.class = classRegional
				q.target = bucketOther(rng, g, regional, .05, source)
			}
			pairs = append(pairs, q)
		}
	}

	f, err := os.Create(*outPath)
	if err != nil {
		fatalf("create output: %v", err)
	}
	defer f.Close()
	w := bufio.NewWriterSize(f, 1<<20)
	defer w.Flush()
	actualEdges := 0
	for from := range g.Nodes {
		actualEdges += len(g.OutEdges(from))
	}
	fmt.Fprintln(w, "AEGIS_ROUTINGKIT_HCBS_CLASSES_V1")
	fmt.Fprintf(w, "%d %d %d\n", len(g.Nodes), actualEdges, len(pairs))
	for from := range g.Nodes {
		for _, edge := range g.OutEdges(from) {
			if edge.Cost >= 2147483647 {
				fatalf("edge %d->%d cost out of RoutingKit range", from, edge.To)
			}
			fmt.Fprintf(w, "%d %d %d\n", from, edge.To, edge.Cost)
		}
	}
	for _, q := range pairs {
		fmt.Fprintf(w, "%d %d %d\n", q.source, q.target, q.class)
	}
	fmt.Printf("hcbs class export: nodes=%d edges=%d queries=%d seeds=%v\n", len(g.Nodes), actualEdges, len(pairs), seeds)
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

func buildBuckets(g *graph.Graph, size float64) map[geoCell][]int {
	buckets := make(map[geoCell][]int, len(g.Nodes)/8)
	for i, node := range g.Nodes {
		key := cellKey(node.Lat, node.Lon, size)
		buckets[key] = append(buckets[key], i)
	}
	return buckets
}

func cellKey(lat, lon, size float64) geoCell {
	return geoCell{
		lat: int32(math.Floor((lat + 90) / size)),
		lon: int32(math.Floor((lon + 180) / size)),
	}
}

func bucketOther(rng *rand.Rand, g *graph.Graph, buckets map[geoCell][]int, size float64, source int) int {
	candidates := buckets[cellKey(g.Nodes[source].Lat, g.Nodes[source].Lon, size)]
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

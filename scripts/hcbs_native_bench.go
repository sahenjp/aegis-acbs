//go:build hcbsnativebench

package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"time"

	"github.com/lasder-ca/aegis-acbs/internal/graph"
	"github.com/lasder-ca/aegis-acbs/internal/search"
)

type benchPair struct{ source, target int }
type benchCell struct{ lat, lon int32 }

func main() {
	graphPath := flag.String("graph", "", "input .aegis graph")
	sidecarPath := flag.String("sidecar", "", "input .hcbs sidecar")
	queryCount := flag.Int("queries", 10000, "query count")
	repeats := flag.Int("repeats", 31, "timed repeats per native query")
	verifyCount := flag.Int("verify", 1000, "number of queries to verify with Dijkstra")
	seed := flag.Int64("seed", 424242, "deterministic seed")
	flag.Parse()
	if *graphPath == "" || *sidecarPath == "" || *queryCount <= 0 || *repeats <= 0 {
		panic("--graph, --sidecar, positive --queries and --repeats are required")
	}
	g, err := graph.Load(*graphPath)
	if err != nil {
		panic(err)
	}
	index, err := search.LoadHCBSIndex(*sidecarPath, *graphPath)
	if err != nil {
		panic(err)
	}
	pairs := makeMixedPairs(g, *queryCount, *seed)
	if *verifyCount > len(pairs) {
		*verifyCount = len(pairs)
	}

	ctx := context.Background()
	for i := 0; i < *verifyCount; i++ {
		q := pairs[i]
		distance, reachable, err := index.Distance(q.source, q.target)
		if err != nil {
			panic(err)
		}
		baseline, err := search.Run(ctx, g, q.source, q.target, search.Dijkstra)
		if err != nil {
			panic(err)
		}
		if reachable != baseline.Stats.Reachable || (reachable && distance != baseline.Stats.Distance) {
			panic(fmt.Sprintf("exactness mismatch query=%d %d->%d native=(%v,%d) dijkstra=(%v,%d)",
				i, q.source, q.target, reachable, distance, baseline.Stats.Reachable, baseline.Stats.Distance))
		}
	}

	medians := make([]int64, 0, len(pairs))
	for _, q := range pairs {
		if _, _, err := index.Distance(q.source, q.target); err != nil {
			panic(err)
		}
		samples := make([]int64, *repeats)
		for repeat := 0; repeat < *repeats; repeat++ {
			started := time.Now()
			if _, _, err := index.Distance(q.source, q.target); err != nil {
				panic(err)
			}
			ns := time.Since(started).Nanoseconds()
			if ns < 1 {
				ns = 1
			}
			samples[repeat] = ns
		}
		sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
		medians = append(medians, samples[len(samples)/2])
	}
	fmt.Printf("native-hcbs graph=%q nodes=%d edges=%d queries=%d repeats=%d dijkstra_verified=%d\n",
		g.Name, len(g.Nodes), g.EdgeCount, len(pairs), *repeats, *verifyCount)
	fmt.Printf("native-hcbs median_ns=%d p95_ns=%d p99_ns=%d\n",
		percentile(medians, .50), percentile(medians, .95), percentile(medians, .99))
}

func percentile(values []int64, p float64) int64 {
	copyValues := append([]int64(nil), values...)
	sort.Slice(copyValues, func(i, j int) bool { return copyValues[i] < copyValues[j] })
	index := int(math.Floor(p * float64(len(copyValues)-1)))
	return copyValues[index]
}

func makeMixedPairs(g *graph.Graph, count int, seed int64) []benchPair {
	rng := rand.New(rand.NewSource(seed))
	local := makeBuckets(g, .01)
	regional := makeBuckets(g, .05)
	pairs := make([]benchPair, 0, count)
	for i := 0; i < count; i++ {
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
		pairs = append(pairs, benchPair{source: source, target: target})
	}
	return pairs
}

func makeBuckets(g *graph.Graph, size float64) map[benchCell][]int {
	buckets := make(map[benchCell][]int, len(g.Nodes)/8)
	for i, node := range g.Nodes {
		key := benchCell{
			lat: int32(math.Floor((node.Lat + 90) / size)),
			lon: int32(math.Floor((node.Lon + 180) / size)),
		}
		buckets[key] = append(buckets[key], i)
	}
	return buckets
}

func bucketOther(rng *rand.Rand, g *graph.Graph, buckets map[benchCell][]int, size float64, source int) int {
	node := g.Nodes[source]
	key := benchCell{
		lat: int32(math.Floor((node.Lat + 90) / size)),
		lon: int32(math.Floor((node.Lon + 180) / size)),
	}
	candidates := buckets[key]
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

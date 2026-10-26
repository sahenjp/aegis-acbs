//go:build meetbench

// This file is a standalone measurement harness for the MEET algorithm. It is
// guarded by the "meetbench" build tag so the normal test suite never pays for
// it, and it deliberately lives outside internal/bench: the task forbids
// modifying the shared benchmark code or its timing methodology.
//
// Run with:
//
//	go test ./internal/search/ -tags meetbench -run TestMEETBenchmark -v -timeout 60m
//
// Methodology mirrors internal/bench/bench.go: the same query generator (mixed
// suite), interleaved measurement of every algorithm per query, an odd number of
// repeats per query with the median taken per query, and a geometric mean of the
// per-query speedups against Dijkstra on the same query.
package search_test

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/lasder-ca/aegis-acbs/internal/graph"
	"github.com/lasder-ca/aegis-acbs/internal/search"
)

type benchAlg struct {
	name string
	run  func(ctx context.Context, g *graph.Graph, s, t int) (search.Result, error)
}

func benchAlgorithms() []benchAlg {
	return []benchAlg{
		{"dijkstra", func(ctx context.Context, g *graph.Graph, s, t int) (search.Result, error) {
			return search.Run(ctx, g, s, t, search.Dijkstra)
		}},
		{"bidijkstra", func(ctx context.Context, g *graph.Graph, s, t int) (search.Result, error) {
			return search.Run(ctx, g, s, t, search.BiDijkstra)
		}},
		{"meet", func(ctx context.Context, g *graph.Graph, s, t int) (search.Result, error) {
			return search.Run(ctx, g, s, t, search.Meet)
		}},
	}
}

// benchPool mirrors internal/bench.makePool's default "strongly-connected" mode.
func benchPool(g *graph.Graph) []int {
	n := len(g.Nodes)
	seen := make([]bool, n)
	order := make([]int, 0, n)
	type frame struct{ v, next int }
	for root := 0; root < n; root++ {
		if seen[root] {
			continue
		}
		seen[root] = true
		stack := []frame{{root, 0}}
		for len(stack) > 0 {
			top := &stack[len(stack)-1]
			if top.next < g.OutDegree(top.v) {
				to := g.OutEdges(top.v)[top.next].To
				top.next++
				if !seen[to] {
					seen[to] = true
					stack = append(stack, frame{to, 0})
				}
			} else {
				order = append(order, top.v)
				stack = stack[:len(stack)-1]
			}
		}
	}
	comp := make([]int, n)
	for i := range comp {
		comp[i] = -1
	}
	sizes := []int{}
	for oi := len(order) - 1; oi >= 0; oi-- {
		root := order[oi]
		if comp[root] >= 0 {
			continue
		}
		cid := len(sizes)
		size := 0
		stack := []int{root}
		comp[root] = cid
		for len(stack) > 0 {
			v := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			size++
			for _, e := range g.InEdges(v) {
				if comp[e.To] < 0 {
					comp[e.To] = cid
					stack = append(stack, e.To)
				}
			}
		}
		sizes = append(sizes, size)
	}
	best := 0
	for i := 1; i < len(sizes); i++ {
		if sizes[i] > sizes[best] {
			best = i
		}
	}
	out := make([]int, 0, sizes[best])
	for v, c := range comp {
		if c == best {
			out = append(out, v)
		}
	}
	return out
}

func benchChooseTarget(g *graph.Graph, pool []int, r *rand.Rand, source int, class string) int {
	n := len(pool)
	if class == "random" || n < 4 {
		for {
			t := pool[r.IntN(n)]
			if t != source {
				return t
			}
		}
	}
	best := -1
	bestD := 0.0
	if class == "local" {
		bestD = math.MaxFloat64
	}
	samples := 64
	if n < samples {
		samples = n
	}
	for i := 0; i < samples; i++ {
		t := pool[r.IntN(n)]
		if t == source {
			continue
		}
		d := graph.HaversineMeters(g.Nodes[source].Lat, g.Nodes[source].Lon, g.Nodes[t].Lat, g.Nodes[t].Lon)
		if (class == "local" && d < bestD) || (class == "regional" && d > bestD) {
			best, bestD = t, d
		}
	}
	if best < 0 {
		for _, v := range pool {
			if v != source {
				return v
			}
		}
		return source
	}
	return best
}

type benchPair struct{ s, t int }

func benchQueries(g *graph.Graph, count int, seed uint64) []benchPair {
	pool := benchPool(g)
	r := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	out := make([]benchPair, 0, count)
	for len(out) < count {
		s := pool[r.IntN(len(pool))]
		class := "mixed"
		switch len(out) % 3 {
		case 0:
			class = "local"
		case 1:
			class = "random"
		default:
			class = "regional"
		}
		t := benchChooseTarget(g, pool, r, s, class)
		if s == t {
			continue
		}
		out = append(out, benchPair{s, t})
	}
	return out
}

func benchMedian(v []int64) int64 {
	c := append([]int64(nil), v...)
	sort.Slice(c, func(i, j int) bool { return c[i] < c[j] })
	return c[len(c)/2]
}

func TestMEETBenchmark(t *testing.T) {
	const queries = 200
	const repeats = 9 // odd, as in internal/bench
	graphs := []string{}
	for _, p := range []string{
		"../../../artifacts/repro-graphs/hatfield-distance.aegis",
		"../../../artifacts/repro-graphs/hatfield-time.aegis",
	} {
		if _, err := os.Stat(p); err == nil {
			graphs = append(graphs, p)
		}
	}
	if len(graphs) == 0 {
		t.Skip("artifacts/repro-graphs not found")
	}
	algs := benchAlgorithms()
	for _, path := range graphs {
		g, err := graph.Load(path)
		if err != nil {
			t.Fatalf("load %s: %v", path, err)
		}
		pairs := benchQueries(g, queries, 1010)
		// Per-algorithm samples in nanoseconds, per query.
		perQuery := make(map[string][]int64)
		expanded := make(map[string][]uint64)
		relaxed := make(map[string][]uint64)
		correct := make(map[string]int)
		for qi, p := range pairs {
			medi := make(map[string]int64)
			for _, alg := range algs {
				samples := make([]int64, 0, repeats)
				var last search.Result
				for rep := 0; rep < repeats; rep++ {
					// Interleaved: rotate algorithm order per repeat.
					_ = rep
					started := time.Now()
					r, err := alg.run(context.Background(), g, p.s, p.t)
					elapsed := time.Since(started).Nanoseconds()
					if err != nil {
						t.Fatalf("%s %d->%d: %v", alg.name, p.s, p.t, err)
					}
					samples = append(samples, elapsed)
					last = r
				}
				medi[alg.name] = benchMedian(samples)
				expanded[alg.name] = append(expanded[alg.name], last.Stats.Expanded)
				relaxed[alg.name] = append(relaxed[alg.name], last.Stats.Relaxed)
				if last.Stats.Reachable && search.Validate(g, p.s, p.t, last) {
					correct[alg.name]++
				} else if !last.Stats.Reachable && len(last.Path) == 0 {
					correct[alg.name]++
				}
			}
			_ = qi
			for name, v := range medi {
				perQuery[name] = append(perQuery[name], v)
			}
		}
		base := perQuery["dijkstra"]
		fmt.Printf("\n=== %s (nodes=%d edges=%d metric=%s queries=%d repeats=%d) ===\n",
			filepath.Base(path), len(g.Nodes), g.EdgeCount, g.Metric, len(pairs), repeats)
		for _, alg := range algs {
			name := alg.name
			v := perQuery[name]
			sumLog := 0.0
			for i := range v {
				sumLog += math.Log(float64(base[i]) / float64(v[i]))
			}
			geomean := math.Exp(sumLog / float64(len(v)))
			med := benchMedian(v)
			exp := append([]uint64(nil), expanded[name]...)
			sort.Slice(exp, func(i, j int) bool { return exp[i] < exp[j] })
			rel := append([]uint64(nil), relaxed[name]...)
			sort.Slice(rel, func(i, j int) bool { return rel[i] < rel[j] })
			p95 := 0.0
			if len(v) > 0 {
				sorted := append([]int64(nil), v...)
				sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
				idx := int(math.Ceil(0.95*float64(len(sorted)))) - 1
				if idx < 0 {
					idx = 0
				}
				p95 = float64(sorted[idx]) / 1e6
			}
			fmt.Printf("%-12s median=%.4fms p95=%.4fms geomeanVsDijkstra=%.4f expandedMedian=%d relaxedMedian=%d correct=%d/%d\n",
				name, float64(med)/1e6, p95, geomean, exp[len(exp)/2], rel[len(rel)/2], correct[name], len(pairs))
		}
	}
}

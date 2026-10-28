// Command schedprobe measures scheduler-level lower-bound rise rates for ACBS,
// for the static ablation, and for bidirectional Dijkstra, per query.
//
// It exists only to produce the measurements cited in docs/THEORY.md. It is
// deliberately outside internal/search so that the theory note does not depend
// on exported test hooks.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"strings"

	"github.com/sahenjp/aegis-acbs/internal/graph"
	"github.com/sahenjp/aegis-acbs/internal/search"
)

type chunkRec struct {
	Chunk          uint64 `json:"chunk"`
	Direction      string `json:"direction"`
	BeforeLB       uint64 `json:"beforeLB"`
	AfterLB        uint64 `json:"afterLB"`
	Gain           uint64 `json:"gain"`
	Work           uint64 `json:"work"`
	Expanded       uint64 `json:"expandedDelta"`
	Relaxed        uint64 `json:"relaxedDelta"`
	Stale          uint64 `json:"stalePopsDelta"`
	HadUpperBefore bool   `json:"hadUpperBefore"`
}

type queryRec struct {
	Source   int        `json:"source"`
	Target   int        `json:"target"`
	Class    string     `json:"class"`
	Dist     uint64     `json:"distance"`
	Expanded uint64     `json:"expanded"`
	Relaxed  uint64     `json:"relaxed"`
	Chunks   []chunkRec `json:"chunks"`
}

type algRec struct {
	Name    string     `json:"name"`
	Queries []queryRec `json:"queries"`
}

type outFile struct {
	Graph string   `json:"graph"`
	Nodes int      `json:"nodes"`
	Edges int      `json:"edges"`
	Algs  []algRec `json:"algorithms"`
}

func main() {
	graphPath := flag.String("graph", "", "aegis graph")
	out := flag.String("out", "", "json output")
	queries := flag.Int("queries", 200, "query count")
	seed := flag.Uint64("seed", 424242, "seed")
	pairsPath := flag.String("pairs", "", "optional file of explicit source target index pairs, 0-based internal indices")
	flag.Parse()

	g, err := graph.Load(*graphPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "load:", err)
		os.Exit(1)
	}

	r := rand.New(rand.NewPCG(*seed, *seed^0x9e3779b97f4a7c15))
	n := len(g.Nodes)
	type q struct {
		s, t  int
		class string
	}
	qs := make([]q, 0, *queries)
	if *pairsPath != "" {
		data, err := os.ReadFile(*pairsPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "pairs:", err)
			os.Exit(1)
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var a, b int
			if _, err := fmt.Sscanf(line, "%d %d", &a, &b); err != nil || a < 0 || b < 0 || a >= n || b >= n || a == b {
				fmt.Fprintln(os.Stderr, "bad pair line:", line)
				os.Exit(1)
			}
			qs = append(qs, q{a, b, "explicit"})
		}
	}
	for *pairsPath == "" && len(qs) < *queries {
		s := r.IntN(n)
		class := []string{"local", "random", "regional"}[len(qs)%3]
		best := -1
		bestD := 0.0
		if class == "local" {
			bestD = 1e18
		}
		for i := 0; i < 64; i++ {
			t := r.IntN(n)
			if t == s {
				continue
			}
			d := graph.HaversineMeters(g.Nodes[s].Lat, g.Nodes[s].Lon, g.Nodes[t].Lat, g.Nodes[t].Lon)
			switch class {
			case "local":
				if d < bestD {
					best, bestD = t, d
				}
			case "regional":
				if d > bestD {
					best, bestD = t, d
				}
			default:
				best = t
			}
			if class == "random" {
				break
			}
		}
		if best < 0 || best == s {
			continue
		}
		qs = append(qs, q{s, best, class})
	}

	algs := []search.Algorithm{search.Aegis, search.AegisStatic, search.BiDijkstra}
	result := outFile{Graph: *graphPath, Nodes: len(g.Nodes), Edges: g.EdgeCount}
	for _, alg := range algs {
		rec := algRec{Name: string(alg)}
		for _, qq := range qs {
			var chunks []chunkRec
			ctx := search.WithACBSTrace(context.Background(), func(e search.ACBSTraceEvent) {
				chunks = append(chunks, chunkRec{
					Chunk:          e.Chunk,
					Direction:      e.Direction,
					BeforeLB:       e.BeforeLowerBound,
					AfterLB:        e.AfterLowerBound,
					Gain:           e.LowerBoundGain,
					Work:           e.Work,
					Expanded:       e.ExpandedDelta,
					Relaxed:        e.RelaxedDelta,
					Stale:          e.StalePopsDelta,
					HadUpperBefore: e.HadUpperBoundBefore,
				})
			})
			res, err := search.Run(ctx, g, qq.s, qq.t, alg)
			if err != nil {
				fmt.Fprintln(os.Stderr, "run:", err)
				os.Exit(1)
			}
			rec.Queries = append(rec.Queries, queryRec{
				Source: qq.s, Target: qq.t, Class: qq.class,
				Dist: res.Stats.Distance, Expanded: res.Stats.Expanded, Relaxed: res.Stats.Relaxed,
				Chunks: chunks,
			})
		}
		result.Algs = append(result.Algs, rec)
	}

	f, err := os.Create(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s: %d queries x %d algorithms\n", *out, len(qs), len(algs))

	base := result.Algs[0].Queries
	for _, a := range result.Algs[1:] {
		if len(a.Queries) != len(base) {
			fmt.Fprintf(os.Stderr, "query count mismatch for %s\n", a.Name)
			os.Exit(1)
		}
		for i := range base {
			if a.Queries[i].Dist != base[i].Dist {
				fmt.Fprintf(os.Stderr, "DISTANCE MISMATCH %s query %d: %d vs %d\n", a.Name, i, a.Queries[i].Dist, base[i].Dist)
				os.Exit(1)
			}
		}
	}
	fmt.Println("distance agreement: all algorithms agree on every query")
}

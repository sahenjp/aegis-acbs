//go:build chbaseline

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"runtime"
	"sort"
	"time"

	ch "github.com/LdDl/ch"
	"github.com/lasder-ca/aegis-acbs/internal/graph"
)

type queryPair struct {
	Source int `json:"source"`
	Target int `json:"target"`
}

type sampleStats struct {
	Algorithm string `json:"algorithm"`
	Distance  uint64 `json:"distance"`
	Reachable bool   `json:"reachable"`
}

type sample struct {
	QueryIndex int         `json:"queryIndex"`
	Stats      sampleStats `json:"stats"`
}

type aegisReport struct {
	QueryPairs []queryPair `json:"queryPairs"`
	Samples    []sample    `json:"samples"`
}

type queryResult struct {
	QueryIndex int    `json:"queryIndex"`
	Source     int    `json:"source"`
	Target     int    `json:"target"`
	DurationNS int64  `json:"durationNs"`
	Distance   uint64 `json:"distance"`
	Reachable  bool   `json:"reachable"`
	Correct    bool   `json:"correct"`
}

type outputReport struct {
	Graph          string        `json:"graph"`
	Nodes          int           `json:"nodes"`
	Edges          int           `json:"edges"`
	Queries        int           `json:"queries"`
	Repeats        int           `json:"repeats"`
	PreprocessNS   int64         `json:"preprocessNs"`
	Shortcuts      int64         `json:"shortcuts"`
	HeapAllocBytes uint64        `json:"heapAllocBytes"`
	HeapSysBytes   uint64        `json:"heapSysBytes"`
	MeanNS         int64         `json:"meanNs"`
	MedianNS       int64         `json:"medianNs"`
	P95NS          int64         `json:"p95Ns"`
	P99NS          int64         `json:"p99Ns"`
	AllCorrect     bool          `json:"allCorrect"`
	Results        []queryResult `json:"results"`
}

func main() {
	graphPath := flag.String("graph", "", "Aegis graph file")
	reportPath := flag.String("report", "", "Aegis benchmark JSON containing queryPairs and Dijkstra samples")
	outputPath := flag.String("output", "ch-baseline.json", "output JSON")
	repeats := flag.Int("repeats", 3, "timed repeats per query")
	flag.Parse()
	if *graphPath == "" || *reportPath == "" {
		fatalf("--graph and --report are required")
	}
	if *repeats < 1 {
		fatalf("--repeats must be >= 1")
	}

	g, err := graph.Load(*graphPath)
	if err != nil {
		fatalf("load graph: %v", err)
	}
	var sourceReport aegisReport
	data, err := os.ReadFile(*reportPath)
	if err != nil {
		fatalf("read report: %v", err)
	}
	if err := json.Unmarshal(data, &sourceReport); err != nil {
		fatalf("decode report: %v", err)
	}
	if len(sourceReport.QueryPairs) == 0 {
		fatalf("report contains no query pairs")
	}

	reference := make(map[int]sampleStats, len(sourceReport.QueryPairs))
	for _, s := range sourceReport.Samples {
		if s.Stats.Algorithm == "dijkstra" {
			reference[s.QueryIndex] = s.Stats
		}
	}
	if len(reference) != len(sourceReport.QueryPairs) {
		fatalf("need one Dijkstra reference per query: have %d want %d", len(reference), len(sourceReport.QueryPairs))
	}

	cg := ch.NewGraph()
	for i := range g.Nodes {
		if err := cg.CreateVertex(int64(i)); err != nil {
			fatalf("create vertex %d: %v", i, err)
		}
	}
	for from := range g.Nodes {
		for _, edge := range g.OutEdges(from) {
			if err := cg.AddEdge(int64(from), int64(edge.To), float64(edge.Cost)); err != nil {
				fatalf("add edge %d->%d: %v", from, edge.To, err)
			}
		}
	}

	preprocessStarted := time.Now()
	cg.PrepareContractionHierarchies()
	preprocessNS := positiveNanoseconds(time.Since(preprocessStarted))

	warmups := 3
	if len(sourceReport.QueryPairs) < warmups {
		warmups = len(sourceReport.QueryPairs)
	}
	for i := 0; i < warmups; i++ {
		q := sourceReport.QueryPairs[i]
		cg.ShortestPath(int64(q.Source), int64(q.Target))
	}

	results := make([]queryResult, 0, len(sourceReport.QueryPairs))
	durations := make([]int64, 0, len(sourceReport.QueryPairs))
	allCorrect := true
	for i, q := range sourceReport.QueryPairs {
		runs := make([]int64, *repeats)
		var cost float64
		for repeat := 0; repeat < *repeats; repeat++ {
			started := time.Now()
			cost, _ = cg.ShortestPath(int64(q.Source), int64(q.Target))
			runs[repeat] = positiveNanoseconds(time.Since(started))
		}
		sort.Slice(runs, func(a, b int) bool { return runs[a] < runs[b] })
		duration := runs[len(runs)/2]
		durations = append(durations, duration)

		ref := reference[i]
		reachable := cost >= 0
		distance := uint64(0)
		if reachable {
			if cost > float64(^uint64(0)) {
				fatalf("query %d returned out-of-range cost %v", i, cost)
			}
			distance = uint64(math.Round(cost))
		}
		correct := reachable == ref.Reachable && (!reachable || distance == ref.Distance)
		if !correct {
			allCorrect = false
		}
		results = append(results, queryResult{QueryIndex: i, Source: q.Source, Target: q.Target, DurationNS: duration, Distance: distance, Reachable: reachable, Correct: correct})
	}

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	out := outputReport{Graph: g.Name, Nodes: len(g.Nodes), Edges: g.EdgeCount, Queries: len(sourceReport.QueryPairs), Repeats: *repeats, PreprocessNS: preprocessNS, Shortcuts: cg.GetShortcutsNum(), HeapAllocBytes: mem.Alloc, HeapSysBytes: mem.HeapSys, AllCorrect: allCorrect, Results: results}
	out.MeanNS, out.MedianNS, out.P95NS, out.P99NS = summarize(durations)

	encoded, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		fatalf("encode output: %v", err)
	}
	if err := os.WriteFile(*outputPath, append(encoded, '\n'), 0o644); err != nil {
		fatalf("write output: %v", err)
	}
	fmt.Printf("ch-baseline queries=%d preprocess=%.3fs median=%.3fms p95=%.3fms p99=%.3fms shortcuts=%d correct=%v\n", out.Queries, float64(out.PreprocessNS)/1e9, float64(out.MedianNS)/1e6, float64(out.P95NS)/1e6, float64(out.P99NS)/1e6, out.Shortcuts, out.AllCorrect)
	if !allCorrect {
		os.Exit(2)
	}
}

func positiveNanoseconds(d time.Duration) int64 {
	ns := d.Nanoseconds()
	if ns < 1 {
		return 1
	}
	return ns
}

func summarize(values []int64) (mean, median, p95, p99 int64) {
	if len(values) == 0 {
		return 0, 0, 0, 0
	}
	ordered := append([]int64(nil), values...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	var total int64
	for _, v := range ordered {
		total += v
	}
	mean = total / int64(len(ordered))
	median = percentile(ordered, 0.50)
	p95 = percentile(ordered, 0.95)
	p99 = percentile(ordered, 0.99)
	return
}

func percentile(values []int64, p float64) int64 {
	if len(values) == 0 {
		return 0
	}
	index := int(math.Ceil(p*float64(len(values)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(values) {
		index = len(values) - 1
	}
	return values[index]
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

//go:build routingkitbaseline

package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/lasder-ca/aegis-acbs/internal/graph"
)

const routingKitInfWeight = uint64(2147483647)

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

type benchmarkReport struct {
	QueryPairs []queryPair `json:"queryPairs"`
	Samples    []sample    `json:"samples"`
}

func main() {
	graphPath := flag.String("graph", "", "Aegis graph file")
	reportPath := flag.String("report", "", "Aegis benchmark JSON")
	outputPath := flag.String("output", "routingkit-input.txt", "RoutingKit edge/query input")
	flag.Parse()
	if *graphPath == "" || *reportPath == "" {
		fatalf("--graph and --report are required")
	}

	g, err := graph.Load(*graphPath)
	if err != nil {
		fatalf("load graph: %v", err)
	}
	data, err := os.ReadFile(*reportPath)
	if err != nil {
		fatalf("read report: %v", err)
	}
	var report benchmarkReport
	if err := json.Unmarshal(data, &report); err != nil {
		fatalf("decode report: %v", err)
	}
	if len(report.QueryPairs) == 0 {
		fatalf("report contains no query pairs")
	}

	reference := make(map[int]sampleStats, len(report.QueryPairs))
	for _, s := range report.Samples {
		if s.Stats.Algorithm == "dijkstra" {
			reference[s.QueryIndex] = s.Stats
		}
	}
	if len(reference) != len(report.QueryPairs) {
		fatalf("need one Dijkstra reference per query: have %d want %d", len(reference), len(report.QueryPairs))
	}

	actualEdges := 0
	for from := range g.Nodes {
		for _, edge := range g.OutEdges(from) {
			actualEdges++
			if edge.Cost >= routingKitInfWeight {
				fatalf("edge %d->%d cost %d exceeds RoutingKit finite range", from, edge.To, edge.Cost)
			}
		}
	}
	if actualEdges != g.EdgeCount {
		fatalf("edge count mismatch: iterated %d metadata %d", actualEdges, g.EdgeCount)
	}

	file, err := os.Create(*outputPath)
	if err != nil {
		fatalf("create output: %v", err)
	}
	defer file.Close()
	w := bufio.NewWriterSize(file, 1<<20)
	fmt.Fprintln(w, "AEGIS_ROUTINGKIT_CH_V1")
	fmt.Fprintf(w, "%d %d %d\n", len(g.Nodes), actualEdges, len(report.QueryPairs))
	for from := range g.Nodes {
		for _, edge := range g.OutEdges(from) {
			fmt.Fprintf(w, "%d %d %d\n", from, edge.To, edge.Cost)
		}
	}
	for i, q := range report.QueryPairs {
		ref := reference[i]
		if ref.Reachable && ref.Distance >= routingKitInfWeight {
			fatalf("query %d reference distance %d exceeds RoutingKit finite range", i, ref.Distance)
		}
		reachable := 0
		if ref.Reachable {
			reachable = 1
		}
		fmt.Fprintf(w, "%d %d %d %d\n", q.Source, q.Target, ref.Distance, reachable)
	}
	if err := w.Flush(); err != nil {
		fatalf("flush output: %v", err)
	}
	if err := file.Close(); err != nil {
		fatalf("close output: %v", err)
	}
	fmt.Printf("routingkit export: nodes=%d edges=%d queries=%d\n", len(g.Nodes), actualEdges, len(report.QueryPairs))
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

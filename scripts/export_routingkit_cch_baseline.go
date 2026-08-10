//go:build routingkitcchbaseline

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

type queryPair struct { Source, Target int }
type sampleStats struct { Algorithm string `json:"algorithm"`; Distance uint64 `json:"distance"`; Reachable bool `json:"reachable"` }
type sample struct { QueryIndex int `json:"queryIndex"`; Stats sampleStats `json:"stats"` }
type benchmarkReport struct { QueryPairs []queryPair `json:"queryPairs"`; Samples []sample `json:"samples"` }

func main() {
	graphPath := flag.String("graph", "", "Aegis graph file")
	reportPath := flag.String("report", "", "Aegis benchmark JSON")
	outputPath := flag.String("output", "routingkit-cch-input.txt", "RoutingKit CCH input")
	flag.Parse()
	if *graphPath == "" || *reportPath == "" { fatalf("--graph and --report are required") }
	g, err := graph.Load(*graphPath); if err != nil { fatalf("load graph: %v", err) }
	data, err := os.ReadFile(*reportPath); if err != nil { fatalf("read report: %v", err) }
	var report benchmarkReport; if err := json.Unmarshal(data, &report); err != nil { fatalf("decode report: %v", err) }
	ref := make(map[int]sampleStats, len(report.QueryPairs))
	for _, s := range report.Samples { if s.Stats.Algorithm == "dijkstra" { ref[s.QueryIndex] = s.Stats } }
	if len(ref) != len(report.QueryPairs) { fatalf("need one Dijkstra reference per query") }
	f, err := os.Create(*outputPath); if err != nil { fatalf("create output: %v", err) }; defer f.Close()
	w := bufio.NewWriterSize(f, 1<<20); defer w.Flush()
	fmt.Fprintln(w, "AEGIS_ROUTINGKIT_CCH_V1")
	fmt.Fprintf(w, "%d %d %d\n", len(g.Nodes), g.EdgeCount, len(report.QueryPairs))
	for _, n := range g.Nodes { fmt.Fprintf(w, "%.9f %.9f\n", n.Lat, n.Lon) }
	for from := range g.Nodes { for _, e := range g.OutEdges(from) { if e.Cost >= routingKitInfWeight { fatalf("edge cost out of range") }; fmt.Fprintf(w, "%d %d %d\n", from, e.To, e.Cost) } }
	for i, q := range report.QueryPairs { r := ref[i]; if r.Reachable && r.Distance >= routingKitInfWeight { fatalf("distance out of range") }; reachable := 0; if r.Reachable { reachable = 1 }; fmt.Fprintf(w, "%d %d %d %d\n", q.Source, q.Target, r.Distance, reachable) }
	fmt.Printf("routingkit cch export: nodes=%d edges=%d queries=%d\n", len(g.Nodes), g.EdgeCount, len(report.QueryPairs))
}

func fatalf(format string, args ...any) { fmt.Fprintf(os.Stderr, format+"\n", args...); os.Exit(1) }

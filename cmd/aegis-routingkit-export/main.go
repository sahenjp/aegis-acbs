package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"

	"github.com/lasder-ca/aegis-acbs/internal/graph"
)

const routingKitInfWeight = uint64(2147483647)

func main() {
	graphPath := flag.String("graph", "", "Aegis graph file")
	outputPath := flag.String("output", "", "RoutingKit CH server graph file")
	flag.Parse()
	if *graphPath == "" || *outputPath == "" {
		flag.Usage()
		os.Exit(2)
	}
	g, err := graph.Load(*graphPath)
	if err != nil {
		fatal("load graph: %v", err)
	}
	file, err := os.Create(*outputPath)
	if err != nil {
		fatal("create output: %v", err)
	}
	w := bufio.NewWriterSize(file, 1<<20)
	if _, err := fmt.Fprintln(w, "AEGIS_ROUTINGKIT_CH_SERVER_V1"); err != nil {
		fatal("write magic: %v", err)
	}
	if _, err := fmt.Fprintf(w, "%d %d\n", len(g.Nodes), g.EdgeCount); err != nil {
		fatal("write header: %v", err)
	}
	actualEdges := 0
	for from := range g.Nodes {
		for _, edge := range g.OutEdges(from) {
			if edge.Cost >= routingKitInfWeight {
				fatal("edge %d->%d cost %d exceeds RoutingKit finite range", from, edge.To, edge.Cost)
			}
			if _, err := fmt.Fprintf(w, "%d %d %d\n", from, edge.To, edge.Cost); err != nil {
				fatal("write edge: %v", err)
			}
			actualEdges++
		}
	}
	if actualEdges != g.EdgeCount {
		fatal("edge count mismatch: iterated %d metadata %d", actualEdges, g.EdgeCount)
	}
	if err := w.Flush(); err != nil {
		fatal("flush output: %v", err)
	}
	if err := file.Close(); err != nil {
		fatal("close output: %v", err)
	}
	fmt.Printf("routingkit CH export: nodes=%d edges=%d output=%s\n", len(g.Nodes), actualEdges, *outputPath)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "aegis-routingkit-export: "+format+"\n", args...)
	os.Exit(1)
}

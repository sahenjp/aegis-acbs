package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"

	"github.com/lasder-ca/aegis-acbs/internal/graph"
	"github.com/lasder-ca/aegis-acbs/internal/maxsearch"
)

const routingKitInfWeight = uint64(2147483647)

func main() {
	graphPath := flag.String("graph", "", "Aegis graph file")
	outputPath := flag.String("output", "", "RoutingKit CCH server graph file")
	flag.Parse()
	if *graphPath == "" || *outputPath == "" {
		flag.Usage()
		os.Exit(2)
	}
	g, err := graph.Load(*graphPath)
	if err != nil {
		fatal("load graph: %v", err)
	}
	fingerprint := maxsearch.RoutingKitGraphFingerprint(g)
	file, err := os.Create(*outputPath)
	if err != nil {
		fatal("create output: %v", err)
	}
	w := bufio.NewWriterSize(file, 1<<20)
	if _, err := fmt.Fprintln(w, "AEGIS_ROUTINGKIT_CCH_SERVER_V1"); err != nil {
		fatal("write magic: %v", err)
	}
	if _, err := fmt.Fprintln(w, fingerprint); err != nil {
		fatal("write fingerprint: %v", err)
	}
	if _, err := fmt.Fprintf(w, "%d %d\n", len(g.Nodes), g.EdgeCount); err != nil {
		fatal("write header: %v", err)
	}
	for _, node := range g.Nodes {
		if _, err := fmt.Fprintf(w, "%.9f %.9f\n", node.Lat, node.Lon); err != nil {
			fatal("write node: %v", err)
		}
	}
	actualEdges := 0
	for from := range g.Nodes {
		for _, edge := range g.OutEdges(from) {
			if edge.Cost == 0 || edge.Cost >= routingKitInfWeight {
				fatal("edge %d->%d cost %d is outside RoutingKit finite range", from, edge.To, edge.Cost)
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
	fmt.Printf("routingkit CCH export: nodes=%d edges=%d fingerprint=%s output=%s\n", len(g.Nodes), actualEdges, fingerprint, *outputPath)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "aegis-routingkit-cch-export: "+format+"\n", args...)
	os.Exit(1)
}

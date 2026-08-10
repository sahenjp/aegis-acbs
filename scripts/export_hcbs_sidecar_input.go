//go:build hcbssidecar

package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/lasder-ca/aegis-acbs/internal/graph"
)

func main() {
	graphPath := flag.String("graph", "", "input .aegis graph")
	outPath := flag.String("output", "hcbs-sidecar-input.txt", "output edge-list file")
	flag.Parse()
	if *graphPath == "" {
		fatal("--graph is required")
	}
	g, err := graph.Load(*graphPath)
	if err != nil {
		fatalf("load graph: %v", err)
	}
	digest, err := fileDigest(*graphPath)
	if err != nil {
		fatalf("hash graph: %v", err)
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
	if actualEdges != g.EdgeCount {
		fatalf("edge count mismatch: iterated=%d metadata=%d", actualEdges, g.EdgeCount)
	}
	fmt.Fprintln(w, "AEGIS_HCBS_SIDECAR_INPUT_V1")
	fmt.Fprintln(w, hex.EncodeToString(digest[:]))
	fmt.Fprintf(w, "%d %d\n", len(g.Nodes), actualEdges)
	for from := range g.Nodes {
		for _, edge := range g.OutEdges(from) {
			if edge.Cost >= 1<<31-1 {
				fatalf("edge %d->%d cost %d exceeds HCBS/RoutingKit finite range", from, edge.To, edge.Cost)
			}
			fmt.Fprintf(w, "%d %d %d\n", from, edge.To, edge.Cost)
		}
	}
	fmt.Printf("hcbs sidecar input: nodes=%d edges=%d sha256=%x\n", len(g.Nodes), actualEdges, digest)
}

func fileDigest(path string) ([sha256.Size]byte, error) {
	var digest [sha256.Size]byte
	f, err := os.Open(path)
	if err != nil {
		return digest, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return digest, err
	}
	copy(digest[:], h.Sum(nil))
	return digest, nil
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/lasder-ca/aegis-acbs/internal/graph"
	"github.com/lasder-ca/aegis-acbs/internal/maxsearch"
)

func main() {
	graphPath := flag.String("graph", "", "path to an Aegis graph")
	source := flag.Int("source", -1, "source node index")
	target := flag.Int("target", -1, "target node index")
	parallel := flag.Int("parallel", 3, "maximum exact solvers to run concurrently")
	hedge := flag.Duration("hedge-delay", 0, "delay before starting another exact solver")
	timeout := flag.Duration("timeout", 30*time.Second, "query timeout")
	verify := flag.Bool("verify", true, "validate the winning path before returning")
	flag.Parse()
	if *graphPath == "" || *source < 0 || *target < 0 {
		flag.Usage()
		os.Exit(2)
	}
	g, err := graph.Load(*graphPath)
	if err != nil {
		fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	out, err := maxsearch.Run(ctx, g, *source, *target, maxsearch.Config{HedgeDelay: *hedge, MaxParallel: *parallel, Verify: *verify})
	if err != nil {
		fatal(err)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "aegis-max: %v\n", err)
	os.Exit(1)
}

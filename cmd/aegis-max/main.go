package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/lasder-ca/aegis-acbs/internal/graph"
	"github.com/lasder-ca/aegis-acbs/internal/maxsearch"
	"github.com/lasder-ca/aegis-acbs/internal/search"
)

func main() {
	graphPath := flag.String("graph", "", "path to an Aegis graph")
	source := flag.Int("source", -1, "source node index")
	target := flag.Int("target", -1, "target node index")
	modeText := flag.String("mode", string(maxsearch.ModeLatency), "portfolio mode: latency, balanced, or efficient")
	parallel := flag.Int("parallel", 3, "maximum exact solvers to run concurrently")
	hedge := flag.Duration("hedge-delay", 0, "balanced-mode delay before starting another exact solver")
	timeout := flag.Duration("timeout", 30*time.Second, "query timeout")
	verify := flag.Bool("verify", true, "validate every successful candidate before accepting it")
	consensus := flag.Bool("consensus", false, "require two exact runners to agree on reachability and distance")
	algorithmsText := flag.String("algorithms", "", "optional comma-separated exact algorithm order")
	planOnly := flag.Bool("plan-only", false, "print the deterministic portfolio plan without running a query")
	flag.Parse()
	if *graphPath == "" || *source < 0 || *target < 0 {
		flag.Usage()
		os.Exit(2)
	}
	g, err := graph.Load(*graphPath)
	if err != nil {
		fatal(err)
	}
	cfg := maxsearch.Config{
		Mode:        maxsearch.Mode(*modeText),
		HedgeDelay:  *hedge,
		MaxParallel: *parallel,
		Verify:      *verify,
		Consensus:   *consensus,
		Algorithms:  parseAlgorithms(*algorithmsText),
	}
	if *planOnly {
		plan, err := maxsearch.BuildPlan(g, *source, *target, cfg)
		if err != nil {
			fatal(err)
		}
		if err := encode(plan); err != nil {
			fatal(err)
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	out, err := maxsearch.Run(ctx, g, *source, *target, cfg)
	if err != nil {
		fatal(err)
	}
	if err := encode(out); err != nil {
		fatal(err)
	}
}

func parseAlgorithms(text string) []search.Algorithm {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	parts := strings.Split(text, ",")
	out := make([]search.Algorithm, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, search.Algorithm(part))
		}
	}
	return out
}

func encode(value any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "aegis-max: %v\n", err)
	os.Exit(1)
}

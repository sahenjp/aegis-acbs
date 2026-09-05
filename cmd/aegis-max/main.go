package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/lasder-ca/aegis-acbs/internal/graph"
	"github.com/lasder-ca/aegis-acbs/internal/maxsearch"
	"github.com/lasder-ca/aegis-acbs/internal/search"
)

type routingKitMetadata struct {
	PreprocessNS int64  `json:"preprocessNs"`
	Fingerprint  string `json:"fingerprint"`
}

type outcomeWithRoutingKit struct {
	maxsearch.Outcome
	RoutingKitCH routingKitMetadata `json:"routingKitCH"`
}

func main() {
	graphPath := flag.String("graph", "", "path to an Aegis graph")
	source := flag.Int("source", -1, "source node index")
	target := flag.Int("target", -1, "target node index")
	modeText := flag.String("mode", string(maxsearch.ModeLatency), "portfolio mode: latency, balanced, or efficient")
	parallel := flag.Int("parallel", 3, "maximum exact solvers to run concurrently")
	hedge := flag.Duration("hedge-delay", 0, "balanced-mode delay before starting another exact solver")
	timeout := flag.Duration("timeout", 30*time.Second, "total timeout including optional CH preprocessing")
	verify := flag.Bool("verify", true, "validate every successful candidate before accepting it")
	consensus := flag.Bool("consensus", false, "require two exact runners to agree on reachability and distance")
	algorithmsText := flag.String("algorithms", "", "optional comma-separated exact algorithm order; routingkit-ch is supported when its sidecar flags are set")
	planOnly := flag.Bool("plan-only", false, "print the deterministic portfolio plan without running a query")
	routingKitServer := flag.String("routingkit-ch-server", "", "optional RoutingKit CH sidecar binary")
	routingKitGraph := flag.String("routingkit-ch-graph", "", "graph produced by aegis-routingkit-export for the same Aegis graph")
	flag.Parse()
	if *graphPath == "" || *source < 0 || *target < 0 {
		flag.Usage()
		os.Exit(2)
	}
	if (*routingKitServer == "") != (*routingKitGraph == "") {
		fatal(errors.New("--routingkit-ch-server and --routingkit-ch-graph must be provided together"))
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
	useRoutingKit := *routingKitServer != ""
	if *planOnly {
		plan, err := buildPlan(g, *source, *target, cfg, useRoutingKit)
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
	if !useRoutingKit {
		if containsAlgorithm(cfg.Algorithms, maxsearch.RoutingKitCH) {
			fatal(errors.New("routingkit-ch requires --routingkit-ch-server and --routingkit-ch-graph"))
		}
		out, err := maxsearch.Run(ctx, g, *source, *target, cfg)
		if err != nil {
			fatal(err)
		}
		if err := encode(out); err != nil {
			fatal(err)
		}
		return
	}

	ch, err := maxsearch.NewRoutingKitCHRunner(ctx, *routingKitServer, *routingKitGraph, g)
	if err != nil {
		fatal(err)
	}
	defer func() { _ = ch.Close() }()
	runners, err := runnersWithRoutingKit(g, *source, *target, cfg, ch)
	if err != nil {
		fatal(err)
	}
	out, err := maxsearch.RunWithRunners(ctx, g, *source, *target, cfg, runners)
	if err != nil {
		fatal(err)
	}
	wrapped := outcomeWithRoutingKit{
		Outcome: out,
		RoutingKitCH: routingKitMetadata{
			PreprocessNS: ch.PreprocessDuration().Nanoseconds(),
			Fingerprint:  ch.Fingerprint(),
		},
	}
	if err := encode(wrapped); err != nil {
		fatal(err)
	}
}

func runnersWithRoutingKit(g *graph.Graph, source, target int, cfg maxsearch.Config, ch *maxsearch.RoutingKitCHRunner) ([]maxsearch.Runner, error) {
	algorithms := append([]search.Algorithm(nil), cfg.Algorithms...)
	if len(algorithms) == 0 {
		baseCfg := cfg
		baseCfg.Algorithms = nil
		plan, err := maxsearch.BuildPlan(g, source, target, baseCfg)
		if err != nil {
			return nil, err
		}
		algorithms = []search.Algorithm{maxsearch.RoutingKitCH}
		for _, candidate := range plan.Candidates {
			algorithms = append(algorithms, candidate.Algorithm)
		}
	} else if !containsAlgorithm(algorithms, maxsearch.RoutingKitCH) {
		algorithms = append([]search.Algorithm{maxsearch.RoutingKitCH}, algorithms...)
	}

	runners := make([]maxsearch.Runner, 0, len(algorithms))
	seen := make(map[search.Algorithm]struct{}, len(algorithms))
	for _, algorithm := range algorithms {
		if _, ok := seen[algorithm]; ok {
			continue
		}
		seen[algorithm] = struct{}{}
		if algorithm == maxsearch.RoutingKitCH {
			runners = append(runners, ch)
			continue
		}
		runners = append(runners, maxsearch.BuiltinRunner{Algorithm: algorithm})
	}
	return runners, nil
}

func buildPlan(g *graph.Graph, source, target int, cfg maxsearch.Config, useRoutingKit bool) (maxsearch.Plan, error) {
	if !useRoutingKit {
		if containsAlgorithm(cfg.Algorithms, maxsearch.RoutingKitCH) {
			return maxsearch.Plan{}, errors.New("routingkit-ch requires --routingkit-ch-server and --routingkit-ch-graph")
		}
		return maxsearch.BuildPlan(g, source, target, cfg)
	}
	if len(cfg.Algorithms) > 0 {
		if !containsAlgorithm(cfg.Algorithms, maxsearch.RoutingKitCH) {
			cfg.Algorithms = append([]search.Algorithm{maxsearch.RoutingKitCH}, cfg.Algorithms...)
		}
		return maxsearch.BuildPlan(g, source, target, cfg)
	}
	base, err := maxsearch.BuildPlan(g, source, target, cfg)
	if err != nil {
		return maxsearch.Plan{}, err
	}
	base.Candidates = append([]maxsearch.Candidate{{Algorithm: maxsearch.RoutingKitCH, Role: "preprocessed-primary"}}, base.Candidates...)
	if base.MaxParallel > len(base.Candidates) {
		base.MaxParallel = len(base.Candidates)
	}
	return base, nil
}

func containsAlgorithm(algorithms []search.Algorithm, target search.Algorithm) bool {
	for _, algorithm := range algorithms {
		if algorithm == target {
			return true
		}
	}
	return false
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

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

type routingKitCHMetadata struct {
	PreprocessNS int64  `json:"preprocessNs"`
	Fingerprint  string `json:"fingerprint"`
}

type routingKitCCHMetadata struct {
	OrderNS      int64  `json:"orderNs"`
	TopologyNS   int64  `json:"topologyNs"`
	CustomizeNS  int64  `json:"customizeNs"`
	PreprocessNS int64  `json:"preprocessNs"`
	Fingerprint  string `json:"fingerprint"`
}

type outcomeWithRoutingKit struct {
	maxsearch.Outcome
	RoutingKitCH  *routingKitCHMetadata  `json:"routingKitCH,omitempty"`
	RoutingKitCCH *routingKitCCHMetadata `json:"routingKitCCH,omitempty"`
}

func main() {
	graphPath := flag.String("graph", "", "path to an Aegis graph")
	source := flag.Int("source", -1, "source node index")
	target := flag.Int("target", -1, "target node index")
	modeText := flag.String("mode", string(maxsearch.ModeLatency), "portfolio mode: latency, balanced, or efficient")
	parallel := flag.Int("parallel", 3, "maximum exact solvers to run concurrently")
	hedge := flag.Duration("hedge-delay", 0, "balanced-mode delay before starting another exact solver")
	timeout := flag.Duration("timeout", 30*time.Second, "total timeout including optional RoutingKit preprocessing")
	verify := flag.Bool("verify", true, "validate every successful candidate before accepting it")
	consensus := flag.Bool("consensus", false, "require two exact runners to agree on reachability and distance")
	algorithmsText := flag.String("algorithms", "", "optional comma-separated exact algorithm order; routingkit-cch and routingkit-ch are supported with sidecar flags")
	planOnly := flag.Bool("plan-only", false, "print the deterministic portfolio plan without running a query")
	routingKitCHServer := flag.String("routingkit-ch-server", "", "optional RoutingKit CH sidecar binary")
	routingKitCHGraph := flag.String("routingkit-ch-graph", "", "graph produced by aegis-routingkit-export for the same Aegis graph")
	routingKitCCHServer := flag.String("routingkit-cch-server", "", "optional RoutingKit CCH sidecar binary")
	routingKitCCHGraph := flag.String("routingkit-cch-graph", "", "graph produced by aegis-routingkit-cch-export for the same Aegis graph")
	flag.Parse()
	if *graphPath == "" || *source < 0 || *target < 0 {
		flag.Usage()
		os.Exit(2)
	}
	if (*routingKitCHServer == "") != (*routingKitCHGraph == "") {
		fatal(errors.New("--routingkit-ch-server and --routingkit-ch-graph must be provided together"))
	}
	if (*routingKitCCHServer == "") != (*routingKitCCHGraph == "") {
		fatal(errors.New("--routingkit-cch-server and --routingkit-cch-graph must be provided together"))
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
	useCH := *routingKitCHServer != ""
	useCCH := *routingKitCCHServer != ""
	if containsAlgorithm(cfg.Algorithms, maxsearch.RoutingKitCH) && !useCH {
		fatal(errors.New("routingkit-ch requires --routingkit-ch-server and --routingkit-ch-graph"))
	}
	if containsAlgorithm(cfg.Algorithms, maxsearch.RoutingKitCCH) && !useCCH {
		fatal(errors.New("routingkit-cch requires --routingkit-cch-server and --routingkit-cch-graph"))
	}
	if *planOnly {
		plan, err := buildPlan(g, *source, *target, cfg, useCCH, useCH)
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
	if !useCH && !useCCH {
		out, err := maxsearch.Run(ctx, g, *source, *target, cfg)
		if err != nil {
			fatal(err)
		}
		if err := encode(out); err != nil {
			fatal(err)
		}
		return
	}

	var cch *maxsearch.RoutingKitCCHRunner
	if useCCH {
		cch, err = maxsearch.NewRoutingKitCCHRunner(ctx, *routingKitCCHServer, *routingKitCCHGraph, g)
		if err != nil {
			fatal(err)
		}
		defer func() { _ = cch.Close() }()
	}
	var ch *maxsearch.RoutingKitCHRunner
	if useCH {
		ch, err = maxsearch.NewRoutingKitCHRunner(ctx, *routingKitCHServer, *routingKitCHGraph, g)
		if err != nil {
			fatal(err)
		}
		defer func() { _ = ch.Close() }()
	}

	runners, err := runnersWithRoutingKit(g, *source, *target, cfg, cch, ch)
	if err != nil {
		fatal(err)
	}
	out, err := maxsearch.RunWithRunners(ctx, g, *source, *target, cfg, runners)
	if err != nil {
		fatal(err)
	}
	wrapped := outcomeWithRoutingKit{Outcome: out}
	if ch != nil {
		wrapped.RoutingKitCH = &routingKitCHMetadata{
			PreprocessNS: ch.PreprocessDuration().Nanoseconds(),
			Fingerprint:  ch.Fingerprint(),
		}
	}
	if cch != nil {
		wrapped.RoutingKitCCH = &routingKitCCHMetadata{
			OrderNS:      cch.OrderDuration().Nanoseconds(),
			TopologyNS:   cch.TopologyDuration().Nanoseconds(),
			CustomizeNS:  cch.CustomizeDuration().Nanoseconds(),
			PreprocessNS: cch.PreprocessDuration().Nanoseconds(),
			Fingerprint:  cch.Fingerprint(),
		}
	}
	if err := encode(wrapped); err != nil {
		fatal(err)
	}
}

func runnersWithRoutingKit(g *graph.Graph, source, target int, cfg maxsearch.Config, cch *maxsearch.RoutingKitCCHRunner, ch *maxsearch.RoutingKitCHRunner) ([]maxsearch.Runner, error) {
	algorithms := append([]search.Algorithm(nil), cfg.Algorithms...)
	if len(algorithms) == 0 {
		baseCfg := cfg
		baseCfg.Algorithms = nil
		plan, err := maxsearch.BuildPlan(g, source, target, baseCfg)
		if err != nil {
			return nil, err
		}
		if cch != nil {
			algorithms = append(algorithms, maxsearch.RoutingKitCCH)
		}
		if ch != nil {
			algorithms = append(algorithms, maxsearch.RoutingKitCH)
		}
		for _, candidate := range plan.Candidates {
			algorithms = append(algorithms, candidate.Algorithm)
		}
	} else {
		if cch != nil && !containsAlgorithm(algorithms, maxsearch.RoutingKitCCH) {
			algorithms = append([]search.Algorithm{maxsearch.RoutingKitCCH}, algorithms...)
		}
		if ch != nil && !containsAlgorithm(algorithms, maxsearch.RoutingKitCH) {
			insertAt := 0
			if cch != nil {
				insertAt = 1
			}
			algorithms = append(algorithms, "")
			copy(algorithms[insertAt+1:], algorithms[insertAt:])
			algorithms[insertAt] = maxsearch.RoutingKitCH
		}
	}

	runners := make([]maxsearch.Runner, 0, len(algorithms))
	seen := make(map[search.Algorithm]struct{}, len(algorithms))
	for _, algorithm := range algorithms {
		if _, ok := seen[algorithm]; ok {
			continue
		}
		seen[algorithm] = struct{}{}
		switch algorithm {
		case maxsearch.RoutingKitCCH:
			if cch == nil {
				return nil, errors.New("routingkit-cch selected without a configured CCH sidecar")
			}
			runners = append(runners, cch)
		case maxsearch.RoutingKitCH:
			if ch == nil {
				return nil, errors.New("routingkit-ch selected without a configured CH sidecar")
			}
			runners = append(runners, ch)
		default:
			runners = append(runners, maxsearch.BuiltinRunner{Algorithm: algorithm})
		}
	}
	return runners, nil
}

func buildPlan(g *graph.Graph, source, target int, cfg maxsearch.Config, useCCH, useCH bool) (maxsearch.Plan, error) {
	if len(cfg.Algorithms) > 0 {
		algorithms := append([]search.Algorithm(nil), cfg.Algorithms...)
		if useCCH && !containsAlgorithm(algorithms, maxsearch.RoutingKitCCH) {
			algorithms = append([]search.Algorithm{maxsearch.RoutingKitCCH}, algorithms...)
		}
		if useCH && !containsAlgorithm(algorithms, maxsearch.RoutingKitCH) {
			insertAt := 0
			if useCCH {
				insertAt = 1
			}
			algorithms = append(algorithms, "")
			copy(algorithms[insertAt+1:], algorithms[insertAt:])
			algorithms[insertAt] = maxsearch.RoutingKitCH
		}
		cfg.Algorithms = algorithms
		return maxsearch.BuildPlan(g, source, target, cfg)
	}
	base, err := maxsearch.BuildPlan(g, source, target, cfg)
	if err != nil {
		return maxsearch.Plan{}, err
	}
	prefix := make([]maxsearch.Candidate, 0, 2)
	if useCCH {
		prefix = append(prefix, maxsearch.Candidate{Algorithm: maxsearch.RoutingKitCCH, Role: "customizable-preprocessed-primary"})
	}
	if useCH {
		prefix = append(prefix, maxsearch.Candidate{Algorithm: maxsearch.RoutingKitCH, Role: "preprocessed-primary"})
	}
	base.Candidates = append(prefix, base.Candidates...)
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

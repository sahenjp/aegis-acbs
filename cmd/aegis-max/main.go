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

type altMetadata struct {
	Landmarks          int    `json:"landmarks"`
	PreprocessNS       int64  `json:"preprocessNs"`
	DistanceTableBytes uint64 `json:"distanceTableBytes"`
}

type outcomeWithPreprocessing struct {
	maxsearch.Outcome
	RoutingKitCH  *routingKitCHMetadata  `json:"routingKitCH,omitempty"`
	RoutingKitCCH *routingKitCCHMetadata `json:"routingKitCCH,omitempty"`
	ALT           *altMetadata           `json:"alt,omitempty"`
}

func main() {
	graphPath := flag.String("graph", "", "path to an Aegis graph")
	source := flag.Int("source", -1, "source node index")
	target := flag.Int("target", -1, "target node index")
	modeText := flag.String("mode", string(maxsearch.ModeLatency), "portfolio mode: latency, balanced, or efficient")
	parallel := flag.Int("parallel", 3, "maximum exact solvers to run concurrently")
	hedge := flag.Duration("hedge-delay", 0, "balanced-mode delay before starting another exact solver")
	timeout := flag.Duration("timeout", 30*time.Second, "total timeout including optional preprocessing")
	verify := flag.Bool("verify", true, "validate every successful candidate before accepting it")
	consensus := flag.Bool("consensus", false, "require two exact runners to agree on reachability and distance")
	algorithmsText := flag.String("algorithms", "", "optional comma-separated exact algorithm order; routingkit-cch, routingkit-ch, and alt are supported with preprocessing flags")
	planOnly := flag.Bool("plan-only", false, "print the deterministic portfolio plan without running a query")
	routingKitCHServer := flag.String("routingkit-ch-server", "", "optional RoutingKit CH sidecar binary")
	routingKitCHGraph := flag.String("routingkit-ch-graph", "", "graph produced by aegis-routingkit-export for the same Aegis graph")
	routingKitCCHServer := flag.String("routingkit-cch-server", "", "optional RoutingKit CCH sidecar binary")
	routingKitCCHGraph := flag.String("routingkit-cch-graph", "", "graph produced by aegis-routingkit-cch-export for the same Aegis graph")
	altLandmarks := flag.Int("alt-landmarks", 0, "build an exact directed ALT runner with this many landmarks (1-32; 0 disables ALT)")
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
	if *altLandmarks < 0 || *altLandmarks > 32 {
		fatal(errors.New("--alt-landmarks must be between 0 and 32"))
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
	useALT := *altLandmarks > 0
	if containsAlgorithm(cfg.Algorithms, maxsearch.RoutingKitCH) && !useCH {
		fatal(errors.New("routingkit-ch requires --routingkit-ch-server and --routingkit-ch-graph"))
	}
	if containsAlgorithm(cfg.Algorithms, maxsearch.RoutingKitCCH) && !useCCH {
		fatal(errors.New("routingkit-cch requires --routingkit-cch-server and --routingkit-cch-graph"))
	}
	if containsAlgorithm(cfg.Algorithms, maxsearch.ALT) && !useALT {
		fatal(errors.New("alt requires --alt-landmarks greater than zero"))
	}
	if *planOnly {
		plan, err := buildPlan(g, *source, *target, cfg, useCCH, useCH, useALT)
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
	if !useCH && !useCCH && !useALT {
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
	var alt *maxsearch.ALTRunner
	if useALT {
		alt, err = maxsearch.NewALTRunner(ctx, g, *altLandmarks)
		if err != nil {
			fatal(err)
		}
	}

	runners, err := runnersWithPreprocessing(g, *source, *target, cfg, cch, ch, alt)
	if err != nil {
		fatal(err)
	}
	out, err := maxsearch.RunWithRunners(ctx, g, *source, *target, cfg, runners)
	if err != nil {
		fatal(err)
	}
	wrapped := outcomeWithPreprocessing{Outcome: out}
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
	if alt != nil {
		wrapped.ALT = &altMetadata{
			Landmarks:          alt.LandmarkCount(),
			PreprocessNS:       alt.PreprocessDuration().Nanoseconds(),
			DistanceTableBytes: alt.DistanceTableBytes(),
		}
	}
	if err := encode(wrapped); err != nil {
		fatal(err)
	}
}

func runnersWithPreprocessing(g *graph.Graph, source, target int, cfg maxsearch.Config, cch *maxsearch.RoutingKitCCHRunner, ch *maxsearch.RoutingKitCHRunner, alt *maxsearch.ALTRunner) ([]maxsearch.Runner, error) {
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
		if alt != nil {
			algorithms = append(algorithms, maxsearch.ALT)
		}
		for _, candidate := range plan.Candidates {
			algorithms = append(algorithms, candidate.Algorithm)
		}
	} else {
		prefix := make([]search.Algorithm, 0, 3)
		if cch != nil && !containsAlgorithm(algorithms, maxsearch.RoutingKitCCH) {
			prefix = append(prefix, maxsearch.RoutingKitCCH)
		}
		if ch != nil && !containsAlgorithm(algorithms, maxsearch.RoutingKitCH) {
			prefix = append(prefix, maxsearch.RoutingKitCH)
		}
		if alt != nil && !containsAlgorithm(algorithms, maxsearch.ALT) {
			prefix = append(prefix, maxsearch.ALT)
		}
		algorithms = append(prefix, algorithms...)
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
		case maxsearch.ALT:
			if alt == nil {
				return nil, errors.New("alt selected without configured landmarks")
			}
			runners = append(runners, alt)
		default:
			runners = append(runners, maxsearch.BuiltinRunner{Algorithm: algorithm})
		}
	}
	return runners, nil
}

func buildPlan(g *graph.Graph, source, target int, cfg maxsearch.Config, useCCH, useCH, useALT bool) (maxsearch.Plan, error) {
	if len(cfg.Algorithms) > 0 {
		prefix := make([]search.Algorithm, 0, 3)
		if useCCH && !containsAlgorithm(cfg.Algorithms, maxsearch.RoutingKitCCH) {
			prefix = append(prefix, maxsearch.RoutingKitCCH)
		}
		if useCH && !containsAlgorithm(cfg.Algorithms, maxsearch.RoutingKitCH) {
			prefix = append(prefix, maxsearch.RoutingKitCH)
		}
		if useALT && !containsAlgorithm(cfg.Algorithms, maxsearch.ALT) {
			prefix = append(prefix, maxsearch.ALT)
		}
		cfg.Algorithms = append(prefix, cfg.Algorithms...)
		return maxsearch.BuildPlan(g, source, target, cfg)
	}
	base, err := maxsearch.BuildPlan(g, source, target, cfg)
	if err != nil {
		return maxsearch.Plan{}, err
	}
	prefix := make([]maxsearch.Candidate, 0, 3)
	if useCCH {
		prefix = append(prefix, maxsearch.Candidate{Algorithm: maxsearch.RoutingKitCCH, Role: "customizable-preprocessed-primary"})
	}
	if useCH {
		prefix = append(prefix, maxsearch.Candidate{Algorithm: maxsearch.RoutingKitCH, Role: "preprocessed-primary"})
	}
	if useALT {
		prefix = append(prefix, maxsearch.Candidate{Algorithm: maxsearch.ALT, Role: "landmark-preprocessed-exact"})
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

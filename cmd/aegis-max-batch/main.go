package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/lasder-ca/aegis-acbs/internal/graph"
	"github.com/lasder-ca/aegis-acbs/internal/maxsearch"
	"github.com/lasder-ca/aegis-acbs/internal/search"
)

type routingKitCHMetadata struct {
	PreprocessNS      int64  `json:"preprocessNs"`
	Fingerprint       string `json:"fingerprint"`
	SidecarGraphBytes int64  `json:"sidecarGraphBytes"`
}

type routingKitCCHMetadata struct {
	OrderNS           int64  `json:"orderNs"`
	TopologyNS        int64  `json:"topologyNs"`
	CustomizeNS       int64  `json:"customizeNs"`
	PreprocessNS      int64  `json:"preprocessNs"`
	Fingerprint       string `json:"fingerprint"`
	SidecarGraphBytes int64  `json:"sidecarGraphBytes"`
}

type altMetadata struct {
	Landmarks          int    `json:"landmarks"`
	PreprocessNS       int64  `json:"preprocessNs"`
	DistanceTableBytes uint64 `json:"distanceTableBytes"`
}

type benchmarkSummary struct {
	RankingByQueryMean []benchmarkRow `json:"rankingByQueryMean"`
}

type benchmarkRow struct {
	Algorithm    search.Algorithm `json:"algorithm"`
	MeanNS       int64            `json:"meanNs"`
	PreprocessNS int64            `json:"preprocessNs"`
	UpdateNS     int64            `json:"updateNs"`
}

type output struct {
	Report        maxsearch.BatchReport      `json:"report"`
	Selection     *maxsearch.SolverSelection `json:"selection,omitempty"`
	RoutingKitCH  *routingKitCHMetadata      `json:"routingKitCH,omitempty"`
	RoutingKitCCH *routingKitCCHMetadata     `json:"routingKitCCH,omitempty"`
	ALT           *altMetadata               `json:"alt,omitempty"`
}

func main() {
	graphPath := flag.String("graph", "", "path to an Aegis graph")
	queriesPath := flag.String("queries", "", "text file containing one 'source target' pair per line")
	routingKitCHServer := flag.String("routingkit-ch-server", "", "optional RoutingKit CH sidecar binary")
	routingKitCHGraph := flag.String("routingkit-ch-graph", "", "graph produced by aegis-routingkit-export")
	routingKitCCHServer := flag.String("routingkit-cch-server", "", "optional RoutingKit CCH sidecar binary")
	routingKitCCHGraph := flag.String("routingkit-cch-graph", "", "graph produced by aegis-routingkit-cch-export")
	altLandmarks := flag.Int("alt-landmarks", 0, "build and reuse an exact directed ALT runner with this many landmarks (1-32; 0 disables ALT)")
	algorithmsText := flag.String("algorithms", "", "comma-separated exact runner order; defaults to configured CH, CCH, ALT, bidijkstra, dijkstra")
	autoSelectBenchmark := flag.String("auto-select-benchmark", "", "summary.json from aegis-max-road-bench.sh; selects one available exact runner for this batch")
	metricUpdates := flag.Int64("metric-updates", 0, "expected metric/edge-weight updates over this batch horizon for adaptive selection")
	consensus := flag.Bool("consensus", false, "require two successful exact runners to agree for every query")
	verify := flag.Bool("verify", true, "validate every successful path")
	summaryOnly := flag.Bool("summary-only", false, "omit per-query samples from JSON output")
	timeout := flag.Duration("timeout", 10*time.Minute, "timeout for preprocessing and the complete batch")
	flag.Parse()
	if *graphPath == "" || *queriesPath == "" {
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
	if *metricUpdates < 0 {
		fatal(errors.New("--metric-updates cannot be negative"))
	}

	configuredCH := *routingKitCHServer != ""
	configuredCCH := *routingKitCCHServer != ""
	configuredALT := *altLandmarks > 0

	g, err := graph.Load(*graphPath)
	if err != nil {
		fatal(err)
	}
	queries, err := readQueries(*queriesPath)
	if err != nil {
		fatal(err)
	}

	algorithms := parseAlgorithms(*algorithmsText)
	var selection *maxsearch.SolverSelection
	if *autoSelectBenchmark != "" {
		if len(algorithms) != 0 {
			fatal(errors.New("--auto-select-benchmark and --algorithms are mutually exclusive"))
		}
		sel, err := selectFromBenchmark(*autoSelectBenchmark, int64(len(queries)), *metricUpdates, configuredCH, configuredCCH, configuredALT)
		if err != nil {
			fatal(err)
		}
		selection = &sel
		algorithms = []search.Algorithm{sel.Selected}
	} else if len(algorithms) == 0 {
		if !configuredCH && !configuredCCH && !configuredALT {
			fatal(errors.New("configure at least one CH, CCH, or ALT preprocessed runner, provide --algorithms, or use --auto-select-benchmark"))
		}
		if configuredCH {
			algorithms = append(algorithms, maxsearch.RoutingKitCH)
		}
		if configuredCCH {
			algorithms = append(algorithms, maxsearch.RoutingKitCCH)
		}
		if configuredALT {
			algorithms = append(algorithms, maxsearch.ALT)
		}
		algorithms = append(algorithms, search.BiDijkstra, search.Dijkstra)
	}

	if containsAlgorithm(algorithms, maxsearch.RoutingKitCCH) && !configuredCCH {
		fatal(errors.New("routingkit-cch selected without a configured CCH sidecar"))
	}
	if containsAlgorithm(algorithms, maxsearch.RoutingKitCH) && !configuredCH {
		fatal(errors.New("routingkit-ch selected without a configured CH sidecar"))
	}
	if containsAlgorithm(algorithms, maxsearch.ALT) && !configuredALT {
		fatal(errors.New("alt selected without --alt-landmarks"))
	}

	useCH := configuredCH && containsAlgorithm(algorithms, maxsearch.RoutingKitCH)
	useCCH := configuredCCH && containsAlgorithm(algorithms, maxsearch.RoutingKitCCH)
	useALT := configuredALT && containsAlgorithm(algorithms, maxsearch.ALT)

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	var ch *maxsearch.RoutingKitCHRunner
	if useCH {
		ch, err = maxsearch.NewRoutingKitCHRunner(ctx, *routingKitCHServer, *routingKitCHGraph, g)
		if err != nil {
			fatal(err)
		}
		defer func() { _ = ch.Close() }()
	}
	var cch *maxsearch.RoutingKitCCHRunner
	if useCCH {
		cch, err = maxsearch.NewRoutingKitCCHRunner(ctx, *routingKitCCHServer, *routingKitCCHGraph, g)
		if err != nil {
			fatal(err)
		}
		defer func() { _ = cch.Close() }()
	}
	var alt *maxsearch.ALTRunner
	if useALT {
		alt, err = maxsearch.NewALTRunner(ctx, g, *altLandmarks)
		if err != nil {
			fatal(err)
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
		case maxsearch.RoutingKitCH:
			if ch == nil {
				fatal(errors.New("routingkit-ch selected without a configured CH sidecar"))
			}
			runners = append(runners, ch)
		case maxsearch.RoutingKitCCH:
			if cch == nil {
				fatal(errors.New("routingkit-cch selected without a configured CCH sidecar"))
			}
			runners = append(runners, cch)
		case maxsearch.ALT:
			if alt == nil {
				fatal(errors.New("alt selected without configured landmarks"))
			}
			runners = append(runners, alt)
		default:
			runners = append(runners, maxsearch.BuiltinRunner{Algorithm: algorithm})
		}
	}
	if *consensus && len(runners) < 2 {
		fatal(errors.New("consensus requires at least two runners"))
	}

	// Efficient mode is deliberate: every stateful preprocessed runner is
	// allowed to finish before another candidate starts, so its index/tables can
	// be reused safely across the complete batch.
	cfg := maxsearch.Config{
		Mode:        maxsearch.ModeEfficient,
		MaxParallel: 1,
		Verify:      *verify,
		Consensus:   *consensus,
		Algorithms:  algorithms,
	}
	report, err := maxsearch.RunBatch(ctx, g, queries, cfg, runners)
	if err != nil {
		fatal(err)
	}
	if *summaryOnly {
		report.Samples = nil
	}
	result := output{Report: report, Selection: selection}
	if ch != nil {
		info, err := os.Stat(*routingKitCHGraph)
		if err != nil {
			fatal(err)
		}
		result.RoutingKitCH = &routingKitCHMetadata{
			PreprocessNS:      ch.PreprocessDuration().Nanoseconds(),
			Fingerprint:       ch.Fingerprint(),
			SidecarGraphBytes: info.Size(),
		}
	}
	if cch != nil {
		info, err := os.Stat(*routingKitCCHGraph)
		if err != nil {
			fatal(err)
		}
		result.RoutingKitCCH = &routingKitCCHMetadata{
			OrderNS:           cch.OrderDuration().Nanoseconds(),
			TopologyNS:        cch.TopologyDuration().Nanoseconds(),
			CustomizeNS:       cch.CustomizeDuration().Nanoseconds(),
			PreprocessNS:      cch.PreprocessDuration().Nanoseconds(),
			Fingerprint:       cch.Fingerprint(),
			SidecarGraphBytes: info.Size(),
		}
	}
	if alt != nil {
		result.ALT = &altMetadata{
			Landmarks:          alt.LandmarkCount(),
			PreprocessNS:       alt.PreprocessDuration().Nanoseconds(),
			DistanceTableBytes: alt.DistanceTableBytes(),
		}
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(result); err != nil {
		fatal(err)
	}
}

func selectFromBenchmark(path string, queries, updates int64, configuredCH, configuredCCH, configuredALT bool) (maxsearch.SolverSelection, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return maxsearch.SolverSelection{}, err
	}
	var summary benchmarkSummary
	if err := json.Unmarshal(data, &summary); err != nil {
		return maxsearch.SolverSelection{}, err
	}
	profiles := make([]maxsearch.SolverProfile, 0, len(summary.RankingByQueryMean))
	for _, row := range summary.RankingByQueryMean {
		available := true
		switch row.Algorithm {
		case maxsearch.RoutingKitCH:
			available = configuredCH
		case maxsearch.RoutingKitCCH:
			available = configuredCCH
		case maxsearch.ALT:
			available = configuredALT
		}
		if !available {
			continue
		}
		profiles = append(profiles, maxsearch.SolverProfile{
			Algorithm: row.Algorithm, QueryNS: row.MeanNS,
			PreprocessNS: row.PreprocessNS, UpdateNS: row.UpdateNS,
		})
	}
	return maxsearch.SelectSolver(profiles, maxsearch.WorkloadHorizon{Queries: queries, MetricUpdates: updates})
}

func readQueries(path string) ([]maxsearch.BatchQuery, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	queries := make([]maxsearch.BatchQuery, 0, 1024)
	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if at := strings.IndexByte(line, '#'); at >= 0 {
			line = strings.TrimSpace(line[:at])
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, fmt.Errorf("queries line %d: expected 'source target'", lineNumber)
		}
		source, err := strconv.Atoi(fields[0])
		if err != nil {
			return nil, fmt.Errorf("queries line %d: invalid source: %w", lineNumber, err)
		}
		target, err := strconv.Atoi(fields[1])
		if err != nil {
			return nil, fmt.Errorf("queries line %d: invalid target: %w", lineNumber, err)
		}
		queries = append(queries, maxsearch.BatchQuery{Source: source, Target: target})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(queries) == 0 {
		return nil, errors.New("queries file contains no query pairs")
	}
	return queries, nil
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

func containsAlgorithm(algorithms []search.Algorithm, target search.Algorithm) bool {
	for _, algorithm := range algorithms {
		if algorithm == target {
			return true
		}
	}
	return false
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "aegis-max-batch: %v\n", err)
	os.Exit(1)
}

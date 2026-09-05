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

type output struct {
	Report        maxsearch.BatchReport  `json:"report"`
	RoutingKitCH  *routingKitCHMetadata  `json:"routingKitCH,omitempty"`
	RoutingKitCCH *routingKitCCHMetadata `json:"routingKitCCH,omitempty"`
	ALT           *altMetadata           `json:"alt,omitempty"`
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
	useCH := *routingKitCHServer != ""
	useCCH := *routingKitCCHServer != ""
	useALT := *altLandmarks > 0
	if !useCH && !useCCH && !useALT {
		fatal(errors.New("configure at least one CH, CCH, or ALT preprocessed runner for persistent batch mode"))
	}

	g, err := graph.Load(*graphPath)
	if err != nil {
		fatal(err)
	}
	queries, err := readQueries(*queriesPath)
	if err != nil {
		fatal(err)
	}
	algorithms := parseAlgorithms(*algorithmsText)
	if len(algorithms) == 0 {
		if useCH {
			algorithms = append(algorithms, maxsearch.RoutingKitCH)
		}
		if useCCH {
			algorithms = append(algorithms, maxsearch.RoutingKitCCH)
		}
		if useALT {
			algorithms = append(algorithms, maxsearch.ALT)
		}
		algorithms = append(algorithms, search.BiDijkstra, search.Dijkstra)
	} else {
		if containsAlgorithm(algorithms, maxsearch.RoutingKitCCH) && !useCCH {
			fatal(errors.New("routingkit-cch selected without a configured CCH sidecar"))
		}
		if containsAlgorithm(algorithms, maxsearch.RoutingKitCH) && !useCH {
			fatal(errors.New("routingkit-ch selected without a configured CH sidecar"))
		}
		if containsAlgorithm(algorithms, maxsearch.ALT) && !useALT {
			fatal(errors.New("alt selected without --alt-landmarks"))
		}
	}

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
	result := output{Report: report}
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

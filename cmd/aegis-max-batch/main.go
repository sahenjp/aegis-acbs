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

type routingKitMetadata struct {
	PreprocessNS     int64  `json:"preprocessNs"`
	Fingerprint      string `json:"fingerprint"`
	SidecarGraphBytes int64  `json:"sidecarGraphBytes"`
}

type output struct {
	Report       maxsearch.BatchReport `json:"report"`
	RoutingKitCH routingKitMetadata     `json:"routingKitCH"`
}

func main() {
	graphPath := flag.String("graph", "", "path to an Aegis graph")
	queriesPath := flag.String("queries", "", "text file containing one 'source target' pair per line")
	routingKitServer := flag.String("routingkit-ch-server", "", "RoutingKit CH sidecar binary")
	routingKitGraph := flag.String("routingkit-ch-graph", "", "graph produced by aegis-routingkit-export")
	algorithmsText := flag.String("algorithms", "routingkit-ch,bidijkstra,dijkstra", "comma-separated exact runner order")
	consensus := flag.Bool("consensus", false, "require two successful exact runners to agree for every query")
	verify := flag.Bool("verify", true, "validate every successful path")
	summaryOnly := flag.Bool("summary-only", false, "omit per-query samples from JSON output")
	timeout := flag.Duration("timeout", 10*time.Minute, "timeout for preprocessing and the complete batch")
	flag.Parse()
	if *graphPath == "" || *queriesPath == "" || *routingKitServer == "" || *routingKitGraph == "" {
		flag.Usage()
		os.Exit(2)
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
		fatal(errors.New("at least one algorithm is required"))
	}
	if !containsAlgorithm(algorithms, maxsearch.RoutingKitCH) {
		algorithms = append([]search.Algorithm{maxsearch.RoutingKitCH}, algorithms...)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	ch, err := maxsearch.NewRoutingKitCHRunner(ctx, *routingKitServer, *routingKitGraph, g)
	if err != nil {
		fatal(err)
	}
	defer func() { _ = ch.Close() }()

	runners := make([]maxsearch.Runner, 0, len(algorithms))
	seen := make(map[search.Algorithm]struct{}, len(algorithms))
	for _, algorithm := range algorithms {
		if _, ok := seen[algorithm]; ok {
			continue
		}
		seen[algorithm] = struct{}{}
		if algorithm == maxsearch.RoutingKitCH {
			runners = append(runners, ch)
		} else {
			runners = append(runners, maxsearch.BuiltinRunner{Algorithm: algorithm})
		}
	}
	if *consensus && len(runners) < 2 {
		fatal(errors.New("consensus requires at least two runners"))
	}

	// Efficient mode is deliberate: queries run sequentially and CH is always
	// allowed to finish before a fallback starts, so the persistent sidecar is
	// not cancelled by a faster competing runner and can be reused safely.
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
	info, err := os.Stat(*routingKitGraph)
	if err != nil {
		fatal(err)
	}
	result := output{
		Report: report,
		RoutingKitCH: routingKitMetadata{
			PreprocessNS:      ch.PreprocessDuration().Nanoseconds(),
			Fingerprint:       ch.Fingerprint(),
			SidecarGraphBytes: info.Size(),
		},
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

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/lasder-ca/aegis-acbs/internal/maxsearch"
	"github.com/lasder-ca/aegis-acbs/internal/search"
)

type benchmarkSummary struct {
	Nodes              int64          `json:"nodes"`
	Edges              int64          `json:"edges"`
	RankingByQueryMean []benchmarkRow `json:"rankingByQueryMean"`
}

type benchmarkRow struct {
	Algorithm    search.Algorithm `json:"algorithm"`
	MeanNS       int64            `json:"meanNs"`
	PreprocessNS int64            `json:"preprocessNs"`
	UpdateNS     int64            `json:"updateNs"`
}

type output struct {
	Nodes int64 `json:"nodes"`
	Edges int64 `json:"edges"`
	maxsearch.SolverSelection
}

func main() {
	input := flag.String("benchmark", "", "road benchmark summary.json")
	queries := flag.Int64("queries", 0, "expected number of queries before the horizon ends")
	updates := flag.Int64("metric-updates", 0, "expected edge-weight/metric updates during the horizon")
	flag.Parse()
	if *input == "" || *queries <= 0 {
		flag.Usage()
		os.Exit(2)
	}

	data, err := os.ReadFile(*input)
	if err != nil {
		fatal(err)
	}
	var summary benchmarkSummary
	if err := json.Unmarshal(data, &summary); err != nil {
		fatal(err)
	}
	if len(summary.RankingByQueryMean) == 0 {
		fatal(errors.New("benchmark contains no solver rows"))
	}

	profiles := make([]maxsearch.SolverProfile, 0, len(summary.RankingByQueryMean))
	for _, row := range summary.RankingByQueryMean {
		if row.MeanNS < 0 || row.PreprocessNS < 0 || row.UpdateNS < 0 {
			fatal(fmt.Errorf("invalid timing for %q", row.Algorithm))
		}
		profiles = append(profiles, maxsearch.SolverProfile{
			Algorithm: row.Algorithm, QueryNS: row.MeanNS,
			PreprocessNS: row.PreprocessNS, UpdateNS: row.UpdateNS,
		})
	}
	selection, err := maxsearch.SelectSolver(profiles, maxsearch.WorkloadHorizon{
		Queries: *queries, MetricUpdates: *updates,
	})
	if err != nil {
		fatal(err)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(output{Nodes: summary.Nodes, Edges: summary.Edges, SolverSelection: selection}); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "aegis-max-select: %v\n", err)
	os.Exit(1)
}

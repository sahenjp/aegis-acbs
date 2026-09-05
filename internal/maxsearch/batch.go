package maxsearch

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/lasder-ca/aegis-acbs/internal/graph"
	"github.com/lasder-ca/aegis-acbs/internal/search"
)

type BatchQuery struct {
	Source int `json:"source"`
	Target int `json:"target"`
}

type BatchSample struct {
	Index            int              `json:"index"`
	Query            BatchQuery       `json:"query"`
	Winner           search.Algorithm `json:"winner"`
	DurationNS       int64            `json:"durationNs"`
	Distance         uint64           `json:"distance"`
	Reachable        bool             `json:"reachable"`
	ConsensusReached bool             `json:"consensusReached"`
	Attempts         []Attempt        `json:"attempts,omitempty"`
}

type BatchSummary struct {
	Queries          int                      `json:"queries"`
	Reachable        int                      `json:"reachable"`
	ConsensusReached int                      `json:"consensusReached"`
	NativeFallbacks  int                      `json:"nativeFallbacks"`
	WinnerCounts     map[search.Algorithm]int `json:"winnerCounts"`
	TotalQueryNS     int64                    `json:"totalQueryNs"`
	MeanNS           int64                    `json:"meanNs"`
	P50NS            int64                    `json:"p50Ns"`
	P95NS            int64                    `json:"p95Ns"`
	P99NS            int64                    `json:"p99Ns"`
	MaxNS            int64                    `json:"maxNs"`
}

type BatchReport struct {
	Summary BatchSummary  `json:"summary"`
	Samples []BatchSample `json:"samples,omitempty"`
	Elapsed time.Duration `json:"elapsed"`
}

// RunBatch executes query pairs sequentially while reusing the supplied Runner
// instances. This is important for preprocessing-based solvers such as CH: the
// expensive index is built once and its query cost can then be measured over a
// realistic workload. ModeEfficient is recommended for stateful sidecars so a
// competing runner cannot cancel a sidecar that should be reused later.
func RunBatch(ctx context.Context, g *graph.Graph, queries []BatchQuery, cfg Config, runners []Runner) (BatchReport, error) {
	if g == nil || len(g.Nodes) == 0 {
		return BatchReport{}, errors.New("maxsearch: batch requires a non-empty graph")
	}
	if len(queries) == 0 {
		return BatchReport{}, errors.New("maxsearch: batch contains no queries")
	}
	if len(runners) == 0 {
		return BatchReport{}, errors.New("maxsearch: batch requires at least one runner")
	}

	started := time.Now()
	report := BatchReport{
		Samples: make([]BatchSample, 0, len(queries)),
		Summary: BatchSummary{WinnerCounts: make(map[search.Algorithm]int)},
	}
	durations := make([]int64, 0, len(queries))
	var total float64

	for index, query := range queries {
		if query.Source < 0 || query.Source >= len(g.Nodes) || query.Target < 0 || query.Target >= len(g.Nodes) {
			report.Elapsed = time.Since(started)
			return report, fmt.Errorf("maxsearch: batch query %d (%d -> %d) is out of range", index, query.Source, query.Target)
		}
		if err := ctx.Err(); err != nil {
			report.Elapsed = time.Since(started)
			return report, err
		}

		outcome, err := RunWithRunners(ctx, g, query.Source, query.Target, cfg, runners)
		if err != nil {
			report.Elapsed = time.Since(started)
			return report, fmt.Errorf("maxsearch: batch query %d (%d -> %d): %w", index, query.Source, query.Target, err)
		}
		durationNS := outcome.Duration.Nanoseconds()
		if durationNS < 1 {
			durationNS = 1
		}
		sample := BatchSample{
			Index:            index,
			Query:            query,
			Winner:           outcome.Winner,
			DurationNS:       durationNS,
			Distance:         outcome.Result.Stats.Distance,
			Reachable:        outcome.Result.Stats.Reachable,
			ConsensusReached: outcome.ConsensusReached,
			Attempts:         outcome.Attempts,
		}
		report.Samples = append(report.Samples, sample)
		report.Summary.Queries++
		if sample.Reachable {
			report.Summary.Reachable++
		}
		if sample.ConsensusReached {
			report.Summary.ConsensusReached++
		}
		if sample.Winner != RoutingKitCH {
			report.Summary.NativeFallbacks++
		}
		report.Summary.WinnerCounts[sample.Winner]++
		durations = append(durations, durationNS)
		total += float64(durationNS)
	}

	sorted := append([]int64(nil), durations...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	report.Summary.TotalQueryNS = durationSum(durations)
	report.Summary.MeanNS = int64(total / float64(len(durations)))
	report.Summary.P50NS = percentileDuration(sorted, 0.50)
	report.Summary.P95NS = percentileDuration(sorted, 0.95)
	report.Summary.P99NS = percentileDuration(sorted, 0.99)
	report.Summary.MaxNS = sorted[len(sorted)-1]
	report.Elapsed = time.Since(started)
	return report, nil
}

func durationSum(values []int64) int64 {
	var total int64
	for _, value := range values {
		if value > 0 && total > int64(^uint64(0)>>1)-value {
			return int64(^uint64(0) >> 1)
		}
		total += value
	}
	return total
}

func percentileDuration(sorted []int64, p float64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	index := int(p * float64(len(sorted)))
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	if index < 0 {
		index = 0
	}
	return sorted[index]
}

package bench

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lasder-ca/aegis-acbs/internal/graph"
	"github.com/lasder-ca/aegis-acbs/internal/search"
	"github.com/lasder-ca/aegis-acbs/internal/version"
)

type StressConfig struct {
	Queries     int              `json:"queries"`
	Workers     int              `json:"workers"`
	Seed        uint64           `json:"seed"`
	Algorithm   search.Algorithm `json:"algorithm"`
	VerifyEvery int              `json:"verifyEvery"`
	Timeout     time.Duration    `json:"-"`
	Suite       string           `json:"suite"`
	PairMode    string           `json:"pairMode"`
}

type StressReport struct {
	Version            string        `json:"version"`
	GeneratedAt        time.Time     `json:"generatedAt"`
	GraphName          string        `json:"graphName"`
	GraphSource        string        `json:"graphSource"`
	Nodes              int           `json:"nodes"`
	Edges              int           `json:"edges"`
	Metric             graph.Metric  `json:"metric"`
	Config             StressConfig  `json:"config"`
	Completed          int           `json:"completed"`
	Reachable          int           `json:"reachable"`
	Verified           int           `json:"verified"`
	Correct            int           `json:"correct"`
	Errors             int           `json:"errors"`
	WallDurationNS     int64         `json:"wallDurationNs"`
	ThroughputQPS      float64       `json:"throughputQps"`
	MeanNS             int64         `json:"meanNs"`
	MedianNS           int64         `json:"medianNs"`
	P95NS              int64         `json:"p95Ns"`
	P99NS              int64         `json:"p99Ns"`
	MaxNS              int64         `json:"maxNs"`
	Memory             MemorySummary `json:"memory"`
	AllVerifiedCorrect bool          `json:"allVerifiedCorrect"`
}

// RunStress executes independent route queries concurrently in one process.
// It is intended to exercise workspace pooling, race safety, long-lived heap
// behavior, and throughput. Every Nth query is checked against Dijkstra; set
// VerifyEvery to 1 for full verification.
func RunStress(parent context.Context, g *graph.Graph, cfg StressConfig) (StressReport, error) {
	if cfg.Queries <= 0 {
		cfg.Queries = 10_000
	}
	if cfg.Workers <= 0 {
		cfg.Workers = runtime.GOMAXPROCS(0)
	}
	if cfg.Workers > 1024 {
		return StressReport{}, errors.New("workers must be <= 1024")
	}
	if cfg.Algorithm == "" {
		cfg.Algorithm = search.Aegis
	}
	if cfg.VerifyEvery < 0 {
		return StressReport{}, errors.New("verify-every must be >= 0")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.Suite == "" {
		cfg.Suite = "mixed"
	}
	if cfg.PairMode == "" {
		cfg.PairMode = "strongly-connected"
	}
	pool := makePool(g, cfg.PairMode)
	if len(pool) < 2 {
		return StressReport{}, errors.New("query pool needs at least two nodes")
	}
	queries := makeQueries(g, pool, cfg.Queries, cfg.Seed, cfg.Suite)
	durations := make([]int64, len(queries))
	var next atomic.Int64
	var completed atomic.Int64
	var reachable atomic.Int64
	var verified atomic.Int64
	var correct atomic.Int64
	var failures atomic.Int64

	started := time.Now()
	var wg sync.WaitGroup
	wg.Add(cfg.Workers)
	for worker := 0; worker < cfg.Workers; worker++ {
		go func() {
			defer wg.Done()
			for {
				i := int(next.Add(1) - 1)
				if i >= len(queries) {
					return
				}
				q := queries[i]
				ctx, cancel := context.WithTimeout(parent, cfg.Timeout)
				result, err := search.Run(ctx, g, q.Source, q.Target, cfg.Algorithm)
				cancel()
				if err != nil || (result.Stats.Reachable && !search.Validate(g, q.Source, q.Target, result)) {
					failures.Add(1)
					continue
				}
				durations[i] = measuredStressNanoseconds(time.Duration(result.Stats.DurationNS))
				completed.Add(1)
				if result.Stats.Reachable {
					reachable.Add(1)
				}
				if cfg.VerifyEvery > 0 && i%cfg.VerifyEvery == 0 {
					verified.Add(1)
					ctx, cancel = context.WithTimeout(parent, cfg.Timeout)
					baseline, baseErr := search.Run(ctx, g, q.Source, q.Target, search.Dijkstra)
					cancel()
					if baseErr == nil && baseline.Stats.Reachable == result.Stats.Reachable && (!result.Stats.Reachable || baseline.Stats.Distance == result.Stats.Distance) {
						correct.Add(1)
					} else {
						failures.Add(1)
					}
				}
			}
		}()
	}
	wg.Wait()
	wallNS := measuredStressNanoseconds(time.Since(started))

	valid := durations[:0]
	var total int64
	for _, duration := range durations {
		if duration > 0 {
			valid = append(valid, duration)
			total += duration
		}
	}
	sort.Slice(valid, func(i, j int) bool { return valid[i] < valid[j] })
	report := StressReport{
		Version: version.Version, GeneratedAt: time.Now().UTC(), GraphName: g.Name,
		GraphSource: g.Source, Nodes: len(g.Nodes), Edges: g.EdgeCount, Metric: g.Metric,
		Config: cfg, Completed: int(completed.Load()), Reachable: int(reachable.Load()),
		Verified: int(verified.Load()), Correct: int(correct.Load()), Errors: int(failures.Load()),
		WallDurationNS: wallNS, Memory: captureMemorySummary(),
	}
	report.ThroughputQPS = float64(report.Completed) * float64(time.Second) / float64(wallNS)
	if len(valid) > 0 {
		report.MeanNS = total / int64(len(valid))
		report.MedianNS = percentileInt64(valid, .5)
		report.P95NS = percentileInt64(valid, .95)
		report.P99NS = percentileInt64(valid, .99)
		report.MaxNS = percentileInt64(valid, 1)
	}
	report.AllVerifiedCorrect = report.Errors == 0 && report.Verified == report.Correct
	if parent.Err() != nil {
		return report, parent.Err()
	}
	return report, nil
}

func measuredStressNanoseconds(duration time.Duration) int64 {
	nanoseconds := duration.Nanoseconds()
	if nanoseconds < 1 {
		return 1
	}
	return nanoseconds
}

func WriteStressJSON(path string, report StressReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

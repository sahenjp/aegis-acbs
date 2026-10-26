package bench

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"math/rand/v2"
	"os"
	"runtime"
	"sort"
	"time"

	"github.com/sahenjp/aegis-acbs/internal/graph"
	"github.com/sahenjp/aegis-acbs/internal/search"
	"github.com/sahenjp/aegis-acbs/internal/version"
)

type Config struct {
	Queries       int                `json:"queries"`
	Seed          uint64             `json:"seed"`
	Algorithms    []search.Algorithm `json:"algorithms"`
	Warmup        int                `json:"warmup"`
	Repeats       int                `json:"repeats"`
	BatchSize     int                `json:"batchSize"`
	Order         string             `json:"order"`
	MeasureMemory bool               `json:"measureMemory"`
	Timeout       time.Duration      `json:"-"`
	Suite         string             `json:"suite"`
	PairMode      string             `json:"pairMode"`
}

type Query struct {
	Source             int     `json:"source"`
	Target             int     `json:"target"`
	StraightLineMeters float64 `json:"straightLineMeters"`
	Class              string  `json:"class"`
}

type Sample struct {
	QueryIndex          int          `json:"queryIndex"`
	QueryClass          string       `json:"queryClass"`
	StraightLineMeters  float64      `json:"straightLineMeters"`
	DistanceRatio       float64      `json:"distanceRatio,omitempty"`
	SourceDegree        int          `json:"sourceDegree,omitempty"`
	TargetReverseDegree int          `json:"targetReverseDegree,omitempty"`
	SourceID            int64        `json:"sourceId"`
	TargetID            int64        `json:"targetId"`
	Stats               search.Stats `json:"stats"`
	Correct             bool         `json:"correct"`
	Error               string       `json:"error,omitempty"`
}

type Summary struct {
	Algorithm                        search.Algorithm `json:"algorithm"`
	Runs                             int              `json:"runs"`
	Reachable                        int              `json:"reachable"`
	Correct                          int              `json:"correct"`
	MedianNS                         int64            `json:"medianNs"`
	MeanNS                           int64            `json:"meanNs"`
	MinNS                            int64            `json:"minNs"`
	MaxNS                            int64            `json:"maxNs"`
	P95NS                            int64            `json:"p95Ns"`
	P99NS                            int64            `json:"p99Ns"`
	MedianEdges                      uint64           `json:"medianRelaxed"`
	MedianExpanded                   uint64           `json:"medianExpanded"`
	MedianQueuePushes                uint64           `json:"medianQueuePushes"`
	MedianQueuePops                  uint64           `json:"medianQueuePops"`
	MedianStalePops                  uint64           `json:"medianStalePops"`
	MedianPrunedAtPop                uint64           `json:"medianPrunedAtPop"`
	MedianPrunedAtRelax              uint64           `json:"medianPrunedAtRelax"`
	MedianBoundPruned                uint64           `json:"medianBoundPruned"`
	MedianAllocBytes                 uint64           `json:"medianAllocBytes"`
	MedianAllocObjects               uint64           `json:"medianAllocObjects"`
	RatioOfMediansVsDijkstra         float64          `json:"ratioOfMediansVsDijkstra"`
	MedianPerQuerySpeedupVsDijkstra  float64          `json:"medianPerQuerySpeedupVsDijkstra"`
	GeomeanPerQuerySpeedupVsDijkstra float64          `json:"geomeanPerQuerySpeedupVsDijkstra"`
	RelaxedReductionPct              float64          `json:"relaxedReductionPct"`
	ExpandedReductionPct             float64          `json:"expandedReductionPct"`
}

type RuntimeComparisonPoint struct {
	QueryIndex                int              `json:"queryIndex"`
	Class                     string           `json:"class"`
	FastestClassicalBaseline  search.Algorithm `json:"fastestClassicalBaseline"`
	RuntimeVsFastestClassical float64          `json:"runtimeVsFastestClassical"`
	ClassicalOracleRegret     float64          `json:"classicalOracleRegret"`
	// Deprecated v0.3-v0.6 aliases retained for report consumers.
	FastestBaseline search.Algorithm `json:"fastestBaseline,omitempty"`
	RelativeRuntime float64          `json:"relativeRuntime,omitempty"`
	OracleRegret    float64          `json:"oracleRegret,omitempty"`
}

type ClassSummary struct {
	Class       string           `json:"class"`
	Algorithm   search.Algorithm `json:"algorithm"`
	Runs        int              `json:"runs"`
	MedianNS    int64            `json:"medianNs"`
	P95NS       int64            `json:"p95Ns"`
	MedianEdges uint64           `json:"medianRelaxed"`
}

type DirectionTotals struct {
	ForwardExpanded  uint64 `json:"forwardExpanded"`
	BackwardExpanded uint64 `json:"backwardExpanded"`
}

type AegisSummary struct {
	Comparisons                     int     `json:"comparisons"`
	MedianRuntimeVsFastestClassical float64 `json:"medianRuntimeVsFastestClassical"`
	P95RuntimeVsFastestClassical    float64 `json:"p95RuntimeVsFastestClassical"`
	MaxRuntimeVsFastestClassical    float64 `json:"maxRuntimeVsFastestClassical"`
	MedianClassicalOracleRegret     float64 `json:"medianClassicalOracleRegret"`
	P95ClassicalOracleRegret        float64 `json:"p95ClassicalOracleRegret"`
	MaxClassicalOracleRegret        float64 `json:"maxClassicalOracleRegret"`
	MedianAbsolutePenaltyNS         int64   `json:"medianAbsolutePenaltyNs"`
	P95AbsolutePenaltyNS            int64   `json:"p95AbsolutePenaltyNs"`
	MaxAbsolutePenaltyNS            int64   `json:"maxAbsolutePenaltyNs"`
	MeaningfulSlowdowns             int     `json:"meaningfulSlowdowns"`
	// Deprecated v0.3-v0.6 aliases retained for report consumers.
	MedianRelativeRuntimeToFastestBaseline float64                    `json:"medianRelativeRuntimeToFastestBaseline,omitempty"`
	P95RelativeRuntimeToFastestBaseline    float64                    `json:"p95RelativeRuntimeToFastestBaseline,omitempty"`
	MaxRelativeRuntimeToFastestBaseline    float64                    `json:"maxRelativeRuntimeToFastestBaseline,omitempty"`
	MedianOracleRegret                     float64                    `json:"medianOracleRegret,omitempty"`
	P95OracleRegret                        float64                    `json:"p95OracleRegret,omitempty"`
	MaxOracleRegret                        float64                    `json:"maxOracleRegret,omitempty"`
	RatioOfMediansVsDijkstra               float64                    `json:"ratioOfMediansVsDijkstra"`
	MedianPerQuerySpeedupVsDijkstra        float64                    `json:"medianPerQuerySpeedupVsDijkstra"`
	GeomeanPerQuerySpeedupVsDijkstra       float64                    `json:"geomeanPerQuerySpeedupVsDijkstra"`
	MedianForwardShare                     float64                    `json:"medianForwardShare"`
	P95ForwardShare                        float64                    `json:"p95ForwardShare"`
	MedianDirectionSwitches                uint64                     `json:"medianDirectionSwitches"`
	MedianChunks                           uint64                     `json:"medianChunks"`
	MedianFirstUpperBoundFraction          float64                    `json:"medianFirstUpperBoundFraction"`
	MedianTerminationTightness             float64                    `json:"medianTerminationTightness"`
	MedianQueuePushes                      uint64                     `json:"medianQueuePushes"`
	MedianQueuePops                        uint64                     `json:"medianQueuePops"`
	MedianStalePops                        uint64                     `json:"medianStalePops"`
	MedianPrunedAtPop                      uint64                     `json:"medianPrunedAtPop"`
	MedianPrunedAtRelax                    uint64                     `json:"medianPrunedAtRelax"`
	MedianBoundPruned                      uint64                     `json:"medianBoundPruned"`
	MedianMeetingChecks                    uint64                     `json:"medianMeetingChecks"`
	MedianConnectionChecks                 uint64                     `json:"medianConnectionChecks"`
	MedianFiniteMeetings                   uint64                     `json:"medianFiniteMeetings"`
	MedianPotentialEvaluations             uint64                     `json:"medianPotentialEvaluations"`
	MedianUpperBoundUpdates                uint64                     `json:"medianUpperBoundUpdates"`
	MedianOptimalityGap                    uint64                     `json:"medianOptimalityGap"`
	DirectionByClass                       map[string]DirectionTotals `json:"directionByClass"`
	RuntimeComparisons                     []RuntimeComparisonPoint   `json:"runtimeComparisons"`
}

type MemorySummary struct {
	Measured          bool   `json:"measured"`
	PeakRSSBytes      uint64 `json:"peakRssBytes"`
	GoHeapAllocBytes  uint64 `json:"goHeapAllocBytes"`
	GoHeapSysBytes    uint64 `json:"goHeapSysBytes"`
	GoTotalAllocBytes uint64 `json:"goTotalAllocBytes"`
	GoMallocs         uint64 `json:"goMallocs"`
	GoNumGC           uint32 `json:"goNumGc"`
}

type Report struct {
	Version           string         `json:"version"`
	GeneratedAt       time.Time      `json:"generatedAt"`
	GoVersion         string         `json:"goVersion"`
	GOOS              string         `json:"goos"`
	GOARCH            string         `json:"goarch"`
	CPUs              int            `json:"cpus"`
	GraphName         string         `json:"graphName"`
	GraphSource       string         `json:"graphSource"`
	Nodes             int            `json:"nodes"`
	Edges             int            `json:"edges"`
	Metric            graph.Metric   `json:"metric"`
	Profile           string         `json:"profile"`
	HeuristicStrength float64        `json:"heuristicStrength"`
	DiameterMeters    float64        `json:"diameterMeters,omitempty"`
	Config            Config         `json:"config"`
	Queries           []Query        `json:"queryPairs"`
	Samples           []Sample       `json:"samples"`
	Summary           []Summary      `json:"summary"`
	ClassSummary      []ClassSummary `json:"classSummary"`
	Aegis             AegisSummary   `json:"aegis"`
	Memory            MemorySummary  `json:"memory"`
	AllCorrect        bool           `json:"allCorrect"`
	QueryPoolSize     int            `json:"queryPoolSize"`
}

func Run(ctx context.Context, g *graph.Graph, cfg Config) (Report, error) {
	if cfg.Queries <= 0 {
		cfg.Queries = 100
	}
	if cfg.Warmup < 0 {
		cfg.Warmup = 0
	}
	if cfg.Repeats <= 0 {
		cfg.Repeats = 7
	}
	if cfg.Repeats > 101 {
		return Report{}, errors.New("repeats must be <= 101")
	}
	if cfg.Repeats%2 == 0 {
		cfg.Repeats++
	}
	if cfg.BatchSize <= 0 {
		switch {
		case len(g.Nodes) < 1_000:
			cfg.BatchSize = 64
		case len(g.Nodes) < 10_000:
			cfg.BatchSize = 16
		case len(g.Nodes) < 100_000:
			cfg.BatchSize = 4
		default:
			cfg.BatchSize = 1
		}
	}
	if cfg.BatchSize > 256 {
		return Report{}, errors.New("batch size must be <= 256")
	}
	if len(cfg.Algorithms) == 0 {
		cfg.Algorithms = []search.Algorithm{search.Dijkstra, search.BiDijkstra}
		if g.MinCostPerMeter > 0 {
			cfg.Algorithms = append(cfg.Algorithms, search.AStar)
		}
		cfg.Algorithms = append(cfg.Algorithms, search.Aegis)
	}
	if len(g.Nodes) < 2 {
		return Report{}, errors.New("graph needs at least two nodes")
	}
	if cfg.Suite == "" {
		cfg.Suite = "mixed"
	}
	if cfg.PairMode == "" {
		cfg.PairMode = "strongly-connected"
	}
	if cfg.Order == "" {
		cfg.Order = "interleaved"
	}
	if cfg.Order != "interleaved" && cfg.Order != "rotated" {
		return Report{}, errors.New("order must be interleaved or rotated")
	}
	pool := makePool(g, cfg.PairMode)
	if len(pool) < 2 {
		return Report{}, errors.New("query pool needs at least two nodes")
	}
	queries := makeQueries(g, pool, cfg.Queries, cfg.Seed, cfg.Suite)
	for i := 0; i < cfg.Warmup && i < len(queries); i++ {
		for _, alg := range cfg.Algorithms {
			_, _ = search.Run(ctx, g, queries[i].Source, queries[i].Target, alg)
		}
	}

	report := Report{
		Version:           version.Version,
		GeneratedAt:       time.Now().UTC(),
		GoVersion:         runtime.Version(),
		GOOS:              runtime.GOOS,
		GOARCH:            runtime.GOARCH,
		CPUs:              runtime.NumCPU(),
		GraphName:         g.Name,
		GraphSource:       g.Source,
		Nodes:             len(g.Nodes),
		Edges:             g.EdgeCount,
		Metric:            g.Metric,
		Profile:           g.Profile,
		HeuristicStrength: g.HeuristicStrength,
		DiameterMeters:    g.DiameterMeters,
		Config:            cfg,
		Queries:           queries,
		AllCorrect:        true,
		QueryPoolSize:     len(pool),
	}

	for qi, q := range queries {
		// Correctness reference is deliberately outside measured samples.
		expected, err := runOne(ctx, g, q, search.Dijkstra, cfg.Timeout)
		if err != nil {
			return report, err
		}
		expectedReachable, expectedDistance := expected.Stats.Reachable, expected.Stats.Distance

		results, runErrors := measureQuery(ctx, g, q, cfg, qi)
		for _, alg := range cfg.Algorithms {
			r := results[alg]
			runErr := runErrors[alg]
			if cfg.MeasureMemory && runErr == nil {
				allocBytes, allocObjects, allocErr := measureAllocations(ctx, g, q, alg, cfg.Timeout)
				if allocErr != nil {
					runErr = allocErr
				} else {
					r.Stats.AllocBytes = allocBytes
					r.Stats.AllocObjects = allocObjects
				}
			}
			correct := runErr == nil && r.Stats.Reachable == expectedReachable && (!expectedReachable || r.Stats.Distance == expectedDistance) && search.Validate(g, q.Source, q.Target, r)
			if !correct {
				report.AllCorrect = false
			}
			distanceRatio := 0.0
			if g.DiameterMeters > 0 {
				distanceRatio = q.StraightLineMeters / g.DiameterMeters
			}
			s := Sample{
				QueryIndex:          qi,
				QueryClass:          q.Class,
				StraightLineMeters:  q.StraightLineMeters,
				DistanceRatio:       distanceRatio,
				SourceDegree:        g.OutDegree(q.Source),
				TargetReverseDegree: g.InDegree(q.Target),
				SourceID:            g.Nodes[q.Source].ID,
				TargetID:            g.Nodes[q.Target].ID,
				Stats:               r.Stats,
				Correct:             correct,
			}
			if runErr != nil {
				s.Error = runErr.Error()
			}
			report.Samples = append(report.Samples, s)
		}
	}

	report.Summary = summarize(report.Samples, cfg.Algorithms)
	report.ClassSummary = summarizeClasses(report.Samples, cfg.Algorithms)
	report.Aegis = summarizeAegis(report.Samples)
	report.Memory = captureMemorySummary()
	return report, nil
}

func measureQuery(parent context.Context, g *graph.Graph, q Query, cfg Config, queryIndex int) (map[search.Algorithm]search.Result, map[search.Algorithm]error) {
	results := make(map[search.Algorithm]search.Result, len(cfg.Algorithms))
	errs := make(map[search.Algorithm]error, len(cfg.Algorithms))
	if cfg.Order == "rotated" {
		for _, alg := range rotated(cfg.Algorithms, queryIndex) {
			r, err := runRepeated(parent, g, q, alg, cfg.Timeout, cfg.Repeats, cfg.BatchSize)
			results[alg], errs[alg] = r, err
		}
		return results, errs
	}

	buckets := make(map[search.Algorithm][]search.Result, len(cfg.Algorithms))
	for repeat := 0; repeat < cfg.Repeats; repeat++ {
		for _, alg := range shuffled(cfg.Algorithms, cfg.Seed, queryIndex, repeat) {
			r, err := runBatch(parent, g, q, alg, cfg.Timeout, cfg.BatchSize)
			if err != nil {
				errs[alg] = err
				continue
			}
			buckets[alg] = append(buckets[alg], r)
		}
	}
	for _, alg := range cfg.Algorithms {
		if errs[alg] != nil {
			continue
		}
		if len(buckets[alg]) != cfg.Repeats {
			errs[alg] = errors.New("incomplete interleaved measurement")
			continue
		}
		results[alg] = medianResult(buckets[alg])
	}
	return results, errs
}

func rotated(in []search.Algorithm, offset int) []search.Algorithm {
	if len(in) == 0 {
		return nil
	}
	out := make([]search.Algorithm, len(in))
	for i := range in {
		out[i] = in[(i+offset)%len(in)]
	}
	return out
}

func shuffled(in []search.Algorithm, seed uint64, queryIndex, repeat int) []search.Algorithm {
	out := append([]search.Algorithm(nil), in...)
	stream := uint64(queryIndex+1)*0x9e3779b97f4a7c15 ^ uint64(repeat+1)*0xbf58476d1ce4e5b9
	r := rand.New(rand.NewPCG(seed^0x243f6a8885a308d3, stream^0x13198a2e03707344))
	r.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}

func runRepeated(parent context.Context, g *graph.Graph, q Query, alg search.Algorithm, timeout time.Duration, repeats, batchSize int) (search.Result, error) {
	results := make([]search.Result, 0, repeats)
	for i := 0; i < repeats; i++ {
		r, err := runBatch(parent, g, q, alg, timeout, batchSize)
		if err != nil {
			return ResultZero(), err
		}
		results = append(results, r)
	}
	return medianResult(results), nil
}

func runBatch(parent context.Context, g *graph.Graph, q Query, alg search.Algorithm, timeout time.Duration, batchSize int) (search.Result, error) {
	var r search.Result
	started := time.Now()
	for batch := 0; batch < batchSize; batch++ {
		var err error
		r, err = runOne(parent, g, q, alg, timeout)
		if err != nil {
			return ResultZero(), err
		}
	}
	r.Stats.DurationNS = time.Since(started).Nanoseconds() / int64(batchSize)
	return r, nil
}

func medianResult(results []search.Result) search.Result {
	sort.Slice(results, func(i, j int) bool { return results[i].Stats.DurationNS < results[j].Stats.DurationNS })
	return results[len(results)/2]
}

func measureAllocations(parent context.Context, g *graph.Graph, q Query, alg search.Algorithm, timeout time.Duration) (uint64, uint64, error) {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, err := runOne(parent, g, q, alg, timeout)
	runtime.ReadMemStats(&after)
	if err != nil {
		return 0, 0, err
	}
	return after.TotalAlloc - before.TotalAlloc, after.Mallocs - before.Mallocs, nil
}

func captureMemorySummary() MemorySummary {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return MemorySummary{
		Measured: true, PeakRSSBytes: readPeakRSSBytes(), GoHeapAllocBytes: stats.HeapAlloc,
		GoHeapSysBytes: stats.HeapSys, GoTotalAllocBytes: stats.TotalAlloc,
		GoMallocs: stats.Mallocs, GoNumGC: stats.NumGC,
	}
}

// ResultZero keeps error paths explicit without exporting an implementation detail.
func ResultZero() search.Result { return search.Result{} }

func runOne(parent context.Context, g *graph.Graph, q Query, alg search.Algorithm, timeout time.Duration) (search.Result, error) {
	ctx := parent
	cancel := func() {}
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(parent, timeout)
	}
	defer cancel()
	return search.Run(ctx, g, q.Source, q.Target, alg)
}

func makePool(g *graph.Graph, mode string) []int {
	if mode == "all" {
		out := make([]int, len(g.Nodes))
		for i := range out {
			out[i] = i
		}
		return out
	}
	// Iterative Kosaraju: query selection only, never included in timings.
	n := len(g.Nodes)
	seen := make([]bool, n)
	order := make([]int, 0, n)
	type frame struct{ v, next int }
	for root := 0; root < n; root++ {
		if seen[root] {
			continue
		}
		seen[root] = true
		stack := []frame{{root, 0}}
		for len(stack) > 0 {
			top := &stack[len(stack)-1]
			if top.next < g.OutDegree(top.v) {
				to := g.OutEdges(top.v)[top.next].To
				top.next++
				if !seen[to] {
					seen[to] = true
					stack = append(stack, frame{to, 0})
				}
			} else {
				order = append(order, top.v)
				stack = stack[:len(stack)-1]
			}
		}
	}
	comp := make([]int, n)
	for i := range comp {
		comp[i] = -1
	}
	sizes := []int{}
	for oi := len(order) - 1; oi >= 0; oi-- {
		root := order[oi]
		if comp[root] >= 0 {
			continue
		}
		cid := len(sizes)
		size := 0
		stack := []int{root}
		comp[root] = cid
		for len(stack) > 0 {
			v := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			size++
			for _, e := range g.InEdges(v) {
				if comp[e.To] < 0 {
					comp[e.To] = cid
					stack = append(stack, e.To)
				}
			}
		}
		sizes = append(sizes, size)
	}
	best := 0
	for i := 1; i < len(sizes); i++ {
		if sizes[i] > sizes[best] {
			best = i
		}
	}
	out := make([]int, 0, sizes[best])
	for v, c := range comp {
		if c == best {
			out = append(out, v)
		}
	}
	return out
}

func makeQueries(g *graph.Graph, pool []int, count int, seed uint64, suite string) []Query {
	r := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	n := len(pool)
	out := make([]Query, 0, count)
	for len(out) < count {
		s := pool[r.IntN(n)]
		class := suite
		if suite == "mixed" {
			switch len(out) % 3 {
			case 0:
				class = "local"
			case 1:
				class = "random"
			default:
				class = "regional"
			}
		}
		t := chooseTarget(g, pool, r, s, class)
		if s == t {
			continue
		}
		d := graph.HaversineMeters(g.Nodes[s].Lat, g.Nodes[s].Lon, g.Nodes[t].Lat, g.Nodes[t].Lon)
		out = append(out, Query{Source: s, Target: t, StraightLineMeters: d, Class: class})
	}
	return out
}

func chooseTarget(g *graph.Graph, pool []int, r *rand.Rand, source int, class string) int {
	n := len(pool)
	if class == "random" || n < 4 {
		for {
			t := pool[r.IntN(n)]
			if t != source {
				return t
			}
		}
	}
	best := -1
	bestD := 0.0
	if class == "local" {
		bestD = math.MaxFloat64
	}
	samples := 64
	if n < samples {
		samples = n
	}
	for i := 0; i < samples; i++ {
		t := pool[r.IntN(n)]
		if t == source {
			continue
		}
		d := graph.HaversineMeters(g.Nodes[source].Lat, g.Nodes[source].Lon, g.Nodes[t].Lat, g.Nodes[t].Lon)
		if (class == "local" && d < bestD) || (class == "regional" && d > bestD) {
			best, bestD = t, d
		}
	}
	if best < 0 {
		for _, v := range pool {
			if v != source {
				return v
			}
		}
		return source
	}
	return best
}

func summarize(samples []Sample, algs []search.Algorithm) []Summary {
	out := make([]Summary, 0, len(algs))
	byQuery := make(map[int]map[search.Algorithm]Sample)
	for _, sample := range samples {
		if _, ok := byQuery[sample.QueryIndex]; !ok {
			byQuery[sample.QueryIndex] = make(map[search.Algorithm]Sample)
		}
		byQuery[sample.QueryIndex][sample.Stats.Algorithm] = sample
	}

	for _, alg := range algs {
		durations := make([]int64, 0)
		relaxed := make([]uint64, 0)
		expanded := make([]uint64, 0)
		queuePushes := make([]uint64, 0)
		queuePops := make([]uint64, 0)
		stalePops := make([]uint64, 0)
		prunedAtPop := make([]uint64, 0)
		prunedAtRelax := make([]uint64, 0)
		boundPruned := make([]uint64, 0)
		allocBytes := make([]uint64, 0)
		allocObjects := make([]uint64, 0)
		summary := Summary{Algorithm: alg}
		for _, sample := range samples {
			if sample.Stats.Algorithm != alg {
				continue
			}
			summary.Runs++
			if sample.Stats.Reachable {
				summary.Reachable++
			}
			if sample.Correct {
				summary.Correct++
			}
			durations = append(durations, sample.Stats.DurationNS)
			relaxed = append(relaxed, sample.Stats.Relaxed)
			expanded = append(expanded, sample.Stats.Expanded)
			queuePushes = append(queuePushes, sample.Stats.QueuePushes)
			queuePops = append(queuePops, sample.Stats.QueuePops)
			stalePops = append(stalePops, sample.Stats.StalePops)
			prunedAtPop = append(prunedAtPop, sample.Stats.PrunedAtPop)
			prunedAtRelax = append(prunedAtRelax, sample.Stats.PrunedAtRelax)
			boundPruned = append(boundPruned, sample.Stats.BoundPruned)
			if sample.Stats.AllocBytes > 0 || sample.Stats.AllocObjects > 0 {
				allocBytes = append(allocBytes, sample.Stats.AllocBytes)
				allocObjects = append(allocObjects, sample.Stats.AllocObjects)
			}
		}
		if len(durations) > 0 {
			summary.MedianNS = percentileInt64(durations, 0.5)
			summary.MeanNS = meanInt64(durations)
			summary.MinNS = percentileInt64(durations, 0)
			summary.MaxNS = percentileInt64(durations, 1)
			summary.P95NS = percentileInt64(durations, 0.95)
			summary.P99NS = percentileInt64(durations, 0.99)
			summary.MedianEdges = percentileUint64(relaxed, 0.5)
			summary.MedianExpanded = percentileUint64(expanded, 0.5)
			summary.MedianQueuePushes = percentileUint64(queuePushes, 0.5)
			summary.MedianQueuePops = percentileUint64(queuePops, 0.5)
			summary.MedianStalePops = percentileUint64(stalePops, 0.5)
			summary.MedianPrunedAtPop = percentileUint64(prunedAtPop, 0.5)
			summary.MedianPrunedAtRelax = percentileUint64(prunedAtRelax, 0.5)
			summary.MedianBoundPruned = percentileUint64(boundPruned, 0.5)
			if len(allocBytes) > 0 {
				summary.MedianAllocBytes = percentileUint64(allocBytes, 0.5)
				summary.MedianAllocObjects = percentileUint64(allocObjects, 0.5)
			}
		}
		out = append(out, summary)
	}

	var base *Summary
	for i := range out {
		if out[i].Algorithm == search.Dijkstra {
			base = &out[i]
			break
		}
	}
	if base == nil {
		return out
	}
	for i := range out {
		if out[i].MedianNS > 0 {
			out[i].RatioOfMediansVsDijkstra = float64(base.MedianNS) / float64(out[i].MedianNS)
		}
		if base.MedianEdges > 0 {
			out[i].RelaxedReductionPct = 100 * (1 - float64(out[i].MedianEdges)/float64(base.MedianEdges))
		}
		if base.MedianExpanded > 0 {
			out[i].ExpandedReductionPct = 100 * (1 - float64(out[i].MedianExpanded)/float64(base.MedianExpanded))
		}
		perQuerySpeedups := make([]float64, 0, len(byQuery))
		for _, group := range byQuery {
			dijkstra, okD := group[search.Dijkstra]
			candidate, okA := group[out[i].Algorithm]
			if !okD || !okA || dijkstra.Stats.DurationNS <= 0 || candidate.Stats.DurationNS <= 0 {
				continue
			}
			perQuerySpeedups = append(perQuerySpeedups, float64(dijkstra.Stats.DurationNS)/float64(candidate.Stats.DurationNS))
		}
		if len(perQuerySpeedups) > 0 {
			out[i].MedianPerQuerySpeedupVsDijkstra = percentileFloat64(perQuerySpeedups, 0.5)
			out[i].GeomeanPerQuerySpeedupVsDijkstra = geometricMean(perQuerySpeedups)
		}
	}
	return out
}

func summarizeClasses(samples []Sample, algs []search.Algorithm) []ClassSummary {
	classes := []string{"local", "random", "regional"}
	out := make([]ClassSummary, 0, len(classes)*len(algs))
	for _, class := range classes {
		for _, alg := range algs {
			durations := []int64{}
			relaxed := []uint64{}
			for _, s := range samples {
				if s.QueryClass == class && s.Stats.Algorithm == alg {
					durations = append(durations, s.Stats.DurationNS)
					relaxed = append(relaxed, s.Stats.Relaxed)
				}
			}
			if len(durations) == 0 {
				continue
			}
			out = append(out, ClassSummary{Class: class, Algorithm: alg, Runs: len(durations), MedianNS: percentileInt64(durations, 0.5), P95NS: percentileInt64(durations, 0.95), MedianEdges: percentileUint64(relaxed, 0.5)})
		}
	}
	return out
}

func summarizeAegis(samples []Sample) AegisSummary {
	out := AegisSummary{DirectionByClass: map[string]DirectionTotals{}}
	byQuery := map[int][]Sample{}
	for _, sample := range samples {
		byQuery[sample.QueryIndex] = append(byQuery[sample.QueryIndex], sample)
	}

	relativeRuntimes := []float64{}
	oracleRegrets := []float64{}
	absolutePenalties := []int64{}
	speedups := []float64{}
	dijkstraDurations := []int64{}
	aegisDurations := []int64{}
	forwardShares := []float64{}
	switches := []uint64{}
	chunks := []uint64{}
	upperFractions := []float64{}
	tightness := []float64{}
	queuePushes := []uint64{}
	queuePops := []uint64{}
	stalePops := []uint64{}
	prunedAtPop := []uint64{}
	prunedAtRelax := []uint64{}
	pruned := []uint64{}
	meetingChecks := []uint64{}
	connectionChecks := []uint64{}
	finiteMeetings := []uint64{}
	potentialEvals := []uint64{}
	upperUpdates := []uint64{}
	gaps := []uint64{}

	indices := make([]int, 0, len(byQuery))
	for i := range byQuery {
		indices = append(indices, i)
	}
	sort.Ints(indices)
	for _, queryIndex := range indices {
		group := byQuery[queryIndex]
		core := map[search.Algorithm]Sample{}
		var aegis *Sample
		for i := range group {
			sample := group[i]
			switch sample.Stats.Algorithm {
			case search.Dijkstra, search.BiDijkstra, search.AStar:
				core[sample.Stats.Algorithm] = sample
			case search.Aegis:
				copy := sample
				aegis = &copy
			}
		}
		if aegis == nil || aegis.Stats.DurationNS <= 0 {
			continue
		}
		fastest := search.Algorithm("")
		fastestNS := int64(math.MaxInt64)
		for _, alg := range []search.Algorithm{search.Dijkstra, search.BiDijkstra, search.AStar} {
			if sample, ok := core[alg]; ok && sample.Stats.DurationNS > 0 && sample.Stats.DurationNS < fastestNS {
				fastest, fastestNS = alg, sample.Stats.DurationNS
			}
		}
		if fastest == "" {
			continue
		}
		relativeRuntime := float64(aegis.Stats.DurationNS) / float64(fastestNS)
		oracleRegret := math.Max(1, relativeRuntime)
		relativeRuntimes = append(relativeRuntimes, relativeRuntime)
		oracleRegrets = append(oracleRegrets, oracleRegret)
		penalty := aegis.Stats.DurationNS - fastestNS
		if penalty < 0 {
			penalty = 0
		}
		absolutePenalties = append(absolutePenalties, penalty)
		if relativeRuntime >= 1.25 && penalty >= int64(time.Millisecond) {
			out.MeaningfulSlowdowns++
		}
		out.RuntimeComparisons = append(out.RuntimeComparisons, RuntimeComparisonPoint{
			QueryIndex: queryIndex, Class: aegis.QueryClass,
			FastestClassicalBaseline: fastest, RuntimeVsFastestClassical: relativeRuntime, ClassicalOracleRegret: oracleRegret,
			FastestBaseline: fastest, RelativeRuntime: relativeRuntime, OracleRegret: oracleRegret,
		})
		if dijkstra, ok := core[search.Dijkstra]; ok && dijkstra.Stats.DurationNS > 0 {
			speedups = append(speedups, float64(dijkstra.Stats.DurationNS)/float64(aegis.Stats.DurationNS))
			dijkstraDurations = append(dijkstraDurations, dijkstra.Stats.DurationNS)
			aegisDurations = append(aegisDurations, aegis.Stats.DurationNS)
		}
		totalDirections := aegis.Stats.ForwardExpanded + aegis.Stats.BackwardExpanded
		if totalDirections > 0 {
			forwardShares = append(forwardShares, float64(aegis.Stats.ForwardExpanded)/float64(totalDirections))
		}
		switches = append(switches, aegis.Stats.DirectionSwitches)
		chunks = append(chunks, aegis.Stats.Chunks)
		if aegis.Stats.Expanded > 0 && aegis.Stats.UpperBoundUpdates > 0 {
			upperFractions = append(upperFractions, float64(aegis.Stats.FirstUpperBoundExpanded)/float64(aegis.Stats.Expanded))
		}
		if aegis.Stats.Distance > 0 && aegis.Stats.TerminationLowerBound > 0 {
			tightness = append(tightness, float64(aegis.Stats.TerminationLowerBound)/float64(aegis.Stats.Distance))
		}
		queuePushes = append(queuePushes, aegis.Stats.QueuePushes)
		queuePops = append(queuePops, aegis.Stats.QueuePops)
		stalePops = append(stalePops, aegis.Stats.StalePops)
		prunedAtPop = append(prunedAtPop, aegis.Stats.PrunedAtPop)
		prunedAtRelax = append(prunedAtRelax, aegis.Stats.PrunedAtRelax)
		pruned = append(pruned, aegis.Stats.BoundPruned)
		meetingChecks = append(meetingChecks, aegis.Stats.MeetingChecks)
		connectionChecks = append(connectionChecks, aegis.Stats.ConnectionChecks)
		finiteMeetings = append(finiteMeetings, aegis.Stats.FiniteMeetings)
		potentialEvals = append(potentialEvals, aegis.Stats.PotentialEvaluations)
		upperUpdates = append(upperUpdates, aegis.Stats.UpperBoundUpdates)
		gaps = append(gaps, aegis.Stats.OptimalityGap)
		totals := out.DirectionByClass[aegis.QueryClass]
		totals.ForwardExpanded += aegis.Stats.ForwardExpanded
		totals.BackwardExpanded += aegis.Stats.BackwardExpanded
		out.DirectionByClass[aegis.QueryClass] = totals
		out.Comparisons++
	}

	if len(relativeRuntimes) > 0 {
		out.MedianRuntimeVsFastestClassical = percentileFloat64(relativeRuntimes, 0.5)
		out.P95RuntimeVsFastestClassical = percentileFloat64(relativeRuntimes, 0.95)
		out.MaxRuntimeVsFastestClassical = percentileFloat64(relativeRuntimes, 1)
		out.MedianClassicalOracleRegret = percentileFloat64(oracleRegrets, 0.5)
		out.P95ClassicalOracleRegret = percentileFloat64(oracleRegrets, 0.95)
		out.MaxClassicalOracleRegret = percentileFloat64(oracleRegrets, 1)
		out.MedianRelativeRuntimeToFastestBaseline = out.MedianRuntimeVsFastestClassical
		out.P95RelativeRuntimeToFastestBaseline = out.P95RuntimeVsFastestClassical
		out.MaxRelativeRuntimeToFastestBaseline = out.MaxRuntimeVsFastestClassical
		out.MedianOracleRegret = out.MedianClassicalOracleRegret
		out.P95OracleRegret = out.P95ClassicalOracleRegret
		out.MaxOracleRegret = out.MaxClassicalOracleRegret
	}
	if len(absolutePenalties) > 0 {
		out.MedianAbsolutePenaltyNS = percentileInt64(absolutePenalties, 0.5)
		out.P95AbsolutePenaltyNS = percentileInt64(absolutePenalties, 0.95)
		out.MaxAbsolutePenaltyNS = percentileInt64(absolutePenalties, 1)
	}
	if len(speedups) > 0 {
		out.MedianPerQuerySpeedupVsDijkstra = percentileFloat64(speedups, 0.5)
		out.GeomeanPerQuerySpeedupVsDijkstra = geometricMean(speedups)
	}
	if len(dijkstraDurations) > 0 && len(aegisDurations) > 0 {
		out.RatioOfMediansVsDijkstra = float64(percentileInt64(dijkstraDurations, 0.5)) / float64(percentileInt64(aegisDurations, 0.5))
	}
	if len(forwardShares) > 0 {
		out.MedianForwardShare = percentileFloat64(forwardShares, 0.5)
		out.P95ForwardShare = percentileFloat64(forwardShares, 0.95)
	}
	if len(switches) > 0 {
		out.MedianDirectionSwitches = percentileUint64(switches, 0.5)
	}
	if len(chunks) > 0 {
		out.MedianChunks = percentileUint64(chunks, 0.5)
	}
	if len(upperFractions) > 0 {
		out.MedianFirstUpperBoundFraction = percentileFloat64(upperFractions, 0.5)
	}
	if len(tightness) > 0 {
		out.MedianTerminationTightness = percentileFloat64(tightness, 0.5)
	}
	if len(queuePushes) > 0 {
		out.MedianQueuePushes = percentileUint64(queuePushes, 0.5)
		out.MedianQueuePops = percentileUint64(queuePops, 0.5)
		out.MedianStalePops = percentileUint64(stalePops, 0.5)
		out.MedianPrunedAtPop = percentileUint64(prunedAtPop, 0.5)
		out.MedianPrunedAtRelax = percentileUint64(prunedAtRelax, 0.5)
		out.MedianBoundPruned = percentileUint64(pruned, 0.5)
		out.MedianMeetingChecks = percentileUint64(meetingChecks, 0.5)
		out.MedianConnectionChecks = percentileUint64(connectionChecks, 0.5)
		out.MedianFiniteMeetings = percentileUint64(finiteMeetings, 0.5)
		out.MedianPotentialEvaluations = percentileUint64(potentialEvals, 0.5)
		out.MedianUpperBoundUpdates = percentileUint64(upperUpdates, 0.5)
		out.MedianOptimalityGap = percentileUint64(gaps, 0.5)
	}
	return out
}

func geometricMean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, value := range values {
		if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			continue
		}
		sum += math.Log(value)
	}
	return math.Exp(sum / float64(len(values)))
}

func meanInt64(values []int64) int64 {
	if len(values) == 0 {
		return 0
	}
	var mean float64
	for i, value := range values {
		mean += (float64(value) - mean) / float64(i+1)
	}
	return int64(math.Round(mean))
}

func percentileInt64(values []int64, p float64) int64 {
	v := append([]int64(nil), values...)
	sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
	return v[percentileIndex(len(v), p)]
}

func percentileUint64(values []uint64, p float64) uint64 {
	v := append([]uint64(nil), values...)
	sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
	return v[percentileIndex(len(v), p)]
}

func percentileFloat64(values []float64, p float64) float64 {
	v := append([]float64(nil), values...)
	sort.Float64s(v)
	return v[percentileIndex(len(v), p)]
}

func percentileIndex(n int, p float64) int {
	if n <= 1 {
		return 0
	}
	if p <= 0 {
		return 0
	}
	if p >= 1 {
		return n - 1
	}
	return int(math.Ceil(float64(n)*p)) - 1
}

func WriteJSON(path string, report Report) error {
	b, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0644)
}

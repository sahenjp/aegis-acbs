package bench

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/sahenjp/aegis-acbs/internal/graph"
	"github.com/sahenjp/aegis-acbs/internal/search"
	"github.com/sahenjp/aegis-acbs/internal/version"
)

// RegretReplayConfig controls isolated replay of meaningful validation cases.
type RegretReplayConfig struct {
	Runs           int           `json:"runs"`
	Warmup         int           `json:"warmup"`
	Timeout        time.Duration `json:"-"`
	TimeoutNS      int64         `json:"timeoutNs"`
	RatioThreshold float64       `json:"ratioThreshold"`
	PenaltyFloorNS int64         `json:"penaltyFloorNs"`
	Top            int           `json:"top"`
}

type ReplayGuardSummary struct {
	Algorithm    search.Algorithm `json:"algorithm"`
	MedianNS     int64            `json:"medianNs"`
	VsAegis      float64          `json:"vsAegis"`
	AdvantageNS  int64            `json:"advantageNs"`
	RegressionNS int64            `json:"regressionNs"`
	Outcome      string           `json:"outcome"`
}

type ReplayGuardOutcomeCounts struct {
	Improved        int   `json:"improved"`
	Neutral         int   `json:"neutral"`
	Regressed       int   `json:"regressed"`
	SchedulerTails  int   `json:"schedulerTails"`
	SchedulerPass   bool  `json:"schedulerPass"`
	MaxRegressionNS int64 `json:"maxRegressionNs"`
}

type ReplayAlgorithmSummary struct {
	Algorithm      search.Algorithm `json:"algorithm"`
	Runs           int              `json:"runs"`
	MeanNS         int64            `json:"meanNs"`
	MedianNS       int64            `json:"medianNs"`
	MinNS          int64            `json:"minNs"`
	MaxNS          int64            `json:"maxNs"`
	P95NS          int64            `json:"p95Ns"`
	MedianExpanded uint64           `json:"medianExpanded"`
	MedianRelaxed  uint64           `json:"medianRelaxed"`
	AllCorrect     bool             `json:"allCorrect"`
	Error          string           `json:"error,omitempty"`
}

type RegretReplayCase struct {
	SourceReport          string                   `json:"sourceReport"`
	QueryIndex            int                      `json:"queryIndex"`
	Class                 string                   `json:"class"`
	SourceID              int64                    `json:"sourceId"`
	TargetID              int64                    `json:"targetId"`
	StraightLineMeters    float64                  `json:"straightLineMeters"`
	DistanceRatio         float64                  `json:"distanceRatio"`
	OriginalBaseline      search.Algorithm         `json:"originalBaseline"`
	OriginalRatio         float64                  `json:"originalRatio"`
	OriginalPenaltyNS     int64                    `json:"originalPenaltyNs"`
	Algorithms            []ReplayAlgorithmSummary `json:"algorithms"`
	FastestClassical      search.Algorithm         `json:"fastestClassical"`
	FastestClassicalNS    int64                    `json:"fastestClassicalNs"`
	AegisNS               int64                    `json:"aegisNs"`
	AegisRatio            float64                  `json:"aegisRatio"`
	AegisPenaltyNS        int64                    `json:"aegisPenaltyNs"`
	ReplayMeaningful      bool                     `json:"replayMeaningful"`
	StaticNS              int64                    `json:"staticNs"`
	StaticVsAegis         float64                  `json:"staticVsAegis"`
	StaticAdvantageNS     int64                    `json:"staticAdvantageNs"`
	LateGuardNS           int64                    `json:"lateGuardNs"`
	LateGuardVsAegis      float64                  `json:"lateGuardVsAegis"`
	LateGuardAdvantageNS  int64                    `json:"lateGuardAdvantageNs"`
	LateGuardRegressionNS int64                    `json:"lateGuardRegressionNs"`
	LateGuardOutcome      string                   `json:"lateGuardOutcome"`
	Guards                []ReplayGuardSummary     `json:"guards,omitempty"`
	Classification        string                   `json:"classification"`
	AllCorrect            bool                     `json:"allCorrect"`
	Trace                 []search.ACBSTraceEvent  `json:"trace"`
	TraceUpperBoundChunk  uint64                   `json:"traceUpperBoundChunk,omitempty"`
}

type RegretReplayReport struct {
	Version               string                              `json:"version"`
	GeneratedAt           time.Time                           `json:"generatedAt"`
	AegisVersion          string                              `json:"aegisVersion"`
	GraphName             string                              `json:"graphName"`
	Metric                string                              `json:"metric"`
	ValidationPath        string                              `json:"validationPath"`
	Config                RegretReplayConfig                  `json:"config"`
	Cases                 []RegretReplayCase                  `json:"cases"`
	RequestedCases        int                                 `json:"requestedCases"`
	ReplayedCases         int                                 `json:"replayedCases"`
	ReproducedMeaningful  int                                 `json:"reproducedMeaningful"`
	AdaptiveSchedulerTail int                                 `json:"adaptiveSchedulerTail"`
	PersistentClassical   int                                 `json:"persistentClassical"`
	NotReproduced         int                                 `json:"notReproduced"`
	LateGuardImproved     int                                 `json:"lateGuardImproved"`
	LateGuardNeutral      int                                 `json:"lateGuardNeutral"`
	LateGuardRegressed    int                                 `json:"lateGuardRegressed"`
	LateGuardPass         bool                                `json:"lateGuardPass"`
	GuardOutcomes         map[string]ReplayGuardOutcomeCounts `json:"guardOutcomes,omitempty"`
	AllCorrect            bool                                `json:"allCorrect"`
}

func LoadRegretValidation(path string) (RegretValidationReport, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return RegretValidationReport{}, err
	}
	var report RegretValidationReport
	if err := json.Unmarshal(data, &report); err != nil {
		return RegretValidationReport{}, err
	}
	return report, nil
}

// ReplayRegret isolates validation outliers, remeasures every implementation in
// an interleaved loop, and captures one deterministic ACBS scheduler trace.
func ReplayRegret(ctx context.Context, g *graph.Graph, validationPath, inputRoot string, cfg RegretReplayConfig) (RegretReplayReport, error) {
	if cfg.Runs <= 0 {
		cfg.Runs = 31
	}
	if cfg.Warmup < 0 {
		return RegretReplayReport{}, errors.New("warmup must be >= 0")
	}
	if cfg.RatioThreshold <= 0 {
		cfg.RatioThreshold = 1.25
	}
	if cfg.PenaltyFloorNS <= 0 {
		cfg.PenaltyFloorNS = int64(time.Millisecond)
	}
	if cfg.Top <= 0 {
		cfg.Top = 50
	}
	if cfg.Timeout <= 0 && cfg.TimeoutNS > 0 {
		cfg.Timeout = time.Duration(cfg.TimeoutNS)
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	cfg.TimeoutNS = cfg.Timeout.Nanoseconds()

	validation, err := LoadRegretValidation(validationPath)
	if err != nil {
		return RegretReplayReport{}, err
	}
	if inputRoot == "" {
		inputRoot = filepath.Dir(validationPath)
	}
	absRoot, err := filepath.Abs(inputRoot)
	if err != nil {
		return RegretReplayReport{}, err
	}
	rows := validation.TopMeaningful
	if len(rows) > cfg.Top {
		rows = rows[:cfg.Top]
	}
	out := RegretReplayReport{
		Version: "regret-replay-v1", GeneratedAt: time.Now().UTC(), AegisVersion: version.Version,
		GraphName: g.Name, Metric: string(g.Metric), ValidationPath: validationPath,
		Config: cfg, RequestedCases: len(rows), AllCorrect: true, LateGuardPass: true,
		GuardOutcomes: map[string]ReplayGuardOutcomeCounts{},
	}
	for _, alg := range connectionGuardCandidates() {
		out.GuardOutcomes[string(alg)] = ReplayGuardOutcomeCounts{SchedulerPass: true}
	}
	if len(rows) == 0 {
		return out, nil
	}

	algs := []search.Algorithm{search.Dijkstra, search.BiDijkstra, search.AStar, search.AegisStatic, search.AegisLateGuard, search.AegisConnect32, search.AegisConnect40, search.AegisConnect32x16, search.Aegis}
	for caseIndex, top := range rows {
		reportPath := filepath.Join(absRoot, filepath.FromSlash(top.Path))
		sourceReport, err := LoadReport(reportPath)
		if err != nil {
			return out, fmt.Errorf("load source report %s: %w", reportPath, err)
		}
		if top.QueryIndex < 0 || top.QueryIndex >= len(sourceReport.Queries) {
			return out, fmt.Errorf("query index %d is out of range in %s", top.QueryIndex, reportPath)
		}
		source, okS := g.IndexByID(top.SourceID)
		target, okT := g.IndexByID(top.TargetID)
		if !okS || !okT {
			return out, fmt.Errorf("query %d node IDs are not present in graph", top.QueryIndex)
		}
		q := Query{Source: source, Target: target, StraightLineMeters: top.StraightLineMeters, Class: top.Class}
		caseReport, err := replayOneCase(ctx, g, q, algs, cfg, caseIndex)
		if err != nil {
			return out, fmt.Errorf("replay %s query %d: %w", top.Path, top.QueryIndex, err)
		}
		caseReport.SourceReport = top.Path
		caseReport.QueryIndex = top.QueryIndex
		caseReport.Class = top.Class
		caseReport.SourceID = top.SourceID
		caseReport.TargetID = top.TargetID
		caseReport.StraightLineMeters = top.StraightLineMeters
		caseReport.DistanceRatio = top.DistanceRatio
		caseReport.OriginalBaseline = top.FastestClassical
		caseReport.OriginalRatio = top.RuntimeRatio
		caseReport.OriginalPenaltyNS = top.AbsolutePenaltyNS
		out.Cases = append(out.Cases, caseReport)
		out.ReplayedCases++
		out.AllCorrect = out.AllCorrect && caseReport.AllCorrect
		if caseReport.ReplayMeaningful {
			out.ReproducedMeaningful++
		}
		switch caseReport.Classification {
		case "adaptive-scheduler-tail":
			out.AdaptiveSchedulerTail++
		case "persistent-classical-tail":
			out.PersistentClassical++
		case "not-reproduced":
			out.NotReproduced++
		}
		switch caseReport.LateGuardOutcome {
		case "improved":
			out.LateGuardImproved++
		case "regressed":
			out.LateGuardRegressed++
		default:
			out.LateGuardNeutral++
		}
		if caseReport.Classification == "adaptive-scheduler-tail" && caseReport.LateGuardOutcome != "improved" {
			out.LateGuardPass = false
		}
		if caseReport.LateGuardRegressionNS >= cfg.PenaltyFloorNS {
			out.LateGuardPass = false
		}
		for _, guard := range caseReport.Guards {
			counts := out.GuardOutcomes[string(guard.Algorithm)]
			if caseReport.Classification == "adaptive-scheduler-tail" {
				counts.SchedulerTails++
				if guard.Outcome != "improved" {
					counts.SchedulerPass = false
				}
			}
			switch guard.Outcome {
			case "improved":
				counts.Improved++
			case "regressed":
				counts.Regressed++
			default:
				counts.Neutral++
			}
			if guard.RegressionNS > counts.MaxRegressionNS {
				counts.MaxRegressionNS = guard.RegressionNS
			}
			if guard.RegressionNS >= cfg.PenaltyFloorNS {
				counts.SchedulerPass = false
			}
			out.GuardOutcomes[string(guard.Algorithm)] = counts
		}
	}
	return out, nil
}

func connectionGuardCandidates() []search.Algorithm {
	return []search.Algorithm{search.AegisConnect32, search.AegisConnect40, search.AegisConnect32x16}
}

func replayOneCase(ctx context.Context, g *graph.Graph, q Query, algs []search.Algorithm, cfg RegretReplayConfig, caseIndex int) (RegretReplayCase, error) {
	for i := 0; i < cfg.Warmup; i++ {
		for _, alg := range rotated(algs, i+caseIndex) {
			if _, err := runReplay(ctx, g, q, alg, cfg.Timeout); err != nil {
				return RegretReplayCase{}, err
			}
		}
	}

	buckets := make(map[search.Algorithm][]search.Result, len(algs))
	var expectedReachable bool
	var expectedDistance uint64
	haveExpected := false
	allCorrect := true
	for run := 0; run < cfg.Runs; run++ {
		for _, alg := range rotated(algs, run+caseIndex) {
			r, err := runReplay(ctx, g, q, alg, cfg.Timeout)
			if err != nil {
				return RegretReplayCase{}, err
			}
			if alg == search.Dijkstra && !haveExpected {
				expectedReachable, expectedDistance, haveExpected = r.Stats.Reachable, r.Stats.Distance, true
			}
			buckets[alg] = append(buckets[alg], r)
		}
	}
	if !haveExpected {
		return RegretReplayCase{}, errors.New("Dijkstra reference was not measured")
	}

	out := RegretReplayCase{AllCorrect: true}
	summaries := make(map[search.Algorithm]ReplayAlgorithmSummary, len(algs))
	for _, alg := range algs {
		results := buckets[alg]
		durations := make([]int64, 0, len(results))
		expanded := make([]uint64, 0, len(results))
		relaxed := make([]uint64, 0, len(results))
		correct := true
		for _, r := range results {
			durations = append(durations, r.Stats.DurationNS)
			expanded = append(expanded, r.Stats.Expanded)
			relaxed = append(relaxed, r.Stats.Relaxed)
			if r.Stats.Reachable != expectedReachable || (expectedReachable && r.Stats.Distance != expectedDistance) || !search.Validate(g, q.Source, q.Target, r) {
				correct = false
			}
		}
		allCorrect = allCorrect && correct
		summary := ReplayAlgorithmSummary{Algorithm: alg, Runs: len(results), AllCorrect: correct}
		if len(durations) > 0 {
			summary.MeanNS = meanInt64(durations)
			summary.MedianNS = percentileInt64(durations, .5)
			summary.MinNS = percentileInt64(durations, 0)
			summary.MaxNS = percentileInt64(durations, 1)
			summary.P95NS = percentileInt64(durations, .95)
			summary.MedianExpanded = percentileUint64(expanded, .5)
			summary.MedianRelaxed = percentileUint64(relaxed, .5)
		}
		summaries[alg] = summary
		out.Algorithms = append(out.Algorithms, summary)
	}
	out.AllCorrect = allCorrect

	for _, alg := range []search.Algorithm{search.Dijkstra, search.BiDijkstra, search.AStar} {
		s := summaries[alg]
		if s.MedianNS <= 0 {
			continue
		}
		if out.FastestClassicalNS == 0 || s.MedianNS < out.FastestClassicalNS {
			out.FastestClassicalNS = s.MedianNS
			out.FastestClassical = alg
		}
	}
	aegis := summaries[search.Aegis]
	static := summaries[search.AegisStatic]
	lateGuard := summaries[search.AegisLateGuard]
	out.AegisNS = aegis.MedianNS
	out.StaticNS = static.MedianNS
	out.LateGuardNS = lateGuard.MedianNS
	if out.FastestClassicalNS > 0 && out.AegisNS > 0 {
		out.AegisRatio = float64(out.AegisNS) / float64(out.FastestClassicalNS)
		out.AegisPenaltyNS = out.AegisNS - out.FastestClassicalNS
		if out.AegisPenaltyNS < 0 {
			out.AegisPenaltyNS = 0
		}
	}
	if out.AegisNS > 0 && out.StaticNS > 0 {
		out.StaticVsAegis = float64(out.StaticNS) / float64(out.AegisNS)
		out.StaticAdvantageNS = out.AegisNS - out.StaticNS
		if out.StaticAdvantageNS < 0 {
			out.StaticAdvantageNS = 0
		}
	}
	if out.AegisNS > 0 && out.LateGuardNS > 0 {
		out.LateGuardVsAegis = float64(out.LateGuardNS) / float64(out.AegisNS)
		out.LateGuardAdvantageNS = out.AegisNS - out.LateGuardNS
		if out.LateGuardAdvantageNS < 0 {
			out.LateGuardRegressionNS = -out.LateGuardAdvantageNS
			out.LateGuardAdvantageNS = 0
		}
		guardFloor := cfg.PenaltyFloorNS / 2
		if guardFloor < int64(250*time.Microsecond) {
			guardFloor = int64(250 * time.Microsecond)
		}
		switch {
		case out.LateGuardAdvantageNS >= guardFloor:
			out.LateGuardOutcome = "improved"
		case out.LateGuardRegressionNS >= cfg.PenaltyFloorNS:
			out.LateGuardOutcome = "regressed"
		default:
			out.LateGuardOutcome = "neutral"
		}
	}
	for _, alg := range connectionGuardCandidates() {
		guard := summaries[alg]
		g := ReplayGuardSummary{Algorithm: alg, MedianNS: guard.MedianNS}
		if out.AegisNS > 0 && g.MedianNS > 0 {
			g.VsAegis = float64(g.MedianNS) / float64(out.AegisNS)
			g.AdvantageNS = out.AegisNS - g.MedianNS
			if g.AdvantageNS < 0 {
				g.RegressionNS = -g.AdvantageNS
				g.AdvantageNS = 0
			}
			guardFloor := cfg.PenaltyFloorNS / 2
			if guardFloor < int64(250*time.Microsecond) {
				guardFloor = int64(250 * time.Microsecond)
			}
			switch {
			case g.AdvantageNS >= guardFloor:
				g.Outcome = "improved"
			case g.RegressionNS >= cfg.PenaltyFloorNS:
				g.Outcome = "regressed"
			default:
				g.Outcome = "neutral"
			}
		}
		out.Guards = append(out.Guards, g)
	}
	out.ReplayMeaningful = out.AegisRatio >= cfg.RatioThreshold && out.AegisPenaltyNS >= cfg.PenaltyFloorNS
	switch {
	case !out.ReplayMeaningful:
		out.Classification = "not-reproduced"
	case out.StaticAdvantageNS >= cfg.PenaltyFloorNS/2 && out.StaticVsAegis <= .95:
		out.Classification = "adaptive-scheduler-tail"
	default:
		out.Classification = "persistent-classical-tail"
	}

	var trace []search.ACBSTraceEvent
	traceCtx := search.WithACBSTrace(ctx, func(event search.ACBSTraceEvent) {
		trace = append(trace, event)
	})
	traced, err := runReplay(traceCtx, g, q, search.Aegis, cfg.Timeout)
	if err != nil {
		return RegretReplayCase{}, err
	}
	if traced.Stats.Reachable != expectedReachable || (expectedReachable && traced.Stats.Distance != expectedDistance) || !search.Validate(g, q.Source, q.Target, traced) {
		out.AllCorrect = false
	}
	out.Trace = trace
	for _, event := range trace {
		if event.HadUpperBoundAfter {
			out.TraceUpperBoundChunk = event.Chunk
			break
		}
	}
	return out, nil
}

func runReplay(parent context.Context, g *graph.Graph, q Query, alg search.Algorithm, timeout time.Duration) (search.Result, error) {
	ctx := parent
	cancel := func() {}
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(parent, timeout)
	}
	defer cancel()
	return search.Run(ctx, g, q.Source, q.Target, alg)
}

func WriteRegretReplayJSON(path string, report RegretReplayReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func WriteRegretReplayCSV(path string, report RegretReplayReport) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	if err := w.Write([]string{"source_report", "query_index", "class", "source_id", "target_id", "distance_km", "original_baseline", "original_ratio", "original_penalty_ms", "replay_baseline", "aegis_median_ms", "baseline_median_ms", "replay_ratio", "replay_penalty_ms", "static_median_ms", "static_vs_aegis", "late_guard_median_ms", "late_guard_vs_aegis", "late_guard_advantage_ms", "late_guard_regression_ms", "late_guard_outcome", "connect_32_ms", "connect_32_outcome", "connect_40_ms", "connect_40_outcome", "connect_32x16_ms", "connect_32x16_outcome", "classification", "trace_chunks", "upper_bound_chunk", "all_correct"}); err != nil {
		return err
	}
	for _, c := range report.Cases {
		record := []string{
			c.SourceReport, strconv.Itoa(c.QueryIndex), c.Class, strconv.FormatInt(c.SourceID, 10), strconv.FormatInt(c.TargetID, 10),
			fmt.Sprintf("%.6f", c.StraightLineMeters/1000), string(c.OriginalBaseline), fmt.Sprintf("%.6f", c.OriginalRatio), fmt.Sprintf("%.6f", float64(c.OriginalPenaltyNS)/1e6),
			string(c.FastestClassical), fmt.Sprintf("%.6f", float64(c.AegisNS)/1e6), fmt.Sprintf("%.6f", float64(c.FastestClassicalNS)/1e6), fmt.Sprintf("%.6f", c.AegisRatio), fmt.Sprintf("%.6f", float64(c.AegisPenaltyNS)/1e6),
			fmt.Sprintf("%.6f", float64(c.StaticNS)/1e6), fmt.Sprintf("%.6f", c.StaticVsAegis), fmt.Sprintf("%.6f", float64(c.LateGuardNS)/1e6), fmt.Sprintf("%.6f", c.LateGuardVsAegis), fmt.Sprintf("%.6f", float64(c.LateGuardAdvantageNS)/1e6), fmt.Sprintf("%.6f", float64(c.LateGuardRegressionNS)/1e6), c.LateGuardOutcome, guardMS(c, search.AegisConnect32), guardOutcome(c, search.AegisConnect32), guardMS(c, search.AegisConnect40), guardOutcome(c, search.AegisConnect40), guardMS(c, search.AegisConnect32x16), guardOutcome(c, search.AegisConnect32x16), c.Classification, strconv.Itoa(len(c.Trace)), strconv.FormatUint(c.TraceUpperBoundChunk, 10), strconv.FormatBool(c.AllCorrect),
		}
		if err := w.Write(record); err != nil {
			return err
		}
	}
	return w.Error()
}

// Stable ordering is useful for downstream diffing even if callers construct a
// report manually.
func replayGuard(c RegretReplayCase, alg search.Algorithm) (ReplayGuardSummary, bool) {
	for _, g := range c.Guards {
		if g.Algorithm == alg {
			return g, true
		}
	}
	return ReplayGuardSummary{}, false
}

func guardMS(c RegretReplayCase, alg search.Algorithm) string {
	g, ok := replayGuard(c, alg)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%.6f", float64(g.MedianNS)/1e6)
}

func guardOutcome(c RegretReplayCase, alg search.Algorithm) string {
	g, ok := replayGuard(c, alg)
	if !ok {
		return ""
	}
	return g.Outcome
}

func SortRegretReplayCases(report *RegretReplayReport) {
	sort.Slice(report.Cases, func(i, j int) bool {
		if report.Cases[i].Classification != report.Cases[j].Classification {
			return report.Cases[i].Classification < report.Cases[j].Classification
		}
		if report.Cases[i].AegisPenaltyNS != report.Cases[j].AegisPenaltyNS {
			return report.Cases[i].AegisPenaltyNS > report.Cases[j].AegisPenaltyNS
		}
		return report.Cases[i].QueryIndex < report.Cases[j].QueryIndex
	})
}

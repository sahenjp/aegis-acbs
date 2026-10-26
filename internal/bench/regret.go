package bench

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"

	"github.com/sahenjp/aegis-acbs/internal/search"
)

type RegretConfig struct {
	Algorithm      search.Algorithm `json:"algorithm"`
	RatioThreshold float64          `json:"ratioThreshold"`
	PenaltyFloorNS int64            `json:"penaltyFloorNs"`
	Top            int              `json:"top"`
}

type RegretBucket struct {
	Name             string  `json:"name"`
	Queries          int     `json:"queries"`
	Meaningful       int     `json:"meaningful"`
	MeanRuntimeRatio float64 `json:"meanRuntimeRatio"`
	P50RuntimeRatio  float64 `json:"p50RuntimeRatio"`
	P95RuntimeRatio  float64 `json:"p95RuntimeRatio"`
	MeanPenaltyNS    int64   `json:"meanPenaltyNs"`
	P95PenaltyNS     int64   `json:"p95PenaltyNs"`
}

type RegretRow struct {
	QueryIndex          int              `json:"queryIndex"`
	Class               string           `json:"class"`
	SourceID            int64            `json:"sourceId"`
	TargetID            int64            `json:"targetId"`
	StraightLineMeters  float64          `json:"straightLineMeters"`
	DistanceRatio       float64          `json:"distanceRatio"`
	SourceDegree        int              `json:"sourceDegree"`
	TargetReverseDegree int              `json:"targetReverseDegree"`
	FastestClassical    search.Algorithm `json:"fastestClassical"`
	FastestClassicalNS  int64            `json:"fastestClassicalNs"`
	Algorithm           search.Algorithm `json:"algorithm"`
	AlgorithmNS         int64            `json:"algorithmNs"`
	RuntimeRatio        float64          `json:"runtimeRatio"`
	OracleRegret        float64          `json:"oracleRegret"`
	AbsolutePenaltyNS   int64            `json:"absolutePenaltyNs"`
	Meaningful          bool             `json:"meaningful"`
	Expanded            uint64           `json:"expanded"`
	Relaxed             uint64           `json:"relaxed"`
	QueuePushes         uint64           `json:"queuePushes"`
	QueuePops           uint64           `json:"queuePops"`
	StalePops           uint64           `json:"stalePops"`
	ForwardShare        float64          `json:"forwardShare"`
	DirectionSwitches   uint64           `json:"directionSwitches"`
	Chunks              uint64           `json:"chunks"`
	SwitchRate          float64          `json:"switchRate"`
	FirstUpperFraction  float64          `json:"firstUpperBoundFraction"`
	ConnectionChecks    uint64           `json:"connectionChecks"`
	FiniteMeetings      uint64           `json:"finiteMeetings"`
	UpperBoundUpdates   uint64           `json:"upperBoundUpdates"`
	ForwardEfficiency   float64          `json:"forwardEfficiency"`
	BackwardEfficiency  float64          `json:"backwardEfficiency"`
	EfficiencyImbalance float64          `json:"efficiencyImbalance"`
	StaleRate           float64          `json:"staleRate"`
}

type RegretReport struct {
	Version           string             `json:"version"`
	GraphName         string             `json:"graphName"`
	Metric            string             `json:"metric"`
	SourceVersion     string             `json:"sourceVersion"`
	Config            RegretConfig       `json:"config"`
	Queries           int                `json:"queries"`
	MeaningfulQueries int                `json:"meaningfulQueries"`
	P50RuntimeRatio   float64            `json:"p50RuntimeRatio"`
	P95RuntimeRatio   float64            `json:"p95RuntimeRatio"`
	MaxRuntimeRatio   float64            `json:"maxRuntimeRatio"`
	P50PenaltyNS      int64              `json:"p50PenaltyNs"`
	P95PenaltyNS      int64              `json:"p95PenaltyNs"`
	MaxPenaltyNS      int64              `json:"maxPenaltyNs"`
	ByClass           []RegretBucket     `json:"byClass"`
	ByBaseline        []RegretBucket     `json:"byBaseline"`
	Correlations      map[string]float64 `json:"correlations"`
	TopByPenalty      []RegretRow        `json:"topByPenalty"`
	TopByRatio        []RegretRow        `json:"topByRatio"`
	Rows              []RegretRow        `json:"rows"`
}

func LoadReport(path string) (Report, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Report{}, err
	}
	var report Report
	if err := json.Unmarshal(data, &report); err != nil {
		return Report{}, err
	}
	return report, nil
}

func AnalyzeRegret(report Report, cfg RegretConfig) (RegretReport, error) {
	if cfg.Algorithm == "" {
		cfg.Algorithm = search.Aegis
	}
	if cfg.RatioThreshold <= 0 {
		cfg.RatioThreshold = 1.25
	}
	if cfg.PenaltyFloorNS <= 0 {
		cfg.PenaltyFloorNS = 1_000_000 // Ignore sub-millisecond ratio noise by default.
	}
	if cfg.Top <= 0 {
		cfg.Top = 25
	}

	byQuery := make(map[int][]Sample)
	for _, sample := range report.Samples {
		byQuery[sample.QueryIndex] = append(byQuery[sample.QueryIndex], sample)
	}
	rows := make([]RegretRow, 0, len(byQuery))
	for queryIndex, samples := range byQuery {
		var target *Sample
		var fastest *Sample
		for i := range samples {
			s := &samples[i]
			if s.Stats.Algorithm == cfg.Algorithm {
				target = s
			}
			if !isClassical(s.Stats.Algorithm) || s.Stats.DurationNS <= 0 || !s.Correct {
				continue
			}
			if fastest == nil || s.Stats.DurationNS < fastest.Stats.DurationNS {
				fastest = s
			}
		}
		if target == nil || fastest == nil || target.Stats.DurationNS <= 0 || !target.Correct {
			continue
		}
		ratio := float64(target.Stats.DurationNS) / float64(fastest.Stats.DurationNS)
		penalty := target.Stats.DurationNS - fastest.Stats.DurationNS
		if penalty < 0 {
			penalty = 0
		}
		forwardShare := 0.0
		directional := target.Stats.ForwardExpanded + target.Stats.BackwardExpanded
		if directional > 0 {
			forwardShare = float64(target.Stats.ForwardExpanded) / float64(directional)
		}
		switchRate := 0.0
		if target.Stats.Chunks > 0 {
			switchRate = float64(target.Stats.DirectionSwitches) / float64(target.Stats.Chunks)
		}
		upperFraction := 0.0
		if target.Stats.Expanded > 0 && target.Stats.UpperBoundUpdates > 0 {
			upperFraction = float64(target.Stats.FirstUpperBoundExpanded) / float64(target.Stats.Expanded)
		}
		staleRate := 0.0
		if target.Stats.QueuePops > 0 {
			staleRate = float64(target.Stats.StalePops) / float64(target.Stats.QueuePops)
		}
		efficiencyImbalance := 0.0
		maxEfficiency := math.Max(target.Stats.ForwardEfficiency, target.Stats.BackwardEfficiency)
		if maxEfficiency > 0 {
			efficiencyImbalance = math.Abs(target.Stats.ForwardEfficiency-target.Stats.BackwardEfficiency) / maxEfficiency
		}
		distanceRatio := target.DistanceRatio
		if distanceRatio == 0 && report.DiameterMeters > 0 {
			distanceRatio = target.StraightLineMeters / report.DiameterMeters
		}
		rows = append(rows, RegretRow{
			QueryIndex: queryIndex, Class: target.QueryClass, SourceID: target.SourceID, TargetID: target.TargetID,
			StraightLineMeters: target.StraightLineMeters, DistanceRatio: distanceRatio,
			SourceDegree: target.SourceDegree, TargetReverseDegree: target.TargetReverseDegree,
			FastestClassical: fastest.Stats.Algorithm, FastestClassicalNS: fastest.Stats.DurationNS,
			Algorithm: cfg.Algorithm, AlgorithmNS: target.Stats.DurationNS,
			RuntimeRatio: ratio, OracleRegret: math.Max(1, ratio), AbsolutePenaltyNS: penalty,
			Meaningful: ratio >= cfg.RatioThreshold && penalty >= cfg.PenaltyFloorNS,
			Expanded:   target.Stats.Expanded, Relaxed: target.Stats.Relaxed,
			QueuePushes: target.Stats.QueuePushes, QueuePops: target.Stats.QueuePops, StalePops: target.Stats.StalePops,
			ForwardShare: forwardShare, DirectionSwitches: target.Stats.DirectionSwitches, Chunks: target.Stats.Chunks,
			SwitchRate: switchRate, FirstUpperFraction: upperFraction,
			ConnectionChecks: target.Stats.ConnectionChecks, FiniteMeetings: target.Stats.FiniteMeetings,
			UpperBoundUpdates: target.Stats.UpperBoundUpdates,
			ForwardEfficiency: target.Stats.ForwardEfficiency, BackwardEfficiency: target.Stats.BackwardEfficiency,
			EfficiencyImbalance: efficiencyImbalance, StaleRate: staleRate,
		})
	}
	if len(rows) == 0 {
		return RegretReport{}, errors.New("report does not contain the requested algorithm and classical baselines")
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].QueryIndex < rows[j].QueryIndex })

	ratios := make([]float64, 0, len(rows))
	penalties := make([]int64, 0, len(rows))
	meaningful := 0
	for _, row := range rows {
		ratios = append(ratios, row.RuntimeRatio)
		penalties = append(penalties, row.AbsolutePenaltyNS)
		if row.Meaningful {
			meaningful++
		}
	}
	out := RegretReport{
		Version: "regret-v1", GraphName: report.GraphName, Metric: string(report.Metric), SourceVersion: report.Version,
		Config: cfg, Queries: len(rows), MeaningfulQueries: meaningful,
		P50RuntimeRatio: percentileFloat64(ratios, .5), P95RuntimeRatio: percentileFloat64(ratios, .95), MaxRuntimeRatio: percentileFloat64(ratios, 1),
		P50PenaltyNS: percentileInt64(penalties, .5), P95PenaltyNS: percentileInt64(penalties, .95), MaxPenaltyNS: percentileInt64(penalties, 1),
		Rows: rows,
	}
	out.ByClass = regretBuckets(rows, func(r RegretRow) string { return r.Class })
	out.ByBaseline = regretBuckets(rows, func(r RegretRow) string { return string(r.FastestClassical) })
	out.Correlations = regretCorrelations(rows)
	out.TopByPenalty = topRows(rows, cfg.Top, func(a, b RegretRow) bool {
		if a.AbsolutePenaltyNS == b.AbsolutePenaltyNS {
			return a.RuntimeRatio > b.RuntimeRatio
		}
		return a.AbsolutePenaltyNS > b.AbsolutePenaltyNS
	})
	out.TopByRatio = topRows(rows, cfg.Top, func(a, b RegretRow) bool {
		if a.RuntimeRatio == b.RuntimeRatio {
			return a.AbsolutePenaltyNS > b.AbsolutePenaltyNS
		}
		return a.RuntimeRatio > b.RuntimeRatio
	})
	return out, nil
}

func isClassical(alg search.Algorithm) bool {
	return alg == search.Dijkstra || alg == search.BiDijkstra || alg == search.AStar
}

func regretBuckets(rows []RegretRow, key func(RegretRow) string) []RegretBucket {
	groups := make(map[string][]RegretRow)
	for _, row := range rows {
		groups[key(row)] = append(groups[key(row)], row)
	}
	keys := make([]string, 0, len(groups))
	for name := range groups {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	out := make([]RegretBucket, 0, len(keys))
	for _, name := range keys {
		group := groups[name]
		ratios := make([]float64, 0, len(group))
		penalties := make([]int64, 0, len(group))
		meaningful := 0
		for _, row := range group {
			ratios = append(ratios, row.RuntimeRatio)
			penalties = append(penalties, row.AbsolutePenaltyNS)
			if row.Meaningful {
				meaningful++
			}
		}
		out = append(out, RegretBucket{Name: name, Queries: len(group), Meaningful: meaningful,
			MeanRuntimeRatio: meanFloat64(ratios), P50RuntimeRatio: percentileFloat64(ratios, .5), P95RuntimeRatio: percentileFloat64(ratios, .95),
			MeanPenaltyNS: meanInt64(penalties), P95PenaltyNS: percentileInt64(penalties, .95)})
	}
	return out
}

func topRows(rows []RegretRow, n int, less func(a, b RegretRow) bool) []RegretRow {
	out := append([]RegretRow(nil), rows...)
	sort.Slice(out, func(i, j int) bool { return less(out[i], out[j]) })
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func regretCorrelations(rows []RegretRow) map[string]float64 {
	y := make([]float64, len(rows))
	features := map[string][]float64{
		"distanceRatio": {}, "sourceDegree": {}, "targetReverseDegree": {}, "expanded": {}, "relaxed": {},
		"forwardShareDeviation": {}, "switchRate": {}, "firstUpperBoundFraction": {}, "staleRate": {}, "efficiencyImbalance": {},
	}
	for i, row := range rows {
		y[i] = float64(row.AbsolutePenaltyNS)
		features["distanceRatio"] = append(features["distanceRatio"], row.DistanceRatio)
		features["sourceDegree"] = append(features["sourceDegree"], float64(row.SourceDegree))
		features["targetReverseDegree"] = append(features["targetReverseDegree"], float64(row.TargetReverseDegree))
		features["expanded"] = append(features["expanded"], float64(row.Expanded))
		features["relaxed"] = append(features["relaxed"], float64(row.Relaxed))
		features["forwardShareDeviation"] = append(features["forwardShareDeviation"], math.Abs(row.ForwardShare-.5))
		features["switchRate"] = append(features["switchRate"], row.SwitchRate)
		features["firstUpperBoundFraction"] = append(features["firstUpperBoundFraction"], row.FirstUpperFraction)
		features["staleRate"] = append(features["staleRate"], row.StaleRate)
		features["efficiencyImbalance"] = append(features["efficiencyImbalance"], row.EfficiencyImbalance)
	}
	out := make(map[string]float64, len(features))
	for name, x := range features {
		out[name] = pearson(x, y)
	}
	return out
}

func pearson(x, y []float64) float64 {
	if len(x) != len(y) || len(x) < 2 {
		return 0
	}
	mx, my := meanFloat64(x), meanFloat64(y)
	var num, dx, dy float64
	for i := range x {
		a, b := x[i]-mx, y[i]-my
		num += a * b
		dx += a * a
		dy += b * b
	}
	if dx == 0 || dy == 0 {
		return 0
	}
	return num / math.Sqrt(dx*dy)
}

func meanFloat64(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var mean float64
	for i, value := range values {
		mean += (value - mean) / float64(i+1)
	}
	return mean
}

func WriteRegretJSON(path string, report RegretReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func WriteRegretCSV(path string, report RegretReport) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	header := []string{"query", "class", "source_id", "target_id", "straight_m", "distance_ratio", "source_degree", "target_reverse_degree", "fastest_classical", "fastest_ms", "algorithm", "algorithm_ms", "runtime_ratio", "penalty_ms", "meaningful", "expanded", "relaxed", "forward_share", "switches", "chunks", "switch_rate", "first_upper_fraction", "stale_rate", "efficiency_imbalance"}
	if err := w.Write(header); err != nil {
		return err
	}
	for _, r := range report.Rows {
		record := []string{
			strconv.Itoa(r.QueryIndex), r.Class, strconv.FormatInt(r.SourceID, 10), strconv.FormatInt(r.TargetID, 10),
			fmt.Sprintf("%.3f", r.StraightLineMeters), fmt.Sprintf("%.6f", r.DistanceRatio), strconv.Itoa(r.SourceDegree), strconv.Itoa(r.TargetReverseDegree),
			string(r.FastestClassical), fmt.Sprintf("%.6f", float64(r.FastestClassicalNS)/1e6), string(r.Algorithm), fmt.Sprintf("%.6f", float64(r.AlgorithmNS)/1e6),
			fmt.Sprintf("%.6f", r.RuntimeRatio), fmt.Sprintf("%.6f", float64(r.AbsolutePenaltyNS)/1e6), strconv.FormatBool(r.Meaningful),
			strconv.FormatUint(r.Expanded, 10), strconv.FormatUint(r.Relaxed, 10), fmt.Sprintf("%.6f", r.ForwardShare),
			strconv.FormatUint(r.DirectionSwitches, 10), strconv.FormatUint(r.Chunks, 10), fmt.Sprintf("%.6f", r.SwitchRate),
			fmt.Sprintf("%.6f", r.FirstUpperFraction), fmt.Sprintf("%.6f", r.StaleRate), fmt.Sprintf("%.6f", r.EfficiencyImbalance),
		}
		if err := w.Write(record); err != nil {
			return err
		}
	}
	return w.Error()
}

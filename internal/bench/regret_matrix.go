package bench

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sahenjp/aegis-acbs/internal/search"
)

type RegretValidationConfig struct {
	Algorithm             search.Algorithm `json:"algorithm"`
	RatioThreshold        float64          `json:"ratioThreshold"`
	PenaltyFloorNS        int64            `json:"penaltyFloorNs"`
	Top                   int              `json:"top"`
	MinimumQueries        int              `json:"minimumQueries"`
	MaximumMeaningfulRate float64          `json:"maximumMeaningfulRate"`
}

type RegretValidationRun struct {
	Path            string  `json:"path"`
	Version         string  `json:"version"`
	GraphName       string  `json:"graphName"`
	Metric          string  `json:"metric"`
	Seed            uint64  `json:"seed"`
	Queries         int     `json:"queries"`
	Meaningful      int     `json:"meaningful"`
	MeaningfulRate  float64 `json:"meaningfulRate"`
	P50RuntimeRatio float64 `json:"p50RuntimeRatio"`
	P95RuntimeRatio float64 `json:"p95RuntimeRatio"`
	MaxRuntimeRatio float64 `json:"maxRuntimeRatio"`
	P95PenaltyNS    int64   `json:"p95PenaltyNs"`
	MaxPenaltyNS    int64   `json:"maxPenaltyNs"`
	AllCorrect      bool    `json:"allCorrect"`
}

type RegretValidationGroup struct {
	Name            string  `json:"name"`
	Runs            int     `json:"runs"`
	Queries         int     `json:"queries"`
	Meaningful      int     `json:"meaningful"`
	MeaningfulRate  float64 `json:"meaningfulRate"`
	P95RuntimeRatio float64 `json:"p95RuntimeRatio"`
	P95PenaltyNS    int64   `json:"p95PenaltyNs"`
	MaxPenaltyNS    int64   `json:"maxPenaltyNs"`
}

type RegretValidationTopRow struct {
	Path string `json:"path"`
	RegretRow
}

type RegretValidationReport struct {
	Version                  string                   `json:"version"`
	GeneratedAt              time.Time                `json:"generatedAt"`
	Config                   RegretValidationConfig   `json:"config"`
	Files                    int                      `json:"files"`
	Runs                     []RegretValidationRun    `json:"runs"`
	TotalQueries             int                      `json:"totalQueries"`
	TotalMeaningful          int                      `json:"totalMeaningful"`
	MeaningfulRate           float64                  `json:"meaningfulRate"`
	MeaningfulRateWilsonLow  float64                  `json:"meaningfulRateWilsonLow95"`
	MeaningfulRateWilsonHigh float64                  `json:"meaningfulRateWilsonHigh95"`
	ZeroEventUpper95         float64                  `json:"zeroEventUpper95"`
	P50RuntimeRatio          float64                  `json:"p50RuntimeRatio"`
	P95RuntimeRatio          float64                  `json:"p95RuntimeRatio"`
	MaxRuntimeRatio          float64                  `json:"maxRuntimeRatio"`
	P50PenaltyNS             int64                    `json:"p50PenaltyNs"`
	P95PenaltyNS             int64                    `json:"p95PenaltyNs"`
	MaxPenaltyNS             int64                    `json:"maxPenaltyNs"`
	ByMetric                 []RegretValidationGroup  `json:"byMetric"`
	ByClass                  []RegretValidationGroup  `json:"byClass"`
	ByBaseline               []RegretValidationGroup  `json:"byBaseline"`
	TopMeaningful            []RegretValidationTopRow `json:"topMeaningful"`
	AllCorrect               bool                     `json:"allCorrect"`
	EnoughQueries            bool                     `json:"enoughQueries"`
	Passed                   bool                     `json:"passed"`
}

type validationRow struct {
	path string
	row  RegretRow
}

func AggregateRegretDirectory(root string, cfg RegretValidationConfig) (RegretValidationReport, error) {
	if cfg.Algorithm == "" {
		cfg.Algorithm = search.Aegis
	}
	if cfg.RatioThreshold <= 0 {
		cfg.RatioThreshold = 1.25
	}
	if cfg.PenaltyFloorNS <= 0 {
		cfg.PenaltyFloorNS = 1_000_000
	}
	if cfg.Top <= 0 {
		cfg.Top = 50
	}
	if cfg.MinimumQueries < 0 {
		return RegretValidationReport{}, errors.New("minimum queries must be >= 0")
	}
	if cfg.MaximumMeaningfulRate < 0 || cfg.MaximumMeaningfulRate > 1 {
		return RegretValidationReport{}, errors.New("maximum meaningful rate must be between 0 and 1")
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return RegretValidationReport{}, err
	}
	var paths []string
	err = filepath.WalkDir(absRoot, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".json") {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return RegretValidationReport{}, err
	}
	sort.Strings(paths)

	out := RegretValidationReport{Version: "regret-validation-v1", GeneratedAt: time.Now().UTC(), Config: cfg, AllCorrect: true}
	var allRows []validationRow
	metricByPath := make(map[string]string)
	for _, path := range paths {
		report, err := LoadReport(path)
		if err != nil || len(report.Samples) == 0 || report.GraphName == "" {
			continue
		}
		diagnostic, err := AnalyzeRegret(report, RegretConfig{Algorithm: cfg.Algorithm, RatioThreshold: cfg.RatioThreshold, PenaltyFloorNS: cfg.PenaltyFloorNS, Top: cfg.Top})
		if err != nil {
			continue
		}
		rel, relErr := filepath.Rel(absRoot, path)
		if relErr != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)
		run := RegretValidationRun{Path: rel, Version: report.Version, GraphName: report.GraphName, Metric: string(report.Metric), Seed: report.Config.Seed, Queries: diagnostic.Queries, Meaningful: diagnostic.MeaningfulQueries, P50RuntimeRatio: diagnostic.P50RuntimeRatio, P95RuntimeRatio: diagnostic.P95RuntimeRatio, MaxRuntimeRatio: diagnostic.MaxRuntimeRatio, P95PenaltyNS: diagnostic.P95PenaltyNS, MaxPenaltyNS: diagnostic.MaxPenaltyNS, AllCorrect: report.AllCorrect}
		if run.Queries > 0 {
			run.MeaningfulRate = float64(run.Meaningful) / float64(run.Queries)
		}
		out.Runs = append(out.Runs, run)
		metricByPath[rel] = run.Metric
		out.TotalQueries += run.Queries
		out.TotalMeaningful += run.Meaningful
		out.AllCorrect = out.AllCorrect && report.AllCorrect
		for _, row := range diagnostic.Rows {
			allRows = append(allRows, validationRow{path: rel, row: row})
		}
	}
	out.Files = len(out.Runs)
	if out.Files == 0 || out.TotalQueries == 0 {
		return RegretValidationReport{}, errors.New("no benchmark JSON reports found")
	}

	ratios := make([]float64, 0, len(allRows))
	penalties := make([]int64, 0, len(allRows))
	for _, item := range allRows {
		ratios = append(ratios, item.row.RuntimeRatio)
		penalties = append(penalties, item.row.AbsolutePenaltyNS)
	}
	out.MeaningfulRate = float64(out.TotalMeaningful) / float64(out.TotalQueries)
	out.MeaningfulRateWilsonLow, out.MeaningfulRateWilsonHigh = wilson95(out.TotalMeaningful, out.TotalQueries)
	if out.TotalMeaningful == 0 {
		out.ZeroEventUpper95 = 1 - math.Pow(0.05, 1/float64(out.TotalQueries))
	}
	out.P50RuntimeRatio = percentileFloat64(ratios, .5)
	out.P95RuntimeRatio = percentileFloat64(ratios, .95)
	out.MaxRuntimeRatio = percentileFloat64(ratios, 1)
	out.P50PenaltyNS = percentileInt64(penalties, .5)
	out.P95PenaltyNS = percentileInt64(penalties, .95)
	out.MaxPenaltyNS = percentileInt64(penalties, 1)
	out.ByMetric = validationGroups(allRows, func(v validationRow) string { return metricByPath[v.path] })
	out.ByClass = validationGroups(allRows, func(v validationRow) string { return v.row.Class })
	out.ByBaseline = validationGroups(allRows, func(v validationRow) string { return string(v.row.FastestClassical) })

	sort.Slice(allRows, func(i, j int) bool {
		if allRows[i].row.Meaningful != allRows[j].row.Meaningful {
			return allRows[i].row.Meaningful
		}
		if allRows[i].row.AbsolutePenaltyNS == allRows[j].row.AbsolutePenaltyNS {
			return allRows[i].row.RuntimeRatio > allRows[j].row.RuntimeRatio
		}
		return allRows[i].row.AbsolutePenaltyNS > allRows[j].row.AbsolutePenaltyNS
	})
	for _, item := range allRows {
		if !item.row.Meaningful {
			continue
		}
		out.TopMeaningful = append(out.TopMeaningful, RegretValidationTopRow{Path: item.path, RegretRow: item.row})
		if len(out.TopMeaningful) >= cfg.Top {
			break
		}
	}
	out.EnoughQueries = out.TotalQueries >= cfg.MinimumQueries
	out.Passed = out.AllCorrect && out.EnoughQueries && out.MeaningfulRate <= cfg.MaximumMeaningfulRate
	return out, nil
}

func validationGroups(rows []validationRow, key func(validationRow) string) []RegretValidationGroup {
	groups := make(map[string][]validationRow)
	for _, row := range rows {
		groups[key(row)] = append(groups[key(row)], row)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]RegretValidationGroup, 0, len(keys))
	for _, key := range keys {
		group := groups[key]
		ratios := make([]float64, 0, len(group))
		penalties := make([]int64, 0, len(group))
		meaningful := 0
		paths := make(map[string]struct{})
		for _, item := range group {
			ratios = append(ratios, item.row.RuntimeRatio)
			penalties = append(penalties, item.row.AbsolutePenaltyNS)
			if item.row.Meaningful {
				meaningful++
			}
			paths[item.path] = struct{}{}
		}
		rate := 0.0
		if len(group) > 0 {
			rate = float64(meaningful) / float64(len(group))
		}
		out = append(out, RegretValidationGroup{Name: key, Runs: len(paths), Queries: len(group), Meaningful: meaningful, MeaningfulRate: rate, P95RuntimeRatio: percentileFloat64(ratios, .95), P95PenaltyNS: percentileInt64(penalties, .95), MaxPenaltyNS: percentileInt64(penalties, 1)})
	}
	return out
}

func wilson95(successes, trials int) (float64, float64) {
	if trials <= 0 {
		return 0, 0
	}
	const z = 1.959963984540054
	n := float64(trials)
	p := float64(successes) / n
	z2 := z * z
	denom := 1 + z2/n
	center := (p + z2/(2*n)) / denom
	margin := z * math.Sqrt((p*(1-p)+z2/(4*n))/n) / denom
	return math.Max(0, center-margin), math.Min(1, center+margin)
}

func WriteRegretValidationJSON(path string, report RegretValidationReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func WriteRegretValidationCSV(path string, report RegretValidationReport) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	if err := w.Write([]string{"path", "version", "graph", "metric", "seed", "queries", "meaningful", "meaningful_rate", "p50_ratio", "p95_ratio", "max_ratio", "p95_penalty_ms", "max_penalty_ms", "all_correct"}); err != nil {
		return err
	}
	for _, run := range report.Runs {
		record := []string{run.Path, run.Version, run.GraphName, run.Metric, strconv.FormatUint(run.Seed, 10), strconv.Itoa(run.Queries), strconv.Itoa(run.Meaningful), fmt.Sprintf("%.9f", run.MeaningfulRate), fmt.Sprintf("%.6f", run.P50RuntimeRatio), fmt.Sprintf("%.6f", run.P95RuntimeRatio), fmt.Sprintf("%.6f", run.MaxRuntimeRatio), fmt.Sprintf("%.6f", float64(run.P95PenaltyNS)/1e6), fmt.Sprintf("%.6f", float64(run.MaxPenaltyNS)/1e6), strconv.FormatBool(run.AllCorrect)}
		if err := w.Write(record); err != nil {
			return err
		}
	}
	return w.Error()
}

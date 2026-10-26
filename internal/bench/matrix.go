package bench

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sahenjp/aegis-acbs/internal/search"
	"github.com/sahenjp/aegis-acbs/internal/version"
)

type MatrixRow struct {
	ReportPath                       string  `json:"reportPath"`
	Version                          string  `json:"version"`
	GraphName                        string  `json:"graphName"`
	GraphSource                      string  `json:"graphSource"`
	Metric                           string  `json:"metric"`
	Profile                          string  `json:"profile"`
	Nodes                            int     `json:"nodes"`
	Edges                            int     `json:"edges"`
	Seed                             uint64  `json:"seed"`
	Queries                          int     `json:"queries"`
	Repeats                          int     `json:"repeats"`
	AllCorrect                       bool    `json:"allCorrect"`
	AegisMedianNS                    int64   `json:"aegisMedianNs"`
	AegisMeanNS                      int64   `json:"aegisMeanNs"`
	AegisMinNS                       int64   `json:"aegisMinNs"`
	AegisMaxNS                       int64   `json:"aegisMaxNs"`
	AegisP95NS                       int64   `json:"aegisP95Ns"`
	AegisP99NS                       int64   `json:"aegisP99Ns"`
	AegisMedianRelaxed               uint64  `json:"aegisMedianRelaxed"`
	AegisMedianExpanded              uint64  `json:"aegisMedianExpanded"`
	AegisMedianQueuePushes           uint64  `json:"aegisMedianQueuePushes"`
	AegisMedianQueuePops             uint64  `json:"aegisMedianQueuePops"`
	AegisMedianStalePops             uint64  `json:"aegisMedianStalePops"`
	AegisMedianPrunedAtPop           uint64  `json:"aegisMedianPrunedAtPop"`
	AegisMedianPrunedAtRelax         uint64  `json:"aegisMedianPrunedAtRelax"`
	AegisMedianAllocBytes            uint64  `json:"aegisMedianAllocBytes"`
	AegisMedianAllocObjects          uint64  `json:"aegisMedianAllocObjects"`
	PeakRSSBytes                     uint64  `json:"peakRssBytes"`
	RatioOfMediansVsDijkstra         float64 `json:"ratioOfMediansVsDijkstra"`
	MedianPerQuerySpeedupVsDijkstra  float64 `json:"medianPerQuerySpeedupVsDijkstra"`
	GeomeanPerQuerySpeedupVsDijkstra float64 `json:"geomeanPerQuerySpeedupVsDijkstra"`
	MedianRuntimeVsFastestClassical  float64 `json:"medianRuntimeVsFastestClassical"`
	P95RuntimeVsFastestClassical     float64 `json:"p95RuntimeVsFastestClassical"`
	MedianClassicalOracleRegret      float64 `json:"medianClassicalOracleRegret"`
	P95ClassicalOracleRegret         float64 `json:"p95ClassicalOracleRegret"`
	// Deprecated aliases for v0.3-v0.6 matrix consumers.
	MedianRelativeRuntimeToFastestBaseline float64 `json:"medianRelativeRuntimeToFastestBaseline,omitempty"`
	P95RelativeRuntimeToFastestBaseline    float64 `json:"p95RelativeRuntimeToFastestBaseline,omitempty"`
	MedianOracleRegret                     float64 `json:"medianOracleRegret,omitempty"`
	P95OracleRegret                        float64 `json:"p95OracleRegret,omitempty"`
}

type MatrixGroup struct {
	GraphName                          string   `json:"graphName"`
	Metric                             string   `json:"metric"`
	Profile                            string   `json:"profile"`
	Runs                               int      `json:"runs"`
	Seeds                              []uint64 `json:"seeds"`
	AllCorrect                         bool     `json:"allCorrect"`
	MedianOfAegisMediansNS             int64    `json:"medianOfAegisMediansNs"`
	MedianOfAegisP95NS                 int64    `json:"medianOfAegisP95Ns"`
	WorstAegisP95NS                    int64    `json:"worstAegisP95Ns"`
	MedianAegisRelaxed                 uint64   `json:"medianAegisRelaxed"`
	MedianAegisExpanded                uint64   `json:"medianAegisExpanded"`
	GeomeanRatioOfMediansVsDijkstra    float64  `json:"geomeanRatioOfMediansVsDijkstra"`
	GeomeanPerQuerySpeedupVsDijkstra   float64  `json:"geomeanPerQuerySpeedupVsDijkstra"`
	MedianP95RuntimeVsFastestClassical float64  `json:"medianP95RuntimeVsFastestClassical"`
	WorstP95RuntimeVsFastestClassical  float64  `json:"worstP95RuntimeVsFastestClassical"`
	MedianP95ClassicalOracleRegret     float64  `json:"medianP95ClassicalOracleRegret"`
	WorstP95ClassicalOracleRegret      float64  `json:"worstP95ClassicalOracleRegret"`
	// Deprecated aliases for v0.3-v0.6 matrix consumers.
	MedianP95RelativeRuntimeToFastest float64 `json:"medianP95RelativeRuntimeToFastest,omitempty"`
	WorstP95RelativeRuntimeToFastest  float64 `json:"worstP95RelativeRuntimeToFastest,omitempty"`
	MedianP95OracleRegret             float64 `json:"medianP95OracleRegret,omitempty"`
	WorstP95OracleRegret              float64 `json:"worstP95OracleRegret,omitempty"`
}

type MatrixReport struct {
	Version     string        `json:"version"`
	GeneratedAt time.Time     `json:"generatedAt"`
	InputDir    string        `json:"inputDir"`
	ReportCount int           `json:"reportCount"`
	AllCorrect  bool          `json:"allCorrect"`
	Rows        []MatrixRow   `json:"rows"`
	Groups      []MatrixGroup `json:"groups"`
}

func AggregateDirectory(inputDir string) (MatrixReport, error) {
	if inputDir == "" {
		return MatrixReport{}, errors.New("input directory is required")
	}
	paths := make([]string, 0)
	err := filepath.WalkDir(inputDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || strings.ToLower(filepath.Ext(path)) != ".json" {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return MatrixReport{}, err
	}
	sort.Strings(paths)
	matrix := MatrixReport{Version: version.Version, GeneratedAt: time.Now().UTC(), InputDir: inputDir, AllCorrect: true}
	for _, path := range paths {
		report, ok, err := readBenchmarkReport(path)
		if err != nil {
			return MatrixReport{}, err
		}
		if !ok {
			continue
		}
		row, ok := matrixRow(path, report)
		if !ok {
			continue
		}
		matrix.Rows = append(matrix.Rows, row)
		if !row.AllCorrect {
			matrix.AllCorrect = false
		}
	}
	if len(matrix.Rows) == 0 {
		return MatrixReport{}, errors.New("no benchmark report JSON files found")
	}
	matrix.ReportCount = len(matrix.Rows)
	matrix.Groups = groupMatrixRows(matrix.Rows)
	return matrix, nil
}

func readBenchmarkReport(path string) (Report, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Report{}, false, err
	}
	var probe struct {
		Version string    `json:"version"`
		Summary []Summary `json:"summary"`
		Config  Config    `json:"config"`
	}
	if err := json.Unmarshal(data, &probe); err != nil || probe.Version == "" || len(probe.Summary) == 0 || probe.Config.Queries == 0 {
		return Report{}, false, nil
	}
	var report Report
	if err := json.Unmarshal(data, &report); err != nil {
		return Report{}, false, fmt.Errorf("decode %s: %w", path, err)
	}
	return report, true, nil
}

func matrixRow(path string, report Report) (MatrixRow, bool) {
	var aegis Summary
	found := false
	for _, summary := range report.Summary {
		if summary.Algorithm == search.Aegis {
			aegis = summary
			found = true
			break
		}
	}
	if !found {
		return MatrixRow{}, false
	}
	runtimeP50 := report.Aegis.MedianRuntimeVsFastestClassical
	if runtimeP50 == 0 {
		runtimeP50 = report.Aegis.MedianRelativeRuntimeToFastestBaseline
	}
	runtimeP95 := report.Aegis.P95RuntimeVsFastestClassical
	if runtimeP95 == 0 {
		runtimeP95 = report.Aegis.P95RelativeRuntimeToFastestBaseline
	}
	regretP50 := report.Aegis.MedianClassicalOracleRegret
	if regretP50 == 0 {
		regretP50 = report.Aegis.MedianOracleRegret
	}
	regretP95 := report.Aegis.P95ClassicalOracleRegret
	if regretP95 == 0 {
		regretP95 = report.Aegis.P95OracleRegret
	}
	return MatrixRow{
		ReportPath: path, Version: report.Version, GraphName: report.GraphName, GraphSource: report.GraphSource,
		Metric: string(report.Metric), Profile: report.Profile, Nodes: report.Nodes, Edges: report.Edges,
		Seed: report.Config.Seed, Queries: report.Config.Queries, Repeats: report.Config.Repeats, AllCorrect: report.AllCorrect,
		AegisMedianNS: aegis.MedianNS, AegisP95NS: aegis.P95NS, AegisP99NS: aegis.P99NS,
		AegisMeanNS: aegis.MeanNS, AegisMinNS: aegis.MinNS, AegisMaxNS: aegis.MaxNS,
		AegisMedianRelaxed: aegis.MedianEdges, AegisMedianExpanded: aegis.MedianExpanded,
		AegisMedianQueuePushes: aegis.MedianQueuePushes, AegisMedianQueuePops: aegis.MedianQueuePops,
		AegisMedianStalePops: aegis.MedianStalePops, AegisMedianPrunedAtPop: aegis.MedianPrunedAtPop,
		AegisMedianPrunedAtRelax:         aegis.MedianPrunedAtRelax,
		AegisMedianAllocBytes:            aegis.MedianAllocBytes,
		AegisMedianAllocObjects:          aegis.MedianAllocObjects,
		PeakRSSBytes:                     report.Memory.PeakRSSBytes,
		RatioOfMediansVsDijkstra:         report.Aegis.RatioOfMediansVsDijkstra,
		MedianPerQuerySpeedupVsDijkstra:  report.Aegis.MedianPerQuerySpeedupVsDijkstra,
		GeomeanPerQuerySpeedupVsDijkstra: report.Aegis.GeomeanPerQuerySpeedupVsDijkstra,
		MedianRuntimeVsFastestClassical:  runtimeP50, P95RuntimeVsFastestClassical: runtimeP95,
		MedianClassicalOracleRegret: regretP50, P95ClassicalOracleRegret: regretP95,
		MedianRelativeRuntimeToFastestBaseline: runtimeP50, P95RelativeRuntimeToFastestBaseline: runtimeP95,
		MedianOracleRegret: regretP50, P95OracleRegret: regretP95,
	}, true
}

func groupMatrixRows(rows []MatrixRow) []MatrixGroup {
	groups := make(map[string][]MatrixRow)
	for _, row := range rows {
		key := row.GraphName + "\x00" + row.Metric + "\x00" + row.Profile
		groups[key] = append(groups[key], row)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]MatrixGroup, 0, len(keys))
	for _, key := range keys {
		items := groups[key]
		medians, p95s := []int64{}, []int64{}
		relaxed, expanded := []uint64{}, []uint64{}
		ratioSpeedups, geomeanSpeedups := []float64{}, []float64{}
		p95Relative, p95Regret := []float64{}, []float64{}
		seedSet := map[uint64]struct{}{}
		group := MatrixGroup{GraphName: items[0].GraphName, Metric: items[0].Metric, Profile: items[0].Profile, Runs: len(items), AllCorrect: true}
		for _, item := range items {
			seedSet[item.Seed] = struct{}{}
			if !item.AllCorrect {
				group.AllCorrect = false
			}
			medians = append(medians, item.AegisMedianNS)
			p95s = append(p95s, item.AegisP95NS)
			relaxed = append(relaxed, item.AegisMedianRelaxed)
			expanded = append(expanded, item.AegisMedianExpanded)
			ratioSpeedups = append(ratioSpeedups, item.RatioOfMediansVsDijkstra)
			geomeanSpeedups = append(geomeanSpeedups, item.GeomeanPerQuerySpeedupVsDijkstra)
			p95Relative = append(p95Relative, item.P95RuntimeVsFastestClassical)
			p95Regret = append(p95Regret, item.P95ClassicalOracleRegret)
		}
		for seed := range seedSet {
			group.Seeds = append(group.Seeds, seed)
		}
		sort.Slice(group.Seeds, func(i, j int) bool { return group.Seeds[i] < group.Seeds[j] })
		group.MedianOfAegisMediansNS = percentileInt64(medians, .5)
		group.MedianOfAegisP95NS = percentileInt64(p95s, .5)
		group.WorstAegisP95NS = percentileInt64(p95s, 1)
		group.MedianAegisRelaxed = percentileUint64(relaxed, .5)
		group.MedianAegisExpanded = percentileUint64(expanded, .5)
		group.GeomeanRatioOfMediansVsDijkstra = geometricMean(ratioSpeedups)
		group.GeomeanPerQuerySpeedupVsDijkstra = geometricMean(geomeanSpeedups)
		group.MedianP95RuntimeVsFastestClassical = percentileFloat64(p95Relative, .5)
		group.WorstP95RuntimeVsFastestClassical = percentileFloat64(p95Relative, 1)
		group.MedianP95ClassicalOracleRegret = percentileFloat64(p95Regret, .5)
		group.WorstP95ClassicalOracleRegret = percentileFloat64(p95Regret, 1)
		group.MedianP95RelativeRuntimeToFastest = group.MedianP95RuntimeVsFastestClassical
		group.WorstP95RelativeRuntimeToFastest = group.WorstP95RuntimeVsFastestClassical
		group.MedianP95OracleRegret = group.MedianP95ClassicalOracleRegret
		group.WorstP95OracleRegret = group.WorstP95ClassicalOracleRegret
		out = append(out, group)
	}
	return out
}

func WriteMatrixJSON(path string, report MatrixReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func WriteMatrixCSV(path string, report MatrixReport) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	defer writer.Flush()
	header := []string{"graph", "metric", "profile", "seed", "queries", "repeats", "correct", "acbs_mean_ms", "acbs_p50_ms", "acbs_best_ms", "acbs_worst_ms", "acbs_p95_ms", "acbs_p99_ms", "relaxed", "expanded", "queue_pushes", "queue_pops", "stale_pops", "pruned_at_pop", "pruned_at_relax", "alloc_bytes", "alloc_objects", "peak_rss_bytes", "ratio_of_medians_vs_dijkstra", "geomean_speedup_vs_dijkstra", "p95_runtime_vs_fastest_classical", "p95_classical_oracle_regret", "report"}
	if err := writer.Write(header); err != nil {
		return err
	}
	for _, row := range report.Rows {
		record := []string{
			row.GraphName, row.Metric, row.Profile, strconv.FormatUint(row.Seed, 10), strconv.Itoa(row.Queries), strconv.Itoa(row.Repeats), strconv.FormatBool(row.AllCorrect),
			fmt.Sprintf("%.6f", float64(row.AegisMeanNS)/1e6), fmt.Sprintf("%.6f", float64(row.AegisMedianNS)/1e6), fmt.Sprintf("%.6f", float64(row.AegisMinNS)/1e6), fmt.Sprintf("%.6f", float64(row.AegisMaxNS)/1e6), fmt.Sprintf("%.6f", float64(row.AegisP95NS)/1e6), fmt.Sprintf("%.6f", float64(row.AegisP99NS)/1e6),
			strconv.FormatUint(row.AegisMedianRelaxed, 10), strconv.FormatUint(row.AegisMedianExpanded, 10), strconv.FormatUint(row.AegisMedianQueuePushes, 10), strconv.FormatUint(row.AegisMedianQueuePops, 10), strconv.FormatUint(row.AegisMedianStalePops, 10), strconv.FormatUint(row.AegisMedianPrunedAtPop, 10), strconv.FormatUint(row.AegisMedianPrunedAtRelax, 10),
			strconv.FormatUint(row.AegisMedianAllocBytes, 10), strconv.FormatUint(row.AegisMedianAllocObjects, 10), strconv.FormatUint(row.PeakRSSBytes, 10),
			fmt.Sprintf("%.6f", row.RatioOfMediansVsDijkstra), fmt.Sprintf("%.6f", row.GeomeanPerQuerySpeedupVsDijkstra), fmt.Sprintf("%.6f", row.P95RuntimeVsFastestClassical), fmt.Sprintf("%.6f", row.P95ClassicalOracleRegret), row.ReportPath,
		}
		if err := writer.Write(record); err != nil {
			return err
		}
	}
	return writer.Error()
}

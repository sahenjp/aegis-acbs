package bench

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sahenjp/aegis-acbs/internal/graph"
	"github.com/sahenjp/aegis-acbs/internal/search"
)

func TestReportIncludesACBSMetricsAndHTML(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sub-microsecond fixture timing is below reliable Windows timer resolution")
	}
	g := graph.New("tiny-road", "fixture", "car", graph.MetricDistance)
	g.Nodes = []graph.Node{
		{ID: 1, Lat: 35.000, Lon: 139.000},
		{ID: 2, Lat: 35.001, Lon: 139.001},
		{ID: 3, Lat: 35.002, Lon: 139.002},
		{ID: 4, Lat: 35.003, Lon: 139.003},
	}
	g.Adj = [][]graph.Edge{
		{{To: 1, Cost: 150_000}},
		{{To: 0, Cost: 150_000}, {To: 2, Cost: 150_000}},
		{{To: 1, Cost: 150_000}, {To: 3, Cost: 150_000}},
		{{To: 2, Cost: 150_000}},
	}
	if err := g.Finalize(); err != nil {
		t.Fatal(err)
	}
	r, err := Run(context.Background(), g, Config{
		Queries: 9, Repeats: 3, BatchSize: 64, Seed: 1010,
		Algorithms: []search.Algorithm{search.Dijkstra, search.BiDijkstra, search.AStar, search.Aegis},
		Suite:      "mixed", PairMode: "strongly-connected", MeasureMemory: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !r.AllCorrect {
		t.Fatal("expected all algorithms to match")
	}
	if r.Aegis.Comparisons != 9 {
		t.Fatalf("unexpected ACBS summary: %+v", r.Aegis)
	}
	if len(r.ClassSummary) == 0 || len(r.Aegis.RuntimeComparisons) != 9 || len(r.Aegis.DirectionByClass) == 0 {
		t.Fatal("missing visual summary data")
	}
	if r.Aegis.MedianClassicalOracleRegret < 1 || r.Aegis.MedianRuntimeVsFastestClassical <= 0 || r.Aegis.RatioOfMediansVsDijkstra <= 0 {
		t.Fatalf("invalid benchmark semantics: %+v", r.Aegis)
	}
	if r.Config.Order != "interleaved" || !r.Memory.Measured || r.Memory.GoHeapSysBytes == 0 {
		t.Fatalf("missing measurement metadata: config=%+v memory=%+v", r.Config, r.Memory)
	}
	if peakRSSSupported() && r.Memory.PeakRSSBytes == 0 {
		t.Fatalf("missing peak RSS on %s: %+v", runtime.GOOS, r.Memory)
	}
	for _, summary := range r.Summary {
		if summary.Runs != 9 || summary.Correct != 9 {
			t.Fatalf("incomplete benchmark summary: %+v", summary)
		}
		if summary.MinNS <= 0 || summary.MeanNS < summary.MinNS || summary.MaxNS < summary.MeanNS {
			t.Fatalf("invalid descriptive statistics: %+v", summary)
		}
	}
	path := filepath.Join(t.TempDir(), "report.html")
	if err := WriteHTML(path, r); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(b)
	if !strings.Contains(html, "ACBS benchmark") || strings.Contains(html, "__AEGIS_REPORT_BASE64__") {
		t.Fatal("standalone report was not rendered")
	}
}

func TestInterleavedOrderIsDeterministicAndVariesByRepeat(t *testing.T) {
	algs := []search.Algorithm{search.Dijkstra, search.BiDijkstra, search.AStar, search.Aegis}
	a := shuffled(algs, 1010, 4, 2)
	b := shuffled(algs, 1010, 4, 2)
	c := shuffled(algs, 1010, 4, 3)
	if fmt.Sprint(a) != fmt.Sprint(b) {
		t.Fatalf("same seed did not reproduce order: %v vs %v", a, b)
	}
	if fmt.Sprint(a) == fmt.Sprint(c) {
		t.Fatalf("adjacent repeats unexpectedly used identical order: %v", a)
	}
}

func TestAggregateDirectoryBuildsMultiSeedMatrix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sub-microsecond fixture timing is below reliable Windows timer resolution")
	}
	g := graph.New("matrix-road", "fixture", "car", graph.MetricDistance)
	g.Nodes = []graph.Node{
		{ID: 1, Lat: 35.000, Lon: 139.000},
		{ID: 2, Lat: 35.001, Lon: 139.001},
		{ID: 3, Lat: 35.002, Lon: 139.002},
		{ID: 4, Lat: 35.003, Lon: 139.003},
	}
	g.Adj = [][]graph.Edge{
		{{To: 1, Cost: 150_000}, {To: 2, Cost: 340_000}},
		{{To: 0, Cost: 150_000}, {To: 2, Cost: 150_000}},
		{{To: 1, Cost: 150_000}, {To: 3, Cost: 150_000}},
		{{To: 2, Cost: 150_000}},
	}
	if err := g.Finalize(); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, seed := range []uint64{1010, 20260717} {
		report, err := Run(context.Background(), g, Config{
			Queries: 9, Repeats: 3, BatchSize: 64, Seed: seed,
			Algorithms: []search.Algorithm{search.Dijkstra, search.BiDijkstra, search.AStar, search.Aegis},
			Suite:      "mixed", PairMode: "strongly-connected",
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := WriteJSON(filepath.Join(dir, fmt.Sprintf("seed-%d.json", seed)), report); err != nil {
			t.Fatal(err)
		}
	}
	matrix, err := AggregateDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	if matrix.ReportCount != 2 || len(matrix.Groups) != 1 || !matrix.AllCorrect {
		t.Fatalf("unexpected matrix: %+v", matrix)
	}
	if matrix.Groups[0].Runs != 2 || len(matrix.Groups[0].Seeds) != 2 || matrix.Groups[0].WorstP95ClassicalOracleRegret < 1 {
		t.Fatalf("unexpected matrix group: %+v", matrix.Groups[0])
	}
	for _, row := range matrix.Rows {
		if row.AegisMeanNS <= 0 || row.AegisMinNS <= 0 || row.AegisMaxNS < row.AegisMeanNS {
			t.Fatalf("missing matrix latency telemetry: %+v", row)
		}
		if peakRSSSupported() && row.PeakRSSBytes == 0 {
			t.Fatalf("missing matrix peak RSS on %s: %+v", runtime.GOOS, row)
		}
	}
	if err := WriteMatrixJSON(filepath.Join(dir, "matrix.json"), matrix); err != nil {
		t.Fatal(err)
	}
	if err := WriteMatrixCSV(filepath.Join(dir, "matrix.csv"), matrix); err != nil {
		t.Fatal(err)
	}
	htmlPath := filepath.Join(dir, "matrix.html")
	if err := WriteMatrixHTML(htmlPath, matrix); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(htmlPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Benchmark Matrix") || strings.Contains(string(data), "__AEGIS_MATRIX_BASE64__") {
		t.Fatal("matrix HTML was not rendered")
	}
}

func TestStressRunsConcurrentACBSAndVerifiesSamples(t *testing.T) {
	g := graph.New("stress-road", "fixture", "car", graph.MetricDistance)
	g.Nodes = []graph.Node{
		{ID: 1, Lat: 35.000, Lon: 139.000},
		{ID: 2, Lat: 35.001, Lon: 139.001},
		{ID: 3, Lat: 35.002, Lon: 139.002},
		{ID: 4, Lat: 35.003, Lon: 139.003},
		{ID: 5, Lat: 35.004, Lon: 139.004},
	}
	g.Adj = [][]graph.Edge{
		{{To: 1, Cost: 150_000}, {To: 2, Cost: 310_000}},
		{{To: 0, Cost: 150_000}, {To: 2, Cost: 150_000}},
		{{To: 1, Cost: 150_000}, {To: 3, Cost: 150_000}},
		{{To: 2, Cost: 150_000}, {To: 4, Cost: 150_000}},
		{{To: 3, Cost: 150_000}},
	}
	if err := g.Finalize(); err != nil {
		t.Fatal(err)
	}
	report, err := RunStress(context.Background(), g, StressConfig{
		Queries: 120, Workers: 6, Seed: 7070, Algorithm: search.Aegis,
		VerifyEvery: 3, Suite: "mixed", PairMode: "strongly-connected",
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Completed != 120 || report.Verified == 0 || report.Correct != report.Verified || report.Errors != 0 || !report.AllVerifiedCorrect {
		t.Fatalf("unexpected stress result: %+v", report)
	}
	if report.ThroughputQPS <= 0 || report.WallDurationNS <= 0 || report.Memory.GoHeapSysBytes == 0 {
		t.Fatalf("missing stress telemetry: %+v", report)
	}
	if peakRSSSupported() && report.Memory.PeakRSSBytes == 0 {
		t.Fatalf("missing stress peak RSS on %s: %+v", runtime.GOOS, report)
	}
	path := filepath.Join(t.TempDir(), "stress.json")
	if err := WriteStressJSON(path, report); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || !strings.Contains(string(data), "throughputQps") {
		t.Fatalf("stress report not written: err=%v", err)
	}
}

func peakRSSSupported() bool {
	return runtime.GOOS == "linux" || runtime.GOOS == "darwin"
}

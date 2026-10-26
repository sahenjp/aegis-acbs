package bench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/sahenjp/aegis-acbs/internal/graph"
	"github.com/sahenjp/aegis-acbs/internal/search"
)

func TestAnalyzeRegretSeparatesRatioNoiseFromMeaningfulPenalty(t *testing.T) {
	report := Report{
		Version:        "test",
		GraphName:      "time-road",
		Metric:         graph.MetricTime,
		DiameterMeters: 10_000,
		Samples: []Sample{
			{QueryIndex: 0, QueryClass: "local", StraightLineMeters: 100, Stats: search.Stats{Algorithm: search.Dijkstra, DurationNS: 1_000}, Correct: true},
			{QueryIndex: 0, QueryClass: "local", StraightLineMeters: 100, Stats: search.Stats{Algorithm: search.BiDijkstra, DurationNS: 2_000}, Correct: true},
			{QueryIndex: 0, QueryClass: "local", StraightLineMeters: 100, Stats: search.Stats{Algorithm: search.Aegis, DurationNS: 2_000, Expanded: 10}, Correct: true},
			{QueryIndex: 1, QueryClass: "regional", StraightLineMeters: 8_000, Stats: search.Stats{Algorithm: search.Dijkstra, DurationNS: 10_000_000}, Correct: true},
			{QueryIndex: 1, QueryClass: "regional", StraightLineMeters: 8_000, Stats: search.Stats{Algorithm: search.AStar, DurationNS: 8_000_000}, Correct: true},
			{QueryIndex: 1, QueryClass: "regional", StraightLineMeters: 8_000, Stats: search.Stats{Algorithm: search.Aegis, DurationNS: 12_000_000, Expanded: 100, ForwardExpanded: 70, BackwardExpanded: 30, Chunks: 10, DirectionSwitches: 4}, Correct: true},
		},
	}
	got, err := AnalyzeRegret(report, RegretConfig{Algorithm: search.Aegis, RatioThreshold: 1.25, PenaltyFloorNS: 1_000_000, Top: 5})
	if err != nil {
		t.Fatal(err)
	}
	if got.Queries != 2 || got.MeaningfulQueries != 1 {
		t.Fatalf("unexpected counts: %+v", got)
	}
	if got.TopByRatio[0].QueryIndex != 0 || got.TopByRatio[0].Meaningful {
		t.Fatalf("ratio-only microbenchmark noise should not be meaningful: %+v", got.TopByRatio[0])
	}
	if got.TopByPenalty[0].QueryIndex != 1 || !got.TopByPenalty[0].Meaningful {
		t.Fatalf("large absolute penalty should be meaningful: %+v", got.TopByPenalty[0])
	}
	if got.TopByPenalty[0].DistanceRatio < .79 || got.TopByPenalty[0].DistanceRatio > .81 {
		t.Fatalf("distance ratio fallback failed: %+v", got.TopByPenalty[0])
	}
}

func TestAggregateRegretDirectoryZeroEventConfidence(t *testing.T) {
	dir := t.TempDir()
	for i, seed := range []uint64{1010, 2020} {
		report := Report{Version: "test", GraphName: "road", Metric: graph.MetricTime, DiameterMeters: 10_000, Config: Config{Seed: seed}, AllCorrect: true}
		for q := 0; q < 50; q++ {
			base := int64(10_000_000 + q)
			report.Samples = append(report.Samples,
				Sample{QueryIndex: q, QueryClass: "mixed", Stats: search.Stats{Algorithm: search.Dijkstra, DurationNS: base}, Correct: true},
				Sample{QueryIndex: q, QueryClass: "mixed", Stats: search.Stats{Algorithm: search.BiDijkstra, DurationNS: base - 1_000_000}, Correct: true},
				Sample{QueryIndex: q, QueryClass: "mixed", Stats: search.Stats{Algorithm: search.Aegis, DurationNS: base - 2_000_000}, Correct: true})
		}
		data, _ := json.Marshal(report)
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("run-%d.json", i)), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := AggregateRegretDirectory(dir, RegretValidationConfig{Algorithm: search.Aegis, RatioThreshold: 1.25, PenaltyFloorNS: 1_000_000, MinimumQueries: 100, MaximumMeaningfulRate: 0})
	if err != nil {
		t.Fatal(err)
	}
	if got.Files != 2 || got.TotalQueries != 100 || got.TotalMeaningful != 0 || !got.Passed {
		t.Fatalf("unexpected aggregate: %+v", got)
	}
	if got.ZeroEventUpper95 < .029 || got.ZeroEventUpper95 > .0305 {
		t.Fatalf("unexpected upper bound: %g", got.ZeroEventUpper95)
	}
}

func TestAggregateRegretDirectoryFailsMeaningfulSlowdown(t *testing.T) {
	dir := t.TempDir()
	report := Report{Version: "test", GraphName: "road", Metric: graph.MetricTime, Config: Config{Seed: 7}, AllCorrect: true, Samples: []Sample{
		{QueryIndex: 0, QueryClass: "regional", Stats: search.Stats{Algorithm: search.Dijkstra, DurationNS: 10_000_000}, Correct: true},
		{QueryIndex: 0, QueryClass: "regional", Stats: search.Stats{Algorithm: search.Aegis, DurationNS: 14_000_000}, Correct: true},
	}}
	data, _ := json.Marshal(report)
	if err := os.WriteFile(filepath.Join(dir, "report.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := AggregateRegretDirectory(dir, RegretValidationConfig{MinimumQueries: 1, MaximumMeaningfulRate: 0})
	if err != nil {
		t.Fatal(err)
	}
	if got.TotalMeaningful != 1 || got.Passed || len(got.TopMeaningful) != 1 {
		t.Fatalf("meaningful slowdown was not retained: %+v", got)
	}
}

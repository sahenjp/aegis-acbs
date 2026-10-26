package bench

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sahenjp/aegis-acbs/internal/graph"
	"github.com/sahenjp/aegis-acbs/internal/search"
)

func TestReplayRegretRemeasuresAndTracesValidationCase(t *testing.T) {
	g := replayGridGraph(t, 24, 24)
	dir := t.TempDir()
	source := Report{
		Version: "test", GraphName: g.Name, Metric: g.Metric, AllCorrect: true,
		Queries: []Query{{Source: 0, Target: len(g.Nodes) - 1, Class: "regional"}},
	}
	sourceData, _ := json.Marshal(source)
	if err := os.MkdirAll(filepath.Join(dir, "seed-7"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "seed-7", "report.json"), sourceData, 0o644); err != nil {
		t.Fatal(err)
	}
	validation := RegretValidationReport{
		Version: "test", TotalQueries: 1, TotalMeaningful: 1,
		TopMeaningful: []RegretValidationTopRow{{Path: "seed-7/report.json", RegretRow: RegretRow{
			QueryIndex: 0, Class: "regional", SourceID: g.Nodes[0].ID, TargetID: g.Nodes[len(g.Nodes)-1].ID,
			StraightLineMeters: graph.HaversineMeters(g.Nodes[0].Lat, g.Nodes[0].Lon, g.Nodes[len(g.Nodes)-1].Lat, g.Nodes[len(g.Nodes)-1].Lon),
			FastestClassical:   search.AStar, RuntimeRatio: 1.5, AbsolutePenaltyNS: int64(2 * time.Millisecond), Meaningful: true,
		}}},
	}
	validationData, _ := json.Marshal(validation)
	validationPath := filepath.Join(dir, "regret-validation.json")
	if err := os.WriteFile(validationPath, validationData, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ReplayRegret(context.Background(), g, validationPath, dir, RegretReplayConfig{Runs: 3, Warmup: 1, Timeout: 5 * time.Second, RatioThreshold: 1.25, PenaltyFloorNS: int64(time.Millisecond), Top: 5})
	if err != nil {
		t.Fatal(err)
	}
	if got.ReplayedCases != 1 || len(got.Cases) != 1 || !got.AllCorrect {
		t.Fatalf("unexpected replay: %+v", got)
	}
	c := got.Cases[0]
	if len(c.Algorithms) != 9 || len(c.Guards) != 3 || len(c.Trace) == 0 || c.SourceID != g.Nodes[0].ID || c.TargetID != g.Nodes[len(g.Nodes)-1].ID {
		t.Fatalf("incomplete replay case: %+v", c)
	}
}

func replayGridGraph(t testing.TB, rows, cols int) *graph.Graph {
	t.Helper()
	g := graph.New("replay-grid", "", "car", graph.MetricDistance)
	g.Nodes = make([]graph.Node, rows*cols)
	g.Adj = make([][]graph.Edge, rows*cols)
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			i := r*cols + c
			g.Nodes[i] = graph.Node{ID: int64(i + 1), Lat: 35 + float64(r)*.001, Lon: 139 + float64(c)*.001}
			if c > 0 {
				g.Adj[i] = append(g.Adj[i], graph.Edge{To: i - 1, Cost: 100_000})
			}
			if c+1 < cols {
				g.Adj[i] = append(g.Adj[i], graph.Edge{To: i + 1, Cost: 100_000})
			}
			if r > 0 {
				g.Adj[i] = append(g.Adj[i], graph.Edge{To: i - cols, Cost: 100_000})
			}
			if r+1 < rows {
				g.Adj[i] = append(g.Adj[i], graph.Edge{To: i + cols, Cost: 100_000})
			}
		}
	}
	if err := g.Finalize(); err != nil {
		t.Fatal(err)
	}
	return g
}

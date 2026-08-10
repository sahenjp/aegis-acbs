package search

import (
	"context"
	"math/rand"
	"testing"

	"github.com/lasder-ca/aegis-acbs/internal/graph"
)

func TestBidirectionalAlgorithmsMatchDijkstraExhaustiveSmallDirected(t *testing.T) {
	ctx := context.Background()
	for n := 2; n <= 4; n++ {
		pairs := make([][2]int, 0, n*(n-1))
		for i := 0; i < n; i++ {
			for j := 0; j < n; j++ {
				if i != j {
					pairs = append(pairs, [2]int{i, j})
				}
			}
		}
		total := 1 << len(pairs)
		for mask := 0; mask < total; mask++ {
			g := graph.New("test", "", "", graph.MetricDistance)
			g.Nodes = make([]graph.Node, n)
			g.Adj = make([][]graph.Edge, n)
			for i := range g.Nodes {
				g.Nodes[i] = graph.Node{ID: int64(i + 1), Lat: 35 + float64(i)*.001, Lon: 139 + float64(i)*.001}
			}
			for bit, p := range pairs {
				if mask&(1<<bit) != 0 {
					g.Adj[p[0]] = append(g.Adj[p[0]], graph.Edge{To: p[1], Cost: uint64(1 + (bit % 7))})
				}
			}
			if err := g.Finalize(); err != nil {
				t.Fatal(err)
			}
			for s := 0; s < n; s++ {
				for d := 0; d < n; d++ {
					a, err := Run(ctx, g, s, d, Dijkstra)
					if err != nil {
						t.Fatal(err)
					}
					for _, alg := range []Algorithm{BiDijkstra, Aegis, AegisEntropic, AegisLateGuard, AegisConnect32, AegisConnect40, AegisConnect32x16, AegisPrune, AegisProjection, AegisStatic, AegisNoPrune} {
						b, err := Run(ctx, g, s, d, alg)
						if err != nil {
							t.Fatal(err)
						}
						if a.Stats.Reachable != b.Stats.Reachable || a.Stats.Distance != b.Stats.Distance || (b.Stats.Reachable && !Validate(g, s, d, b)) {
							t.Fatalf("n=%d mask=%d %d->%d alg=%s dijkstra=%+v got=%+v", n, mask, s, d, alg, a.Stats, b.Stats)
						}
					}
				}
			}
		}
	}
}

func TestAStarAndAegisMatchDijkstraRoadLike(t *testing.T) {
	rnd := rand.New(rand.NewSource(1010))
	ctx := context.Background()
	for caseNo := 0; caseNo < 100; caseNo++ {
		n := 25 + rnd.Intn(50)
		g := graph.New("road", "", "car", graph.MetricDistance)
		g.Nodes = make([]graph.Node, n)
		g.Adj = make([][]graph.Edge, n)
		for i := 0; i < n; i++ {
			g.Nodes[i] = graph.Node{ID: int64(i + 1), Lat: 35 + rnd.Float64()*.1, Lon: 139 + rnd.Float64()*.1}
		}
		for i := 0; i < n; i++ {
			for j := i + 1; j < n; j++ {
				if rnd.Float64() < .08 {
					m := graph.HaversineMeters(g.Nodes[i].Lat, g.Nodes[i].Lon, g.Nodes[j].Lat, g.Nodes[j].Lon)
					cost := uint64(m*1000) + uint64(rnd.Intn(5000))
					g.Adj[i] = append(g.Adj[i], graph.Edge{To: j, Cost: cost})
					g.Adj[j] = append(g.Adj[j], graph.Edge{To: i, Cost: cost})
				}
			}
		}
		if err := g.Finalize(); err != nil {
			t.Fatal(err)
		}
		for q := 0; q < 30; q++ {
			s, d := rnd.Intn(n), rnd.Intn(n)
			a, _ := Run(ctx, g, s, d, Dijkstra)
			for _, alg := range []Algorithm{AStar, BiDijkstra, Aegis, AegisEntropic, AegisLateGuard, AegisConnect32, AegisConnect40, AegisConnect32x16, AegisPrune, AegisProjection, AegisStatic, AegisNoPrune, AegisRace} {
				b, err := Run(ctx, g, s, d, alg)
				if err != nil {
					t.Fatal(err)
				}
				if a.Stats.Reachable != b.Stats.Reachable || a.Stats.Distance != b.Stats.Distance {
					t.Fatalf("case=%d alg=%s %d->%d expected=%+v got=%+v", caseNo, alg, s, d, a.Stats, b.Stats)
				}
			}
		}
	}
}

func TestLegacyPortfolioUsesDijkstraForTinyGraph(t *testing.T) {
	g := gridGraph(t, 10, 10, true)
	if got := Select(g, 0, len(g.Nodes)-1); got != Dijkstra {
		t.Fatalf("expected dijkstra for tiny graph, got %s", got)
	}
	d := Explain(g, 0, len(g.Nodes)-1)
	if d.Selected != Dijkstra || d.Reason != "small_graph" {
		t.Fatalf("unexpected decision: %+v", d)
	}
}

func TestLegacyPortfolioUsesAStarForLargeRegionalRoadGraph(t *testing.T) {
	g := gridGraph(t, 72, 72, true)
	if got := Select(g, 0, len(g.Nodes)-1); got != AStar {
		t.Fatalf("expected astar for large regional road graph, got %s; decision=%+v", got, Explain(g, 0, len(g.Nodes)-1))
	}
}

func TestLegacyPortfolioUsesBiDijkstraWithoutCoordinates(t *testing.T) {
	g := gridGraph(t, 72, 72, false)
	if got := Select(g, 0, len(g.Nodes)-1); got != BiDijkstra {
		t.Fatalf("expected bidijkstra without coordinates, got %s; decision=%+v", got, Explain(g, 0, len(g.Nodes)-1))
	}
}

func gridGraph(t testing.TB, rows, cols int, coordinates bool) *graph.Graph {
	t.Helper()
	n := rows * cols
	g := graph.New("grid", "", "car", graph.MetricDistance)
	g.Nodes = make([]graph.Node, n)
	g.Adj = make([][]graph.Edge, n)
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			i := r*cols + c
			lat, lon := 0.0, 0.0
			if coordinates {
				lat = 35 + float64(r)*0.001
				lon = 139 + float64(c)*0.001
			}
			g.Nodes[i] = graph.Node{ID: int64(i + 1), Lat: lat, Lon: lon}
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

func TestLegacyPortfolioSplitsTimeRoutesByDistance(t *testing.T) {
	g := gridGraph(t, 72, 72, true)
	g.Metric = graph.MetricTime
	g.HeuristicStrength = 0.25

	localTarget := 8*72 + 8
	if got := Select(g, 0, localTarget); got != AStar {
		t.Fatalf("expected astar for local time route, got %s; decision=%+v", got, Explain(g, 0, localTarget))
	}
	if got := Select(g, 0, len(g.Nodes)-1); got != BiDijkstra {
		t.Fatalf("expected bidijkstra for long time route, got %s; decision=%+v", got, Explain(g, 0, len(g.Nodes)-1))
	}
	local := Explain(g, 0, localTarget)
	if local.PolicyVersion != "road-v3-time-aware" || local.AStarRatioLimit <= 0 || local.Metric != graph.MetricTime {
		t.Fatalf("unexpected time decision metadata: %+v", local)
	}
}

func TestWorkspaceReuseDoesNotLeakDistances(t *testing.T) {
	g := gridGraph(t, 20, 20, true)
	ctx := context.Background()
	pairs := [][2]int{{0, 399}, {399, 0}, {10, 11}, {50, 350}, {200, 25}}
	for round := 0; round < 50; round++ {
		for _, pair := range pairs {
			want, err := Run(ctx, g, pair[0], pair[1], Dijkstra)
			if err != nil {
				t.Fatal(err)
			}
			for _, alg := range []Algorithm{AStar, BiDijkstra, Aegis, AegisEntropic} {
				got, err := Run(ctx, g, pair[0], pair[1], alg)
				if err != nil {
					t.Fatal(err)
				}
				if got.Stats.Reachable != want.Stats.Reachable || got.Stats.Distance != want.Stats.Distance || !Validate(g, pair[0], pair[1], got) {
					t.Fatalf("round=%d pair=%v alg=%s want=%+v got=%+v", round, pair, alg, want.Stats, got.Stats)
				}
			}
		}
	}
}

func TestACBSUsesBothDirectionsOnLargeGrid(t *testing.T) {
	g := gridGraph(t, 72, 72, true)
	r, err := Run(context.Background(), g, 0, len(g.Nodes)-1, Aegis)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Stats.Reachable || !Validate(g, 0, len(g.Nodes)-1, r) {
		t.Fatalf("invalid ACBS result: %+v", r.Stats)
	}
	if r.Stats.ForwardExpanded == 0 || r.Stats.BackwardExpanded == 0 || r.Stats.Chunks < 2 {
		t.Fatalf("expected coupled bidirectional work, got %+v", r.Stats)
	}
	if r.Stats.TerminationLowerBound > r.Stats.Distance {
		t.Fatalf("lower bound exceeds solution: %+v", r.Stats)
	}
}

func TestACBSBalancedPotentialsHaveNonnegativeReducedEdges(t *testing.T) {
	g := gridGraph(t, 40, 40, true)
	source, target := 0, len(g.Nodes)-1
	for _, projection := range []bool{false, true} {
		w := acquireBiWorkspace(len(g.Nodes))
		potential := newACBSPotential(g, source, target, projection)
		for from := range g.Nodes {
			edges := g.OutEdges(from)
			phiFrom, _ := w.potential(g, potential, from)
			for _, e := range edges {
				phiTo, _ := w.potential(g, potential, e.To)
				forward := int64(2*e.Cost) + phiTo - phiFrom
				backward := int64(2*e.Cost) + phiFrom - phiTo
				if forward < 0 || backward < 0 {
					t.Fatalf("projection=%v negative reduced edge %d->%d: forward=%d backward=%d", projection, from, e.To, forward, backward)
				}
			}
		}
		releaseBiWorkspace(w)
	}
}

func TestChordPotentialIsAdmissibleAgainstGreatCircle(t *testing.T) {
	rnd := rand.New(rand.NewSource(20260717))
	for i := 0; i < 10_000; i++ {
		a := graph.Node{Lat: -80 + rnd.Float64()*160, Lon: -180 + rnd.Float64()*360}
		b := graph.Node{Lat: -80 + rnd.Float64()*160, Lon: -180 + rnd.Float64()*360}
		ax, ay, az := graph.EarthUnit(a.Lat, a.Lon)
		bx, by, bz := graph.EarthUnit(b.Lat, b.Lon)
		chord := chordUnitMeters(ax, ay, az, bx, by, bz)
		arc := graph.HaversineMeters(a.Lat, a.Lon, b.Lat, b.Lon)
		if chord > arc+1e-6 {
			t.Fatalf("chord exceeds arc: chord=%f arc=%f", chord, arc)
		}
	}
}

func TestACBSReturnsZeroGapCertificateAndPrunes(t *testing.T) {
	g := gridGraph(t, 96, 96, true)
	r, err := Run(context.Background(), g, 0, len(g.Nodes)-1, Aegis)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Stats.Reachable || !Validate(g, 0, len(g.Nodes)-1, r) {
		t.Fatalf("invalid ACBS result: %+v", r.Stats)
	}
	if r.Stats.UpperBound != r.Stats.Distance || r.Stats.LowerBound != r.Stats.Distance || r.Stats.OptimalityGap != 0 {
		t.Fatalf("invalid optimality certificate: %+v", r.Stats)
	}
	if r.Stats.PotentialModel != acbsChordPotentialModel || r.Stats.SchedulerVersion != acbsSchedulerVersion {
		t.Fatalf("missing algorithm metadata: %+v", r.Stats)
	}
	if r.Stats.PotentialEvaluations == 0 || r.Stats.UpperBoundUpdates == 0 {
		t.Fatalf("expected potential and incumbent activity: %+v", r.Stats)
	}
}

func TestACBSMatchesDijkstraOnRandomDirectedTimeGraphs(t *testing.T) {
	rnd := rand.New(rand.NewSource(1202))
	ctx := context.Background()
	for caseNo := 0; caseNo < 250; caseNo++ {
		n := 8 + rnd.Intn(70)
		g := graph.New("time-road", "", "car", graph.MetricTime)
		g.Nodes = make([]graph.Node, n)
		g.Adj = make([][]graph.Edge, n)
		for i := range g.Nodes {
			g.Nodes[i] = graph.Node{ID: int64(i + 1), Lat: 35 + rnd.Float64()*.25, Lon: 139 + rnd.Float64()*.25}
		}
		// A directed backbone guarantees reachability in one direction, while
		// random reverse and shortcut edges exercise asymmetric frontiers.
		for i := 0; i+1 < n; i++ {
			m := graph.HaversineMeters(g.Nodes[i].Lat, g.Nodes[i].Lon, g.Nodes[i+1].Lat, g.Nodes[i+1].Lon)
			cost := uint64(m*(40+rnd.Float64()*140)) + 1
			g.Adj[i] = append(g.Adj[i], graph.Edge{To: i + 1, Cost: cost})
			if rnd.Float64() < .7 {
				g.Adj[i+1] = append(g.Adj[i+1], graph.Edge{To: i, Cost: cost + uint64(rnd.Intn(2000))})
			}
		}
		for i := 0; i < n; i++ {
			for j := 0; j < n; j++ {
				if i == j || rnd.Float64() >= .035 {
					continue
				}
				m := graph.HaversineMeters(g.Nodes[i].Lat, g.Nodes[i].Lon, g.Nodes[j].Lat, g.Nodes[j].Lon)
				g.Adj[i] = append(g.Adj[i], graph.Edge{To: j, Cost: uint64(m*(40+rnd.Float64()*160)) + 1})
			}
		}
		if err := g.Finalize(); err != nil {
			t.Fatal(err)
		}
		for q := 0; q < 40; q++ {
			s, d := rnd.Intn(n), rnd.Intn(n)
			want, err := Run(ctx, g, s, d, Dijkstra)
			if err != nil {
				t.Fatal(err)
			}
			got, err := Run(ctx, g, s, d, Aegis)
			if err != nil {
				t.Fatal(err)
			}
			if got.Stats.Reachable != want.Stats.Reachable || got.Stats.Distance != want.Stats.Distance || (got.Stats.Reachable && !Validate(g, s, d, got)) {
				t.Fatalf("case=%d query=%d %d->%d want=%+v got=%+v", caseNo, q, s, d, want.Stats, got.Stats)
			}
			if got.Stats.Reachable && got.Stats.OptimalityGap != 0 {
				t.Fatalf("non-zero exactness gap: %+v", got.Stats)
			}
		}
	}
}

func TestACBSPrunesAfterEarlyIncumbent(t *testing.T) {
	g := graph.New("pruning", "", "car", graph.MetricDistance)
	g.Nodes = []graph.Node{
		{ID: 1, Lat: 35, Lon: 139},
		{ID: 2, Lat: 35, Lon: 139.001},
		{ID: 3, Lat: 35, Lon: 139.0004},
		{ID: 4, Lat: 35.01, Lon: 139.01},
		{ID: 5, Lat: 35.02, Lon: 139.02},
	}
	g.Adj = make([][]graph.Edge, len(g.Nodes))
	g.Adj[0] = []graph.Edge{
		{To: 1, Cost: 100_000}, // early feasible incumbent
		{To: 2, Cost: 20_000},  // better two-edge path
		{To: 3, Cost: 200_000}, // safely pruned after incumbent
		{To: 4, Cost: 250_000}, // safely pruned after incumbent
	}
	g.Adj[2] = []graph.Edge{{To: 1, Cost: 20_000}}
	if err := g.Finalize(); err != nil {
		t.Fatal(err)
	}
	want, _ := Run(context.Background(), g, 0, 1, Dijkstra)
	got, err := Run(context.Background(), g, 0, 1, AegisPrune)
	if err != nil {
		t.Fatal(err)
	}
	if got.Stats.Distance != want.Stats.Distance || !Validate(g, 0, 1, got) {
		t.Fatalf("want=%+v got=%+v", want.Stats, got.Stats)
	}
	if got.Stats.UpperBoundUpdates < 2 || got.Stats.BoundPruned == 0 {
		t.Fatalf("expected early incumbent refinement and pruning: %+v", got.Stats)
	}
	if got.Stats.BoundPruned != got.Stats.PrunedAtPop+got.Stats.PrunedAtRelax {
		t.Fatalf("pruning counters do not add up: %+v", got.Stats)
	}
	if got.Stats.QueuePops == 0 || got.Stats.QueuePushes == 0 || got.Stats.MeetingChecks == 0 {
		t.Fatalf("missing queue or meeting counters: %+v", got.Stats)
	}
	if got.Stats.ConnectionChecks < got.Stats.FiniteMeetings || got.Stats.FiniteMeetings != got.Stats.MeetingChecks || got.Stats.FiniteMeetings < got.Stats.UpperBoundUpdates {
		t.Fatalf("invalid connection accounting: %+v", got.Stats)
	}
}

func TestProductionACBSDisablesInactiveIncumbentPruning(t *testing.T) {
	g := gridGraph(t, 30, 30, true)
	production, err := Run(context.Background(), g, 0, len(g.Nodes)-1, Aegis)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := Run(context.Background(), g, 0, len(g.Nodes)-1, AegisNoPrune)
	if err != nil {
		t.Fatal(err)
	}
	if production.Stats.Distance != legacy.Stats.Distance || production.Stats.Relaxed != legacy.Stats.Relaxed || production.Stats.Expanded != legacy.Stats.Expanded {
		t.Fatalf("legacy no-prune alias diverged: production=%+v legacy=%+v", production.Stats, legacy.Stats)
	}
	if production.Stats.BoundEvaluations != 0 || production.Stats.BoundPruned != 0 || production.Stats.PrunedAtPop != 0 || production.Stats.PrunedAtRelax != 0 {
		t.Fatalf("production ACBS unexpectedly ran incumbent pruning: %+v", production.Stats)
	}
	if production.Stats.SchedulerVersion != acbsSchedulerVersion {
		t.Fatalf("unexpected production scheduler label: %q", production.Stats.SchedulerVersion)
	}
}

func TestACBSTraceRecordsSchedulerChunksWithoutChangingResult(t *testing.T) {
	g := gridGraph(t, 40, 40, true)
	var events []ACBSTraceEvent
	ctx := WithACBSTrace(context.Background(), func(event ACBSTraceEvent) {
		events = append(events, event)
	})
	got, err := Run(ctx, g, 0, len(g.Nodes)-1, Aegis)
	if err != nil {
		t.Fatal(err)
	}
	want, err := Run(context.Background(), g, 0, len(g.Nodes)-1, Dijkstra)
	if err != nil {
		t.Fatal(err)
	}
	if got.Stats.Distance != want.Stats.Distance || !Validate(g, 0, len(g.Nodes)-1, got) {
		t.Fatalf("trace changed result: got=%+v want=%+v", got.Stats, want.Stats)
	}
	if len(events) == 0 || uint64(len(events)) != got.Stats.Chunks {
		t.Fatalf("unexpected trace length %d for %d chunks", len(events), got.Stats.Chunks)
	}
	for i, event := range events {
		if event.Chunk != uint64(i+1) || (event.Direction != "F" && event.Direction != "B") || event.Budget <= 0 {
			t.Fatalf("invalid event %d: %+v", i, event)
		}
		if event.AfterLowerBound < event.BeforeLowerBound {
			t.Fatalf("lower bound regressed: %+v", event)
		}
	}
}

func TestACBSLateGuardActivatesOnlyForLateTimeSearches(t *testing.T) {
	ctx := context.Background()
	timeGraph := gridGraph(t, 180, 180, true)
	timeGraph.Metric = graph.MetricTime
	guarded, err := Run(ctx, timeGraph, 0, len(timeGraph.Nodes)-1, AegisLateGuard)
	if err != nil {
		t.Fatal(err)
	}
	want, err := Run(ctx, timeGraph, 0, len(timeGraph.Nodes)-1, Dijkstra)
	if err != nil {
		t.Fatal(err)
	}
	if guarded.Stats.Distance != want.Stats.Distance || !Validate(timeGraph, 0, len(timeGraph.Nodes)-1, guarded) {
		t.Fatalf("late guard changed exact result: want=%+v got=%+v", want.Stats, guarded.Stats)
	}
	if guarded.Stats.LateGuardActivations != 1 || guarded.Stats.LateGuardChunks == 0 || guarded.Stats.LateGuardFirstChunk < acbsLateGuardStartChunk+1 {
		t.Fatalf("expected one late guard activation, got %+v", guarded.Stats)
	}

	distanceGraph := gridGraph(t, 180, 180, true)
	plain, err := Run(ctx, distanceGraph, 0, len(distanceGraph.Nodes)-1, AegisLateGuard)
	if err != nil {
		t.Fatal(err)
	}
	if plain.Stats.LateGuardActivations != 0 || plain.Stats.LateGuardChunks != 0 {
		t.Fatalf("distance metric must not activate time-tail guard: %+v", plain.Stats)
	}

	shortTarget := 8*180 + 8
	short, err := Run(ctx, timeGraph, 0, shortTarget, AegisLateGuard)
	if err != nil {
		t.Fatal(err)
	}
	if short.Stats.LateGuardActivations != 0 {
		t.Fatalf("short time route must not activate late guard: %+v", short.Stats)
	}
}

func TestACBSConnectionGuardCandidatesAreExactAndScoped(t *testing.T) {
	ctx := context.Background()
	timeGraph := gridGraph(t, 180, 180, true)
	timeGraph.Metric = graph.MetricTime
	want, err := Run(ctx, timeGraph, 0, len(timeGraph.Nodes)-1, Dijkstra)
	if err != nil {
		t.Fatal(err)
	}
	for _, alg := range []Algorithm{AegisConnect32, AegisConnect40, AegisConnect32x16} {
		got, err := Run(ctx, timeGraph, 0, len(timeGraph.Nodes)-1, alg)
		if err != nil {
			t.Fatalf("%s: %v", alg, err)
		}
		if got.Stats.Distance != want.Stats.Distance || !Validate(timeGraph, 0, len(timeGraph.Nodes)-1, got) {
			t.Fatalf("%s changed exact result: want=%+v got=%+v", alg, want.Stats, got.Stats)
		}
		if got.Stats.ConnectionGuardActivations != 1 || got.Stats.ConnectionGuardChunks == 0 || got.Stats.ConnectionGuardMode == "" {
			t.Fatalf("%s did not activate connection guard: %+v", alg, got.Stats)
		}
	}

	distanceGraph := gridGraph(t, 180, 180, true)
	for _, alg := range []Algorithm{AegisConnect32, AegisConnect40, AegisConnect32x16} {
		got, err := Run(ctx, distanceGraph, 0, len(distanceGraph.Nodes)-1, alg)
		if err != nil {
			t.Fatalf("%s: %v", alg, err)
		}
		if got.Stats.ConnectionGuardActivations != 0 || got.Stats.ConnectionGuardChunks != 0 {
			t.Fatalf("%s activated on distance metric: %+v", alg, got.Stats)
		}
	}
}

func TestConnectionGuardThresholdsAndWindows(t *testing.T) {
	if shouldEngageConnectionGuard(acbsGuardConnect32UntilUpper, 31, 31, 100, 100, true, true) {
		t.Fatal("connect-32 activated before threshold")
	}
	if !shouldEngageConnectionGuard(acbsGuardConnect32UntilUpper, 32, 20, 120, 100, true, true) {
		t.Fatal("connect-32 did not activate")
	}
	if shouldEngageConnectionGuard(acbsGuardConnect40UntilUpper, 39, 30, 120, 100, true, true) {
		t.Fatal("connect-40 activated before threshold")
	}
	if !shouldEngageConnectionGuard(acbsGuardConnect40UntilUpper, 40, 30, 120, 100, true, true) {
		t.Fatal("connect-40 did not activate")
	}
	if connectionGuardMaxChunks(acbsGuardConnect32UntilUpper) != 0 || connectionGuardMaxChunks(acbsGuardConnect40UntilUpper) != 0 {
		t.Fatal("until-upper candidates must be unbounded until first upper bound")
	}
	if connectionGuardMaxChunks(acbsGuardConnect32x16) != 16 {
		t.Fatal("connect-32x16 must use a 16 chunk window")
	}
}

func TestLateUpperBoundGuardRequiresOscillationAndAmbiguousScores(t *testing.T) {
	if shouldEngageLateUpperBoundGuard(47, 47, 100, 100, true, true) {
		t.Fatal("guard activated before threshold")
	}
	if shouldEngageLateUpperBoundGuard(48, 10, 100, 100, true, true) {
		t.Fatal("guard activated without enough direction oscillation")
	}
	if shouldEngageLateUpperBoundGuard(48, 30, 200, 100, true, true) {
		t.Fatal("guard activated with a clear efficiency winner")
	}
	if !shouldEngageLateUpperBoundGuard(48, 30, 120, 100, true, true) {
		t.Fatal("guard did not activate for a late ambiguous oscillating search")
	}
}

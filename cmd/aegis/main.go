package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime/pprof"
	"strconv"
	"strings"
	"time"

	"github.com/lasder-ca/aegis-acbs/internal/bench"
	"github.com/lasder-ca/aegis-acbs/internal/graph"
	"github.com/lasder-ca/aegis-acbs/internal/i18n"
	"github.com/lasder-ca/aegis-acbs/internal/search"
	"github.com/lasder-ca/aegis-acbs/internal/server"
	"github.com/lasder-ca/aegis-acbs/internal/version"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "import-osm":
		err = importOSM(os.Args[2:])
	case "import-dimacs":
		err = importDIMACS(os.Args[2:])
	case "route":
		err = route(os.Args[2:])
	case "benchmark":
		err = benchmark(os.Args[2:])
	case "aggregate":
		err = aggregate(os.Args[2:])
	case "stress":
		err = stress(os.Args[2:])
	case "diagnose":
		err = diagnose(os.Args[2:])
	case "validate-regret":
		err = validateRegret(os.Args[2:])
	case "replay-regret":
		err = replayRegret(os.Args[2:])
	case "profile-trigger":
		err = profileTrigger(os.Args[2:])
	case "serve":
		err = serve(os.Args[2:])
	case "inspect":
		err = inspect(os.Args[2:])
	case "version", "--version", "-v":
		fmt.Printf("%s v%s\n", version.Name, version.Version)
	case "help", "--help", "-h":
		usage()
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Printf(`%s v%s

Usage:
  aegis import-osm --input city.osm --output city.aegis [--profile car|bike|walk] [--metric distance|time]
  aegis import-dimacs --graph USA-road-d.NY.gr --coords USA-road-d.NY.co --output ny.aegis
  aegis route --graph city.aegis --source ID|lat,lon --target ID|lat,lon [--algorithm aegis]
  aegis benchmark --graph city.aegis --queries 100 --repeats 9 --output report.json --html report.html
  aegis aggregate --input-dir artifacts/benchmarks --output matrix.json --csv matrix.csv --html matrix.html
  aegis stress --graph city.aegis --queries 10000 --workers 8 --verify-every 100
  aegis diagnose --input benchmark.json --output regret.json --csv regret.csv --html regret.html
  aegis validate-regret --input-dir validation --min-queries 10000 --output validation.json --csv validation.csv --html validation.html
  aegis replay-regret --graph city.aegis --validation validation.json --input-root validation --output replay.json --csv replay.csv --html replay.html
  aegis profile-trigger --graph city.aegis --validation validation.json --replay replay.json --input-root validation --output trigger-profile.json
  aegis serve --graph city.aegis --listen 127.0.0.1:8787
  aegis inspect --graph city.aegis
`, version.Name, version.Version)
}

func importOSM(args []string) error {
	fs := flag.NewFlagSet("import-osm", flag.ContinueOnError)
	in := fs.String("input", "", "OSM XML input")
	out := fs.String("output", "", "Aegis graph output")
	name := fs.String("name", "", "graph name")
	profile := fs.String("profile", "car", "car, bike, or walk")
	metric := fs.String("metric", "distance", "distance or time")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *in == "" || *out == "" {
		return errors.New("--input and --output are required")
	}
	if *profile != "car" && *profile != "bike" && *profile != "walk" {
		return errors.New("invalid profile")
	}
	m := graph.Metric(*metric)
	if m != graph.MetricDistance && m != graph.MetricTime {
		return errors.New("invalid metric")
	}
	g, err := graph.ImportOSMXML(*in, graph.OSMImportOptions{Name: *name, Profile: *profile, Metric: m})
	if err != nil {
		return err
	}
	if err := graph.Save(*out, g); err != nil {
		return err
	}
	fmt.Printf("created %s: %d nodes, %d edges\n", *out, len(g.Nodes), g.EdgeCount)
	return nil
}

func importDIMACS(args []string) error {
	fs := flag.NewFlagSet("import-dimacs", flag.ContinueOnError)
	in := fs.String("graph", "", "DIMACS .gr input")
	coords := fs.String("coords", "", "DIMACS .co input")
	out := fs.String("output", "", "Aegis graph output")
	name := fs.String("name", "", "graph name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *in == "" || *out == "" {
		return errors.New("--graph and --output are required")
	}
	g, err := graph.ImportDIMACS(*in, *coords, *name)
	if err != nil {
		return err
	}
	if err := graph.Save(*out, g); err != nil {
		return err
	}
	fmt.Printf("created %s: %d nodes, %d edges\n", *out, len(g.Nodes), g.EdgeCount)
	return nil
}

func route(args []string) error {
	fs := flag.NewFlagSet("route", flag.ContinueOnError)
	path := fs.String("graph", "", "Aegis graph")
	src := fs.String("source", "", "node ID or lat,lon")
	dst := fs.String("target", "", "node ID or lat,lon")
	alg := fs.String("algorithm", "aegis", "algorithm")
	langS := fs.String("lang", "en", "ja, en, zh-CN, ko, fr")
	timeout := fs.Duration("timeout", 30*time.Second, "query timeout")
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *path == "" || *src == "" || *dst == "" {
		return errors.New("--graph, --source, and --target are required")
	}
	g, err := graph.Load(*path)
	if err != nil {
		return err
	}
	s, err := resolve(g, *src)
	if err != nil {
		return err
	}
	t, err := resolve(g, *dst)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	r, err := search.Run(ctx, g, s, t, search.Algorithm(*alg))
	if err != nil {
		return err
	}
	if *jsonOut {
		return json.NewEncoder(os.Stdout).Encode(routeJSON(g, r))
	}
	lang := i18n.Normalize(*langS)
	fmt.Printf("%s: %v\n", i18n.T(lang, "reachable"), r.Stats.Reachable)
	fmt.Printf("%s: %d\n", i18n.T(lang, "distance"), r.Stats.Distance)
	fmt.Printf("%s: %d\n", i18n.T(lang, "expanded"), r.Stats.Expanded)
	fmt.Printf("%s: %d\n", i18n.T(lang, "relaxed"), r.Stats.Relaxed)
	fmt.Printf("%s: %.3f ms\n", i18n.T(lang, "time"), float64(r.Stats.DurationNS)/1e6)
	return nil
}

func routeJSON(g *graph.Graph, r search.Result) map[string]any {
	ids := make([]int64, len(r.Path))
	coords := make([][2]float64, len(r.Path))
	for i, v := range r.Path {
		ids[i] = g.Nodes[v].ID
		coords[i] = [2]float64{g.Nodes[v].Lat, g.Nodes[v].Lon}
	}
	return map[string]any{"stats": r.Stats, "pathNodeIds": ids, "coordinates": coords}
}

func benchmark(args []string) error {
	fs := flag.NewFlagSet("benchmark", flag.ContinueOnError)
	path := fs.String("graph", "", "Aegis graph")
	queries := fs.Int("queries", 100, "query count")
	seed := fs.Uint64("seed", 1010, "deterministic seed")
	out := fs.String("output", "benchmark.json", "JSON report")
	htmlOut := fs.String("html", "benchmark.html", "self-contained visual HTML report; empty disables")
	repeats := fs.Int("repeats", 9, "odd repeated measurements per query and algorithm")
	batchSize := fs.Int("batch", 0, "executions per measurement; 0 selects by graph size")
	order := fs.String("order", "interleaved", "measurement order: interleaved or rotated")
	measureMemory := fs.Bool("measure-memory", false, "run an untimed allocation pass per query and algorithm")
	algs := fs.String("algorithms", "", "comma-separated algorithms; default chooses valid exact algorithms")
	research := fs.Bool("research", false, "include the ACBS static-scheduler ablation")
	experimental := fs.Bool("experimental", false, "include incumbent-pruning and projection-potential experiments")
	timeout := fs.Duration("timeout", 30*time.Second, "per-query timeout")
	suite := fs.String("suite", "mixed", "mixed, local, regional, or random")
	pairMode := fs.String("pair-mode", "strongly-connected", "strongly-connected or all")
	cpu := fs.String("cpuprofile", "", "write CPU profile")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *path == "" {
		return errors.New("--graph is required")
	}
	if *cpu != "" {
		f, err := os.Create(*cpu)
		if err != nil {
			return err
		}
		defer f.Close()
		if err := pprof.StartCPUProfile(f); err != nil {
			return err
		}
		defer pprof.StopCPUProfile()
	}
	g, err := graph.Load(*path)
	if err != nil {
		return err
	}
	list := []search.Algorithm{}
	for _, s := range strings.Split(*algs, ",") {
		if s = strings.TrimSpace(s); s != "" {
			list = append(list, search.Algorithm(s))
		}
	}
	if (*research || *experimental) && len(list) == 0 {
		list = []search.Algorithm{search.Dijkstra, search.BiDijkstra}
		if g.MinCostPerMeter > 0 {
			list = append(list, search.AStar)
		}
		list = append(list, search.AegisStatic, search.AegisEntropic)
		if *experimental {
			list = append(list, search.AegisLateGuard, search.AegisConnect32, search.AegisConnect40, search.AegisConnect32x16, search.AegisPrune, search.AegisProjection)
		}
		list = append(list, search.Aegis)
	}
	report, err := bench.Run(context.Background(), g, bench.Config{Queries: *queries, Seed: *seed, Algorithms: list, Warmup: 3, Repeats: *repeats, BatchSize: *batchSize, Order: *order, MeasureMemory: *measureMemory, Timeout: *timeout, Suite: *suite, PairMode: *pairMode})
	if err != nil {
		return err
	}
	if err := bench.WriteJSON(*out, report); err != nil {
		return err
	}
	if *htmlOut != "" {
		if err := bench.WriteHTML(*htmlOut, report); err != nil {
			return err
		}
	}
	for _, s := range report.Summary {
		fmt.Printf("%-14s mean=%8.3fms median=%8.3fms best=%8.3fms worst=%8.3fms p95=%8.3fms p99=%8.3fms relaxed=%d expanded=%d alloc=%dB correct=%d/%d\n",
			s.Algorithm, float64(s.MeanNS)/1e6, float64(s.MedianNS)/1e6, float64(s.MinNS)/1e6, float64(s.MaxNS)/1e6, float64(s.P95NS)/1e6, float64(s.P99NS)/1e6,
			s.MedianEdges, s.MedianExpanded, s.MedianAllocBytes, s.Correct, s.Runs)
	}
	if report.Aegis.Comparisons > 0 {
		fmt.Printf("acbs           ratio-of-medians-vs-dijkstra=%.3fx geomean-speedup=%.3fx runtime-vs-fastest-classical(p50/p95)=%.3fx/%.3fx classical-oracle-regret(p50/p95)=%.3fx/%.3fx meaningful=%d penalty(p50/p95/max)=%.3fms/%.3fms/%.3fms\n",
			report.Aegis.RatioOfMediansVsDijkstra, report.Aegis.GeomeanPerQuerySpeedupVsDijkstra,
			report.Aegis.MedianRuntimeVsFastestClassical, report.Aegis.P95RuntimeVsFastestClassical,
			report.Aegis.MedianClassicalOracleRegret, report.Aegis.P95ClassicalOracleRegret, report.Aegis.MeaningfulSlowdowns,
			float64(report.Aegis.MedianAbsolutePenaltyNS)/1e6, float64(report.Aegis.P95AbsolutePenaltyNS)/1e6, float64(report.Aegis.MaxAbsolutePenaltyNS)/1e6)
		fmt.Printf("acbs-work      median-forward-share=%.1f%% median-switches=%d median-chunks=%d median-pushes=%d median-pops=%d median-stale=%d median-pruned(pop/relax)=%d/%d median-connections=%d median-finite-meetings=%d median-upper-updates=%d\n",
			100*report.Aegis.MedianForwardShare, report.Aegis.MedianDirectionSwitches, report.Aegis.MedianChunks,
			report.Aegis.MedianQueuePushes, report.Aegis.MedianQueuePops, report.Aegis.MedianStalePops,
			report.Aegis.MedianPrunedAtPop, report.Aegis.MedianPrunedAtRelax, report.Aegis.MedianConnectionChecks,
			report.Aegis.MedianFiniteMeetings, report.Aegis.MedianUpperBoundUpdates)
	}
	fmt.Printf("memory         peak-rss=%.2fMiB go-heap=%.2fMiB go-heap-sys=%.2fMiB total-alloc=%.2fMiB gc=%d\n",
		float64(report.Memory.PeakRSSBytes)/(1024*1024), float64(report.Memory.GoHeapAllocBytes)/(1024*1024),
		float64(report.Memory.GoHeapSysBytes)/(1024*1024), float64(report.Memory.GoTotalAllocBytes)/(1024*1024), report.Memory.GoNumGC)
	if !report.AllCorrect {
		return errors.New("correctness mismatch detected")
	}
	fmt.Println("report:", *out)
	if *htmlOut != "" {
		fmt.Println("visual report:", *htmlOut)
	}
	return nil
}

func stress(args []string) error {
	fs := flag.NewFlagSet("stress", flag.ContinueOnError)
	path := fs.String("graph", "", "Aegis graph")
	queries := fs.Int("queries", 10_000, "total concurrent query count")
	workers := fs.Int("workers", 0, "concurrent workers; 0 uses GOMAXPROCS")
	seed := fs.Uint64("seed", 7070, "deterministic seed")
	alg := fs.String("algorithm", "aegis", "algorithm under stress")
	verifyEvery := fs.Int("verify-every", 100, "verify every Nth query against Dijkstra; 0 disables")
	timeout := fs.Duration("timeout", 30*time.Second, "per-query timeout")
	suite := fs.String("suite", "mixed", "mixed, local, regional, or random")
	pairMode := fs.String("pair-mode", "strongly-connected", "strongly-connected or all")
	out := fs.String("output", "stress.json", "JSON report")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *path == "" {
		return errors.New("--graph is required")
	}
	g, err := graph.Load(*path)
	if err != nil {
		return err
	}
	report, err := bench.RunStress(context.Background(), g, bench.StressConfig{
		Queries: *queries, Workers: *workers, Seed: *seed, Algorithm: search.Algorithm(*alg),
		VerifyEvery: *verifyEvery, Timeout: *timeout, Suite: *suite, PairMode: *pairMode,
	})
	if writeErr := bench.WriteStressJSON(*out, report); writeErr != nil {
		return writeErr
	}
	fmt.Printf("stress         algorithm=%s workers=%d completed=%d verified=%d correct=%d errors=%d throughput=%.2f qps\n",
		report.Config.Algorithm, report.Config.Workers, report.Completed, report.Verified, report.Correct, report.Errors, report.ThroughputQPS)
	fmt.Printf("latency        mean=%.3fms median=%.3fms p95=%.3fms p99=%.3fms worst=%.3fms\n",
		float64(report.MeanNS)/1e6, float64(report.MedianNS)/1e6, float64(report.P95NS)/1e6, float64(report.P99NS)/1e6, float64(report.MaxNS)/1e6)
	fmt.Printf("memory         peak-rss=%.2fMiB go-heap=%.2fMiB total-alloc=%.2fMiB gc=%d\n",
		float64(report.Memory.PeakRSSBytes)/(1024*1024), float64(report.Memory.GoHeapAllocBytes)/(1024*1024),
		float64(report.Memory.GoTotalAllocBytes)/(1024*1024), report.Memory.GoNumGC)
	fmt.Println("report:", *out)
	if err != nil {
		return err
	}
	if !report.AllVerifiedCorrect {
		return errors.New("stress verification mismatch or query error detected")
	}
	return nil
}

func diagnose(args []string) error {
	fs := flag.NewFlagSet("diagnose", flag.ContinueOnError)
	input := fs.String("input", "", "benchmark JSON report")
	algorithm := fs.String("algorithm", "aegis", "algorithm to diagnose")
	ratio := fs.Float64("ratio-threshold", 1.25, "minimum runtime ratio for a meaningful slowdown")
	penalty := fs.Duration("penalty-floor", time.Millisecond, "minimum absolute slowdown for a meaningful slowdown")
	top := fs.Int("top", 25, "number of top queries to include")
	output := fs.String("output", "regret.json", "diagnostic JSON output")
	csvOut := fs.String("csv", "regret.csv", "diagnostic CSV output; empty disables")
	htmlOut := fs.String("html", "regret.html", "self-contained diagnostic HTML output; empty disables")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *input == "" {
		return errors.New("--input is required")
	}
	report, err := bench.LoadReport(*input)
	if err != nil {
		return err
	}
	diagnostic, err := bench.AnalyzeRegret(report, bench.RegretConfig{
		Algorithm: search.Algorithm(*algorithm), RatioThreshold: *ratio, PenaltyFloorNS: penalty.Nanoseconds(), Top: *top,
	})
	if err != nil {
		return err
	}
	if err := bench.WriteRegretJSON(*output, diagnostic); err != nil {
		return err
	}
	if *csvOut != "" {
		if err := bench.WriteRegretCSV(*csvOut, diagnostic); err != nil {
			return err
		}
	}
	if *htmlOut != "" {
		if err := bench.WriteRegretHTML(*htmlOut, diagnostic); err != nil {
			return err
		}
	}
	fmt.Printf("diagnosis      algorithm=%s queries=%d meaningful=%d ratio(p50/p95/max)=%.3fx/%.3fx/%.3fx penalty(p50/p95/max)=%.3fms/%.3fms/%.3fms\n",
		diagnostic.Config.Algorithm, diagnostic.Queries, diagnostic.MeaningfulQueries,
		diagnostic.P50RuntimeRatio, diagnostic.P95RuntimeRatio, diagnostic.MaxRuntimeRatio,
		float64(diagnostic.P50PenaltyNS)/1e6, float64(diagnostic.P95PenaltyNS)/1e6, float64(diagnostic.MaxPenaltyNS)/1e6)
	for _, row := range diagnostic.TopByPenalty {
		if !row.Meaningful {
			continue
		}
		fmt.Printf("regret         query=%d class=%s baseline=%s ratio=%.3fx penalty=%.3fms distance=%.2fkm expanded=%d forward=%.1f%% switches=%d/%d\n",
			row.QueryIndex, row.Class, row.FastestClassical, row.RuntimeRatio, float64(row.AbsolutePenaltyNS)/1e6,
			row.StraightLineMeters/1000, row.Expanded, 100*row.ForwardShare, row.DirectionSwitches, row.Chunks)
	}
	fmt.Println("diagnostic report:", *output)
	if *csvOut != "" {
		fmt.Println("diagnostic csv:", *csvOut)
	}
	if *htmlOut != "" {
		fmt.Println("diagnostic visual report:", *htmlOut)
	}
	return nil
}

func validateRegret(args []string) error {
	fs := flag.NewFlagSet("validate-regret", flag.ContinueOnError)
	inputDir := fs.String("input-dir", "", "directory containing benchmark JSON reports")
	algorithm := fs.String("algorithm", "aegis", "algorithm to validate")
	ratio := fs.Float64("ratio-threshold", 1.25, "minimum runtime ratio for a meaningful slowdown")
	penalty := fs.Duration("penalty-floor", time.Millisecond, "minimum absolute slowdown for a meaningful slowdown")
	top := fs.Int("top", 50, "number of meaningful slowdowns to retain")
	minimumQueries := fs.Int("min-queries", 10000, "minimum total validated queries")
	maximumMeaningfulRate := fs.Float64("max-meaningful-rate", 0, "maximum allowed meaningful slowdown rate from 0 to 1")
	output := fs.String("output", "regret-validation.json", "validation JSON output")
	csvOut := fs.String("csv", "regret-validation.csv", "per-run CSV output; empty disables")
	htmlOut := fs.String("html", "regret-validation.html", "self-contained validation HTML output; empty disables")
	failOnViolation := fs.Bool("fail-on-violation", true, "return a non-zero status when validation fails")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *inputDir == "" {
		return errors.New("--input-dir is required")
	}
	report, err := bench.AggregateRegretDirectory(*inputDir, bench.RegretValidationConfig{Algorithm: search.Algorithm(*algorithm), RatioThreshold: *ratio, PenaltyFloorNS: penalty.Nanoseconds(), Top: *top, MinimumQueries: *minimumQueries, MaximumMeaningfulRate: *maximumMeaningfulRate})
	if err != nil {
		return err
	}
	if err := bench.WriteRegretValidationJSON(*output, report); err != nil {
		return err
	}
	if *csvOut != "" {
		if err := bench.WriteRegretValidationCSV(*csvOut, report); err != nil {
			return err
		}
	}
	if *htmlOut != "" {
		if err := bench.WriteRegretValidationHTML(*htmlOut, report); err != nil {
			return err
		}
	}
	fmt.Printf("validation     runs=%d queries=%d meaningful=%d rate=%.6f%% wilson95=%.6f%%..%.6f%% zero-event-upper95=%.6f%% correct=%v enough=%v pass=%v\n", report.Files, report.TotalQueries, report.TotalMeaningful, 100*report.MeaningfulRate, 100*report.MeaningfulRateWilsonLow, 100*report.MeaningfulRateWilsonHigh, 100*report.ZeroEventUpper95, report.AllCorrect, report.EnoughQueries, report.Passed)
	fmt.Printf("tail           ratio(p50/p95/max)=%.3fx/%.3fx/%.3fx penalty(p50/p95/max)=%.3fms/%.3fms/%.3fms\n", report.P50RuntimeRatio, report.P95RuntimeRatio, report.MaxRuntimeRatio, float64(report.P50PenaltyNS)/1e6, float64(report.P95PenaltyNS)/1e6, float64(report.MaxPenaltyNS)/1e6)
	for _, run := range report.Runs {
		fmt.Printf("run            metric=%-8s seed=%-10d queries=%-6d meaningful=%-4d p95-ratio=%.3fx max-penalty=%.3fms correct=%v path=%s\n", run.Metric, run.Seed, run.Queries, run.Meaningful, run.P95RuntimeRatio, float64(run.MaxPenaltyNS)/1e6, run.AllCorrect, run.Path)
	}
	fmt.Println("validation report:", *output)
	if *csvOut != "" {
		fmt.Println("validation csv:", *csvOut)
	}
	if *htmlOut != "" {
		fmt.Println("validation visual report:", *htmlOut)
	}
	if *failOnViolation && !report.Passed {
		return errors.New("regret validation did not meet the configured acceptance criteria")
	}
	return nil
}

func replayRegret(args []string) error {
	fs := flag.NewFlagSet("replay-regret", flag.ContinueOnError)
	graphPath := fs.String("graph", "", "Aegis graph used by the source benchmark reports")
	validationPath := fs.String("validation", "", "regret-validation JSON report")
	inputRoot := fs.String("input-root", "", "root for source report paths; defaults to validation directory")
	runs := fs.Int("runs", 31, "timed replays per algorithm and case")
	warmup := fs.Int("warmup", 5, "warmup replays per algorithm and case")
	timeout := fs.Duration("timeout", 30*time.Second, "per-search timeout")
	ratio := fs.Float64("ratio-threshold", 1.25, "minimum replay runtime ratio for a meaningful slowdown")
	penalty := fs.Duration("penalty-floor", time.Millisecond, "minimum replay absolute slowdown")
	top := fs.Int("top", 50, "maximum validation outliers to replay")
	output := fs.String("output", "regret-replay.json", "replay JSON output")
	csvOut := fs.String("csv", "regret-replay.csv", "replay CSV output; empty disables")
	htmlOut := fs.String("html", "regret-replay.html", "self-contained replay HTML output; empty disables")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *graphPath == "" || *validationPath == "" {
		return errors.New("--graph and --validation are required")
	}
	g, err := graph.Load(*graphPath)
	if err != nil {
		return err
	}
	report, err := bench.ReplayRegret(context.Background(), g, *validationPath, *inputRoot, bench.RegretReplayConfig{
		Runs: *runs, Warmup: *warmup, Timeout: *timeout, RatioThreshold: *ratio, PenaltyFloorNS: penalty.Nanoseconds(), Top: *top,
	})
	if err != nil {
		return err
	}
	bench.SortRegretReplayCases(&report)
	if err := bench.WriteRegretReplayJSON(*output, report); err != nil {
		return err
	}
	if *csvOut != "" {
		if err := bench.WriteRegretReplayCSV(*csvOut, report); err != nil {
			return err
		}
	}
	if *htmlOut != "" {
		if err := bench.WriteRegretReplayHTML(*htmlOut, report); err != nil {
			return err
		}
	}
	fmt.Printf("replay         requested=%d replayed=%d reproduced=%d scheduler-tail=%d persistent=%d not-reproduced=%d legacy-guard(improved/neutral/regressed)=%d/%d/%d legacy-pass=%v correct=%v\n",
		report.RequestedCases, report.ReplayedCases, report.ReproducedMeaningful, report.AdaptiveSchedulerTail, report.PersistentClassical, report.NotReproduced, report.LateGuardImproved, report.LateGuardNeutral, report.LateGuardRegressed, report.LateGuardPass, report.AllCorrect)
	for _, alg := range []search.Algorithm{search.AegisConnect32, search.AegisConnect40, search.AegisConnect32x16} {
		c := report.GuardOutcomes[string(alg)]
		fmt.Printf("guard          algorithm=%s improved=%d neutral=%d regressed=%d scheduler-tails=%d scheduler-pass=%v max-regression=%.3fms\n",
			alg, c.Improved, c.Neutral, c.Regressed, c.SchedulerTails, c.SchedulerPass, float64(c.MaxRegressionNS)/1e6)
	}
	for _, c := range report.Cases {
		guards := ""
		for _, g := range c.Guards {
			if guards != "" {
				guards += " "
			}
			guards += fmt.Sprintf("%s=%.3fms(%s)", g.Algorithm, float64(g.MedianNS)/1e6, g.Outcome)
		}
		fmt.Printf("case           run=%s query=%d class=%s original=%.3fx/%.3fms replay=%s %.3fx/%.3fms static=%.3fms legacy=%.3fms(%s) guards=[%s] classification=%s chunks=%d upper-chunk=%d\n",
			c.SourceReport, c.QueryIndex, c.Class, c.OriginalRatio, float64(c.OriginalPenaltyNS)/1e6, c.FastestClassical, c.AegisRatio, float64(c.AegisPenaltyNS)/1e6, float64(c.StaticNS)/1e6, float64(c.LateGuardNS)/1e6, c.LateGuardOutcome, guards, c.Classification, len(c.Trace), c.TraceUpperBoundChunk)
	}
	fmt.Println("replay report:", *output)
	if *csvOut != "" {
		fmt.Println("replay csv:", *csvOut)
	}
	if *htmlOut != "" {
		fmt.Println("replay visual report:", *htmlOut)
	}
	if !report.AllCorrect {
		return errors.New("one or more replayed algorithms returned an incorrect path")
	}
	return nil
}

func profileTrigger(args []string) error {
	fs := flag.NewFlagSet("profile-trigger", flag.ContinueOnError)
	graphPath := fs.String("graph", "", "Aegis graph used by the validation benchmark reports")
	validationPath := fs.String("validation", "", "regret-validation JSON report")
	replayPath := fs.String("replay", "", "isolated regret-replay JSON with confirmed labels")
	inputRoot := fs.String("input-root", "", "root for source report paths; defaults to validation directory")
	checkpointsText := fs.String("checkpoints", "24,32,40,48", "comma-separated scheduler chunk checkpoints")
	timeout := fs.Duration("timeout", 30*time.Second, "per-search timeout")
	maxMatches := fs.Int("max-matches", 5, "maximum whole-suite matches for an eligible trigger")
	topRules := fs.Int("top-rules", 20, "number of ranked rule candidates to retain")
	labelRepeats := fs.Int("label-repeats", 3, "deterministic trace repetitions for replay-labelled tails")
	output := fs.String("output", "trigger-profile.json", "profile JSON output")
	csvOut := fs.String("csv", "trigger-profile.csv", "profile CSV output; empty disables")
	htmlOut := fs.String("html", "trigger-profile.html", "self-contained profile HTML output; empty disables")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *graphPath == "" || *validationPath == "" || *replayPath == "" {
		return errors.New("--graph, --validation, and --replay are required")
	}
	var checkpoints []uint64
	for _, part := range strings.Split(*checkpointsText, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		v, err := strconv.ParseUint(part, 10, 64)
		if err != nil || v == 0 {
			return fmt.Errorf("invalid checkpoint %q", part)
		}
		checkpoints = append(checkpoints, v)
	}
	if len(checkpoints) == 0 {
		return errors.New("at least one checkpoint is required")
	}
	g, err := graph.Load(*graphPath)
	if err != nil {
		return err
	}
	report, err := bench.ProfileSchedulerTriggers(context.Background(), g, *validationPath, *replayPath, *inputRoot, bench.TriggerProfileConfig{
		Checkpoints: checkpoints, Timeout: *timeout, MaxMatches: *maxMatches, TopRules: *topRules, LabelRepeats: *labelRepeats,
	})
	if err != nil {
		return err
	}
	if err := bench.WriteTriggerProfileJSON(*output, report); err != nil {
		return err
	}
	if *csvOut != "" {
		if err := bench.WriteTriggerProfileCSV(*csvOut, report); err != nil {
			return err
		}
	}
	if *htmlOut != "" {
		if err := bench.WriteTriggerProfileHTML(*htmlOut, report); err != nil {
			return err
		}
	}
	fmt.Printf("trigger-profile queries=%d scheduler-tails=%d persistent=%d errors=%d unstable-labels=%d selected=%v correct=%v\n", report.Queries, report.SchedulerTails, report.PersistentTails, report.TraceErrors, report.UnstableLabels, report.SelectedRule != nil, report.AllCorrect)
	for i, rule := range report.Rules {
		if i >= 10 {
			break
		}
		fmt.Printf("rule           checkpoint=%d eligible=%v matches=%d positives=%d false-positive=%d precision=%.3f recall=%.3f conditions=", rule.Checkpoint, rule.Eligible, rule.Matches, rule.PositiveMatches, rule.FalsePositives, rule.Precision, rule.Recall)
		for j, condition := range rule.Conditions {
			if j > 0 {
				fmt.Print(" && ")
			}
			fmt.Printf("%s%s%.6g", condition.Feature, condition.Operator, condition.Threshold)
		}
		fmt.Println()
	}
	if report.SelectedRule != nil {
		fmt.Printf("SELECTED       checkpoint=%d matches=%d false-positive=%d\n", report.SelectedRule.Checkpoint, report.SelectedRule.Matches, report.SelectedRule.FalsePositives)
	} else {
		fmt.Println("SELECTED       none")
	}
	fmt.Println("trigger profile:", *output)
	if *csvOut != "" {
		fmt.Println("trigger csv:", *csvOut)
	}
	if *htmlOut != "" {
		fmt.Println("trigger visual report:", *htmlOut)
	}
	if !report.AllCorrect {
		return errors.New("one or more profiled queries returned an incorrect path")
	}
	return nil
}

func aggregate(args []string) error {
	fs := flag.NewFlagSet("aggregate", flag.ContinueOnError)
	inputDir := fs.String("input-dir", "", "directory containing benchmark JSON reports")
	output := fs.String("output", "benchmark-matrix.json", "aggregate JSON output")
	csvOut := fs.String("csv", "benchmark-matrix.csv", "aggregate CSV output; empty disables")
	htmlOut := fs.String("html", "benchmark-matrix.html", "self-contained aggregate HTML output; empty disables")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *inputDir == "" {
		return errors.New("--input-dir is required")
	}
	report, err := bench.AggregateDirectory(*inputDir)
	if err != nil {
		return err
	}
	if err := bench.WriteMatrixJSON(*output, report); err != nil {
		return err
	}
	if *csvOut != "" {
		if err := bench.WriteMatrixCSV(*csvOut, report); err != nil {
			return err
		}
	}
	if *htmlOut != "" {
		if err := bench.WriteMatrixHTML(*htmlOut, report); err != nil {
			return err
		}
	}
	for _, group := range report.Groups {
		fmt.Printf("%-20s %-8s runs=%d p50=%8.3fms median-p95=%8.3fms speedup=%.3fx worst-p95-regret=%.3fx correct=%v\n",
			group.GraphName, group.Metric, group.Runs, float64(group.MedianOfAegisMediansNS)/1e6,
			float64(group.MedianOfAegisP95NS)/1e6, group.GeomeanPerQuerySpeedupVsDijkstra,
			group.WorstP95OracleRegret, group.AllCorrect)
	}
	fmt.Println("matrix report:", *output)
	if *csvOut != "" {
		fmt.Println("matrix csv:", *csvOut)
	}
	if *htmlOut != "" {
		fmt.Println("matrix visual report:", *htmlOut)
	}
	if !report.AllCorrect {
		return errors.New("one or more benchmark reports contain correctness mismatches")
	}
	return nil
}

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	path := fs.String("graph", "", "Aegis graph")
	listen := fs.String("listen", "127.0.0.1:8787", "listen address")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *path == "" {
		return errors.New("--graph is required")
	}
	g, err := graph.Load(*path)
	if err != nil {
		return err
	}
	srv := &http.Server{Addr: *listen, Handler: server.App{Graph: g}.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 5 * time.Minute, IdleTimeout: 60 * time.Second}
	log.Printf("Aegis ACBS v%s: http://%s", version.Version, *listen)
	return srv.ListenAndServe()
}

func inspect(args []string) error {
	fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
	path := fs.String("graph", "", "Aegis graph")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *path == "" {
		return errors.New("--graph is required")
	}
	g, err := graph.Load(*path)
	if err != nil {
		return err
	}
	minLat, minLon, maxLat, maxLon := g.BoundingBox()
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"name": g.Name, "source": g.Source, "profile": g.Profile, "metric": g.Metric, "nodes": len(g.Nodes), "edges": g.EdgeCount, "directed": g.Directed, "minCostPerMeter": g.MinCostPerMeter, "meanCostPerMeter": g.MeanCostPerMeter, "heuristicStrength": g.HeuristicStrength, "averageDegree": g.AverageDegree, "diameterMeters": g.DiameterMeters, "bbox": []float64{minLat, minLon, maxLat, maxLon}})
}

func resolve(g *graph.Graph, s string) (int, error) {
	s = strings.TrimSpace(s)
	if strings.Contains(s, ",") {
		p := strings.Split(s, ",")
		if len(p) != 2 {
			return -1, errors.New("invalid coordinates")
		}
		lat, e1 := strconv.ParseFloat(strings.TrimSpace(p[0]), 64)
		lon, e2 := strconv.ParseFloat(strings.TrimSpace(p[1]), 64)
		if e1 != nil || e2 != nil {
			return -1, errors.New("invalid coordinates")
		}
		i, _ := g.Nearest(lat, lon)
		return i, nil
	}
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return -1, errors.New("expected node ID or lat,lon")
	}
	if i, ok := g.IndexByID(id); ok {
		return i, nil
	}
	return -1, errors.New("node not found")
}

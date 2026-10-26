package bench

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sahenjp/aegis-acbs/internal/graph"
	"github.com/sahenjp/aegis-acbs/internal/search"
	"github.com/sahenjp/aegis-acbs/internal/version"
)

// TriggerProfileConfig controls whole-suite scheduler checkpoint profiling.
type TriggerProfileConfig struct {
	Checkpoints  []uint64      `json:"checkpoints"`
	Timeout      time.Duration `json:"-"`
	TimeoutNS    int64         `json:"timeoutNs"`
	MaxMatches   int           `json:"maxMatches"`
	TopRules     int           `json:"topRules"`
	LabelRepeats int           `json:"labelRepeats"`
}

// TriggerCheckpoint contains deterministic scheduler features at one chunk.
type TriggerCheckpoint struct {
	Chunk                    uint64  `json:"chunk"`
	Reached                  bool    `json:"reached"`
	UpperBoundMissing        bool    `json:"upperBoundMissing"`
	LowerBound               uint64  `json:"lowerBound"`
	CumulativeWork           uint64  `json:"cumulativeWork"`
	CumulativeLowerBoundGain uint64  `json:"cumulativeLowerBoundGain"`
	LowerGainPerWork         float64 `json:"lowerGainPerWork"`
	RecentLowerGainPerWork   float64 `json:"recentLowerGainPerWork"`
	SwitchRate               float64 `json:"switchRate"`
	ScoreImbalance           float64 `json:"scoreImbalance"`
	QueueImbalance           float64 `json:"queueImbalance"`
	PriorityImbalance        float64 `json:"priorityImbalance"`
	FrontierGrowth           float64 `json:"frontierGrowth"`
	StaleRate                float64 `json:"staleRate"`
	FiniteMeetingRate        float64 `json:"finiteMeetingRate"`
	DirectionWorkImbalance   float64 `json:"directionWorkImbalance"`
	QueueTotal               int     `json:"queueTotal"`
	ForwardQueue             int     `json:"forwardQueue"`
	BackwardQueue            int     `json:"backwardQueue"`
	ForwardScore             float64 `json:"forwardScore"`
	BackwardScore            float64 `json:"backwardScore"`
	ForwardPriority          uint64  `json:"forwardPriority"`
	BackwardPriority         uint64  `json:"backwardPriority"`
	DirectionSwitches        uint64  `json:"directionSwitches"`
	FiniteMeetings           uint64  `json:"finiteMeetings"`
}

// TriggerProfileRow is one deterministic query profile.
type TriggerProfileRow struct {
	SourceReport       string              `json:"sourceReport"`
	Seed               uint64              `json:"seed"`
	QueryIndex         int                 `json:"queryIndex"`
	Class              string              `json:"class"`
	SourceID           int64               `json:"sourceId"`
	TargetID           int64               `json:"targetId"`
	StraightLineMeters float64             `json:"straightLineMeters"`
	SchedulerTail      bool                `json:"schedulerTail"`
	PersistentTail     bool                `json:"persistentTail"`
	Correct            bool                `json:"correct"`
	Stable             bool                `json:"stable"`
	ProfileRepeats     int                 `json:"profileRepeats"`
	Error              string              `json:"error,omitempty"`
	Chunks             uint64              `json:"chunks"`
	UpperBoundChunk    uint64              `json:"upperBoundChunk,omitempty"`
	Expanded           uint64              `json:"expanded"`
	Relaxed            uint64              `json:"relaxed"`
	Checkpoints        []TriggerCheckpoint `json:"checkpoints"`
}

// TriggerCondition is an interpretable threshold predicate.
type TriggerCondition struct {
	Feature   string  `json:"feature"`
	Operator  string  `json:"operator"`
	Threshold float64 `json:"threshold"`
}

// TriggerRule is a candidate narrow scheduler trigger.
type TriggerRule struct {
	Checkpoint      uint64             `json:"checkpoint"`
	RequireNoUpper  bool               `json:"requireNoUpper"`
	Conditions      []TriggerCondition `json:"conditions"`
	Matches         int                `json:"matches"`
	PositiveMatches int                `json:"positiveMatches"`
	FalsePositives  int                `json:"falsePositives"`
	Precision       float64            `json:"precision"`
	Recall          float64            `json:"recall"`
	Eligible        bool               `json:"eligible"`
}

// TriggerProfileReport contains whole-suite profiles and ranked rules.
type TriggerProfileReport struct {
	Version         string               `json:"version"`
	GeneratedAt     time.Time            `json:"generatedAt"`
	AegisVersion    string               `json:"aegisVersion"`
	GraphName       string               `json:"graphName"`
	Metric          string               `json:"metric"`
	ValidationPath  string               `json:"validationPath"`
	ReplayPath      string               `json:"replayPath"`
	Config          TriggerProfileConfig `json:"config"`
	Rows            []TriggerProfileRow  `json:"rows"`
	Rules           []TriggerRule        `json:"rules"`
	SelectedRule    *TriggerRule         `json:"selectedRule,omitempty"`
	Queries         int                  `json:"queries"`
	SchedulerTails  int                  `json:"schedulerTails"`
	PersistentTails int                  `json:"persistentTails"`
	TraceErrors     int                  `json:"traceErrors"`
	UnstableLabels  int                  `json:"unstableLabels"`
	AllCorrect      bool                 `json:"allCorrect"`
}

type triggerLabel struct {
	scheduler  bool
	persistent bool
}

func LoadRegretReplay(path string) (RegretReplayReport, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return RegretReplayReport{}, err
	}
	var report RegretReplayReport
	if err := json.Unmarshal(data, &report); err != nil {
		return RegretReplayReport{}, err
	}
	return report, nil
}

// ProfileSchedulerTriggers traces every query referenced by a validation report
// once, retains only configured checkpoints, and searches narrow threshold rules
// that cover every replay-confirmed adaptive scheduler tail.
func ProfileSchedulerTriggers(ctx context.Context, g *graph.Graph, validationPath, replayPath, inputRoot string, cfg TriggerProfileConfig) (TriggerProfileReport, error) {
	if len(cfg.Checkpoints) == 0 {
		cfg.Checkpoints = []uint64{24, 32, 40, 48}
	}
	cfg.Checkpoints = normalizeCheckpoints(cfg.Checkpoints)
	if cfg.Timeout <= 0 && cfg.TimeoutNS > 0 {
		cfg.Timeout = time.Duration(cfg.TimeoutNS)
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	cfg.TimeoutNS = cfg.Timeout.Nanoseconds()
	if cfg.MaxMatches <= 0 {
		cfg.MaxMatches = 5
	}
	if cfg.TopRules <= 0 {
		cfg.TopRules = 20
	}
	if cfg.LabelRepeats <= 0 {
		cfg.LabelRepeats = 3
	}

	validation, err := LoadRegretValidation(validationPath)
	if err != nil {
		return TriggerProfileReport{}, err
	}
	replay, err := LoadRegretReplay(replayPath)
	if err != nil {
		return TriggerProfileReport{}, err
	}
	if inputRoot == "" {
		inputRoot = filepath.Dir(validationPath)
	}
	absRoot, err := filepath.Abs(inputRoot)
	if err != nil {
		return TriggerProfileReport{}, err
	}

	labels := make(map[string]triggerLabel)
	for _, c := range replay.Cases {
		key := triggerKey(c.SourceReport, c.QueryIndex)
		labels[key] = triggerLabel{
			scheduler:  c.Classification == "adaptive-scheduler-tail",
			persistent: c.Classification == "persistent-classical-tail",
		}
	}

	out := TriggerProfileReport{
		Version: "trigger-profile-v1", GeneratedAt: time.Now().UTC(), AegisVersion: version.Version,
		GraphName: g.Name, Metric: string(g.Metric), ValidationPath: validationPath, ReplayPath: replayPath,
		Config: cfg, AllCorrect: true,
	}
	for _, run := range validation.Runs {
		path := filepath.Join(absRoot, filepath.FromSlash(run.Path))
		sourceReport, loadErr := LoadReport(path)
		if loadErr != nil {
			return out, fmt.Errorf("load source report %s: %w", path, loadErr)
		}
		for queryIndex, q := range sourceReport.Queries {
			row := TriggerProfileRow{SourceReport: run.Path, Seed: run.Seed, QueryIndex: queryIndex, Class: q.Class, StraightLineMeters: q.StraightLineMeters, Correct: true, Stable: true}
			label := labels[triggerKey(run.Path, queryIndex)]
			row.SchedulerTail, row.PersistentTail = label.scheduler, label.persistent
			if row.SchedulerTail {
				out.SchedulerTails++
			}
			if row.PersistentTail {
				out.PersistentTails++
			}
			if q.Source < 0 || q.Source >= len(g.Nodes) || q.Target < 0 || q.Target >= len(g.Nodes) {
				row.Correct = false
				row.Error = "query indices are out of range"
				out.TraceErrors++
				out.AllCorrect = false
				out.Rows = append(out.Rows, row)
				continue
			}
			row.SourceID, row.TargetID = g.Nodes[q.Source].ID, g.Nodes[q.Target].ID
			repeats := 1
			if row.SchedulerTail || row.PersistentTail {
				repeats = cfg.LabelRepeats
			}
			row.ProfileRepeats = repeats
			var events []search.ACBSTraceEvent
			var referenceEvents []search.ACBSTraceEvent
			for repeat := 0; repeat < repeats; repeat++ {
				events = nil
				traceCtx := search.WithACBSTrace(ctx, func(event search.ACBSTraceEvent) { events = append(events, event) })
				runCtx, cancel := context.WithTimeout(traceCtx, cfg.Timeout)
				result, runErr := search.Run(runCtx, g, q.Source, q.Target, search.Aegis)
				cancel()
				if runErr != nil {
					row.Correct = false
					row.Error = runErr.Error()
					out.TraceErrors++
					out.AllCorrect = false
					break
				}
				if !search.Validate(g, q.Source, q.Target, result) {
					row.Correct = false
					row.Error = "path validation failed"
					out.AllCorrect = false
					break
				}
				if repeat == 0 {
					referenceEvents = append([]search.ACBSTraceEvent(nil), events...)
					row.Chunks, row.Expanded, row.Relaxed = result.Stats.Chunks, result.Stats.Expanded, result.Stats.Relaxed
				} else if !reflect.DeepEqual(referenceEvents, events) {
					row.Stable = false
				}
			}
			events = referenceEvents
			if !row.Stable && (row.SchedulerTail || row.PersistentTail) {
				out.UnstableLabels++
			}
			for _, event := range events {
				if event.HadUpperBoundAfter {
					row.UpperBoundChunk = event.Chunk
					break
				}
			}
			row.Checkpoints = summarizeTriggerCheckpoints(events, cfg.Checkpoints)
			out.Rows = append(out.Rows, row)
		}
	}
	out.Queries = len(out.Rows)
	if out.Queries == 0 {
		return out, errors.New("validation report did not reference any benchmark queries")
	}
	out.Rules = searchTriggerRules(out.Rows, cfg)
	if len(out.Rules) > cfg.TopRules {
		out.Rules = out.Rules[:cfg.TopRules]
	}
	for i := range out.Rules {
		if out.Rules[i].Eligible {
			rule := out.Rules[i]
			out.SelectedRule = &rule
			break
		}
	}
	return out, nil
}

func normalizeCheckpoints(values []uint64) []uint64 {
	seen := map[uint64]bool{}
	out := make([]uint64, 0, len(values))
	for _, v := range values {
		if v == 0 || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func triggerKey(path string, query int) string {
	return filepath.ToSlash(filepath.Clean(path)) + "#" + strconv.Itoa(query)
}

func summarizeTriggerCheckpoints(events []search.ACBSTraceEvent, checkpoints []uint64) []TriggerCheckpoint {
	out := make([]TriggerCheckpoint, 0, len(checkpoints))
	for _, checkpoint := range checkpoints {
		cp := TriggerCheckpoint{Chunk: checkpoint}
		if len(events) < int(checkpoint) {
			out = append(out, cp)
			continue
		}
		cp.Reached = true
		var totalWork, totalGain, pops, stale, checks, meetings, forwardWork, backwardWork uint64
		initialQueue := events[0].ForwardQueueBefore + events[0].BackwardQueueBefore
		startRecent := int(checkpoint) - 8
		if startRecent < 0 {
			startRecent = 0
		}
		var recentWork, recentGain uint64
		for i := 0; i < int(checkpoint); i++ {
			e := events[i]
			totalWork += e.Work
			totalGain += e.LowerBoundGain
			pops += e.QueuePopsDelta
			stale += e.StalePopsDelta
			checks += e.ConnectionChecksDelta
			meetings += e.FiniteMeetingsDelta
			if e.Direction == "F" {
				forwardWork += e.Work
			} else {
				backwardWork += e.Work
			}
			if i >= startRecent {
				recentWork += e.Work
				recentGain += e.LowerBoundGain
			}
		}
		e := events[checkpoint-1]
		cp.UpperBoundMissing = !e.HadUpperBoundAfter
		cp.LowerBound = e.AfterLowerBound
		cp.CumulativeWork, cp.CumulativeLowerBoundGain = totalWork, totalGain
		cp.LowerGainPerWork = safeRatio(float64(totalGain), float64(totalWork))
		cp.RecentLowerGainPerWork = safeRatio(float64(recentGain), float64(recentWork))
		cp.SwitchRate = safeRatio(float64(e.DirectionSwitchesTotal), float64(checkpoint))
		cp.ScoreImbalance = normalizedImbalance(e.ForwardScoreAfter, e.BackwardScoreAfter)
		cp.QueueImbalance = normalizedImbalance(float64(e.ForwardQueueAfter), float64(e.BackwardQueueAfter))
		cp.PriorityImbalance = normalizedImbalance(float64(e.ForwardPriorityAfter), float64(e.BackwardPriorityAfter))
		cp.FrontierGrowth = safeRatio(float64(e.ForwardQueueAfter+e.BackwardQueueAfter), float64(maxInt(initialQueue, 1)))
		cp.StaleRate = safeRatio(float64(stale), float64(pops))
		cp.FiniteMeetingRate = safeRatio(float64(meetings), float64(checks))
		cp.DirectionWorkImbalance = normalizedImbalance(float64(forwardWork), float64(backwardWork))
		cp.QueueTotal, cp.ForwardQueue, cp.BackwardQueue = e.ForwardQueueAfter+e.BackwardQueueAfter, e.ForwardQueueAfter, e.BackwardQueueAfter
		cp.ForwardScore, cp.BackwardScore = e.ForwardScoreAfter, e.BackwardScoreAfter
		cp.ForwardPriority, cp.BackwardPriority = e.ForwardPriorityAfter, e.BackwardPriorityAfter
		cp.DirectionSwitches, cp.FiniteMeetings = e.DirectionSwitchesTotal, meetings
		out = append(out, cp)
	}
	return out
}

func safeRatio(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}

func normalizedImbalance(a, b float64) float64 {
	m := math.Max(math.Abs(a), math.Abs(b))
	if m == 0 {
		return 0
	}
	return math.Abs(a-b) / m
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func checkpointFor(row TriggerProfileRow, chunk uint64) (TriggerCheckpoint, bool) {
	for _, cp := range row.Checkpoints {
		if cp.Chunk == chunk {
			return cp, cp.Reached
		}
	}
	return TriggerCheckpoint{}, false
}

var triggerFeatures = []string{
	"lowerGainPerWork", "recentLowerGainPerWork", "switchRate", "scoreImbalance", "queueImbalance",
	"priorityImbalance", "frontierGrowth", "staleRate", "finiteMeetingRate", "directionWorkImbalance", "queueTotal", "cumulativeWork",
}

func triggerFeature(cp TriggerCheckpoint, name string) float64 {
	switch name {
	case "lowerGainPerWork":
		return cp.LowerGainPerWork
	case "recentLowerGainPerWork":
		return cp.RecentLowerGainPerWork
	case "switchRate":
		return cp.SwitchRate
	case "scoreImbalance":
		return cp.ScoreImbalance
	case "queueImbalance":
		return cp.QueueImbalance
	case "priorityImbalance":
		return cp.PriorityImbalance
	case "frontierGrowth":
		return cp.FrontierGrowth
	case "staleRate":
		return cp.StaleRate
	case "finiteMeetingRate":
		return cp.FiniteMeetingRate
	case "directionWorkImbalance":
		return cp.DirectionWorkImbalance
	case "queueTotal":
		return float64(cp.QueueTotal)
	case "cumulativeWork":
		return float64(cp.CumulativeWork)
	default:
		return 0
	}
}

func searchTriggerRules(rows []TriggerProfileRow, cfg TriggerProfileConfig) []TriggerRule {
	positiveTotal := 0
	for _, row := range rows {
		if row.SchedulerTail {
			positiveTotal++
		}
	}
	if positiveTotal == 0 {
		return nil
	}
	seen := map[string]bool{}
	var candidates []TriggerRule
	for _, checkpoint := range cfg.Checkpoints {
		var positives []TriggerCheckpoint
		for _, row := range rows {
			if !row.SchedulerTail {
				continue
			}
			if cp, ok := checkpointFor(row, checkpoint); ok && cp.UpperBoundMissing {
				positives = append(positives, cp)
			}
		}
		if len(positives) != positiveTotal {
			continue
		}
		var atoms []TriggerCondition
		for _, feature := range triggerFeatures {
			lo, hi := math.Inf(1), math.Inf(-1)
			for _, cp := range positives {
				v := triggerFeature(cp, feature)
				lo = math.Min(lo, v)
				hi = math.Max(hi, v)
			}
			atoms = append(atoms,
				TriggerCondition{Feature: feature, Operator: ">=", Threshold: lo},
				TriggerCondition{Feature: feature, Operator: "<=", Threshold: hi},
			)
		}
		for i := range atoms {
			addTriggerRule(&candidates, seen, evaluateTriggerRule(rows, checkpoint, []TriggerCondition{atoms[i]}, positiveTotal, cfg.MaxMatches))
			for j := i + 1; j < len(atoms); j++ {
				if atoms[i].Feature == atoms[j].Feature {
					continue
				}
				addTriggerRule(&candidates, seen, evaluateTriggerRule(rows, checkpoint, []TriggerCondition{atoms[i], atoms[j]}, positiveTotal, cfg.MaxMatches))
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.Eligible != b.Eligible {
			return a.Eligible
		}
		if a.Recall != b.Recall {
			return a.Recall > b.Recall
		}
		if a.Matches != b.Matches {
			return a.Matches < b.Matches
		}
		if len(a.Conditions) != len(b.Conditions) {
			return len(a.Conditions) < len(b.Conditions)
		}
		if a.Checkpoint != b.Checkpoint {
			return a.Checkpoint > b.Checkpoint
		}
		return triggerRuleText(a) < triggerRuleText(b)
	})
	return candidates
}

func addTriggerRule(out *[]TriggerRule, seen map[string]bool, rule TriggerRule) {
	key := triggerRuleText(rule)
	if seen[key] {
		return
	}
	seen[key] = true
	*out = append(*out, rule)
}

func evaluateTriggerRule(rows []TriggerProfileRow, checkpoint uint64, conditions []TriggerCondition, positiveTotal, maxMatches int) TriggerRule {
	rule := TriggerRule{Checkpoint: checkpoint, RequireNoUpper: true, Conditions: append([]TriggerCondition(nil), conditions...)}
	for _, row := range rows {
		cp, ok := checkpointFor(row, checkpoint)
		if !ok || !cp.UpperBoundMissing || !conditionsMatch(cp, conditions) {
			continue
		}
		rule.Matches++
		if row.SchedulerTail {
			rule.PositiveMatches++
		}
	}
	rule.FalsePositives = rule.Matches - rule.PositiveMatches
	rule.Precision = safeRatio(float64(rule.PositiveMatches), float64(rule.Matches))
	rule.Recall = safeRatio(float64(rule.PositiveMatches), float64(positiveTotal))
	rule.Eligible = rule.PositiveMatches == positiveTotal && rule.Matches <= maxMatches
	return rule
}

func conditionsMatch(cp TriggerCheckpoint, conditions []TriggerCondition) bool {
	for _, c := range conditions {
		v := triggerFeature(cp, c.Feature)
		switch c.Operator {
		case ">=":
			if v < c.Threshold {
				return false
			}
		case "<=":
			if v > c.Threshold {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func triggerRuleText(rule TriggerRule) string {
	parts := []string{fmt.Sprintf("chunk=%d", rule.Checkpoint), "upper=missing"}
	for _, c := range rule.Conditions {
		parts = append(parts, fmt.Sprintf("%s%s%.9g", c.Feature, c.Operator, c.Threshold))
	}
	return strings.Join(parts, " && ")
}

func WriteTriggerProfileJSON(path string, report TriggerProfileReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func WriteTriggerProfileCSV(path string, report TriggerProfileReport) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	header := []string{"source_report", "seed", "query", "class", "source_id", "target_id", "scheduler_tail", "persistent_tail", "correct", "checkpoint", "reached", "upper_missing", "lower_gain_per_work", "recent_lower_gain_per_work", "switch_rate", "score_imbalance", "queue_imbalance", "priority_imbalance", "frontier_growth", "stale_rate", "finite_meeting_rate", "direction_work_imbalance", "queue_total", "cumulative_work"}
	if err := w.Write(header); err != nil {
		return err
	}
	for _, row := range report.Rows {
		for _, cp := range row.Checkpoints {
			record := []string{row.SourceReport, strconv.FormatUint(row.Seed, 10), strconv.Itoa(row.QueryIndex), row.Class, strconv.FormatInt(row.SourceID, 10), strconv.FormatInt(row.TargetID, 10), strconv.FormatBool(row.SchedulerTail), strconv.FormatBool(row.PersistentTail), strconv.FormatBool(row.Correct), strconv.FormatUint(cp.Chunk, 10), strconv.FormatBool(cp.Reached), strconv.FormatBool(cp.UpperBoundMissing), fmt.Sprintf("%.9g", cp.LowerGainPerWork), fmt.Sprintf("%.9g", cp.RecentLowerGainPerWork), fmt.Sprintf("%.9g", cp.SwitchRate), fmt.Sprintf("%.9g", cp.ScoreImbalance), fmt.Sprintf("%.9g", cp.QueueImbalance), fmt.Sprintf("%.9g", cp.PriorityImbalance), fmt.Sprintf("%.9g", cp.FrontierGrowth), fmt.Sprintf("%.9g", cp.StaleRate), fmt.Sprintf("%.9g", cp.FiniteMeetingRate), fmt.Sprintf("%.9g", cp.DirectionWorkImbalance), strconv.Itoa(cp.QueueTotal), strconv.FormatUint(cp.CumulativeWork, 10)}
			if err := w.Write(record); err != nil {
				return err
			}
		}
	}
	return w.Error()
}

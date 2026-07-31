package search

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/lasder-ca/aegis-acbs/internal/graph"
)

type Algorithm string

const (
	Dijkstra          Algorithm = "dijkstra"
	BiDijkstra        Algorithm = "bidijkstra"
	AStar             Algorithm = "astar"
	Aegis             Algorithm = "aegis"
	AegisEntropic     Algorithm = "aegis-entropic"
	AegisStatic       Algorithm = "aegis-static"
	AegisLateGuard    Algorithm = "aegis-late-guard"
	AegisConnect32    Algorithm = "aegis-connect-32"
	AegisConnect40    Algorithm = "aegis-connect-40"
	AegisConnect32x16 Algorithm = "aegis-connect-32x16"
	AegisPrune        Algorithm = "aegis-prune"
	AegisNoPrune      Algorithm = "aegis-no-prune" // deprecated compatibility alias for aegis
	AegisProjection   Algorithm = "aegis-projection"
	Portfolio         Algorithm = "portfolio"
	AegisRace         Algorithm = "aegis-race"
)

const (
	policyVersion = "road-v3-time-aware"
	// Below the first ACBS edge-budget tier, the proof scheduler has no room
	// to amortize its state updates before local routes terminate. Preserve the
	// production transition system while retaining the candidate identity.
	aegisEntropicMinimumEdges = 10_000
)

// Decision exposes the deterministic features used by the Aegis selector.
// PredictedWork is a unitless relative estimate; it is not presented as time.
type Decision struct {
	PolicyVersion       string                `json:"policyVersion"`
	Selected            Algorithm             `json:"selected"`
	Reason              string                `json:"reason"`
	NodeCount           int                   `json:"nodeCount"`
	EdgeCount           int                   `json:"edgeCount"`
	StraightLineMeters  float64               `json:"straightLineMeters"`
	DistanceRatio       float64               `json:"distanceRatio"`
	AverageDegree       float64               `json:"averageDegree"`
	HeuristicStrength   float64               `json:"heuristicStrength"`
	Metric              graph.Metric          `json:"metric"`
	AStarRatioLimit     float64               `json:"aStarRatioLimit,omitempty"`
	SourceDegree        int                   `json:"sourceDegree"`
	TargetReverseDegree int                   `json:"targetReverseDegree"`
	PredictedWork       map[Algorithm]float64 `json:"predictedWork"`
}

type Stats struct {
	Algorithm                  Algorithm `json:"algorithm"`
	Selected                   Algorithm `json:"selected,omitempty"`
	DurationNS                 int64     `json:"durationNs"`
	Expanded                   uint64    `json:"expanded"`
	Relaxed                    uint64    `json:"relaxed"`
	QueuePushes                uint64    `json:"queuePushes"`
	QueuePops                  uint64    `json:"queuePops"`
	StalePops                  uint64    `json:"stalePops"`
	Distance                   uint64    `json:"distance"`
	Reachable                  bool      `json:"reachable"`
	PathNodes                  int       `json:"pathNodes"`
	ForwardExpanded            uint64    `json:"forwardExpanded,omitempty"`
	BackwardExpanded           uint64    `json:"backwardExpanded,omitempty"`
	DirectionSwitches          uint64    `json:"directionSwitches,omitempty"`
	LateGuardActivations       uint64    `json:"lateGuardActivations,omitempty"`
	LateGuardChunks            uint64    `json:"lateGuardChunks,omitempty"`
	LateGuardFirstChunk        uint64    `json:"lateGuardFirstChunk,omitempty"`
	ConnectionGuardActivations uint64    `json:"connectionGuardActivations,omitempty"`
	ConnectionGuardChunks      uint64    `json:"connectionGuardChunks,omitempty"`
	ConnectionGuardFirstChunk  uint64    `json:"connectionGuardFirstChunk,omitempty"`
	ConnectionGuardMode        string    `json:"connectionGuardMode,omitempty"`
	Chunks                     uint64    `json:"chunks,omitempty"`
	FirstUpperBoundExpanded    uint64    `json:"firstUpperBoundExpanded,omitempty"`
	TerminationLowerBound      uint64    `json:"terminationLowerBound,omitempty"`
	ForwardEfficiency          float64   `json:"forwardEfficiency,omitempty"`
	BackwardEfficiency         float64   `json:"backwardEfficiency,omitempty"`
	MeetingChecks              uint64    `json:"meetingChecks,omitempty"`
	ConnectionChecks           uint64    `json:"connectionChecks,omitempty"`
	FiniteMeetings             uint64    `json:"finiteMeetings,omitempty"`
	UpperBoundUpdates          uint64    `json:"upperBoundUpdates,omitempty"`
	PrunedAtPop                uint64    `json:"prunedAtPop,omitempty"`
	PrunedAtRelax              uint64    `json:"prunedAtRelax,omitempty"`
	BoundPruned                uint64    `json:"boundPruned,omitempty"`
	PotentialEvaluations       uint64    `json:"potentialEvaluations,omitempty"`
	BoundEvaluations           uint64    `json:"boundEvaluations,omitempty"`
	UpperBound                 uint64    `json:"upperBound,omitempty"`
	LowerBound                 uint64    `json:"lowerBound,omitempty"`
	OptimalityGap              uint64    `json:"optimalityGap,omitempty"`
	SchedulerVersion           string    `json:"schedulerVersion,omitempty"`
	PotentialModel             string    `json:"potentialModel,omitempty"`
	AllocBytes                 uint64    `json:"allocBytes,omitempty"`
	AllocObjects               uint64    `json:"allocObjects,omitempty"`
}

type Result struct {
	Path  []int `json:"path"`
	Stats Stats `json:"stats"`
}

func Run(ctx context.Context, g *graph.Graph, source, target int, alg Algorithm) (Result, error) {
	if source < 0 || source >= len(g.Nodes) || target < 0 || target >= len(g.Nodes) {
		return Result{}, errors.New("source or target is out of range")
	}
	started := time.Now()
	var r Result
	var err error
	switch alg {
	case Dijkstra:
		r, err = dijkstra(ctx, g, source, target, false)
	case AStar:
		if g.MinCostPerMeter <= 0 {
			return Result{}, errors.New("A* requires coordinates and an admissible cost-per-meter bound")
		}
		r, err = dijkstra(ctx, g, source, target, true)
	case BiDijkstra:
		r, err = bidirectionalDijkstra(ctx, g, source, target)
	case Aegis:
		r, err = acbs(ctx, g, source, target)
	case AegisEntropic:
		if g.EdgeCount < aegisEntropicMinimumEdges {
			r, err = acbs(ctx, g, source, target)
			r.Stats.Algorithm = AegisEntropic
			r.Stats.SchedulerVersion = acbsEntropicSchedulerVersion
		} else {
			r, err = acbsEntropic(ctx, g, source, target)
		}
	case AegisStatic:
		r, err = acbsStatic(ctx, g, source, target)
	case AegisLateGuard:
		r, err = acbsLateGuard(ctx, g, source, target)
	case AegisConnect32:
		r, err = acbsConnect32(ctx, g, source, target)
	case AegisConnect40:
		r, err = acbsConnect40(ctx, g, source, target)
	case AegisConnect32x16:
		r, err = acbsConnect32x16(ctx, g, source, target)
	case AegisPrune:
		r, err = acbsPrune(ctx, g, source, target)
	case AegisNoPrune:
		r, err = acbsNoPrune(ctx, g, source, target)
	case AegisProjection:
		r, err = acbsProjection(ctx, g, source, target)
	case Portfolio:
		selected := Select(g, source, target)
		switch selected {
		case Dijkstra:
			r, err = dijkstra(ctx, g, source, target, false)
		case AStar:
			r, err = dijkstra(ctx, g, source, target, true)
		case BiDijkstra:
			r, err = bidirectionalDijkstra(ctx, g, source, target)
		default:
			err = fmt.Errorf("selector returned unsupported algorithm %q", selected)
		}
		r.Stats.Algorithm = Portfolio
		r.Stats.Selected = selected
	case AegisRace:
		r, err = race(ctx, g, source, target)
	default:
		return Result{}, fmt.Errorf("unknown algorithm %q", alg)
	}
	r.Stats.DurationNS = time.Since(started).Nanoseconds()
	return r, err
}

// Select returns the exact core algorithm chosen by the allocation-free road policy.
func Select(g *graph.Graph, source, target int) Algorithm {
	if len(g.Nodes) < 4096 {
		return Dijkstra
	}
	_, ratio, hasGeography := queryGeometry(g, source, target)
	if !hasGeography || g.HeuristicStrength < 0.05 {
		return BiDijkstra
	}

	// Travel-time weights have a much weaker lower bound than pure distance.
	// A* wins on local and medium routes, while two balanced frontiers are more
	// stable on long cross-region routes. The threshold scales with the measured
	// admissible-heuristic strength of the imported graph.
	if g.Metric == graph.MetricTime {
		if ratio <= timeAStarRatioLimit(g.HeuristicStrength) {
			return AStar
		}
		return BiDijkstra
	}

	// Distance graphs normally have a near-perfect geographic lower bound. Use
	// A* whenever that bound is useful; otherwise fall back to balanced frontiers.
	if g.HeuristicStrength >= 0.18 {
		return AStar
	}
	return BiDijkstra
}

// Explain returns the complete deterministic selector explanation for UI and
// diagnostics. It is kept off the timed routing hot path.
func Explain(g *graph.Graph, source, target int) Decision {
	selected := Select(g, source, target)
	return explainDecision(g, source, target, selected)
}

func queryGeometry(g *graph.Graph, source, target int) (straight, ratio float64, hasGeography bool) {
	hasGeography = g.MinCostPerMeter > 0 && g.DiameterMeters > 0
	if !hasGeography {
		return 0, 0.35, false
	}
	straight = graph.HaversineMeters(g.Nodes[source].Lat, g.Nodes[source].Lon, g.Nodes[target].Lat, g.Nodes[target].Lon)
	ratio = clamp(straight/g.DiameterMeters, 0, 1)
	return straight, ratio, true
}

func timeAStarRatioLimit(strength float64) float64 {
	// Tokyo's travel-time graph has strength ~=0.25, producing a limit ~=0.43.
	// This keeps local/random routes on A* and sends the longest regional routes
	// to bidirectional Dijkstra, reducing the heavy A* tail.
	return clamp(0.18+strength, 0.22, 0.62)
}

func explainDecision(g *graph.Graph, source, target int, selected Algorithm) Decision {
	n := len(g.Nodes)
	edges := g.EdgeCount
	avgDegree := g.AverageDegree
	if avgDegree <= 0 {
		avgDegree = float64(edges) / math.Max(1, float64(n))
	}
	straight, ratio, hasGeography := queryGeometry(g, source, target)
	strength := clamp(g.HeuristicStrength, 0, 1)
	limit := 0.0
	if g.Metric == graph.MetricTime {
		limit = timeAStarRatioLimit(strength)
	}

	searchFraction := clamp(0.008+0.82*math.Sqrt(ratio), 0.008, 0.95)
	dijkstraWork := 550 + float64(edges)*searchFraction*(1+0.018*float64(g.OutDegree(source)))
	biFraction := clamp(0.018+0.42*math.Sqrt(searchFraction), 0.018, 0.78)
	biWork := 1250 + float64(edges)*biFraction*(1.05+0.025*avgDegree)
	work := map[Algorithm]float64{Dijkstra: dijkstraWork, BiDijkstra: biWork}
	if hasGeography && strength >= 0.05 {
		heuristicGain := clamp(strength*(0.32+0.68*math.Sqrt(ratio)), 0, 0.9)
		aFraction := clamp(searchFraction*(1-0.82*heuristicGain), 0.006, 0.95)
		work[AStar] = 1450 + float64(edges)*aFraction*(1.20+0.035*avgDegree)
	}

	reason := "balanced_frontiers"
	switch {
	case selected == Dijkstra:
		reason = "small_graph"
	case selected == AStar && g.Metric == graph.MetricTime:
		reason = "time_local_or_medium_route"
	case selected == BiDijkstra && g.Metric == graph.MetricTime:
		reason = "time_long_route_tail_control"
	case selected == AStar:
		reason = "strong_distance_heuristic"
	case !hasGeography:
		reason = "coordinates_unavailable"
	case strength < 0.05:
		reason = "weak_geographic_heuristic"
	}

	return Decision{
		PolicyVersion:       policyVersion,
		Selected:            selected,
		Reason:              reason,
		NodeCount:           n,
		EdgeCount:           edges,
		StraightLineMeters:  straight,
		DistanceRatio:       ratio,
		AverageDegree:       avgDegree,
		HeuristicStrength:   g.HeuristicStrength,
		Metric:              g.Metric,
		AStarRatioLimit:     limit,
		SourceDegree:        g.OutDegree(source),
		TargetReverseDegree: g.InDegree(target),
		PredictedWork:       work,
	}
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func reconstruct(prev []int, source, target int) []int {
	if source == target {
		return []int{source}
	}
	if target < 0 || target >= len(prev) || prev[target] < 0 {
		return nil
	}
	length := 1
	for v := target; v != source; {
		v = prev[v]
		if v < 0 || v >= len(prev) {
			return nil
		}
		length++
	}
	path := make([]int, length)
	for i, v := length-1, target; i >= 0; i-- {
		path[i] = v
		if v == source {
			break
		}
		v = prev[v]
	}
	return path
}

// reconstructBidirectional materializes a path with one exact-sized allocation.
// The forward parent array points toward source, while the backward parent array
// points toward target.
func reconstructBidirectional(pf, pb []int32, source, meet, target int) []int {
	if meet < 0 || meet >= len(pf) || meet >= len(pb) {
		return nil
	}
	leftLen := 1
	for v := meet; v != source; {
		v = int(pf[v])
		if v < 0 || v >= len(pf) {
			return nil
		}
		leftLen++
	}
	rightLen := 0
	reachedTarget := meet == target
	for v := int(pb[meet]); v >= 0; v = int(pb[v]) {
		if v >= len(pb) {
			return nil
		}
		rightLen++
		if v == target {
			reachedTarget = true
			break
		}
	}
	if !reachedTarget {
		return nil
	}

	path := make([]int, leftLen+rightLen)
	for i, v := leftLen-1, meet; i >= 0; i-- {
		path[i] = v
		if v == source {
			break
		}
		v = int(pf[v])
	}
	pos := leftLen
	for v := int(pb[meet]); pos < len(path); v = int(pb[v]) {
		path[pos] = v
		pos++
	}
	return path
}

// Validate verifies that a reported path is continuous and that its edge costs
// add up to the reported distance. It is intended for benchmark correctness checks.
func Validate(g *graph.Graph, source, target int, r Result) bool {
	if !r.Stats.Reachable {
		return len(r.Path) == 0
	}
	if len(r.Path) == 0 || r.Path[0] != source || r.Path[len(r.Path)-1] != target {
		return false
	}
	var total uint64
	for i := 0; i+1 < len(r.Path); i++ {
		from, to := r.Path[i], r.Path[i+1]
		best := inf
		for _, e := range g.OutEdges(from) {
			if e.To == to && e.Cost < best {
				best = e.Cost
			}
		}
		if best == inf || total > inf-best {
			return false
		}
		total += best
	}
	return total == r.Stats.Distance
}

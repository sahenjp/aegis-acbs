package maxsearch

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/lasder-ca/aegis-acbs/internal/graph"
	"github.com/lasder-ca/aegis-acbs/internal/search"
)

type Mode string

const (
	ModeLatency   Mode = "latency"
	ModeBalanced  Mode = "balanced"
	ModeEfficient Mode = "efficient"
)

var (
	ErrConsensusMismatch    = errors.New("maxsearch: exact runners disagreed")
	ErrConsensusUnavailable = errors.New("maxsearch: consensus requested but fewer than two exact runners succeeded")
)

type Config struct {
	Mode        Mode               `json:"mode"`
	HedgeDelay  time.Duration      `json:"hedgeDelay"`
	MaxParallel int                `json:"maxParallel"`
	Verify      bool               `json:"verify"`
	Consensus   bool               `json:"consensus"`
	Algorithms  []search.Algorithm `json:"algorithms,omitempty"`
}

type Candidate struct {
	Algorithm     search.Algorithm `json:"algorithm"`
	Role          string           `json:"role"`
	PredictedWork *float64         `json:"predictedWork,omitempty"`
}

type Plan struct {
	Mode        Mode            `json:"mode"`
	Selector    search.Decision `json:"selector"`
	Candidates  []Candidate     `json:"candidates"`
	MaxParallel int             `json:"maxParallel"`
	HedgeDelay  time.Duration   `json:"hedgeDelay"`
}

type Attempt struct {
	Algorithm    search.Algorithm `json:"algorithm"`
	StartedAfter time.Duration    `json:"startedAfter"`
	Duration     time.Duration    `json:"duration"`
	Stats        search.Stats     `json:"stats"`
	Error        string           `json:"error,omitempty"`
}

type Outcome struct {
	Result           search.Result    `json:"result"`
	Winner           search.Algorithm `json:"winner"`
	Plan             Plan             `json:"plan"`
	Attempts         []Attempt        `json:"attempts"`
	Duration         time.Duration    `json:"duration"`
	ConsensusReached bool             `json:"consensusReached"`
}

type Runner interface {
	Name() search.Algorithm
	Run(context.Context, *graph.Graph, int, int) (search.Result, error)
}

type BuiltinRunner struct{ Algorithm search.Algorithm }

func (r BuiltinRunner) Name() search.Algorithm { return r.Algorithm }

func (r BuiltinRunner) Run(ctx context.Context, g *graph.Graph, source, target int) (search.Result, error) {
	return search.Run(ctx, g, source, target, r.Algorithm)
}

type attemptResult struct {
	result  search.Result
	attempt Attempt
	err     error
}

func DefaultConfig() Config {
	return Config{Mode: ModeLatency, MaxParallel: 3, Verify: true}
}

func BuildPlan(g *graph.Graph, source, target int, cfg Config) (Plan, error) {
	if source < 0 || source >= len(g.Nodes) || target < 0 || target >= len(g.Nodes) {
		return Plan{}, errors.New("maxsearch: source or target is out of range")
	}
	cfg, err := normalizeConfig(cfg)
	if err != nil {
		return Plan{}, err
	}
	decision := search.Explain(g, source, target)
	algorithms := cfg.Algorithms
	roles := make(map[search.Algorithm]string)
	if len(algorithms) == 0 {
		algorithms = defaultAlgorithms(g, decision.Selected)
		if len(algorithms) > 0 {
			roles[algorithms[0]] = "primary"
		}
		if len(algorithms) > 1 {
			roles[algorithms[1]] = "diverse-hedge"
		}
		for _, alg := range algorithms[2:] {
			if alg == search.Aegis {
				roles[alg] = "research-exact"
			} else {
				roles[alg] = "fallback"
			}
		}
	} else {
		algorithms = uniqueAlgorithms(algorithms)
		for _, alg := range algorithms {
			roles[alg] = "custom"
		}
	}
	if len(algorithms) == 0 {
		return Plan{}, errors.New("maxsearch: no exact candidates")
	}
	if cfg.MaxParallel > len(algorithms) {
		cfg.MaxParallel = len(algorithms)
	}
	candidates := make([]Candidate, 0, len(algorithms))
	for _, alg := range algorithms {
		candidate := Candidate{Algorithm: alg, Role: roles[alg]}
		if predicted, ok := decision.PredictedWork[alg]; ok {
			value := predicted
			candidate.PredictedWork = &value
		}
		candidates = append(candidates, candidate)
	}
	return Plan{Mode: cfg.Mode, Selector: decision, Candidates: candidates, MaxParallel: cfg.MaxParallel, HedgeDelay: cfg.HedgeDelay}, nil
}

func Candidates(g *graph.Graph, source, target int) []search.Algorithm {
	plan, err := BuildPlan(g, source, target, DefaultConfig())
	if err != nil {
		return nil
	}
	out := make([]search.Algorithm, len(plan.Candidates))
	for i, candidate := range plan.Candidates {
		out[i] = candidate.Algorithm
	}
	return out
}

func defaultAlgorithms(g *graph.Graph, primary search.Algorithm) []search.Algorithm {
	out := make([]search.Algorithm, 0, 4)
	add := func(alg search.Algorithm) {
		if alg == search.AStar && g.MinCostPerMeter <= 0 {
			return
		}
		for _, existing := range out {
			if existing == alg {
				return
			}
		}
		out = append(out, alg)
	}
	add(primary)
	switch primary {
	case search.AStar:
		add(search.BiDijkstra)
	case search.BiDijkstra:
		if g.MinCostPerMeter > 0 {
			add(search.AStar)
		} else {
			add(search.Dijkstra)
		}
	case search.Dijkstra:
		add(search.BiDijkstra)
	default:
		add(search.BiDijkstra)
	}
	add(search.Aegis)
	add(search.AStar)
	add(search.BiDijkstra)
	add(search.Dijkstra)
	return out
}

func uniqueAlgorithms(in []search.Algorithm) []search.Algorithm {
	out := make([]search.Algorithm, 0, len(in))
	seen := make(map[search.Algorithm]struct{}, len(in))
	for _, alg := range in {
		if alg == "" {
			continue
		}
		if _, ok := seen[alg]; ok {
			continue
		}
		seen[alg] = struct{}{}
		out = append(out, alg)
	}
	return out
}

func normalizeConfig(cfg Config) (Config, error) {
	if cfg.Mode == "" {
		cfg.Mode = ModeLatency
	}
	switch cfg.Mode {
	case ModeLatency, ModeBalanced, ModeEfficient:
	default:
		return Config{}, fmt.Errorf("maxsearch: unknown mode %q", cfg.Mode)
	}
	if cfg.MaxParallel <= 0 {
		cfg.MaxParallel = DefaultConfig().MaxParallel
	}
	if cfg.MaxParallel > 8 {
		return Config{}, errors.New("maxsearch: max parallelism is capped at 8")
	}
	if cfg.Mode == ModeEfficient {
		cfg.MaxParallel = 1
		cfg.HedgeDelay = 0
	}
	if cfg.Mode == ModeLatency {
		cfg.HedgeDelay = 0
	}
	if cfg.Mode == ModeBalanced && cfg.HedgeDelay <= 0 {
		return Config{}, errors.New("maxsearch: balanced mode requires a positive hedge delay")
	}
	return cfg, nil
}

func Run(ctx context.Context, g *graph.Graph, source, target int, cfg Config) (Outcome, error) {
	plan, err := BuildPlan(g, source, target, cfg)
	if err != nil {
		return Outcome{}, err
	}
	runners := make([]Runner, 0, len(plan.Candidates))
	for _, candidate := range plan.Candidates {
		runners = append(runners, BuiltinRunner{Algorithm: candidate.Algorithm})
	}
	return runPlan(ctx, g, source, target, cfg, plan, runners)
}

// RunWithRunners is the extension point for future exact algorithms. A custom
// runner does not need a built-in search.Run case, but it must obey the exact
// shortest-path contract. Verify and Consensus can be used while onboarding it.
func RunWithRunners(ctx context.Context, g *graph.Graph, source, target int, cfg Config, runners []Runner) (Outcome, error) {
	if source < 0 || source >= len(g.Nodes) || target < 0 || target >= len(g.Nodes) {
		return Outcome{}, errors.New("maxsearch: source or target is out of range")
	}
	normalized, err := normalizeConfig(cfg)
	if err != nil {
		return Outcome{}, err
	}
	if len(runners) == 0 {
		return Outcome{}, errors.New("maxsearch: no exact runners")
	}
	seen := make(map[search.Algorithm]struct{}, len(runners))
	candidates := make([]Candidate, 0, len(runners))
	unique := make([]Runner, 0, len(runners))
	for _, runner := range runners {
		if runner == nil || runner.Name() == "" {
			continue
		}
		name := runner.Name()
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		unique = append(unique, runner)
		candidates = append(candidates, Candidate{Algorithm: name, Role: "external-exact"})
	}
	if len(unique) == 0 {
		return Outcome{}, errors.New("maxsearch: no usable exact runners")
	}
	if normalized.MaxParallel > len(unique) {
		normalized.MaxParallel = len(unique)
	}
	plan := Plan{Mode: normalized.Mode, Candidates: candidates, MaxParallel: normalized.MaxParallel, HedgeDelay: normalized.HedgeDelay}
	return runPlan(ctx, g, source, target, normalized, plan, unique)
}

func runPlan(ctx context.Context, g *graph.Graph, source, target int, cfg Config, plan Plan, runners []Runner) (Outcome, error) {
	cfg, err := normalizeConfig(cfg)
	if err != nil {
		return Outcome{}, err
	}
	if cfg.MaxParallel > len(runners) {
		cfg.MaxParallel = len(runners)
	}
	plan.MaxParallel = cfg.MaxParallel
	plan.HedgeDelay = cfg.HedgeDelay
	plan.Mode = cfg.Mode

	startedAt := time.Now()
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan attemptResult, len(runners))
	attempts := make([]Attempt, 0, len(runners))
	errs := make([]error, 0, len(runners))
	active, next := 0, 0

	start := func(runner Runner) {
		active++
		startedAfter := time.Since(startedAt)
		go func() {
			at := time.Now()
			r, runErr := runner.Run(runCtx, g, source, target)
			if runErr == nil && cfg.Verify && !search.Validate(g, source, target, r) {
				runErr = fmt.Errorf("maxsearch: %s returned an invalid path", runner.Name())
			}
			attempt := Attempt{Algorithm: runner.Name(), StartedAfter: startedAfter, Duration: time.Since(at), Stats: r.Stats}
			if runErr != nil {
				attempt.Error = runErr.Error()
			}
			results <- attemptResult{result: r, attempt: attempt, err: runErr}
		}()
	}

	start(runners[next])
	next++
	if cfg.Mode == ModeLatency {
		for next < len(runners) && active < cfg.MaxParallel {
			start(runners[next])
			next++
		}
	}

	var timer *time.Timer
	var timerC <-chan time.Time
	armTimer := func() {
		if timer != nil {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
		timerC = nil
		if cfg.Mode != ModeBalanced || next >= len(runners) || active >= cfg.MaxParallel {
			return
		}
		timer = time.NewTimer(cfg.HedgeDelay)
		timerC = timer.C
	}
	armTimer()

	var firstSuccess *attemptResult
	for active > 0 || next < len(runners) {
		select {
		case <-ctx.Done():
			return Outcome{Plan: plan, Attempts: attempts, Duration: time.Since(startedAt)}, ctx.Err()
		case <-timerC:
			if next < len(runners) && active < cfg.MaxParallel {
				start(runners[next])
				next++
			}
			armTimer()
		case got := <-results:
			active--
			attempts = append(attempts, got.attempt)
			if got.err == nil {
				if !cfg.Consensus {
					cancel()
					return Outcome{Result: got.result, Winner: got.attempt.Algorithm, Plan: plan, Attempts: attempts, Duration: time.Since(startedAt)}, nil
				}
				if firstSuccess == nil {
					copyResult := got
					firstSuccess = &copyResult
					if active == 0 && next < len(runners) {
						start(runners[next])
						next++
					}
				} else {
					if firstSuccess.result.Stats.Reachable != got.result.Stats.Reachable || (got.result.Stats.Reachable && firstSuccess.result.Stats.Distance != got.result.Stats.Distance) {
						cancel()
						return Outcome{Result: firstSuccess.result, Winner: firstSuccess.attempt.Algorithm, Plan: plan, Attempts: attempts, Duration: time.Since(startedAt)}, ErrConsensusMismatch
					}
					cancel()
					return Outcome{Result: firstSuccess.result, Winner: firstSuccess.attempt.Algorithm, Plan: plan, Attempts: attempts, Duration: time.Since(startedAt), ConsensusReached: true}, nil
				}
			} else {
				errs = append(errs, got.err)
			}
			if firstSuccess == nil && next < len(runners) && active < cfg.MaxParallel && cfg.Mode != ModeBalanced {
				start(runners[next])
				next++
			}
			if firstSuccess != nil && active == 0 && next < len(runners) {
				start(runners[next])
				next++
			}
			armTimer()
		}
	}
	if firstSuccess != nil {
		return Outcome{Result: firstSuccess.result, Winner: firstSuccess.attempt.Algorithm, Plan: plan, Attempts: attempts, Duration: time.Since(startedAt)}, ErrConsensusUnavailable
	}
	return Outcome{Plan: plan, Attempts: attempts, Duration: time.Since(startedAt)}, errors.Join(errs...)
}

package mcsp

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
)

var (
	ErrTooManyInputs = errors.New("mcsp: uint64 truth tables support at most 6 inputs")
	ErrStateLimit    = errors.New("mcsp: state limit reached")
	ErrNotFound      = errors.New("mcsp: target not found within gate limit")
)

type Gate struct {
	Left  int `json:"left"`
	Right int `json:"right"`
}

type Circuit struct {
	Inputs int    `json:"inputs"`
	Gates  []Gate `json:"gates"`
	Output int    `json:"output"`
}

type Config struct {
	MaxGates  int `json:"maxGates"`
	MaxStates int `json:"maxStates"`
	Workers   int `json:"workers"`
}

type Bound struct {
	Value              int    `json:"value"`
	EssentialVariables int    `json:"essentialVariables"`
	Reason             string `json:"reason"`
}

type Stats struct {
	Expanded     uint64 `json:"expanded"`
	Generated    uint64 `json:"generated"`
	Deduped      uint64 `json:"deduped"`
	PeakFrontier int    `json:"peakFrontier"`
	Depth        int    `json:"depth"`
	LowerBound   int    `json:"lowerBound"`
	SeenStates   int    `json:"seenStates"`
}

type Result struct {
	Circuit Circuit `json:"circuit"`
	Target  uint64  `json:"target"`
	Optimal bool    `json:"optimal"`
	Bound   Bound   `json:"bound"`
	Stats   Stats   `json:"stats"`
}

type Solver interface {
	Name() string
	Solve(context.Context, int, uint64) (Result, error)
}

type ExactBFSSolver struct {
	Config Config
}

func (s ExactBFSSolver) Name() string { return "nand-exact-bfs-v2" }

func (s ExactBFSSolver) Solve(ctx context.Context, inputs int, target uint64) (Result, error) {
	return Solve(ctx, inputs, target, s.Config)
}

type state struct {
	signals []uint64
	gates   []Gate
}

type candidate struct {
	signals []uint64
	gates   []Gate
	value   uint64
}

type expansion struct {
	candidates []candidate
	deduped    uint64
}

func VariableTruthTable(inputs, variable int) (uint64, error) {
	if inputs < 1 || inputs > 6 {
		return 0, ErrTooManyInputs
	}
	if variable < 0 || variable >= inputs {
		return 0, fmt.Errorf("mcsp: variable %d out of range", variable)
	}
	rows := 1 << inputs
	var table uint64
	for row := 0; row < rows; row++ {
		if (row>>(inputs-1-variable))&1 == 1 {
			table |= uint64(1) << row
		}
	}
	return table, nil
}

func Mask(inputs int) (uint64, error) {
	if inputs < 1 || inputs > 6 {
		return 0, ErrTooManyInputs
	}
	rows := 1 << inputs
	if rows == 64 {
		return ^uint64(0), nil
	}
	return (uint64(1) << rows) - 1, nil
}

func EssentialVariableCount(inputs int, target uint64) (int, error) {
	mask, err := Mask(inputs)
	if err != nil {
		return 0, err
	}
	if target&^mask != 0 {
		return 0, errors.New("mcsp: target contains bits outside its truth table")
	}
	rows := 1 << inputs
	count := 0
	for variable := 0; variable < inputs; variable++ {
		shift := inputs - 1 - variable
		delta := 1 << shift
		essential := false
		for row := 0; row < rows; row++ {
			if row&delta != 0 {
				continue
			}
			left := (target >> row) & 1
			right := (target >> (row | delta)) & 1
			if left != right {
				essential = true
				break
			}
		}
		if essential {
			count++
		}
	}
	return count, nil
}

// StructuralLowerBound proves a size lower bound for fan-in-2 NAND circuits.
// If an output depends on k distinct input variables, its transitive fan-in DAG
// needs at least k-1 binary gates to connect those k sources to one output.
func StructuralLowerBound(inputs int, target uint64) (Bound, error) {
	essential, err := EssentialVariableCount(inputs, target)
	if err != nil {
		return Bound{}, err
	}
	value := 0
	if essential > 1 {
		value = essential - 1
	}
	return Bound{
		Value:              value,
		EssentialVariables: essential,
		Reason:             "fan-in-2 transitive support",
	}, nil
}

func Evaluate(c Circuit) (uint64, error) {
	mask, err := Mask(c.Inputs)
	if err != nil {
		return 0, err
	}
	signals := make([]uint64, c.Inputs, c.Inputs+len(c.Gates))
	for i := 0; i < c.Inputs; i++ {
		signals[i], err = VariableTruthTable(c.Inputs, i)
		if err != nil {
			return 0, err
		}
	}
	for i, gate := range c.Gates {
		limit := c.Inputs + i
		if gate.Left < 0 || gate.Left >= limit || gate.Right < 0 || gate.Right >= limit {
			return 0, fmt.Errorf("mcsp: gate %d references unavailable signal", i)
		}
		signals = append(signals, ^(signals[gate.Left]&signals[gate.Right])&mask)
	}
	if c.Output < 0 || c.Output >= len(signals) {
		return 0, errors.New("mcsp: output references unavailable signal")
	}
	return signals[c.Output], nil
}

func Verify(target uint64, c Circuit) error {
	mask, err := Mask(c.Inputs)
	if err != nil {
		return err
	}
	if target&^mask != 0 {
		return errors.New("mcsp: target contains bits outside its truth table")
	}
	got, err := Evaluate(c)
	if err != nil {
		return err
	}
	if got != target {
		return fmt.Errorf("mcsp: circuit computes 0x%x, want 0x%x", got, target)
	}
	return nil
}

func VerifyMinimal(ctx context.Context, target uint64, c Circuit, maxStates int) error {
	if err := Verify(target, c); err != nil {
		return err
	}
	if len(c.Gates) == 0 {
		return nil
	}
	if len(c.Gates) == 1 {
		for i := 0; i < c.Inputs; i++ {
			primary, err := VariableTruthTable(c.Inputs, i)
			if err != nil {
				return err
			}
			if primary == target {
				return errors.New("mcsp: a zero-gate circuit exists")
			}
		}
		return nil
	}
	_, err := Solve(ctx, c.Inputs, target, Config{MaxGates: len(c.Gates) - 1, MaxStates: maxStates, Workers: 1})
	switch {
	case errors.Is(err, ErrNotFound):
		return nil
	case errors.Is(err, ErrStateLimit):
		return errors.New("mcsp: state limit prevented minimality verification")
	case err == nil:
		return errors.New("mcsp: a smaller circuit exists")
	default:
		return err
	}
}

func Solve(ctx context.Context, inputs int, target uint64, cfg Config) (Result, error) {
	mask, err := Mask(inputs)
	if err != nil {
		return Result{}, err
	}
	if target&^mask != 0 {
		return Result{}, errors.New("mcsp: target contains bits outside its truth table")
	}
	cfg = normalizedConfig(cfg)
	bound, err := StructuralLowerBound(inputs, target)
	if err != nil {
		return Result{}, err
	}
	stats := Stats{PeakFrontier: 1, LowerBound: bound.Value}

	initialSignals := make([]uint64, inputs)
	for i := range initialSignals {
		initialSignals[i], _ = VariableTruthTable(inputs, i)
		if initialSignals[i] == target {
			stats.SeenStates = 1
			return Result{Circuit: Circuit{Inputs: inputs, Output: i}, Target: target, Optimal: true, Bound: bound, Stats: stats}, nil
		}
	}
	if cfg.MaxGates < bound.Value {
		stats.Depth = cfg.MaxGates
		stats.SeenStates = 1
		return Result{Target: target, Bound: bound, Stats: stats}, ErrNotFound
	}

	frontier := []state{{signals: initialSignals}}
	seen := map[string]struct{}{canonicalKey(initialSignals): {}}

	for depth := 0; depth < cfg.MaxGates; depth++ {
		expansions, err := expandFrontier(ctx, frontier, mask, cfg.Workers)
		if err != nil {
			return Result{}, err
		}
		stats.Expanded += uint64(len(frontier))
		next := make([]state, 0)
		for _, ex := range expansions {
			stats.Deduped += ex.deduped
			for _, cand := range ex.candidates {
				stats.Generated++
				if cand.value == target {
					circuit := Circuit{Inputs: inputs, Gates: cand.gates, Output: inputs + len(cand.gates) - 1}
					stats.Depth = depth + 1
					stats.SeenStates = len(seen)
					return Result{Circuit: circuit, Target: target, Optimal: true, Bound: bound, Stats: stats}, nil
				}
				key := canonicalKey(cand.signals)
				if _, ok := seen[key]; ok {
					stats.Deduped++
					continue
				}
				if len(seen) >= cfg.MaxStates {
					stats.SeenStates = len(seen)
					return Result{Target: target, Bound: bound, Stats: stats}, ErrStateLimit
				}
				seen[key] = struct{}{}
				next = append(next, state{signals: cand.signals, gates: cand.gates})
			}
		}
		frontier = next
		if len(frontier) > stats.PeakFrontier {
			stats.PeakFrontier = len(frontier)
		}
		if len(frontier) == 0 {
			break
		}
	}
	stats.Depth = cfg.MaxGates
	stats.SeenStates = len(seen)
	return Result{Target: target, Bound: bound, Stats: stats}, ErrNotFound
}

func normalizedConfig(cfg Config) Config {
	if cfg.MaxGates <= 0 {
		cfg.MaxGates = 8
	}
	if cfg.MaxStates <= 0 {
		cfg.MaxStates = 1_000_000
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 1
	}
	if cfg.Workers > 64 {
		cfg.Workers = 64
	}
	return cfg
}

func expandFrontier(ctx context.Context, frontier []state, mask uint64, workers int) ([]expansion, error) {
	out := make([]expansion, len(frontier))
	if workers <= 1 || len(frontier) <= 1 {
		for i, st := range frontier {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			out[i] = expandState(st, mask)
		}
		return out, nil
	}
	if workers > len(frontier) {
		workers = len(frontier)
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	wg.Add(workers)
	for worker := 0; worker < workers; worker++ {
		go func() {
			defer wg.Done()
			for i := range jobs {
				if ctx.Err() != nil {
					continue
				}
				out[i] = expandState(frontier[i], mask)
			}
		}()
	}
	for i := range frontier {
		if err := ctx.Err(); err != nil {
			close(jobs)
			wg.Wait()
			return nil, err
		}
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func expandState(st state, mask uint64) expansion {
	n := len(st.signals)
	out := expansion{candidates: make([]candidate, 0, n*(n+1)/2)}
	for left := 0; left < n; left++ {
		for right := left; right < n; right++ {
			value := ^(st.signals[left] & st.signals[right]) & mask
			if contains(st.signals, value) {
				out.deduped++
				continue
			}
			gates := appendCopy(st.gates, Gate{Left: left, Right: right})
			signals := appendCopy(st.signals, value)
			out.candidates = append(out.candidates, candidate{signals: signals, gates: gates, value: value})
		}
	}
	return out
}

func canonicalKey(signals []uint64) string {
	normalized := append([]uint64(nil), signals...)
	sort.Slice(normalized, func(i, j int) bool { return normalized[i] < normalized[j] })
	b := make([]byte, 8*len(normalized))
	for i, value := range normalized {
		for j := 0; j < 8; j++ {
			b[i*8+j] = byte(value >> (8 * j))
		}
	}
	return string(b)
}

func contains(values []uint64, target uint64) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func appendCopy[T any](src []T, value T) []T {
	dst := make([]T, len(src)+1)
	copy(dst, src)
	dst[len(src)] = value
	return dst
}

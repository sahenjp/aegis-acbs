package maxsearch

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/lasder-ca/aegis-acbs/internal/graph"
	"github.com/lasder-ca/aegis-acbs/internal/search"
)

type Config struct {
	HedgeDelay  time.Duration `json:"hedgeDelay"`
	MaxParallel int           `json:"maxParallel"`
	Verify      bool          `json:"verify"`
}

type Attempt struct {
	Algorithm search.Algorithm `json:"algorithm"`
	Duration  time.Duration    `json:"duration"`
	Error     string           `json:"error,omitempty"`
}

type Outcome struct {
	Result     search.Result      `json:"result"`
	Winner     search.Algorithm   `json:"winner"`
	Candidates []search.Algorithm `json:"candidates"`
	Attempts   []Attempt          `json:"attempts"`
	Duration   time.Duration      `json:"duration"`
}

type attemptResult struct {
	result  search.Result
	attempt Attempt
	err     error
}

func DefaultConfig() Config {
	return Config{MaxParallel: 3, Verify: true}
}

func Candidates(g *graph.Graph, source, target int) []search.Algorithm {
	primary := search.Select(g, source, target)
	out := make([]search.Algorithm, 0, 4)
	add := func(a search.Algorithm) {
		for _, existing := range out {
			if existing == a {
				return
			}
		}
		out = append(out, a)
	}
	add(primary)
	add(search.Aegis)
	if g.MinCostPerMeter > 0 {
		add(search.AStar)
	}
	add(search.BiDijkstra)
	if g.MinCostPerMeter <= 0 {
		add(search.Dijkstra)
	}
	return out
}

func Run(ctx context.Context, g *graph.Graph, source, target int, cfg Config) (Outcome, error) {
	if cfg.MaxParallel <= 0 {
		cfg.MaxParallel = DefaultConfig().MaxParallel
	}
	if cfg.MaxParallel > 8 {
		return Outcome{}, errors.New("maxsearch: max parallelism is capped at 8")
	}
	candidates := Candidates(g, source, target)
	if len(candidates) == 0 {
		return Outcome{}, errors.New("maxsearch: no exact candidates")
	}
	if cfg.MaxParallel > len(candidates) {
		cfg.MaxParallel = len(candidates)
	}

	startedAt := time.Now()
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan attemptResult, len(candidates))
	attempts := make([]Attempt, 0, len(candidates))
	errs := make([]error, 0, len(candidates))
	active, next := 0, 0

	start := func(alg search.Algorithm) {
		active++
		go func() {
			at := time.Now()
			r, err := search.Run(runCtx, g, source, target, alg)
			if err == nil && cfg.Verify && !search.Validate(g, source, target, r) {
				err = fmt.Errorf("maxsearch: %s returned an invalid path", alg)
			}
			a := Attempt{Algorithm: alg, Duration: time.Since(at)}
			if err != nil {
				a.Error = err.Error()
			}
			results <- attemptResult{result: r, attempt: a, err: err}
		}()
	}

	start(candidates[next])
	next++
	if cfg.HedgeDelay <= 0 {
		for next < len(candidates) && active < cfg.MaxParallel {
			start(candidates[next])
			next++
		}
	}

	var timerC <-chan time.Time
	armTimer := func() {
		if cfg.HedgeDelay <= 0 || next >= len(candidates) || active >= cfg.MaxParallel {
			timerC = nil
			return
		}
		timerC = time.After(cfg.HedgeDelay)
	}
	armTimer()

	for active > 0 || next < len(candidates) {
		select {
		case <-ctx.Done():
			return Outcome{Candidates: candidates, Attempts: attempts, Duration: time.Since(startedAt)}, ctx.Err()
		case <-timerC:
			if next < len(candidates) && active < cfg.MaxParallel {
				start(candidates[next])
				next++
			}
			armTimer()
		case got := <-results:
			active--
			attempts = append(attempts, got.attempt)
			if got.err == nil {
				cancel()
				return Outcome{
					Result: got.result, Winner: got.attempt.Algorithm,
					Candidates: candidates, Attempts: attempts, Duration: time.Since(startedAt),
				}, nil
			}
			errs = append(errs, got.err)
			if next < len(candidates) && active < cfg.MaxParallel {
				start(candidates[next])
				next++
			}
			armTimer()
		}
	}
	return Outcome{Candidates: candidates, Attempts: attempts, Duration: time.Since(startedAt)}, errors.Join(errs...)
}

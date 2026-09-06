package maxsearch

import (
	"errors"
	"math"
	"sort"

	"github.com/lasder-ca/aegis-acbs/internal/search"
)

// SolverProfile is measured evidence for one exact solver. UpdateNS is the
// cost of making the solver usable after one metric/weight update. For static
// preprocessing such as CH this is normally a full rebuild; for CCH it can be
// only the customization phase.
type SolverProfile struct {
	Algorithm    search.Algorithm `json:"algorithm"`
	QueryNS      int64            `json:"queryNs"`
	PreprocessNS int64            `json:"preprocessNs"`
	UpdateNS     int64            `json:"updateNs"`
}

// WorkloadHorizon describes the expected useful lifetime of preprocessing.
type WorkloadHorizon struct {
	Queries       int64 `json:"queries"`
	MetricUpdates int64 `json:"metricUpdates"`
}

type SolverEstimate struct {
	Algorithm        search.Algorithm `json:"algorithm"`
	EstimatedTotalNS int64            `json:"estimatedTotalNs"`
	EstimatedMeanNS  int64            `json:"estimatedMeanNs"`
	QueryNS          int64            `json:"queryNs"`
	PreprocessNS     int64            `json:"preprocessNs"`
	UpdateNS         int64            `json:"updateNs"`
}

type SolverSelection struct {
	Selected search.Algorithm `json:"selected"`
	Horizon  WorkloadHorizon  `json:"horizon"`
	Ranking  []SolverEstimate `json:"ranking"`
}

// SelectSolver minimizes measured end-to-end cost over the supplied horizon.
// It deliberately has no region-specific thresholds: graph size influences the
// measured profile, while query volume and update frequency determine how much
// preprocessing can be amortized.
func SelectSolver(profiles []SolverProfile, horizon WorkloadHorizon) (SolverSelection, error) {
	if horizon.Queries <= 0 {
		return SolverSelection{}, errors.New("maxsearch: selector requires at least one query")
	}
	if horizon.MetricUpdates < 0 {
		return SolverSelection{}, errors.New("maxsearch: metric updates cannot be negative")
	}
	if len(profiles) == 0 {
		return SolverSelection{}, errors.New("maxsearch: selector has no solver profiles")
	}

	ranking := make([]SolverEstimate, 0, len(profiles))
	seen := make(map[search.Algorithm]struct{}, len(profiles))
	for _, p := range profiles {
		if p.Algorithm == "" || p.QueryNS < 0 || p.PreprocessNS < 0 || p.UpdateNS < 0 {
			return SolverSelection{}, errors.New("maxsearch: invalid solver profile")
		}
		if _, ok := seen[p.Algorithm]; ok {
			return SolverSelection{}, errors.New("maxsearch: duplicate solver profile")
		}
		seen[p.Algorithm] = struct{}{}

		total, ok := checkedCost(p, horizon)
		if !ok {
			return SolverSelection{}, errors.New("maxsearch: selector cost overflow")
		}
		ranking = append(ranking, SolverEstimate{
			Algorithm:        p.Algorithm,
			EstimatedTotalNS: total,
			EstimatedMeanNS:  total / horizon.Queries,
			QueryNS:          p.QueryNS,
			PreprocessNS:     p.PreprocessNS,
			UpdateNS:         p.UpdateNS,
		})
	}

	sort.SliceStable(ranking, func(i, j int) bool {
		if ranking[i].EstimatedTotalNS != ranking[j].EstimatedTotalNS {
			return ranking[i].EstimatedTotalNS < ranking[j].EstimatedTotalNS
		}
		if ranking[i].QueryNS != ranking[j].QueryNS {
			return ranking[i].QueryNS < ranking[j].QueryNS
		}
		return ranking[i].Algorithm < ranking[j].Algorithm
	})
	return SolverSelection{Selected: ranking[0].Algorithm, Horizon: horizon, Ranking: ranking}, nil
}

func checkedCost(p SolverProfile, h WorkloadHorizon) (int64, bool) {
	q, ok := checkedMul(p.QueryNS, h.Queries)
	if !ok {
		return 0, false
	}
	u, ok := checkedMul(p.UpdateNS, h.MetricUpdates)
	if !ok || p.PreprocessNS > math.MaxInt64-q {
		return 0, false
	}
	total := p.PreprocessNS + q
	if total > math.MaxInt64-u {
		return 0, false
	}
	return total + u, true
}

func checkedMul(a, b int64) (int64, bool) {
	if a == 0 || b == 0 {
		return 0, true
	}
	if a > math.MaxInt64/b {
		return 0, false
	}
	return a * b, true
}

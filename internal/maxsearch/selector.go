package maxsearch

import (
	"errors"
	"math"
	"sort"

	"github.com/lasder-ca/aegis-acbs/internal/search"
)

type SelectionStatistic string

type PreprocessState string

const (
	SelectionMean SelectionStatistic = "mean"
	SelectionP95  SelectionStatistic = "p95"
	SelectionP99  SelectionStatistic = "p99"

	PreprocessCold PreprocessState = "cold"
	PreprocessWarm PreprocessState = "warm"
)

// SolverProfile is measured evidence for one exact solver. UpdateNS is the
// cost of making the solver usable after one metric/weight update. For static
// preprocessing such as CH this is normally a full rebuild; for CCH it can be
// only the customization phase. WarmPreprocessNS is optional evidence for a
// trusted persisted index; if it is unavailable, warm selection falls back to
// the cold preprocessing cost instead of assuming a free startup.
type SolverProfile struct {
	Algorithm        search.Algorithm `json:"algorithm"`
	QueryNS          int64            `json:"queryNs"`
	QueryP95NS       int64            `json:"queryP95Ns,omitempty"`
	QueryP99NS       int64            `json:"queryP99Ns,omitempty"`
	PreprocessNS     int64            `json:"preprocessNs"`
	WarmPreprocessNS int64            `json:"warmPreprocessNs,omitempty"`
	UpdateNS         int64            `json:"updateNs"`
}

// WorkloadHorizon describes the expected useful lifetime of preprocessing.
// An empty PreprocessState is normalized to cold for backwards compatibility.
type WorkloadHorizon struct {
	Queries         int64           `json:"queries"`
	MetricUpdates   int64           `json:"metricUpdates"`
	PreprocessState PreprocessState `json:"preprocessState"`
}

type SolverEstimate struct {
	Algorithm        search.Algorithm `json:"algorithm"`
	EstimatedTotalNS int64            `json:"estimatedTotalNs"`
	EstimatedMeanNS  int64            `json:"estimatedMeanNs"`
	QueryNS          int64            `json:"queryNs"`
	PreprocessNS     int64            `json:"preprocessNs"`
	ColdPreprocessNS int64            `json:"coldPreprocessNs"`
	WarmPreprocessNS int64            `json:"warmPreprocessNs,omitempty"`
	UpdateNS         int64            `json:"updateNs"`
}

type SolverSelection struct {
	Selected  search.Algorithm   `json:"selected"`
	Statistic SelectionStatistic `json:"statistic"`
	Horizon   WorkloadHorizon    `json:"horizon"`
	Ranking   []SolverEstimate   `json:"ranking"`
}

func SelectSolver(profiles []SolverProfile, horizon WorkloadHorizon) (SolverSelection, error) {
	return SelectSolverByStatistic(profiles, horizon, SelectionMean)
}

func SelectSolverByStatistic(profiles []SolverProfile, horizon WorkloadHorizon, statistic SelectionStatistic) (SolverSelection, error) {
	if horizon.Queries <= 0 {
		return SolverSelection{}, errors.New("maxsearch: selector requires at least one query")
	}
	if horizon.MetricUpdates < 0 {
		return SolverSelection{}, errors.New("maxsearch: metric updates cannot be negative")
	}
	if horizon.PreprocessState == "" {
		horizon.PreprocessState = PreprocessCold
	}
	if horizon.PreprocessState != PreprocessCold && horizon.PreprocessState != PreprocessWarm {
		return SolverSelection{}, errors.New("maxsearch: preprocess state must be cold or warm")
	}
	if len(profiles) == 0 {
		return SolverSelection{}, errors.New("maxsearch: selector has no solver profiles")
	}
	if statistic != SelectionMean && statistic != SelectionP95 && statistic != SelectionP99 {
		return SolverSelection{}, errors.New("maxsearch: selector statistic must be mean, p95, or p99")
	}

	ranking := make([]SolverEstimate, 0, len(profiles))
	seen := make(map[search.Algorithm]struct{}, len(profiles))
	for _, p := range profiles {
		if p.Algorithm == "" || p.QueryNS < 0 || p.QueryP95NS < 0 || p.QueryP99NS < 0 || p.PreprocessNS < 0 || p.WarmPreprocessNS < 0 || p.UpdateNS < 0 {
			return SolverSelection{}, errors.New("maxsearch: invalid solver profile")
		}
		if _, ok := seen[p.Algorithm]; ok {
			return SolverSelection{}, errors.New("maxsearch: duplicate solver profile")
		}
		seen[p.Algorithm] = struct{}{}

		queryNS, err := profileQueryCost(p, statistic)
		if err != nil {
			return SolverSelection{}, err
		}
		preprocessNS := profilePreprocessCost(p, horizon.PreprocessState)
		total, ok := checkedCost(queryNS, preprocessNS, p.UpdateNS, horizon)
		if !ok {
			return SolverSelection{}, errors.New("maxsearch: selector cost overflow")
		}
		ranking = append(ranking, SolverEstimate{
			Algorithm:        p.Algorithm,
			EstimatedTotalNS: total,
			EstimatedMeanNS:  total / horizon.Queries,
			QueryNS:          queryNS,
			PreprocessNS:     preprocessNS,
			ColdPreprocessNS: p.PreprocessNS,
			WarmPreprocessNS: p.WarmPreprocessNS,
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
	return SolverSelection{Selected: ranking[0].Algorithm, Statistic: statistic, Horizon: horizon, Ranking: ranking}, nil
}

func profileQueryCost(p SolverProfile, statistic SelectionStatistic) (int64, error) {
	switch statistic {
	case SelectionMean:
		return p.QueryNS, nil
	case SelectionP95:
		if p.QueryP95NS <= 0 {
			return 0, errors.New("maxsearch: p95 selector requires p95 timing for every solver")
		}
		return p.QueryP95NS, nil
	case SelectionP99:
		if p.QueryP99NS <= 0 {
			return 0, errors.New("maxsearch: p99 selector requires p99 timing for every solver")
		}
		return p.QueryP99NS, nil
	default:
		return 0, errors.New("maxsearch: unknown selector statistic")
	}
}

func profilePreprocessCost(p SolverProfile, state PreprocessState) int64 {
	if state == PreprocessWarm && p.WarmPreprocessNS > 0 {
		return p.WarmPreprocessNS
	}
	return p.PreprocessNS
}

func checkedCost(queryNS, preprocessNS, updateNS int64, h WorkloadHorizon) (int64, bool) {
	q, ok := checkedMul(queryNS, h.Queries)
	if !ok {
		return 0, false
	}
	u, ok := checkedMul(updateNS, h.MetricUpdates)
	if !ok || preprocessNS > math.MaxInt64-q {
		return 0, false
	}
	total := preprocessNS + q
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

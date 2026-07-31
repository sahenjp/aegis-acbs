package search

import "math"

const (
	// Once both frontiers have been sampled, the allocation remains in the
	// simplex interior. The debt rounding below realizes this fractional policy
	// with a prefix discrepancy smaller than one chunk.
	acbsMinimumDirectionShare = 1.0 / 8.0

	acbsProofRateAlpha       = 0.25
	acbsProofRateClipLog     = 1.3862943611198906 // ln(4)
	acbsExplorationWeight    = 0.45
	acbsPriorityWeightBefore = 1.60
	acbsPriorityWeightAfter  = 0.80
	acbsSwitchPenalty        = 0.08
	acbsMirrorTemperature    = 0.80
)

type acbsProofRate struct {
	logMean float64
	samples uint64
}

func (r acbsProofRate) rate() float64 {
	if r.samples == 0 {
		return 0
	}
	return math.Exp(r.logMean)
}

func (r acbsProofRate) sampled() bool { return r.samples != 0 }

func (r *acbsProofRate) update(gain, work uint64) {
	// Laplace smoothing keeps zero-progress chunks finite. Estimating in log
	// space makes multiplicative changes natural, while clipping limits one
	// observation to a factor of 4^(1/4) in the stored rate.
	observation := math.Log((float64(gain) + 1.0) / (float64(work) + 1.0))
	if r.samples == 0 {
		r.logMean = observation
		r.samples = 1
		return
	}
	delta := observation - r.logMean
	if delta > acbsProofRateClipLog {
		delta = acbsProofRateClipLog
	} else if delta < -acbsProofRateClipLog {
		delta = -acbsProofRateClipLog
	}
	r.logMean += acbsProofRateAlpha * delta
	r.samples++
}

type acbsScheduleDecision struct {
	direction byte
	forwardP  float64
	entropy   float64
	certainty float64
}

type acbsEntropicScheduler struct {
	forward  acbsProofRate
	backward acbsProofRate
	debtF    float64
}

func (s *acbsEntropicScheduler) choose(frontF, frontB item, last byte, hasUpperBound bool) acbsScheduleDecision {
	// Bootstrap with one observation from each frontier. The interior policy
	// begins immediately afterwards.
	if !s.forward.sampled() {
		return acbsScheduleDecision{direction: 'F', forwardP: 1, entropy: math.Ln2}
	}
	if !s.backward.sampled() {
		return acbsScheduleDecision{direction: 'B', forwardP: 0, entropy: math.Ln2}
	}

	totalSamples := s.forward.samples + s.backward.samples
	exploreF := acbsExplorationWeight * math.Sqrt(math.Log(float64(totalSamples)+2.0)/float64(s.forward.samples+1))
	exploreB := acbsExplorationWeight * math.Sqrt(math.Log(float64(totalSamples)+2.0)/float64(s.backward.samples+1))

	priorityWeight := acbsPriorityWeightBefore
	if hasUpperBound {
		priorityWeight = acbsPriorityWeightAfter
	}
	pressureF := acbsNormalizedPriorityPressure(frontF.priority, frontB.priority)

	uF := s.forward.logMean + exploreF + priorityWeight*pressureF
	uB := s.backward.logMean + exploreB - priorityWeight*pressureF
	if last == 'F' {
		uB -= acbsSwitchPenalty
	} else if last == 'B' {
		uF -= acbsSwitchPenalty
	}

	// This logistic form is the closed-form Gibbs solution of
	// argmax_q <q,u> + tau H(q) over the two-action simplex.
	rawPF := acbsLogistic((uF - uB) / acbsMirrorTemperature)
	entropy := acbsBinaryEntropy(rawPF)
	certainty := 1.0 - entropy/math.Ln2
	pF := acbsMinimumDirectionShare + (1.0-2.0*acbsMinimumDirectionShare)*rawPF

	// Deterministic low-discrepancy rounding. For every prefix K,
	// |N_F(K) - sum_{k<=K} p_F(k)| < 1.
	s.debtF += pF
	direction := byte('B')
	if s.debtF >= 0.5 {
		direction = 'F'
		s.debtF -= 1.0
	}
	return acbsScheduleDecision{
		direction: direction,
		forwardP:  pF,
		entropy:   entropy,
		certainty: certainty,
	}
}

func (s *acbsEntropicScheduler) observe(direction byte, gain, work uint64) {
	if direction == 'F' {
		s.forward.update(gain, work)
		return
	}
	s.backward.update(gain, work)
}

func acbsNormalizedPriorityPressure(forward, backward uint64) float64 {
	if forward == backward {
		return 0
	}
	if forward < backward {
		return float64(backward-forward) / (float64(backward) + 1.0)
	}
	return -float64(forward-backward) / (float64(forward) + 1.0)
}

func acbsLogistic(x float64) float64 {
	if x >= 0 {
		if x > 60 {
			return 1
		}
		e := math.Exp(-x)
		return 1 / (1 + e)
	}
	if x < -60 {
		return 0
	}
	e := math.Exp(x)
	return e / (1 + e)
}

func acbsBinaryEntropy(p float64) float64 {
	if p <= 0 || p >= 1 {
		return 0
	}
	return -p*math.Log(p) - (1-p)*math.Log(1-p)
}

func acbsEntropicEdgeBudget(edgeCount int, certainty float64, hasUpperBound bool) int {
	base := acbsBaseEdgeBudget(edgeCount)
	if certainty < 0 {
		certainty = 0
	} else if certainty > 1 {
		certainty = 1
	}
	budget := int(math.Round(float64(base) * (1.0 + 3.0*certainty*certainty)))
	maxBudget := 4 * base
	if hasUpperBound {
		maxBudget = 2 * base
	}
	if budget < base {
		return base
	}
	if budget > maxBudget {
		return maxBudget
	}
	return budget
}

func acbsDirectionalGain(direction byte, beforeF, afterF, beforeB, afterB uint64) uint64 {
	if direction == 'F' {
		if afterF > beforeF {
			return afterF - beforeF
		}
		return 0
	}
	if afterB > beforeB {
		return afterB - beforeB
	}
	return 0
}

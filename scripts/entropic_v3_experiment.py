#!/usr/bin/env python3
from __future__ import annotations

import argparse
from pathlib import Path


def replace_once(text: str, old: str, new: str, label: str) -> str:
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{label}: expected one match, found {count}")
    return text.replace(old, new, 1)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--minimum-samples", type=int, required=True)
    parser.add_argument("--evidence-margin", type=float, required=True)
    parser.add_argument("--minimum-proof-gap", type=float, required=True)
    args = parser.parse_args()

    scheduler_path = Path("internal/search/acbs_entropic_scheduler.go")
    scheduler = scheduler_path.read_text()

    scheduler = replace_once(
        scheduler,
        "\tacbsMirrorTemperature    = 0.80\n",
        "\tacbsMirrorTemperature    = 0.80\n"
        f"\tacbsActivationMinimumSamples = uint64({args.minimum_samples})\n"
        f"\tacbsActivationEvidenceMargin = {args.evidence_margin:.12g}\n"
        f"\tacbsActivationMinimumProofGap = {args.minimum_proof_gap:.12g}\n",
        "activation constants",
    )

    scheduler = replace_once(
        scheduler,
        "func (r *acbsProofRate) seedFixedPoint(score uint64) {\n"
        "\t// Production ACBS stores proof efficiency as gain/work scaled by 1e6.\n"
        "\t// Reuse that sufficient statistic as the prior for the post-incumbent\n"
        "\t// mirror-descent phase without replaying pre-incumbent chunks.\n"
        "\tr.firstGain = score\n"
        "\tr.firstWork = 1_000_000\n"
        "\tr.samples = 1\n"
        "}\n",
        "func (r *acbsProofRate) seedFixedPoint(score, samples uint64) {\n"
        "\t// Production ACBS stores proof efficiency as gain/work scaled by 1e6.\n"
        "\t// Reuse that sufficient statistic as the prior for the post-incumbent\n"
        "\t// mirror-descent phase without replaying pre-incumbent chunks. Preserve\n"
        "\t// the production sample count so the UCB term does not re-explore a\n"
        "\t// direction that was already measured repeatedly.\n"
        "\tif samples <= 1 {\n"
        "\t\tr.firstGain = score\n"
        "\t\tr.firstWork = 1_000_000\n"
        "\t\tr.samples = 1\n"
        "\t\treturn\n"
        "\t}\n"
        "\tr.logMean = acbsLogSmoothedRate(score, 1_000_000)\n"
        "\tr.samples = samples\n"
        "}\n",
        "fixed-point seed",
    )

    scheduler = replace_once(
        scheduler,
        "func (s *acbsEntropicScheduler) seed(scoreF, scoreB uint64, sampledF, sampledB bool) {\n"
        "\tif !s.forward.sampled() && sampledF {\n"
        "\t\ts.forward.seedFixedPoint(scoreF)\n"
        "\t}\n"
        "\tif !s.backward.sampled() && sampledB {\n"
        "\t\ts.backward.seedFixedPoint(scoreB)\n"
        "\t}\n"
        "}\n",
        "func (s *acbsEntropicScheduler) seed(scoreF, scoreB uint64, sampledF, sampledB bool) {\n"
        "\ts.seedWithSamples(scoreF, scoreB, sampledF, sampledB, 1, 1)\n"
        "}\n\n"
        "func (s *acbsEntropicScheduler) seedWithSamples(\n"
        "\tscoreF, scoreB uint64, sampledF, sampledB bool, samplesF, samplesB uint64,\n"
        ") {\n"
        "\tif !s.forward.sampled() && sampledF {\n"
        "\t\ts.forward.seedFixedPoint(scoreF, samplesF)\n"
        "\t}\n"
        "\tif !s.backward.sampled() && sampledB {\n"
        "\t\ts.backward.seedFixedPoint(scoreB, samplesB)\n"
        "\t}\n"
        "}\n",
        "scheduler seed",
    )

    activation_code = r'''
type acbsActivationDecision struct {
	activate       bool
	direction      byte
	evidenceMargin float64
	proofGap       float64
}

func acbsEntropicActivation(
	scoreF, scoreB uint64,
	samplesF, samplesB uint64,
	frontF, frontB item,
	last, baseline byte,
	lowerBound, upperBound uint64,
) acbsActivationDecision {
	if scoreF == 0 || scoreB == 0 ||
		samplesF < acbsActivationMinimumSamples || samplesB < acbsActivationMinimumSamples ||
		upperBound == 0 || lowerBound >= upperBound {
		return acbsActivationDecision{}
	}

	proofGap := float64(upperBound-lowerBound) / (float64(upperBound) + 1.0)
	if proofGap < acbsActivationMinimumProofGap {
		return acbsActivationDecision{proofGap: proofGap}
	}

	// The fixed-point denominator is common to both production scores, so it
	// cancels in the log-rate difference. The coupled-bound pressure is a known
	// state term; only the empirical direction rates receive an uncertainty
	// allowance. This is an anytime UCB-style evidence margin, not a claim of a
	// calibrated statistical confidence interval.
	totalSamples := samplesF + samplesB
	rateGap := math.Log((float64(scoreF) + 1.0) / (float64(scoreB) + 1.0))
	pressure := acbsNormalizedPriorityPressure(frontF.priority, frontB.priority)
	signal := rateGap + 2.0*acbsPriorityWeightAfter*pressure
	uncertainty := acbsExplorationWeight * math.Sqrt(
		math.Log(float64(totalSamples)+2.0)*
			(1.0/float64(samplesF+1)+1.0/float64(samplesB+1)),
	)
	evidenceMargin := math.Abs(signal) - uncertainty
	if evidenceMargin < acbsActivationEvidenceMargin {
		return acbsActivationDecision{evidenceMargin: evidenceMargin, proofGap: proofGap}
	}

	// Intervene only when the complete mirror policy, including UCB and switch
	// cost, actually disagrees with production. Agreement has no allocation
	// benefit and cannot amortize the extra transcendental operations.
	var proposal acbsEntropicScheduler
	proposal.seedWithSamples(scoreF, scoreB, true, true, samplesF, samplesB)
	decision := proposal.choose(frontF, frontB, last, true)
	if decision.direction == baseline {
		return acbsActivationDecision{
			direction: decision.direction, evidenceMargin: evidenceMargin, proofGap: proofGap,
		}
	}
	return acbsActivationDecision{
		activate: true, direction: decision.direction,
		evidenceMargin: evidenceMargin, proofGap: proofGap,
	}
}

'''
    scheduler = replace_once(
        scheduler,
        "func acbsDirectionalGain(direction byte, beforeF, afterF, beforeB, afterB uint64) uint64 {\n",
        activation_code + "func acbsDirectionalGain(direction byte, beforeF, afterF, beforeB, afterB uint64) uint64 {\n",
        "activation helper insertion",
    )
    scheduler_path.write_text(scheduler)

    acbs_path = Path("internal/search/acbs.go")
    acbs = acbs_path.read_text()
    acbs = replace_once(
        acbs,
        'acbsEntropicSchedulerVersion = "entropic-proof-rate-v2"',
        'acbsEntropicSchedulerVersion = "entropic-proof-rate-v3-experiment"',
        "scheduler version",
    )
    acbs = replace_once(
        acbs,
        "\tvar scoreF, scoreB uint64\n"
        "\tvar sampledF, sampledB bool\n"
        "\tvar entropicScheduler acbsEntropicScheduler\n",
        "\tvar scoreF, scoreB uint64\n"
        "\tvar sampledF, sampledB bool\n"
        "\tvar productionSamplesF, productionSamplesB uint64\n"
        "\tvar entropicScheduler acbsEntropicScheduler\n"
        "\tentropicActive := false\n",
        "scheduler state",
    )

    old_decision = '''\t\tdecision := acbsScheduleDecision{}
\t\tdirection := byte(0)
\t\tif connectionGuardActive {
\t\t\tdirection = chooseACBSStaticDirection(g, frontF, frontB, qf.Len(), qb.Len())
\t\t} else if opts.entropic && bestReduced != inf {
\t\t\tentropicScheduler.seed(scoreF, scoreB, sampledF, sampledB)
\t\t\tdecision = entropicScheduler.choose(frontF, frontB, lastDirection, true)
\t\t\tdirection = decision.direction
\t\t} else if opts.adaptive {
\t\t\tdirection = chooseACBSDirection(
\t\t\t\tg, frontF, frontB, qf.Len(), qb.Len(), scoreF, scoreB,
\t\t\t\tsampledF, sampledB, lastDirection, consecutive,
\t\t\t)
\t\t} else {
\t\t\tdirection = chooseACBSStaticDirection(g, frontF, frontB, qf.Len(), qb.Len())
\t\t}
'''
    new_decision = '''\t\tdecision := acbsScheduleDecision{}
\t\tdirection := byte(0)
\t\tif connectionGuardActive {
\t\t\tdirection = chooseACBSStaticDirection(g, frontF, frontB, qf.Len(), qb.Len())
\t\t} else if opts.entropic && bestReduced != inf {
\t\t\tbaselineDirection := chooseACBSDirection(
\t\t\t\tg, frontF, frontB, qf.Len(), qb.Len(), scoreF, scoreB,
\t\t\t\tsampledF, sampledB, lastDirection, consecutive,
\t\t\t)
\t\t\tif !entropicActive {
\t\t\t\tactivation := acbsEntropicActivation(
\t\t\t\t\tscoreF, scoreB, productionSamplesF, productionSamplesB,
\t\t\t\t\tfrontF, frontB, lastDirection, baselineDirection,
\t\t\t\t\tlowerBound, bestReduced,
\t\t\t\t)
\t\t\t\tif activation.activate {
\t\t\t\t\tentropicScheduler.seedWithSamples(
\t\t\t\t\t\tscoreF, scoreB, sampledF, sampledB,
\t\t\t\t\t\tproductionSamplesF, productionSamplesB,
\t\t\t\t\t)
\t\t\t\t\tentropicActive = true
\t\t\t\t}
\t\t\t}
\t\t\tif entropicActive {
\t\t\t\tdecision = entropicScheduler.choose(frontF, frontB, lastDirection, true)
\t\t\t\tdirection = decision.direction
\t\t\t} else {
\t\t\t\tdirection = baselineDirection
\t\t\t}
\t\t} else if opts.adaptive {
\t\t\tdirection = chooseACBSDirection(
\t\t\t\tg, frontF, frontB, qf.Len(), qb.Len(), scoreF, scoreB,
\t\t\t\tsampledF, sampledB, lastDirection, consecutive,
\t\t\t)
\t\t} else {
\t\t\tdirection = chooseACBSStaticDirection(g, frontF, frontB, qf.Len(), qb.Len())
\t\t}
'''
    acbs = replace_once(acbs, old_decision, new_decision, "decision block")

    acbs = replace_once(
        acbs,
        "\t\tif opts.entropic && bestReduced != inf {\n"
        "\t\t\tbudget = acbsEntropicEdgeBudget(g.EdgeCount, decision.certainty, true)\n"
        "\t\t}\n",
        "\t\tif opts.entropic && entropicActive {\n"
        "\t\t\tbudget = acbsEntropicEdgeBudget(g.EdgeCount, decision.certainty, true)\n"
        "\t\t}\n",
        "budget gate",
    )

    old_observe = '''\t\twork := schedulerWork(
\t\t\tstats.Relaxed-beforeRelaxed,
\t\t\tstats.Expanded-beforeExpanded,
\t\t\tqf.Len()+qb.Len()-beforeQueues,
\t\t)
\t\tif opts.entropic && beforeBest != inf {
\t\t\tafterPriorityF, afterPriorityB := beforePriorityF, beforePriorityB
\t\t\tif okF {
\t\t\t\tafterPriorityF = frontF.priority
\t\t\t}
\t\t\tif okB {
\t\t\t\tafterPriorityB = frontB.priority
\t\t\t}
\t\t\tdirectionalGain := acbsDirectionalGain(
\t\t\t\tdirection, beforePriorityF, afterPriorityF, beforePriorityB, afterPriorityB,
\t\t\t)
\t\t\tqueueGrowth := qf.Len() - beforeQF
\t\t\tif direction == 'B' {
\t\t\t\tqueueGrowth = qb.Len() - beforeQB
\t\t\t}
\t\t\twork = schedulerWork(
\t\t\t\tstats.Relaxed-beforeRelaxed,
\t\t\t\tstats.Expanded-beforeExpanded,
\t\t\t\tqueueGrowth,
\t\t\t)
\t\t\tentropicScheduler.observe(direction, directionalGain, work)
\t\t} else if opts.adaptive {
\t\t\tinstant := efficiencyScore(gain, work)
\t\t\tif direction == 'F' {
\t\t\t\tscoreF = emaScore(scoreF, instant, sampledF)
\t\t\t\tsampledF = true
\t\t\t} else {
\t\t\t\tscoreB = emaScore(scoreB, instant, sampledB)
\t\t\t\tsampledB = true
\t\t\t}
\t\t}
'''
    new_observe = '''\t\twork := schedulerWork(
\t\t\tstats.Relaxed-beforeRelaxed,
\t\t\tstats.Expanded-beforeExpanded,
\t\t\tqf.Len()+qb.Len()-beforeQueues,
\t\t)
\t\tdirectionalGain := uint64(0)
\t\tdirectionalWork := work
\t\tif opts.entropic {
\t\t\tafterPriorityF, afterPriorityB := beforePriorityF, beforePriorityB
\t\t\tif okF {
\t\t\t\tafterPriorityF = frontF.priority
\t\t\t}
\t\t\tif okB {
\t\t\t\tafterPriorityB = frontB.priority
\t\t\t}
\t\t\tdirectionalGain = acbsDirectionalGain(
\t\t\t\tdirection, beforePriorityF, afterPriorityF, beforePriorityB, afterPriorityB,
\t\t\t)
\t\t\tqueueGrowth := qf.Len() - beforeQF
\t\t\tif direction == 'B' {
\t\t\t\tqueueGrowth = qb.Len() - beforeQB
\t\t\t}
\t\t\tdirectionalWork = schedulerWork(
\t\t\t\tstats.Relaxed-beforeRelaxed,
\t\t\t\tstats.Expanded-beforeExpanded,
\t\t\t\tqueueGrowth,
\t\t\t)
\t\t}
\t\tif opts.entropic && entropicActive && beforeBest != inf {
\t\t\twork = directionalWork
\t\t\tentropicScheduler.observe(direction, directionalGain, directionalWork)
\t\t} else if opts.adaptive {
\t\t\tinstant := efficiencyScore(gain, work)
\t\t\tif direction == 'F' {
\t\t\t\tscoreF = emaScore(scoreF, instant, sampledF)
\t\t\t\tsampledF = true
\t\t\t\tif opts.entropic {
\t\t\t\t\tproductionSamplesF++
\t\t\t\t}
\t\t\t} else {
\t\t\t\tscoreB = emaScore(scoreB, instant, sampledB)
\t\t\t\tsampledB = true
\t\t\t\tif opts.entropic {
\t\t\t\t\tproductionSamplesB++
\t\t\t\t}
\t\t\t}
\t\t}
'''
    acbs = replace_once(acbs, old_observe, new_observe, "observation block")
    acbs_path.write_text(acbs)

    Path("internal/search/acbs_entropic_activation_test.go").write_text(
        '''package search

import "testing"

func TestACBSEntropicActivationNeedsEvidence(t *testing.T) {
	d := acbsEntropicActivation(
		4_000_000, 1_000_000,
		acbsActivationMinimumSamples-1, acbsActivationMinimumSamples,
		item{priority: 100}, item{priority: 100},
		'B', 'B', 100, 200,
	)
	if d.activate {
		t.Fatal("activation ignored minimum evidence")
	}
}

func TestACBSEntropicActivationRejectsSmallProofGap(t *testing.T) {
	d := acbsEntropicActivation(
		10_000_000, 1_000_000,
		100, 100,
		item{priority: 100}, item{priority: 100},
		'B', 'B', 9999, 10000,
	)
	if d.activate {
		t.Fatal("activation ignored remaining proof gap")
	}
}

func TestACBSEntropicActivationRequiresPolicyDisagreement(t *testing.T) {
	d := acbsEntropicActivation(
		10_000_000, 1_000_000,
		100, 100,
		item{priority: 100}, item{priority: 100},
		'F', 'F', 100, 1000,
	)
	if d.activate {
		t.Fatal("activation intervened despite production agreement")
	}
}

func TestACBSEntropicActivationAcceptsStrongDisagreement(t *testing.T) {
	d := acbsEntropicActivation(
		10_000_000, 1_000_000,
		100, 100,
		item{priority: 100}, item{priority: 100},
		'F', 'B', 100, 1000,
	)
	if !d.activate || d.direction != 'F' {
		t.Fatalf("activation = %+v", d)
	}
}
'''
    )


if __name__ == "__main__":
    main()

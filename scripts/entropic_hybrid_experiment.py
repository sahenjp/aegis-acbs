#!/usr/bin/env python3
from __future__ import annotations

import argparse
from pathlib import Path


def replace_once(source: str, old: str, new: str, label: str) -> str:
    if old not in source:
        raise SystemExit(f"missing {label} target")
    return source.replace(old, new, 1)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--minimum-share", default="0.125")
    parser.add_argument("--temperature", default="0.80")
    parser.add_argument("--switch-penalty", default="0.08")
    parser.add_argument("--priority-after", default="0.80")
    parser.add_argument("--linear-budget", action="store_true")
    args = parser.parse_args()

    scheduler_path = Path("internal/search/acbs_entropic_scheduler.go")
    scheduler = scheduler_path.read_text()
    replacements = (
        ("acbsMinimumDirectionShare = 1.0 / 8.0", f"acbsMinimumDirectionShare = {args.minimum_share}", "minimum share"),
        ("acbsPriorityWeightAfter  = 0.80", f"acbsPriorityWeightAfter  = {args.priority_after}", "post-bound priority"),
        ("acbsSwitchPenalty        = 0.08", f"acbsSwitchPenalty        = {args.switch_penalty}", "switch penalty"),
        ("acbsMirrorTemperature    = 0.80", f"acbsMirrorTemperature    = {args.temperature}", "temperature"),
    )
    for old, new, label in replacements:
        scheduler = replace_once(scheduler, old, new, label)
    if args.linear_budget:
        scheduler = replace_once(
            scheduler,
            "1.0 + 3.0*certainty*certainty",
            "1.0 + 3.0*certainty",
            "linear budget",
        )
    scheduler_path.write_text(scheduler)

    acbs_path = Path("internal/search/acbs.go")
    acbs = acbs_path.read_text()
    acbs = replace_once(
        acbs,
        "} else if opts.entropic {\n\t\t\tdecision = entropicScheduler.choose(frontF, frontB, lastDirection, bestReduced != inf)",
        "} else if opts.entropic && bestReduced != inf {\n\t\t\tdecision = entropicScheduler.choose(frontF, frontB, lastDirection, true)",
        "direction phase",
    )
    acbs = replace_once(
        acbs,
        "if opts.entropic {\n\t\t\tbudget = acbsEntropicEdgeBudget(g.EdgeCount, decision.certainty, bestReduced != inf)\n\t\t}",
        "if opts.entropic && bestReduced != inf {\n\t\t\tbudget = acbsEntropicEdgeBudget(g.EdgeCount, decision.certainty, true)\n\t\t}",
        "budget phase",
    )
    old_observation = """\t\tif opts.entropic {
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
\t\t}"""
    new_observation = """\t\tif opts.entropic {
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
\t\t}
\t\tif opts.adaptive && (!opts.entropic || beforeBest == inf) {
\t\t\tinstant := efficiencyScore(gain, work)
\t\t\tif direction == 'F' {
\t\t\t\tscoreF = emaScore(scoreF, instant, sampledF)
\t\t\t\tsampledF = true
\t\t\t} else {
\t\t\t\tscoreB = emaScore(scoreB, instant, sampledB)
\t\t\t\tsampledB = true
\t\t\t}
\t\t}"""
    acbs = replace_once(acbs, old_observation, new_observation, "observation phase")
    acbs_path.write_text(acbs)


if __name__ == "__main__":
    main()

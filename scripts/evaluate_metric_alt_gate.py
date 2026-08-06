#!/usr/bin/env python3
"""Aggregate sharded exact-shortest-path benchmark reports.

This script evaluates the Tokyo/large-graph portion of the promotion gate. It is
intentionally insufficient to approve production promotion by itself: the full
gate still requires at least two additional real road graphs and a reproducible
preprocessed baseline class.
"""

from __future__ import annotations

import argparse
import json
import math
import re
import sys
from collections import defaultdict
from pathlib import Path
from statistics import median
from typing import Any

ARTIFACT_RE = re.compile(
    r"metric-alt-tokyo-r(?P<repeat>\d+)-(?P<metric>distance|time)-"
    r"s(?P<seed>\d+)-p(?P<shard>\d+)"
)


def percentile(values: list[int], quantile: float) -> float:
    if not values:
        raise ValueError("cannot compute a percentile of an empty sample")
    ordered = sorted(values)
    position = (len(ordered) - 1) * quantile
    lower = math.floor(position)
    upper = math.ceil(position)
    if lower == upper:
        return float(ordered[lower])
    fraction = position - lower
    return ordered[lower] * (1.0 - fraction) + ordered[upper] * fraction


def geometric_mean_speedup(baseline: list[int], candidate: list[int]) -> float:
    if len(baseline) != len(candidate) or not baseline:
        raise ValueError("paired runtime samples are required")
    return math.exp(
        sum(math.log(base / cand) for base, cand in zip(baseline, candidate, strict=True))
        / len(baseline)
    )


def discover_reports(root: Path) -> list[tuple[dict[str, int | str], Path]]:
    discovered: list[tuple[dict[str, int | str], Path]] = []
    for report in root.rglob("report.json"):
        match = None
        for parent in report.parents:
            match = ARTIFACT_RE.search(parent.name)
            if match:
                break
        if match is None:
            hardware = report.parent / "hardware.txt"
            if hardware.exists():
                fields: dict[str, str] = {}
                for line in hardware.read_text(encoding="utf-8", errors="replace").splitlines():
                    if "=" in line:
                        key, value = line.split("=", 1)
                        fields[key] = value
                required = {"repeat", "metric", "base_seed", "shard"}
                if required <= fields.keys():
                    discovered.append(
                        (
                            {
                                "repeat": int(fields["repeat"]),
                                "metric": fields["metric"],
                                "seed": int(fields["base_seed"]),
                                "shard": int(fields["shard"]),
                            },
                            report,
                        )
                    )
            continue
        discovered.append(
            (
                {
                    "repeat": int(match.group("repeat")),
                    "metric": match.group("metric"),
                    "seed": int(match.group("seed")),
                    "shard": int(match.group("shard")),
                },
                report,
            )
        )
    return discovered


def evaluate(args: argparse.Namespace) -> tuple[dict[str, Any], bool]:
    reports = discover_reports(args.root)
    grouped: dict[tuple[int, str], list[tuple[dict[str, int | str], Path]]] = defaultdict(list)
    for metadata, path in reports:
        grouped[(int(metadata["repeat"]), str(metadata["metric"]))].append((metadata, path))

    expected_groups = {
        (repeat, metric)
        for repeat in range(1, args.required_repeats + 1)
        for metric in args.metrics
    }
    result: dict[str, Any] = {
        "gate": "metric-alt-large-graph-subgate-v1",
        "candidate": args.candidate,
        "baseline": args.baseline,
        "requirements": {
            "requiredRepeats": args.required_repeats,
            "requiredSeeds": args.required_seeds,
            "requiredQueriesPerGroup": args.required_queries,
            "minimumGeomeanSpeedup": args.minimum_speedup,
            "maximumTailRegression": args.maximum_tail_regression,
            "metrics": args.metrics,
        },
        "scopeWarning": (
            "Passing this report is not sufficient for production promotion. "
            "The complete gate also requires at least two additional real road graphs "
            "and a reproducible CH/MLD/landmark-equivalent preprocessed comparator."
        ),
        "groups": [],
    }

    overall_pass = set(grouped) == expected_groups
    for key in sorted(expected_groups):
        repeat, metric = key
        entries = grouped.get(key, [])
        seeds = {int(metadata["seed"]) for metadata, _ in entries}
        shards = {(int(metadata["seed"]), int(metadata["shard"])) for metadata, _ in entries}
        baseline_ns: list[int] = []
        candidate_ns: list[int] = []
        correctness_failures = 0
        optimality_gap_failures = 0
        graph_fingerprints: set[tuple[int, int, str]] = set()
        preprocessing: list[dict[str, Any]] = []

        for metadata, path in entries:
            document = json.loads(path.read_text(encoding="utf-8"))
            graph_fingerprints.add((int(document["nodes"]), int(document["edges"]), str(document["metric"])))
            by_query: dict[int, dict[str, dict[str, Any]]] = defaultdict(dict)
            for sample in document.get("samples", []):
                stats = sample["stats"]
                by_query[int(sample["queryIndex"])][str(stats["algorithm"])] = stats | {
                    "correct": bool(sample.get("correct", False))
                }
            for algorithms in by_query.values():
                if args.baseline not in algorithms or args.candidate not in algorithms:
                    correctness_failures += 1
                    continue
                base = algorithms[args.baseline]
                cand = algorithms[args.candidate]
                if not cand["correct"] or cand["distance"] != base["distance"] or cand["reachable"] != base["reachable"]:
                    correctness_failures += 1
                upper = cand.get("upperBound")
                lower = cand.get("lowerBound")
                if cand.get("reachable") and upper is not None and lower is not None and upper != lower:
                    optimality_gap_failures += 1
                baseline_ns.append(int(base["durationNs"]))
                candidate_ns.append(int(cand["durationNs"]))

            for line in (path.parent / "preprocess-and-resource.log").read_text(
                encoding="utf-8", errors="replace"
            ).splitlines() if (path.parent / "preprocess-and-resource.log").exists() else []:
                if "metric ALT preprocessing:" in line:
                    preprocessing.append({"line": line.strip(), "artifact": str(path.parent)})

        group_pass = True
        reasons: list[str] = []
        if len(seeds) < args.required_seeds:
            group_pass = False
            reasons.append(f"only {len(seeds)} distinct base seeds")
        if len(baseline_ns) < args.required_queries:
            group_pass = False
            reasons.append(f"only {len(baseline_ns)} paired queries")
        if correctness_failures:
            group_pass = False
            reasons.append(f"{correctness_failures} distance/reachability mismatches")
        if optimality_gap_failures:
            group_pass = False
            reasons.append(f"{optimality_gap_failures} non-zero optimality gaps")
        if len(graph_fingerprints) != 1:
            group_pass = False
            reasons.append("inconsistent graph metadata across shards")

        statistics: dict[str, Any] = {}
        if baseline_ns and len(baseline_ns) == len(candidate_ns):
            speedup = geometric_mean_speedup(baseline_ns, candidate_ns)
            base_p95 = percentile(baseline_ns, 0.95)
            cand_p95 = percentile(candidate_ns, 0.95)
            base_p99 = percentile(baseline_ns, 0.99)
            cand_p99 = percentile(candidate_ns, 0.99)
            p95_regression = cand_p95 / base_p95 - 1.0
            p99_regression = cand_p99 / base_p99 - 1.0
            statistics = {
                "pairedQueries": len(baseline_ns),
                "geomeanSpeedup": speedup,
                "baselineMedianNs": median(baseline_ns),
                "candidateMedianNs": median(candidate_ns),
                "baselineP95Ns": base_p95,
                "candidateP95Ns": cand_p95,
                "p95Regression": p95_regression,
                "baselineP99Ns": base_p99,
                "candidateP99Ns": cand_p99,
                "p99Regression": p99_regression,
            }
            if speedup < args.minimum_speedup:
                group_pass = False
                reasons.append(f"geomean speedup {speedup:.6f} below {args.minimum_speedup:.6f}")
            if p95_regression > args.maximum_tail_regression:
                group_pass = False
                reasons.append(f"p95 regression {p95_regression:.2%} exceeds {args.maximum_tail_regression:.2%}")
            if p99_regression > args.maximum_tail_regression:
                group_pass = False
                reasons.append(f"p99 regression {p99_regression:.2%} exceeds {args.maximum_tail_regression:.2%}")
        else:
            group_pass = False
            reasons.append("paired runtime samples are missing")

        overall_pass = overall_pass and group_pass
        result["groups"].append(
            {
                "repeat": repeat,
                "metric": metric,
                "pass": group_pass,
                "reasons": reasons,
                "baseSeeds": sorted(seeds),
                "shardCount": len(shards),
                "graphFingerprints": [list(item) for item in sorted(graph_fingerprints)],
                "correctnessFailures": correctness_failures,
                "optimalityGapFailures": optimality_gap_failures,
                "statistics": statistics,
                "preprocessingEvidence": preprocessing[:1],
            }
        )

    missing = sorted(expected_groups - set(grouped))
    unexpected = sorted(set(grouped) - expected_groups)
    result["missingGroups"] = [{"repeat": r, "metric": m} for r, m in missing]
    result["unexpectedGroups"] = [{"repeat": r, "metric": m} for r, m in unexpected]
    result["pass"] = overall_pass
    return result, overall_pass


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("root", type=Path)
    parser.add_argument("--output", type=Path, default=Path("metric-alt-tokyo-gate.json"))
    parser.add_argument("--candidate", default="aegis-alt")
    parser.add_argument("--baseline", default="aegis")
    parser.add_argument("--metrics", nargs="+", default=["distance", "time"])
    parser.add_argument("--required-repeats", type=int, default=2)
    parser.add_argument("--required-seeds", type=int, default=3)
    parser.add_argument("--required-queries", type=int, default=10_000)
    parser.add_argument("--minimum-speedup", type=float, default=1.05)
    parser.add_argument("--maximum-tail-regression", type=float, default=0.02)
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    result, passed = evaluate(args)
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(result, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(json.dumps(result, indent=2, sort_keys=True))
    return 0 if passed else 1


if __name__ == "__main__":
    sys.exit(main())

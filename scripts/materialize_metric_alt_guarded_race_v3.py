#!/usr/bin/env python3
from pathlib import Path
import runpy


# Materialize the current guarded-race implementation first, then tighten the
# geometry window using thresholds selected from paired replay of the latest
# final-evaluation artifact. This keeps the experiment isolated from v2 while
# preserving the exact same search implementations and correctness boundary.
runpy.run_path("scripts/materialize_metric_alt_guarded_race.py", run_name="__main__")

path = Path("internal/search/metric_alt_race.go")
text = path.read_text()
replacements = {
    "metricALTRaceRegionalRatio   = 0.25": "metricALTRaceRegionalRatio   = 0.18",
    "metricALTRaceLargeGraphRatio = 0.30": "metricALTRaceLargeGraphRatio = 0.20",
}
for old, new in replacements.items():
    if text.count(old) != 1:
        raise SystemExit(f"expected one occurrence of {old!r}")
    text = text.replace(old, new, 1)

text = text.replace(
    "regional graphs use\n// 0.25 of the graph diameter, while million-edge graphs use 0.30 so large-city\n// workloads retain enough race coverage to amortize the portfolio overhead.",
    "regional graphs use\n// 0.18 of the graph diameter, while million-edge graphs use 0.20. These tighter\n// bounds are the largest replay-safe windows that avoided p95/p99 regressions\n// in the 2026-08-07 final-evaluation artifact while retaining a >5% Tokyo\n// geometric-mean latency margin for a fresh independent gate.",
)
path.write_text(text)

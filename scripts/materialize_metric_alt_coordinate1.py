#!/usr/bin/env python3
from pathlib import Path


def replace_once(text: str, old: str, new: str, label: str) -> str:
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{label}: expected one match, found {count}")
    return text.replace(old, new, 1)


metric_path = Path("internal/search/metric_alt.go")
metric = metric_path.read_text()
metric = replace_once(
    metric,
    '''func absDiffUint64(a, b uint64) uint64 {
''',
    '''// coordinatePhi turns the selected landmark distance into a balanced
// query potential. Since v -> d_U(l,v) is 1-Lipschitz on every original edge,
// multiplying an oriented difference by two is exactly within ACBS's
// |phi(v)-phi(u)| <= 2*c(u,v) feasibility budget.
func (query metricALTQuery) coordinatePhi(v int) int64 {
\tif query.activeCount != 1 || query.scalarSource == inf || query.scalarTarget == inf || query.scalarSource == query.scalarTarget {
\t\treturn 0
\t}
\tlandmark := int(query.scalarLandmark)
\tvalue := query.index.distances[v*query.index.stride+landmark]
\tif value == inf {
\t\treturn 0
\t}

\tconst phiLimit = uint64((1 << 61) - 1)
\tconst deltaLimit = phiLimit / 2
\tvar magnitude uint64
\tpositiveDelta := value >= query.scalarSource
\tif positiveDelta {
\t\tmagnitude = value - query.scalarSource
\t} else {
\t\tmagnitude = query.scalarSource - value
\t}
\tif magnitude > deltaLimit {
\t\tmagnitude = deltaLimit
\t}
\tphi := int64(magnitude * 2)
\tif !positiveDelta {
\t\tphi = -phi
\t}
\t// Orient the coordinate so the target always has lower potential than the
\t// source. Adding a constant is irrelevant to reduced costs, so source=0.
\tif query.scalarSource < query.scalarTarget {
\t\tphi = -phi
\t}
\treturn phi
}

func absDiffUint64(a, b uint64) uint64 {
''',
    "oriented coordinate potential",
)
metric_path.write_text(metric)

acbs_path = Path("internal/search/acbs.go")
acbs = acbs_path.read_text()
acbs = replace_once(
    acbs,
    '''func (p acbsPotential) phi(g *graph.Graph, v int) int64 {
\tif !p.enabled {
\t\treturn 0
\t}
\tx, y, z := g.UnitVector(v)
''',
    '''func (p acbsPotential) phi(g *graph.Graph, v int) int64 {
\tif !p.enabled {
\t\treturn 0
\t}
\tif p.metricALT.activeCount == 1 {
\t\treturn p.metricALT.coordinatePhi(v)
\t}
\tx, y, z := g.UnitVector(v)
''',
    "coordinate potential dispatch",
)
acbs_path.write_text(acbs)

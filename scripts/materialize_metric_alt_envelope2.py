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
    '''func orientedMetricCoordinate(value, source, target uint64) int64 {
\tif value == inf || source == inf || target == inf || source == target {
\t\treturn 0
\t}
\tconst limit = uint64((1 << 60) - 1)
\tvar coordinate int64
\tif value >= source {
\t\tdelta := value - source
\t\tif delta > limit {
\t\t\tdelta = limit
\t\t}
\t\tcoordinate = int64(delta)
\t} else {
\t\tdelta := source - value
\t\tif delta > limit {
\t\t\tdelta = limit
\t\t}
\t\tcoordinate = -int64(delta)
\t}
\t// Orient every landmark coordinate so target is on the negative side.
\tif source < target {
\t\tcoordinate = -coordinate
\t}
\treturn coordinate
}

// coordinateEnvelopePhi is twice the lower envelope of the two query-selected
// oriented landmark coordinates. Each coordinate is 1-Lipschitz on every
// original directed edge; pointwise min preserves that property, so doubling
// uses exactly ACBS's 2*c balanced-potential feasibility budget.
func (query metricALTQuery) coordinateEnvelopePhi(v int) int64 {
\tif query.activeCount != 2 {
\t\treturn 0
\t}
\tbase := v * query.index.stride
\tvalue0 := query.index.distances[base+int(query.scalarLandmark0)]
\tvalue1 := query.index.distances[base+int(query.scalarLandmark1)]
\tc0 := orientedMetricCoordinate(value0, query.scalarSource0, query.scalarTarget0)
\tc1 := orientedMetricCoordinate(value1, query.scalarSource1, query.scalarTarget1)
\tif c1 < c0 {
\t\tc0 = c1
\t}
\treturn c0 * 2
}

func absDiffUint64(a, b uint64) uint64 {
''',
    "coordinate envelope helper",
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
\tif p.metricALT.activeCount == 2 {
\t\treturn p.metricALT.coordinateEnvelopePhi(v)
\t}
\tx, y, z := g.UnitVector(v)
''',
    "coordinate envelope potential dispatch",
)
acbs_path.write_text(acbs)

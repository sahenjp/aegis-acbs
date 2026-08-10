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
    '''func absDiffUint64(a, b uint64) uint64 {\n''',
    '''// coordinate2Phi sums two oriented landmark coordinates. Each oriented\n// coordinate v -> +/- (d_U(l,v)-d_U(l,s)) is 1-Lipschitz on every original\n// directed edge. Their sum is therefore 2-Lipschitz, exactly matching ACBS's\n// balanced-potential feasibility budget |phi(v)-phi(u)| <= 2*c(u,v).\nfunc (query metricALTQuery) coordinate2Phi(v int) int64 {\n\tif query.activeCount != 2 {\n\t\treturn 0\n\t}\n\tbase := v * query.index.stride\n\tvalue0 := query.index.distances[base+int(query.scalarLandmark0)]\n\tvalue1 := query.index.distances[base+int(query.scalarLandmark1)]\n\tconst componentLimit = uint64((1 << 60) - 1)\n\tcomponent := func(value, source, target uint64) int64 {\n\t\tif value == inf || source == inf || target == inf || source == target {\n\t\t\treturn 0\n\t\t}\n\t\tvar magnitude uint64\n\t\tpositive := value >= source\n\t\tif positive {\n\t\t\tmagnitude = value - source\n\t\t} else {\n\t\t\tmagnitude = source - value\n\t\t}\n\t\tif magnitude > componentLimit {\n\t\t\tmagnitude = componentLimit\n\t\t}\n\t\tpart := int64(magnitude)\n\t\tif !positive {\n\t\t\tpart = -part\n\t\t}\n\t\t// Orient each coordinate so the target contribution is non-positive.\n\t\tif source < target {\n\t\t\tpart = -part\n\t\t}\n\t\treturn part\n\t}\n\treturn component(value0, query.scalarSource0, query.scalarTarget0) +\n\t\tcomponent(value1, query.scalarSource1, query.scalarTarget1)\n}\n\nfunc absDiffUint64(a, b uint64) uint64 {\n''',
    "two-landmark coordinate potential",
)
metric_path.write_text(metric)

acbs_path = Path("internal/search/acbs.go")
acbs = acbs_path.read_text()
acbs = replace_once(
    acbs,
    '''func (p acbsPotential) phi(g *graph.Graph, v int) int64 {\n\tif !p.enabled {\n\t\treturn 0\n\t}\n\tx, y, z := g.UnitVector(v)\n''',
    '''func (p acbsPotential) phi(g *graph.Graph, v int) int64 {\n\tif !p.enabled {\n\t\treturn 0\n\t}\n\tif p.metricALT.activeCount == 2 {\n\t\treturn p.metricALT.coordinate2Phi(v)\n\t}\n\tx, y, z := g.UnitVector(v)\n''',
    "coordinate2 potential dispatch",
)
acbs_path.write_text(acbs)

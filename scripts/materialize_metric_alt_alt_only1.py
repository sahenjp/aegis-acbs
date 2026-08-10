#!/usr/bin/env python3
from pathlib import Path


# Apply after the scalar1 materializer. For one active metric landmark, the
# landmark forward/backward bounds are independently admissible and 1-Lipschitz
# on every original directed edge. Their difference is therefore a valid
# balanced 2-Lipschitz potential, so the chord bound is not required for
# correctness in this experimental path.
def replace_once(text: str, old: str, new: str, label: str) -> str:
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{label}: expected one match, found {count}")
    return text.replace(old, new, 1)


path = Path("internal/search/acbs.go")
text = path.read_text()

text = replace_once(
    text,
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
\t\tforward, backward := p.metricALT.bounds(v)
\t\treturn signedDifference(forward, backward)
\t}
\tx, y, z := g.UnitVector(v)
''',
    "ALT-only scalar phi",
)

text = replace_once(
    text,
    '''func (p acbsPotential) bounds(g *graph.Graph, v int) (forward, backward uint64) {
\tif !p.enabled {
\t\treturn 0, 0
\t}
\tx, y, z := g.UnitVector(v)
''',
    '''func (p acbsPotential) bounds(g *graph.Graph, v int) (forward, backward uint64) {
\tif !p.enabled {
\t\treturn 0, 0
\t}
\tif p.metricALT.activeCount == 1 {
\t\treturn p.metricALT.bounds(v)
\t}
\tx, y, z := g.UnitVector(v)
''',
    "ALT-only scalar bounds",
)

path.write_text(text)

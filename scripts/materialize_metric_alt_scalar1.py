#!/usr/bin/env python3
from pathlib import Path
import runpy


# Start from the exact node-major top1 candidate. This script changes only the
# one-active-landmark evaluation path; ranking, admissibility and ACBS stopping
# conditions remain identical.
runpy.run_path("scripts/materialize_metric_alt_top1.py", run_name="__main__")


def replace_once(text: str, old: str, new: str, label: str) -> str:
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{label}: expected one match, found {count}")
    return text.replace(old, new, 1)


path = Path("internal/search/metric_alt.go")
text = path.read_text()

text = replace_once(
    text,
    '''type metricALTQuery struct {
\tindex       *metricALTIndex
\tsource      int
\ttarget      int
\tactiveMask  uint16
\tactiveCount uint8
}
''',
    '''type metricALTQuery struct {
\tindex          *metricALTIndex
\tsource         int
\ttarget         int
\tactiveMask     uint16
\tactiveCount    uint8
\tscalarLandmark uint8
\tscalarSource   uint64
\tscalarTarget   uint64
}
''',
    "metricALTQuery scalar cache",
)

text = replace_once(
    text,
    '''\tquery := metricALTQuery{
\t\tindex:       index,
\t\tsource:      source,
\t\ttarget:      target,
\t\tactiveCount: uint8(limit),
\t}
\tfor i := 0; i < limit; i++ {
''',
    '''\tquery := metricALTQuery{
\t\tindex:       index,
\t\tsource:      source,
\t\ttarget:      target,
\t\tactiveCount: uint8(limit),
\t}
\tif limit == 1 {
\t\tlandmark := ranked[0].index
\t\tquery.scalarLandmark = landmark
\t\tquery.scalarSource = sourceRow[landmark]
\t\tquery.scalarTarget = targetRow[landmark]
\t}
\tfor i := 0; i < limit; i++ {
''',
    "query-local scalar endpoint cache",
)

text = replace_once(
    text,
    '''func (query metricALTQuery) bounds(v int) (forward, backward uint64) {
\trow := query.index.row(v)
''',
    '''func (query metricALTQuery) bounds(v int) (forward, backward uint64) {
\tif query.activeCount == 1 {
\t\tlandmark := int(query.scalarLandmark)
\t\tvalue := query.index.distances[v*query.index.stride+landmark]
\t\tif value == inf {
\t\t\treturn 0, 0
\t\t}
\t\tif query.scalarTarget != inf {
\t\t\tforward = absDiffUint64(query.scalarTarget, value)
\t\t}
\t\tif query.scalarSource != inf {
\t\t\tbackward = absDiffUint64(query.scalarSource, value)
\t\t}
\t\treturn forward, backward
\t}

\trow := query.index.row(v)
''',
    "scalar one-landmark bounds",
)

path.write_text(text)

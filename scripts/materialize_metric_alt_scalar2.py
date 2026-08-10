#!/usr/bin/env python3
from pathlib import Path


# Apply after materialize_metric_alt_top1.py. The selected two-landmark set is
# unchanged; only the repeated generic mask/source-row work is unrolled.
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
\tindex           *metricALTIndex
\tsource          int
\ttarget          int
\tactiveMask      uint16
\tactiveCount     uint8
\tscalarLandmark0 uint8
\tscalarLandmark1 uint8
\tscalarSource0   uint64
\tscalarSource1   uint64
\tscalarTarget0   uint64
\tscalarTarget1   uint64
}
''',
    "metricALTQuery scalar2 cache",
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
\tif limit == 2 {
\t\tlandmark0 := ranked[0].index
\t\tlandmark1 := ranked[1].index
\t\tquery.scalarLandmark0 = landmark0
\t\tquery.scalarLandmark1 = landmark1
\t\tquery.scalarSource0 = sourceRow[landmark0]
\t\tquery.scalarSource1 = sourceRow[landmark1]
\t\tquery.scalarTarget0 = targetRow[landmark0]
\t\tquery.scalarTarget1 = targetRow[landmark1]
\t}
\tfor i := 0; i < limit; i++ {
''',
    "query-local scalar2 endpoint cache",
)

text = replace_once(
    text,
    '''func (query metricALTQuery) bounds(v int) (forward, backward uint64) {
\trow := query.index.row(v)
''',
    '''func (query metricALTQuery) bounds(v int) (forward, backward uint64) {
\tif query.activeCount == 2 {
\t\tbase := v * query.index.stride
\t\tvalue0 := query.index.distances[base+int(query.scalarLandmark0)]
\t\tvalue1 := query.index.distances[base+int(query.scalarLandmark1)]

\t\tif value0 != inf {
\t\t\tif query.scalarTarget0 != inf {
\t\t\t\tforward = absDiffUint64(query.scalarTarget0, value0)
\t\t\t}
\t\t\tif query.scalarSource0 != inf {
\t\t\t\tbackward = absDiffUint64(query.scalarSource0, value0)
\t\t\t}
\t\t}
\t\tif value1 != inf {
\t\t\tif query.scalarTarget1 != inf {
\t\t\t\tif bound := absDiffUint64(query.scalarTarget1, value1); bound > forward {
\t\t\t\t\tforward = bound
\t\t\t\t}
\t\t\t}
\t\t\tif query.scalarSource1 != inf {
\t\t\t\tif bound := absDiffUint64(query.scalarSource1, value1); bound > backward {
\t\t\t\t\tbackward = bound
\t\t\t\t}
\t\t\t}
\t\t}
\t\treturn forward, backward
\t}

\trow := query.index.row(v)
''',
    "scalar two-landmark bounds",
)

path.write_text(text)

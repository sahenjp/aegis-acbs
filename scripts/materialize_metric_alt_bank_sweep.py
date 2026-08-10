#!/usr/bin/env python3
from pathlib import Path


def replace_once(text: str, old: str, new: str, label: str) -> str:
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{label}: expected one match, found {count}")
    return text.replace(old, new, 1)


path = Path("internal/search/metric_alt.go")
text = path.read_text()
text = replace_once(
    text,
    'import (\n\t"errors"\n\t"math/bits"\n\t"sync"\n\t"time"\n',
    'import (\n\t"errors"\n\t"math/bits"\n\t"os"\n\t"strconv"\n\t"sync"\n\t"time"\n',
    "imports",
)
text = replace_once(
    text,
    '\tmetricALTMaximumLandmarks = 16\n',
    '\tmetricALTMaximumLandmarks = 64\n',
    "maximum landmarks",
)
old = '''func RecommendedMetricALTLandmarks(g *graph.Graph) int {
\tif g == nil || g.EdgeCount < metricALTMinimumEdges {
\t\treturn 0
\t}
\tif g.EdgeCount < metricALTLargeEdges {
\t\treturn 4
\t}
\treturn 8
}
'''
new = '''func RecommendedMetricALTLandmarks(g *graph.Graph) int {
\tif g == nil || g.EdgeCount < metricALTMinimumEdges {
\t\treturn 0
\t}
\tif override := os.Getenv("AEGIS_ALT_LANDMARKS"); override != "" {
\t\tif count, err := strconv.Atoi(override); err == nil &&
\t\t\tcount > 0 && count <= metricALTMaximumLandmarks {
\t\t\treturn count
\t\t}
\t}
\tif g.EdgeCount < metricALTLargeEdges {
\t\treturn 4
\t}
\treturn 8
}
'''
text = replace_once(text, old, new, "recommended landmarks")
path.write_text(text)

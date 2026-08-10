#!/usr/bin/env python3
from pathlib import Path

path = Path("cmd/aegis/main.go")
text = path.read_text()
old = '''\tneedMetric := false
\tneedDirected := false
\tfor _, algorithm := range algorithms {
\t\tswitch algorithm {
\t\tcase search.AegisALT, search.AegisALTTop4, search.AegisALTTop2,
\t\t\tsearch.AStarALT, search.AStarDirectedALT:
\t\t\tneedMetric = true
\t\t}
\t\tif algorithm == search.AStarDirectedALT {
\t\t\tneedDirected = true
\t\t}
\t}
'''
new = '''\tneedMetric := false
\tneedDirected := false
\tfor _, algorithm := range algorithms {
\t\tname := string(algorithm)
\t\tif name == "aegis-alt" ||
\t\t\tstrings.HasPrefix(name, "aegis-alt-") ||
\t\t\tname == "astar-alt" ||
\t\t\tstrings.HasPrefix(name, "astar-directed-alt") {
\t\t\tneedMetric = true
\t\t}
\t\tif strings.HasPrefix(name, "astar-directed-alt") {
\t\t\tneedDirected = true
\t\t}
\t}
'''
count = text.count(old)
if count != 1:
    raise SystemExit(f"directed CLI preparation: expected one match, found {count}")
path.write_text(text.replace(old, new, 1))

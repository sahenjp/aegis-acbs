#!/usr/bin/env python3
from pathlib import Path

path = Path("cmd/aegis/main.go")
text = path.read_text()
old = '''\tfor _, algorithm := range algorithms {
\t\tif algorithm != search.AegisALT {
\t\t\tcontinue
\t\t}
'''
new = '''\tfor _, algorithm := range algorithms {
\t\tname := string(algorithm)
\t\tif name != "aegis-alt" &&
\t\t\t!strings.HasPrefix(name, "aegis-alt-") &&
\t\t\tname != "astar-alt" {
\t\t\tcontinue
\t\t}
'''
count = text.count(old)
if count != 1:
    raise SystemExit(f"CLI preparation: expected one match, found {count}")
path.write_text(text.replace(old, new, 1))

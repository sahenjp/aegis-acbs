#!/usr/bin/env python3
from pathlib import Path

path = Path("internal/search/metric_alt.go")
text = path.read_text()
old = "newACBSMetricALTPotential(g, source, target, index, limit)"
new = "newACBSMetricALTPotential(g, source, target, index, limit, false)"
count = text.count(old)
if count != 1:
    raise SystemExit(f"validator constructor: expected one match, found {count}")
path.write_text(text.replace(old, new, 1))

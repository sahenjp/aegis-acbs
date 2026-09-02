#!/usr/bin/env python3
from pathlib import Path
import re

path = Path("internal/search/corridor_generated_test.go")
text = path.read_text()
# Corridor unit tests are about graph-topology contraction, not geographic
# potentials. Remove coordinates so production ACBS uses the zero-potential
# path and the fixture cannot accidentally violate a synthetic geo/cost scale.
text, count = re.subn(
    r'graph\.Node\{ID: ([^,}]+), Lat: [^,}]+, Lon: [^}]+\}',
    r'graph.Node{ID: \1}',
    text,
)
if count < 3:
    raise SystemExit(f"expected at least three coordinate fixture replacements, got {count}")
path.write_text(text)

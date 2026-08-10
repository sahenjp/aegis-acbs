#!/usr/bin/env python3
from pathlib import Path
import runpy
import subprocess
import sys


def replace_once(text: str, old: str, new: str, label: str) -> str:
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{label}: expected one match, found {count}")
    return text.replace(old, new, 1)


subprocess.run([sys.executable, "scripts/materialize_metric_alt_layout_sweep.py"], check=True)

acbs_path = Path("internal/search/acbs.go")
acbs = acbs_path.read_text()
acbs = replace_once(
    acbs,
    '''func acbsMetricALTTop2(ctx context.Context, g *graph.Graph, source, target int) (Result, error) {
\treturn acbsMetricALTWithLimit(ctx, g, source, target, AegisALTTop2, 2)
}
''',
    '''func acbsMetricALTTop2(ctx context.Context, g *graph.Graph, source, target int) (Result, error) {
\treturn acbsMetricALTWithLimit(ctx, g, source, target, AegisALTTop2, 2)
}

func acbsMetricALTTop1(ctx context.Context, g *graph.Graph, source, target int) (Result, error) {
\treturn acbsMetricALTWithLimit(ctx, g, source, target, AegisALTTop1, 1)
}
''',
    "top1 entry point",
)
acbs_path.write_text(acbs)

search_path = Path("internal/search/search.go")
search = search_path.read_text()
search = replace_once(
    search,
    '\tAegisALTTop2      Algorithm = "aegis-alt-top2"\n',
    '\tAegisALTTop2      Algorithm = "aegis-alt-top2"\n\tAegisALTTop1      Algorithm = "aegis-alt-top1"\n',
    "top1 algorithm constant",
)
search = replace_once(
    search,
    '''\tcase AegisALTTop2:
\t\tr, err = acbsMetricALTTop2(ctx, g, source, target)
''',
    '''\tcase AegisALTTop2:
\t\tr, err = acbsMetricALTTop2(ctx, g, source, target)
\tcase AegisALTTop1:
\t\tr, err = acbsMetricALTTop1(ctx, g, source, target)
''',
    "top1 algorithm dispatch",
)
search_path.write_text(search)

# Keep top1 generic as an in-run reference. Top2 uses the unrolled endpoint
# cache and then the two-coordinate balanced potential below.
runpy.run_path("scripts/materialize_metric_alt_scalar2.py", run_name="__main__")
runpy.run_path("scripts/materialize_metric_alt_coordinate2.py", run_name="__main__")

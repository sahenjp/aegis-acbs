#!/usr/bin/env python3
from pathlib import Path
import runpy

runpy.run_path("scripts/materialize_hierarchy_epoch_interleaved.py", run_name="__main__")

path = Path("scripts/hierarchy_interleaved_benchmark.cpp")
text = path.read_text()


def replace_once(old: str, new: str, label: str) -> None:
    global text
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{label}: expected one match, found {count}")
    text = text.replace(old, new, 1)


replace_once(
    '''    ContractionHierarchyQuery standard(ch);
    HCBSQuery hcbs(ch);
    HCBSEpochQueueQuery hcbs_epoch(ch);
    CustomizableContractionHierarchyQuery cch_query(metric);
''',
    '''    ContractionHierarchyQuery standard(ch);
    HCBSEpochQueueQuery hcbs_epoch(ch);
    CustomizableContractionHierarchyQuery cch_query(metric);
''',
    "query objects",
)
replace_once(
    '''    constexpr size_t algorithm_count = 6;
    const std::array<std::string, algorithm_count> names = {
        "routingkit-ch", "hcbs-alternate", "hcbs-lower-key", "hcbs-lower-key-queue",
        "hcbs-epoch-lower-key", "routingkit-cch"
    };
''',
    '''    constexpr size_t algorithm_count = 3;
    const std::array<std::string, algorithm_count> names = {
        "routingkit-ch", "hcbs-epoch-lower-key", "routingkit-cch"
    };
''',
    "algorithm list",
)
start_marker = '''    auto run_algorithm = [&](size_t algorithm, const HierarchyPair& q) -> unsigned {
'''
start = text.find(start_marker)
end = text.find('    };\n\n    uint64_t exact_checks', start)
if start < 0 or end < 0:
    raise SystemExit(f"dispatch anchors missing: start={start} end={end}")
dispatch = '''    auto run_algorithm = [&](size_t algorithm, const HierarchyPair& q) -> unsigned {
        switch (algorithm) {
        case 0:
            standard.reset().add_source(q.source).add_target(q.target).run();
            return standard.get_distance();
        case 1:
            return hcbs_epoch.run(q.source, q.target, Scheduler::lower_key);
        case 2:
            cch_query.reset().add_source(q.source).add_target(q.target).run();
            return cch_query.get_distance();
        default:
            throw std::runtime_error("unknown algorithm");
        }
'''
text = text[:start] + dispatch + text[end:]

start = text.find('    std::cout << "hcbs-epoch-vs-ch-geomean-ratio="')
end = text.find('    return 0;\n}', start)
if start < 0 or end < 0:
    raise SystemExit(f"ratio anchors missing: start={start} end={end}")
ratios = '''    std::cout << "hcbs-epoch-vs-ch-geomean-ratio="
              << static_cast<double>(geomean_ratio(query_medians[1], query_medians[0])) << "\\n";
    std::cout << "hcbs-epoch-vs-cch-geomean-ratio="
              << static_cast<double>(geomean_ratio(query_medians[1], query_medians[2])) << "\\n";
'''
text = text[:start] + ratios + text[end:]
path.write_text(text)

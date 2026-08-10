#!/usr/bin/env python3
from pathlib import Path
import runpy

# First materialize the five-way CH/HCBS/CCH comparison, then replace HCBS's
# queue implementation with the epoch-capable template while retaining the
# ordinary queue alias for an exact within-process control.
runpy.run_path("scripts/materialize_hierarchy_interleaved_v2.py", run_name="__main__")
runpy.run_path("scripts/materialize_hcbs_epoch_queue.py", run_name="__main__")

path = Path("scripts/hierarchy_interleaved_benchmark.cpp")
text = path.read_text()


def replace_once(old: str, new: str, label: str) -> None:
    global text
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{label}: expected one match, found {count}")
    text = text.replace(old, new, 1)


replace_once(
    '''    constexpr size_t algorithm_count = 5;
    const std::array<std::string, algorithm_count> names = {
        "routingkit-ch", "hcbs-alternate", "hcbs-lower-key", "hcbs-lower-key-queue", "routingkit-cch"
    };
''',
    '''    constexpr size_t algorithm_count = 6;
    const std::array<std::string, algorithm_count> names = {
        "routingkit-ch", "hcbs-alternate", "hcbs-lower-key", "hcbs-lower-key-queue",
        "hcbs-epoch-lower-key", "routingkit-cch"
    };
''',
    "algorithm list",
)
replace_once(
    '''    ContractionHierarchyQuery standard(ch);
    HCBSQuery hcbs(ch);
    CustomizableContractionHierarchyQuery cch_query(metric);
''',
    '''    ContractionHierarchyQuery standard(ch);
    HCBSQuery hcbs(ch);
    HCBSEpochQueueQuery hcbs_epoch(ch);
    CustomizableContractionHierarchyQuery cch_query(metric);
''',
    "query construction",
)
replace_once(
    '''        case 3:
            return hcbs.run(q.source, q.target, Scheduler::lower_key_then_queue);
        case 4:
            cch_query.reset().add_source(q.source).add_target(q.target).run();
            return cch_query.get_distance();
''',
    '''        case 3:
            return hcbs.run(q.source, q.target, Scheduler::lower_key_then_queue);
        case 4:
            return hcbs_epoch.run(q.source, q.target, Scheduler::lower_key);
        case 5:
            cch_query.reset().add_source(q.source).add_target(q.target).run();
            return cch_query.get_distance();
''',
    "algorithm dispatch",
)

start_marker = '    std::cout << "hcbs-lower-key-vs-ch-geomean-ratio="'
end_marker = '    return 0;\n}'
start = text.find(start_marker)
end = text.find(end_marker, start)
if start < 0 or end < 0:
    raise SystemExit(f"ratio block anchors missing: start={start} end={end}")
ratio_block = '''    std::cout << "hcbs-epoch-vs-ch-geomean-ratio="
              << static_cast<double>(geomean_ratio(query_medians[4], query_medians[0])) << "\\n";
    std::cout << "hcbs-epoch-vs-cch-geomean-ratio="
              << static_cast<double>(geomean_ratio(query_medians[4], query_medians[5])) << "\\n";
    std::cout << "hcbs-epoch-vs-lower-key-geomean-ratio="
              << static_cast<double>(geomean_ratio(query_medians[4], query_medians[2])) << "\\n";
    std::cout << "hcbs-lower-key-vs-cch-geomean-ratio="
              << static_cast<double>(geomean_ratio(query_medians[2], query_medians[5])) << "\\n";
'''
text = text[:start] + ratio_block + text[end:]
path.write_text(text)

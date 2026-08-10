#!/usr/bin/env python3
from pathlib import Path
import runpy

# Keep the helper-symbol fix from the first interleaved screen.
runpy.run_path("scripts/materialize_hierarchy_interleaved.py", run_name="__main__")

path = Path("scripts/hierarchy_interleaved_benchmark.cpp")
text = path.read_text()

def replace_once(old: str, new: str, label: str) -> None:
    global text
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{label}: expected one match, found {count}")
    text = text.replace(old, new, 1)

replace_once(
    '''    constexpr size_t algorithm_count = 4;
    const std::array<std::string, algorithm_count> names = {
        "routingkit-ch", "hcbs-alternate", "hcbs-lower-key-queue", "routingkit-cch"
    };
''',
    '''    constexpr size_t algorithm_count = 5;
    const std::array<std::string, algorithm_count> names = {
        "routingkit-ch", "hcbs-alternate", "hcbs-lower-key", "hcbs-lower-key-queue", "routingkit-cch"
    };
''',
    "algorithm list",
)
replace_once(
    '''        case 2:
            return hcbs.run(q.source, q.target, Scheduler::lower_key_then_queue);
        case 3:
            cch_query.reset().add_source(q.source).add_target(q.target).run();
            return cch_query.get_distance();
''',
    '''        case 2:
            return hcbs.run(q.source, q.target, Scheduler::lower_key);
        case 3:
            return hcbs.run(q.source, q.target, Scheduler::lower_key_then_queue);
        case 4:
            cch_query.reset().add_source(q.source).add_target(q.target).run();
            return cch_query.get_distance();
''',
    "algorithm dispatch",
)
replace_once(
    '''    std::cout << "hcbs-best-vs-ch-geomean-ratio="
              << static_cast<double>(geomean_ratio(query_medians[2], query_medians[0])) << "\n";
    std::cout << "hcbs-best-vs-cch-geomean-ratio="
              << static_cast<double>(geomean_ratio(query_medians[2], query_medians[3])) << "\n";
    std::cout << "hcbs-scheduler-vs-hcbs-alternate-geomean-ratio="
              << static_cast<double>(geomean_ratio(query_medians[2], query_medians[1])) << "\n";
''',
    '''    std::cout << "hcbs-lower-key-vs-ch-geomean-ratio="
              << static_cast<double>(geomean_ratio(query_medians[2], query_medians[0])) << "\n";
    std::cout << "hcbs-lower-key-vs-cch-geomean-ratio="
              << static_cast<double>(geomean_ratio(query_medians[2], query_medians[4])) << "\n";
    std::cout << "hcbs-lower-key-vs-alternate-geomean-ratio="
              << static_cast<double>(geomean_ratio(query_medians[2], query_medians[1])) << "\n";
    std::cout << "hcbs-lower-key-queue-vs-cch-geomean-ratio="
              << static_cast<double>(geomean_ratio(query_medians[3], query_medians[4])) << "\n";
''',
    "ratio output",
)
path.write_text(text)

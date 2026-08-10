#!/usr/bin/env python3
from pathlib import Path
import runpy

# Start from the strongest current six-way benchmark: standard CH, ordinary
# HCBS variants, epoch-clear HCBS, and RoutingKit CCH on one rotated stream.
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
    '''    const auto customize_begin = Clock::now();
    CustomizableContractionHierarchyMetric metric(cch, data.weight);
    metric.customize();
    const uint64_t cch_customize_ns = elapsed_ns(customize_begin, Clock::now());

    ContractionHierarchyQuery standard(ch);
    HCBSQuery hcbs(ch);
    HCBSEpochQueueQuery hcbs_epoch(ch);
    CustomizableContractionHierarchyQuery cch_query(metric);
''',
    '''    const auto customize_begin = Clock::now();
    CustomizableContractionHierarchyMetric metric(cch, data.weight);
    metric.customize();
    const uint64_t cch_customize_ns = elapsed_ns(customize_begin, Clock::now());
    const auto perfect_begin = Clock::now();
    const ContractionHierarchy perfect_ch = metric.build_contraction_hierarchy_using_perfect_witness_search();
    const uint64_t perfect_witness_ch_ns = elapsed_ns(perfect_begin, Clock::now());

    ContractionHierarchyQuery standard(ch);
    HCBSQuery hcbs(ch);
    HCBSEpochQueueQuery hcbs_epoch(ch);
    CustomizableContractionHierarchyQuery cch_query(metric);
    ContractionHierarchyQuery perfect_standard(perfect_ch);
    HCBSEpochQueueQuery perfect_epoch(perfect_ch);
''',
    "perfect witness preprocessing",
)
replace_once(
    '''    constexpr size_t algorithm_count = 6;
    const std::array<std::string, algorithm_count> names = {
        "routingkit-ch", "hcbs-alternate", "hcbs-lower-key", "hcbs-lower-key-queue",
        "hcbs-epoch-lower-key", "routingkit-cch"
    };
''',
    '''    constexpr size_t algorithm_count = 8;
    const std::array<std::string, algorithm_count> names = {
        "routingkit-ch", "hcbs-alternate", "hcbs-lower-key", "hcbs-lower-key-queue",
        "hcbs-epoch-lower-key", "routingkit-cch", "cch-perfect-ch", "cch-perfect-epoch-hcbs"
    };
''',
    "algorithm list",
)
replace_once(
    '''        case 5:
            cch_query.reset().add_source(q.source).add_target(q.target).run();
            return cch_query.get_distance();
        default:
''',
    '''        case 5:
            cch_query.reset().add_source(q.source).add_target(q.target).run();
            return cch_query.get_distance();
        case 6:
            perfect_standard.reset().add_source(q.source).add_target(q.target).run();
            return perfect_standard.get_distance();
        case 7:
            return perfect_epoch.run(q.source, q.target, Scheduler::lower_key);
        default:
''',
    "perfect query dispatch",
)
replace_once(
    '''              << " cch_customize_ns=" << cch_customize_ns
              << " queries=" << data.queries.size() << " repeats=" << repeats
''',
    '''              << " cch_customize_ns=" << cch_customize_ns
              << " perfect_witness_ch_ns=" << perfect_witness_ch_ns
              << " queries=" << data.queries.size() << " repeats=" << repeats
''',
    "preprocessing output",
)

start_marker = '    std::cout << "hcbs-epoch-vs-ch-geomean-ratio="'
end_marker = '    return 0;\n}'
start = text.find(start_marker)
end = text.find(end_marker, start)
if start < 0 or end < 0:
    raise SystemExit(f"ratio block anchors missing: start={start} end={end}")
ratio_block = '''    std::cout << "hcbs-epoch-vs-ch-geomean-ratio="
              << static_cast<double>(geomean_ratio(query_medians[4], query_medians[0])) << "\\n";
    std::cout << "hcbs-epoch-vs-cch-geomean-ratio="
              << static_cast<double>(geomean_ratio(query_medians[4], query_medians[5])) << "\\n";
    std::cout << "perfect-ch-vs-classic-ch-geomean-ratio="
              << static_cast<double>(geomean_ratio(query_medians[6], query_medians[0])) << "\\n";
    std::cout << "perfect-ch-vs-cch-geomean-ratio="
              << static_cast<double>(geomean_ratio(query_medians[6], query_medians[5])) << "\\n";
    std::cout << "perfect-epoch-vs-perfect-ch-geomean-ratio="
              << static_cast<double>(geomean_ratio(query_medians[7], query_medians[6])) << "\\n";
    std::cout << "perfect-epoch-vs-cch-geomean-ratio="
              << static_cast<double>(geomean_ratio(query_medians[7], query_medians[5])) << "\\n";
    std::cout << "classic-epoch-vs-perfect-epoch-geomean-ratio="
              << static_cast<double>(geomean_ratio(query_medians[4], query_medians[7])) << "\\n";
'''
text = text[:start] + ratio_block + text[end:]
path.write_text(text)

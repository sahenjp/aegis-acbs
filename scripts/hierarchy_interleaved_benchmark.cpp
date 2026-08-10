#include <routingkit/customizable_contraction_hierarchy.h>
#include <routingkit/nested_dissection.h>

#include <algorithm>
#include <array>
#include <chrono>
#include <cmath>
#include <cstdint>
#include <fstream>
#include <iostream>
#include <stdexcept>
#include <string>
#include <vector>

#define main routingkit_hcbs_standalone_main
#include "routingkit_hcbs_query.cpp"
#undef main

using RoutingKit::CustomizableContractionHierarchy;
using RoutingKit::CustomizableContractionHierarchyMetric;
using RoutingKit::CustomizableContractionHierarchyQuery;
using RoutingKit::compute_nested_node_dissection_order_using_inertial_flow;

using Clock = std::chrono::steady_clock;

struct HierarchyPair {
    unsigned source = 0;
    unsigned target = 0;
};

struct HierarchyInput {
    unsigned node_count = 0;
    std::vector<float> lat;
    std::vector<float> lon;
    std::vector<unsigned> tail;
    std::vector<unsigned> head;
    std::vector<unsigned> weight;
    std::vector<HierarchyPair> queries;
};

static uint64_t elapsed_ns(Clock::time_point begin, Clock::time_point end) {
    const auto value = std::chrono::duration_cast<std::chrono::nanoseconds>(end - begin).count();
    return value < 1 ? 1 : static_cast<uint64_t>(value);
}

static HierarchyInput read_hierarchy_input(const std::string& path) {
    std::ifstream in(path);
    if (!in) throw std::runtime_error("cannot open hierarchy input");
    std::string magic;
    in >> magic;
    if (magic != "AEGIS_HIERARCHY_INTERLEAVED_V1") throw std::runtime_error("bad hierarchy input magic");
    unsigned edge_count = 0, query_count = 0;
    HierarchyInput data;
    in >> data.node_count >> edge_count >> query_count;
    data.lat.resize(data.node_count);
    data.lon.resize(data.node_count);
    for (unsigned i = 0; i < data.node_count; ++i) in >> data.lat[i] >> data.lon[i];
    data.tail.resize(edge_count);
    data.head.resize(edge_count);
    data.weight.resize(edge_count);
    for (unsigned i = 0; i < edge_count; ++i) in >> data.tail[i] >> data.head[i] >> data.weight[i];
    data.queries.resize(query_count);
    for (unsigned i = 0; i < query_count; ++i) in >> data.queries[i].source >> data.queries[i].target;
    if (!in) throw std::runtime_error("truncated hierarchy input");
    return data;
}

struct TimingSummary {
    std::string name;
    uint64_t median_ns = 0;
    uint64_t p95_ns = 0;
    uint64_t p99_ns = 0;
};

static uint64_t percentile(std::vector<uint64_t> values, double p) {
    if (values.empty()) return 0;
    std::sort(values.begin(), values.end());
    const size_t index = static_cast<size_t>(p * static_cast<double>(values.size() - 1));
    return values[index];
}

static long double geomean_ratio(const std::vector<uint64_t>& left, const std::vector<uint64_t>& right) {
    long double sum = 0;
    for (size_t i = 0; i < left.size(); ++i) {
        sum += std::log(static_cast<long double>(left[i]) / static_cast<long double>(right[i]));
    }
    return std::exp(sum / static_cast<long double>(left.size()));
}

int main(int argc, char** argv) {
    if (argc < 2 || argc > 3) {
        std::cerr << "usage: hierarchy_interleaved_benchmark INPUT [REPEATS]\n";
        return 2;
    }
    const unsigned repeats = argc == 3 ? static_cast<unsigned>(std::stoul(argv[2])) : 31;
    if (repeats == 0) throw std::runtime_error("repeats must be positive");
    const HierarchyInput data = read_hierarchy_input(argv[1]);

    const auto ch_begin = Clock::now();
    const ContractionHierarchy ch = ContractionHierarchy::build(data.node_count, data.tail, data.head, data.weight);
    const uint64_t ch_preprocess_ns = elapsed_ns(ch_begin, Clock::now());

    const auto order_begin = Clock::now();
    const auto order = compute_nested_node_dissection_order_using_inertial_flow(
        data.node_count, data.tail, data.head, data.lat, data.lon);
    const uint64_t cch_order_ns = elapsed_ns(order_begin, Clock::now());
    const auto topology_begin = Clock::now();
    const CustomizableContractionHierarchy cch(order, data.tail, data.head);
    const uint64_t cch_topology_ns = elapsed_ns(topology_begin, Clock::now());
    const auto customize_begin = Clock::now();
    CustomizableContractionHierarchyMetric metric(cch, data.weight);
    metric.customize();
    const uint64_t cch_customize_ns = elapsed_ns(customize_begin, Clock::now());

    ContractionHierarchyQuery standard(ch);
    HCBSQuery hcbs(ch);
    CustomizableContractionHierarchyQuery cch_query(metric);

    constexpr size_t algorithm_count = 4;
    const std::array<std::string, algorithm_count> names = {
        "routingkit-ch", "hcbs-alternate", "hcbs-lower-key-queue", "routingkit-cch"
    };
    std::array<std::vector<uint64_t>, algorithm_count> query_medians;
    for (auto& values : query_medians) values.reserve(data.queries.size());

    auto run_algorithm = [&](size_t algorithm, const HierarchyPair& q) -> unsigned {
        switch (algorithm) {
        case 0:
            standard.reset().add_source(q.source).add_target(q.target).run();
            return standard.get_distance();
        case 1:
            return hcbs.run(q.source, q.target, Scheduler::alternate);
        case 2:
            return hcbs.run(q.source, q.target, Scheduler::lower_key_then_queue);
        case 3:
            cch_query.reset().add_source(q.source).add_target(q.target).run();
            return cch_query.get_distance();
        default:
            throw std::runtime_error("unknown algorithm");
        }
    };

    uint64_t exact_checks = 0;
    for (size_t qi = 0; qi < data.queries.size(); ++qi) {
        const auto& q = data.queries[qi];
        const unsigned reference = run_algorithm(0, q);
        for (size_t algorithm = 1; algorithm < algorithm_count; ++algorithm) {
            if (run_algorithm(algorithm, q) != reference) {
                std::cerr << "distance mismatch query=" << qi << " algorithm=" << names[algorithm] << "\n";
                return 1;
            }
            ++exact_checks;
        }

        std::array<std::vector<uint64_t>, algorithm_count> samples;
        for (auto& values : samples) values.reserve(repeats);
        for (unsigned repeat = 0; repeat < repeats; ++repeat) {
            const size_t rotation = (qi + repeat) % algorithm_count;
            for (size_t offset = 0; offset < algorithm_count; ++offset) {
                const size_t algorithm = (rotation + offset) % algorithm_count;
                const auto begin = Clock::now();
                const unsigned result = run_algorithm(algorithm, q);
                const auto end = Clock::now();
                if (result != reference) {
                    std::cerr << "timed distance mismatch query=" << qi << " algorithm=" << names[algorithm] << "\n";
                    return 1;
                }
                samples[algorithm].push_back(elapsed_ns(begin, end));
            }
        }
        for (size_t algorithm = 0; algorithm < algorithm_count; ++algorithm) {
            std::sort(samples[algorithm].begin(), samples[algorithm].end());
            query_medians[algorithm].push_back(samples[algorithm][samples[algorithm].size() / 2]);
        }
    }

    std::array<TimingSummary, algorithm_count> summaries;
    for (size_t algorithm = 0; algorithm < algorithm_count; ++algorithm) {
        summaries[algorithm] = TimingSummary{
            names[algorithm],
            percentile(query_medians[algorithm], .50),
            percentile(query_medians[algorithm], .95),
            percentile(query_medians[algorithm], .99),
        };
    }

    std::cout << "hierarchy-preprocess ch_ns=" << ch_preprocess_ns
              << " cch_order_ns=" << cch_order_ns
              << " cch_topology_ns=" << cch_topology_ns
              << " cch_customize_ns=" << cch_customize_ns
              << " queries=" << data.queries.size() << " repeats=" << repeats
              << " exact_checks=" << exact_checks << "\n";
    for (const auto& summary : summaries) {
        std::cout << summary.name << " median_ns=" << summary.median_ns
                  << " p95_ns=" << summary.p95_ns
                  << " p99_ns=" << summary.p99_ns << " correct=true\n";
    }
    std::cout << "hcbs-best-vs-ch-geomean-ratio="
              << static_cast<double>(geomean_ratio(query_medians[2], query_medians[0])) << "\n";
    std::cout << "hcbs-best-vs-cch-geomean-ratio="
              << static_cast<double>(geomean_ratio(query_medians[2], query_medians[3])) << "\n";
    std::cout << "hcbs-scheduler-vs-hcbs-alternate-geomean-ratio="
              << static_cast<double>(geomean_ratio(query_medians[2], query_medians[1])) << "\n";
    return 0;
}

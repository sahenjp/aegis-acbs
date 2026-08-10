#include <routingkit/contraction_hierarchy.h>

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

#define main routingkit_hcbs_embedded_main
#include "routingkit_hcbs_query.cpp"
#undef main

using Clock = std::chrono::steady_clock;

struct TailPair {
    unsigned source = 0;
    unsigned target = 0;
    unsigned cls = 0;
};

struct TailInput {
    unsigned node_count = 0;
    std::vector<unsigned> tail;
    std::vector<unsigned> head;
    std::vector<unsigned> weight;
    std::vector<TailPair> queries;
};

static TailInput read_tail_input(const std::string& path) {
    std::ifstream in(path);
    if (!in) throw std::runtime_error("cannot open input");
    std::string magic;
    in >> magic;
    if (magic != "AEGIS_ROUTINGKIT_HCBS_CLASSES_V1") throw std::runtime_error("bad class input magic");
    unsigned edge_count = 0, query_count = 0;
    TailInput data;
    in >> data.node_count >> edge_count >> query_count;
    data.tail.resize(edge_count);
    data.head.resize(edge_count);
    data.weight.resize(edge_count);
    for (unsigned i = 0; i < edge_count; ++i) in >> data.tail[i] >> data.head[i] >> data.weight[i];
    data.queries.resize(query_count);
    for (unsigned i = 0; i < query_count; ++i) {
        in >> data.queries[i].source >> data.queries[i].target >> data.queries[i].cls;
        if (data.queries[i].cls > 2) throw std::runtime_error("invalid query class");
    }
    if (!in) throw std::runtime_error("truncated input");
    return data;
}

static uint64_t tail_elapsed(Clock::time_point begin, Clock::time_point end) {
    const auto value = std::chrono::duration_cast<std::chrono::nanoseconds>(end - begin).count();
    return value < 1 ? 1 : static_cast<uint64_t>(value);
}

static uint64_t tail_percentile(std::vector<uint64_t> values, double p) {
    if (values.empty()) return 0;
    std::sort(values.begin(), values.end());
    const size_t index = static_cast<size_t>(p * static_cast<double>(values.size() - 1));
    return values[index];
}

static long double tail_geomean_ratio(const std::vector<uint64_t>& candidate, const std::vector<uint64_t>& base) {
    long double total = 0;
    for (size_t i = 0; i < candidate.size(); ++i) {
        total += std::log(static_cast<long double>(candidate[i]) / static_cast<long double>(base[i]));
    }
    return std::exp(total / static_cast<long double>(candidate.size()));
}

int main(int argc, char** argv) {
    if (argc < 2 || argc > 3) {
        std::cerr << "usage: routingkit_hcbs_class_profile INPUT [REPEATS]\n";
        return 2;
    }
    const unsigned repeats = argc == 3 ? static_cast<unsigned>(std::stoul(argv[2])) : 31;
    const TailInput data = read_tail_input(argv[1]);
    const auto prep_begin = Clock::now();
    const ContractionHierarchy ch = ContractionHierarchy::build(data.node_count, data.tail, data.head, data.weight);
    const uint64_t prep_ns = tail_elapsed(prep_begin, Clock::now());

    ContractionHierarchyQuery standard(ch);
    HCBSQuery hcbs(ch);
    constexpr size_t algorithm_count = 3;
    const std::array<std::string, algorithm_count> names = {"routingkit-ch", "hcbs-alternate", "hcbs-lower-key"};
    std::array<std::array<std::vector<uint64_t>, 3>, algorithm_count> by_class;
    std::array<std::vector<uint64_t>, algorithm_count> overall;
    for (auto& algorithms : by_class) {
        for (auto& values : algorithms) values.reserve(data.queries.size() / 3 + 8);
    }
    for (auto& values : overall) values.reserve(data.queries.size());

    auto run = [&](size_t algorithm, const TailPair& q) -> unsigned {
        if (algorithm == 0) {
            standard.reset().add_source(q.source).add_target(q.target).run();
            return standard.get_distance();
        }
        if (algorithm == 1) return hcbs.run(q.source, q.target, Scheduler::alternate);
        return hcbs.run(q.source, q.target, Scheduler::lower_key);
    };

    uint64_t exact_checks = 0;
    for (size_t qi = 0; qi < data.queries.size(); ++qi) {
        const auto& q = data.queries[qi];
        const unsigned reference = run(0, q);
        for (size_t algorithm = 1; algorithm < algorithm_count; ++algorithm) {
            if (run(algorithm, q) != reference) {
                std::cerr << "exactness mismatch query=" << qi << " algorithm=" << names[algorithm] << "\n";
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
                const unsigned result = run(algorithm, q);
                const auto end = Clock::now();
                if (result != reference) {
                    std::cerr << "timed mismatch query=" << qi << " algorithm=" << names[algorithm] << "\n";
                    return 1;
                }
                samples[algorithm].push_back(tail_elapsed(begin, end));
            }
        }
        for (size_t algorithm = 0; algorithm < algorithm_count; ++algorithm) {
            std::sort(samples[algorithm].begin(), samples[algorithm].end());
            const uint64_t median = samples[algorithm][samples[algorithm].size() / 2];
            overall[algorithm].push_back(median);
            by_class[algorithm][q.cls].push_back(median);
        }
    }

    std::cout << "tail-profile preprocess_ns=" << prep_ns << " queries=" << data.queries.size()
              << " repeats=" << repeats << " exact_checks=" << exact_checks << "\n";
    const std::array<std::string, 3> class_names = {"global", "local", "regional"};
    for (size_t algorithm = 0; algorithm < algorithm_count; ++algorithm) {
        std::cout << names[algorithm] << " overall median=" << tail_percentile(overall[algorithm], .50)
                  << " p95=" << tail_percentile(overall[algorithm], .95)
                  << " p99=" << tail_percentile(overall[algorithm], .99) << "\n";
        for (size_t cls = 0; cls < 3; ++cls) {
            const auto& values = by_class[algorithm][cls];
            std::cout << names[algorithm] << " class=" << class_names[cls]
                      << " n=" << values.size()
                      << " median=" << tail_percentile(values, .50)
                      << " p95=" << tail_percentile(values, .95)
                      << " p99=" << tail_percentile(values, .99) << "\n";
        }
    }
    for (size_t cls = 0; cls < 3; ++cls) {
        std::cout << "lower-key-vs-ch class=" << class_names[cls]
                  << " geomean_ratio=" << static_cast<double>(tail_geomean_ratio(by_class[2][cls], by_class[0][cls])) << "\n";
        std::cout << "alternate-vs-ch class=" << class_names[cls]
                  << " geomean_ratio=" << static_cast<double>(tail_geomean_ratio(by_class[1][cls], by_class[0][cls])) << "\n";
        std::cout << "lower-key-vs-alternate class=" << class_names[cls]
                  << " geomean_ratio=" << static_cast<double>(tail_geomean_ratio(by_class[2][cls], by_class[1][cls])) << "\n";
    }
    return 0;
}

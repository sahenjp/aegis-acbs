#include <routingkit/contraction_hierarchy.h>

#include <algorithm>
#include <array>
#include <chrono>
#include <cmath>
#include <cstdint>
#include <fstream>
#include <iostream>
#include <limits>
#include <stdexcept>
#include <string>
#include <vector>

#define main routingkit_hcbs_embedded_main
#include "routingkit_hcbs_query.cpp"
#undef main

using Clock = std::chrono::steady_clock;

struct HybridPair {
    unsigned source = 0;
    unsigned target = 0;
};

struct HybridInput {
    unsigned node_count = 0;
    std::vector<float> lat;
    std::vector<float> lon;
    std::vector<unsigned> tail;
    std::vector<unsigned> head;
    std::vector<unsigned> weight;
    std::vector<HybridPair> queries;
};

static HybridInput read_hybrid_input(const std::string& path) {
    std::ifstream in(path);
    if (!in) throw std::runtime_error("cannot open input");
    std::string magic;
    in >> magic;
    if (magic != "AEGIS_HIERARCHY_INTERLEAVED_V1") throw std::runtime_error("bad input magic");
    unsigned edge_count = 0, query_count = 0;
    HybridInput data;
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
    if (!in) throw std::runtime_error("truncated input");
    return data;
}

static uint64_t hybrid_elapsed(Clock::time_point begin, Clock::time_point end) {
    const auto ns = std::chrono::duration_cast<std::chrono::nanoseconds>(end - begin).count();
    return ns < 1 ? 1 : static_cast<uint64_t>(ns);
}

static uint64_t hybrid_percentile(std::vector<uint64_t> values, double p) {
    if (values.empty()) return 0;
    std::sort(values.begin(), values.end());
    const size_t index = static_cast<size_t>(p * static_cast<double>(values.size() - 1));
    return values[index];
}

static long double hybrid_geomean_ratio(const std::vector<uint64_t>& candidate, const std::vector<uint64_t>& base) {
    long double total = 0;
    for (size_t i = 0; i < candidate.size(); ++i) {
        total += std::log(static_cast<long double>(candidate[i]) / static_cast<long double>(base[i]));
    }
    return std::exp(total / static_cast<long double>(candidate.size()));
}

int main(int argc, char** argv) {
    if (argc < 2 || argc > 3) {
        std::cerr << "usage: hcbs_distance_hybrid_sweep INPUT [REPEATS]\n";
        return 2;
    }
    const unsigned repeats = argc == 3 ? static_cast<unsigned>(std::stoul(argv[2])) : 31;
    if (repeats == 0) throw std::runtime_error("repeats must be positive");
    const HybridInput data = read_hybrid_input(argv[1]);

    float min_lat = std::numeric_limits<float>::infinity();
    float max_lat = -std::numeric_limits<float>::infinity();
    float min_lon = std::numeric_limits<float>::infinity();
    float max_lon = -std::numeric_limits<float>::infinity();
    for (unsigned i = 0; i < data.node_count; ++i) {
        min_lat = std::min(min_lat, data.lat[i]);
        max_lat = std::max(max_lat, data.lat[i]);
        min_lon = std::min(min_lon, data.lon[i]);
        max_lon = std::max(max_lon, data.lon[i]);
    }
    const double inv_lat_span = max_lat > min_lat ? 1.0 / static_cast<double>(max_lat - min_lat) : 0.0;
    const double inv_lon_span = max_lon > min_lon ? 1.0 / static_cast<double>(max_lon - min_lon) : 0.0;

    const auto prep_begin = Clock::now();
    const ContractionHierarchy ch = ContractionHierarchy::build(data.node_count, data.tail, data.head, data.weight);
    const uint64_t prep_ns = hybrid_elapsed(prep_begin, Clock::now());
    ContractionHierarchyQuery standard(ch);
    HCBSQuery hcbs(ch);

    std::vector<uint64_t> standard_times;
    std::vector<uint64_t> alternate_times;
    std::vector<uint64_t> lower_key_times;
    std::vector<double> separations;
    standard_times.reserve(data.queries.size());
    alternate_times.reserve(data.queries.size());
    lower_key_times.reserve(data.queries.size());
    separations.reserve(data.queries.size());

    uint64_t exact_checks = 0;
    for (size_t qi = 0; qi < data.queries.size(); ++qi) {
        const auto& q = data.queries[qi];
        standard.reset().add_source(q.source).add_target(q.target).run();
        const unsigned reference = standard.get_distance();
        if (hcbs.run(q.source, q.target, Scheduler::alternate) != reference ||
            hcbs.run(q.source, q.target, Scheduler::lower_key) != reference) {
            std::cerr << "exactness mismatch query=" << qi << "\n";
            return 1;
        }
        exact_checks += 2;

        std::array<std::vector<uint64_t>, 3> samples;
        for (auto& values : samples) values.reserve(repeats);
        for (unsigned repeat = 0; repeat < repeats; ++repeat) {
            const size_t rotation = (qi + repeat) % 3;
            for (size_t offset = 0; offset < 3; ++offset) {
                const size_t algorithm = (rotation + offset) % 3;
                const auto begin = Clock::now();
                unsigned result = inf_weight;
                if (algorithm == 0) {
                    standard.reset().add_source(q.source).add_target(q.target).run();
                    result = standard.get_distance();
                } else if (algorithm == 1) {
                    result = hcbs.run(q.source, q.target, Scheduler::alternate);
                } else {
                    result = hcbs.run(q.source, q.target, Scheduler::lower_key);
                }
                const auto end = Clock::now();
                if (result != reference) {
                    std::cerr << "timed exactness mismatch query=" << qi << " algorithm=" << algorithm << "\n";
                    return 1;
                }
                samples[algorithm].push_back(hybrid_elapsed(begin, end));
            }
        }
        for (auto& values : samples) std::sort(values.begin(), values.end());
        standard_times.push_back(samples[0][samples[0].size() / 2]);
        alternate_times.push_back(samples[1][samples[1].size() / 2]);
        lower_key_times.push_back(samples[2][samples[2].size() / 2]);

        const double dlat = std::abs(static_cast<double>(data.lat[q.source]) - static_cast<double>(data.lat[q.target])) * inv_lat_span;
        const double dlon = std::abs(static_cast<double>(data.lon[q.source]) - static_cast<double>(data.lon[q.target])) * inv_lon_span;
        separations.push_back(std::max(dlat, dlon));
    }

    std::cout << "distance-hybrid-sweep preprocess_ns=" << prep_ns
              << " queries=" << data.queries.size() << " repeats=" << repeats
              << " exact_checks=" << exact_checks << "\n";
    std::cout << "standard median=" << hybrid_percentile(standard_times, .50)
              << " p95=" << hybrid_percentile(standard_times, .95)
              << " p99=" << hybrid_percentile(standard_times, .99) << "\n";
    std::cout << "alternate median=" << hybrid_percentile(alternate_times, .50)
              << " p95=" << hybrid_percentile(alternate_times, .95)
              << " p99=" << hybrid_percentile(alternate_times, .99)
              << " geomean_vs_ch=" << static_cast<double>(hybrid_geomean_ratio(alternate_times, standard_times)) << "\n";
    std::cout << "lower-key median=" << hybrid_percentile(lower_key_times, .50)
              << " p95=" << hybrid_percentile(lower_key_times, .95)
              << " p99=" << hybrid_percentile(lower_key_times, .99)
              << " geomean_vs_ch=" << static_cast<double>(hybrid_geomean_ratio(lower_key_times, standard_times)) << "\n";

    const std::array<double, 12> thresholds = {0.03, 0.05, 0.08, 0.10, 0.15, 0.20, 0.25, 0.35, 0.50, 0.65, 0.80, 1.01};
    for (double threshold : thresholds) {
        std::vector<uint64_t> hybrid;
        hybrid.reserve(data.queries.size());
        size_t alternate_count = 0;
        for (size_t i = 0; i < data.queries.size(); ++i) {
            if (separations[i] > threshold) {
                hybrid.push_back(alternate_times[i]);
                ++alternate_count;
            } else {
                hybrid.push_back(lower_key_times[i]);
            }
        }
        std::cout << "threshold=" << threshold
                  << " alternate_n=" << alternate_count
                  << " lower_key_n=" << (data.queries.size() - alternate_count)
                  << " median=" << hybrid_percentile(hybrid, .50)
                  << " p95=" << hybrid_percentile(hybrid, .95)
                  << " p99=" << hybrid_percentile(hybrid, .99)
                  << " geomean_vs_ch=" << static_cast<double>(hybrid_geomean_ratio(hybrid, standard_times))
                  << " geomean_vs_alternate=" << static_cast<double>(hybrid_geomean_ratio(hybrid, alternate_times))
                  << " geomean_vs_lower_key=" << static_cast<double>(hybrid_geomean_ratio(hybrid, lower_key_times)) << "\n";
    }
    return 0;
}

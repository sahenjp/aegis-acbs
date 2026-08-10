#include <routingkit/contraction_hierarchy.h>
#include <routingkit/constants.h>

#include <algorithm>
#include <chrono>
#include <cstdint>
#include <fstream>
#include <iostream>
#include <stdexcept>
#include <string>
#include <vector>

using namespace RoutingKit;
using Clock = std::chrono::steady_clock;

struct Query {
    unsigned source;
    unsigned target;
    unsigned expected_distance;
    bool expected_reachable;
};

struct Result {
    long long duration_ns;
    unsigned distance;
    bool reachable;
    bool correct;
};

static long long positive_ns(Clock::duration d) {
    auto ns = std::chrono::duration_cast<std::chrono::nanoseconds>(d).count();
    return ns < 1 ? 1 : ns;
}

static long long percentile(std::vector<long long> values, double p) {
    if (values.empty()) return 0;
    std::sort(values.begin(), values.end());
    std::size_t index = static_cast<std::size_t>(p * values.size());
    if (index >= values.size()) index = values.size() - 1;
    return values[index];
}

int main(int argc, char** argv) {
    if (argc < 3 || argc > 4) {
        std::cerr << "usage: routingkit_ch_baseline INPUT OUTPUT [REPEATS]\n";
        return 2;
    }
    int repeats = argc == 4 ? std::stoi(argv[3]) : 3;
    if (repeats < 1) throw std::runtime_error("repeats must be >= 1");

    std::ifstream in(argv[1]);
    if (!in) throw std::runtime_error("cannot open input");
    std::string magic;
    in >> magic;
    if (magic != "AEGIS_ROUTINGKIT_CH_V1") throw std::runtime_error("bad input magic");

    unsigned node_count;
    std::size_t edge_count, query_count;
    in >> node_count >> edge_count >> query_count;
    std::vector<unsigned> tail(edge_count), head(edge_count), weight(edge_count);
    for (std::size_t i = 0; i < edge_count; ++i) {
        unsigned long long w;
        in >> tail[i] >> head[i] >> w;
        if (w >= inf_weight) throw std::runtime_error("edge weight exceeds finite RoutingKit range");
        weight[i] = static_cast<unsigned>(w);
    }
    std::vector<Query> queries(query_count);
    for (std::size_t i = 0; i < query_count; ++i) {
        unsigned long long distance;
        int reachable;
        in >> queries[i].source >> queries[i].target >> distance >> reachable;
        if (distance >= inf_weight && reachable) throw std::runtime_error("reference distance exceeds finite RoutingKit range");
        queries[i].expected_distance = static_cast<unsigned>(distance);
        queries[i].expected_reachable = reachable != 0;
    }

    auto prep_begin = Clock::now();
    ContractionHierarchy ch = ContractionHierarchy::build(node_count, tail, head, weight);
    long long preprocess_ns = positive_ns(Clock::now() - prep_begin);
    ContractionHierarchyQuery query(ch);

    // Warm reusable query buffers outside timed samples.
    for (std::size_t i = 0; i < std::min<std::size_t>(queries.size(), 3); ++i) {
        query.reset().add_source(queries[i].source).add_target(queries[i].target).run();
        (void)query.get_distance();
    }

    std::vector<Result> results;
    results.reserve(query_count);
    std::vector<long long> durations;
    durations.reserve(query_count);
    bool all_correct = true;

    for (const auto& q : queries) {
        std::vector<long long> runs;
        runs.reserve(repeats);
        unsigned distance = inf_weight;
        for (int r = 0; r < repeats; ++r) {
            auto begin = Clock::now();
            query.reset().add_source(q.source).add_target(q.target).run();
            distance = query.get_distance();
            runs.push_back(positive_ns(Clock::now() - begin));
        }
        std::sort(runs.begin(), runs.end());
        long long duration = runs[runs.size() / 2];
        bool reachable = distance != inf_weight;
        bool correct = reachable == q.expected_reachable && (!reachable || distance == q.expected_distance);
        all_correct = all_correct && correct;
        durations.push_back(duration);
        results.push_back({duration, distance, reachable, correct});
    }

    long long total = 0;
    for (auto d : durations) total += d;
    long long mean = durations.empty() ? 0 : total / static_cast<long long>(durations.size());
    long long median = percentile(durations, 0.50);
    long long p95 = percentile(durations, 0.95);
    long long p99 = percentile(durations, 0.99);

    std::ofstream out(argv[2]);
    if (!out) throw std::runtime_error("cannot open output");
    out << "{\n"
        << "  \"nodes\": " << node_count << ",\n"
        << "  \"edges\": " << edge_count << ",\n"
        << "  \"queries\": " << query_count << ",\n"
        << "  \"repeats\": " << repeats << ",\n"
        << "  \"preprocessNs\": " << preprocess_ns << ",\n"
        << "  \"meanNs\": " << mean << ",\n"
        << "  \"medianNs\": " << median << ",\n"
        << "  \"p95Ns\": " << p95 << ",\n"
        << "  \"p99Ns\": " << p99 << ",\n"
        << "  \"allCorrect\": " << (all_correct ? "true" : "false") << ",\n"
        << "  \"results\": [\n";
    for (std::size_t i = 0; i < results.size(); ++i) {
        const auto& r = results[i];
        out << "    {\"queryIndex\": " << i
            << ", \"durationNs\": " << r.duration_ns
            << ", \"distance\": " << r.distance
            << ", \"reachable\": " << (r.reachable ? "true" : "false")
            << ", \"correct\": " << (r.correct ? "true" : "false") << "}";
        if (i + 1 != results.size()) out << ',';
        out << '\n';
    }
    out << "  ]\n}\n";

    std::cout << "RoutingKit CH: queries=" << query_count
              << " preprocess=" << (preprocess_ns / 1e9)
              << "s median=" << (median / 1e6)
              << "ms p95=" << (p95 / 1e6)
              << "ms p99=" << (p99 / 1e6)
              << "ms correct=" << (all_correct ? "true" : "false") << '\n';
    return all_correct ? 0 : 3;
}

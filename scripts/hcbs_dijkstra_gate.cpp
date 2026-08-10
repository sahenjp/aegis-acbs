#include <routingkit/contraction_hierarchy.h>

#include <algorithm>
#include <cstdint>
#include <fstream>
#include <iostream>
#include <limits>
#include <queue>
#include <stdexcept>
#include <string>
#include <utility>
#include <vector>

#define main routingkit_hcbs_standalone_main
#include "routingkit_hcbs_query.cpp"
#undef main

using RoutingKit::ContractionHierarchy;

struct Pair {
    unsigned source = 0;
    unsigned target = 0;
};

struct Input {
    unsigned node_count = 0;
    std::vector<unsigned> tail;
    std::vector<unsigned> head;
    std::vector<unsigned> weight;
    std::vector<Pair> queries;
};

static Input read_input(const std::string& path) {
    std::ifstream in(path);
    if (!in) throw std::runtime_error("cannot open hierarchy input");
    std::string magic;
    unsigned edge_count = 0;
    unsigned query_count = 0;
    Input data;
    in >> magic >> data.node_count >> edge_count >> query_count;
    if (magic != "AEGIS_HIERARCHY_INTERLEAVED_V1") throw std::runtime_error("bad hierarchy input magic");
    double ignored_lat = 0;
    double ignored_lon = 0;
    for (unsigned i = 0; i < data.node_count; ++i) in >> ignored_lat >> ignored_lon;
    data.tail.resize(edge_count);
    data.head.resize(edge_count);
    data.weight.resize(edge_count);
    for (unsigned i = 0; i < edge_count; ++i) in >> data.tail[i] >> data.head[i] >> data.weight[i];
    data.queries.resize(query_count);
    for (auto& query : data.queries) in >> query.source >> query.target;
    if (!in) throw std::runtime_error("truncated hierarchy input");
    return data;
}

class DijkstraVerifier {
public:
    explicit DijkstraVerifier(const Input& data)
        : first_out_(data.node_count + 1, 0),
          head_(data.head.size()),
          weight_(data.weight.size()),
          distance_(data.node_count),
          seen_(data.node_count, 0) {
        for (unsigned tail : data.tail) {
            if (tail >= data.node_count) throw std::runtime_error("tail out of range");
            ++first_out_[tail + 1];
        }
        for (unsigned i = 1; i < first_out_.size(); ++i) first_out_[i] += first_out_[i - 1];
        std::vector<unsigned> cursor = first_out_;
        for (size_t arc = 0; arc < data.head.size(); ++arc) {
            if (data.head[arc] >= data.node_count) throw std::runtime_error("head out of range");
            const unsigned slot = cursor[data.tail[arc]]++;
            head_[slot] = data.head[arc];
            weight_[slot] = data.weight[arc];
        }
    }

    unsigned distance(unsigned source, unsigned target) {
        if (source == target) return 0;
        advance_epoch();
        while (!queue_.empty()) queue_.pop();
        seen_[source] = epoch_;
        distance_[source] = 0;
        queue_.push({0, source});
        while (!queue_.empty()) {
            const auto [distance, node] = queue_.top();
            queue_.pop();
            if (seen_[node] != epoch_ || distance != distance_[node]) continue;
            if (node == target) return distance;
            for (unsigned arc = first_out_[node]; arc < first_out_[node + 1]; ++arc) {
                const unsigned next = head_[arc];
                const uint64_t candidate64 = static_cast<uint64_t>(distance) + weight_[arc];
                if (candidate64 >= inf_) continue;
                const unsigned candidate = static_cast<unsigned>(candidate64);
                if (seen_[next] != epoch_ || candidate < distance_[next]) {
                    seen_[next] = epoch_;
                    distance_[next] = candidate;
                    queue_.push({candidate, next});
                }
            }
        }
        return inf_;
    }

private:
    using QueueItem = std::pair<unsigned, unsigned>;
    static constexpr unsigned inf_ = 2147483647u;
    std::vector<unsigned> first_out_;
    std::vector<unsigned> head_;
    std::vector<unsigned> weight_;
    std::vector<unsigned> distance_;
    std::vector<uint32_t> seen_;
    uint32_t epoch_ = 0;
    std::priority_queue<QueueItem, std::vector<QueueItem>, std::greater<QueueItem>> queue_;

    void advance_epoch() {
        ++epoch_;
        if (epoch_ == 0) {
            std::fill(seen_.begin(), seen_.end(), 0);
            epoch_ = 1;
        }
    }
};

static std::vector<size_t> verification_indices(size_t query_count, unsigned per_seed) {
    constexpr size_t seed_count = 3;
    if (query_count % seed_count != 0) throw std::runtime_error("query stream is not split evenly across three seeds");
    const size_t block = query_count / seed_count;
    if (per_seed == 0 || per_seed > block) throw std::runtime_error("invalid verification count per seed");
    std::vector<size_t> indices;
    indices.reserve(seed_count * per_seed);
    for (size_t seed = 0; seed < seed_count; ++seed) {
        const size_t begin = seed * block;
        for (unsigned i = 0; i < per_seed; ++i) indices.push_back(begin + i);
    }
    return indices;
}

int main(int argc, char** argv) {
    if (argc < 2 || argc > 3) {
        std::cerr << "usage: hcbs_dijkstra_gate INPUT [QUERIES_PER_SEED]\n";
        return 2;
    }
    const unsigned per_seed = argc == 3 ? static_cast<unsigned>(std::stoul(argv[2])) : 3334;
    const Input data = read_input(argv[1]);
    const auto indices = verification_indices(data.queries.size(), per_seed);

    const ContractionHierarchy ch = ContractionHierarchy::build(data.node_count, data.tail, data.head, data.weight);
    RoutingKit::ContractionHierarchyQuery standard(ch);
    HCBSEpochQueueQuery hcbs(ch);
    DijkstraVerifier dijkstra(data);

    uint64_t checks = 0;
    uint64_t mismatches = 0;
    uint64_t max_gap = 0;
    for (size_t index : indices) {
        const auto& query = data.queries[index];
        const unsigned reference = dijkstra.distance(query.source, query.target);
        standard.reset().add_source(query.source).add_target(query.target).run();
        const unsigned ch_distance = standard.get_distance();
        const unsigned hcbs_distance = hcbs.run(query.source, query.target, Scheduler::lower_key);
        if (ch_distance != reference || hcbs_distance != reference) {
            ++mismatches;
            const uint64_t ch_gap = ch_distance > reference ? ch_distance - reference : reference - ch_distance;
            const uint64_t hcbs_gap = hcbs_distance > reference ? hcbs_distance - reference : reference - hcbs_distance;
            max_gap = std::max(max_gap, std::max(ch_gap, hcbs_gap));
            std::cerr << "mismatch query=" << index << " source=" << query.source << " target=" << query.target
                      << " dijkstra=" << reference << " ch=" << ch_distance << " hcbs=" << hcbs_distance << "\n";
            if (mismatches >= 10) return 1;
        }
        ++checks;
    }

    std::cout << "dijkstra_gate nodes=" << data.node_count
              << " edges=" << data.head.size()
              << " seeds=3 per_seed=" << per_seed
              << " comparisons=" << checks
              << " mismatches=" << mismatches
              << " max_optimality_gap=" << max_gap << "\n";
    if (mismatches != 0 || max_gap != 0) return 1;
    return 0;
}

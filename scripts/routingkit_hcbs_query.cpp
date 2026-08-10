#include <routingkit/contraction_hierarchy.h>
#include <routingkit/constants.h>
#include <routingkit/id_queue.h>

#include <algorithm>
#include <chrono>
#include <cstdint>
#include <fstream>
#include <iostream>
#include <stdexcept>
#include <string>
#include <vector>

using RoutingKit::ContractionHierarchy;
using RoutingKit::ContractionHierarchyQuery;
using RoutingKit::IDKeyPair;
using RoutingKit::MinIDQueue;
using RoutingKit::inf_weight;

struct Query {
    unsigned source = 0;
    unsigned target = 0;
    unsigned reference = 0;
    bool reachable = false;
};

struct Input {
    unsigned node_count = 0;
    std::vector<unsigned> tail;
    std::vector<unsigned> head;
    std::vector<unsigned> weight;
    std::vector<Query> queries;
    bool has_reference = false;
};

static Input read_input(const std::string& path) {
    std::ifstream in(path);
    if (!in) throw std::runtime_error("cannot open input");
    std::string magic;
    in >> magic;
    Input data;
    if (magic == "AEGIS_ROUTINGKIT_CH_V1") {
        data.has_reference = true;
    } else if (magic == "AEGIS_ROUTINGKIT_CH_V2") {
        data.has_reference = false;
    } else {
        throw std::runtime_error("bad input magic");
    }
    unsigned edge_count = 0, query_count = 0;
    in >> data.node_count >> edge_count >> query_count;
    data.tail.resize(edge_count);
    data.head.resize(edge_count);
    data.weight.resize(edge_count);
    for (unsigned i = 0; i < edge_count; ++i) {
        in >> data.tail[i] >> data.head[i] >> data.weight[i];
    }
    data.queries.resize(query_count);
    for (unsigned i = 0; i < query_count; ++i) {
        if (data.has_reference) {
            unsigned reachable = 0;
            in >> data.queries[i].source >> data.queries[i].target >> data.queries[i].reference >> reachable;
            data.queries[i].reachable = reachable != 0;
        } else {
            in >> data.queries[i].source >> data.queries[i].target;
        }
    }
    if (!in) throw std::runtime_error("truncated input");
    return data;
}

enum class Scheduler {
    alternate,
    lower_key,
    smaller_queue,
    lower_key_then_queue,
};

class HCBSQuery {
public:
    explicit HCBSQuery(const ContractionHierarchy& hierarchy)
        : ch(hierarchy),
          forward_queue(hierarchy.node_count()), backward_queue(hierarchy.node_count()),
          forward_distance(hierarchy.node_count()), backward_distance(hierarchy.node_count()),
          forward_seen(hierarchy.node_count()), backward_seen(hierarchy.node_count()) {}

    unsigned run(unsigned external_source, unsigned external_target, Scheduler scheduler) {
        begin_query();
        const unsigned source = ch.rank[external_source];
        const unsigned target = ch.rank[external_target];
        mark_forward(source, 0);
        mark_backward(target, 0);
        unsigned incumbent = inf_weight;
        bool alternate_forward = true;

        for (;;) {
            const bool forward_finished = forward_queue.empty() || forward_queue.peek().key >= incumbent;
            const bool backward_finished = backward_queue.empty() || backward_queue.peek().key >= incumbent;
            if (forward_finished && backward_finished) break;

            bool use_forward;
            if (forward_finished) {
                use_forward = false;
            } else if (backward_finished) {
                use_forward = true;
            } else {
                use_forward = choose_forward(scheduler, alternate_forward);
            }

            if (use_forward) {
                settle_forward(incumbent);
            } else {
                settle_backward(incumbent);
            }
            alternate_forward = !use_forward;
        }
        return incumbent;
    }

private:
    const ContractionHierarchy& ch;
    MinIDQueue forward_queue, backward_queue;
    std::vector<unsigned> forward_distance, backward_distance;
    std::vector<uint32_t> forward_seen, backward_seen;
    uint32_t epoch = 0;

    void begin_query() {
        forward_queue.clear();
        backward_queue.clear();
        ++epoch;
        if (epoch == 0) {
            std::fill(forward_seen.begin(), forward_seen.end(), 0);
            std::fill(backward_seen.begin(), backward_seen.end(), 0);
            epoch = 1;
        }
    }

    bool seen_forward(unsigned node) const { return forward_seen[node] == epoch; }
    bool seen_backward(unsigned node) const { return backward_seen[node] == epoch; }

    void mark_forward(unsigned node, unsigned distance) {
        forward_seen[node] = epoch;
        forward_distance[node] = distance;
        forward_queue.push({node, distance});
    }

    void mark_backward(unsigned node, unsigned distance) {
        backward_seen[node] = epoch;
        backward_distance[node] = distance;
        backward_queue.push({node, distance});
    }

    static unsigned degree(const ContractionHierarchy::Side& side, unsigned node) {
        return side.first_out[node + 1] - side.first_out[node];
    }

    bool choose_forward(Scheduler scheduler, bool alternate_forward) const {
        switch (scheduler) {
        case Scheduler::alternate:
            return alternate_forward;
        case Scheduler::lower_key:
            return forward_queue.peek().key <= backward_queue.peek().key;
        case Scheduler::smaller_queue:
            return forward_queue.size() <= backward_queue.size();
        case Scheduler::lower_key_then_queue: {
            const auto fk = forward_queue.peek().key;
            const auto bk = backward_queue.peek().key;
            if (fk != bk) return fk < bk;
            const auto fn = forward_queue.peek().id;
            const auto bn = backward_queue.peek().id;
            const auto fw = degree(ch.forward, fn);
            const auto bw = degree(ch.backward, bn);
            if (fw != bw) return fw < bw;
            return forward_queue.size() <= backward_queue.size();
        }
        }
        return alternate_forward;
    }

    bool stall_forward(unsigned node) const {
        const unsigned current = forward_distance[node];
        for (unsigned arc = ch.backward.first_out[node]; arc < ch.backward.first_out[node + 1]; ++arc) {
            const unsigned x = ch.backward.head[arc];
            if (seen_forward(x)) {
                const unsigned w = ch.backward.weight[arc];
                if (forward_distance[x] <= current && w <= current - forward_distance[x]) return true;
            }
        }
        return false;
    }

    bool stall_backward(unsigned node) const {
        const unsigned current = backward_distance[node];
        for (unsigned arc = ch.forward.first_out[node]; arc < ch.forward.first_out[node + 1]; ++arc) {
            const unsigned x = ch.forward.head[arc];
            if (seen_backward(x)) {
                const unsigned w = ch.forward.weight[arc];
                if (backward_distance[x] <= current && w <= current - backward_distance[x]) return true;
            }
        }
        return false;
    }

    void relax_forward(unsigned node, unsigned distance) {
        for (unsigned arc = ch.forward.first_out[node]; arc < ch.forward.first_out[node + 1]; ++arc) {
            const unsigned head = ch.forward.head[arc];
            const unsigned weight = ch.forward.weight[arc];
            if (distance >= inf_weight - weight) continue;
            const unsigned candidate = distance + weight;
            if (!seen_forward(head)) {
                mark_forward(head, candidate);
            } else if (candidate < forward_distance[head]) {
                forward_distance[head] = candidate;
                if (forward_queue.contains_id(head)) {
                    forward_queue.decrease_key({head, candidate});
                } else {
                    forward_queue.push({head, candidate});
                }
            }
        }
    }

    void relax_backward(unsigned node, unsigned distance) {
        for (unsigned arc = ch.backward.first_out[node]; arc < ch.backward.first_out[node + 1]; ++arc) {
            const unsigned head = ch.backward.head[arc];
            const unsigned weight = ch.backward.weight[arc];
            if (distance >= inf_weight - weight) continue;
            const unsigned candidate = distance + weight;
            if (!seen_backward(head)) {
                mark_backward(head, candidate);
            } else if (candidate < backward_distance[head]) {
                backward_distance[head] = candidate;
                if (backward_queue.contains_id(head)) {
                    backward_queue.decrease_key({head, candidate});
                } else {
                    backward_queue.push({head, candidate});
                }
            }
        }
    }

    void settle_forward(unsigned& incumbent) {
        const IDKeyPair item = forward_queue.pop();
        const unsigned node = item.id;
        const unsigned distance = item.key;
        if (seen_backward(node) && backward_distance[node] < inf_weight - distance) {
            incumbent = std::min(incumbent, distance + backward_distance[node]);
        }
        if (!stall_forward(node)) relax_forward(node, distance);
    }

    void settle_backward(unsigned& incumbent) {
        const IDKeyPair item = backward_queue.pop();
        const unsigned node = item.id;
        const unsigned distance = item.key;
        if (seen_forward(node) && forward_distance[node] < inf_weight - distance) {
            incumbent = std::min(incumbent, distance + forward_distance[node]);
        }
        if (!stall_backward(node)) relax_backward(node, distance);
    }
};

struct Summary {
    std::string name;
    uint64_t median_ns = 0;
    uint64_t p95_ns = 0;
    uint64_t p99_ns = 0;
    bool correct = true;
};

static uint64_t percentile(std::vector<uint64_t> values, double p) {
    if (values.empty()) return 0;
    std::sort(values.begin(), values.end());
    const size_t index = static_cast<size_t>(p * static_cast<double>(values.size() - 1));
    return values[index];
}

template<class Run>
static Summary benchmark_variant(const std::string& name, const Input& data, unsigned repeats, Run&& run) {
    std::vector<uint64_t> medians;
    medians.reserve(data.queries.size());
    bool correct = true;
    for (const auto& q : data.queries) {
        std::vector<uint64_t> samples;
        samples.reserve(repeats);
        unsigned result = run(q);
        for (unsigned r = 0; r < repeats; ++r) {
            const auto start = std::chrono::steady_clock::now();
            result = run(q);
            const auto end = std::chrono::steady_clock::now();
            samples.push_back(static_cast<uint64_t>(std::chrono::duration_cast<std::chrono::nanoseconds>(end - start).count()));
        }
        std::sort(samples.begin(), samples.end());
        medians.push_back(samples[samples.size() / 2]);
        const bool reachable = result != inf_weight;
        if (reachable != q.reachable || (reachable && result != q.reference)) correct = false;
    }
    Summary s;
    s.name = name;
    s.median_ns = percentile(medians, .50);
    s.p95_ns = percentile(medians, .95);
    s.p99_ns = percentile(medians, .99);
    s.correct = correct;
    return s;
}

int main(int argc, char** argv) {
    if (argc < 2 || argc > 3) {
        std::cerr << "usage: routingkit_hcbs_query INPUT [REPEATS]\n";
        return 2;
    }
    const unsigned repeats = argc == 3 ? static_cast<unsigned>(std::stoul(argv[2])) : 31;
    Input data = read_input(argv[1]);
    const auto prep_start = std::chrono::steady_clock::now();
    const ContractionHierarchy ch = ContractionHierarchy::build(data.node_count, data.tail, data.head, data.weight);
    const auto prep_end = std::chrono::steady_clock::now();
    const auto prep_ns = std::chrono::duration_cast<std::chrono::nanoseconds>(prep_end - prep_start).count();

    ContractionHierarchyQuery oracle(ch);
    for (auto& q : data.queries) {
        oracle.reset().add_source(q.source).add_target(q.target).run();
        const unsigned result = oracle.get_distance();
        const bool reachable = result != inf_weight;
        if (data.has_reference && (reachable != q.reachable || (reachable && result != q.reference))) {
            std::cerr << "RoutingKit standard query disagrees with supplied reference\n";
            return 1;
        }
        if (!data.has_reference) {
            q.reference = result;
            q.reachable = reachable;
        }
    }

    ContractionHierarchyQuery standard(ch);
    HCBSQuery hcbs(ch);
    auto standard_summary = benchmark_variant("routingkit-alternate", data, repeats, [&](const Query& q) {
        standard.reset().add_source(q.source).add_target(q.target).run();
        return standard.get_distance();
    });
    auto alternate = benchmark_variant("hcbs-reimplemented-alternate", data, repeats, [&](const Query& q) {
        return hcbs.run(q.source, q.target, Scheduler::alternate);
    });
    auto lower_key = benchmark_variant("hcbs-lower-key", data, repeats, [&](const Query& q) {
        return hcbs.run(q.source, q.target, Scheduler::lower_key);
    });
    auto smaller_queue = benchmark_variant("hcbs-smaller-queue", data, repeats, [&](const Query& q) {
        return hcbs.run(q.source, q.target, Scheduler::smaller_queue);
    });
    auto lower_key_queue = benchmark_variant("hcbs-lower-key-queue", data, repeats, [&](const Query& q) {
        return hcbs.run(q.source, q.target, Scheduler::lower_key_then_queue);
    });

    std::cout << "hcbs-preprocess-ns=" << prep_ns << " queries=" << data.queries.size() << " repeats=" << repeats << "\n";
    for (const auto& s : {standard_summary, alternate, lower_key, smaller_queue, lower_key_queue}) {
        std::cout << s.name << " median_ns=" << s.median_ns << " p95_ns=" << s.p95_ns
                  << " p99_ns=" << s.p99_ns << " correct=" << (s.correct ? "true" : "false") << "\n";
        if (!s.correct) return 1;
    }
    return 0;
}

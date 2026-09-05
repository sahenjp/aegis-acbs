#include <routingkit/contraction_hierarchy.h>
#include <routingkit/constants.h>

#include <chrono>
#include <cctype>
#include <cstdint>
#include <fstream>
#include <iostream>
#include <stdexcept>
#include <string>
#include <vector>

using namespace RoutingKit;
using Clock = std::chrono::steady_clock;

static long long positive_ns(Clock::duration d) {
    auto value = std::chrono::duration_cast<std::chrono::nanoseconds>(d).count();
    return value < 1 ? 1 : value;
}

static bool valid_fingerprint(const std::string& value) {
    if (value.size() != 64) return false;
    for (unsigned char c : value) {
        if (!std::isxdigit(c)) return false;
    }
    return true;
}

int main(int argc, char** argv) {
    try {
        if (argc != 2) {
            std::cerr << "usage: aegis-routingkit-ch-server GRAPH\n";
            return 2;
        }
        std::ifstream in(argv[1]);
        if (!in) throw std::runtime_error("cannot open graph input");

        std::string magic;
        in >> magic;
        if (magic != "AEGIS_ROUTINGKIT_CH_SERVER_V2") {
            throw std::runtime_error("bad graph magic; re-export with current aegis-routingkit-export");
        }
        std::string fingerprint;
        in >> fingerprint;
        if (!valid_fingerprint(fingerprint)) {
            throw std::runtime_error("invalid graph fingerprint");
        }
        unsigned node_count = 0;
        std::size_t edge_count = 0;
        in >> node_count >> edge_count;
        if (!in || node_count == 0) {
            throw std::runtime_error("invalid graph header");
        }

        std::vector<unsigned> tail(edge_count), head(edge_count), weight(edge_count);
        for (std::size_t i = 0; i < edge_count; ++i) {
            unsigned long long w = 0;
            in >> tail[i] >> head[i] >> w;
            if (!in || tail[i] >= node_count || head[i] >= node_count || w == 0 || w >= inf_weight) {
                throw std::runtime_error("invalid graph edge");
            }
            weight[i] = static_cast<unsigned>(w);
        }

        auto prep_begin = Clock::now();
        ContractionHierarchy ch = ContractionHierarchy::build(node_count, tail, head, weight);
        const long long preprocess_ns = positive_ns(Clock::now() - prep_begin);
        ContractionHierarchyQuery query(ch);

        std::cout << "READY " << preprocess_ns << ' ' << fingerprint << '\n' << std::flush;

        std::string command;
        while (std::cin >> command) {
            if (command == "X") {
                return 0;
            }
            if (command != "Q") {
                std::cout << "E unknown-command\n" << std::flush;
                continue;
            }
            unsigned source = 0, target = 0;
            if (!(std::cin >> source >> target)) {
                throw std::runtime_error("truncated query");
            }
            if (source >= node_count || target >= node_count) {
                std::cout << "E node-out-of-range\n" << std::flush;
                continue;
            }

            const auto begin = Clock::now();
            query.reset().add_source(source).add_target(target).run();
            const unsigned distance = query.get_distance();
            if (distance == inf_weight) {
                std::cout << "U " << positive_ns(Clock::now() - begin) << '\n' << std::flush;
                continue;
            }
            const auto path = query.get_node_path();
            const long long duration_ns = positive_ns(Clock::now() - begin);
            if (path.empty() || path.front() != source || path.back() != target) {
                std::cout << "E invalid-path\n" << std::flush;
                continue;
            }
            std::cout << "R " << distance << ' ' << duration_ns << ' ' << path.size();
            for (unsigned node : path) {
                std::cout << ' ' << node;
            }
            std::cout << '\n' << std::flush;
        }
        return 0;
    } catch (const std::exception& e) {
        std::cerr << "aegis-routingkit-ch-server: " << e.what() << '\n';
        return 1;
    }
}

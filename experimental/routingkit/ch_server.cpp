#include <routingkit/contraction_hierarchy.h>
#include <routingkit/constants.h>

#include <chrono>
#include <cctype>
#include <cstdint>
#include <filesystem>
#include <fstream>
#include <iostream>
#include <memory>
#include <stdexcept>
#include <string>
#include <system_error>
#include <vector>

using namespace RoutingKit;
using Clock = std::chrono::steady_clock;
namespace fs = std::filesystem;

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

static bool load_cache_meta(
    const std::string& path,
    const std::string& fingerprint,
    long long& rebuild_ns
) {
    std::ifstream in(path);
    if (!in) return false;
    std::string magic, cached_fingerprint;
    long long cached_rebuild_ns = 0;
    in >> magic >> cached_fingerprint >> cached_rebuild_ns;
    if (!in || magic != "AEGIS_ROUTINGKIT_CH_CACHE_V1" ||
        cached_fingerprint != fingerprint || cached_rebuild_ns < 1) {
        return false;
    }
    rebuild_ns = cached_rebuild_ns;
    return true;
}

static unsigned long long file_size_or_zero(const std::string& path) {
    std::error_code ec;
    const auto size = fs::file_size(path, ec);
    return ec ? 0 : static_cast<unsigned long long>(size);
}

static void replace_file(const std::string& tmp, const std::string& dst) {
    std::error_code ec;
    fs::remove(dst, ec);
    ec.clear();
    fs::rename(tmp, dst, ec);
    if (ec) throw std::runtime_error("cannot atomically replace cache file: " + ec.message());
}

static void best_effort_save_cache(
    const ContractionHierarchy& ch,
    const std::string& cache_path,
    const std::string& meta_path,
    const std::string& fingerprint,
    long long rebuild_ns
) {
    const auto nonce = std::to_string(
        std::chrono::duration_cast<std::chrono::nanoseconds>(Clock::now().time_since_epoch()).count()
    );
    const std::string cache_tmp = cache_path + ".tmp." + nonce;
    const std::string meta_tmp = meta_path + ".tmp." + nonce;
    try {
        ch.save_file(cache_tmp);
        {
            std::ofstream meta(meta_tmp, std::ios::trunc);
            if (!meta) throw std::runtime_error("cannot create CH cache metadata");
            meta << "AEGIS_ROUTINGKIT_CH_CACHE_V1\n"
                 << fingerprint << '\n'
                 << rebuild_ns << '\n';
            if (!meta) throw std::runtime_error("cannot write CH cache metadata");
        }
        // Publish the data first and the fingerprint-bearing metadata last. A
        // crash between the two leaves a cache that will not be trusted.
        replace_file(cache_tmp, cache_path);
        replace_file(meta_tmp, meta_path);
    } catch (const std::exception& e) {
        std::error_code ignored;
        fs::remove(cache_tmp, ignored);
        fs::remove(meta_tmp, ignored);
        std::cerr << "aegis-routingkit-ch-server: cache-save-warning: " << e.what() << '\n';
    }
}

int main(int argc, char** argv) {
    try {
        if (argc != 2) {
            std::cerr << "usage: aegis-routingkit-ch-server GRAPH\n";
            return 2;
        }
        const std::string graph_path = argv[1];
        const std::string cache_path = graph_path + ".ch-index";
        const std::string meta_path = cache_path + ".meta";

        std::ifstream in(graph_path);
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

        std::unique_ptr<ContractionHierarchy> ch;
        bool cache_hit = false;
        long long startup_ns = 0;
        long long rebuild_ns = 0;

        // CH files are intentionally treated as trusted local cache only. The
        // graph fingerprint binds the cache to the exact Aegis from/to/cost
        // graph, but it is not a sandbox for attacker-controlled CH binaries.
        if (file_size_or_zero(cache_path) > 0 && load_cache_meta(meta_path, fingerprint, rebuild_ns)) {
            try {
                const auto load_begin = Clock::now();
                auto loaded = ContractionHierarchy::load_file(cache_path);
                startup_ns = positive_ns(Clock::now() - load_begin);
                if (loaded.node_count() != node_count) {
                    throw std::runtime_error("cached CH node count mismatch");
                }
                ch = std::make_unique<ContractionHierarchy>(std::move(loaded));
                cache_hit = true;
            } catch (const std::exception& e) {
                std::cerr << "aegis-routingkit-ch-server: cache-load-warning: " << e.what() << '\n';
                ch.reset();
                cache_hit = false;
                rebuild_ns = 0;
            }
        }

        if (!ch) {
            std::vector<unsigned> tail(edge_count), head(edge_count), weight(edge_count);
            for (std::size_t i = 0; i < edge_count; ++i) {
                unsigned long long w = 0;
                in >> tail[i] >> head[i] >> w;
                if (!in || tail[i] >= node_count || head[i] >= node_count || w == 0 || w >= inf_weight) {
                    throw std::runtime_error("invalid graph edge");
                }
                weight[i] = static_cast<unsigned>(w);
            }

            const auto prep_begin = Clock::now();
            auto built = ContractionHierarchy::build(node_count, tail, head, weight);
            rebuild_ns = positive_ns(Clock::now() - prep_begin);
            startup_ns = rebuild_ns;
            ch = std::make_unique<ContractionHierarchy>(std::move(built));
            best_effort_save_cache(*ch, cache_path, meta_path, fingerprint, rebuild_ns);
        }

        const unsigned long long index_bytes = file_size_or_zero(cache_path);
        ContractionHierarchyQuery query(*ch);

        // V3 separates actual startup cost from the full rebuild cost. On a
        // cold start both are equal. On a trusted cache hit startup is only the
        // load time while metric changes still pay rebuild_ns.
        std::cout << "READY " << startup_ns << ' ' << rebuild_ns << ' '
                  << (cache_hit ? 1 : 0) << ' ' << index_bytes << ' '
                  << fingerprint << '\n' << std::flush;

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

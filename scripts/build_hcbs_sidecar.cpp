#include <routingkit/contraction_hierarchy.h>

#include <array>
#include <cstdint>
#include <fstream>
#include <iostream>
#include <limits>
#include <stdexcept>
#include <string>
#include <vector>

using RoutingKit::ContractionHierarchy;

static void write_u32(std::ostream& out, uint32_t value) {
    const std::array<unsigned char, 4> bytes = {
        static_cast<unsigned char>(value),
        static_cast<unsigned char>(value >> 8),
        static_cast<unsigned char>(value >> 16),
        static_cast<unsigned char>(value >> 24),
    };
    out.write(reinterpret_cast<const char*>(bytes.data()), bytes.size());
}

static void write_u64(std::ostream& out, uint64_t value) {
    std::array<unsigned char, 8> bytes{};
    for (unsigned i = 0; i < 8; ++i) bytes[i] = static_cast<unsigned char>(value >> (8 * i));
    out.write(reinterpret_cast<const char*>(bytes.data()), bytes.size());
}

static unsigned hex_nibble(char c) {
    if (c >= '0' && c <= '9') return static_cast<unsigned>(c - '0');
    if (c >= 'a' && c <= 'f') return static_cast<unsigned>(c - 'a' + 10);
    if (c >= 'A' && c <= 'F') return static_cast<unsigned>(c - 'A' + 10);
    throw std::runtime_error("invalid digest hex");
}

static std::array<unsigned char, 32> parse_digest(const std::string& text) {
    if (text.size() != 64) throw std::runtime_error("digest must contain 64 hex characters");
    std::array<unsigned char, 32> digest{};
    for (unsigned i = 0; i < digest.size(); ++i) {
        digest[i] = static_cast<unsigned char>((hex_nibble(text[2*i]) << 4) | hex_nibble(text[2*i+1]));
    }
    return digest;
}

static void write_vector(std::ostream& out, const std::vector<unsigned>& values) {
    for (unsigned value : values) write_u32(out, value);
}

static void write_side(std::ostream& out, const ContractionHierarchy::Side& side) {
    write_vector(out, side.first_out);
    write_vector(out, side.head);
    write_vector(out, side.weight);
}

int main(int argc, char** argv) {
    if (argc != 3) {
        std::cerr << "usage: build_hcbs_sidecar INPUT.txt OUTPUT.hcbs\n";
        return 2;
    }
    std::ifstream in(argv[1]);
    if (!in) throw std::runtime_error("cannot open sidecar input");
    std::string magic;
    std::string digest_text;
    unsigned node_count = 0;
    uint64_t edge_count = 0;
    in >> magic >> digest_text >> node_count >> edge_count;
    if (magic != "AEGIS_HCBS_SIDECAR_INPUT_V1") throw std::runtime_error("bad sidecar input magic");
    if (edge_count > static_cast<uint64_t>(std::numeric_limits<unsigned>::max())) {
        throw std::runtime_error("edge count does not fit RoutingKit input vectors");
    }
    const auto digest = parse_digest(digest_text);
    std::vector<unsigned> tail(edge_count), head(edge_count), weight(edge_count);
    for (uint64_t i = 0; i < edge_count; ++i) {
        in >> tail[i] >> head[i] >> weight[i];
        if (!in) throw std::runtime_error("truncated sidecar edge list");
        if (tail[i] >= node_count || head[i] >= node_count || weight[i] >= 2147483647u) {
            throw std::runtime_error("sidecar input edge out of range");
        }
    }

    const ContractionHierarchy ch = ContractionHierarchy::build(node_count, tail, head, weight);
    if (ch.rank.size() != node_count || ch.forward.first_out.size() != node_count + 1 ||
        ch.backward.first_out.size() != node_count + 1) {
        throw std::runtime_error("unexpected RoutingKit CH dimensions");
    }

    std::ofstream out(argv[2], std::ios::binary | std::ios::trunc);
    if (!out) throw std::runtime_error("cannot create HCBS sidecar");
    const std::array<char, 8> output_magic = {'A','E','G','H','C','B','0','1'};
    out.write(output_magic.data(), output_magic.size());
    out.write(reinterpret_cast<const char*>(digest.data()), digest.size());
    write_u32(out, node_count);
    write_u64(out, ch.forward.head.size());
    write_u64(out, ch.backward.head.size());
    write_vector(out, ch.rank);
    write_side(out, ch.forward);
    write_side(out, ch.backward);
    out.flush();
    if (!out) throw std::runtime_error("failed while writing HCBS sidecar");

    std::cout << "hcbs sidecar: nodes=" << node_count
              << " forward_arcs=" << ch.forward.head.size()
              << " backward_arcs=" << ch.backward.head.size() << "\n";
    return 0;
}

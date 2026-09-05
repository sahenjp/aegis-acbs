#include <cuda_runtime.h>

#include <algorithm>
#include <cstddef>
#include <cstdint>
#include <iostream>
#include <limits>
#include <stdexcept>
#include <string>
#include <vector>

struct Gate {
    int left;
    int right;
};

static void cuda_check(cudaError_t err, const char* where) {
    if (err != cudaSuccess) {
        throw std::runtime_error(std::string(where) + ": " + cudaGetErrorString(err));
    }
}

__global__ void evaluate_chunk(
    int inputs,
    int gate_count,
    int output,
    const Gate* gates,
    std::uint64_t start,
    std::uint64_t count,
    unsigned char* workspace,
    unsigned long long* best) {
    const std::uint64_t local = static_cast<std::uint64_t>(blockIdx.x) * blockDim.x + threadIdx.x;
    if (local >= count) return;

    const std::uint64_t assignment = start + local;
    const int signal_count = inputs + gate_count;
    unsigned char* signals = workspace + local * static_cast<std::uint64_t>(signal_count);

    for (int i = 0; i < inputs; ++i) {
        const int shift = inputs - 1 - i;
        signals[i] = static_cast<unsigned char>((assignment >> shift) & 1ULL);
    }
    for (int i = 0; i < gate_count; ++i) {
        const Gate g = gates[i];
        signals[inputs + i] = static_cast<unsigned char>(!(signals[g.left] && signals[g.right]));
    }
    if (signals[output]) {
        atomicMin(best, static_cast<unsigned long long>(assignment));
    }
}

int main(int argc, char** argv) {
    try {
        std::uint64_t requested_chunk = 1ULL << 20;
        for (int i = 1; i < argc; ++i) {
            const std::string arg = argv[i];
            if (arg == "--chunk" && i + 1 < argc) {
                requested_chunk = std::stoull(argv[++i]);
            } else {
                std::cerr << "usage: aegis-circuitsat-cuda [--chunk ASSIGNMENTS]\n";
                return 2;
            }
        }
        if (requested_chunk == 0) throw std::runtime_error("chunk must be positive");

        std::string magic;
        if (!(std::cin >> magic) || magic != "AEGIS_CIRCUITSAT_CUDA_V1") {
            throw std::runtime_error("bad input magic");
        }
        int inputs = 0, gate_count = 0, output = 0;
        if (!(std::cin >> inputs >> gate_count >> output)) {
            throw std::runtime_error("missing circuit header");
        }
        if (inputs < 0 || inputs > 62 || gate_count < 0) {
            throw std::runtime_error("unsupported circuit dimensions");
        }
        const int signal_count = inputs + gate_count;
        if (signal_count <= 0 || output < 0 || output >= signal_count) {
            throw std::runtime_error("output is out of range");
        }

        std::vector<Gate> gates(static_cast<std::size_t>(gate_count));
        for (int i = 0; i < gate_count; ++i) {
            if (!(std::cin >> gates[i].left >> gates[i].right)) {
                throw std::runtime_error("truncated gate list");
            }
            const int limit = inputs + i;
            if (gates[i].left < 0 || gates[i].left >= limit || gates[i].right < 0 || gates[i].right >= limit) {
                throw std::runtime_error("gate references unavailable signal");
            }
        }

        Gate* device_gates = nullptr;
        if (gate_count > 0) {
            cuda_check(cudaMalloc(reinterpret_cast<void**>(&device_gates), gates.size() * sizeof(Gate)), "cudaMalloc gates");
            cuda_check(cudaMemcpy(device_gates, gates.data(), gates.size() * sizeof(Gate), cudaMemcpyHostToDevice), "cudaMemcpy gates");
        }

        unsigned long long* device_best = nullptr;
        cuda_check(cudaMalloc(reinterpret_cast<void**>(&device_best), sizeof(unsigned long long)), "cudaMalloc best");

        std::size_t free_bytes = 0, total_bytes = 0;
        cuda_check(cudaMemGetInfo(&free_bytes, &total_bytes), "cudaMemGetInfo");
        (void)total_bytes;
        const std::size_t usable_bytes = free_bytes * 3 / 4;
        const std::uint64_t memory_chunk = static_cast<std::uint64_t>(usable_bytes / static_cast<std::size_t>(signal_count));
        if (memory_chunk == 0) {
            throw std::runtime_error("not enough GPU memory for one assignment workspace");
        }
        const std::uint64_t chunk = std::min(requested_chunk, memory_chunk);

        const std::uint64_t total = 1ULL << inputs;
        std::uint64_t checked = 0;
        const int threads = 256;
        for (std::uint64_t start = 0; start < total; start += chunk) {
            const std::uint64_t count = std::min(chunk, total - start);
            if (count > std::numeric_limits<std::size_t>::max() / static_cast<std::size_t>(signal_count)) {
                throw std::runtime_error("workspace size overflow");
            }
            const std::size_t workspace_bytes = static_cast<std::size_t>(count) * static_cast<std::size_t>(signal_count);
            unsigned char* workspace = nullptr;
            cuda_check(cudaMalloc(reinterpret_cast<void**>(&workspace), workspace_bytes), "cudaMalloc workspace");

            const unsigned long long none = std::numeric_limits<unsigned long long>::max();
            cuda_check(cudaMemcpy(device_best, &none, sizeof(none), cudaMemcpyHostToDevice), "reset best");
            const std::uint64_t block_count = (count + static_cast<std::uint64_t>(threads) - 1) / static_cast<std::uint64_t>(threads);
            if (block_count > std::numeric_limits<unsigned>::max()) {
                cudaFree(workspace);
                throw std::runtime_error("CUDA grid would exceed supported launch range");
            }
            evaluate_chunk<<<static_cast<unsigned>(block_count), threads>>>(
                inputs, gate_count, output, device_gates, start, count, workspace, device_best);
            cuda_check(cudaGetLastError(), "kernel launch");
            cuda_check(cudaDeviceSynchronize(), "kernel execution");

            unsigned long long best = none;
            cuda_check(cudaMemcpy(&best, device_best, sizeof(best), cudaMemcpyDeviceToHost), "read best");
            cuda_check(cudaFree(workspace), "cudaFree workspace");
            checked += count;
            if (best != none) {
                std::cout << "SAT " << best << " " << checked << "\n";
                cudaFree(device_best);
                if (device_gates) cudaFree(device_gates);
                return 0;
            }
        }

        std::cout << "UNSAT " << checked << "\n";
        cudaFree(device_best);
        if (device_gates) cudaFree(device_gates);
        return 0;
    } catch (const std::exception& e) {
        std::cerr << "aegis-circuitsat-cuda: " << e.what() << "\n";
        return 1;
    }
}

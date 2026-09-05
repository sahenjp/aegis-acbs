#include <cuda_runtime.h>

#include <algorithm>
#include <cstdint>
#include <cstdlib>
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
        std::uint64_t chunk = 1ULL << 20;
        for (int i = 1; i < argc; ++i) {
            const std::string arg = argv[i];
            if (arg == "--chunk" && i + 1 < argc) {
                chunk = std::stoull(argv[++i]);
            } else {
                std::cerr << "usage: aegis-circuitsat-cuda [--chunk ASSIGNMENTS]\n";
                return 2;
            }
        }
        if (chunk == 0) throw std::runtime_error("chunk must be positive");

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
        if (output < 0 || output >= signal_count) {
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
            cuda_check(cudaMalloc(&device_gates, gates.size() * sizeof(Gate)), "cudaMalloc gates");
            cuda_check(cudaMemcpy(device_gates, gates.data(), gates.size() * sizeof(Gate), cudaMemcpyHostToDevice), "cudaMemcpy gates");
        }

        const std::uint64_t total = 1ULL << inputs;
        unsigned long long* device_best = nullptr;
        cuda_check(cudaMalloc(&device_best, sizeof(unsigned long long)), "cudaMalloc best");

        std::uint64_t checked = 0;
        const int threads = 256;
        for (std::uint64_t start = 0; start < total; start += chunk) {
            const std::uint64_t count = std::min(chunk, total - start);
            const std::uint64_t workspace_bytes = count * static_cast<std::uint64_t>(std::max(1, signal_count));
            unsigned char* workspace = nullptr;
            cuda_check(cudaMalloc(&workspace, static_cast<std::size_t>(workspace_bytes)), "cudaMalloc workspace");

            const unsigned long long none = std::numeric_limits<unsigned long long>::max();
            cuda_check(cudaMemcpy(device_best, &none, sizeof(none), cudaMemcpyHostToDevice), "reset best");
            const unsigned blocks = static_cast<unsigned>((count + threads - 1) / threads);
            evaluate_chunk<<<blocks, threads>>>(inputs, gate_count, output, device_gates, start, count, workspace, device_best);
            cuda_check(cudaGetLastError(), "kernel launch");
            cuda_check(cudaDeviceSynchronize(), "kernel execution");

            unsigned long long best = none;
            cuda_check(cudaMemcpy(&best, device_best, sizeof(best), cudaMemcpyDeviceToHost), "read best");
            cudaFree(workspace);
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

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/lasder-ca/aegis-acbs/internal/complexity/circuitsat"
	"github.com/lasder-ca/aegis-acbs/internal/complexity/mcsp"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "mcsp":
		err = runMCSP(os.Args[2:])
	case "catalog":
		err = runCatalog(os.Args[2:])
	case "circuit-sat":
		err = runCircuitSAT(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fatal(err)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage:")
	fmt.Fprintln(os.Stderr, "  aegis-lab mcsp --inputs N --target HEX [--max-gates N] [--max-states N] [--workers N] [--verify-minimal]")
	fmt.Fprintln(os.Stderr, "  aegis-lab catalog --inputs N [--max-gates N] [--max-states N] [--workers N] [--include-circuits]")
	fmt.Fprintln(os.Stderr, "  aegis-lab circuit-sat --circuit FILE [--backend cpu|cuda] [--workers N] [--cuda-bin FILE]")
}

func runMCSP(args []string) error {
	fs := flag.NewFlagSet("mcsp", flag.ContinueOnError)
	inputs := fs.Int("inputs", 0, "number of Boolean inputs (1..6)")
	targetText := fs.String("target", "", "truth table as hexadecimal, for example 0x8")
	maxGates := fs.Int("max-gates", 8, "maximum NAND gates to search")
	maxStates := fs.Int("max-states", 1_000_000, "maximum canonical states")
	workers := fs.Int("workers", runtime.GOMAXPROCS(0), "parallel frontier expansion workers")
	verifyMinimal := fs.Bool("verify-minimal", false, "independently re-run all smaller depths")
	timeout := fs.Duration("timeout", 30*time.Second, "search timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *inputs < 1 || *inputs > 6 || *targetText == "" {
		return errors.New("mcsp requires --inputs 1..6 and --target")
	}
	target, err := parseHex(*targetText)
	if err != nil {
		return fmt.Errorf("invalid target: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	result, err := mcsp.Solve(ctx, *inputs, target, mcsp.Config{MaxGates: *maxGates, MaxStates: *maxStates, Workers: *workers})
	if err != nil {
		return err
	}
	if err := mcsp.Verify(target, result.Circuit); err != nil {
		return err
	}
	if *verifyMinimal {
		if err := mcsp.VerifyMinimal(ctx, target, result.Circuit, *maxStates); err != nil {
			return err
		}
	}
	return encodeJSON(result)
}

func runCatalog(args []string) error {
	fs := flag.NewFlagSet("catalog", flag.ContinueOnError)
	inputs := fs.Int("inputs", 0, "number of Boolean inputs (1..4)")
	maxGates := fs.Int("max-gates", 8, "maximum NAND gates to enumerate")
	maxStates := fs.Int("max-states", 2_000_000, "maximum canonical states")
	workers := fs.Int("workers", runtime.GOMAXPROCS(0), "parallel frontier expansion workers")
	includeCircuits := fs.Bool("include-circuits", false, "include a representative minimal circuit for every discovered function")
	timeout := fs.Duration("timeout", 2*time.Minute, "catalog timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *inputs < 1 || *inputs > 4 {
		return errors.New("catalog requires --inputs 1..4")
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	result, err := mcsp.Catalog(ctx, *inputs, mcsp.Config{MaxGates: *maxGates, MaxStates: *maxStates, Workers: *workers}, *includeCircuits)
	if err != nil && !errors.Is(err, mcsp.ErrStateLimit) {
		return err
	}
	if encodeErr := encodeJSON(result); encodeErr != nil {
		return encodeErr
	}
	if errors.Is(err, mcsp.ErrStateLimit) {
		fmt.Fprintln(os.Stderr, "aegis-lab: catalog stopped at the state limit; output is partial")
	}
	return nil
}

func runCircuitSAT(args []string) error {
	fs := flag.NewFlagSet("circuit-sat", flag.ContinueOnError)
	path := fs.String("circuit", "", "path to a NAND circuit JSON file")
	backend := fs.String("backend", "cpu", "exact backend: cpu or cuda")
	workers := fs.Int("workers", runtime.GOMAXPROCS(0), "CPU parallel 64-assignment block workers")
	cudaBin := fs.String("cuda-bin", "bin/aegis-circuitsat-cuda", "CUDA sidecar executable")
	cudaChunk := fs.Uint64("cuda-chunk", 1<<20, "assignments evaluated per CUDA allocation")
	crossCheckUnsat := fs.Bool("cross-check-unsat", false, "for CUDA UNSAT, repeat exhaustive CPU search before accepting the result")
	timeout := fs.Duration("timeout", 30*time.Second, "search timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *path == "" {
		return errors.New("circuit-sat requires --circuit")
	}
	data, err := os.ReadFile(*path)
	if err != nil {
		return err
	}
	var circuit circuitsat.Circuit
	if err := json.Unmarshal(data, &circuit); err != nil {
		return fmt.Errorf("decode circuit: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	var result circuitsat.Result
	switch *backend {
	case "cpu":
		result, err = circuitsat.Solve(ctx, circuit, circuitsat.Config{Workers: *workers})
	case "cuda":
		result, err = circuitsat.SolveCUDA(ctx, circuit, circuitsat.CUDAConfig{Binary: *cudaBin, Chunk: *cudaChunk})
	default:
		return fmt.Errorf("unknown circuit-sat backend %q", *backend)
	}
	if err != nil {
		return err
	}
	if result.Satisfiable {
		if err := circuitsat.VerifyAssignment(circuit, result.Assignment); err != nil {
			return err
		}
	} else if *backend == "cuda" && *crossCheckUnsat {
		cpuResult, cpuErr := circuitsat.Solve(ctx, circuit, circuitsat.Config{Workers: *workers})
		if cpuErr != nil {
			return fmt.Errorf("CUDA UNSAT cross-check failed: %w", cpuErr)
		}
		if cpuResult.Satisfiable {
			return errors.New("CUDA backend reported UNSAT but CPU reference found a witness")
		}
	}
	return encodeJSON(result)
}

func parseHex(text string) (uint64, error) {
	text = strings.TrimSpace(strings.ToLower(text))
	text = strings.TrimPrefix(text, "0x")
	return strconv.ParseUint(text, 16, 64)
}

func encodeJSON(value any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

func fatal(err error) {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		fmt.Fprintln(os.Stderr, "aegis-lab: search timed out")
	case errors.Is(err, context.Canceled):
		fmt.Fprintln(os.Stderr, "aegis-lab: search canceled")
	default:
		fmt.Fprintf(os.Stderr, "aegis-lab: %v\n", err)
	}
	os.Exit(1)
}

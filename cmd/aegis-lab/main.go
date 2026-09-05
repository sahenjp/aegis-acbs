package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/lasder-ca/aegis-acbs/internal/complexity/mcsp"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "mcsp" {
		fmt.Fprintln(os.Stderr, "usage: aegis-lab mcsp --inputs N --target HEX [--max-gates N] [--max-states N] [--verify-minimal]")
		os.Exit(2)
	}
	fs := flag.NewFlagSet("mcsp", flag.ExitOnError)
	inputs := fs.Int("inputs", 0, "number of Boolean inputs (1..6)")
	targetText := fs.String("target", "", "truth table as hexadecimal, for example 0x8")
	maxGates := fs.Int("max-gates", 8, "maximum NAND gates to search")
	maxStates := fs.Int("max-states", 1_000_000, "maximum canonical states")
	verifyMinimal := fs.Bool("verify-minimal", false, "independently re-run all smaller depths")
	timeout := fs.Duration("timeout", 30*time.Second, "search timeout")
	_ = fs.Parse(os.Args[2:])
	if *inputs < 1 || *inputs > 6 || *targetText == "" {
		fs.Usage()
		os.Exit(2)
	}
	target, err := strconv.ParseUint(strings.TrimPrefix(strings.ToLower(*targetText), "0x"), 16, 64)
	if err != nil {
		fatal(fmt.Errorf("invalid target: %w", err))
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	result, err := mcsp.Solve(ctx, *inputs, target, mcsp.Config{MaxGates: *maxGates, MaxStates: *maxStates})
	if err != nil {
		fatal(err)
	}
	if err := mcsp.Verify(target, result.Circuit); err != nil {
		fatal(err)
	}
	if *verifyMinimal {
		if err := mcsp.VerifyMinimal(ctx, target, result.Circuit, *maxStates); err != nil {
			fatal(err)
		}
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(result); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	if errors.Is(err, context.DeadlineExceeded) {
		fmt.Fprintln(os.Stderr, "aegis-lab: search timed out")
	} else {
		fmt.Fprintf(os.Stderr, "aegis-lab: %v\n", err)
	}
	os.Exit(1)
}

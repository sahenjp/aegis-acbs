package circuitsat

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
)

var ErrCUDAProtocol = errors.New("circuitsat: invalid CUDA sidecar response")

type CUDAConfig struct {
	Binary string `json:"binary"`
	Chunk  uint64 `json:"chunk"`
}

// SolveCUDA delegates exhaustive assignment evaluation to the optional CUDA
// sidecar. The sidecar is deliberately outside the normal Go build so CUDA is
// never a required dependency of Aegis. SAT witnesses are always re-evaluated
// by the Go verifier before they are accepted.
func SolveCUDA(ctx context.Context, c Circuit, cfg CUDAConfig) (Result, error) {
	if err := Validate(c); err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(cfg.Binary) == "" {
		return Result{}, errors.New("circuitsat: CUDA binary is required")
	}
	args := []string{}
	if cfg.Chunk > 0 {
		args = append(args, "--chunk", strconv.FormatUint(cfg.Chunk, 10))
	}
	cmd := exec.CommandContext(ctx, cfg.Binary, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return Result{}, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return Result{}, err
	}
	if err := cmd.Start(); err != nil {
		return Result{}, err
	}
	writeErr := writeCUDAProblem(stdin, c)
	_ = stdin.Close()
	response, readErr := io.ReadAll(stdout)
	stderrBytes, _ := io.ReadAll(stderr)
	waitErr := cmd.Wait()
	if writeErr != nil {
		return Result{}, writeErr
	}
	if readErr != nil {
		return Result{}, readErr
	}
	if waitErr != nil {
		message := strings.TrimSpace(string(stderrBytes))
		if message == "" {
			return Result{}, waitErr
		}
		return Result{}, fmt.Errorf("circuitsat: CUDA sidecar failed: %s: %w", message, waitErr)
	}
	result, err := parseCUDAResponse(string(response))
	if err != nil {
		return Result{}, err
	}
	if result.Satisfiable {
		if err := VerifyAssignment(c, result.Assignment); err != nil {
			return Result{}, fmt.Errorf("circuitsat: CUDA witness verification failed: %w", err)
		}
	}
	return result, nil
}

func writeCUDAProblem(w io.Writer, c Circuit) error {
	bw := bufio.NewWriter(w)
	if _, err := fmt.Fprintln(bw, "AEGIS_CIRCUITSAT_CUDA_V1"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(bw, "%d %d %d\n", c.Inputs, len(c.Gates), c.Output); err != nil {
		return err
	}
	for _, gate := range c.Gates {
		if _, err := fmt.Fprintf(bw, "%d %d\n", gate.Left, gate.Right); err != nil {
			return err
		}
	}
	return bw.Flush()
}

func parseCUDAResponse(text string) (Result, error) {
	fields := strings.Fields(text)
	if len(fields) < 2 {
		return Result{}, ErrCUDAProtocol
	}
	switch fields[0] {
	case "SAT":
		if len(fields) != 3 {
			return Result{}, ErrCUDAProtocol
		}
		assignment, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return Result{}, ErrCUDAProtocol
		}
		checked, err := strconv.ParseUint(fields[2], 10, 64)
		if err != nil {
			return Result{}, ErrCUDAProtocol
		}
		return Result{Satisfiable: true, Assignment: assignment, Stats: Stats{CheckedAssignments: checked}}, nil
	case "UNSAT":
		if len(fields) != 2 {
			return Result{}, ErrCUDAProtocol
		}
		checked, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return Result{}, ErrCUDAProtocol
		}
		return Result{Satisfiable: false, Stats: Stats{CheckedAssignments: checked, Complete: true}}, nil
	default:
		return Result{}, ErrCUDAProtocol
	}
}

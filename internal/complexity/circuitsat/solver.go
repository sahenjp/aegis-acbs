package circuitsat

import (
	"context"
	"errors"
	"fmt"
	"math/bits"
	"sync"
)

const MaxInputs = 62

var ErrTooManyInputs = fmt.Errorf("circuitsat: at most %d inputs are supported", MaxInputs)

type Gate struct {
	Left  int `json:"left"`
	Right int `json:"right"`
}

type Circuit struct {
	Inputs int    `json:"inputs"`
	Gates  []Gate `json:"gates"`
	Output int    `json:"output"`
}

type Config struct {
	Workers int `json:"workers"`
}

type Stats struct {
	CheckedAssignments uint64 `json:"checkedAssignments"`
	CheckedBlocks      uint64 `json:"checkedBlocks"`
	Workers            int    `json:"workers"`
	Complete           bool   `json:"complete"`
}

type Result struct {
	Satisfiable bool   `json:"satisfiable"`
	Assignment  uint64 `json:"assignment,omitempty"`
	Stats       Stats  `json:"stats"`
}

type blockResult struct {
	found      bool
	assignment uint64
	checked    uint64
}

func Validate(c Circuit) error {
	if c.Inputs < 0 || c.Inputs > MaxInputs {
		return ErrTooManyInputs
	}
	for i, gate := range c.Gates {
		limit := c.Inputs + i
		if gate.Left < 0 || gate.Left >= limit || gate.Right < 0 || gate.Right >= limit {
			return fmt.Errorf("circuitsat: gate %d references unavailable signal", i)
		}
	}
	if c.Output < 0 || c.Output >= c.Inputs+len(c.Gates) {
		return errors.New("circuitsat: output references unavailable signal")
	}
	return nil
}

func EvaluateAssignment(c Circuit, assignment uint64) (bool, error) {
	if err := Validate(c); err != nil {
		return false, err
	}
	if assignment >= (uint64(1) << uint(c.Inputs)) {
		return false, errors.New("circuitsat: assignment is outside input range")
	}
	signals := make([]bool, c.Inputs, c.Inputs+len(c.Gates))
	for i := 0; i < c.Inputs; i++ {
		shift := c.Inputs - 1 - i
		signals[i] = (assignment>>uint(shift))&1 == 1
	}
	for _, gate := range c.Gates {
		signals = append(signals, !(signals[gate.Left] && signals[gate.Right]))
	}
	return signals[c.Output], nil
}

func VerifyAssignment(c Circuit, assignment uint64) error {
	ok, err := EvaluateAssignment(c, assignment)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("circuitsat: assignment does not satisfy the circuit")
	}
	return nil
}

// Solve performs exact NAND Circuit-SAT using 64 assignments per machine word.
// Workers are executed in deterministic contiguous waves: the first successful
// wave therefore contains the numerically smallest satisfying assignment.
func Solve(ctx context.Context, c Circuit, cfg Config) (Result, error) {
	if err := Validate(c); err != nil {
		return Result{}, err
	}
	workers := cfg.Workers
	if workers <= 0 {
		workers = 1
	}
	if workers > 64 {
		workers = 64
	}
	total := uint64(1) << uint(c.Inputs)
	blocks := (total-1)/64 + 1
	stats := Stats{Workers: workers}

	for base := uint64(0); base < blocks; base += uint64(workers) {
		if err := ctx.Err(); err != nil {
			return Result{Stats: stats}, err
		}
		wave := workers
		if remain := int(blocks - base); remain < wave {
			wave = remain
		}
		results := make([]blockResult, wave)
		var wg sync.WaitGroup
		wg.Add(wave)
		for i := 0; i < wave; i++ {
			index := i
			block := base + uint64(i)
			go func() {
				defer wg.Done()
				if ctx.Err() != nil {
					return
				}
				results[index] = solveBlock(c, block, total)
			}()
		}
		wg.Wait()
		if err := ctx.Err(); err != nil {
			return Result{Stats: stats}, err
		}
		var best uint64
		found := false
		for _, result := range results {
			stats.CheckedBlocks++
			stats.CheckedAssignments += result.checked
			if result.found && (!found || result.assignment < best) {
				found = true
				best = result.assignment
			}
		}
		if found {
			return Result{Satisfiable: true, Assignment: best, Stats: stats}, nil
		}
	}
	stats.Complete = true
	return Result{Satisfiable: false, Stats: stats}, nil
}

func solveBlock(c Circuit, block, total uint64) blockResult {
	start := block * 64
	count := uint64(64)
	if remaining := total - start; remaining < count {
		count = remaining
	}
	validMask := ^uint64(0)
	if count < 64 {
		validMask = (uint64(1) << uint(count)) - 1
	}
	signals := make([]uint64, c.Inputs, c.Inputs+len(c.Gates))
	for input := 0; input < c.Inputs; input++ {
		shift := c.Inputs - 1 - input
		var word uint64
		for bit := uint64(0); bit < count; bit++ {
			assignment := start + bit
			if (assignment>>uint(shift))&1 == 1 {
				word |= uint64(1) << uint(bit)
			}
		}
		signals[input] = word
	}
	for _, gate := range c.Gates {
		signals = append(signals, ^(signals[gate.Left]&signals[gate.Right])&validMask)
	}
	word := signals[c.Output] & validMask
	if word == 0 {
		return blockResult{checked: count}
	}
	bit := uint64(bits.TrailingZeros64(word))
	return blockResult{found: true, assignment: start + bit, checked: count}
}

package mcsp

import (
	"context"
	"errors"
	"testing"
)

func TestVariableTruthTable(t *testing.T) {
	x, err := VariableTruthTable(2, 0)
	if err != nil {
		t.Fatal(err)
	}
	y, err := VariableTruthTable(2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if x != 0b1100 || y != 0b1010 {
		t.Fatalf("unexpected tables x=%04b y=%04b", x, y)
	}
}

func TestSolvePrimaryInput(t *testing.T) {
	x, _ := VariableTruthTable(2, 0)
	got, err := Solve(context.Background(), 2, x, Config{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Circuit.Gates) != 0 || got.Circuit.Output != 0 || !got.Optimal {
		t.Fatalf("unexpected result: %+v", got)
	}
	if err := Verify(x, got.Circuit); err != nil {
		t.Fatal(err)
	}
}

func TestSolveNot(t *testing.T) {
	x, _ := VariableTruthTable(1, 0)
	mask, _ := Mask(1)
	target := ^x & mask
	got, err := Solve(context.Background(), 1, target, Config{MaxGates: 2, MaxStates: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Circuit.Gates) != 1 {
		t.Fatalf("gates=%d want=1", len(got.Circuit.Gates))
	}
	if err := VerifyMinimal(context.Background(), target, got.Circuit, 100); err != nil {
		t.Fatal(err)
	}
}

func TestSolveAnd(t *testing.T) {
	x, _ := VariableTruthTable(2, 0)
	y, _ := VariableTruthTable(2, 1)
	target := x & y
	got, err := Solve(context.Background(), 2, target, Config{MaxGates: 3, MaxStates: 10000})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Circuit.Gates) != 2 {
		t.Fatalf("gates=%d want=2", len(got.Circuit.Gates))
	}
	if err := VerifyMinimal(context.Background(), target, got.Circuit, 10000); err != nil {
		t.Fatal(err)
	}
}

func TestGateLimit(t *testing.T) {
	x, _ := VariableTruthTable(2, 0)
	y, _ := VariableTruthTable(2, 1)
	_, err := Solve(context.Background(), 2, x&y, Config{MaxGates: 1, MaxStates: 10000})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v", err)
	}
}

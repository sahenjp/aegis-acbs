package circuitsat

import (
	"context"
	"testing"
)

func andCircuit() Circuit {
	return Circuit{Inputs: 2, Gates: []Gate{{Left: 0, Right: 1}, {Left: 2, Right: 2}}, Output: 3}
}

func TestSolveSAT(t *testing.T) {
	got, err := Solve(context.Background(), andCircuit(), Config{Workers: 4})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Satisfiable || got.Assignment != 3 {
		t.Fatalf("got=%+v", got)
	}
	if err := VerifyAssignment(andCircuit(), got.Assignment); err != nil {
		t.Fatal(err)
	}
}

func TestSolveUNSAT(t *testing.T) {
	c := Circuit{Inputs: 1, Gates: []Gate{{Left: 0, Right: 0}, {Left: 0, Right: 1}, {Left: 2, Right: 2}}, Output: 3}
	got, err := Solve(context.Background(), c, Config{Workers: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got.Satisfiable || !got.Stats.Complete || got.Stats.CheckedAssignments != 2 {
		t.Fatalf("got=%+v", got)
	}
}

func TestInvalidCircuit(t *testing.T) {
	c := Circuit{Inputs: 1, Gates: []Gate{{Left: 0, Right: 2}}, Output: 1}
	if err := Validate(c); err == nil {
		t.Fatal("expected validation error")
	}
}

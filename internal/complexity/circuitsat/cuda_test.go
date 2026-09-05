package circuitsat

import (
	"bytes"
	"strings"
	"testing"
)

func TestCUDAProtocolProblem(t *testing.T) {
	c := Circuit{Inputs: 2, Gates: []Gate{{Left: 0, Right: 1}, {Left: 2, Right: 2}}, Output: 3}
	var buf bytes.Buffer
	if err := writeCUDAProblem(&buf, c); err != nil {
		t.Fatal(err)
	}
	want := "AEGIS_CIRCUITSAT_CUDA_V1\n2 2 3\n0 1\n2 2\n"
	if buf.String() != want {
		t.Fatalf("protocol=%q want=%q", buf.String(), want)
	}
}

func TestParseCUDAResponse(t *testing.T) {
	sat, err := parseCUDAResponse("SAT 3 64\n")
	if err != nil {
		t.Fatal(err)
	}
	if !sat.Satisfiable || sat.Assignment != 3 || sat.Stats.CheckedAssignments != 64 {
		t.Fatalf("sat=%+v", sat)
	}
	unsat, err := parseCUDAResponse("UNSAT 128\n")
	if err != nil {
		t.Fatal(err)
	}
	if unsat.Satisfiable || !unsat.Stats.Complete || unsat.Stats.CheckedAssignments != 128 {
		t.Fatalf("unsat=%+v", unsat)
	}
}

func TestParseCUDAResponseRejectsGarbage(t *testing.T) {
	for _, text := range []string{"", "SAT", "SAT nope 2", "UNSAT", "UNKNOWN 1"} {
		if _, err := parseCUDAResponse(text); err == nil {
			t.Fatalf("accepted %q", strings.TrimSpace(text))
		}
	}
}

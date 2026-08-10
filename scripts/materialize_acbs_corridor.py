#!/usr/bin/env python3
from pathlib import Path


def replace_once(text: str, old: str, new: str, label: str) -> str:
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{label}: expected one match, found {count}")
    return text.replace(old, new, 1)


search_path = Path("internal/search/search.go")
search = search_path.read_text()
search = replace_once(
    search,
    '\tAegis             Algorithm = "aegis"\n',
    '\tAegis             Algorithm = "aegis"\n\tAegisCorridor     Algorithm = "aegis-corridor"\n',
    "algorithm constant",
)
search = replace_once(
    search,
    '''\tcase Aegis:\n\t\tr, err = acbs(ctx, g, source, target)\n''',
    '''\tcase Aegis:\n\t\tr, err = acbs(ctx, g, source, target)\n\tcase AegisCorridor:\n\t\tr, err = acbsCorridor(ctx, g, source, target)\n''',
    "algorithm dispatch",
)
search_path.write_text(search)

acbs_path = Path("internal/search/acbs.go")
acbs = acbs_path.read_text()
acbs = replace_once(
    acbs,
    '''type acbsOptions struct {\n\talgorithm  Algorithm\n\tadaptive   bool\n\tpruning    bool\n\tprojection bool\n\tguardMode  acbsGuardMode\n}\n''',
    '''type acbsOptions struct {\n\talgorithm  Algorithm\n\tadaptive   bool\n\tpruning    bool\n\tprojection bool\n\tcorridor   bool\n\tguardMode  acbsGuardMode\n}\n''',
    "corridor option",
)
acbs = replace_once(
    acbs,
    '''func acbs(ctx context.Context, g *graph.Graph, source, target int) (Result, error) {\n\t// The incumbent-bound pruning ablation was inactive on the large road\n\t// benchmarks and added extra bound evaluation work. The production ACBS\n\t// path therefore keeps the exact coupled-bound termination rule but does\n\t// not run the optional per-node incumbent pruning experiment.\n\treturn acbsWithOptions(ctx, g, source, target, acbsOptions{algorithm: Aegis, adaptive: true, pruning: false})\n}\n''',
    '''func acbs(ctx context.Context, g *graph.Graph, source, target int) (Result, error) {\n\t// The incumbent-bound pruning ablation was inactive on the large road\n\t// benchmarks and added extra bound evaluation work. The production ACBS\n\t// path therefore keeps the exact coupled-bound termination rule but does\n\t// not run the optional per-node incumbent pruning experiment.\n\treturn acbsWithOptions(ctx, g, source, target, acbsOptions{algorithm: Aegis, adaptive: true, pruning: false})\n}\n\nfunc acbsCorridor(ctx context.Context, g *graph.Graph, source, target int) (Result, error) {\n\treturn acbsWithOptions(ctx, g, source, target, acbsOptions{\n\t\talgorithm: AegisCorridor, adaptive: true, pruning: false, corridor: true,\n\t})\n}\n''',
    "corridor entry point",
)

# In corridor mode, edge work is charged by the number of original directed
# arcs represented by each macro edge. Non-corridor mode is byte-for-byte the
# production accounting path.
acbs = replace_once(
    acbs,
    '''\t\t\t\tedges := g.OutEdges(cur.node)\n\t\t\t\tused += max(1, len(edges))\n\t\t\t\tstats.Expanded++\n\t\t\t\tstats.ForwardExpanded++\n\t\t\t\tfor _, e := range edges {\n\t\t\t\t\tstats.Relaxed++\n''',
    '''\t\t\t\tedges := g.OutEdges(cur.node)\n\t\t\t\tif opts.corridor {\n\t\t\t\t\tif len(edges) == 0 {\n\t\t\t\t\t\tused++\n\t\t\t\t\t}\n\t\t\t\t} else {\n\t\t\t\t\tused += max(1, len(edges))\n\t\t\t\t}\n\t\t\t\tstats.Expanded++\n\t\t\t\tstats.ForwardExpanded++\n\t\t\t\tfor _, e := range edges {\n\t\t\t\t\tif opts.corridor {\n\t\t\t\t\t\tmacro := corridorForwardMacro(g, cur.node, e, target)\n\t\t\t\t\t\te = graph.Edge{To: macro.to, Cost: macro.cost}\n\t\t\t\t\t\tused += macro.steps\n\t\t\t\t\t\tstats.Relaxed += uint64(macro.steps)\n\t\t\t\t\t} else {\n\t\t\t\t\t\tstats.Relaxed++\n\t\t\t\t\t}\n''',
    "forward macro relaxation",
)
acbs = replace_once(
    acbs,
    '''\t\t\t\tedges := g.InEdges(cur.node)\n\t\t\t\tused += max(1, len(edges))\n\t\t\t\tstats.Expanded++\n\t\t\t\tstats.BackwardExpanded++\n\t\t\t\tfor _, e := range edges {\n\t\t\t\t\tstats.Relaxed++\n''',
    '''\t\t\t\tedges := g.InEdges(cur.node)\n\t\t\t\tif opts.corridor {\n\t\t\t\t\tif len(edges) == 0 {\n\t\t\t\t\t\tused++\n\t\t\t\t\t}\n\t\t\t\t} else {\n\t\t\t\t\tused += max(1, len(edges))\n\t\t\t\t}\n\t\t\t\tstats.Expanded++\n\t\t\t\tstats.BackwardExpanded++\n\t\t\t\tfor _, e := range edges {\n\t\t\t\t\tif opts.corridor {\n\t\t\t\t\t\tmacro := corridorBackwardMacro(g, cur.node, e, source)\n\t\t\t\t\t\te = graph.Edge{To: macro.to, Cost: macro.cost}\n\t\t\t\t\t\tused += macro.steps\n\t\t\t\t\t\tstats.Relaxed += uint64(macro.steps)\n\t\t\t\t\t} else {\n\t\t\t\t\t\tstats.Relaxed++\n\t\t\t\t\t}\n''',
    "backward macro relaxation",
)
acbs = replace_once(
    acbs,
    '''\tpath := reconstructBidirectional(pf, pb, source, meet, target)\n''',
    '''\tpath := reconstructBidirectional(pf, pb, source, meet, target)\n\tif opts.corridor {\n\t\tpath = reconstructCorridorBidirectional(g, pf, pb, df, db, source, meet, target)\n\t}\n''',
    "corridor path reconstruction",
)
acbs = replace_once(
    acbs,
    '''func schedulerName(opts acbsOptions) string {\n''',
    '''func schedulerName(opts acbsOptions) string {\n\tif opts.corridor {\n\t\treturn acbsSchedulerVersion + "-corridor-v1"\n\t}\n''',
    "corridor scheduler identity",
)
acbs_path.write_text(acbs)

corridor = r'''package search

import "github.com/lasder-ca/aegis-acbs/internal/graph"

type corridorMacroEdge struct {
	to    int
	cost  uint64
	steps int
}

func corridorForwardMacro(g *graph.Graph, start int, first graph.Edge, stop int) corridorMacroEdge {
	original := corridorMacroEdge{to: first.To, cost: first.Cost, steps: 1}
	prev, cur := start, first.To
	cost, steps := first.Cost, 1
	if cur == stop {
		return original
	}
	for corridorInterior(g, cur) {
		next, ok := corridorForwardNext(g, cur, prev)
		if !ok || cost > inf-next.Cost {
			break
		}
		prev, cur = cur, next.To
		cost += next.Cost
		steps++
		if cur == start {
			// A component consisting only of a degree-two cycle has no canonical
			// contraction endpoint. Fall back to the original arc rather than
			// creating a self macro edge.
			return original
		}
		if cur == stop {
			break
		}
	}
	return corridorMacroEdge{to: cur, cost: cost, steps: steps}
}

func corridorBackwardMacro(g *graph.Graph, start int, first graph.Edge, stop int) corridorMacroEdge {
	original := corridorMacroEdge{to: first.To, cost: first.Cost, steps: 1}
	nextTowardTarget, cur := start, first.To
	cost, steps := first.Cost, 1
	if cur == stop {
		return original
	}
	for corridorInterior(g, cur) {
		prev, ok := corridorBackwardNext(g, cur, nextTowardTarget)
		if !ok || cost > inf-prev.Cost {
			break
		}
		nextTowardTarget, cur = cur, prev.To
		cost += prev.Cost
		steps++
		if cur == start {
			return original
		}
		if cur == stop {
			break
		}
	}
	return corridorMacroEdge{to: cur, cost: cost, steps: steps}
}

func corridorInterior(g *graph.Graph, node int) bool {
	first, second := -1, -1
	add := func(neighbor int) bool {
		if neighbor == node {
			return false
		}
		if neighbor == first || neighbor == second {
			return true
		}
		if first < 0 {
			first = neighbor
			return true
		}
		if second < 0 {
			second = neighbor
			return true
		}
		return false
	}
	for _, edge := range g.OutEdges(node) {
		if !add(edge.To) {
			return false
		}
	}
	for _, edge := range g.InEdges(node) {
		if !add(edge.To) {
			return false
		}
	}
	return first >= 0 && second >= 0
}

func corridorForwardNext(g *graph.Graph, node, previous int) (graph.Edge, bool) {
	var next graph.Edge
	found := false
	for _, edge := range g.OutEdges(node) {
		if edge.To == previous || edge.To == node {
			continue
		}
		if found {
			return graph.Edge{}, false
		}
		next, found = edge, true
	}
	return next, found
}

func corridorBackwardNext(g *graph.Graph, node, nextTowardTarget int) (graph.Edge, bool) {
	var previous graph.Edge
	found := false
	for _, edge := range g.InEdges(node) {
		if edge.To == nextTowardTarget || edge.To == node {
			continue
		}
		if found {
			return graph.Edge{}, false
		}
		previous, found = edge, true
	}
	return previous, found
}

func reconstructCorridorBidirectional(
	g *graph.Graph, pf, pb []int32, df, db []uint64, source, meet, target int,
) []int {
	forward := []int{meet}
	for node := meet; node != source; {
		parent := int(pf[node])
		if parent < 0 || parent >= len(g.Nodes) {
			return nil
		}
		forward = append(forward, parent)
		node = parent
		if len(forward) > len(g.Nodes)+1 {
			return nil
		}
	}
	for i, j := 0, len(forward)-1; i < j; i, j = i+1, j-1 {
		forward[i], forward[j] = forward[j], forward[i]
	}

	path := make([]int, 1, len(forward)+8)
	path[0] = source
	for i := 0; i+1 < len(forward); i++ {
		from, to := forward[i], forward[i+1]
		if df[from] == inf || df[to] == inf || df[to] < df[from] {
			return nil
		}
		var ok bool
		path, ok = appendCorridorSegment(g, path, from, to, df[to]-df[from])
		if !ok {
			return nil
		}
	}

	for node := meet; node != target; {
		next := int(pb[node])
		if next < 0 || next >= len(g.Nodes) || db[node] == inf || db[next] == inf || db[node] < db[next] {
			return nil
		}
		var ok bool
		path, ok = appendCorridorSegment(g, path, node, next, db[node]-db[next])
		if !ok {
			return nil
		}
		node = next
		if len(path) > len(g.Nodes)+1 {
			return nil
		}
	}
	return path
}

func appendCorridorSegment(
	g *graph.Graph, path []int, from, to int, expected uint64,
) ([]int, bool) {
	for _, first := range g.OutEdges(from) {
		segment := []int{first.To}
		cost := first.Cost
		if first.To == to {
			if cost == expected {
				return append(path, segment...), true
			}
			continue
		}
		previous, current := from, first.To
		for corridorInterior(g, current) {
			next, ok := corridorForwardNext(g, current, previous)
			if !ok || cost > inf-next.Cost {
				break
			}
			previous, current = current, next.To
			cost += next.Cost
			segment = append(segment, current)
			if current == from {
				break
			}
			if current == to {
				if cost == expected {
					return append(path, segment...), true
				}
				break
			}
		}
	}
	return path, false
}
'''
Path("internal/search/corridor_generated.go").write_text(corridor)

tests = r'''package search

import (
	"context"
	"testing"

	"github.com/lasder-ca/aegis-acbs/internal/graph"
)

func TestCorridorChainExactAndCompressesQueue(t *testing.T) {
	g := graph.New("corridor-chain", "test", "car", graph.MetricDistance)
	const n = 200
	g.Nodes = make([]graph.Node, n)
	g.Adj = make([][]graph.Edge, n)
	for i := 0; i < n; i++ {
		g.Nodes[i] = graph.Node{ID: int64(i + 1), Lat: 35.0, Lon: 139.0 + float64(i)*1e-5}
		if i+1 < n {
			g.Adj[i] = append(g.Adj[i], graph.Edge{To: i + 1, Cost: 100})
			g.Adj[i+1] = append(g.Adj[i+1], graph.Edge{To: i, Cost: 100})
		}
	}
	if err := g.Finalize(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	baseline, err := Run(ctx, g, 0, n-1, Aegis)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := Run(ctx, g, 0, n-1, AegisCorridor)
	if err != nil {
		t.Fatal(err)
	}
	if !candidate.Stats.Reachable || candidate.Stats.Distance != baseline.Stats.Distance {
		t.Fatalf("distance mismatch: baseline=%+v candidate=%+v", baseline.Stats, candidate.Stats)
	}
	if !Validate(g, 0, n-1, candidate) {
		t.Fatal("corridor path failed validation")
	}
	if len(candidate.Path) != n {
		t.Fatalf("expected fully expanded path with %d nodes, got %d", n, len(candidate.Path))
	}
	if candidate.Stats.QueuePushes >= baseline.Stats.QueuePushes {
		t.Fatalf("expected fewer queue pushes: baseline=%d candidate=%d", baseline.Stats.QueuePushes, candidate.Stats.QueuePushes)
	}
}

func TestCorridorExactOnBranchingDirectedGraph(t *testing.T) {
	g := graph.New("corridor-branch", "test", "car", graph.MetricTime)
	g.Nodes = make([]graph.Node, 12)
	g.Adj = make([][]graph.Edge, 12)
	for i := range g.Nodes {
		g.Nodes[i] = graph.Node{ID: int64(i + 1), Lat: 35 + float64(i)*1e-4, Lon: 139 + float64(i%3)*1e-4}
	}
	add := func(a, b int, c uint64) { g.Adj[a] = append(g.Adj[a], graph.Edge{To: b, Cost: c}) }
	add(0, 1, 4); add(1, 2, 5); add(2, 3, 6); add(3, 4, 7)
	add(4, 3, 8); add(3, 2, 6); add(2, 1, 5); add(1, 0, 4)
	add(2, 5, 2); add(5, 6, 2); add(6, 7, 2); add(7, 8, 2)
	add(8, 7, 2); add(7, 6, 2); add(6, 5, 2); add(5, 2, 2)
	add(3, 9, 3); add(9, 10, 3); add(10, 11, 3)
	add(11, 10, 4); add(10, 9, 4); add(9, 3, 4)
	if err := g.Finalize(); err != nil { t.Fatal(err) }
	ctx := context.Background()
	for s := range g.Nodes {
		for target := range g.Nodes {
			want, err := Run(ctx, g, s, target, Dijkstra)
			if err != nil { t.Fatal(err) }
			got, err := Run(ctx, g, s, target, AegisCorridor)
			if err != nil { t.Fatalf("%d->%d: %v", s, target, err) }
			if want.Stats.Reachable != got.Stats.Reachable || (want.Stats.Reachable && want.Stats.Distance != got.Stats.Distance) {
				t.Fatalf("%d->%d mismatch: dijkstra=%+v corridor=%+v", s, target, want.Stats, got.Stats)
			}
			if got.Stats.Reachable && !Validate(g, s, target, got) {
				t.Fatalf("%d->%d invalid reconstructed path: %v", s, target, got.Path)
			}
		}
	}
}

func TestCorridorFallsBackOnPureCycle(t *testing.T) {
	g := graph.New("corridor-cycle", "test", "car", graph.MetricDistance)
	const n = 16
	g.Nodes = make([]graph.Node, n)
	g.Adj = make([][]graph.Edge, n)
	for i := 0; i < n; i++ {
		g.Nodes[i] = graph.Node{ID: int64(i + 1), Lat: 35, Lon: 139 + float64(i)*1e-5}
		next := (i + 1) % n
		prev := (i + n - 1) % n
		g.Adj[i] = append(g.Adj[i], graph.Edge{To: next, Cost: 10}, graph.Edge{To: prev, Cost: 11})
	}
	if err := g.Finalize(); err != nil { t.Fatal(err) }
	got, err := Run(context.Background(), g, 0, 8, AegisCorridor)
	if err != nil { t.Fatal(err) }
	want, err := Run(context.Background(), g, 0, 8, Dijkstra)
	if err != nil { t.Fatal(err) }
	if got.Stats.Distance != want.Stats.Distance || !Validate(g, 0, 8, got) {
		t.Fatalf("cycle mismatch: want=%+v got=%+v path=%v", want.Stats, got.Stats, got.Path)
	}
}
'''
Path("internal/search/corridor_generated_test.go").write_text(tests)

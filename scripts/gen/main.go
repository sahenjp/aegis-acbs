// Command gen-grid writes synthetic DIMACS road networks for scale testing the
// Aegis shortest-path stack.
//
// The output is a planar-ish city grid whose directed edges model two-way
// streets crossed by signalised intersections. Each node carries a traffic
// signal phase, and at signalised nodes only one axis may be travelled per
// phase. This yields realistic turn restrictions (no illegal diagonal
// crossings) while keeping every street two-way, so the graph is strongly
// connected by construction.
//
// Node coordinates are laid out on an equirectangular projection and every edge
// cost is the exact integer metre count of its specified length. The weights
// therefore have zero floating-point slack: an A* heuristic built as floor
// meters * minCostPerMeter stays admissible, which is what makes
// cross-algorithm agreement a meaningful correctness check.
//
// Usage:
//
//	go run ./scripts/gen -nodes 100000 -out /tmp/aegis-graphs
package main

import (
	"bufio"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const (
	// A 100 m intersection spacing keeps 1M-node grids inside a plausible city
	// footprint while staying far above float64 resolution in metres.
	blockMeters = 100.0
	earthRadius = 6371008.8
	// Block lengths in whole metres. Rows are short north-south blocks, columns
	// are longer east-west blocks, which models a rectangular city grid.
	shortMeters = 100
	longMeters  = 141
)

type node struct {
	row, col int32
	lat, lon float64
	// signalised reports whether this is a controlled intersection. Traffic that
	// arrives into it must leave along the axis of its own approach, which is the
	// usual left-turn-prohibited-at-signal rule and removes illegal crossings.
	signalised bool
}

type edge struct {
	from, to int32
	cost     uint32
}

func blockLength(dr, dc int32) uint32 {
	if dr != 0 {
		return shortMeters
	}
	if dc != 0 {
		return longMeters
	}
	return 0
}

// gridSide returns the grid side for a node count. Non-square targets are
// rounded down to the nearest perfect square and the caller is told the exact
// final node count, so requests like 100000 still produce a usable graph.
func gridSide(n int) int {
	return int(math.Floor(math.Sqrt(float64(n))))
}

// isSignalised decides whether an intersection is controlled. A pure function of
// the coordinates keeps generation deterministic and independent of iteration
// order, so a 100k grid is a prefix-compatible refinement of a 10k grid.
func isSignalised(row, col int) bool {
	h := uint64(row)*0x9e3779b97f4a7c15 ^ uint64(col)*0xc2b2ae3d27d4eb4f
	h ^= h >> 29
	h *= 0xbf58476d1ce4e5b9
	h ^= h >> 32
	// Roughly 70% of intersections are controlled.
	return h%10 < 7
}

type generator struct {
	side    int
	nodes   []node
	edges   []edge
	edgeCnt int64
	minCost uint32
	maxCost uint32
	lon     []float64
}

func (g *generator) at(row, col int) int32 { return int32(row*g.side + col) }

func newGenerator(side int, baseLat float64) *generator {
	g := &generator{side: side, nodes: make([]node, side*side), minCost: math.MaxUint32, lon: make([]float64, side)}
	// Latitude of row r: constant 100 m meridian steps from the base latitude.
	latAt := func(r int) float64 {
		return baseLat + float64(r)*blockMeters/earthRadius*180/math.Pi
	}
	for r := 0; r < side; r++ {
		lat := latAt(r)
		for c := 0; c < side; c++ {
			n := &g.nodes[g.at(r, c)]
			n.row, n.col = int32(r), int32(c)
			n.lat = lat
			n.signalised = isSignalised(r, c)
		}
	}
	// Longitude is accumulated column by column at the base row's latitude. One
	// global column table keeps every row aligned while the desired east-west
	// spacing is exactly longMeters anywhere inside a realistic latitude band.
	// The haversine length along a row therefore never exceeds the 141 m
	// half-diagonal, so the A* lower bound stays admissible.
	baseLatRad := baseLat * math.Pi / 180
	g.lon[0] = 0
	for c := 1; c < side; c++ {
		g.lon[c] = g.lon[c-1] + float64(longMeters)/(earthRadius*math.Cos(baseLatRad))*180/math.Pi
	}
	for r := 0; r < side; r++ {
		for c := 0; c < side; c++ {
			g.nodes[g.at(r, c)].lon = g.lon[c]
		}
	}
	g.buildEdges()
	return g
}

func (g *generator) add(from, to int32, cost uint32) {
	g.edges = append(g.edges, edge{from: from, to: to, cost: cost})
	g.edgeCnt++
	if cost < g.minCost {
		g.minCost = cost
	}
	if cost > g.maxCost {
		g.maxCost = cost
	}
}

func (g *generator) buildEdges() {
	dr := [4]int32{-1, 1, 0, 0}
	dc := [4]int32{0, 0, -1, 1}
	for r := 0; r < g.side; r++ {
		for c := 0; c < g.side; c++ {
			from := g.at(r, c)
			for k := 0; k < 4; k++ {
				nr, nc := r+int(dr[k]), c+int(dc[k])
				if nr < 0 || nr >= g.side || nc < 0 || nc >= g.side {
					continue
				}
				// Every street is two-way, which alone guarantees strong
				// connectivity; control only constrains turns through a junction.
				dst := g.at(nr, nc)
				if g.nodes[dst].signalised && dr[k] != 0 {
					continue
				}
				g.add(from, dst, blockLength(dr[k], dc[k]))
			}
		}
	}
}

func (g *generator) writeDIMACSGraph(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriterSize(f, 1<<20)
	fmt.Fprintf(w, "c synthetic grid road network generated by scripts/gen\n")
	fmt.Fprintf(w, "c planar-ish grid with signalised intersections and two-way streets\n")
	fmt.Fprintf(w, "c grid side %d, block spacing %.0f m, costs are integer metres\n", g.side, blockMeters)
	fmt.Fprintf(w, "p sp %d %d\n", len(g.nodes), g.edgeCnt)
	for _, e := range g.edges {
		// DIMACS node numbers are 1-based.
		w.WriteString("a ")
		w.WriteString(strconv.Itoa(int(e.from) + 1))
		w.WriteByte(' ')
		w.WriteString(strconv.Itoa(int(e.to) + 1))
		w.WriteByte(' ')
		w.WriteString(strconv.Itoa(int(e.cost)))
		w.WriteByte('\n')
	}
	if err := w.Flush(); err != nil {
		return err
	}
	return f.Sync()
}

func (g *generator) writeDIMACSPlaces(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriterSize(f, 1<<20)
	fmt.Fprintf(w, "c vertex coordinates, longitude and latitude in degrees\n")
	fmt.Fprintf(w, "p aux sp co %d\n", len(g.nodes))
	for i := range g.nodes {
		n := &g.nodes[i]
		w.WriteString("v ")
		w.WriteString(strconv.Itoa(i + 1))
		w.WriteByte(' ')
		w.WriteString(strconv.FormatFloat(n.lon, 'f', 7, 64))
		w.WriteByte(' ')
		w.WriteString(strconv.FormatFloat(n.lat, 'f', 7, 64))
		w.WriteByte('\n')
	}
	if err := w.Flush(); err != nil {
		return err
	}
	return f.Sync()
}

// verify recomputes the structural invariants the benchmark suite relies on.
func (g *generator) verify() error {
	seen := make(map[uint64]struct{}, len(g.edges))
	for _, e := range g.edges {
		if e.cost < shortMeters {
			return fmt.Errorf("edge %d->%d has cost %d below minimum %d", e.from, e.to, e.cost, shortMeters)
		}
		key := uint64(e.from)<<32 | uint64(e.to)
		if _, dup := seen[key]; dup {
			return fmt.Errorf("duplicate directed edge %d->%d", e.from, e.to)
		}
		seen[key] = struct{}{}
	}
	return g.verifyStronglyConnected()
}

// verifyStronglyConnected runs an iterative Tarjan SCC pass and requires a
// single component covering every node.
func (g *generator) verifyStronglyConnected() error {
	adj := make([][]int32, len(g.nodes))
	for _, e := range g.edges {
		adj[e.from] = append(adj[e.from], e.to)
	}
	n := len(g.nodes)
	index := make([]int32, n)
	low := make([]int32, n)
	onStack := make([]bool, n)
	for i := range index {
		index[i] = -1
	}
	var stack []int32
	nextIndex := int32(0)
	components := 0
	type frame struct{ v, next int32 }
	for root := 0; root < n; root++ {
		if index[root] >= 0 {
			continue
		}
		work := []frame{{v: int32(root)}}
		index[root], low[root] = nextIndex, nextIndex
		nextIndex++
		stack = append(stack, int32(root))
		onStack[root] = true
		for len(work) > 0 {
			top := &work[len(work)-1]
			if int(top.next) < len(adj[top.v]) {
				to := adj[top.v][top.next]
				top.next++
				if index[to] < 0 {
					index[to], low[to] = nextIndex, nextIndex
					nextIndex++
					stack = append(stack, to)
					onStack[to] = true
					work = append(work, frame{v: to})
				} else if onStack[to] && index[to] < low[top.v] {
					low[top.v] = index[to]
				}
				continue
			}
			v := top.v
			work = work[:len(work)-1]
			if len(work) > 0 {
				parent := work[len(work)-1].v
				if low[v] < low[parent] {
					low[parent] = low[v]
				}
			}
			if low[v] == index[v] {
				components++
				for {
					w := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					onStack[w] = false
					if w == v {
						break
					}
				}
			}
		}
	}
	if components != 1 {
		return fmt.Errorf("graph is not strongly connected: %d components", components)
	}
	return nil
}

func main() {
	nodesFlag := flag.Int("nodes", 10000, "number of nodes; must be a perfect square (grid side^2)")
	outDir := flag.String("out", "/tmp/aegis-graphs", "output directory for .dimacs and .co files")
	name := flag.String("name", "", "output base name; defaults to a compact node count such as 100k")
	baseLat := flag.Float64("lat", 35.6812, "latitude of the south-west grid corner")
	flag.Parse()

	if *nodesFlag < 4 {
		fail(fmt.Errorf("node count %d is too small; a grid needs at least 2x2 nodes", *nodesFlag))
	}
	side := gridSide(*nodesFlag)
	if *name == "" {
		*name = humanSize(*nodesFlag)
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fail(err)
	}
	started := time.Now()
	gen := newGenerator(side, *baseLat)
	genElapsed := time.Since(started)

	graphPath := filepath.Join(*outDir, *name+".dimacs")
	coordPath := filepath.Join(*outDir, *name+".co")
	if err := gen.writeDIMACSGraph(graphPath); err != nil {
		fail(err)
	}
	if err := gen.writeDIMACSPlaces(coordPath); err != nil {
		fail(err)
	}
	if err := gen.verify(); err != nil {
		fail(err)
	}
	graphInfo, err := os.Stat(graphPath)
	if err != nil {
		fail(err)
	}
	coordInfo, err := os.Stat(coordPath)
	if err != nil {
		fail(err)
	}
	fmt.Printf("generated %s in %.2fs\n", graphPath, genElapsed.Seconds())
	fmt.Printf("  %-28s nodes=%d edges=%d bytes=%d min-cost=%d max-cost=%d\n",
		filepath.Base(graphPath), len(gen.nodes), gen.edgeCnt, graphInfo.Size(), gen.minCost, gen.maxCost)
	fmt.Printf("  %-28s nodes=%d bytes=%d\n", filepath.Base(coordPath), len(gen.nodes), coordInfo.Size())
	fmt.Printf("  grid side %d, block spacing %.0f m, strongly-connected=true\n", side, blockMeters)
	if side*side != *nodesFlag {
		fmt.Printf("  requested %d nodes; the nearest perfect square below it is %d (%dx%d)\n", *nodesFlag, side*side, side, side)
	}
}

func humanSize(n int) string {
	switch {
	case n%1000000 == 0:
		return strconv.Itoa(n/1000000) + "m"
	case n%1000 == 0:
		return strconv.Itoa(n/1000) + "k"
	default:
		return strconv.Itoa(n)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "gen-grid:", err)
	os.Exit(1)
}

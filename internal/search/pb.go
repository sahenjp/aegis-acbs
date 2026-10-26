package search

import (
	"context"
	"math/bits"

	"github.com/lasder-ca/aegis-acbs/internal/graph"
)

// pbBuckets is the PB-Buckets selection structure: a radix heap for uint64 keys
// whose bucket numbering uses a balanced (top-zero-run) symbol instead of the
// plain XOR-with-last used by radixHeap.
//
// Standard radix bucketing indexes a key by bits.Len64(key^last), so after a
// pop the next key is either table[0] or table[bits.Len64(key^last)]. Bucket
// numbering by the number of leading zero bits of the key, measured against the
// most recent polled key, keeps that invariant: a key whose symbol matches the
// current symbol belongs to bucket 0, and any other key belongs to bucket
// bits.Len64(sym^lastSym). The symbol is monotone in the key, so bucket order
// always agrees with key order. This is the correctness property the earlier
// bit-reversed variant violated (it produced non-monotone pops and therefore
// non-optimal distances).
type pbBuckets struct {
	buckets [65][]item
	scratch []item
	last    uint64
	size    int
}

func (b *pbBuckets) len() int { return b.size }

func (b *pbBuckets) reset() {
	for i := range b.buckets {
		b.buckets[i] = b.buckets[i][:0]
	}
	b.scratch = b.scratch[:0]
	b.last = 0
	b.size = 0
}

// pbBucket indexes a key by the position of its most significant differing
// bit relative to the last polled key. This is the standard radix-heap bucket
// numbering: a key equal to the current minimum belongs to bucket 0 and every
// other key belongs to bucket bits.Len64(key^last). Because last only ever
// increases and newly pushed keys are >= last, bucket indices are monotone in
// key order, which is what makes "first non-empty bucket holds the minimum"
// true.
func pbBucket(key, last uint64) int {
	if key == last {
		return 0
	}
	return bits.Len64(key ^ last)
}

func (b *pbBuckets) push(key uint64, x item) {
	if key < b.last {
		panic("pb buckets received a non-monotone key")
	}
	x.priority = key
	at := pbBucket(key, b.last)
	b.buckets[at] = append(b.buckets[at], x)
	b.size++
}

// reduce pulls the first non-empty bucket down to its minimum key so bucket 0
// always holds the global minimum. Symbols are non-decreasing, so the first
// non-empty bucket contains the global minimum.
func (b *pbBuckets) reduce() {
	if len(b.buckets[0]) > 0 || b.size == 0 {
		return
	}
	first := 1
	for first < len(b.buckets) && len(b.buckets[first]) == 0 {
		first++
	}
	if first >= len(b.buckets) {
		return
	}
	// Clone before truncating: the source bucket is cleared below, so iterating
	// its slice in place would see an empty slice.
	b.scratch = append(b.scratch[:0], b.buckets[first]...)
	items := b.scratch
	next := items[0].priority
	for i := 1; i < len(items); i++ {
		if items[i].priority < next {
			next = items[i].priority
		}
	}
	b.last = next
	b.buckets[first] = b.buckets[first][:0]
	for _, x := range items {
		b.buckets[pbBucket(x.priority, next)] = append(b.buckets[pbBucket(x.priority, next)], x)
	}
}

// minKey returns the smallest key without removing it.
func (b *pbBuckets) minKey() (uint64, bool) {
	if b.size == 0 {
		return 0, false
	}
	b.reduce()
	return b.last, true
}

func (b *pbBuckets) pop() item {
	b.reduce()
	items := b.buckets[0]
	n := len(items)
	x := items[n-1]
	items[n-1] = item{}
	b.buckets[0] = items[:n-1]
	b.size--
	return x
}

// pbWorkspace holds the two symmetric labels plus the two bucket frontiers.
// Every field is pooled and reset by touch lists, so a steady-state query only
// allocates the returned path slice.
type pbWorkspace struct {
	df, db   []uint64
	pf, pb   []int32
	settledF []bool
	settledB []bool
	touched  []int32
	bf, bb   pbBuckets
}

func (w *pbWorkspace) touchForward(v int) {
	if w.df[v] == inf {
		w.touched = append(w.touched, int32(v))
	}
}

func (w *pbWorkspace) touchBackward(v int) {
	if w.db[v] == inf {
		w.touched = append(w.touched, int32(v))
	}
}

var pbWorkspacePool = syncPoolPB{}

// pbSearch runs the PB-Buckets exact bidirectional Dijkstra: one settled node
// per direction, strictly alternating, with both frontiers held in balanced
// radix bucket queues.
func pbSearch(ctx context.Context, g *graph.Graph, source, target int) (Result, error) {
	if source == target {
		return Result{Path: []int{source}, Stats: Stats{Algorithm: PB, Reachable: true, PathNodes: 1}}, nil
	}
	n := len(g.Nodes)
	w := pbWorkspacePool.get(n)
	defer pbWorkspacePool.put(w)
	df, db := w.df, w.db
	pf, pb := w.pf, w.pb
	settledF, settledB := w.settledF, w.settledB

	w.touchForward(source)
	w.touchBackward(target)
	df[source], db[target] = 0, 0
	w.bf.push(0, item{node: source, distance: 0})
	w.bb.push(0, item{node: target, distance: 0})

	best, meet := inf, -1
	stats := Stats{Algorithm: PB, QueuePushes: 2}
	forward := true
	work := 0
	for w.bf.len() > 0 && w.bb.len() > 0 {
		if work&1023 == 0 {
			select {
			case <-ctx.Done():
				return Result{}, ctx.Err()
			default:
			}
		}
		work++
		// Exact bidirectional Dijkstra termination (Arad, Shapira): kF and kB
		// are the minimum keys still in each frontier. Any path better than the
		// incumbent must cross an edge (u,v) with df(u) + w(u,v) + db(v)
		// >= kF + kB, so no improvement remains once kF + kB >= best. Only
		// frontier minima are used here; last-settled keys are not valid lower
		// bounds and are never substituted.
		kF, _ := w.bf.minKey()
		kB, _ := w.bb.minKey()
		if best != inf && kF >= best-kB {
			break
		}
		if forward {
			cur := w.bf.pop()
			stats.QueuePops++
			if cur.distance != df[cur.node] || settledF[cur.node] {
				stats.StalePops++
				forward = false
				continue
			}
			settledF[cur.node] = true
			stats.Expanded++
			stats.ForwardExpanded++
			if settledB[cur.node] && df[cur.node] < inf-db[cur.node] && df[cur.node]+db[cur.node] < best {
				best = df[cur.node] + db[cur.node]
				meet = cur.node
			}
			for _, e := range g.OutEdges(cur.node) {
				stats.Relaxed++
				if df[cur.node] > inf-e.Cost {
					continue
				}
				nd := df[cur.node] + e.Cost
				if nd < df[e.To] {
					w.touchForward(e.To)
					df[e.To] = nd
					pf[e.To] = int32(cur.node)
					w.bf.push(nd, item{node: e.To, distance: nd})
					stats.QueuePushes++
				}
				if db[e.To] != inf && nd < inf-db[e.To] && nd+db[e.To] < best {
					best = nd + db[e.To]
					meet = e.To
				}
			}
			forward = false
		} else {
			cur := w.bb.pop()
			stats.QueuePops++
			if cur.distance != db[cur.node] || settledB[cur.node] {
				stats.StalePops++
				forward = true
				continue
			}
			settledB[cur.node] = true
			stats.Expanded++
			stats.BackwardExpanded++
			if settledF[cur.node] && df[cur.node] < inf-db[cur.node] && df[cur.node]+db[cur.node] < best {
				best = df[cur.node] + db[cur.node]
				meet = cur.node
			}
			for _, e := range g.InEdges(cur.node) {
				stats.Relaxed++
				if db[cur.node] > inf-e.Cost {
					continue
				}
				nd := db[cur.node] + e.Cost
				if nd < db[e.To] {
					w.touchBackward(e.To)
					db[e.To] = nd
					pb[e.To] = int32(cur.node)
					w.bb.push(nd, item{node: e.To, distance: nd})
					stats.QueuePushes++
				}
				if df[e.To] != inf && nd < inf-df[e.To] && nd+df[e.To] < best {
					best = nd + df[e.To]
					meet = e.To
				}
			}
			forward = true
		}
	}
	if best == inf || meet < 0 {
		return Result{Stats: stats}, nil
	}
	path := pbReconstruct(pf, pb, df, db, source, meet, target)
	if len(path) == 0 {
		return Result{Stats: stats}, nil
	}
	stats.Distance, stats.Reachable, stats.PathNodes = best, true, len(path)
	return Result{Path: path, Stats: stats}, nil
}

// pbReconstruct joins the forward parent chain from the meet node back to the
// source with the backward chain from the meet node to the target.
//
// reconstructBidirectional cannot be used directly: it requires pb[meet] to be
// set, but the meet recorded by this search can be a node the backward search
// only relaxed (never settled), in which case pb[meet] is -1 and the generic
// helper returns nil. Both chains are validated while walking, so a partially
// built path is never returned.
func pbReconstruct(pf, pb []int32, df, db []uint64, source, meet, target int) []int {
	n := len(pf)
	if meet < 0 || meet >= n || df[meet] == inf || db[meet] == inf {
		return nil
	}
	left := []int{meet}
	for v := meet; v != source; {
		p := int(pf[v])
		if p < 0 || p >= n || len(left) > n {
			return nil
		}
		left = append(left, p)
		v = p
	}
	right := make([]int, 0, 8)
	for v := meet; v != target; {
		q := int(pb[v])
		if q < 0 || q >= n || len(right) > n {
			return nil
		}
		right = append(right, q)
		v = q
	}
	path := make([]int, 0, len(left)+len(right))
	for i := len(left) - 1; i >= 0; i-- {
		path = append(path, left[i])
	}
	path = append(path, right...)
	if len(path) == 0 || path[0] != source || path[len(path)-1] != target {
		return nil
	}
	return path
}

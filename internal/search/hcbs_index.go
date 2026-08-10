package search

import (
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

var hcbsSidecarMagic = [8]byte{'A', 'E', 'G', 'H', 'C', 'B', '0', '1'}

const hcbsInf = uint32(1<<31 - 1)

// HCBSIndex is an immutable contraction-hierarchy query index. Node IDs passed
// to Distance use the original Aegis graph numbering; rank maps them to the
// internal upward-graph numbering stored in the sidecar.
//
// The sidecar format deliberately contains only query data. RoutingKit is used
// by the research builder to generate a hierarchy, but no RoutingKit code or
// dependency is required to load or query an HCBSIndex.
type HCBSIndex struct {
	rank     []uint32
	forward  hcbsSide
	backward hcbsSide
	pool     sync.Pool
}

type hcbsSide struct {
	firstOut []uint32
	head     []uint32
	weight   []uint32
}

// LoadHCBSIndex opens a sidecar and verifies that it was built from graphPath.
// A SHA-256 of the exact .aegis graph is embedded in the sidecar so an index
// cannot silently be attached to a graph with different node or edge order.
func LoadHCBSIndex(sidecarPath, graphPath string) (*HCBSIndex, error) {
	graphDigest, err := sha256File(graphPath)
	if err != nil {
		return nil, fmt.Errorf("hash graph: %w", err)
	}
	f, err := os.Open(sidecarPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)

	var magic [8]byte
	if _, err := io.ReadFull(r, magic[:]); err != nil {
		return nil, err
	}
	if magic != hcbsSidecarMagic {
		return nil, errors.New("not a supported HCBS sidecar")
	}
	var embeddedDigest [sha256.Size]byte
	if _, err := io.ReadFull(r, embeddedDigest[:]); err != nil {
		return nil, err
	}
	if embeddedDigest != graphDigest {
		return nil, errors.New("HCBS sidecar graph fingerprint mismatch")
	}

	var nodeCount uint32
	var forwardCount, backwardCount uint64
	if err := binary.Read(r, binary.LittleEndian, &nodeCount); err != nil {
		return nil, err
	}
	if err := binary.Read(r, binary.LittleEndian, &forwardCount); err != nil {
		return nil, err
	}
	if err := binary.Read(r, binary.LittleEndian, &backwardCount); err != nil {
		return nil, err
	}
	if nodeCount == 0 || nodeCount > 1_000_000_000 {
		return nil, errors.New("HCBS sidecar node count is unreasonable")
	}
	if forwardCount > 20_000_000_000 || backwardCount > 20_000_000_000 {
		return nil, errors.New("HCBS sidecar edge count is unreasonable")
	}
	if uint64(int(forwardCount)) != forwardCount || uint64(int(backwardCount)) != backwardCount {
		return nil, errors.New("HCBS sidecar does not fit this architecture")
	}

	index := &HCBSIndex{rank: make([]uint32, int(nodeCount))}
	if err := readUint32Slice(r, index.rank); err != nil {
		return nil, fmt.Errorf("read rank: %w", err)
	}
	if err := validateRank(index.rank); err != nil {
		return nil, err
	}
	index.forward, err = readHCBSSide(r, int(nodeCount), int(forwardCount))
	if err != nil {
		return nil, fmt.Errorf("read forward hierarchy: %w", err)
	}
	index.backward, err = readHCBSSide(r, int(nodeCount), int(backwardCount))
	if err != nil {
		return nil, fmt.Errorf("read backward hierarchy: %w", err)
	}
	if extra, err := r.ReadByte(); err == nil {
		_ = extra
		return nil, errors.New("HCBS sidecar has trailing data")
	} else if !errors.Is(err, io.EOF) {
		return nil, err
	}

	n := len(index.rank)
	index.pool.New = func() any { return newHCBSWorkspace(n) }
	return index, nil
}

func readHCBSSide(r io.Reader, nodeCount, edgeCount int) (hcbsSide, error) {
	side := hcbsSide{
		firstOut: make([]uint32, nodeCount+1),
		head:     make([]uint32, edgeCount),
		weight:   make([]uint32, edgeCount),
	}
	if err := readUint32Slice(r, side.firstOut); err != nil {
		return hcbsSide{}, err
	}
	if err := readUint32Slice(r, side.head); err != nil {
		return hcbsSide{}, err
	}
	if err := readUint32Slice(r, side.weight); err != nil {
		return hcbsSide{}, err
	}
	if side.firstOut[0] != 0 || side.firstOut[len(side.firstOut)-1] != uint32(edgeCount) {
		return hcbsSide{}, errors.New("invalid hierarchy CSR offsets")
	}
	for i := 1; i < len(side.firstOut); i++ {
		if side.firstOut[i] < side.firstOut[i-1] {
			return hcbsSide{}, errors.New("hierarchy CSR offsets are not monotone")
		}
	}
	for i, head := range side.head {
		if int(head) >= nodeCount {
			return hcbsSide{}, fmt.Errorf("hierarchy head out of range at arc %d", i)
		}
		if side.weight[i] >= hcbsInf {
			return hcbsSide{}, fmt.Errorf("hierarchy weight out of range at arc %d", i)
		}
	}
	return side, nil
}

func readUint32Slice(r io.Reader, values []uint32) error {
	return binary.Read(r, binary.LittleEndian, values)
}

func validateRank(rank []uint32) error {
	seen := make([]bool, len(rank))
	for original, value := range rank {
		if int(value) >= len(rank) {
			return fmt.Errorf("HCBS rank out of range for node %d", original)
		}
		if seen[value] {
			return errors.New("HCBS rank is not a permutation")
		}
		seen[value] = true
	}
	return nil
}

func sha256File(path string) ([sha256.Size]byte, error) {
	var digest [sha256.Size]byte
	f, err := os.Open(path)
	if err != nil {
		return digest, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return digest, err
	}
	copy(digest[:], h.Sum(nil))
	return digest, nil
}

// Distance returns the exact shortest-path cost represented by the hierarchy.
// reachable is false when no path exists. The implementation uses the same
// exact CH stopping condition as the research HCBS comparator, a lower-key
// frontier policy, stall-on-demand, epoch-marked distance state, and O(1)
// priority-queue reset.
func (index *HCBSIndex) Distance(source, target int) (distance uint64, reachable bool, err error) {
	if index == nil || source < 0 || target < 0 || source >= len(index.rank) || target >= len(index.rank) {
		return 0, false, errors.New("HCBS query node out of range")
	}
	if source == target {
		return 0, true, nil
	}
	workspace := index.pool.Get().(*hcbsWorkspace)
	defer index.pool.Put(workspace)
	result := workspace.run(index, index.rank[source], index.rank[target])
	if result == hcbsInf {
		return 0, false, nil
	}
	return uint64(result), true, nil
}

type hcbsWorkspace struct {
	forwardQueue  hcbsEpochQueue
	backwardQueue hcbsEpochQueue
	forwardDist   []uint32
	backwardDist  []uint32
	forwardSeen   []uint32
	backwardSeen  []uint32
	epoch         uint32
}

func newHCBSWorkspace(n int) *hcbsWorkspace {
	return &hcbsWorkspace{
		forwardQueue:  newHCBSEpochQueue(n),
		backwardQueue: newHCBSEpochQueue(n),
		forwardDist:   make([]uint32, n),
		backwardDist:  make([]uint32, n),
		forwardSeen:   make([]uint32, n),
		backwardSeen:  make([]uint32, n),
	}
}

func (w *hcbsWorkspace) reset() {
	w.forwardQueue.clear()
	w.backwardQueue.clear()
	w.epoch++
	if w.epoch == 0 {
		clear(w.forwardSeen)
		clear(w.backwardSeen)
		w.epoch = 1
	}
}

func (w *hcbsWorkspace) run(index *HCBSIndex, source, target uint32) uint32 {
	w.reset()
	w.setForward(source, 0)
	w.setBackward(target, 0)
	incumbent := hcbsInf

	for {
		forwardFinished := w.forwardQueue.empty() || w.forwardQueue.peek().key >= incumbent
		backwardFinished := w.backwardQueue.empty() || w.backwardQueue.peek().key >= incumbent
		if forwardFinished && backwardFinished {
			return incumbent
		}
		switch {
		case forwardFinished:
			w.settleBackward(index, &incumbent)
		case backwardFinished:
			w.settleForward(index, &incumbent)
		case w.forwardQueue.peek().key <= w.backwardQueue.peek().key:
			w.settleForward(index, &incumbent)
		default:
			w.settleBackward(index, &incumbent)
		}
	}
}

func (w *hcbsWorkspace) setForward(node, distance uint32) {
	w.forwardSeen[node] = w.epoch
	w.forwardDist[node] = distance
	w.forwardQueue.push(hcbsQueueItem{id: node, key: distance})
}

func (w *hcbsWorkspace) setBackward(node, distance uint32) {
	w.backwardSeen[node] = w.epoch
	w.backwardDist[node] = distance
	w.backwardQueue.push(hcbsQueueItem{id: node, key: distance})
}

func (w *hcbsWorkspace) seenForward(node uint32) bool { return w.forwardSeen[node] == w.epoch }
func (w *hcbsWorkspace) seenBackward(node uint32) bool { return w.backwardSeen[node] == w.epoch }

func (w *hcbsWorkspace) settleForward(index *HCBSIndex, incumbent *uint32) {
	item := w.forwardQueue.pop()
	node, distance := item.id, item.key
	if w.seenBackward(node) {
		other := w.backwardDist[node]
		if distance < hcbsInf-other {
			candidate := distance + other
			if candidate < *incumbent {
				*incumbent = candidate
			}
		}
	}
	if w.stallForward(index, node) {
		return
	}
	start, end := index.forward.firstOut[node], index.forward.firstOut[node+1]
	for arc := start; arc < end; arc++ {
		head := index.forward.head[arc]
		weight := index.forward.weight[arc]
		if distance >= hcbsInf-weight {
			continue
		}
		candidate := distance + weight
		if !w.seenForward(head) {
			w.setForward(head, candidate)
		} else if candidate < w.forwardDist[head] {
			w.forwardDist[head] = candidate
			if w.forwardQueue.contains(head) {
				w.forwardQueue.decreaseKey(hcbsQueueItem{id: head, key: candidate})
			} else {
				w.forwardQueue.push(hcbsQueueItem{id: head, key: candidate})
			}
		}
	}
}

func (w *hcbsWorkspace) settleBackward(index *HCBSIndex, incumbent *uint32) {
	item := w.backwardQueue.pop()
	node, distance := item.id, item.key
	if w.seenForward(node) {
		other := w.forwardDist[node]
		if distance < hcbsInf-other {
			candidate := distance + other
			if candidate < *incumbent {
				*incumbent = candidate
			}
		}
	}
	if w.stallBackward(index, node) {
		return
	}
	start, end := index.backward.firstOut[node], index.backward.firstOut[node+1]
	for arc := start; arc < end; arc++ {
		head := index.backward.head[arc]
		weight := index.backward.weight[arc]
		if distance >= hcbsInf-weight {
			continue
		}
		candidate := distance + weight
		if !w.seenBackward(head) {
			w.setBackward(head, candidate)
		} else if candidate < w.backwardDist[head] {
			w.backwardDist[head] = candidate
			if w.backwardQueue.contains(head) {
				w.backwardQueue.decreaseKey(hcbsQueueItem{id: head, key: candidate})
			} else {
				w.backwardQueue.push(hcbsQueueItem{id: head, key: candidate})
			}
		}
	}
}

func (w *hcbsWorkspace) stallForward(index *HCBSIndex, node uint32) bool {
	current := w.forwardDist[node]
	start, end := index.backward.firstOut[node], index.backward.firstOut[node+1]
	for arc := start; arc < end; arc++ {
		x := index.backward.head[arc]
		if !w.seenForward(x) {
			continue
		}
		distance := w.forwardDist[x]
		weight := index.backward.weight[arc]
		if distance <= current && weight <= current-distance {
			return true
		}
	}
	return false
}

func (w *hcbsWorkspace) stallBackward(index *HCBSIndex, node uint32) bool {
	current := w.backwardDist[node]
	start, end := index.forward.firstOut[node], index.forward.firstOut[node+1]
	for arc := start; arc < end; arc++ {
		x := index.forward.head[arc]
		if !w.seenBackward(x) {
			continue
		}
		distance := w.backwardDist[x]
		weight := index.forward.weight[arc]
		if distance <= current && weight <= current-distance {
			return true
		}
	}
	return false
}

type hcbsQueueItem struct {
	id  uint32
	key uint32
}

type hcbsEpochQueue struct {
	pos      []uint32
	posEpoch []uint32
	heap     []hcbsQueueItem
	size     uint32
	epoch    uint32
}

func newHCBSEpochQueue(n int) hcbsEpochQueue {
	return hcbsEpochQueue{
		pos:      make([]uint32, n),
		posEpoch: make([]uint32, n),
		heap:     make([]hcbsQueueItem, n),
		epoch:    1,
	}
}

func (q *hcbsEpochQueue) empty() bool { return q.size == 0 }
func (q *hcbsEpochQueue) peek() hcbsQueueItem { return q.heap[0] }
func (q *hcbsEpochQueue) contains(id uint32) bool { return q.posEpoch[id] == q.epoch }

func (q *hcbsEpochQueue) clear() {
	q.size = 0
	q.epoch++
	if q.epoch == 0 {
		clear(q.posEpoch)
		q.epoch = 1
	}
}

func (q *hcbsEpochQueue) push(item hcbsQueueItem) {
	position := q.size
	q.size++
	q.heap[position] = item
	q.pos[item.id] = position
	q.posEpoch[item.id] = q.epoch
	q.moveUp(position)
}

func (q *hcbsEpochQueue) pop() hcbsQueueItem {
	q.size--
	result := q.heap[0]
	q.posEpoch[result.id] = 0
	if q.size == 0 {
		return result
	}
	q.heap[0] = q.heap[q.size]
	q.pos[q.heap[0].id] = 0
	q.moveDown(0)
	return result
}

func (q *hcbsEpochQueue) decreaseKey(item hcbsQueueItem) bool {
	position := q.pos[item.id]
	if q.heap[position].key <= item.key {
		return false
	}
	q.heap[position].key = item.key
	q.moveUp(position)
	return true
}

func (q *hcbsEpochQueue) swap(a, b uint32) {
	q.heap[a], q.heap[b] = q.heap[b], q.heap[a]
	q.pos[q.heap[a].id] = a
	q.pos[q.heap[b].id] = b
}

func (q *hcbsEpochQueue) moveUp(position uint32) {
	for position != 0 {
		parent := (position - 1) / 4
		if q.heap[parent].key <= q.heap[position].key {
			return
		}
		q.swap(parent, position)
		position = parent
	}
}

func (q *hcbsEpochQueue) moveDown(position uint32) {
	for {
		first := 4*position + 1
		if first >= q.size {
			return
		}
		best := first
		end := first + 4
		if end > q.size {
			end = q.size
		}
		for child := first + 1; child < end; child++ {
			if q.heap[child].key < q.heap[best].key {
				best = child
			}
		}
		if q.heap[position].key <= q.heap[best].key {
			return
		}
		q.swap(position, best)
		position = best
	}
}

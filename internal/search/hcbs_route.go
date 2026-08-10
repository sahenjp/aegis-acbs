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

var hcbsPathSidecarMagic = [8]byte{'A', 'E', 'G', 'H', 'C', 'P', '0', '1'}

// HCBSRouteIndex layers optional shortcut-unpacking metadata over the lean
// distance-only HCBS index. Keeping path metadata in a second sidecar means
// Distance retains the exact data layout and hot path of HCBSIndex.
type HCBSRouteIndex struct {
	base          *HCBSIndex
	forward       hcbsPathSide
	backward      hcbsPathSide
	inputArcCount uint64
	pool          sync.Pool
}

type hcbsPathSide struct {
	original []byte
	first    []uint32
	second   []uint32
}

// HCBSRoute is an exact shortest path in original Aegis node numbering.
type HCBSRoute struct {
	Distance  uint64
	Reachable bool
	Path      []int
}

// LoadHCBSRouteIndex loads a distance sidecar and its optional path companion.
// The path file is bound to both the exact .aegis graph and exact .hcbs bytes
// with SHA-256 fingerprints, preventing shortcut metadata from being paired
// with a different hierarchy instance.
func LoadHCBSRouteIndex(baseSidecarPath, pathSidecarPath, graphPath string) (*HCBSRouteIndex, error) {
	base, err := LoadHCBSIndex(baseSidecarPath, graphPath)
	if err != nil {
		return nil, err
	}
	graphDigest, err := sha256File(graphPath)
	if err != nil {
		return nil, fmt.Errorf("hash graph: %w", err)
	}
	baseDigest, err := sha256File(baseSidecarPath)
	if err != nil {
		return nil, fmt.Errorf("hash HCBS sidecar: %w", err)
	}

	f, err := os.Open(pathSidecarPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)

	var magic [8]byte
	if _, err := io.ReadFull(r, magic[:]); err != nil {
		return nil, err
	}
	if magic != hcbsPathSidecarMagic {
		return nil, errors.New("not a supported HCBS path sidecar")
	}
	var embeddedGraphDigest, embeddedBaseDigest [sha256.Size]byte
	if _, err := io.ReadFull(r, embeddedGraphDigest[:]); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(r, embeddedBaseDigest[:]); err != nil {
		return nil, err
	}
	if embeddedGraphDigest != graphDigest {
		return nil, errors.New("HCBS path sidecar graph fingerprint mismatch")
	}
	if embeddedBaseDigest != baseDigest {
		return nil, errors.New("HCBS path sidecar hierarchy fingerprint mismatch")
	}

	var nodeCount uint32
	var forwardCount, backwardCount, inputArcCount uint64
	for _, value := range []any{&nodeCount, &forwardCount, &backwardCount, &inputArcCount} {
		if err := binary.Read(r, binary.LittleEndian, value); err != nil {
			return nil, err
		}
	}
	if int(nodeCount) != len(base.rank) || forwardCount != uint64(len(base.forward.head)) || backwardCount != uint64(len(base.backward.head)) {
		return nil, errors.New("HCBS path sidecar dimensions do not match hierarchy")
	}
	if inputArcCount > 20_000_000_000 {
		return nil, errors.New("HCBS path sidecar input arc count is unreasonable")
	}

	forward, err := readHCBSPathSide(r, int(forwardCount), int(nodeCount), inputArcCount, len(base.forward.head), len(base.backward.head))
	if err != nil {
		return nil, fmt.Errorf("read forward HCBS path metadata: %w", err)
	}
	backward, err := readHCBSPathSide(r, int(backwardCount), int(nodeCount), inputArcCount, len(base.forward.head), len(base.backward.head))
	if err != nil {
		return nil, fmt.Errorf("read backward HCBS path metadata: %w", err)
	}
	if extra, err := r.ReadByte(); err == nil {
		_ = extra
		return nil, errors.New("HCBS path sidecar has trailing data")
	} else if !errors.Is(err, io.EOF) {
		return nil, err
	}

	index := &HCBSRouteIndex{base: base, forward: forward, backward: backward, inputArcCount: inputArcCount}
	n := len(base.rank)
	index.pool.New = func() any { return newHCBSRouteWorkspace(n) }
	return index, nil
}

func readHCBSPathSide(r io.Reader, arcCount, nodeCount int, inputArcCount uint64, forwardCount, backwardCount int) (hcbsPathSide, error) {
	flagBytes := (arcCount + 7) / 8
	side := hcbsPathSide{
		original: make([]byte, flagBytes),
		first:    make([]uint32, arcCount),
		second:   make([]uint32, arcCount),
	}
	if _, err := io.ReadFull(r, side.original); err != nil {
		return hcbsPathSide{}, err
	}
	if err := binary.Read(r, binary.LittleEndian, side.first); err != nil {
		return hcbsPathSide{}, err
	}
	if err := binary.Read(r, binary.LittleEndian, side.second); err != nil {
		return hcbsPathSide{}, err
	}
	for arc := 0; arc < arcCount; arc++ {
		if side.isOriginal(uint32(arc)) {
			if uint64(side.first[arc]) >= inputArcCount {
				return hcbsPathSide{}, fmt.Errorf("original input arc out of range at hierarchy arc %d", arc)
			}
			if int(side.second[arc]) >= nodeCount {
				return hcbsPathSide{}, fmt.Errorf("original path head out of range at hierarchy arc %d", arc)
			}
			continue
		}
		if int(side.first[arc]) >= backwardCount || int(side.second[arc]) >= forwardCount {
			return hcbsPathSide{}, fmt.Errorf("shortcut child out of range at hierarchy arc %d", arc)
		}
	}
	return side, nil
}

func (side hcbsPathSide) isOriginal(arc uint32) bool {
	return side.original[arc>>3]&(1<<uint(arc&7)) != 0
}

// Route returns an exact shortest path while leaving HCBSIndex.Distance's
// distance-only workspace and queue unchanged.
func (index *HCBSRouteIndex) Route(source, target int) (HCBSRoute, error) {
	if index == nil || index.base == nil || source < 0 || target < 0 || source >= len(index.base.rank) || target >= len(index.base.rank) {
		return HCBSRoute{}, errors.New("HCBS route node out of range")
	}
	if source == target {
		return HCBSRoute{Reachable: true, Path: []int{source}}, nil
	}
	workspace := index.pool.Get().(*hcbsRouteWorkspace)
	defer index.pool.Put(workspace)
	distance, meeting := workspace.run(index.base, index.base.rank[source], index.base.rank[target])
	if distance == hcbsInf || meeting == hcbsInvalidNode {
		return HCBSRoute{}, nil
	}
	path, err := workspace.reconstruct(index, source, target, index.base.rank[source], index.base.rank[target], meeting)
	if err != nil {
		return HCBSRoute{}, err
	}
	return HCBSRoute{Distance: uint64(distance), Reachable: true, Path: path}, nil
}

const hcbsInvalidNode = ^uint32(0)

type hcbsRouteWorkspace struct {
	forwardQueue  hcbsEpochQueue
	backwardQueue hcbsEpochQueue
	forwardDist   []uint32
	backwardDist  []uint32
	forwardSeen   []uint32
	backwardSeen  []uint32
	forwardParent []uint32
	backwardParent []uint32
	forwardArc    []uint32
	backwardArc   []uint32
	epoch         uint32
}

func newHCBSRouteWorkspace(n int) *hcbsRouteWorkspace {
	return &hcbsRouteWorkspace{
		forwardQueue:   newHCBSEpochQueue(n),
		backwardQueue:  newHCBSEpochQueue(n),
		forwardDist:    make([]uint32, n),
		backwardDist:   make([]uint32, n),
		forwardSeen:    make([]uint32, n),
		backwardSeen:   make([]uint32, n),
		forwardParent:  make([]uint32, n),
		backwardParent: make([]uint32, n),
		forwardArc:     make([]uint32, n),
		backwardArc:    make([]uint32, n),
	}
}

func (w *hcbsRouteWorkspace) reset() {
	w.forwardQueue.clear()
	w.backwardQueue.clear()
	w.epoch++
	if w.epoch == 0 {
		clear(w.forwardSeen)
		clear(w.backwardSeen)
		w.epoch = 1
	}
}

func (w *hcbsRouteWorkspace) run(index *HCBSIndex, source, target uint32) (uint32, uint32) {
	w.reset()
	w.setForward(source, 0, hcbsInvalidNode, hcbsInvalidNode)
	w.setBackward(target, 0, hcbsInvalidNode, hcbsInvalidNode)
	incumbent, meeting := uint32(hcbsInf), uint32(hcbsInvalidNode)
	for {
		forwardFinished := w.forwardQueue.empty() || w.forwardQueue.peek().key >= incumbent
		backwardFinished := w.backwardQueue.empty() || w.backwardQueue.peek().key >= incumbent
		if forwardFinished && backwardFinished {
			return incumbent, meeting
		}
		switch {
		case forwardFinished:
			w.settleBackward(index, &incumbent, &meeting)
		case backwardFinished:
			w.settleForward(index, &incumbent, &meeting)
		case w.forwardQueue.peek().key <= w.backwardQueue.peek().key:
			w.settleForward(index, &incumbent, &meeting)
		default:
			w.settleBackward(index, &incumbent, &meeting)
		}
	}
}

func (w *hcbsRouteWorkspace) setForward(node, distance, parent, arc uint32) {
	w.forwardSeen[node] = w.epoch
	w.forwardDist[node] = distance
	w.forwardParent[node] = parent
	w.forwardArc[node] = arc
	w.forwardQueue.push(hcbsQueueItem{id: node, key: distance})
}

func (w *hcbsRouteWorkspace) setBackward(node, distance, parent, arc uint32) {
	w.backwardSeen[node] = w.epoch
	w.backwardDist[node] = distance
	w.backwardParent[node] = parent
	w.backwardArc[node] = arc
	w.backwardQueue.push(hcbsQueueItem{id: node, key: distance})
}

func (w *hcbsRouteWorkspace) seenForward(node uint32) bool  { return w.forwardSeen[node] == w.epoch }
func (w *hcbsRouteWorkspace) seenBackward(node uint32) bool { return w.backwardSeen[node] == w.epoch }

func updateHCBSRouteIncumbent(distance, other, node uint32, incumbent, meeting *uint32) {
	if distance >= hcbsInf-other {
		return
	}
	candidate := distance + other
	if candidate < *incumbent {
		*incumbent = candidate
		*meeting = node
	}
}

func (w *hcbsRouteWorkspace) settleForward(index *HCBSIndex, incumbent, meeting *uint32) {
	item := w.forwardQueue.pop()
	node, distance := item.id, item.key
	if w.seenBackward(node) {
		updateHCBSRouteIncumbent(distance, w.backwardDist[node], node, incumbent, meeting)
	}
	if w.stallForward(index, node) {
		return
	}
	start, end := index.forward.firstOut[node], index.forward.firstOut[node+1]
	for arc := start; arc < end; arc++ {
		head, weight := index.forward.head[arc], index.forward.weight[arc]
		if distance >= hcbsInf-weight {
			continue
		}
		candidate := distance + weight
		if !w.seenForward(head) {
			w.setForward(head, candidate, node, arc)
		} else if candidate < w.forwardDist[head] {
			w.forwardDist[head] = candidate
			w.forwardParent[head] = node
			w.forwardArc[head] = arc
			if w.forwardQueue.contains(head) {
				w.forwardQueue.decreaseKey(hcbsQueueItem{id: head, key: candidate})
			} else {
				w.forwardQueue.push(hcbsQueueItem{id: head, key: candidate})
			}
		}
	}
}

func (w *hcbsRouteWorkspace) settleBackward(index *HCBSIndex, incumbent, meeting *uint32) {
	item := w.backwardQueue.pop()
	node, distance := item.id, item.key
	if w.seenForward(node) {
		updateHCBSRouteIncumbent(distance, w.forwardDist[node], node, incumbent, meeting)
	}
	if w.stallBackward(index, node) {
		return
	}
	start, end := index.backward.firstOut[node], index.backward.firstOut[node+1]
	for arc := start; arc < end; arc++ {
		head, weight := index.backward.head[arc], index.backward.weight[arc]
		if distance >= hcbsInf-weight {
			continue
		}
		candidate := distance + weight
		if !w.seenBackward(head) {
			w.setBackward(head, candidate, node, arc)
		} else if candidate < w.backwardDist[head] {
			w.backwardDist[head] = candidate
			w.backwardParent[head] = node
			w.backwardArc[head] = arc
			if w.backwardQueue.contains(head) {
				w.backwardQueue.decreaseKey(hcbsQueueItem{id: head, key: candidate})
			} else {
				w.backwardQueue.push(hcbsQueueItem{id: head, key: candidate})
			}
		}
	}
}

func (w *hcbsRouteWorkspace) stallForward(index *HCBSIndex, node uint32) bool {
	current := w.forwardDist[node]
	start, end := index.backward.firstOut[node], index.backward.firstOut[node+1]
	for arc := start; arc < end; arc++ {
		x := index.backward.head[arc]
		if !w.seenForward(x) {
			continue
		}
		distance, weight := w.forwardDist[x], index.backward.weight[arc]
		if distance <= current && weight <= current-distance {
			return true
		}
	}
	return false
}

func (w *hcbsRouteWorkspace) stallBackward(index *HCBSIndex, node uint32) bool {
	current := w.backwardDist[node]
	start, end := index.forward.firstOut[node], index.forward.firstOut[node+1]
	for arc := start; arc < end; arc++ {
		x := index.forward.head[arc]
		if !w.seenBackward(x) {
			continue
		}
		distance, weight := w.backwardDist[x], index.forward.weight[arc]
		if distance <= current && weight <= current-distance {
			return true
		}
	}
	return false
}

func (w *hcbsRouteWorkspace) reconstruct(index *HCBSRouteIndex, source, target int, sourceRank, targetRank, meeting uint32) ([]int, error) {
	forwardArcs := make([]uint32, 0, 32)
	x := meeting
	for steps := 0; x != sourceRank; steps++ {
		if steps > len(index.base.rank) || !w.seenForward(x) || w.forwardParent[x] == hcbsInvalidNode {
			return nil, errors.New("invalid HCBS forward predecessor chain")
		}
		forwardArcs = append(forwardArcs, w.forwardArc[x])
		x = w.forwardParent[x]
	}

	path := make([]int, 1, len(forwardArcs)+16)
	path[0] = source
	for i := len(forwardArcs) - 1; i >= 0; i-- {
		if err := index.appendUnpacked(&path, true, forwardArcs[i]); err != nil {
			return nil, err
		}
	}

	x = meeting
	for steps := 0; x != targetRank; steps++ {
		if steps > len(index.base.rank) || !w.seenBackward(x) || w.backwardParent[x] == hcbsInvalidNode {
			return nil, errors.New("invalid HCBS backward predecessor chain")
		}
		if err := index.appendUnpacked(&path, false, w.backwardArc[x]); err != nil {
			return nil, err
		}
		x = w.backwardParent[x]
	}
	if len(path) == 0 || path[0] != source || path[len(path)-1] != target {
		return nil, errors.New("HCBS shortcut unpacking produced invalid endpoints")
	}
	return path, nil
}

type hcbsUnpackItem struct {
	forward bool
	arc     uint32
}

func (index *HCBSRouteIndex) appendUnpacked(path *[]int, forward bool, root uint32) error {
	stack := []hcbsUnpackItem{{forward: forward, arc: root}}
	maxOps := 2*(len(index.forward.first)+len(index.backward.first)) + 1
	for ops := 0; len(stack) > 0; ops++ {
		if ops > maxOps {
			return errors.New("HCBS shortcut metadata contains a cycle")
		}
		last := len(stack) - 1
		item := stack[last]
		stack = stack[:last]
		meta := index.backward
		if item.forward {
			meta = index.forward
		}
		if int(item.arc) >= len(meta.first) {
			return errors.New("HCBS shortcut arc out of range")
		}
		if meta.isOriginal(item.arc) {
			*path = append(*path, int(meta.second[item.arc]))
			continue
		}
		// RoutingKit shortcut semantics are identical on both roots: expand the
		// backward child first and then the forward child. Stack order is LIFO.
		stack = append(stack,
			hcbsUnpackItem{forward: true, arc: meta.second[item.arc]},
			hcbsUnpackItem{forward: false, arc: meta.first[item.arc]},
		)
	}
	return nil
}

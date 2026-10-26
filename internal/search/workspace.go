package search

import (
	"sync"

	"github.com/sahenjp/aegis-acbs/internal/graph"
)

type singleWorkspace struct {
	dist    []uint64
	prev    []int
	touched []int
	q       minHeap
}

var singleWorkspacePool = sync.Pool{
	New: func() any { return &singleWorkspace{} },
}

func acquireSingleWorkspace(n int) *singleWorkspace {
	w := singleWorkspacePool.Get().(*singleWorkspace)
	if cap(w.dist) < n {
		w.dist = make([]uint64, n)
		w.prev = make([]int, n)
		for i := range w.dist {
			w.dist[i] = inf
			w.prev[i] = -1
		}
	} else {
		w.dist = w.dist[:n]
		w.prev = w.prev[:n]
	}
	w.touched = w.touched[:0]
	w.q = w.q[:0]
	return w
}

func (w *singleWorkspace) touch(v int) {
	if w.dist[v] == inf {
		w.touched = append(w.touched, v)
	}
}

func releaseSingleWorkspace(w *singleWorkspace) {
	for _, v := range w.touched {
		w.dist[v] = inf
		w.prev[v] = -1
	}
	w.touched = w.touched[:0]
	w.q = w.q[:0]
	singleWorkspacePool.Put(w)
}

type biWorkspace struct {
	df, db              []uint64
	pf, pb              []int32
	settledF, settledB  []bool
	phi                 []int64
	hForward, hBackward []uint64
	phiKnown, hKnown    []bool
	touchedF, touchedB  []int32
	touchedPhi          []int32
	qf, qb              radixHeap
}

var biWorkspacePool = sync.Pool{
	New: func() any { return &biWorkspace{} },
}

func acquireBiWorkspace(n int) *biWorkspace {
	w := biWorkspacePool.Get().(*biWorkspace)
	if cap(w.df) < n {
		w.df = make([]uint64, n)
		w.db = make([]uint64, n)
		w.pf = make([]int32, n)
		w.pb = make([]int32, n)
		w.settledF = make([]bool, n)
		w.settledB = make([]bool, n)
		w.phi = make([]int64, n)
		w.hForward = make([]uint64, n)
		w.hBackward = make([]uint64, n)
		w.phiKnown = make([]bool, n)
		w.hKnown = make([]bool, n)
		for i := range w.df {
			w.df[i], w.db[i] = inf, inf
			w.pf[i], w.pb[i] = -1, -1
		}
	} else {
		w.df = w.df[:n]
		w.db = w.db[:n]
		w.pf = w.pf[:n]
		w.pb = w.pb[:n]
		w.settledF = w.settledF[:n]
		w.settledB = w.settledB[:n]
		w.phi = w.phi[:n]
		w.hForward = w.hForward[:n]
		w.hBackward = w.hBackward[:n]
		w.phiKnown = w.phiKnown[:n]
		w.hKnown = w.hKnown[:n]
	}
	w.touchedF = w.touchedF[:0]
	w.touchedB = w.touchedB[:0]
	w.touchedPhi = w.touchedPhi[:0]
	w.qf.reset()
	w.qb.reset()
	return w
}

func (w *biWorkspace) touchForward(v int) {
	if w.df[v] == inf {
		w.touchedF = append(w.touchedF, int32(v))
	}
}

func (w *biWorkspace) touchBackward(v int) {
	if w.db[v] == inf {
		w.touchedB = append(w.touchedB, int32(v))
	}
}

func (w *biWorkspace) potential(g *graph.Graph, model acbsPotential, v int) (phi int64, fresh bool) {
	if w.phiKnown[v] {
		return w.phi[v], false
	}
	phi = model.phi(g, v)
	w.phi[v] = phi
	w.phiKnown[v] = true
	w.touchedPhi = append(w.touchedPhi, int32(v))
	return phi, true
}

func (w *biWorkspace) heuristicBounds(g *graph.Graph, model acbsPotential, v int) (forward, backward uint64, fresh bool) {
	if w.hKnown[v] {
		return w.hForward[v], w.hBackward[v], false
	}
	forward, backward = model.bounds(g, v)
	w.hForward[v] = forward
	w.hBackward[v] = backward
	w.hKnown[v] = true
	if !w.phiKnown[v] {
		w.phi[v] = model.phi(g, v)
		w.phiKnown[v] = true
		w.touchedPhi = append(w.touchedPhi, int32(v))
	}
	return forward, backward, true
}

func releaseBiWorkspace(w *biWorkspace) {
	for _, raw := range w.touchedF {
		v := int(raw)
		w.df[v] = inf
		w.pf[v] = -1
		w.settledF[v] = false
	}
	for _, raw := range w.touchedB {
		v := int(raw)
		w.db[v] = inf
		w.pb[v] = -1
		w.settledB[v] = false
	}
	for _, raw := range w.touchedPhi {
		v := int(raw)
		w.phi[v] = 0
		w.hForward[v] = 0
		w.hBackward[v] = 0
		w.phiKnown[v] = false
		w.hKnown[v] = false
	}
	w.touchedF = w.touchedF[:0]
	w.touchedB = w.touchedB[:0]
	w.touchedPhi = w.touchedPhi[:0]
	w.qf.reset()
	w.qb.reset()
	biWorkspacePool.Put(w)
}

package search

import "sync"

// syncPoolPB caches PB workspaces so steady-state queries do not allocate label
// arrays. Bucket capacities are reset by slicing, never discarded, so bucket
// slices survive across queries.
type syncPoolPB struct {
	pool sync.Pool
}

func (p *syncPoolPB) get(n int) *pbWorkspace {
	w, _ := p.pool.Get().(*pbWorkspace)
	if w == nil {
		w = &pbWorkspace{}
	}
	if cap(w.df) < n {
		w.df = make([]uint64, n)
		w.db = make([]uint64, n)
		w.pf = make([]int32, n)
		w.pb = make([]int32, n)
		w.settledF = make([]bool, n)
		w.settledB = make([]bool, n)
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
	}
	w.touched = w.touched[:0]
	clear(w.settledF)
	clear(w.settledB)
	w.bf.reset()
	w.bb.reset()
	return w
}

func (p *syncPoolPB) put(w *pbWorkspace) {
	for _, raw := range w.touched {
		v := int(raw)
		w.df[v] = inf
		w.db[v] = inf
		w.pf[v] = -1
		w.pb[v] = -1
	}
	w.touched = w.touched[:0]
	w.bf.reset()
	w.bb.reset()
	p.pool.Put(w)
}

package search

// catchRand is a tiny deterministic PRNG for the CATCH tests. It is kept local
// so the tests do not depend on math/rand's global state and stay reproducible.
type catchRand struct{ s uint64 }

func newCatchRand(seed uint64) *catchRand {
	return &catchRand{s: seed*6364136223846793005 + 1442695040888963407}
}

func (r *catchRand) next() uint64 {
	r.s = r.s*6364136223846793005 + 1442695040888963407
	x := r.s
	x ^= x >> 33
	x *= 0xff51afd7ed558ccd
	x ^= x >> 33
	return x
}

func (r *catchRand) intn(n int) int { return int(r.next() % uint64(n)) }

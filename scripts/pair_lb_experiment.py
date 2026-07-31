#!/usr/bin/env python3
from __future__ import annotations

from pathlib import Path


def replace_once(text: str, old: str, new: str, label: str) -> str:
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{label}: expected one match, found {count}")
    return text.replace(old, new, 1)


def main() -> None:
    acbs_path = Path("internal/search/acbs.go")
    acbs = acbs_path.read_text()

    acbs = replace_once(
        acbs,
        'import (\n\t"context"\n\t"math"\n)',
        'import (\n\t"context"\n\t"math"\n\t"sort"\n)',
        "sort import",
    )
    acbs = replace_once(
        acbs,
        'acbsEntropicSchedulerVersion = "entropic-proof-rate-v2"',
        'acbsEntropicSchedulerVersion = "pair-lower-bound-exact-scan-v2"',
        "version",
    )
    acbs = replace_once(
        acbs,
        "\tentropic   bool\n\tpruning    bool\n",
        "\tentropic   bool\n\tpairBound  bool\n\tpruning    bool\n",
        "option field",
    )
    acbs = replace_once(
        acbs,
        "\t\talgorithm: AegisEntropic,\n\t\tadaptive:  true,\n\t\tentropic:  true,\n\t\tpruning:   false,\n",
        "\t\talgorithm: AegisEntropic,\n\t\tadaptive:  true,\n\t\tpairBound: true,\n\t\tpruning:   false,\n",
        "candidate options",
    )

    old_bound = '''\t\tlowerBound := saturatingAdd(frontF.priority, frontB.priority)
\t\tif bestReduced != inf && lowerBound >= bestReduced {
\t\t\tstats.TerminationLowerBound = reducedToOriginalLowerBound(lowerBound, phiS, phiT)
\t\t\tif stats.TerminationLowerBound > best {
\t\t\t\tstats.TerminationLowerBound = best
\t\t\t}
\t\t\tterminatedByBound = true
\t\t\tbreak
\t\t}
'''
    new_bound = '''\t\tlowerBound := saturatingAdd(frontF.priority, frontB.priority)
\t\tif bestReduced != inf {
\t\t\toriginalLowerBound := reducedToOriginalLowerBound(lowerBound, phiS, phiT)
\t\t\tif opts.pairBound {
\t\t\t\tstats.PairBoundScans++
\t\t\t\tpairBound := acbsPairLowerBoundScan(g, potential, w, df, db, settledF, settledB)
\t\t\t\tpairTightened := pairBound > originalLowerBound
\t\t\t\tif pairTightened {
\t\t\t\t\tstats.PairBoundTightens++
\t\t\t\t\tgain := pairBound - originalLowerBound
\t\t\t\t\tif gain > stats.PairBoundMaxGain {
\t\t\t\t\t\tstats.PairBoundMaxGain = gain
\t\t\t\t\t}
\t\t\t\t\toriginalLowerBound = pairBound
\t\t\t\t}
\t\t\t\tif pairTightened && originalLowerBound >= best {
\t\t\t\t\tstats.PairBoundTerminations++
\t\t\t\t}
\t\t\t}
\t\t\tif originalLowerBound >= best {
\t\t\t\tstats.TerminationLowerBound = best
\t\t\t\tterminatedByBound = true
\t\t\t\tbreak
\t\t\t}
\t\t}
'''
    acbs = replace_once(acbs, old_bound, new_bound, "outer bound")

    helper = r'''
type acbsPairPoint struct {
	f uint64
	g uint64
}

func acbsPairLowerBoundScan(
	g *graph.Graph,
	potential acbsPotential,
	w *biWorkspace,
	df, db []uint64,
	settledF, settledB []bool,
) uint64 {
	forward := make([]acbsPairPoint, 0, len(w.touchedF))
	for _, raw := range w.touchedF {
		v := int(raw)
		if settledF[v] || df[v] == inf {
			continue
		}
		hForward, _ := potential.bounds(g, v)
		forward = append(forward, acbsPairPoint{
			f: saturatingAdd(df[v], hForward),
			g: df[v],
		})
	}

	backward := make([]acbsPairPoint, 0, len(w.touchedB))
	for _, raw := range w.touchedB {
		v := int(raw)
		if settledB[v] || db[v] == inf {
			continue
		}
		_, hBackward := potential.bounds(g, v)
		backward = append(backward, acbsPairPoint{
			f: saturatingAdd(db[v], hBackward),
			g: db[v],
		})
	}
	return acbsPairLowerBoundPoints(forward, backward)
}

func acbsPairLowerBoundPoints(forward, backward []acbsPairPoint) uint64 {
	if len(forward) == 0 || len(backward) == 0 {
		return 0
	}
	sort.Slice(forward, func(i, j int) bool {
		if forward[i].f == forward[j].f {
			return forward[i].g < forward[j].g
		}
		return forward[i].f < forward[j].f
	})
	sort.Slice(backward, func(i, j int) bool {
		if backward[i].f == backward[j].f {
			return backward[i].g < backward[j].g
		}
		return backward[i].f < backward[j].f
	})

	// At a threshold C, a pair with pair-lb <= C exists iff each frontier has
	// a node with f <= C and the minimum corresponding g-values sum to <= C.
	// Prefix minima make that predicate logarithmic after sorting.
	for i := 1; i < len(forward); i++ {
		if forward[i-1].g < forward[i].g {
			forward[i].g = forward[i-1].g
		}
	}
	for i := 1; i < len(backward); i++ {
		if backward[i-1].g < backward[i].g {
			backward[i].g = backward[i-1].g
		}
	}

	lower := forward[0].f
	if backward[0].f > lower {
		lower = backward[0].f
	}
	if cross := saturatingAdd(forward[len(forward)-1].g, backward[len(backward)-1].g); cross > lower {
		lower = cross
	}

	upper := forward[0].f
	if backward[0].f > upper {
		upper = backward[0].f
	}
	if cross := saturatingAdd(forward[0].g, backward[0].g); cross > upper {
		upper = cross
	}
	if lower >= upper {
		return lower
	}

	feasible := func(limit uint64) bool {
		fi := sort.Search(len(forward), func(i int) bool { return forward[i].f > limit })
		bi := sort.Search(len(backward), func(i int) bool { return backward[i].f > limit })
		if fi == 0 || bi == 0 {
			return false
		}
		fg := forward[fi-1].g
		bg := backward[bi-1].g
		return fg <= limit && bg <= limit-fg
	}

	for lower < upper {
		mid := lower + (upper-lower)/2
		if feasible(mid) {
			upper = mid
		} else {
			lower = mid + 1
		}
	}
	return lower
}

'''
    acbs = replace_once(
        acbs,
        "func boundCannotImprove(gCost, heuristic, incumbent uint64) bool {\n",
        helper + "func boundCannotImprove(gCost, heuristic, incumbent uint64) bool {\n",
        "pair-bound helper",
    )
    acbs_path.write_text(acbs)

    search_path = Path("internal/search/search.go")
    search = search_path.read_text()
    search = replace_once(
        search,
        "\tPotentialModel             string    `json:\"potentialModel,omitempty\"`\n",
        "\tPotentialModel             string    `json:\"potentialModel,omitempty\"`\n"
        "\tPairBoundScans             uint64    `json:\"pairBoundScans,omitempty\"`\n"
        "\tPairBoundTightens          uint64    `json:\"pairBoundTightens,omitempty\"`\n"
        "\tPairBoundTerminations      uint64    `json:\"pairBoundTerminations,omitempty\"`\n"
        "\tPairBoundMaxGain           uint64    `json:\"pairBoundMaxGain,omitempty\"`\n",
        "stats fields",
    )
    search_path.write_text(search)

    Path("internal/search/acbs_pair_bound_test.go").write_text(
        '''package search

import (
	"context"
	"testing"
)

func TestACBSPairBoundCandidateRemainsExact(t *testing.T) {
	g := gridGraph(t, 48, 48, true)
	for source := 0; source < len(g.Nodes); source += 97 {
		for target := 13; target < len(g.Nodes); target += 131 {
			want, err := Run(context.Background(), g, source, target, Dijkstra)
			if err != nil {
				t.Fatal(err)
			}
			got, err := Run(context.Background(), g, source, target, AegisEntropic)
			if err != nil {
				t.Fatal(err)
			}
			if got.Stats.Distance != want.Stats.Distance || got.Stats.OptimalityGap != 0 {
				t.Fatalf("%d -> %d: got=%+v want=%+v", source, target, got.Stats, want.Stats)
			}
		}
	}
}

func TestACBSPairLowerBoundPointsCapturesCrossCorrelation(t *testing.T) {
	forward := []acbsPairPoint{{f: 5, g: 100}, {f: 10, g: 1}}
	backward := []acbsPairPoint{{f: 5, g: 100}, {f: 10, g: 1}}
	if got := acbsPairLowerBoundPoints(forward, backward); got != 10 {
		t.Fatalf("pair lower bound = %d, want 10", got)
	}
}
'''
    )


if __name__ == "__main__":
    main()

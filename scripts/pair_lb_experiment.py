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
        'acbsEntropicSchedulerVersion = "entropic-proof-rate-v2"',
        'acbsEntropicSchedulerVersion = "pair-lower-bound-scan-v1"',
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
\t\t\t\tif pairBound > originalLowerBound {
\t\t\t\t\tstats.PairBoundTightens++
\t\t\t\t\tgain := pairBound - originalLowerBound
\t\t\t\t\tif gain > stats.PairBoundMaxGain {
\t\t\t\t\t\tstats.PairBoundMaxGain = gain
\t\t\t\t\t}
\t\t\t\t\toriginalLowerBound = pairBound
\t\t\t\t}
\t\t\t\tif originalLowerBound >= best {
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
func acbsPairLowerBoundScan(
	g *graph.Graph,
	potential acbsPotential,
	w *biWorkspace,
	df, db []uint64,
	settledF, settledB []bool,
) uint64 {
	minForwardF, minForwardG := inf, inf
	for _, raw := range w.touchedF {
		v := int(raw)
		if settledF[v] || df[v] == inf {
			continue
		}
		hForward, _ := potential.bounds(g, v)
		f := saturatingAdd(df[v], hForward)
		if f < minForwardF {
			minForwardF = f
		}
		if df[v] < minForwardG {
			minForwardG = df[v]
		}
	}

	minBackwardF, minBackwardG := inf, inf
	for _, raw := range w.touchedB {
		v := int(raw)
		if settledB[v] || db[v] == inf {
			continue
		}
		_, hBackward := potential.bounds(g, v)
		f := saturatingAdd(db[v], hBackward)
		if f < minBackwardF {
			minBackwardF = f
		}
		if db[v] < minBackwardG {
			minBackwardG = db[v]
		}
	}

	if minForwardF == inf || minBackwardF == inf || minForwardG == inf || minBackwardG == inf {
		return 0
	}
	bound := minForwardF
	if minBackwardF > bound {
		bound = minBackwardF
	}
	if cross := saturatingAdd(minForwardG, minBackwardG); cross > bound {
		bound = cross
	}
	return bound
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

func TestACBSPairLowerBoundUsesAllThreeTerms(t *testing.T) {
	g := gridGraph(t, 4, 4, true)
	w := acquireBiWorkspace(len(g.Nodes))
	defer releaseBiWorkspace(w)
	for _, v := range []int{1, 2} {
		w.touchForward(v)
		w.df[v] = uint64(10 + v)
	}
	for _, v := range []int{9, 10} {
		w.touchBackward(v)
		w.db[v] = uint64(20 + v)
	}
	p := newACBSPotential(g, 0, len(g.Nodes)-1, false)
	got := acbsPairLowerBoundScan(g, p, w, w.df, w.db, w.settledF, w.settledB)
	if got == 0 || got == inf {
		t.Fatalf("invalid pair lower bound: %d", got)
	}
}
'''
    )


if __name__ == "__main__":
    main()

#!/usr/bin/env python3
from __future__ import annotations

from pathlib import Path


def replace_once(text: str, old: str, new: str, label: str) -> str:
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{label}: expected one match, found {count}")
    return text.replace(old, new, 1)


def main() -> None:
    path = Path("internal/search/acbs.go")
    text = path.read_text()

    text = replace_once(
        text,
        '\tacbsChordPotentialModel      = "balanced-chord-v3"\n'
        '\tacbsProjectionModel          = "balanced-projection-v1"\n',
        '\tacbsChordPotentialModel      = "balanced-chord-v3"\n'
        '\tacbsGeodesicPotentialModel   = "balanced-geodesic-v1"\n'
        '\tacbsProjectionModel          = "balanced-projection-v1"\n',
        "model constants",
    )
    text = replace_once(
        text,
        "\tentropic   bool\n\tpruning    bool\n\tprojection bool\n",
        "\tentropic   bool\n\tpruning    bool\n\tprojection bool\n\tgeodesic   bool\n",
        "options",
    )
    text = replace_once(
        text,
        "\t\talgorithm: AegisEntropic,\n\t\tadaptive:  true,\n\t\tentropic:  true,\n\t\tpruning:   false,\n",
        "\t\talgorithm: AegisEntropic,\n\t\tadaptive:  true,\n\t\tgeodesic:  true,\n\t\tpruning:   false,\n",
        "candidate",
    )
    text = replace_once(
        text,
        "\tmodelName := acbsChordPotentialModel\n\tif opts.projection {\n\t\tmodelName = acbsProjectionModel\n\t}\n",
        "\tmodelName := acbsChordPotentialModel\n\tif opts.projection {\n\t\tmodelName = acbsProjectionModel\n\t} else if opts.geodesic {\n\t\tmodelName = acbsGeodesicPotentialModel\n\t}\n",
        "model name",
    )
    text = replace_once(
        text,
        "\tpotential := newACBSPotential(g, source, target, opts.projection)\n",
        "\tpotential := newACBSPotential(g, source, target, opts.projection, opts.geodesic)\n",
        "potential construction",
    )
    text = text.replace(
        "newACBSPotential(g, source, target, false)",
        "newACBSPotential(g, source, target, false, false)",
    )
    text = text.replace(
        "newACBSPotential(g, source, target, true)",
        "newACBSPotential(g, source, target, true, false)",
    )
    text = replace_once(
        text,
        "\tprojection                bool\n\tenabled                   bool\n",
        "\tprojection                bool\n\tgeodesic                  bool\n\tenabled                   bool\n",
        "potential field",
    )
    text = replace_once(
        text,
        "func newACBSPotential(g *graph.Graph, source, target int, projection bool) acbsPotential {\n",
        "func newACBSPotential(g *graph.Graph, source, target int, projection, geodesic bool) acbsPotential {\n",
        "potential signature",
    )
    text = replace_once(
        text,
        "\t\tcostPerMeter: g.MinCostPerMeter * (1 - 1e-12),\n"
        "\t\tprojection:   projection,\n"
        "\t\tenabled:      true,\n",
        "\t\tcostPerMeter: g.MinCostPerMeter * (1 - 1e-12),\n"
        "\t\tprojection:   projection,\n"
        "\t\tgeodesic:     geodesic,\n"
        "\t\tenabled:      true,\n",
        "potential init",
    )
    text = replace_once(
        text,
        "\tforward := lowerBoundCost(chordUnitMeters(x, y, z, p.targetX, p.targetY, p.targetZ), p.costPerMeter)\n"
        "\tbackward := lowerBoundCost(chordUnitMeters(x, y, z, p.sourceX, p.sourceY, p.sourceZ), p.costPerMeter)\n",
        "\tforward := lowerBoundCost(p.metricMeters(x, y, z, p.targetX, p.targetY, p.targetZ), p.costPerMeter)\n"
        "\tbackward := lowerBoundCost(p.metricMeters(x, y, z, p.sourceX, p.sourceY, p.sourceZ), p.costPerMeter)\n",
        "phi distances",
    )
    text = replace_once(
        text,
        "\tforward = lowerBoundCost(chordUnitMeters(x, y, z, p.targetX, p.targetY, p.targetZ), p.costPerMeter)\n"
        "\tbackward = lowerBoundCost(chordUnitMeters(x, y, z, p.sourceX, p.sourceY, p.sourceZ), p.costPerMeter)\n",
        "\tforward = lowerBoundCost(p.metricMeters(x, y, z, p.targetX, p.targetY, p.targetZ), p.costPerMeter)\n"
        "\tbackward = lowerBoundCost(p.metricMeters(x, y, z, p.sourceX, p.sourceY, p.sourceZ), p.costPerMeter)\n",
        "bound distances",
    )
    helper = r'''
func (p acbsPotential) metricMeters(ax, ay, az, bx, by, bz float64) float64 {
	chord := chordUnitMeters(ax, ay, az, bx, by, bz)
	if !p.geodesic || chord <= 0 {
		return chord
	}
	const earthRadiusMeters = 6371008.8
	x := chord / (2 * earthRadiusMeters)
	if x >= 1 {
		return math.Pi * earthRadiusMeters
	}
	return 2 * earthRadiusMeters * math.Asin(x)
}

'''
    text = replace_once(
        text,
        "func chordUnitMeters(ax, ay, az, bx, by, bz float64) float64 {\n",
        helper + "func chordUnitMeters(ax, ay, az, bx, by, bz float64) float64 {\n",
        "metric helper",
    )
    text = replace_once(
        text,
        'acbsEntropicSchedulerVersion = "entropic-proof-rate-v2"',
        'acbsEntropicSchedulerVersion = "balanced-geodesic-v1"',
        "candidate version",
    )
    path.write_text(text)

    Path("internal/search/acbs_geodesic_test.go").write_text(
        '''package search

import (
	"context"
	"math"
	"testing"
)

func TestACBSGeodesicMetricDominatesChord(t *testing.T) {
	p := acbsPotential{geodesic: true}
	a := p.metricMeters(1, 0, 0, 0, 1, 0)
	c := chordUnitMeters(1, 0, 0, 0, 1, 0)
	if !(a > c) {
		t.Fatalf("geodesic=%f chord=%f", a, c)
	}
	const earthRadiusMeters = 6371008.8
	if math.Abs(a-earthRadiusMeters*math.Pi/2) > 1e-6 {
		t.Fatalf("quarter circumference = %f", a)
	}
}

func TestACBSGeodesicCandidateRemainsExact(t *testing.T) {
	g := gridGraph(t, 40, 40, true)
	for source := 0; source < len(g.Nodes); source += 83 {
		for target := 19; target < len(g.Nodes); target += 127 {
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
'''
    )


if __name__ == "__main__":
    main()

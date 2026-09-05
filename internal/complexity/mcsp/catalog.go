package mcsp

import (
	"context"
	"errors"
	"sort"
)

var ErrCatalogTooLarge = errors.New("mcsp: full catalog is limited to at most 4 inputs")

type CatalogEntry struct {
	TruthTable uint64   `json:"truthTable"`
	MinGates   int      `json:"minGates"`
	LowerBound int      `json:"lowerBound"`
	Circuit    *Circuit `json:"circuit,omitempty"`
}

type CatalogResult struct {
	Inputs       int            `json:"inputs"`
	Entries      []CatalogEntry `json:"entries"`
	Distribution map[int]int    `json:"distribution"`
	Found        int            `json:"found"`
	Total        uint64         `json:"total"`
	Complete     bool           `json:"complete"`
	Stats        Stats          `json:"stats"`
}

func Catalog(ctx context.Context, inputs int, cfg Config, includeCircuits bool) (CatalogResult, error) {
	if inputs < 1 || inputs > 4 {
		return CatalogResult{}, ErrCatalogTooLarge
	}
	mask, err := Mask(inputs)
	if err != nil {
		return CatalogResult{}, err
	}
	cfg = normalizedConfig(cfg)
	total := uint64(1) << uint(1<<inputs)

	initialSignals := make([]uint64, inputs)
	entries := make(map[uint64]CatalogEntry, int(total))
	for i := range initialSignals {
		initialSignals[i], _ = VariableTruthTable(inputs, i)
		if _, ok := entries[initialSignals[i]]; !ok {
			entry := CatalogEntry{TruthTable: initialSignals[i], MinGates: 0}
			bound, _ := StructuralLowerBound(inputs, initialSignals[i])
			entry.LowerBound = bound.Value
			if includeCircuits {
				entry.Circuit = &Circuit{Inputs: inputs, Output: i}
			}
			entries[initialSignals[i]] = entry
		}
	}

	frontier := []state{{signals: initialSignals}}
	seen := map[string]struct{}{canonicalKey(initialSignals): {}}
	stats := Stats{PeakFrontier: 1}
	complete := uint64(len(entries)) == total

	for depth := 0; !complete && depth < cfg.MaxGates; depth++ {
		expansions, err := expandFrontier(ctx, frontier, mask, cfg.Workers)
		if err != nil {
			return CatalogResult{}, err
		}
		stats.Expanded += uint64(len(frontier))
		next := make([]state, 0)
		for _, ex := range expansions {
			stats.Deduped += ex.deduped
			for _, cand := range ex.candidates {
				stats.Generated++
				if _, ok := entries[cand.value]; !ok {
					entry := CatalogEntry{TruthTable: cand.value, MinGates: depth + 1}
					bound, _ := StructuralLowerBound(inputs, cand.value)
					entry.LowerBound = bound.Value
					if includeCircuits {
						c := Circuit{Inputs: inputs, Gates: cand.gates, Output: inputs + len(cand.gates) - 1}
						entry.Circuit = &c
					}
					entries[cand.value] = entry
					if uint64(len(entries)) == total {
						complete = true
					}
				}
				key := canonicalKey(cand.signals)
				if _, ok := seen[key]; ok {
					stats.Deduped++
					continue
				}
				if len(seen) >= cfg.MaxStates {
					stats.Depth = depth + 1
					stats.SeenStates = len(seen)
					return makeCatalogResult(inputs, entries, total, false, stats), ErrStateLimit
				}
				seen[key] = struct{}{}
				next = append(next, state{signals: cand.signals, gates: cand.gates})
			}
		}
		frontier = next
		stats.Depth = depth + 1
		if len(frontier) > stats.PeakFrontier {
			stats.PeakFrontier = len(frontier)
		}
		if len(frontier) == 0 {
			break
		}
	}
	stats.SeenStates = len(seen)
	return makeCatalogResult(inputs, entries, total, complete, stats), nil
}

func makeCatalogResult(inputs int, entries map[uint64]CatalogEntry, total uint64, complete bool, stats Stats) CatalogResult {
	ordered := make([]CatalogEntry, 0, len(entries))
	distribution := make(map[int]int)
	for _, entry := range entries {
		ordered = append(ordered, entry)
		distribution[entry.MinGates]++
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].TruthTable < ordered[j].TruthTable })
	return CatalogResult{
		Inputs:       inputs,
		Entries:      ordered,
		Distribution: distribution,
		Found:        len(ordered),
		Total:        total,
		Complete:     complete,
		Stats:        stats,
	}
}

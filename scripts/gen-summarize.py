#!/usr/bin/env python3
"""Collect generated graph sizes and benchmark medians into graphs.json."""

import json
import os
import sys


def main(out):
    graphs = []
    queries = None
    for name in ("10k", "100k", "1m"):
        path = os.path.join(out, name + ".bench.json")
        if not os.path.exists(path):
            print("missing", path, file=sys.stderr)
            return 1
        with open(path) as f:
            bench = json.load(f)
        queries = bench["config"]["queries"]
        graphs.append({
            "name": name,
            "nodes": bench["nodes"],
            "edges": bench["edges"],
            "metric": bench["metric"],
            "heuristicStrength": bench["heuristicStrength"],
            "diameterMeters": bench.get("diameterMeters"),
            "allCorrect": bench["allCorrect"],
            "algorithms": {
                s["algorithm"]: {
                    "medianMs": round(s["medianNs"] / 1e6, 3),
                    "p95Ms": round(s["p95Ns"] / 1e6, 3),
                    "medianExpanded": s["medianExpanded"],
                    "correct": s["correct"],
                    "runs": s["runs"],
                }
                for s in bench["summary"]
            },
        })
    summary = {"generator": "scripts/gen", "queries": queries, "graphs": graphs}
    dest = os.path.join(out, "graphs.json")
    with open(dest, "w") as f:
        json.dump(summary, f, indent=2)
        f.write("\n")
    print("wrote", dest)
    for g in graphs:
        dj = g["algorithms"].get("dijkstra", {})
        print("  %-5s nodes=%-8d edges=%-8d dijkstra-median=%.3fms correct=%s"
              % (g["name"], g["nodes"], g["edges"], dj.get("medianMs", 0), g["allCorrect"]))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1]))

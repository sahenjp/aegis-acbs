# Reproducing the road-network measurements

The numbers in `PAPER.md` and `docs/CANDIDATES.md` come from runs on this
machine. The raw JSON reports are not committed, because the graphs they refer
to are several hundred megabytes and cannot live in the repository. This file
records exactly what to rebuild and in what order, so the results can be
regenerated rather than taken on trust.

## What is needed

- OpenStreetMap extracts from Geofabrik:
  - `https://download.geofabrik.de/asia/japan/kanto-latest.osm.pbf` (about 500 MB)
  - `https://download.geofabrik.de/asia/japan/kansai-latest.osm.pbf` (about 350 MB)
- pyosmium, for the bounding-box extraction (`pip install osmium` provides it in
  a venv; it is not a repository dependency)
- Go 1.23 or newer

## Rebuilding a city graph

Both cities use a box around the city centre. The extraction keeps only ways
whose `highway` tag carries vehicle traffic and whose nodes all fall inside the
box, and drops OSM relations. Dropping relations means turn restrictions are not
modelled; see the limitations section of `PAPER.md`.

| City | Centre | Radius used |
|---|---|---|
| Tokyo | 35.6812, 139.7671 | 0.18 degrees |
| Osaka | 34.6937, 135.5023 | 0.16 degrees |

Extract with pyosmium in two passes, ways first and then nodes, because a single
pass with `locations=True` holds the whole file in memory and does not finish.
Write OSM XML, then import it:

```bash
bin/aegis import-osm --input tokyo.osm  --output tokyo-distance.aegis --profile car --metric distance
bin/aegis import-osm --input tokyo.osm  --output tokyo-time.aegis     --profile car --metric time
bin/aegis import-osm --input osaka.osm  --output osaka-distance.aegis --profile car --metric distance
bin/aegis import-osm --input osaka.osm  --output osaka-time.aegis     --profile car --metric time
```

Check the import with `bin/aegis inspect --graph <file>`. The Tokyo graph should
report 1,098,173 nodes and 2,262,568 directed edges with
`heuristicStrength` 0.9999 for distance and 0.2505 for time. Osaka should report
748,903 nodes and 1,575,615 edges with 1.0000 and 0.2786. If those differ, the
upstream extract has moved on and the comparison is no longer exact.

## Running the comparison

```bash
bin/aegis benchmark \
  --graph tokyo-distance.aegis \
  --algorithms dijkstra,bidijkstra,astar,aegis \
  --queries 400 --repeats 5 --order interleaved --suite mixed --seed 1010 \
  --output tokyo-distance.json --html ""
```

Use 300 queries and seed 1010 for the travel-time runs. The seed matters: query
pairs are deterministic given the seed, so the paired win and loss counts in
`PAPER.md` are only directly comparable at the seed that produced them.

The synthetic lattice sweep in section 5.3 comes from `scripts/gen` with the
topology held fixed and edge costs re-sampled. `scripts/gen-large-graphs.sh`
drives that pipeline.

## Verifying exactness rather than trusting the flag

`--algorithms` runs report a `correct` count, but that flag compares against the
harness's own Dijkstra. For an independent check, compare returned distances per
query between algorithms on the same JSON:

```python
import json
d = json.load(open("tokyo-distance.json"))
by = {}
for s in d["samples"]:
    by.setdefault(s["stats"]["algorithm"], {})[(s["sourceId"], s["targetId"])] = s["stats"]["distance"]
for other in ("bidijkstra", "astar", "aegis"):
    common = set(by["dijkstra"]) & set(by[other])
    bad = [k for k in common if by[other][k] != by["dijkstra"][k]]
    print(other, len(common) - len(bad), "/", len(common))
```

The paired wins and losses quoted in the paper are computed the same way, from
`stats.durationNs` on the shared `(sourceId, targetId)` keys.

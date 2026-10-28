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

## London and New York (OpenStreetMap)

The two non-Japanese cities in [BENCHMARK_CITIES.md](BENCHMARK_CITIES.md) use the
same box method as Tokyo and Osaka, but take whole country extracts from Geofabrik
and cut the city out locally. `scripts/extract-osm-bbox.py` does the cut with
pyosmium; it is the two-pass version (ways first, then the nodes they reference)
that [What is needed](#what-is-needed) already asks for.

| City | Extract | Size | Centre | Radius |
|---|---|---:|---|---:|
| London | `https://download.geofabrik.de/europe/great-britain-latest.osm.pbf` | 2.19 GB | 51.5074, -0.1278 | 0.18 |
| New York | `https://download.geofabrik.de/north-america/us/new-york-latest.osm.pbf` | 0.50 GB | 40.7128, -74.0060 | 0.18 |

```bash
mkdir -p /tmp/osm-pbf /tmp/osm-cities

# Download (the GB extract took 486 s, NY took 131 s, on the machine in
# BENCHMARK_CITIES.md; -C - resumes an interrupted transfer)
curl -L -C - -o /tmp/osm-pbf/great-britain.osm.pbf \
  https://download.geofabrik.de/europe/great-britain-latest.osm.pbf
curl -L -C - -o /tmp/osm-pbf/new-york.osm.pbf \
  https://download.geofabrik.de/north-america/us/new-york-latest.osm.pbf

# Cut the bounding box out (692 s / 4.4 GB RSS for London, 169 s / 1.3 GB for NY)
python3 scripts/extract-osm-bbox.py \
  /tmp/osm-pbf/great-britain.osm.pbf /tmp/osm-cities/london.osm 51.5074 -0.1278 0.18
python3 scripts/extract-osm-bbox.py \
  /tmp/osm-pbf/new-york.osm.pbf /tmp/osm-cities/new-york.osm 40.7128 -74.0060 0.18

# Import
bin/aegis import-osm --input /tmp/osm-cities/london.osm \
  --output /tmp/osm-cities/london-distance.aegis --profile car --metric distance --name london
bin/aegis import-osm --input /tmp/osm-cities/new-york.osm \
  --output /tmp/osm-cities/new-york-distance.aegis --profile car --metric distance --name new-york

# Compare (362 s for London, 180 s for New York)
bin/aegis benchmark --graph /tmp/osm-cities/london-distance.aegis \
  --algorithms dijkstra,bidijkstra,astar,aegis,aegis-static \
  --queries 300 --repeats 9 --order interleaved --suite mixed --seed 1010 \
  --output /tmp/osm-cities/london-distance.json --html ''
bin/aegis benchmark --graph /tmp/osm-cities/new-york-distance.aegis \
  --algorithms dijkstra,bidijkstra,astar,aegis,aegis-static \
  --queries 300 --repeats 9 --order interleaved --suite mixed --seed 1010 \
  --output /tmp/osm-cities/new-york-distance.json --html ''
```

Check the import with `bin/aegis inspect --graph <file>`. London should report
761,244 nodes and 1,449,256 directed edges with `heuristicStrength` 0.999919; New
York 353,434 nodes and 590,876 edges with 0.999934. If those differ, the upstream
extract has moved on.

**Memory.** The extractor holds an OSM location cache for the whole country file:
about 4.4 GB RSS for the 2.19 GB GB extract. Do not run two extractions, or an
extraction and a large benchmark, at the same time. That combination exhausted the
31 GB machine and all 7 GB of swap, and the kernel killed the Berlin extraction;
see section 5 of [BENCHMARK_CITIES.md](BENCHMARK_CITIES.md).

> [!NOTE]
> Germany (4.87 GB) and France (5.11 GB) were downloaded but their box extraction
> did not finish: Berlin was killed under memory pressure and Paris was stopped
> after 22 minutes. No numbers exist for them. A city-sized regional extract, for
> example a single Bundesland, avoids the whole-country node scan and is the
> untested next step.

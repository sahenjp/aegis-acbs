#!/usr/bin/env python3
"""Extract a city bounding box from a Geofabrik PBF as OSM XML.

This is the two-pass extractor described in docs/REPRODUCING.md. Pass one keeps
ways whose `highway` tag carries vehicle traffic and whose nodes all fall inside
the box, and records the node ids those ways need. Pass two resolves just those
node ids to coordinates. A single pyosmium pass with locations=True would have
to hold the whole extract in memory and does not finish on a country file.

OSM relations are dropped on purpose, so turn restrictions are not modelled; the
limitation is recorded in PAPER.md.

Usage:
  python3 scripts/extract-osm-bbox.py INPUT.pbf OUTPUT.osm LAT LON RADIUS_DEG [--max-nodes N]
"""

import argparse
import sys

import osmium

# Highway values that are not draivable by a car.
NON_DRIVABLE = {
    'footway', 'cycleway', 'path', 'pedestrian', 'steps', 'track', 'bridleway',
    'corridor', 'construction', 'proposed', 'razed', 'abandoned', 'platform',
}


def in_box(lat, lon, centre_lat, centre_lon, radius):
    return (abs(lat - centre_lat) <= radius) and (abs(lon - centre_lon) <= radius)


def drivable(tags):
    hw = tags.get('highway')
    if not hw:
        return False
    if hw in NON_DRIVABLE:
        return False
    if tags.get('access') in ('no', 'private'):
        return False
    return True


class WayCollector:
    """Pass one: collect ways in the box and the node ids they reference."""

    def __init__(self, lat, lon, radius, max_nodes):
        self.lat = lat
        self.lon = lon
        self.radius = radius
        self.max_nodes = max_nodes
        self.ways = []
        self.needed = set()
        self.ways_skipped_outside = 0
        self.ways_skipped_tag = 0
        self.aborted = False

    def way(self, w):
        if self.aborted:
            return
        if not drivable(w.tags):
            self.ways_skipped_tag += 1
            return
        refs = []
        for n in w.nodes:
            try:
                loc = n.location
            except osmium.InvalidLocationError:
                self.ways_skipped_outside += 1
                return
            if not in_box(loc.lat, loc.lon, self.lat, self.lon, self.radius):
                self.ways_skipped_outside += 1
                return
            refs.append(n.ref)
        if len(refs) < 2:
            return
        self.ways.append((w.id, refs, dict(w.tags)))
        self.needed.update(refs)
        if self.max_nodes and len(self.needed) > self.max_nodes:
            # The cap keeps a country-sized box from exhausting memory; the caller
            # shrinks the radius instead of silently truncating the network.
            self.aborted = True


class NodeCollector(osmium.SimpleHandler):
    """Pass two: keep coordinates for node ids the selected ways reference."""

    def __init__(self, needed):
        super().__init__()
        self.needed = needed
        self.coords = {}

    def node(self, n):
        if n.id in self.needed:
            self.coords[n.id] = (n.location.lat, n.location.lon)


def escape(value):
    return (value.replace('&', '&amp;').replace('"', '&quot;')
            .replace('<', '&lt;').replace('>', '&gt;'))


def write_xml(path, ways, coords, meta):
    with open(path, "w", encoding="utf-8") as f:
        f.write('<?xml version="1.0" encoding="UTF-8"?>\n')
        f.write('<!-- %s -->\n')
        f.write('<osm version="0.6" generator="extract-osm-bbox">\n')
        for node_id, (lat, lon) in coords.items():
            f.write('  <node id="%d" lat="%.7f" lon="%.7f"/>\n' % (node_id, lat, lon))
        for way_id, refs, tags in ways:
            f.write('  <way id="%d">\n' % way_id)
            for ref in refs:
                f.write('    <nd ref="%d"/>\n' % ref)
            for k, v in sorted(tags.items()):
                f.write('    <tag k="%s" v="%s"/>\n' % (escape(k), escape(v)))
            f.write("  </way>\n")
        f.write("</osm>\n")


def main(argv):
    ap = argparse.ArgumentParser()
    ap.add_argument("input")
    ap.add_argument("output")
    ap.add_argument("lat", type=float)
    ap.add_argument("lon", type=float)
    ap.add_argument("radius", type=float)
    ap.add_argument('--max-nodes', type=int, default=0)
    args = ap.parse_args(argv)

    collector = WayCollector(args.lat, args.lon, args.radius, args.max_nodes)
    # Pass one caches node locations while iterating ways. The location cache is
    # fed from node entities, so nodes must be part of the processed entity set.
    processor = osmium.FileProcessor(
        args.input, osmium.osm.NODE | osmium.osm.WAY
    ).with_locations(storage="flex_mem")
    for obj in processor:
        if obj.is_way():
            collector.way(obj)
            if collector.aborted:
                break
    if collector.aborted:
        print("aborted: more than %d nodes; use a smaller radius" % args.max_nodes,
              file=sys.stderr)
        return 3

    # Pass two resolves only the node ids the selected ways reference.
    nodes = NodeCollector(collector.needed)
    for obj in osmium.FileProcessor(args.input, osmium.osm.NODE):
        nodes.node(obj)

    for way_id, refs, _ in collector.ways:
        for ref in refs:
            if ref not in nodes.coords:
                print("missing node %d for way %d" % (ref, way_id), file=sys.stderr)
                return 4

    meta = "box lat=%.4f lon=%.4f radius=%.3f" % (args.lat, args.lon, args.radius)
    write_xml(args.output, collector.ways, nodes.coords, meta)
    print("ways=%d nodes=%d skipped_tag=%d skipped_outside=%d -> %s" % (
        len(collector.ways), len(nodes.coords), collector.ways_skipped_tag,
        collector.ways_skipped_outside, args.output))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))

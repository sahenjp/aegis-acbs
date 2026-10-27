# Zenodo deposit

This file records what to put in the Zenodo record for a release, so the metadata
lives in the repository rather than in someone's scratch directory.

Zenodo's GitHub integration does **not** read `.zenodo.json`. That file only
applies to manual or API uploads. For the GitHub-integration route, paste the
title and description below into the Zenodo record editor.

## Steps

1. In Zenodo, open Settings, then GitHub, and enable the switch for
   `sahenjp/aegis-acbs`. The repository must be public and owned by the
   connected account.
2. The integration archives **GitHub releases**, not tags and not drafts. A
   release created as a draft, or a tag with no release, will not be picked up.
   `v0.2.1` is published, not a draft, so it should archive on the switch.
3. After the deposit appears, open the record and check the files, the title, the
   creators and the license against this file.
4. Zenodo issues two identifiers. The **concept DOI** covers all versions and is
   what a paper should cite. The **version DOI** pins one release. Both appear on
   the record page.
5. Paste the concept DOI into the Citation section of both READMEs, replacing the
   sentence that says a DOI will be added once the archive is registered.

## Title

```
Aegis Coupled-Bound Search (ACBS)
```

## Creators

```
sahen
```

The pen name is the author credit. `sahenjp` is the GitHub account the repository
lives under; the two are deliberately different.

## Description

```
Aegis Coupled-Bound Search (ACBS) is an exact point-to-point shortest path
implementation for weighted directed road graphs. A forward and a reverse
frontier advance under a single admissible potential, and the search stops when
the sum of the two frontier minima reaches the best complete path found. That
stopping rule is the known g-bound; docs/PAPER.md derives the equivalence and
identifies it as bound B3 in the enumeration of Siag et al. (IJCAI 2023), so no
novelty is claimed for it. The component this release actually contributes is a
scheduler that decides which frontier receives the next chunk of edge work,
based on how fast each direction is raising the coupled lower bound.

The repository includes measurements on real OpenStreetMap road networks for
Tokyo (1,098,173 nodes) and Osaka (748,903 nodes), under both a distance metric
and a travel-time metric. Every query in those runs returned an exact shortest
path. On distance, the implementation finished ahead of bidirectional Dijkstra on
390 of 400 Tokyo queries and 288 of 300 Osaka queries, by 8.64x and 6.59x on the
median. On travel time the gains were 3.46x and 3.39x.

Those numbers are not a general superiority claim. The advantage depends on how
much information the geographic lower bound carries: it shrinks as the bound
weakens, and on a synthetic uniform lattice it disappears and then reverses. The
package also documents three further exact search variants and a multi-bound
experiment that did not beat bidirectional Dijkstra, including the defects found
in each, because a result that only reports successes is not reusable.

Scope: two cities, one machine, travel-time costs from a speed model rather than
observed traffic, and turn restrictions not modelled.
```

## Fields

| Field | Value |
|---|---|
| Resource type | Software |
| Version | `0.2.1` |
| License | MIT |
| Publication date | the date of the deposit |
| Related identifier | `https://github.com/sahenjp/aegis-acbs` (is supplement to / is version of) |
| Related identifier | `https://github.com/sahenjp/aegis-acbs/releases/tag/v0.2.1` (is identical to) |
| Related identifier | commit `9499f2981b2eaf8bca789e92b77f54fd4bd7c20d` (is version of) |

## Keywords

```
shortest path
bidirectional search
exact search
road networks
A* search
OpenStreetMap
```

## What the record should contain

Zenodo archives the release tarball by default. The `v0.2.1` release also carries
29 assets, including source archives, four platform binaries, a CycloneDX SBOM,
SHA-256 checksums, and the raw Tokyo evidence bundle. The SBOM and the checksums
are the ones that make the deposit verifiable; if only one file is kept, keep the
release tarball plus `SHA256SUMS`.

## Things that go wrong

- Enabling the switch before the repository rename settled would register the
  old name. The rename is finished, so this no longer applies, but check that the
  record shows `sahenjp/aegis-acbs`.
- Draft releases are ignored. Confirm the release page shows no "Draft" badge.
- A large upload can sit in `processing` for a while. That is not a failure.
- Editing the description after publication creates a new version. Do the proof
  reading before publishing.
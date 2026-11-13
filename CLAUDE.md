# mapgen

`mapgen` (`github.com/maloquacious/mapgen`) generates Earth-like world maps for games.
It is a Go library under `internal/`, with one command per generation stage under `cmd/`.

How to work here (build, checks, versioning, commits) is in [WORKFLOW.md](WORKFLOW.md).
Prefer building reusable tools to ad-hoc shell pipelines; see [TOOLING.md](TOOLING.md).

@WORKFLOW.md
@TOOLING.md

## What we are building

- Earth-like worlds with 30–35 percent land.
- An internal generation resolution of 6 km per cell; player-facing output at 10–25 km per cell, derived later.
- A cylindrical playable map: it wraps east to west, and nothing crosses the northern or southern edge.
  Every stage before climate wraps in both directions (toroidal), so the map can be recentered at any of those stages without breaks.
  The northern and southern edges become fixed at the climate stage; no recentering is available from then on.
- Convincing continents and mountain systems, coherent drainage, climate, biomes, geology, and mineral resources.
- Staged generation: each stage writes a self-contained artifact directory, so a user can keep accepted results through one stage and try different seeds for a later one.

## Specifications

These documents in the repository root define the design. Read the relevant one before changing a stage.

- [World Map Layers and Generation Stages.md](<World Map Layers and Generation Stages.md>): vocabulary for map categories (topography, landforms, climate, hydrology, biomes, surface cover, geology and ores) and the staged workflow.
- [Cylindrical World Map Generator Specification for Go Coding Agents.md](<Cylindrical World Map Generator Specification for Go Coding Agents.md>): the overall generator; coordinates, wrapping rules, state model, determinism, and the MUST requirements.
- [Fracture Continent Generator Implementation Specification.md](<Fracture Continent Generator Implementation Specification.md>): the continent source, adapted from mdhender/hamwold at a pinned commit.
- [Structure Generation Design.md](<Structure Generation Design.md>): geological structure before terrain and final coastline selection, feature records, checkpoints, previews, and integration changes.

`docs/` holds reference material: research papers (`docs/research/`, PDFs are git-ignored) and the USGS land use classification.

Numerical defaults in the specifications are calibration values, not contracts.
Contracts and invariants (wrapping, determinism, land fraction, artifact formats) must be preserved.

## Pipeline so far

1. `cmd/continents`: builds continental affinity fields with the rectangular fracture generator (`fracture-rect-v1`), optionally recentering the largest landmass.
   Writes `manifest.json`, raw float32 rasters (`affinity`, `continental`, `coast`), and `preview.png`.
2. `cmd/landmask`: selects exactly `round(fraction * cells)` land cells by affinity from a `continents` artifact and writes a self-contained land/ocean checkpoint.
   See [cmd/landmask/README.md](cmd/landmask/README.md).

3. `cmd/structure`: attaches geological features (belts, rifts, arcs, faults, plateaus, basins) to a `continents` artifact and writes a self-contained structure checkpoint.
   It partitions the map into geological provinces. So far it takes features only from an explicit fixture file; automatic placement, rock classification, and recentering are not implemented.
   See [cmd/structure/README.md](cmd/structure/README.md).

Later stages (terrain, the terrain-based landmask, drainage, climate, biomes, resources) are not implemented yet.

The planned generation order is `continents → structure → terrain → landmask → drainage → climate → ecosystems`, with resources depending on geology. The current affinity-based landmask command remains useful for inspection; final coastline acceptance will require a terrain-input mode. Do not assume its present continental-input artifact freezes coastlines before geological structure in the default pipeline.

## Layout

- `version.go`: module version (package `mapgen`).
- `internal/fracture`: the disc-sum fracture field generator and recentering.
- `internal/continent`: storing and loading the continental artifact.
- `internal/landmask`: creating, storing, and loading the land/ocean artifact.
- `internal/structure`: feature records and their validation, toroidal geometry queries, province partitioning, and the structure artifact.
- `internal/artifact`: helpers shared by packages that publish artifact directories.
- `internal/prng`: reproducible random streams derived from a seed and a domain name.
- `internal/cerrs`: constant sentinel errors.
- `internal/dotenv`: environment file loading.

## Conventions

- Every Go file starts with the copyright line and `SPDX-License-Identifier: AGPL-3.0-or-later`.
  Preserve upstream attribution when adapting hamwold code.
- Rasters are flat slices, row-major (`index = row*cols + col`), row 0 at the north, float32 little-endian on disk with no header.
- Horizontal indices wrap with a positive modulo. Rows also wrap in every stage before climate; they do not wrap from climate onward or in the playable map.
  Distances and filters must follow the wrapping convention in the cylindrical specification.
- Randomness comes only from `internal/prng` streams, keyed by a stable domain name, never from global or time-based sources.
- Artifacts never record input paths, so identical inputs give identical outputs wherever they are stored.

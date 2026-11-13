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
  Intermediate stages may wrap in both directions (toroidal) as long as the final world respects the playable boundaries.
- Convincing continents and mountain systems, coherent drainage, climate, biomes, geology, and mineral resources.
- Staged generation: each stage writes a self-contained artifact directory, so a user can keep accepted results through one stage and try different seeds for a later one.

## Specifications

These documents in the repository root define the design. Read the relevant one before changing a stage.

- [World Map Layers and Generation Stages.md](<World Map Layers and Generation Stages.md>): vocabulary for map categories (topography, landforms, climate, hydrology, biomes, surface cover, geology and ores) and the staged workflow.
- [Cylindrical World Map Generator Specification for Go Coding Agents.md](<Cylindrical World Map Generator Specification for Go Coding Agents.md>): the overall generator; coordinates, wrapping rules, state model, determinism, and the MUST requirements.
- [Fracture Continent Generator Implementation Specification.md](<Fracture Continent Generator Implementation Specification.md>): the continent source, adapted from mdhender/hamwold at a pinned commit.

`docs/` holds reference material: research papers (`docs/research/`, PDFs are git-ignored) and the USGS land use classification.

Numerical defaults in the specifications are calibration values, not contracts.
Contracts and invariants (wrapping, determinism, land fraction, artifact formats) must be preserved.

## Pipeline so far

1. `cmd/continents`: builds continental affinity fields with the rectangular fracture generator (`fracture-rect-v1`), optionally recentering the largest landmass.
   Writes `manifest.json`, raw float32 rasters (`affinity`, `continental`, `coast`), and `preview.png`.
2. `cmd/landmask`: selects exactly `round(fraction * cells)` land cells by affinity from a `continents` artifact and writes a self-contained land/ocean checkpoint.
   See [cmd/landmask/README.md](cmd/landmask/README.md).

Later stages (elevation, mountains, drainage, climate, biomes, geology) are not implemented yet.

## Layout

- `version.go`: module version (package `mapgen`).
- `internal/fracture`: the disc-sum fracture field generator and recentering.
- `internal/continent`: storing and loading the continental artifact.
- `internal/landmask`: creating, storing, and loading the land/ocean artifact.
- `internal/prng`: reproducible random streams derived from a seed and a domain name.
- `internal/cerrs`: constant sentinel errors.
- `internal/dotenv`: environment file loading.

## Conventions

- Every Go file starts with the copyright line and `SPDX-License-Identifier: AGPL-3.0-or-later`.
  Preserve upstream attribution when adapting hamwold code.
- Rasters are flat slices, row-major (`index = row*cols + col`), row 0 at the north, float32 little-endian on disk with no header.
- Horizontal indices wrap with a positive modulo; rows do not wrap in the playable map.
  Distances and filters must follow the wrapping convention in the cylindrical specification.
- Randomness comes only from `internal/prng` streams, keyed by a stable domain name, never from global or time-based sources.
- Artifacts never record input paths, so identical inputs give identical outputs wherever they are stored.

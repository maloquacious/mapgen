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
- [Trench Generation Design.md](<Trench Generation Design.md>): subduction polarity of active margins and island arcs, and ocean trench records, between structure and terrain.
- [Terrain Generation Design.md](<Terrain Generation Design.md>): elevation from accepted structure; the continental foundation, feature synthesis, subcell rasterization, residual detail, the terrain artifact, and the landmask terrain-input mode.

`docs/` holds reference material: research papers (`docs/research/`, PDFs are git-ignored) and the USGS land use classification.

Numerical defaults in the specifications are calibration values, not contracts.
Contracts and invariants (wrapping, determinism, land fraction, artifact formats) must be preserved.

## Pipeline so far

1. `cmd/continents`: builds continental affinity fields with the rectangular fracture generator (`fracture-rect-v1`), optionally recentering the largest landmass.
   Writes `manifest.json`, raw float32 rasters (`affinity`, `continental`, `coast`), and `preview.png`.
2. `cmd/landmask`: selects exactly `round(fraction * cells)` land cells by affinity from a `continents` artifact and writes a self-contained land/ocean checkpoint.
   See [cmd/landmask/README.md](cmd/landmask/README.md).

3. `cmd/structure`: attaches geological features (belts, rifts, arcs, faults, plateaus, basins) to a `continents` artifact and writes a self-contained structure checkpoint.
   It partitions the map into geological provinces and interprets the provisional coast (coast distance and coast chains on a 50 km working grid). Features come from an explicit fixture file or from automatic placement, which places collision belts with plateaus and foreland basins, active margins with inland basins, intermontane basins inside wide belts, old orogens (some interrupted), rifts, island arcs, interior basins, and faults.
   Rock classification converts features and provinces to geological proxies (pressure, shear, igneous influence) and classifies every cell as sedimentary, igneous, or metamorphic.
   `--recenter` translates the complete result to center the largest landmass.
   See [cmd/structure/README.md](cmd/structure/README.md).

4. `cmd/trench`: decides which side of each active margin and island arc subducts and places ocean trenches there, from a `structure` artifact, and writes a self-contained trench checkpoint that retains the structure records.
   Active margins subduct opposite their elevated flank; arcs toward the more open water. Trenches follow the parent offshore and are cut where land (ignoring islands under structure's province minimum) or other features are in the way.
   `--recenter` translates the complete result, structure records included, with the same shift structure's `--recenter` would choose.
   See [cmd/trench/README.md](cmd/trench/README.md).

5. `cmd/terrain`: builds ground and seabed elevation from a `trench` artifact and writes a self-contained terrain checkpoint that retains the structure and trench records.
   It builds the continental foundation, the regional surface, belt components, subcell sampling, residual detail, and recentering (steps 1 to 6 of the terrain design): an exact full-resolution torus coast distance, a coastal plain rising to smoothed province base levels, a shelf (narrow near active margins and arcs), slope, and abyssal plain; then every feature pulls that surface toward its drawn regional target through its envelope weight, uplifts combining by the largest plus a quarter of the next, depressions by the largest.
   Belts then carry ridge chains with gaps, branches, troughs, and corridors with saddles; arcs and active margins carry volcanic peaks; trenches deepen the seabed. These are published in `ridges.json`.
   Cells that features reach are sampled at 4 x 4 points for mean elevation, high, and relief, in parallel tiles whose result does not depend on the worker count.
   Residual noise (no wavelength under four cells, at most 10 percent of relief, grain along belts) is added unless `--residual=false`.
   `--recenter` translates the complete result, structure and trench records included, to center the land terrain implies.
   Elevations are relative to the provisional coast at 0 m; the landmask terrain-input mode comes next.
   See [cmd/terrain/README.md](cmd/terrain/README.md).

Later stages (terrain features, the terrain-based landmask, drainage, climate, biomes, resources) are not implemented yet.

The planned generation order is `continents → structure → trench → terrain → landmask → drainage → climate → ecosystems`, with resources depending on geology. The current affinity-based landmask command remains useful for inspection; final coastline acceptance will require a terrain-input mode. Do not assume its present continental-input artifact freezes coastlines before geological structure in the default pipeline.

## Layout

- `version.go`: module version (package `mapgen`).
- `internal/fracture`: the disc-sum fracture field generator and recentering.
- `internal/continent`: storing and loading the continental artifact.
- `internal/landmask`: creating, storing, and loading the land/ocean artifact.
- `internal/structure`: feature records and their validation, toroidal geometry queries, province partitioning, coast interpretation, automatic placement, geology proxies and rock classification, recentering, and the structure artifact.
- `internal/trench`: subduction polarity, trench placement and clipping, trench records and their validation, recentering, and the trench artifact.
- `internal/terrain`: the coast distance transform, the continental foundation, the regional surface, belt components and ridge records, trench depressions, tiled subcell rasterization, residual detail, recentering, and the terrain artifact.
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

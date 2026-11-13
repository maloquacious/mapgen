# Structure Generation Design

## Purpose and place in the pipeline

`structure` creates the geological arrangement that terrain generation turns into mountains, plateaus, basins, and seabed.
It runs before final coastline selection, so geological features can influence which areas ultimately emerge above sea level.

The generation order is:

```text
continents → structure → terrain → landmask → drainage → climate → ecosystems
                 └─────────────────────────────────────────────→ resources
```

* `continents` supplies continental affinity.
* `structure` interprets that field using a provisional coast and places geological regions and events.
* `terrain` generates elevations and relief.
* `landmask` selects and freezes the final ocean and land distribution from that terrain.

A user accepts each checkpoint independently and can vary the next stage without regenerating accepted inputs.

This is a design for a static procedural model, not a simulation of moving plates or geological history.
It refines the province and event approach in the cylindrical generator specification.
Numerical defaults remain calibration choices.

## What accepting structure means

The user accepts the locations, shapes, relationships, and broad character of geological features.
For example:

- A curved active margin follows the western side of a provisional continent.
- An inland collision belt has a broad plateau on its northern flank.
- An older interrupted belt crosses a continental interior.
- A rift has a depressed axis and raised shoulders.
- A sedimentary basin lies beside a mountain system.

Accepting structure does not accept exact coastlines, peak elevations, individual ridges, valleys, rivers, or biomes.
Those have not been generated yet.
Changing the terrain seed can change the detailed ridges within an accepted belt, but must preserve its backbone, width envelope, flank orientation, and associated regional features.

## Command interface

Proposed usage:

```sh
go run ./cmd/structure \
  --in tmp/continents \
  --seed 42 \
  --provisional-land-fraction 0.325 \
  --out tmp/structure-42
```

| Flag | Meaning |
| --- | --- |
| `--in` | Accepted continental artifact; default `tmp/continents` |
| `--out` | New output directory; default `tmp/structure` |
| `--seed` | Structure seed; if omitted, use the continental input's seed. The manifest records the seed and whether it was explicit. Stages share a seed safely because each draws from streams keyed by its own domain names |
| `--provisional-land-fraction` | Fraction used to interpret continental affinity while placing features; default 0.325 |
| `--config` | Deferred. A file that supplies values for these flags; not needed yet and not part of version one |
| `--features` | Optional explicit feature fixture for algorithm development and repeatable visual comparisons |
| `--recenter` | Optional translation of the complete result; off by default |
| `--png` | Generate diagnostic previews; default true |
| `--png-scale` | Sampling interval for raster preview backgrounds; default 1 |
| `--version` | Print generator version |

The provisional fraction is a placement assumption, not a commitment to final coastlines.
Final land fraction belongs to the terrain-based landmask stage.
The input is never modified and existing output directories are never replaced.

## Inputs and physical domain

Load and verify the continental artifact, including its dimensions, physical spacing, field checksums, and finite values.
Retain its affinity, continental, and coast fields.
Work internally at the inherited resolution, normally 6 km, with feature geometry expressed in kilometers.
Conversion to player-facing 10–25 km cells occurs later.

All structure calculations use toroidal topology, the same as the continental field: x wraps east to west and y wraps north to south.
Features may cross either seam.
The map can be recentered at any stage before climate, so every stage up to that point must wrap in both directions; otherwise a recentered map would show breaks along the old edges.
The north and south edges become real boundaries when climate is generated, and no recentering is available from that stage on.

Use the full-resolution highest-affinity cells to establish a provisional mask, with deterministic index tie handling.
This mask is diagnostic placement data.
Neither it nor zero affinity is an authoritative sea-level threshold.

At a working spacing near 50 km, derive the provisional land coverage, coastline distance, and contiguous continental regions.
Retain actual coarse spacing so the coarse grid tiles the physical extent.
Coarse cells summarize coverage rather than relying on a single sample that could erase a small island.
Label full-resolution components on the torus: land regions are eight-connected and ocean regions four-connected.
A fracture mask at full size has tens of thousands of single-cell islands and enclosed waters, so provinces are built on a simplified mask; see the province rules in the version one schema.

Distances are physical distances on the torus.
Approximate eight-neighbor graph distances are acceptable if their approximation is recorded.
Zero coast distance lies on land/water interfaces, not at an arbitrarily chosen coastal cell center.
Coast tracing must resolve diagonal ambiguities deterministically.

All-land and all-ocean inputs have no coast.
They must use an explicit interior-only or ocean-only recipe rather than inventing coast segments or requiring nonexistent margin features.

## Geological regions and events

A geological province is a contiguous region of related context.
It is not a tectonic plate.
Events are sparse features that may cross provinces and overlap one another.
Do not uplift every province boundary.

| Record | Purpose and examples |
| --- | --- |
| Province | Broad context such as a continental shield, sedimentary region, volcanic region, or oceanic region |
| Mountain belt | Backbone and envelope for a collision belt, active margin, or old orogen |
| Rift | Depressed axis and raised shoulders |
| Island arc | Offshore volcanic belt, allowed to produce future islands |
| Basin | Regional depression associated with a belt or continental interior |
| Plateau | Broad elevated region, including a chosen flank of a collision belt |
| Fault | Structural line and optional shear context; not automatically a mountain range |

Each feature has a stable ID, type, physical geometry, relevant province IDs, and its own deterministic random key.
Relationships are explicit: a plateau identifies its parent belt; an adjacent basin identifies its associated margin or belt.
A belt can have several associated features.

Belts store a locally unwrapped backbone, smoothly varying full width, side orientation, target regional elevation and relief ranges, and morphological maturity.
Maturity describes intended shape, not an age in years.
The record also states whether an elevated flank lies to the left or right when following the backbone.

Targets in meters are constraints for the terrain stage, not generated elevations.
Detailed ridge chains, branching, cross-belt corridors, and pass saddles belong to terrain.
A plateau must have its own area or envelope rather than being represented as an unusually wide ridge.

## Placement method

1. **Interpret the continental field.**
   Build provisional mask, component labels, coast chains, and coast-distance data.
   Preserve small islands and coastline seam continuity.
2. **Partition geological regions.**
   Use deterministic, spatially separated seeds and bounded multi-source region growth.
   Keep region ownership contiguous within each component.
   Large components can contain several provinces; isolated islands receive their own ownership without false connections across ocean.
3. **Choose event requests.**
   Scale counts by provisional continental area, with explicit overrides.
   Use the existing specification's area-based counts as starting values, not a requirement that every tiny map contain every event family.
4. **Place candidate backbones.** 
   Inland belts use inland anchors, optionally biased toward selected province interfaces.
   Margins follow eligible coast chains with an inland offset.
   Arcs use offshore corridors.
   Rifts require enough room for their axis and shoulders.
5. **Fit regional envelopes.**
   Assign width controls, flank orientation, plateau areas, and associated basins.
   Test the entire envelope, not just its centerline, against eligibility.
6. **Resolve conflicts.**
   Reject immediate self-intersections, tight bends relative to belt width, and incompatible overlaps.
   Permit deliberate relationships such as a basin beside a collision belt.
   Avoid a checkerboard of competing event labels.
7. **Publish features and diagnostics.**
   Save authoritative vector records and reproducible raster summaries.
   No terrain erosion or elevation solve is required here.

Each requested event gets at most 32 candidate attempts.
Unplaceable optional events are recorded as omissions with reasons; required fixture features produce an error.
Never keep searching until an attractive result happens to appear.
All stochastic choices use stage and feature-specific streams from `internal/prng`.

Initial full belt widths follow the existing specification: active margins 100–400 km, collisions 300–1200 km, old orogens 100–600 km, rifts 50–300 km, and arcs 30–150 km.
These are configurable starting ranges.
A small domain can omit a feature that cannot fit rather than shrinking it into an unrelated shape.

Use bounded smoothing and resampling in physical units.
A collision belt must not become a coastline-following ring merely because coastal anchors are easy to find.
An active margin follows selected eligible coast segments; the whole coastline must not become mountains.

Store seam-crossing geometry in continuous local coordinates on both axes.
For example, x values just below the world width and just above it describe a short crossing, not a segment across the entire map; the same holds for y and the world height.
Rasterization and spatial lookup consider the relevant periodic images.

## Geology and rock classification

Structure owns geological context.
Its feature records must provide enough information for the rock rules in the layer reference: compressional or extensional context, shear context, volcanic influence, and their spatial falloff.
A continental affinity value is none of these quantities.

The existing rock recipe refers to pressure and shear derived from plate boundaries.
This static event model has no moving plates.
Before implementing that classifier, define and version the conversion from events to dimensionless pressure, shear, and igneous-influence proxies, including their normalization and distance falloff.
Do not silently label a procedural proxy as measured stress or reuse unrelated noise as pressure.

Once that conversion is specified, rock classification can be a deterministic substep of `structure` with its own random stream.
It must cover ocean and continental cells, distinguish unclassified from classified absence, and preserve the layer reference's rock and ore eligibility rules.
Any change to that recipe must be explicit.
The percentages quoted in the reference are not acceptance targets established for this new model.

The first structural fixture implementation can publish geological regions and events before rock classification is ready.
Such an artifact must declare rock classification absent; resource generation must reject missing required rock inputs.
Ore placement remains a later stage and does not belong in structure.

## Saved artifact

Use a versioned format, initially `mapgen-structure-v1`, with these logical contents:

| Content | Purpose |
| --- | --- |
| `manifest.json` | Generator and algorithm versions, input content hashes, resolved configuration, structure seed, PRNG derivation, grid, field schemas, checksums, and coordinate transforms |
| Retained continental fields | Self-contained source for later terrain construction |
| `provisional-land.u8` | Exact provisional interpretation used for placement; never labeled final landmask |
| `provinces.json` and province ownership raster | Geological region records and their spatial ownership |
| `features.json` | Belts, rifts, arcs, basins, plateaus, faults, and their relationships |
| Optional geology rasters | Versioned proxy inputs and rock type when classification is implemented |
| `diagnostics.json` | Requested and placed counts, rejected/omitted features, eligibility measures, validation results, and performance measurements |
| Diagnostic PNGs | Placement, provinces, feature envelopes, and any generated geology fields |

List authoritative fields separately from derived diagnostics in the manifest.
JSON holds sparse physical geometry; dense rasters use explicitly typed, endian-defined storage.
Vector features are authoritative for terrain synthesis, rather than nearest-feature labels in a preview raster.

Do not record filesystem input paths.
Separate operational timing and memory diagnostics from canonical generation metadata so they do not affect the identity of reproducible world content.
Write the complete artifact through a temporary directory and publish without replacing an existing checkpoint.

## Version one schema

This section fixes what implementation step 1 needs: coordinates, record shapes, the fixture file, and the geometry queries terrain will call.
Field names and file names here are contracts once accepted; numeric values in examples are not.

### Coordinates

- Geometry is in kilometers as float64: `x` east from the western edge, `y` south from the northern edge.
  This matches the continental manifest, where cell `(col, row)` has its center at `((col+0.5)*cell_width_km, (row+0.5)*cell_height_km)`.
- The domain is a torus: `x` is periodic with period `width_km` and `y` with period `height_km`.
- Stored geometry is unwrapped on both axes.
  The first vertex of a polyline or ring has `0 <= x < width_km` and `0 <= y < height_km`; later vertices continue from it without wrapping and may fall outside those ranges.
  Every segment has `|dx| < width_km / 2` and `|dy| < height_km / 2`.
- "Left" and "right" are taken while travelling along a backbone in vertex order, viewed on the map with north up.
   With tangent `t` and offset `v` from the nearest backbone point, `t.x*v.y - t.y*v.x > 0` means right.
- Geometry never depends on raster resolution.
  Rasters are derived from it.

### Feature file

`features.json` and the `--features` fixture share one schema, `mapgen-structure-features-v1`, so a saved artifact's features can be fed back as a fixture.

```json
{
  "format": "mapgen-structure-features-v1",
  "domain": { "width_km": 39996, "height_km": 19998 },
  "features": [
    {
      "id": 1,
      "type": "mountain_belt",
      "kind": "collision",
      "name": "fixture collision belt",
      "key": "1",
      "maturity": 0.2,
      "elevated_flank": "left",
      "backbone": [
        { "x": 39200, "y": 8000, "width_km": 500 },
        { "x": 40100, "y": 8300, "width_km": 800 },
        { "x": 41000, "y": 8100, "width_km": 600 }
      ],
      "targets": { "regional_elevation_m": [1500, 3000], "relief_m": [2000, 5000] }
    },
    {
      "id": 2,
      "type": "plateau",
      "key": "2",
      "parent": 1,
      "side": "left",
      "outline": [
        { "x": 39300, "y": 6500 }, { "x": 40900, "y": 6600 },
        { "x": 40800, "y": 7800 }, { "x": 39400, "y": 7700 }
      ],
      "edge_km": 150,
      "targets": { "regional_elevation_m": [2500, 4500], "relief_m": [200, 800] }
    }
  ]
}
```

Fields common to every feature:

| Field | Type | Rule |
| --- | --- | --- |
| `id` | integer | 1 or greater, unique in the file; 0 means none wherever an ID is referenced |
| `type` | string | `mountain_belt`, `rift`, `island_arc`, `fault`, `plateau`, or `basin` |
| `name` | string | Optional label for previews and diagnostics; no effect on generation |
| `key` | string | Unsigned 64-bit decimal; the feature's random key, see below |
| `parent` | integer | Optional ID of the feature this one is associated with |
| `provinces` | integer list | Province IDs the feature touches; written on output, recomputed (not trusted) on input |
| `targets` | object | Optional `[min, max]` ranges in meters: `regional_elevation_m` and `relief_m`; constraints for terrain, with `min <= max` |

Linear features (`mountain_belt`, `rift`, `island_arc`, `fault`) carry a `backbone`: two or more samples `{x, y, width_km}`.
The polyline through the samples is the authoritative backbone; placement smooths and resamples before saving, and readers do not smooth again.
`width_km` is the full width at that sample, positive, and interpolated linearly by arc length between samples.

| Type | Additional fields |
| --- | --- |
| `mountain_belt` | `kind`: `collision`, `active_margin`, or `old_orogen`. `maturity` in `[0, 1]`. `elevated_flank`: `left`, `right`, or `none` |
| `rift` | `axis_fraction` in `(0, 1)`: share of the width occupied by the depressed axis; the rest is shoulders |
| `island_arc` | None |
| `fault` | `sense`: `strike_slip`, `normal`, or `thrust`. Width is the zone of structural influence. No `targets` |

Area features (`plateau`, `basin`) carry an `outline`: a simple ring of three or more `{x, y}` vertices, implicitly closed, either winding.
`edge_km` is the distance inside the outline over which the feature rises from absent to full; it is positive.

| Type | Additional fields |
| --- | --- |
| `plateau` | `parent`: required, a `mountain_belt`. `side`: `left` or `right` of the parent, equal to the parent's `elevated_flank` |
| `basin` | `kind`: `foreland`, `intermontane`, or `interior`. `parent` and `side` are required except for `interior` |

### Validation

Loading a feature file fails, naming the feature ID, when:

- the format string or domain does not match the input artifact's physical extent;
- an ID is zero or repeated, a `parent` does not exist, or a parent has the wrong type;
- a required field for the type is missing, or an unknown field is present;
- a number is not finite, a width or `edge_km` is not positive, or a range has `min > max`;
- a first vertex is not in `[0, width_km)` by `[0, height_km)`, or a segment spans half the world or more on either axis;
- an outline is not simple, is as wide or as tall as the world, or any geometry has a zero-length segment;
- a basin's parent is not a `mountain_belt`, `rift`, or `island_arc`.

Self-intersection, tight-bend, and overlap rules belong to automatic placement.
A fixture that breaks them loads with a recorded warning, so awkward cases can be drawn deliberately.
Until automatic placement defines the bend and overlap rules, the only warning is a backbone that crosses itself.

### Fixture mode

`--features FILE` replaces event selection and placement (steps 3 to 6 of the placement method) with the file's features.
Every feature in the file is required: an invalid one is an error, never an omission.

A continental artifact is still required through `--in`.
The fixture's `domain` must equal that artifact's physical extent, so the artifact carries the retained fields and provisional coast that previews and terrain need.
Small fixed-seed continental artifacts serve as the "fixed physical domains" of the cylindrical specification.
A fixture is therefore drawn for one particular continental artifact; each fixture records, beside the file, the `continents` seed and grid that produce it.
There is no fixture mode without a continental input.

`--seed` remains valid in fixture mode and seeds province growth.
Features are copied to `features.json` unchanged apart from recomputed `provinces`.

### Random keys and streams

All streams use `prng.New(seed, domain, sequence)` with the structure seed and these domains:

| Domain | Sequence | Use |
| --- | --- | --- |
| `structure-v1/provinces/seeds` | Landmass number | Province seed positions within one landmass |
| `structure-v1/provinces/cost` | 0 | The smooth field in the growth cost |
| `structure-v1/provinces/types` | 0 | Volcanic draws, one per land province in ID order |
| `structure-v1/requests` | 0 | Event counts and request attributes |
| `structure-v1/place/<type>` | Request index within the type | All candidate attempts for one requested event |

An automatically placed feature's `key` is the first value drawn from its placement stream.
A fixture feature's `key` is whatever the file states.
Terrain derives a feature's detail stream from the terrain seed, a terrain domain, and `key` as the sequence, so changing the terrain seed changes detail in every belt while an unchanged key keeps belts independent of one another.

### Provinces

`provinces.json` uses `mapgen-structure-provinces-v1`:

```json
{
  "format": "mapgen-structure-provinces-v1",
  "provinces": [
    { "id": 1, "type": "shield", "component": 1, "seed": { "x": 12030, "y": 7410 }, "cells": 184220, "area_km2": 6631920 }
  ]
}
```

- `type` is `shield`, `sedimentary`, `volcanic`, or `oceanic`.
   Belt-related context comes from features, not from province types.
- `component` is the landmass of the simplified mask the province belongs to, numbered from 1 in order of each landmass's lowest row-major cell index; 0 for oceanic provinces.
- `provinces.u32` is the ownership raster: little-endian uint32, full resolution, row-major.
   Every cell has a nonzero ID.
   Each connected ocean region of the simplified mask is one `oceanic` province.

This differs from the cylindrical specification in two accepted ways: ocean cells are owned, where that specification partitions only continental area, and collision, old orogen, active margin, and rift are features only, not province types.

Provinces follow these rules, named `provinces-v1`. Their numeric values are calibration defaults recorded in the manifest.

1. **Simplify the mask.**
   Turn every enclosed water smaller than 20,000 square km to land, then every landmass smaller than 20,000 square km to ocean.
   The provisional mask itself is saved unchanged; only province ownership uses the simplified one.
   So a small island belongs to the ocean province around it and a small lake to the land province around it, and neither is lost from the mask.
2. **Seed.**
   A landmass gets one seed per 1.5 million square km, at least one.
   Seeds are chosen on a grid of about 50 km spacing that tiles the domain: the first at random, each later one farthest from those already chosen, with a random factor between 0.75 and 1.25 on the distance.
3. **Grow.**
   Each land cell goes to the seed that reaches it most cheaply through eight-connected land, at full resolution, so every province is connected and none crosses water.
   Leaving a cell costs the step length times a factor from 1 to 1.6, taken from a smooth field.
   The smooth field is a fracture field built on the 50 km grid, scaled to the range 0 to 1.
   Equal costs go to the lower province ID.
4. **Type.**
   Rank a landmass's provinces by the mean of the `continental` field over their cells.
   The upper half are `shield` and the rest `sedimentary`.
   A landmass that is a single province is `shield` when its mean is at least the mean over all land.
   Each land province then has a 4 percent chance of becoming `volcanic`; step 3 can replace this draw with proximity to arcs and rifts.
   Continental affinity is a layout value, so this is a plausible proxy, not a derivation.

Province IDs run from 1: landmasses in order, each landmass's provinces in seed order, then ocean regions in order.

### Coast interpretation

The coast is interpreted on a working grid: the grid nearest 50 km per cell that tiles the world.
A working cell is land when at least half of the simplified provisional mask inside it is land.
The simplified mask is the one provinces use, so islands and enclosed waters under the province minimum area are not coastlines.

Coast distance is the signed distance in kilometers from a working cell center to the nearest land/water interface of the working mask: positive on land, negative on water.
It is exact on the torus, not a graph approximation; the interface is the cell edges between land and water, so the distance is half a cell at a coastal cell center.
Between cell centers it is interpolated.
A world that is all land or all ocean has no coast: the distance is infinite and there are no chains.

A coast chain is one closed coastline of the working mask.
Every coastline on a torus is closed.
Walking a chain's points in order keeps land on the right, by the same rule belts use for sides.
Where two land cells touch only at a corner the chain keeps them joined, matching the eight-connected land and four-connected water of the region labels.
A chain's points are the midpoints of its cell edges, smoothed along the chain with a standard deviation of 75 km, and unwrapped on both axes.
A chain records its length, the areas of the working-grid land region and water region it separates, and how many times it circles the world before closing.
A band of land right round the world has two chains that close only by wrapping.

Chains are numbered in order of each chain's first edge in row-major order.
The working mask, coast distance, and chains are placement data: they are rebuilt from the provisional mask and are not saved as rasters.
`diagnostics.json` lists the working grid and every chain's summary, and `preview-coast.png` shows the distance in 100 km bands with the smoothed chains.
The manifest records the parameters as `coast_parameters` with algorithm `coast-v1`.

Which chains are eligible for active margins and island arcs is a placement decision, made in step 3.

### Geometry queries

These are the handoff to terrain.
They live in the structure package, take points in kilometers, and consider the nine periodic images of a feature: offsets of `-width_km`, `0`, and `+width_km` in x combined with `-height_km`, `0`, and `+height_km` in y.

| Query | Result |
| --- | --- |
| `Nearest(p)` on a linear feature | Arc-length position `s` in km along the backbone, unsigned distance `d` in km, and side (`left`, `right`, or `on`) |
| `WidthAt(s)` | Full width in km at arc-length position `s`, clamped to the backbone's ends |
| `Cross(p)` | Normalized cross-belt position `u = d / (WidthAt(s) / 2)`; the envelope is `u <= 1` |
| `Length()` | Backbone length in km |
| `Inside(p)` on an area feature | Signed distance in km to the outline, positive inside |
| `Mask(p)` | `clamp(Inside(p) / edge_km, 0, 1)` |
| `Bounds()` | Unwrapped bounding box in km, expanded by the half width, for rasterization |

Rules:

- Beyond a backbone's end, `Nearest` returns the end's `s` and the distance to the end point, giving round caps.
   Side is taken from the end segment's tangent.
- Equal distances resolve to the lowest segment index, then to the lowest y image offset, then to the lowest x image offset.
- Queries return identical results for `p` and `p` shifted by any whole number of world widths and world heights.

Step 1 is complete when tests show, on fixed features:

1. Analytic cases: a straight belt of constant width gives the expected `s`, `d`, side, and `u` at chosen points, including beyond both ends; a rectangular plateau gives the expected signed distance and mask.
2. Seam invariance: a feature stored across a seam and the same feature shifted by `width_km`, by `height_km`, or by both answer every query identically.
   A query near `x = 0` finds a belt stored near `x = width_km`, and a query near `y = 0` finds one stored near `y = height_km`.
3. Translation invariance: shifting a feature and the query point by the same whole number of cells, on either axis, leaves `s`, `d`, side, and `u` unchanged.
   This is the property recentering relies on.
4. Round trip: writing `features.json` and loading it as a fixture reproduces the artifact byte for byte.
5. Resolution independence: rasterizing an envelope at two cell sizes disagrees only in cells the envelope boundary crosses.

## Recentering

Recentering is a convenience available at any stage before climate, not a condition for accepting structure.
It preserves relationships and records the applied coordinate transform.
The mask, retained fields, province labels, feature geometry, and previews must all agree after translation.

Because structure is toroidal, a translation by whole cells on either axis is exact: it cuts nothing and changes no adjacency.
`--recenter` centers the largest provisional land component, using the same toroidal, eight-neighbor convention as `continents` and `landmask`.
Rasters shift cyclically.
Feature and province-seed geometry shifts by the same distance in kilometers, then each polyline or ring is moved by whole world widths and heights so its first vertex is back in the base domain.
IDs, keys, and component numbers assigned before the shift are kept; they are not renumbered from the new cell order.
Shifts are relative to the input checkpoint, as in `landmask`.

The stage that fixes the north and south edges, climate, is responsible for what lies across them.
Structure makes no promise that a feature avoids the eventual polar edges; the user avoids that by recentering before climate.

## Previews and acceptance

The primary preview shows the provisional coastline in a neutral color, geological regions, belt backbones and widths, plateau flanks, and basin areas.
Distinguish provisional coastlines visibly in the legend.
A second preview isolates structural envelopes without noise or decorative relief shading.
A third can display rock types when available.

The user's review asks: are ranges coherent and selectively placed; do they have plausible room for foothills, plateaus, and basins; do continental interiors retain broad quiet areas; and do seam-crossing features remain continuous?
Actual mountain surface quality is judged after terrain generation.

Correctness checks cover deterministic output, valid IDs and references, complete province ownership, finite parameters, positive widths, bounded feature attempts, normalized geometry, continuity across both seams, invariance under recentering, and input preservation.
Seam tests compare periodic geometric queries rather than requiring the first and last raster columns to be equal.

Required visual fixtures are a margin with an inland basin, a collision belt with a plateau, an interrupted old belt, a rift with shoulders, and an offshore arc.
Include a belt crossing the east/west seam, a belt crossing the north/south seam, and a small island fixture.
Evaluate structural placement before adding terrain noise.

## Implementation order and integration changes

1. Define feature records, physical geometry queries, artifact storage, and an explicit fixture input.
   Prove the handoff to terrain on fixed features before tuning automatic placement.
2. Add provisional coast interpretation and province ownership.
   Component labels and provinces are done.
3. Add bounded, seeded automatic event placement and diagnostic previews.
   It is built in three parts: coast distance and coast chains (done); inland features, which are collision belts and old orogens with their plateaus and basins, then rifts; and coastal features, which are active margins, island arcs, and faults.
4. Specify geological proxy conversion and implement rock classification as its own deterministic substep.
5. Add optional complete-result recentering after the coordinate and seam contracts are verified.

The existing `cmd/landmask` reads continental affinity.
Keep that behavior explicitly identified as affinity-based layout selection; it remains useful for inspection.
The final pipeline needs a terrain-input mode with physical sea-level selection and the ocean-connectivity checks from the cylindrical specification.
Its manifest must identify the input kind and selection algorithm.
It must not interpret an affinity threshold as meters or accept an affinity-based mask as the final terrain-derived ocean without an explicit workflow choice.

Adopting this order changes the earlier suggestion that accepted coastlines precede mountain generation.
Structure and terrain alternatives can change the eventual coastlines until `landmask` is accepted.
After that point, drainage and later stages must preserve the frozen ocean membership.
To hold coastlines fixed while experimenting with terrain would require a separate constrained-terrain workflow; that is not the default pipeline designed here.

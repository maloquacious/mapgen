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
The conversion from events to dimensionless pressure, shear, and igneous-influence proxies, with their normalization and distance falloff, is defined and versioned under "Geology proxies and rock classification" in the version one schema.
Do not silently label a procedural proxy as measured stress or reuse unrelated noise as pressure.

With that conversion, rock classification can be a deterministic substep of `structure` with its own random stream.
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
| `structure-v1/requests` | Family number | Request attributes for one family: eight values per request, whatever the family uses |
| `structure-v1/place/<family>` | Request index within the family | All candidate attempts for one requested event |
| `structure-v1/rocks/noise` | Octave number | The fracture field for one octave of rock noise |

Families are `collision` (0), `active_margin` (1), `old_orogen` (2), `rift` (3), `island_arc` (4), and `fault` (5).
The numbers are fixed, so adding a family does not change what the others draw.

An automatically placed feature's `key` is the first value drawn from its placement stream.
A plateau or basin placed with a belt draws its key from the belt's stream after the belt is placed.
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

Which coasts are eligible for active margins and island arcs is a placement decision; see "The major coast" under automatic placement.

### Automatic placement

Without `--features`, features are placed automatically and the manifest's `mode` is `automatic`.
The manifest records the parameters as `placement_parameters` with algorithm `placement-v1`.
The resulting `features.json` is a valid fixture and reproduces the same features.

**Requests.**
Each family requests one event per so much provisional land, in million square km: collision belts one per 35 and at least one, active margins one per 30 and at least one, old orogens one per 20 and at least one, rifts one per 45, island arcs one per 50, and faults one per 25 and at least one.
The fault count is this design's own starting value; the cylindrical specification gives none.
Families are placed in the order collision, active margin, old orogen, rift, island arc, fault, so the larger and more constrained events choose first.

| Family | Length, km | Width at the middle, km | Other |
| --- | --- | --- | --- |
| Collision | 1,500–5,000 | 300–1,200 | Maturity 0–0.3; elevated flank drawn left or right |
| Active margin | 1,500–6,000 | 100–400 | Maturity 0–0.4; elevated flank inland |
| Old orogen | 1,000–3,500 | 100–600 | Maturity 0.6–1; no elevated flank |
| Rift | 800–3,000 | 50–300 | Axis fraction 0.2–0.4 |
| Island arc | 600–2,500 | 30–150 | |
| Fault | 300–2,000 | 20–100 | Sense: half strike-slip, a quarter normal, a quarter thrust |

**Inland backbone.**
Collision belts, old orogens, rifts, and faults are grown this way.
A candidate starts at a random working cell that is free and far enough inland for the family's narrowest width, with a random heading.
Its width at the middle is drawn between the family's minimum and the smaller of the family's maximum and twice the coast distance at the anchor.
It grows from both ends in 50 km steps to the requested length, or until both ends are blocked.
The heading drifts smoothly and bends no tighter than a radius of one belt width, and never tighter than 300 km.
Neither half may turn more than 60 degrees from the heading it started with, so a belt curves but does not hook back.
When the land three steps ahead is too narrow for the full width, the backbone turns toward the land farther from the coast.
An end is blocked where the coast is nearer than half the end width, where another feature lies, or where the belt would come within its own width of itself.
A candidate shorter than the family's minimum length is rejected; one that was blocked before its requested length is kept at the length it reached.

The backbone is then smoothed, resampled every 100 km, and given widths: widest in the middle, 55 percent of that at the ends, and narrower wherever the coast is closer than half the width, but never below the end width.
So a belt's envelope can overhang the provisional coast near its ends and where the land narrows.
A candidate is rejected if its backbone crosses itself or if its envelope, widened by a 100 km gap, touches any feature already placed.

Each request has at most 32 candidates.
A request with none accepted is recorded in the diagnostics as an omission with a count of each reason for rejection.

**Faults.**
A fault is grown like an inland backbone, with three differences.
It may start in any working cell at least 50 km inland and may cross other features, since it is a structural line and not a range.
Neither half turns more than 20 degrees from its starting heading, so it is nearly straight.
It claims no room, so nothing avoids it; faults are placed last.

**The major coast.**
Active margins and island arcs are placed against the major coast, not the whole provisional coast.
Starting from the working mask, every water region under 2 million square km becomes land, then every landmass under 1 million square km becomes water.
What remains is mainland facing ocean.
The major coast distance is the signed distance to that coast, measured as coast distance is: positive on the mainland side.
So an enclosed sea has no active margin and no arc, a small island has no active margin, and offshore islands do not disturb the line a margin or arc follows.

**Coastal backbone.**
An active margin draws its width, then an offset from the major coast between 50 and 200 km and never less than half its width.
An island arc draws its width and an offset between 200 and 600 km offshore.
A candidate starts in a random free working cell whose major coast distance is within half a cell of the offset, heading along the contour of that distance in one direction or the other.
At each 50 km step it takes, from five turns no tighter than the bend limit, the one that best keeps the major coast distance 100 km ahead at the offset.
An end is blocked where that distance strays more than 100 km from the offset, where another feature lies, or where the backbone would come within 300 km or its own width of itself.
A margin must also stay on provisional land with room for its end width; an arc need not avoid small islands.
Neither half of a margin may turn more than 90 degrees from its starting heading; an arc is held to the inland limit of 60.
Length, smoothing, widths, and the checks on the finished candidate are as for an inland backbone, except that an arc's width is not limited by the coast.

A margin's elevated flank is its inland one: the side farther from the major coast.
Half the margins, by a request attribute, get an inland basin on that side, built like a foreland basin; one with no room is omitted and recorded.

**Plateau and foreland basin.**
A collision belt gets a plateau on its elevated flank and a foreland basin on the other.
Each is an outline beside the middle of the belt: its inner edge runs inside the belt so the two blend, and its outer edge reaches up to 0.5–1 belt widths (plateau) or 0.4–0.8 (basin) beyond the belt's edge, deepest in the middle.
The outer edge is pulled in where it would come within 50 km of the coast (plateau), cross the coast (basin), or reach another feature.
If the plateau has no room on the drawn flank the other flank is tried, and the belt's `elevated_flank` follows the plateau.
A plateau or basin that reaches less than 50 km on average, is not a simple outline, or comes within the gap of an unrelated feature is omitted and recorded.

**Targets.**
Targets are fixed by family from the cylindrical specification's ranges: collision belts 1,500–3,000 m regional elevation and 2,500–5,000 m relief; plateaus 2,500–4,500 m and 200–800 m; foreland basins 0–400 m and 0–200 m; rifts 300–1,500 m and 500–2,000 m; old orogens 300–1,200 m with relief from 500 m up to 2,500 m for the least mature and 1,000 m for the most.
Active margins have 1,000–2,500 m and 2,000–4,000 m, and their inland basins the foreland values.
Island arcs have −1,500 to 500 m regional elevation, so they cross sea level, and 1,000–3,000 m relief; these two ranges are this design's own starting values.
Faults carry no targets.

### Geology proxies and rock classification

Rock classification turns features and provinces into three dimensionless proxies, then applies the layer reference's rock rule to every cell, land and ocean.
It runs in both modes, after features are final and provinces are owned.
The manifest records the parameters as `rock_parameters`, with proxy conversion `geology-proxy-v1` and classifier `rocks-v1`, and sets `rock_classification` to `rocks-v1`.

The proxies are procedural stand-ins for geological context.
They are not measured or simulated stress, and their names say only which term of the rock rule they feed.

| Proxy | Range | Meaning |
| --- | --- | --- |
| `pressure` | −2 to 2 | Compressional context, positive, or extensional context, negative |
| `shear` | 0 to 1 | Strike-slip and oblique context |
| `igneous` | 0 to 1 | Volcanic and intrusive influence |

**Profiles.**
A linear feature contributes at a point by its cross-belt position `u = Cross(p)` from the geometry queries.
Its strength is `1 − smoothstep(core, reach, u)`, where `smoothstep(a, b, x)` is 0 below `a`, 1 above `b`, and `t²(3 − 2t)` between, with `t = (x − a)/(b − a)`.
So a feature is at full strength through the middle of its envelope and fades to nothing at `reach` half widths from its backbone.
Stress (pressure and shear) and igneous influence have separate profiles, so igneous influence can reach farther from a convergent margin than its stress does.
An area feature contributes its `Mask(p)`.

| Feature | Pressure | Shear | Igneous | Stress core, reach | Igneous core, reach |
| --- | --- | --- | --- | --- | --- |
| Collision belt | +2.0 | 0.3 | 0 | 0.6, 1.5 | |
| Active margin | +1.6 | 0.4 | 1.0 | 0.6, 1.5 | 1.0, 3.0 |
| Old orogen | +1.2 | 0.2 | 0 | 0.5, 1.2 | |
| Rift | −1.2 | 0.2 | 0.7 | 0.5, 1.5 | 0.5, 2.0 |
| Island arc | +0.8 | 0.2 | 1.0 | 0.6, 1.5 | 1.0, 2.0 |
| Strike-slip fault | 0 | 1.0 | 0 | 0.5, 2.0 | |
| Thrust fault | +1.0 | 0.3 | 0 | 0.5, 2.0 | |
| Normal fault | −0.8 | 0.3 | 0 | 0.5, 2.0 | |
| Plateau | +0.8 | 0 | 0 | outline mask | |
| Basin | 0 | 0 | 0 | | |

A contribution is the table value times the strength.
Pressure reaches 2 because the rule's noise term, `2·rv − 1`, spans −1 to 1: with pressure limited to 1, a collision core's stress term would be 0.65 and the noise would hide most belts.
At 2, a collision core is metamorphic throughout, an active margin's core in about two cells of three, and the share falls off across each flank.
The rock rule reacts to igneous influence only above 0.25, so an igneous value is effectively a switch and its profile sets how far the igneous zone reaches.
Collision belts, old orogens, and plateaus therefore have none: a small value would do nothing, and one above 0.25 would make every non-metamorphic cell of a belt's core igneous.
Provinces add igneous influence without falloff: 0.6 over every `volcanic` province, and 0.3 over `oceanic` provinces for basaltic crust.
The oceanic value is withheld from a continental shelf: it is scaled by `smoothstep(150, 300, −coast distance)`, using the coast interpretation's distance in km, so it is absent within 150 km of the provisional coast and full beyond 300 km.
A world with no coast has no shelf.

**Combining.**
Features overlap, so contributions are combined per cell without summing whole envelopes:
pressure is the greatest positive contribution plus the most negative one, clamped to [−2, 2], so compression and extension that overlap offset each other;
shear and igneous influence are the greatest contribution of any feature or province.
A cell no feature reaches has zero pressure and shear, and the igneous influence of its province.

**Rock noise.**
`rv` is four octaves of fracture-field noise, independent of every proxy.
Octave `k`, from 0 to 3, has wavelength `400 / 2^k` km and weight `2^−k`.
Each octave is a fracture field on the grid nearest a quarter of its wavelength that tiles the world, at least 2 by 2 cells, with one disc per two cells, radii between a quarter and a half of the wavelength, and falloff 2, drawn from stream `structure-v1/rocks/noise` with the octave number as the sequence.
A cell's raw value is the weighted sum of the octaves sampled bilinearly on the torus at its center.
The raw values are converted to their mid-rank over all cells, `n = (below + equal/2) / cells`, which is uniform on [0, 1] whatever the grid, and `rv = n²`.

**Classification.**
The layer reference's rule is applied unchanged, in order:

1. Metamorphic when `(|pressure| + shear)/2 + (2·rv − 1) >= limit`, where `limit` is 0.09 when `shear > |pressure|`, a transform-dominated cell, and 0.10 otherwise.
2. Igneous when `rv > 0.4` or `igneous > 0.25`.
3. Sedimentary otherwise.

With no feature nearby, about 26 percent of land is metamorphic, 11 percent igneous, and 63 percent sedimentary; the reference's shares came from plate boundaries everywhere and are not targets here.
Ocean beyond the shelf is igneous wherever it is not metamorphic.

**Storage.**
`rock.u8` holds one byte per cell: 0 unclassified, 1 sedimentary, 2 igneous, 3 metamorphic.
A classified artifact has no 0; 0 exists so that later stages can tell a missing classification from a classified cell.
`pressure.f32`, `shear.f32`, `igneous.f32`, and `rock-noise.f32` (the `rv` values) are saved as float32 fields under `geology_fields` in the manifest.
Resources need `rv` again for ore selection, so it is saved rather than rebuilt.
`diagnostics.json` counts each rock on provisional land and on provisional ocean, and `preview-rock.png` shows the rock types with the provisional coast.

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
   Done, in three parts: coast distance and coast chains; inland features, which are collision belts with their plateaus and basins, old orogens, and rifts; and coastal features, which are active margins, island arcs, and faults.
   Not yet placed: interrupted old belts, and intermontane and interior basins.
4. Specify geological proxy conversion and implement rock classification as its own deterministic substep.
   Done; see "Geology proxies and rock classification" in the version one schema.
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

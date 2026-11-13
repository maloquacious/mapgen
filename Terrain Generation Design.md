# Terrain Generation Design

## Purpose and place in the pipeline

`terrain` turns an accepted structure checkpoint into ground and seabed elevation: a continental foundation, mountain belts with ridges and passes, plateaus, basins, rifts, island arcs, and the seabed.
It runs before final coastline selection.
The coast it implies can differ from the provisional coast that structure used, and `landmask` freezes ocean membership from this terrain afterwards.

```text
continents → structure → terrain → landmask → drainage → climate → ecosystems
                 └─────────────────────────────────────────────→ resources
```

* `structure` supplies the provisional mask, provinces, authoritative feature records with elevation and relief targets, and geology.
* `terrain` generates mean elevation, representative high elevation, and relief at full resolution, with sparse ridge and pass records.
* `landmask`, in a new terrain-input mode, selects sea level and the connected ocean from that elevation.

This refines section 5 (the foundation profiles) and section 6 (mountain belt synthesis) of the cylindrical generator specification.
It is a static procedural model: features are rasterized directly from their records, with no uplift or erosion simulation.
Numerical defaults are calibration choices.
The contracts are the inputs, outputs, wrapping, determinism, and the relationship between feature records and the elevations they produce.

## What accepting terrain means

The user accepts the shape of the land: where mountains, plateaus, basins, and lowlands are, how high they reach, how rugged they are, and the seabed.
For example:

- A collision belt shows several ridge chains, a plateau on its elevated flank, a foreland basin on the other, and passes through it.
- An old orogen is a lower, broken range with broad valleys between its chains.
- A rift has a trough between raised shoulders, deep enough in places to hold a lake or let the sea in.
- An island arc breaks the surface as a chain of separate islands.
- A continental interior away from any feature is broad and quiet.

Accepting terrain does not accept the exact coastline, rivers, lakes, or valley incision.
The coastline is selected by `landmask` against this terrain, and drainage may cut valleys within the bounds of section 8 of the cylindrical specification.

Changing the terrain seed changes ridge chains, branches, pass positions, target draws within each feature's ranges, and residual detail.
It must not move a feature's backbone or outline, widen or narrow its envelope, change its elevated flank, or remove an associated plateau or basin; those belong to the accepted structure.

## Command interface

Proposed usage:

```sh
go run ./cmd/terrain \
  --in tmp/structure-42 \
  --seed 42 \
  --out tmp/terrain-42
```

| Flag | Meaning |
| --- | --- |
| `--in` | Accepted structure artifact; default `tmp/structure` |
| `--out` | New output directory; default `tmp/terrain`. An existing one is never replaced |
| `--seed` | Terrain seed; if omitted, the structure seed. The manifest records the seed and whether it was explicit |
| `--residual` | Add residual detail noise; default true. `--residual=false` publishes the structured surface alone, which must remain recognizable |
| `--recenter` | Translate the complete result; off by default |
| `--png` | Generate previews; default true |
| `--png-scale` | Sampling interval for previews; default 1 |
| `--workers` | Number of worker goroutines; default the number of CPUs. Never affects the output |
| `--version` | Print the generator version |

`--config` is deferred, as it is for structure.
There is no fixture flag: structure's `--features` already supplies fixed features, and terrain reads them from the structure artifact.

## Inputs and physical domain

Load and verify the structure artifact with `structure.Load`, without the original continental directory.
Terrain requires the provisional mask, provinces, and features.
It does not require rock classification, and it does not read the geology proxies; geology describes rock and stress, and terrain must not derive elevation from it.
Terrain does read `provinces.json` types, which the foundation uses to choose lowland and upland base levels.

Work at the inherited resolution, normally 6 km.
Feature geometry is in kilometers, and every query uses the structure package's geometry queries (`Nearest`, `WidthAt`, `Cross`, `Inside`, `Mask`, `Bounds`), which already consider the nine periodic images.
The domain is a torus: x and y both wrap, as in every stage before climate.

### Elevation datum

Elevations are meters relative to a provisional datum: 0 m is the provisional coast.
The foundation places the provisional land/water interface exactly at 0 m, so with no features the provisional mask is exactly the cells above 0 m.
Features then raise or lower terrain on both sides of that line.

This datum is not sea level.
`landmask` chooses sea level as the elevation quantile that leaves the requested fraction of cells above it, then shifts that level to 0 m.
Terrain reports how far that shift would be at the provisional land fraction, and how many cells would change sides, so the user can see how much terrain moved the coast before running `landmask`.

## The continental foundation

The foundation is the surface with no features: lowlands, interior uplands, shelves, slopes, and abyssal plains.
It follows section 5 of the cylindrical specification, built from the provisional mask at full resolution.

**Coast distance.**
Compute the exact signed distance from every cell center to the nearest land/water interface of the unsimplified provisional mask, on the torus: positive on land, negative on water, half a cell at a coastal cell center.
This is a full-resolution transform (for example, the Felzenszwalb–Huttenlocher squared-distance transform run on both periodic axes), not structure's 50 km working-grid distance.
The working grid would round the fracture coastline away, and the unsimplified mask keeps small islands and small enclosed waters that the province rules dropped.
The interface nearest a cell center is the nearest point of the nearest cell of the other kind, and the squared distance to a cell's square separates by axis, so the transform runs as Felzenszwalb–Huttenlocher's two passes; the squared distance to a square is convex but not a parabola, so the row pass finds each envelope crossing by binary search.

**Land profile.**
Land rises from 0 m at the coast through a coastal plain to an interior base level:

```text
land(d) = B * smoothstep(0, Rin, d)
```

`d` is the coast distance in km, `Rin` the rise distance, default 250 km, and `B` the interior base level at that cell.
`B` comes from the province: `shield` 300–600 m, `sedimentary` 100–300 m, `volcanic` 400–800 m, each province's value drawn once from its own stream.
Per-province values are placed on the working grid, smoothed with a 150 km Gaussian (standard deviation, cut off at three) on the torus so province boundaries do not show as steps, and interpolated bilinearly to cells.
Only land provinces contribute: the smoothed level is the Gaussian-weighted mean of nearby land provinces' levels, so oceanic provinces do not pull coastal interiors down.
Land beyond the kernel's reach of any land province, a small island in an oceanic province, uses an island base level, default 200 m.
A small smooth fracture field adds ±15 percent of `B`, with wavelengths no shorter than 400 km, so interiors are not uniformly flat.

**Water profile.**
Water falls from 0 m through a shelf, a slope, and an abyssal plain:

```text
shelf:  0 to −S           over 0 < −d <= W
slope:  −S to −A          over W < −d <= W + Rs
plain:  −A                beyond
```

`S` is the shelf-break depth, 50–200 m; `W` the shelf width, 50–200 km; `Rs` the slope width, default 150 km; and `A` the abyssal depth, 3,500–5,500 m.
Each comes from its own smooth field: a fracture field on a grid of half the minimum wavelength (200 km), with disc radii from 200 to 400 km, normalized to [0,1] by its extremes and sampled bilinearly on the torus, like the interior variation field.
Each segment joins the next with a smoothstep, so the profile has no kinks.
Within the stress reach of an active margin or island arc, the shelf width is reduced toward 50 km: margins facing a trench have narrow shelves.
This depends on features, so it lands with step 2 rather than step 1.
Water enclosed by land gets the same profile; a small enclosed water is a shallow depression, which `landmask` will not join to the ocean, and which drainage may keep as a lake.

**No coast.**
An all-land world uses the land profile with `d` infinite, and an all-ocean world the water profile at the abyssal plain.

## Feature synthesis

Each feature contributes to the surface through its envelope, and the regional surface, the ridges, and the residual detail are combined in a fixed order.
All features are rasterized from their vector records, never from a preview or nearest-feature raster.

### Targets

A feature's `targets` are `[min, max]` ranges in meters.
Terrain draws actual values inside them, from the feature's terrain stream, as control samples along the backbone every 150–400 km, interpolated smoothly along `s`.
An area feature draws one value per target.

- `regional_elevation_m` is the level relative to the provisional datum that the regional surface reaches in the feature's core.
  It is absolute, not added to the foundation: a plateau target of 3,000 m means a plateau at 3,000 m, and an island arc target of −500 m means a ridge whose crest lies 500 m under the datum, whatever the seabed beneath.
- `relief_m` is the high-minus-valley amplitude terrain builds on top of that level: ridge heights above the regional surface for belts, the depth of a rift axis below its shoulders, rim roughness for a plateau.

Faults have no targets and get terrain's own small values, below.

### Regional surface

A linear feature's weight at a point is the compact kernel of section 6 of the cylindrical specification:

```text
K(t) = (1 − t²)² for |t| < 1, otherwise 0
w(p) = K(Cross(p))
```

An area feature's weight is a smoothstep of its `Mask(p)`.
Each feature pulls the foundation `F` toward its regional target `T`:

```text
uplift     u = w * max(0, T − F)
depression v = w * max(0, F − T)
```

Features that raise ground (belts, plateaus, rift shoulders, arcs) contribute uplift; basins and rift axes contribute depression.
Uplifts combine by the largest plus a share of the second largest, capped at 25 percent of it, as section 6 requires; depressions apply to the result by the largest alone:

```text
U = F + u1 + c * u2          (u1 >= u2 the two largest uplifts, c = 0.25)
R = U − max over depressions of  w * max(0, U − T)
```

So a foreland basin whose inner edge runs inside its belt pulls the belt's flank down to the basin level instead of stacking the two, and two crossing belts do not double in height.

### Belt components

A mountain belt is built from section 6's components, scaled by its kind and maturity.

1. **Regional uplift** from the belt's envelope, above.
2. **Ridge chains:** two to six polylines that follow the backbone at different lateral offsets, each with its own perturbation, amplitude profile, and gaps.
   A chain contributes `amplitude * K(distance / halfwidth) * along(s)`, where `along` fades the chain in and out over 50–150 km at its ends and gaps.
   Chains combine with each other by maximum, then add to the regional surface.
3. **Branches:** shorter chains leaving selected chain sections at reduced amplitude.
4. **Longitudinal basins:** troughs between selected adjacent chains, as depressions with their own small targets.
5. **Corridors and passes:** one corridor per 300–700 km of backbone, at least 150 km apart, crossing the belt.
   Along a corridor every chain is lowered to a saddle, so the corridor is a continuous low route across the belt.
   Saddle floors stay above the regional surface on both sides, so a corridor is a pass, not a drained valley; drainage decides later whether a river takes it.
6. **Residual detail** aligned with the local structure, below.

Ridge chains, branches, and corridors are sparse vector records published in `ridges.json`, keyed by their parent feature, so drainage and pass extraction can use them.

| Kind | Chains | Character |
| --- | --- | --- |
| Collision | 3–6 | Tall, nearly continuous chains; the plateau rises on the elevated flank; the foreland basin lies on the other |
| Active margin | 2–4 | Asymmetric: the highest chain on the coast side, an inland basin on the elevated flank where structure placed one, and an optional chain of separate volcanic peaks |
| Old orogen | 2–4 | Low chains with frequent gaps, broad valleys, and subdued residual detail |

Maturity controls shape, not a single height factor.
As maturity rises, chain amplitude falls toward the bottom of the relief range, gaps grow more frequent and longer, valley widths grow, branches thin out, and residual amplitude falls.

### Other features

- **Plateau:** a flat surface at its regional target inside its outline, rising over `edge_km`.
  Its relief target sets low-amplitude rolling detail and a rougher, dissected rim along the edge band.
  It blends with its parent belt's flank through the uplift rule.
- **Basin:** a depression to its regional target, which is low (0–400 m on land), with a smooth floor.
  A basin under the datum floods at `landmask` only if it connects to the ocean; otherwise it is a closed depression for drainage to resolve.
- **Rift:** the shoulders, outside `axis_fraction` of the width, are uplift to the regional target; the axis is a depression to the shoulder level minus the relief draw.
  The axis floor may fall under the datum, so a rift can become an inlet, a lake, or an arm of the sea.
- **Island arc:** a submarine ridge to its regional target, plus separate volcanic peaks along the backbone, each a compact cone 10–40 km across, 50–150 km apart, with heights from the relief draw.
  Peaks that rise above sea level after `landmask` become the arc's islands.
- **Trench:** an active margin or island arc facing ocean gets a trench on its ocean side: a narrow depression to 6,000–8,000 m below the datum, beyond the edge of the feature's envelope, following the backbone.
  The trench is terrain's own addition, with starting values of its own; structure records no trench feature.
- **Fault:** a narrow linear form within its width: a trough up to 200 m deep for `strike_slip`, a step of up to 300 m across the line for `normal`, and a low ridge up to 300 m high for `thrust`.
  A fault never makes a mountain range by itself.

## Rasterization

Most cells lie under no feature.
They take the foundation, evaluated once at the cell center, and contribute no relief beyond their residual detail.

A cell intersected by any feature's bounds is sampled at a fixed 4 by 4 grid of subcell points, as section 6 requires.
At each point the full surface (foundation, regional surface, ridges, depressions) is evaluated.
Then:

- `ElevationM` is the mean of the 16 samples;
- the initial high is the largest sample and the initial valley the smallest;
- a bounded subcell relief model adds relief for forms narrower than about two cells, which 16 samples cannot resolve: volcanic peaks, narrow ridge crests, and fault scarps add their analytic peak height above the sampled maximum to the high, up to the feature's relief draw;
- `HighM` is the high, at least `ElevationM`, and `ReliefM` is high minus valley, at least 0.

`HighM` is a representative high, not a guaranteed maximum over the cell.
Drainage recomputes relief later against the final valley floor.

A cell's bounds test uses each feature's `Bounds()`, expanded for ridges, trenches, and kernels that reach past the envelope.
A coarse spatial index, features binned by bounds on the working grid, finds the candidate features for each cell.
Features are always visited in ascending ID order, and every per-cell result depends only on that cell, so the output does not depend on the number of workers or on tile order.

## Residual detail

Residual noise adds variation to mean elevation that the structure does not specify.
Section 6 sets two bounds, which are contracts here:

- noise that changes `ElevationM` has wavelengths of at least four cells (24 km at 6 km cells); shorter wavelengths affect only `ReliefM` and `HighM`;
- its amplitude is at most 10 percent of the cell's structured relief, with a small floor, default 20 m, so quiet interiors are not perfectly smooth.

Within a belt envelope, the noise is stretched along the local backbone direction, so it reads as structure-aligned grain rather than isotropic bumps.
The noise is a fracture field, like the rock noise, built on grids that tile the world, so it wraps on both axes.
`--residual=false` omits it, and the result must still show every feature clearly.

## Random keys and streams

All streams use `prng.New(seed, domain, sequence)` with the terrain seed:

| Domain | Sequence | Use |
| --- | --- | --- |
| `terrain-v1/foundation/provinces` | Province ID | Interior base level of one province |
| `terrain-v1/foundation/fields` | Field number | Smooth fields: interior variation (0), shelf depth (1), shelf width (2), abyssal depth (3) |
| `terrain-v1/feature` | Feature `key` | Target control samples, chains, branches, gaps, corridors, peaks, and trench depth for one feature |
| `terrain-v1/residual` | Octave number | One octave of residual noise |

Using the feature `key`, not the feature ID or its position in the list, keeps each feature's terrain independent of every other feature: adding, removing, or reordering a feature in a fixture leaves the others unchanged.
Plateaus and basins have their own keys and streams; their parent's stream is not consumed on their behalf.
The order of draws within each stream is part of `terrain-v1`; changing it changes outputs and requires a format bump.

## Saved artifact

The format is `mapgen-terrain-v1`.

| Content | Purpose |
| --- | --- |
| `manifest.json` | Format and generator versions, input structure manifest and its checksum, seed, grid, resolved parameters (`foundation-v1`, `belts-v1`, `subcell-v1`, `residual-v1`), residual on or off, recentering, and checksums for every saved file |
| `elevation.f32` | `ElevationM`: mean solid surface elevation, m, relative to the provisional datum |
| `high.f32` | `HighM`: representative high, m, at least `ElevationM` |
| `relief.f32` | `ReliefM`: representative high minus valley, m, at least 0 |
| `ridges.json` | Ridge chains, branches, corridors, and saddles in the structure coordinate convention, keyed by parent feature |
| Retained structure records | `features.json`, `provinces.json`, `provinces.u32`, `provisional-land.u8`, `rock.u8`, `pressure.f32`, `shear.f32`, `igneous.f32`, `rock-noise.f32`, unchanged |
| `diagnostics.json` | Elevation and relief statistics on provisional land and water, the datum shift and changed cells at the provisional fraction, target conformance per feature, and noise bounds measured |
| Previews | See "Previews and acceptance" |

Rasters are float32 little-endian, full resolution, row-major, row 0 at the north, as everywhere else.
The structure records are retained so the artifact is self-contained: `landmask` and the stages after it never need the structure directory, and resources reach geology through each checkpoint.
The continental affinity fields are not retained: they are layout values that elevation supersedes, and their checksums remain in the recorded input manifest chain.

Input paths are not recorded.
Timing and memory measurements are kept apart from canonical metadata.
The artifact is written to a temporary directory and published without replacing an existing one.

On the full-size grid (6666 by 3333 cells) each float32 field is about 89 MB; the three terrain fields and the retained structure fields make the artifact about 650 MB.
Peak memory should stay under the cylindrical specification's 2 GB target; the foundation's distance transform and the elevation fields dominate it.

## Recentering

Terrain is toroidal, so `--recenter` translates the complete result by whole cells, exactly as structure does: rasters shift cyclically, feature, ridge, and province-seed geometry moves by the same distance in km and is renormalized to the base domain, and IDs and keys are kept.
The result is generated in the input's frame and then translated; nothing is regenerated.
The landmass centered is the largest eight-connected component of the cells above the provisional-fraction quantile, the coast terrain implies, so a recentered terrain is centered on its own land rather than the provisional land.
The manifest's `recentering` record is always present, as in structure.

## Previews and acceptance

- `preview.png`: hypsometric tint with hill shading, and the coast at the provisional-fraction quantile drawn as a line.
- `preview-structure.png`: the surface without residual detail and without shading, with feature envelopes, ridge chains, and corridors drawn over it, to judge structure against terrain.
- `preview-relief.png`: `ReliefM` in bands, with the same coast line.
- `preview-change.png`: the provisional mask against the provisional-fraction land, marking cells that terrain moved from water to land and from land to water.

The user's review asks: do the ranges look like ranges, not ribbons; do plateaus, basins, and rifts read as such; are interiors broad and quiet; do arcs break into islands; do passes cross belts; and has the coast moved only where features justify it?

Correctness checks cover determinism across worker counts, finite values, `HighM >= ElevationM` and `ReliefM >= 0`, seam continuity on both axes, recentering invariance, input preservation, the residual bounds, and target conformance.

## Verification

Tests use the smallest grid that shows each behavior.

1. **Foundation.**
   A straight coastline on a small torus gives the land and water profiles analytically at chosen distances; a provisional mask with no features gives exactly the provisional land above 0 m.
2. **Kernel profile.**
   A straight belt of constant width with residual off and no ridges gives `F + w * (T − F)` across the belt within float32 tolerance.
3. **Targets.**
   With residual off, a plateau's interior lies at its regional draw; a basin's floor at its target; a rift's axis below its shoulders by the relief draw; every regional draw lies within its range.
4. **Combination.**
   Two crossing belts reach at most the larger uplift plus 25 percent of the smaller; a basin inside a belt's flank lowers it to the basin level.
5. **Seams.**
   A feature crossing either seam produces the same elevations as the same feature shifted by one world width or height, compared through periodic queries rather than equal edge columns.
6. **Determinism.**
   One run with 1 worker and one with 8 give byte-identical artifacts.
7. **Seed independence.**
   Changing the terrain seed changes ridges and residual detail but not where the regional surface's envelopes begin and end.
   Adding a feature to a fixture leaves the elevations outside its reach byte-identical.
8. **Resolution.**
   The same physical domain and features at two cell sizes give regional means that agree within a stated tolerance, cells the envelope boundary crosses excepted.
9. **Residual bounds.**
   With residual on, `ElevationM` differs from the residual-off surface by no more than 10 percent of structured relief or the floor, and contains no wavelength under four cells.

## Landmask integration

`landmask` gains a terrain-input mode that implements section 7 of the cylindrical specification against `elevation.f32`:

1. Select `k = round(cells * fraction)` land cells by elevation quantile, ties by index, and shift that level to 0 m.
2. Take the largest eight-connected component of the lowest `cells − k` cells, periodic in both axes, and grow it to exactly `cells − k` through a priority queue keyed by elevation then index; that is the ocean.
3. Require the repair fraction, the symmetric difference between that ocean and the quantile mask over all cells, to be at most 0.01.
4. Clamp ocean cells to at most −1 m and other cells to at least +1 m, rejecting the layout if any cell moves more than 500 m, and report the changed cells.

The specification's retry over continental attempts does not fit a staged pipeline.
A layout that fails steps 3 or 4 is an error that names the measure, and the user varies the terrain or structure seed.
The terrain-mode artifact carries the clamped elevation, high, and relief fields with the frozen ocean mask, and its manifest identifies the input kind (`terrain`) and the selection algorithm, distinct from the affinity mode, which remains available for inspection.

## Implementation order

1. **Foundation and artifact.** Done; see `cmd/terrain/README.md`.
   Full-resolution torus distance transform, land and water profiles, province base levels, the artifact with `Load`, `preview.png`, and the foundation tests.
   No features yet: the provisional land is exactly the land above 0 m.
2. **Regional surface on fixtures.**
   Envelope weights and target interpretation for every feature type, the combination rule, and `preview-structure.png`, with residual off.
   This is the handoff the structure design asks to prove on fixed features.
3. **Belt components.**
   Ridge chains, branches, longitudinal basins, corridors and saddles, volcanic peaks, trenches, and `ridges.json`.
4. **Subcell rasterization.**
   The 4 by 4 sampling, high and relief, the subcell relief model, the spatial index, and parallel tiles with the worker-count test.
5. **Residual detail** with its bounds tests.
6. **Recentering.**
7. **Landmask terrain-input mode.**

### Prerequisite in structure

Structure step 3 is finished: automatic placement produces every feature kind, including interrupted old belts, intermontane basins, and interior basins.
`internal/structure/testdata/fixture.json` holds one of every kind, so terrain step 2 can test the handoff on it.
An interrupted belt is a chain of `old_orogen` segments, each after the first naming the first as its parent; terrain treats each segment as an ordinary belt.

## Open decisions

These choices are made above, but are worth confirming before implementation:

- the provisional datum, with the provisional coast at 0 m, rather than terrain choosing sea level itself;
- dropping the continental affinity fields from the terrain artifact;
- trenches as a terrain addition rather than a structure feature;
- landform classes (plain, hills, mountain, plateau, basin) deferred until after drainage, since they depend on the final coast and valleys.

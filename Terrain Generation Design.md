# Terrain Generation Design

## Purpose and place in the pipeline

`terrain` turns an accepted trench checkpoint, which carries the structure records and the trench records, into ground and seabed elevation: a continental foundation, mountain belts with ridges and passes, plateaus, basins, rifts, island arcs, and the seabed.
It runs before final coastline selection.
The coast it implies can differ from the provisional coast that structure used, and `landmask` freezes ocean membership from this terrain afterwards.

```text
continents → structure → trench → terrain → landmask → drainage → climate → ecosystems
                 └──────────────────────────────────────────────────────→ resources
```

* `structure` supplies the provisional mask, provinces, authoritative feature records with elevation and relief targets, and geology.
* `trench` supplies the polarity of every active margin and island arc, and trench records; see [Trench Generation Design.md](<Trench Generation Design.md>).
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
Nor may it move, add, or remove a trench; those belong to the accepted trench checkpoint.

## Command interface

Proposed usage:

```sh
go run ./cmd/terrain \
  --in tmp/trench-42 \
  --seed 42 \
  --out tmp/terrain-42
```

| Flag | Meaning |
| --- | --- |
| `--in` | Accepted trench artifact; default `tmp/trench` |
| `--out` | New output directory; default `tmp/terrain`. An existing one is never replaced |
| `--seed` | Terrain seed; if omitted, the trench seed. The manifest records the seed and whether it was explicit |
| `--residual` | Add residual detail noise; default true. `--residual=false` publishes the structured surface alone, which must remain recognizable |
| `--recenter` | Translate the complete result; off by default |
| `--png` | Generate previews; default true |
| `--png-scale` | Sampling interval for previews; default 1 |
| `--config` | Calibration file, `mapgen-terrain-config-v1`, overriding default parameters |
| `--print-config` | Print the default calibration as a configuration file |
| `--workers` | Number of worker goroutines; default the number of CPUs. Never affects the output |
| `--version` | Print the generator version |

`--config` reads a calibration file in format `mapgen-terrain-config-v1`: the format, and any of the sections `foundation`, `regional`, `belts`, `subcell`, and `residual`, each naming only the values it changes; map entries merge by key.
`--print-config` prints the defaults as such a file, a starting point to edit.
A configuration changes values, not algorithms: algorithm names and descriptions cannot change, maps keep the default keys, every number stays finite with its default's sign (zero allowed), every pair keeps its order, values the algorithms divide or count by stay positive, and shares stay at most 1; anything else is refused with the name of the value.
The resolved values are recorded in the manifest as the `*_parameters`, so the artifact alone says how it was made; the file's path is not recorded.
Structure has the same mechanism, `mapgen-structure-config-v1`.
There is no fixture flag: structure's `--features` already supplies fixed features, and terrain reads them, with the trenches, from the trench artifact.

## Inputs and physical domain

Load and verify the trench artifact with `trench.LoadRecords`, which verifies the structure records it retains, without the structure or continental directories.
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
Near an active margin or island arc, the shelf width is reduced toward 50 km: margins facing a trench have narrow shelves.
Within 1.5 half widths of the backbone the shelf is 50 km wide, or its own width if narrower, easing back to its own width at 3 half widths.
This depends on features, so it landed with step 2, as `foundation-v2`.
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

A record may omit `targets`, or one of them; fixtures often do.
Terrain then uses the range structure's automatic placement assigns to that type and kind, recorded as `regional_parameters.default_targets`.
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

`belts-v1` builds the components this way; every value is recorded as `belt_parameters`.

- **Chains** are offset curves of the backbone, sampled every 15 km, evenly spaced within 0.6 of the half width and jittered by up to a fifth of their spacing.
  Each wanders across the belt by up to 0.08 of the half width, as the mean of two sinusoids with wavelengths of 200–600 km.
  A chain's kernel half width is 0.55–0.8 of the chain spacing, narrowed by up to 35 percent with maturity, and at least 12 km; chain, kernel, and wander stay within 0.9 of the half width.
  A vertex whose step runs backward is dropped, removing the loops an offset curve forms in a tight bend.
  Each chain starts and ends up to a tenth of the backbone's length from its ends.
- **Amplitude** is the relief draw at the nearest backbone position, pulled toward the bottom of the relief range by 0.7 × maturity.
  The main chain has the full amplitude: on an active margin the chain nearest the coast, opposite the elevated flank; otherwise the middle chain.
  The others draw 0.6–0.95 of it, the larger values going to the chains nearest the main chain.
- **Gaps** occur 0.3 per 1000 km of chain at maturity 0, rising to 2.5 at maturity 1, at exponentially distributed distances, each 20–80 km long times 1 + 2 × maturity.
- **Branches** leave a chain 1.2 per 1000 km at maturity 0, falling to 0.2 at maturity 1, at 25–50° outward from it, reaching half to all of the room left to 0.9 of the half width, at 0.4–0.7 of the chain's amplitude and tapering to nothing.
  A branch shorter than 30 km is not placed.
- **Troughs** lie midway between adjacent chains with probability 0.3 at maturity 0 to 0.7 at maturity 1, over 100–400 km, 100–600 m deep, as depressions of the regional surface under the largest-depression rule.
- **Corridors** cross the belt every 300–700 km, up to 20° from square, with a kernel half width of 15–40 km, lowering every ridge of the belt by 0.6–0.85 of its height on their line, so the saddles stay above the regional surface.
  A saddle is published where a corridor crosses a ridge, with the surface elevation of the cell there.
- **Volcanic peaks** of an active margin, present with probability 0.6, lie halfway from the backbone to the main chain, 50–150 km apart, 10–40 km across, 0.7–1.1 of the amplitude high.
- Chains, branches, and peaks combine by maximum and add to the regional surface.
  Over a basin they fade out with the basin's weight, so an intermontane or foreland basin stays a basin.
  A ridge's kernel or a peak may reach a little past a narrow envelope.

### Other features

- **Plateau:** a flat surface at its regional target inside its outline, rising over `edge_km`.
  Its relief target sets low-amplitude rolling detail and a rougher, dissected rim along the edge band.
  In `regional-v2` the plateau's target at a point is its regional draw plus its relief draw times 0.25 of one smooth field (120 km wavelength) plus 0.5 × 4m(1 − m) of another (48 km), `m` the outline mask, so the interior rolls and the rim is roughest midway up the edge.
  This is structured surface, kept with `--residual=false`.
  It blends with its parent belt's flank through the uplift rule.
- **Basin:** a depression to its regional target, which is low (0–400 m on land), with a smooth floor.
  A basin under the datum floods at `landmask` only if it connects to the ocean; otherwise it is a closed depression for drainage to resolve.
- **Rift:** the shoulders are uplift to the regional target, at full weight within `axis_fraction` `a` of the half width and weighing `K((t − a) / (1 − a))` beyond it; the axis is a depression weighing `K(t / a)` to the shoulder level, the larger of the target and the foundation, minus the relief draw.
  So the shoulders crest at the axis edge, and the axis floor may fall under the datum: a rift can become an inlet, a lake, or an arm of the sea.
  In `regional-v3` the axis is at least `rift_floor_m` (300 m) below the lower of the foundation and the datum: on land a rift floor ends no deeper than a dry continental depression (the Dead Sea shore is 430 m below sea level), so `landmask` can raise a rift cut off from the sea to the frozen coast within its 500 m limit; offshore it may still cut 300 m below the seabed.
  Without the floor, shoulders of 300 m and a relief draw of 2000 m put inland rift axes 1400 m below the datum on the full-size seed 42 map.
- **Island arc:** a submarine ridge to its regional target, plus separate volcanic peaks along the backbone, each a compact cone 10–40 km across, 50–150 km apart, 0.6–1 of the relief draw high, with the profile `K(r / radius)`.
  Peaks that rise above sea level after `landmask` become the arc's islands.
- **Trench:** each trench record is a narrow depression along its backbone, to its floor target below the datum, deepest on the axis and easing to the surrounding seabed at the edge of its width.
  The floor is drawn within the record's range as control samples every 150–400 km, like a feature's targets, and the depression fades in over 100 km at each end, or a third of the trench's length if shorter.
  The `trench` stage decides where trenches lie and on which side of their parents; terrain only gives them depth.
  A trench never lowers provisional land: trench placement looks past islands smaller than structure's province minimum, and those islands stay.
- **Fault:** a narrow linear form within its width, weighted by `K(t)`: a trough 50–200 m deep for `strike_slip`, a step of 100–300 m across the line for `normal`, and a low ridge 100–300 m high for `thrust`.
  A normal fault raises one side by half its offset and lowers the other by half, the downthrown side drawn from its stream; the trough is a depression and the ridge an uplift in the combination rule.
  A fault never makes a mountain range by itself.

## Rasterization

Most cells lie under no feature.
They take the foundation, evaluated once at the cell center, and contribute no relief beyond their residual detail.

A cell that any feature, ridge, trough, peak, or trench reaches at one of its subcell points is sampled at a fixed 4 by 4 grid of subcell points, as section 6 requires.
At each point the full surface (foundation, regional surface, ridges, depressions) is evaluated.
At a subcell point the foundation takes the signed coast distance and the shelf narrowing interpolated bilinearly from the cell centers, so the provisional coast falls on cell edges as before.
A trench reaches only the points of provisional water cells.
Then:

- `ElevationM` is the mean of the 16 samples;
- the initial high is the largest sample and the initial valley the smallest;
- a bounded subcell relief model adds relief for forms narrower than about two cells, which 16 samples cannot resolve: volcanic peaks, narrow ridge crests, and fault scarps add their analytic peak height above the sampled maximum to the high, up to the feature's relief draw.
  In `subcell-v1`, a ridge whose crest passes within half a sample diagonal of a sample, or a peak centered in the cell, offers its crest height (after corridors and basins); the high gains whatever of the largest such crest exceeds the largest ridge or peak component sampled in the cell.
  A fault scarp is a step, which the samples on its two sides already span;
- `HighM` is the high, at least `ElevationM`, and `ReliefM` is high minus valley, at least 0.

`HighM` is a representative high, not a guaranteed maximum over the cell.
Drainage recomputes relief later against the final valley floor.

The work is divided into tiles of 64 by 64 cells.
A spatial index bins every feature, trough, ridge, peak, and trench by its bounds into the tiles its cells reach, and a tile visits only those, rasterizing each linear one on the tile's subcell points alone.
Features are always visited in ascending ID order, and every per-cell result depends only on that cell, so the output does not depend on the number of workers or on tile order; a test compares one worker with eight byte for byte.
The feature diagnostics are still measured at cell centers.

## Residual detail

Residual noise adds variation to mean elevation that the structure does not specify.
Section 6 sets two bounds, which are contracts here:

- noise that changes `ElevationM` has wavelengths of at least four cells (24 km at 6 km cells); shorter wavelengths affect only `ReliefM` and `HighM`;
- its amplitude is at most 10 percent of the cell's structured relief, with a small floor, default 20 m, so quiet interiors are not perfectly smooth.

Within a belt envelope, the noise is stretched along the local backbone direction, so it reads as structure-aligned grain rather than isotropic bumps.
The noise is a fracture field, like the rock noise, built on grids that tile the world, so it wraps on both axes.
`--residual=false` omits it, and the result must still show every feature clearly.

`residual-v1` does this at cell centers, after subcell sampling:

- The noise has octaves of 192, 96, and 48 km, weighted 0.7^k, each a fracture field on a grid of a quarter of its wavelength normalized to [−1, 1].
  An octave whose grid would be finer than two cells is left out, so at coarser cells there are fewer octaves.
- Within a belt, the noise is the mean of 13 samples along the backbone direction over ±150 km, blended with the plain noise by the belt's kernel weight at the cell; the belt with the largest weight sets the direction.
  The amplitude falls by up to half with maturity times that weight.
- The amplitude is 10 percent of the structured relief, at least the 20 m floor; but the floor is at most half the cell's distance from the datum.
  Without this cap the floor floods and drains flat coastal plains: at seed 42 it moved 614,000 cells across the provisional coast, where features move 18,000.
  With it, only relief, which features make, can move the coast.
- Shorter wavelengths are not added to the elevation: half the amplitude is added to `ReliefM`, and a quarter to `HighM`.
- Tests measure the bound on every cell, less than 0.1 percent of the residual's power at wavelengths under four cells, and residual differences along a belt below 0.6 of those across it.

## Random keys and streams

All streams use `prng.New(seed, domain, sequence)` with the terrain seed:

| Domain | Sequence | Use |
| --- | --- | --- |
| `terrain-v1/foundation/provinces` | Province ID | Interior base level of one province |
| `terrain-v1/foundation/fields` | Field number | Smooth fields: interior variation (0), shelf depth (1), shelf width (2), abyssal depth (3) |
| `terrain-v1/feature` | Feature `key` | Target control samples, chains, branches, gaps, corridors, and peaks for one feature. A fault draws its offset, then its downthrown side; a linear feature its control spacing, then a regional and a relief value at each control sample; an area feature its regional and relief values. A mountain belt then draws its chain count, chain width share, and for each chain its jitter, scale, two wavelengths and phases, two end trims, and fade; then each chain's gaps, each chain's branches (angle, reach, scale, side), each adjacent pair's trough (presence, start, length, depth), its corridors (angle, half width, saddle share), and for an active margin the presence of volcanic peaks and then each peak (diameter, height). An island arc draws its peaks |
| `terrain-v1/trench` | Trench `key` | The floor of one trench: its control spacing, then a floor value at each control sample |
| `terrain-v1/residual` | Octave number | One octave of residual noise |
| `terrain-v1/plateau` | Field number | The plateau rolling field (0) and rim field (1), shared by every plateau |

Using the feature `key`, not the feature ID or its position in the list, keeps each feature's terrain independent of every other feature: adding, removing, or reordering a feature in a fixture leaves the others unchanged.
Plateaus and basins have their own keys and streams; their parent's stream is not consumed on their behalf.
The order of draws within each stream is part of `terrain-v1`; changing it changes outputs and requires a format bump.

## Saved artifact

The format is `mapgen-terrain-v8`; v2 read a trench checkpoint instead of a structure checkpoint, v3 added the regional surface, v4 belt components, peaks, trench depressions, and `ridges.json`, v5 subcell sampling with real high and relief, v6 residual detail and plateau detail, v7 recentering, and v8 the rift floor.

| Content | Purpose |
| --- | --- |
| `manifest.json` | Format and generator versions, input trench manifest (which records the structure manifest) and its checksum, seed, grid, resolved parameters (`foundation-v2`, `regional-v3`, `belts-v1`, `subcell-v1`, `residual-v1`), how far feature synthesis goes (`feature_synthesis`), residual on or off, recentering, and checksums for every saved file |
| `elevation.f32` | `ElevationM`: mean solid surface elevation, m, relative to the provisional datum |
| `high.f32` | `HighM`: representative high, m, at least `ElevationM` |
| `relief.f32` | `ReliefM`: representative high minus valley, m, at least 0 |
| `ridges.json` | Format `mapgen-ridges-v1`: ridge chains and branches (backbones with kernel widths, and gaps in km along them), troughs with depths, corridors with saddle shares and saddles (ridge, position, elevation), and volcanic peaks (position, radius, height), in the structure coordinate convention, keyed by parent feature |
| Retained records | The structure records `features.json`, `provinces.json`, `provinces.u32`, `provisional-land.u8`, `rock.u8`, `pressure.f32`, `shear.f32`, `igneous.f32`, `rock-noise.f32`, and the trench records `trenches.json`, unchanged |
| `diagnostics.json` | Elevation and relief statistics on provisional land and water, the datum shift and changed cells at the provisional fraction, target conformance per feature (its drawn targets and the elevation of its core, the cells where its own weight is at least 0.9, a rift's shoulders), and noise bounds measured |
| Previews | See "Previews and acceptance" |

Rasters are float32 little-endian, full resolution, row-major, row 0 at the north, as everywhere else.
The structure and trench records are retained so the artifact is self-contained: `landmask` and the stages after it never need the structure or trench directories, and resources reach geology through each checkpoint.
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
The manifest's `recentering` record is always present, as in structure, and gives this stage's shift relative to its trench input.

The retained records move too: the structure records by structure's own translation, with the step accumulated into their manifest's recentering; the trench records, ridges, troughs, and corridors by the same distance in km, each backbone renormalized; saddles and peaks into the base domain.
When the shift is not zero the manifest adds `recentered_records`: the input trench manifest as the trench stage would have written it had it recentered by the combined shift, its own `recentered_records` the moved structure manifest.
So `trench.LoadRecords` verifies the moved records with its usual checks, and Load accepts them only if they differ from the input trench manifest in nothing but the record files and the accumulated recentering.
With a zero shift the records are retained byte for byte.
Rasters rotate in place, so recentering a full-size result does not keep a second copy of each.

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
   With residual off, a plateau's interior lies at its regional draw; a basin's floor at its target; a rift's axis below its shoulders by the relief draw, or at its floor; every regional draw lies within its range.
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

Implemented as `landmask` format `mapgen-landmask-terrain-v2`, selection `ocean-v1`; see `cmd/landmask/README.md`.

- The command reads the input's manifest format and chooses the mode; without `--land-fraction`, terrain input uses its provisional land fraction.
- Sea level is the level terrain's diagnostics report as the shift, midway between the lowest land and the highest water of the quantile, so the two always agree.
- The connected-ocean selection is shared with structure (`fracture.ConnectedOcean`), which applies it to affinity for the provisional mask.
  Before structure did so, the provisional mask's enclosed waters, which terrain correctly builds as sea floor, failed the repair limit on every seed: 3.1 percent on the full-size seed 42 map.
- Clamping moves high with elevation, so relief is unchanged.
  Non-ocean cells at or below sea level before clamping are counted as depressions, where drainage may place lakes; the rest is dry land.
- The artifact retains the structure and trench records and `ridges.json` unchanged; ridge and saddle elevations stay relative to the provisional datum, and the manifest's `sea_level_m` converts them.
- `--recenter` centers the largest landmass of the frozen mask, moving the records exactly as terrain's recentering does (`terrain.MoveRecords`), and is checked on load the same way.

On the full-size seed 42 map sea level is 0.11 m below the provisional datum, the repair is 6,940 cells (0.031 percent) from 126 quantile ocean components, 3,470 cells are depressions, and the largest clamp raises a rift floor by 301 m.
It runs in about 7 seconds with a peak of 1.7 GB.

## Implementation order

1. **Foundation and artifact.** Done; see `cmd/terrain/README.md`.
   Full-resolution torus distance transform, land and water profiles, province base levels, the artifact with `Load`, `preview.png`, and the foundation tests.
   No features yet: the provisional land is exactly the land above 0 m.
2. **Regional surface on fixtures.** Done.
   Envelope weights and target interpretation for every feature type, the combination rule, narrow shelves near margins and arcs, and `preview-structure.png`, with residual off.
   The surface is evaluated at cell centers; step 4 replaces that with subcell sampling.
   The structure fixture's handoff test shows every feature in the surface, with each draw in its range.
3. **Belt components.** Done.
   Ridge chains, branches, longitudinal basins, corridors and saddles, volcanic peaks, the trench depressions from the trench records, and `ridges.json`.
   Still evaluated at cell centers, so peaks narrower than a cell may fall between centers until step 4.
4. **Subcell rasterization.** Done.
   The 4 by 4 sampling, high and relief, the subcell relief model, the spatial index, and parallel tiles with the worker-count test.
5. **Residual detail** with its bounds tests. Done, with plateau rolling detail and rims.
6. **Recentering.** Done.
7. **Landmask terrain-input mode.** Done; see "Landmask integration".

### Prerequisites in structure and trench

Structure step 3 is finished: automatic placement produces every feature kind, including interrupted old belts, intermontane basins, and interior basins.
`internal/structure/testdata/fixture.json` holds one of every kind, so terrain step 2 can test the handoff on it.
An interrupted belt is a chain of `old_orogen` segments, each after the first naming the first as its parent; terrain treats each segment as an ordinary belt.

The `trench` stage is implemented; terrain reads its checkpoint.
The structure fixture's world is too small for a trench, so trench depressions are tested on a synthetic trench record; a handoff test through the trench stage still needs a larger fixture or an automatically placed structure.

## Decisions

These choices were open and are settled:

- Trenches are records of their own `trench` stage, between structure and terrain, rather than a terrain addition or a structure feature.
- The residual floor is capped at half a cell's distance from the datum, so residual detail alone never moves the provisional coast.
- Landform classes (plain, hills, mountain, plateau, basin) are deferred until after drainage, since they depend on the final coast and valleys.
- Elevations are relative to the provisional datum, with the provisional coast at 0 m; terrain does not choose sea level.
  `landmask` alone counts the land exactly and decides which low ground joins the ocean, the land fraction can change without regenerating terrain, and terrain reports the shift `landmask` would make.
- Structure's provisional mask has one connected ocean, selected by the same rule `landmask` applies to elevation, so terrain builds sea floor only where the sea is.
- A rift's axis is floored 300 m below the lower of the foundation and the datum, so a rift on land cannot fail `landmask`'s clamp limit.
- The continental affinity fields are not carried past structure.
  No later stage reads them: provinces mark oceanic and continental crust, and elevation replaces the layout values.
  If a later stage needs a continuous crust value, the intermediate stages will be extended to carry it forward.

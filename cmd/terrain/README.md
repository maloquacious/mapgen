# Terrain command

Build ground and seabed elevation from a trench artifact and save a terrain checkpoint.

```sh
go run ./cmd/continents --seed 42 --out tmp/continents
go run ./cmd/structure --in tmp/continents --out tmp/structure
go run ./cmd/trench --in tmp/structure --out tmp/trench
go run ./cmd/terrain --in tmp/trench --png-scale 3 --out tmp/terrain
```

The design is in [Terrain Generation Design.md](<../../Terrain Generation Design.md>).
This covers implementation steps 1 and 2 of that design: the continental foundation, the artifact, and the regional surface each feature pulls the foundation toward, evaluated at cell centers.

## Not implemented yet

- Ridge chains, branches, longitudinal basins, corridors and passes, volcanic peaks, and trench depressions (step 3), and `ridges.json`.
- Subcell sampling (step 4): `feature_synthesis` is `regional-v1`, so `high` equals `elevation` and `relief` is 0.
- `preview-relief.png` and `preview-change.png`.
- `--residual`, `--recenter`, `--workers`, and `--config`.

## Flags

| Flag | Meaning |
| --- | --- |
| `--in` | Trench artifact directory; default `tmp/trench` |
| `--out` | New output directory; default `tmp/terrain`. An existing one is never replaced |
| `--seed` | Terrain seed, unsigned 64-bit. If omitted, the trench input's seed is used; the manifest records the seed and which of the two it was |
| `--png` | Write `preview.png` and `preview-structure.png`; default true |
| `--png-scale` | Sampling interval in cells for the previews; default 1 |
| `--version` | Print the generator version |

## The foundation

Elevations are meters relative to a provisional datum: 0 m is the structure records' provisional coast, not sea level.

Every cell gets its exact distance to the provisional coast on the torus, at full resolution, from the unsimplified provisional mask.
Land rises from 0 m at the coast to an interior base level over 250 km.
Each land province draws its base level once: shield 300–600 m, sedimentary 100–300 m, volcanic 400–800 m.
The levels are averaged on a 50 km working grid, over land provinces only, and smoothed with a 150 km Gaussian, so province boundaries do not show as steps.
Land beyond the smoothing's reach of any land province, a small island in an oceanic province, uses 200 m.
A smooth field varies the base level by ±15 percent.

Water falls through a shelf (50–200 m deep, 50–200 km wide), a 150 km slope, and an abyssal plain (3,500–5,500 m), each range spanned by its own smooth field.
Each segment eases into the next.
Within 1.5 half widths of an active margin's or island arc's backbone the shelf narrows to 50 km, easing back to its own width at 3 half widths.
Small enclosed waters get the same profile, so they are shallow pits.

The manifest records every value as `foundation_parameters`.

## The regional surface

Each feature pulls the foundation toward its regional target through its envelope weight.
A linear feature weighs `K(t) = (1 − t²)²` at cross-belt position `t`; an area feature weighs a smoothstep of its outline mask.
Targets are drawn within the record's ranges from the feature's own stream: a linear feature draws a control sample every 150–400 km along its backbone and eases between them; an area feature draws once.
A feature whose record gives no targets uses the ranges structure's automatic placement assigns to its type and kind.

- Belts, island arcs, and plateaus raise ground below their target to it, and leave higher ground alone.
- Basins lower ground above their target to it.
- A rift raises its shoulders to its target, at full weight within its axis fraction and falling to 0 at its edge, and cuts its axis to the shoulder level minus its relief draw, so the axis may fall below the datum.
- Faults have no targets: a strike-slip fault is a trough 50–200 m deep, a normal fault a step of 100–300 m with a drawn downthrown side, and a thrust fault a ridge 100–300 m high, each confined to its width.

Uplifts combine by the largest plus a quarter of the second largest, so crossing belts do not double; then the largest depression applies, so a basin inside a belt's flank lowers it to the basin's level.
The manifest records every value as `regional_parameters`.

## Output

| File | Content |
| --- | --- |
| `manifest.json` | Format `mapgen-terrain-v3`, versions, the input trench manifest (which records the structure manifest) and its checksum, seed, grid, datum, parameters, retained records, and checksums |
| `elevation.f32`, `high.f32`, `relief.f32` | Float32 rasters, row-major, row 0 at the north |
| `features.json`, `provinces.json`, `provinces.u32`, `provisional-land.u8`, `rock.u8`, `pressure.f32`, `shear.f32`, `igneous.f32`, `rock-noise.f32` | The structure records, byte for byte |
| `trenches.json` | The trench records, byte for byte |
| `diagnostics.json` | Elevation and relief statistics on provisional land and water, the shift at the provisional fraction, and for each feature its drawn targets and the elevation of its core (the cells where its weight is at least 0.9; a rift's shoulders) |
| `preview.png` | Hypsometric tint with hill shading, and the coast at the provisional fraction as a line |
| `preview-structure.png` | The same tint without shading, each feature's envelope outline, every backbone, and trench backbones in red |

The continental affinity fields are not kept; their checksums remain in the recorded input manifest chain.
The shift reports what `landmask` would do at the provisional land fraction: the elevation it would move to 0 m, and how many cells would change sides.
Features move the coast where they raise shallow seabed or lower coastal land.

On the full-size grid (6666 x 3333) a run takes about 13 seconds, with about 2 GB of peak resident memory.

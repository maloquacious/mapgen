# Terrain command

Build ground and seabed elevation from a structure artifact and save a terrain checkpoint.

```sh
go run ./cmd/continents --seed 42 --out tmp/continents
go run ./cmd/structure --in tmp/continents --out tmp/structure
go run ./cmd/terrain --in tmp/structure --png-scale 3 --out tmp/terrain
```

The design is in [Terrain Generation Design.md](<../../Terrain Generation Design.md>).
This covers implementation step 1 of that design: the continental foundation and the artifact.
The surface is the foundation alone, so the provisional land is exactly the cells above 0 m.

## Not implemented yet

- Feature synthesis: belts, ridges, plateaus, basins, rifts, arcs, and faults (`feature_synthesis` is `absent`; `high` equals `elevation` and `relief` is 0).
- Narrower shelves facing active margins and island arcs, which depend on features.
- `ridges.json`, `preview-structure.png`, `preview-relief.png`, and `preview-change.png`.
- `--residual`, `--recenter`, `--workers`, and `--config`.

## Flags

| Flag | Meaning |
| --- | --- |
| `--in` | Structure artifact directory; default `tmp/structure` |
| `--out` | New output directory; default `tmp/terrain`. An existing one is never replaced |
| `--seed` | Terrain seed, unsigned 64-bit. If omitted, the structure input's seed is used; the manifest records the seed and which of the two it was |
| `--png` | Write `preview.png`; default true |
| `--png-scale` | Sampling interval in cells for `preview.png`; default 1 |
| `--version` | Print the generator version |

## The foundation

Elevations are meters relative to a provisional datum: 0 m is the structure's provisional coast, not sea level.

Every cell gets its exact distance to the provisional coast on the torus, at full resolution, from the unsimplified provisional mask.
Land rises from 0 m at the coast to an interior base level over 250 km.
Each land province draws its base level once: shield 300–600 m, sedimentary 100–300 m, volcanic 400–800 m.
The levels are averaged on a 50 km working grid, over land provinces only, and smoothed with a 150 km Gaussian, so province boundaries do not show as steps.
Land beyond the smoothing's reach of any land province, a small island in an oceanic province, uses 200 m.
A smooth field varies the base level by ±15 percent.

Water falls through a shelf (50–200 m deep, 50–200 km wide), a 150 km slope, and an abyssal plain (3,500–5,500 m), each range spanned by its own smooth field.
Each segment eases into the next.
Small enclosed waters get the same profile, so they are shallow pits.

The manifest records every value as `foundation_parameters`.

## Output

| File | Content |
| --- | --- |
| `manifest.json` | Format `mapgen-terrain-v1`, versions, the input structure manifest and its checksum, seed, grid, datum, parameters, retained records, and checksums |
| `elevation.f32`, `high.f32`, `relief.f32` | Float32 rasters, row-major, row 0 at the north |
| `features.json`, `provinces.json`, `provinces.u32`, `provisional-land.u8`, `rock.u8`, `pressure.f32`, `shear.f32`, `igneous.f32`, `rock-noise.f32` | The structure records, byte for byte |
| `diagnostics.json` | Elevation and relief statistics on provisional land and water, and the shift at the provisional fraction |
| `preview.png` | Hypsometric tint with hill shading, and the coast at the provisional fraction as a line |

The continental affinity fields are not kept; their checksums remain in the recorded input manifest chain.
The shift reports what `landmask` would do at the provisional land fraction: the elevation it would move to 0 m, and how many cells would change sides.
With the foundation alone it is a fraction of a meter and no cells change.

On the full-size grid (6666 x 3333) a run takes about 7 seconds and 700 MB.

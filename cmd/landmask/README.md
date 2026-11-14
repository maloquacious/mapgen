# Landmask command

Select and save an authoritative land/ocean layout.
The input decides the mode, by the format of its manifest:

- **Terrain input** freezes the coastline from a `terrain` artifact's elevation, the step of the default pipeline.
- **Continental input** selects land by affinity from a `continents` artifact, for inspection.

```sh
go run ./cmd/terrain --in tmp/trench --out tmp/terrain
go run ./cmd/landmask --in tmp/terrain --out tmp/landmask

go run ./cmd/continents --seed 42 --out tmp/continents
go run ./cmd/landmask --in tmp/continents --land-fraction 0.325 --out tmp/landmask-affinity
```

The input is unchanged. Output must be a new directory, and an existing one is never replaced; use different directories to compare alternatives. No seed is needed: selection is deterministic.

## Flags

| Flag | Meaning |
| --- | --- |
| `--in` | Terrain or continental artifact directory; default `tmp/terrain` |
| `--out` | New output directory; default `tmp/landmask`. An existing one is never replaced |
| `--land-fraction` | Fraction of cells selected as land (non-ocean), from 0 through 1. For terrain input, default the terrain's provisional land fraction; for continental input, default 0.325 |
| `--recenter` | Continental input only: center the largest selected landmass; default false. For terrain input, recenter with `terrain --recenter` |
| `--png` | Write `preview.png`; default true |
| `--png-scale` | Sampling interval in cells for the preview; default 1 |
| `--version` | Print the generator version |

## Terrain input

This implements section 7 of the cylindrical specification, on the torus; the design is "Landmask integration" in [Terrain Generation Design.md](<../../Terrain Generation Design.md>).

1. `k = round(fraction * cells)` cells are non-ocean.
   Sea level is the elevation midway between the lowest of the `k` highest cells (ties by index) and the highest of the rest, the level `terrain` reports as its shift; it moves to 0 m.
2. The ocean is the largest eight-connected component of the other cells, grown to exactly `cells − k` through a frontier keyed by elevation then index.
   Low ground cut off from it is not ocean; the ocean takes the lowest cells on its edge instead.
3. The repair fraction, the cells where the ocean differs from the plain quantile over all cells, must be at most 0.01.
4. Ocean cells are clamped to at most −1 m and other cells to at least +1 m; no cell may move more than 500 m.
   High moves with elevation, so relief is unchanged.

A layout that fails step 3 or 4 is an error naming the measure and its value; vary the terrain or structure seed.
On the full-size seed 42 map the repair is 0.031 percent and the largest clamp 301 m, raising a rift floor cut off from the sea.

The artifact is self-contained; later stages load it with `internal/landmask.LoadTerrain`. It contains:

- `landmask.u8`: the frozen ocean mask, one byte per cell, 0 for ocean and 1 for non-ocean (land and inland depressions). Later stages take ocean membership from it, never from an elevation threshold.
- `elevation.f32`, `high.f32`, `relief.f32`: terrain's fields relative to sea level, clamped.
- The structure and trench records and `ridges.json`, copied unchanged from the terrain input. Ridge and saddle elevations stay relative to terrain's provisional datum; `sea_level_m` converts them.
- `diagnostics.json`: ocean, non-ocean, depression, and dry land cells and fractions; the quantile ocean's components and the largest; the repair; the cells raised and lowered by the clamp, with the largest and mean change; and the cells that changed sides of the provisional coast. Depressions are non-ocean cells at or below sea level before clamping, where drainage may place lakes; the rest is dry land.
- `manifest.json`: format `mapgen-landmask-terrain-v1`, input kind `terrain`, the input terrain manifest and its checksum, the grid, requested and actual fraction, the selection and its limits (`ocean-v1`), `sea_level_m`, `repair_fraction`, and checksums for every saved file.
- `preview.png`: a hypsometric tint with hill shading relative to sea level, the frozen coast drawn as a line.

Load verifies every checksum, the retained records, the count, that the ocean is one connected component, and that every cell lies on its side of the frozen coast.

On the full-size grid a run takes about 7 seconds, with a peak of 1.7 GB.

## Continental input

`--land-fraction` selects exactly `round(fraction * cell_count)` cells with the highest continental affinity. Ties are resolved by their input row-major index. This selects land and ocean, without elevations, sea level, or a connected ocean.

The artifact contains:

- `landmask.u8`: one byte per cell, 0 for ocean and 1 for land.
- `affinity.f32`, `continental.f32`, and `coast.f32`: retained continental fields, in the same coordinates as the mask.
- `manifest.json`: format `mapgen-landmask-v1` and generator version, physical grid, requested and actual land fraction, selection rule, land count, original input manifest and its checksum, optional recentering shift, and checksums for the saved fields.
- `preview.png`: blue ocean and green land, rendered from the selected mask.

Later stages can load it with `internal/landmask.Load`.

`--recenter` centers the largest selected landmass and translates the already selected mask and every retained field together. It uses the same toroidal, eight-neighbor centering convention as `continents`; the playable map remains cylindrical. Selection happens before translation so tied cells do not get reselected after moving. Shifts are relative to the input checkpoint; the original input manifest retains its previous centering history.

## Common to both

The input directory path is not recorded, so identical inputs give identical artifacts wherever they are stored.
All rasters are row-major with row zero at the north; float32 fields are little-endian. Physical grid spacing is inherited, normally 6 km; this command does not resample to player-facing resolution.

Use `--png=false` to omit the preview or `--png-scale N` to sample every Nth cell for a smaller image. At scale 1 the preview shows every cell; downsampling can hide small features. Saved rasters keep their full resolution.

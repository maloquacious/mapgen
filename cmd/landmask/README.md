# Landmask command

Select and save an authoritative land/ocean layout from a continental artifact:

```sh
go run ./cmd/continents --seed 42 --out tmp/continents
go run ./cmd/landmask --in tmp/continents --land-fraction 0.325 --out tmp/landmask
```

`--land-fraction` selects exactly `round(fraction * cell_count)` cells with the highest continental affinity. Ties are resolved by their input row-major index. The default fraction is 0.325; values from 0 through 1 are supported. No new seed is needed. This selects land and ocean, without assigning elevations or physical sea level.

The input is unchanged. Output must be a new directory, and an existing one is never replaced; use different directories to compare alternatives. It contains:

- `landmask.u8`: one byte per cell, 0 for ocean and 1 for land.
- `affinity.f32`, `continental.f32`, and `coast.f32`: retained continental fields, in the same coordinates as the mask.
- `manifest.json`: format and generator version, physical grid, requested and actual land fraction, selection rule, land count, original input manifest and its checksum (the input directory path is not recorded, so identical inputs give identical artifacts wherever they are stored), optional recentering shift, and checksums for the saved fields.
- `preview.png`: blue ocean and green land, rendered from the selected mask.

All rasters are row-major with row zero at the north. Float32 fields use little-endian encoding. The artifact is self-contained: later stages can load it with `internal/landmask.Load` without accessing the original continental directory. Physical grid spacing is inherited, normally 6 km; this command does not resample to player-facing resolution.

`--recenter` is off by default. When enabled, it centers the largest selected landmass and translates the already selected mask and every retained field together. It uses the same toroidal, eight-neighbor centering convention as `continents`; the playable map remains cylindrical. Selection happens before translation so tied cells do not get reselected after moving. Shifts are relative to the input checkpoint; the original input manifest retains its previous centering history.

Use `--png=false` to omit the preview or `--png-scale N` to sample every Nth cell for a smaller inspection image. At scale 1 the preview shows every saved cell; downsampling can hide small features. All saved rasters retain their full resolution.

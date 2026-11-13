# Trench command

Decide the subduction polarity of every active margin and island arc in a structure artifact, place ocean trenches on the subducting side, and save a trench checkpoint.

```sh
go run ./cmd/continents --seed 42 --out tmp/continents
go run ./cmd/structure --in tmp/continents --out tmp/structure
go run ./cmd/trench --in tmp/structure --png-scale 3 --out tmp/trench
go run ./cmd/terrain --in tmp/trench --out tmp/terrain
```

The design is in [Trench Generation Design.md](<../../Trench Generation Design.md>).

## Not implemented yet

- A trench fixture flag and `--config`.

## Flags

| Flag | Meaning |
| --- | --- |
| `--in` | Structure artifact directory; default `tmp/structure` |
| `--out` | New output directory; default `tmp/trench`. An existing one is never replaced |
| `--seed` | Trench seed, unsigned 64-bit. If omitted, the structure input's seed is used; the manifest records the seed and which of the two it was |
| `--recenter` | Translate the complete result, structure records included, to center the largest provisional landmass; default false |
| `--png` | Write `preview.png`; default true |
| `--png-scale` | Sampling interval in cells for `preview.png`; default 1 |
| `--version` | Print the generator version |

## What it decides

Every active margin and island arc gets a subduction record naming the side of its backbone the sinking plate lies on:

- an active margin with an elevated flank subducts on the other side (basis `elevated_flank`);
- an island arc, or a margin with no elevated flank, subducts on the side with more open water (basis `open_ocean`).

The trench follows the parent's backbone on that side, at least 20–60 km of water beyond the parent's envelope and 60–150 km beyond the last land.
Its distance from the backbone changes at most 0.5 km per km along it, so it swings around headlands.
Where the trench would cross land or another feature it is cut, and pieces shorter than 300 km are dropped and listed as omissions.
Land here is structure's simplified provisional mask, which ignores islands and lakes under 20,000 km².
Each trench is 60–120 km wide, with a floor range 1,000 m deep centered 6,500–7,500 m below the provisional datum.

The seed changes widths, distances, and depths; polarity changes with it only for an arc with exactly equal open water on both sides.

## Recentering

`--recenter` places trenches first, then moves everything by whole cells to center the largest provisional landmass.
It centers the same mask structure's `--recenter` does, so on an unrecentered structure it gives structure's own shift, and on a recentered one it moves nothing.
The manifest's `recentering` record gives this stage's shift.
When the shift is not zero, the structure records are rewritten and `recentered_records` describes them: the input structure manifest with new record checksums and the accumulated recentering.

## Output

| File | Content |
| --- | --- |
| `manifest.json` | Format `mapgen-trench-v2`, versions, the input structure manifest and its checksum, seed, grid, `trench_parameters`, `land_mask`, `recentering`, `recentered_records` when the records moved, retained records, and checksums |
| `trenches.json` | Format `mapgen-trenches-v1`: subduction records and trench records, geometry in km like structure features |
| `features.json`, `provinces.json`, `provinces.u32`, `provisional-land.u8`, `rock.u8`, `pressure.f32`, `shear.f32`, `igneous.f32`, `rock-noise.f32` | The structure records, byte for byte unless `--recenter` moved them |
| `diagnostics.json` | Subduction zones by basis, trench count and total length, omissions |
| `preview.png` | Simplified land and water, active margin and arc envelopes (orange), and trench footprints (dark blue) |

The continental fields and the structure diagnostics are not kept; their checksums remain in the recorded input manifest.
On the full-size grid (6666 x 3333) a run takes about a second.

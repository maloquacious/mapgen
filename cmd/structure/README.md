# Structure command

Attach geological features to a continental artifact and save a structure checkpoint:

```sh
go run ./cmd/continents --seed 42 --cols 96 --rows 48 --out tmp/continents
go run ./cmd/structure --in tmp/continents --features internal/structure/testdata/fixture.json --out tmp/structure
```

The design is in [Structure Generation Design.md](<../../Structure Generation Design.md>).
This covers implementation steps 1 and 2 of that design: feature records, geometry queries, artifact storage, fixture input, and provinces.

## Not implemented yet

- Automatic placement. `--features` is required; without it the command fails.
- Rock classification. The manifest records it as `absent`.
- Coast chains and coast distance, which arrive with automatic placement.
- `--recenter` and `--config`.

## Flags

| Flag | Meaning |
| --- | --- |
| `--in` | Continental artifact directory; default `tmp/continents` |
| `--out` | New output directory; default `tmp/structure`. An existing one is never replaced |
| `--features` | Feature fixture file, in the `mapgen-structure-features-v1` schema |
| `--seed` | Structure seed, unsigned 64-bit. If omitted, the continental input's seed is used; the manifest records the seed and which of the two it was. It decides province seeds, boundaries, and volcanic draws |
| `--provisional-land-fraction` | Fraction of cells treated as land while placing features; default 0.325 |
| `--png` | Write `preview.png`; default true |
| `--png-scale` | Preview sampling interval in cells; default 1 |
| `--version` | Print the generator version |

## Fixtures

A fixture is drawn for one particular continental artifact: its `domain` must equal that artifact's physical extent.
`internal/structure/testdata/fixture.json` is drawn for `continents --seed 42 --cols 96 --rows 48` (576 x 288 km).

Every feature in a fixture is required. An invalid feature is an error that names the feature's ID.
Geometry is in kilometers, `x` east and `y` south from the north-west corner, unwrapped on both axes: a belt that crosses a seam continues past the world's width or height instead of jumping back.
The first vertex of each backbone or outline lies in the base domain.

The `features.json` of any structure artifact is itself a valid fixture for the same continental artifact, and reproduces that artifact byte for byte.

## Output

- `features.json`: the authoritative feature records, in canonical form. Each lists the provinces under its envelope.
- `provisional-land.u8`: one byte per cell, 1 for provisional land. This is placement data, not the final landmask.
- `provinces.json` and `provinces.u32`: the geological provinces and the province ID of every cell, as little-endian uint32. Every cell has an owner. Islands and enclosed waters under 20,000 square km belong to the province around them.
- `affinity.f32`, `continental.f32`, `coast.f32`: retained continental fields.
- `diagnostics.json`: features placed per type, omissions, and warnings. The only warning so far is a backbone that crosses itself.
- `manifest.json`: format and generator version, the input manifest and its checksum, seed, grid, provisional selection, what is absent, and checksums for every saved file. Input and fixture paths are not recorded.
- `preview.png`: provinces in pale tones by type (shield tan, sedimentary green, volcanic pink, oceanic blue) with their boundaries, the provisional coast in grey, each feature's envelope tinted by type, and backbones as dark lines.

The world is a torus at this stage: both axes wrap, and features may cross either seam.
`internal/structure.Load` reads and verifies an artifact without the original continental directory.

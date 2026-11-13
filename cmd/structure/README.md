# Structure command

Attach geological features to a continental artifact and save a structure checkpoint:

```sh
go run ./cmd/continents --seed 42 --cols 96 --rows 48 --out tmp/continents
go run ./cmd/structure --in tmp/continents --features internal/structure/testdata/fixture.json --out tmp/structure
```

The design is in [Structure Generation Design.md](<../../Structure Generation Design.md>).
This is implementation step 1 of that design: feature records, geometry queries, artifact storage, and fixture input.

## Not implemented yet

- Automatic placement. `--features` is required; without it the command fails.
- Provinces and rock classification. The manifest records both as `absent`.
- `--recenter` and `--config`.

## Flags

| Flag | Meaning |
| --- | --- |
| `--in` | Continental artifact directory; default `tmp/continents` |
| `--out` | New output directory; default `tmp/structure`. An existing one is never replaced |
| `--features` | Feature fixture file, in the `mapgen-structure-features-v1` schema |
| `--seed` | Structure seed, unsigned 64-bit. If omitted, one is generated, printed, and recorded. It is reserved for province growth and does not affect the output yet, apart from being recorded |
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

- `features.json`: the authoritative feature records, in canonical form.
- `provisional-land.u8`: one byte per cell, 1 for provisional land. This is placement data, not the final landmask.
- `affinity.f32`, `continental.f32`, `coast.f32`: retained continental fields.
- `diagnostics.json`: features placed per type, omissions, and warnings. The only warning so far is a backbone that crosses itself.
- `manifest.json`: format and generator version, the input manifest and its checksum, seed, grid, provisional selection, what is absent, and checksums for every saved file. Input and fixture paths are not recorded.
- `preview.png`: provisional land in neutral tones, each feature's envelope tinted by type, and backbones as dark lines.

The world is a torus at this stage: both axes wrap, and features may cross either seam.
`internal/structure.Load` reads and verifies an artifact without the original continental directory.

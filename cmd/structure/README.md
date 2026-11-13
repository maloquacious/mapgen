# Structure command

Attach geological features to a continental artifact and save a structure checkpoint.
Features are placed automatically:

```sh
go run ./cmd/continents --seed 42 --out tmp/continents
go run ./cmd/structure --in tmp/continents --png-scale 3 --out tmp/structure
```

or taken from a fixture file drawn for that artifact:

```sh
go run ./cmd/continents --seed 42 --cols 96 --rows 48 --out tmp/c96
go run ./cmd/structure --in tmp/c96 --features internal/structure/testdata/fixture.json --out tmp/s96
```

The design is in [Structure Generation Design.md](<../../Structure Generation Design.md>).
This covers implementation steps 1 to 4 of that design: feature records, geometry queries, artifact storage, fixture input, provinces, the coast interpretation, automatic placement, and rock classification.

## Not implemented yet

- `--recenter` and `--config`.

## Flags

| Flag | Meaning |
| --- | --- |
| `--in` | Continental artifact directory; default `tmp/continents` |
| `--out` | New output directory; default `tmp/structure`. An existing one is never replaced |
| `--features` | Feature fixture file, in the `mapgen-structure-features-v1` schema. If omitted, features are placed automatically |
| `--seed` | Structure seed, unsigned 64-bit. If omitted, the continental input's seed is used; the manifest records the seed and which of the two it was. It decides province seeds, boundaries, volcanic draws, automatic placement, and rock noise |
| `--provisional-land-fraction` | Fraction of cells treated as land while placing features; default 0.325 |
| `--png` | Write `preview.png`, `preview-rock.png`, and `preview-coast.png`; default true |
| `--png-scale` | Sampling interval in cells for `preview.png`; default 1. `preview-coast.png` has its own scale |
| `--version` | Print the generator version |

## Automatic placement

Without `--features` the command places, in this order: collision belts, each with a plateau on one flank and a foreland basin on the other; active margins, about half with an inland basin; old orogens; rifts; island arcs; and faults.
The number of each scales with the provisional land area; on the full-size seed 42 map that is 7 collision belts, 9 active margins (8 found room), 13 old orogens, 6 rifts, 5 island arcs, and 10 faults.
Inland belts stay inland, narrow where the land narrows, and do not hook back on themselves.
An active margin follows the coast of a large landmass facing an ocean, 50 to 200 km inland; an island arc follows such a coast 200 to 600 km offshore.
Features keep 100 km clear of one another, except faults, which may cross anything.
Each request gets at most 32 candidates; one that cannot be placed is listed under `omitted` in `diagnostics.json` with the reasons, and printed.
A world too small for a belt gets none; a fault needs only 300 km of land.
The rules and parameters are in the design under "Automatic placement"; the manifest records the parameters as `placement_parameters`.

## Rock classification

Every run, in either mode, classifies every cell, land and ocean, as sedimentary, igneous, or metamorphic.
Features and provinces are first converted to three dimensionless proxies: `pressure` (compression positive, extension negative), `shear`, and `igneous` influence.
Each feature contributes at full strength through the middle of its envelope and fades to nothing a set number of half widths out; volcanic provinces, and ocean beyond a 150–300 km shelf, add igneous influence.
The layer reference's rock rule is then applied with its own rock noise, `rv`.
So collision belts have metamorphic cores, active margins, island arcs, rifts, and volcanic provinces carry igneous rock, shelves and quiet interiors are mostly sedimentary, and open ocean is igneous where it is not metamorphic.
The proxies are procedural stand-ins, not measured stress.
The rules and values are in the design under "Geology proxies and rock classification"; the manifest records them as `rock_parameters`.

On the full-size seed 42 map, provisional land is about 54 percent sedimentary, 32 percent metamorphic, and 14 percent igneous.

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
- `rock.u8`: one byte per cell, 1 sedimentary, 2 igneous, 3 metamorphic; 0 would mean unclassified and does not occur.
- `pressure.f32`, `shear.f32`, `igneous.f32`: the geology proxies, listed under `geology_fields` in the manifest.
- `rock-noise.f32`: the rock noise `rv`, saved because ore selection uses it again. `rock.u8` follows from these four fields, and `Load` checks that it does.
- `diagnostics.json`: the coast interpretation, events requested per family, features placed per type, omissions, warnings, and cells of each rock on provisional land and ocean. The only warning so far is a backbone that crosses itself.
  The coast section gives the working grid (about 50 km per cell), the greatest distance from the coast inland and offshore, and every coast chain: its length, the land and water areas it separates, and whether it circles the world.
- `manifest.json`: format and generator version, the input manifest and its checksum, seed, grid, mode (`automatic` or `fixture`), provisional selection, what is absent, and checksums for every saved file. Input and fixture paths are not recorded.
- `preview.png`: provinces in pale tones by type (shield tan, sedimentary green, volcanic pink, oceanic blue) with their boundaries, the provisional coast in grey, each feature's envelope tinted by type, and backbones as dark lines.

- `preview-rock.png`: rock types, sedimentary tan, igneous red, metamorphic blue, paler offshore, with the provisional coast. It uses the `--png-scale` of `preview.png`.

- `preview-coast.png`: distance from the provisional coast in 100 km bands, tan inland and blue offshore, darker farther away, with the smoothed coast chains as dark lines. It is drawn from the working grid, at least 1200 pixels wide.

Coast distance and coast chains are placement data. They are rebuilt from the provisional mask on each run and are not saved as rasters; islands and enclosed waters under 20,000 square km are not coastlines.

The world is a torus at this stage: both axes wrap, and features may cross either seam.
`internal/structure.Load` reads and verifies an artifact without the original continental directory.

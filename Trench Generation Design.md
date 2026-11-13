# Trench Generation Design

## Purpose and place in the pipeline

`trench` decides where ocean trenches lie, from an accepted structure checkpoint, before terrain gives them depth.

```text
continents → structure → trench → terrain → landmask → drainage → climate → ecosystems
                 └──────────────────────────────────────────────────────→ resources
```

A trench marks a subduction zone: the line where an oceanic plate sinks under an active margin or an island arc.
Which side of the margin or arc the sinking plate lies on, its polarity, is a geological decision.
It sets where the trench, the forearc, and any back-arc basin go, and later stages (rock, resources) may use it.
Structure records no polarity for island arcs, and an arc offshore usually has ocean on both sides, so terrain could only guess it.
A stage of its own makes the decision visible and acceptable before terrain runs, without reopening structure.

The stage writes two kinds of record:

- **Subduction records**, one per active margin and island arc: which side the sinking plate is on, and why.
- **Trench records**, linear features in km with a backbone, a width, and a depth target, on the subducting side and clear of land and other features.

`terrain` rasterizes trenches as it rasterizes structure features; it makes no trench decisions of its own.

## What accepting trenches means

The user accepts the polarity of every subduction zone and the course of every trench: which coasts face a trench, how far offshore it runs, and where it is interrupted.
Changing the trench seed changes trench widths, distances offshore, and depth targets.
It changes the polarity only of an island arc with equally open water on both sides.

## Command interface

```sh
go run ./cmd/trench --in tmp/structure --out tmp/trench
```

| Flag | Meaning |
| --- | --- |
| `--in` | Accepted structure artifact; default `tmp/structure` |
| `--out` | New output directory; default `tmp/trench`. An existing one is never replaced |
| `--seed` | Trench seed; if omitted, the structure seed. The manifest records the seed and whether it was explicit |
| `--png` | Write `preview.png`; default true |
| `--png-scale` | Preview sampling interval in cells; default 1 |
| `--version` | Print the generator version |

Deferred: `--recenter` (recenter at structure instead), a trench fixture flag, and `--config`.

## Inputs

The stage loads the structure records with `structure.LoadRecords`: the provisional mask, features, provinces, and rock classification.
It reads no continental fields and does not need the continental directory.
The domain is a torus; every query wraps in both directions.

Wherever this design says land, it means structure's simplified provisional mask: land and water regions smaller than structure's `province_parameters.min_area_km2` (20,000 km² by default) flipped, as structure does before building provinces and the coast.
Specks would otherwise cut every trench along a ragged coast into pieces too short to keep.
A trench may therefore pass over a small island; terrain must not lower provisional land with a trench, so the island stays.
The manifest records this as `land_mask`.

## Polarity

Only `mountain_belt` features of kind `active_margin`, and `island_arc` features, subduct.

- An active margin with `elevated_flank` `left` or `right` subducts on the other side: the range stands highest inland, away from the trench.
  Basis `elevated_flank`.
- An island arc, or an active margin with `elevated_flank` `none`, subducts on the side with more open water.
  From each backbone sample, a probe walks along the normal from the envelope edge until it meets provisional land or has gone `probe_km`.
  The side with the greater total probe length is the subducting side, so an arc's back-arc faces the nearer continent.
  An exact tie is broken by the feature's random stream.
  Basis `open_ocean`.

## Trench course

Each subduction zone draws, from its own stream, a trench width, an envelope gap, a coast gap, and a depth.

At each parent backbone sample, along the normal on the subducting side:

1. The envelope edge is at half the parent's width there.
2. The last land is the farthest provisional land cell between the envelope edge and `coast_reach_km` beyond it, if any.
3. The required offset of the trench axis is the larger of the envelope edge plus the envelope gap, and the last land plus the coast gap, plus half the trench width.

The offsets are then raised to their upper envelope with slope `max_offset_slope`: no offset changes faster than that many km per km along the backbone, and none falls below its requirement.
So a trench swings around a headland instead of kinking at it.
A vertex whose step runs backward relative to the parent's step is dropped, which removes the loops an offset curve forms inside a tight bend.

## Clipping

The trench is walked in stations one cell apart.
A station is blocked when any point of a one-cell lattice within half the trench width plus one cell of it is land, or lies in the envelope of a feature other than the parent, the parent's own parent, the parent's children, and faults.
The disks of a run's stations cover its whole envelope, including its rounded ends, and the extra cell reaches every cell whose center the envelope covers.
Trenches accepted earlier block later ones.

Each run of clear stations becomes one trench record.
A run shorter than `min_length_km` is dropped and recorded as an omission.
A subduction zone keeps its subduction record even when no trench survives.

## Records

`trenches.json`, format `mapgen-trenches-v1`:

| Field | Meaning |
| --- | --- |
| `domain` | Must match the input's domain |
| `subduction` | One record per active margin and island arc, in feature order: `feature`, `side` (`left` or `right` of the parent's backbone), `basis` |
| `trenches` | Trench records in parent order, then along the parent |

A trench record has `id` (from 1, in order), `parent` (a feature with a subduction record), `key` (a 64-bit value for terrain's streams), `side` (equal to its parent's subduction side), `backbone` (samples `{x, y, width_km}`, unwrapped, the first in the base domain, the same rules as structure backbones), and `targets.regional_elevation_m`, the trench floor range in m relative to the provisional datum, both bounds below 0.

## Random keys and streams

| Domain | Sequence | Draws |
| --- | --- | --- |
| `trench-v1/trench` | The parent feature's `key` | Width, envelope gap, coast gap, depth, tie break, in that order and always all of them; then one key per surviving trench |

## Parameters

Recorded as `trench_parameters`; calibration values, not contracts.

| Parameter | Default | Meaning |
| --- | --- | --- |
| `width_km` | 60–120 | Full trench width |
| `envelope_gap_km` | 20–60 | Least water between the parent's envelope and the trench edge |
| `coast_gap_km` | 60–150 | Least distance from the last land on the subducting side to the trench edge |
| `coast_reach_km` | 400 | How far beyond the envelope land is looked for |
| `max_offset_slope` | 0.5 | Fastest change of the offset along the backbone, km per km |
| `probe_km` | 1500 | Reach of the open-water probe |
| `depth_m` | 6500–7500 | The middle of a trench's floor range below the datum |
| `depth_spread_m` | 500 | Half the floor range |
| `min_length_km` | 300 | Shortest trench kept |

## Saved artifact

| File | Content |
| --- | --- |
| `manifest.json` | Format `mapgen-trench-v1`, versions, the input structure manifest and its checksum, seed, grid, parameters, the retained records, and checksums |
| `trenches.json` | The subduction and trench records |
| `features.json`, `provinces.json`, `provinces.u32`, `provisional-land.u8`, and the rock files | The structure records, byte for byte |
| `diagnostics.json` | Subduction zones by basis, trench count and length, omissions |
| `preview.png` | The simplified land and water, active margin and arc envelopes, and trench footprints |

The artifact is self-contained: terrain reads it without the structure directory.
The continental fields and structure diagnostics are not kept; their checksums remain in the recorded input manifest.

## Verification

- Every trench footprint cell is water in the simplified mask and outside every blocking envelope.
- Every trench vertex lies at least the envelope gap and half the trench width outside its parent's envelope.
- An active margin's trench is on the side opposite its elevated flank; an arc's is on the side with more open water.
- A blocked middle splits a trench in two; a short remainder is dropped and recorded.
- The same seed and input give the same bytes; a different seed keeps every polarity decided by an elevated flank.
- Load rejects changed records and records that do not match the retained features.

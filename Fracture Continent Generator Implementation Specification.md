# Purpose and source baseline

This document specifies the fracture based continent source for the [Cylindrical World Map Generator Specification for Go Coding Agents](https://chatgpt.com/space/page_6b49a48d7ad0819183781335b89c65a5). It separates the behavior of an existing implementation from the adaptation required by the rectangular world generator. Coding agents should implement the rectangular adaptation by default and retain a source compatibility fixture for comparison.

The reviewed repository is mdhender/hamwold, pinned to commit `b7b600ecf8d96f5dcc61487db453af39356c9570`. Primary source: [fracture.go](https://github.com/mdhender/hamwold/blob/b7b600ecf8d96f5dcc61487db453af39356c9570/internal/worldgen/fracture.go). Supporting sources are [grid.go](https://github.com/mdhender/hamwold/blob/b7b600ecf8d96f5dcc61487db453af39356c9570/internal/worldgen/grid.go), [worldgen.go](https://github.com/mdhender/hamwold/blob/b7b600ecf8d96f5dcc61487db453af39356c9570/internal/worldgen/worldgen.go), and [worldgen_test.go](https://github.com/mdhender/hamwold/blob/b7b600ecf8d96f5dcc61487db453af39356c9570/internal/worldgen/worldgen_test.go). The fracture file blob is `7bb438c781aa4299b39524266e899fb2e7076a97`.

The source identifies Michael D Henderson as copyright holder and carries SPDX identifier AGPL-3.0-or-later. Preserve the applicable attribution and license notices when copying or adapting its code. This document describes the implementation; it does not change its license.

Statements under Source behavior describe the pinned code. Statements under Rectangular integration define new project choices. This review is based on source inspection; the upstream test suite has not been executed.

# Algorithm overview

A fracture field is the signed sum of randomly positioned discs. Each disc adds either plus one or minus one everywhere it covers. Its radius is sampled from a truncated power law, making small discs much more common than large ones. Overlapping discs produce coherent regions at several scales.

This is a stochastic terrain construction method. The discs are not faults, plates, crustal blocks, or a simulation of geological fracture. The output is dimensionless continental affinity. Large values are candidates for land. They are not elevation in meters, geological age, or evidence of a mountain range.

A disc intersects any touched column in a consecutive interval of rows. Instead of adding to every row in that interval, record a positive change at its beginning and an opposite change at its end. A final prefix sum down each column reconstructs the field. A wrapped interval is represented by two intervals.

The algorithm is therefore:

1. Allocate a zero difference raster with one extra sentinel row.

2. Sample a disc center, radius, and sign.

3. Visit candidate columns, derive the covered row interval, and record its endpoint changes.

4. Repeat for all discs.

5. Prefix sum down columns to obtain the field.

Disc edges are abrupt. Large scale coherence comes from overlap and the radius distribution, not a smooth radial bump profile.

# Source behavior

## Grid geometry

The source uses flat top hexes in odd column offset coordinates, stored at `row*cols+col`. In units of row spacing:

```text
columnStep = sqrt(3)/2
centerX(col) = col * columnStep
centerY(col,row) = row + 0.5 + 0.5*(col & 1)
```

The number of columns must be even in the enclosing generator so wrapping preserves column parity. This restriction comes from staggered hex geometry, not from the fracture method itself.

During fracture construction both axes wrap. During ordinary gameplay only x wraps. The extra difference row is a sentinel for interval termination; it is not a playable row.

## Random sampling

For each disc the source consumes random values in exactly this order:

```text
cx = Float64() * cols
cy = Float64() * rows
u  = Float64()
r  = powerLawRadius(u)
delta = +1
if IntN(2) == 0: delta = -1
```

Here cx is measured in column indices, while cy and radius are measured in row spacing units. The conversion to physical x distance happens during column intersection.

For minimum radius a, maximum radius b, and falloff p, define e = 1 - p. For p different from 1:

```text
r = (a^e + u*(b^e - a^e))^(1/e)
```

This is inverse CDF sampling of density proportional to r raised to minus p on [a,b]. When p = 2 it can equivalently be written as `1 / (1/a + u*(1/b - 1/a))`.

The source directly uses the general expression and has no special handling for p = 1. A robust new implementation MUST handle p = 1 with `r = a * exp(u*log(b/a))`; if a equals b, return a directly.

For an unlimited planar disc approximation, expected covered area from a logarithmic radius band is proportional to r raised to (3 - p). Thus p = 3 gives equal expected total covered area per logarithmic radius band, while lower p gives greater influence to large radii. This explains the source comment; it is not a proof that every radius contributes equally to visual structure or field variance on a finite torus.

## Column intersection and row intervals

The source chooses inclusive candidate columns:

```text
first = floor(cx - r/columnStep)
last  = ceil(cx + r/columnStep)
if last-first >= cols:
    last = first + cols - 1

for c from first through last:
    dx = (c-cx) * columnStep
    if abs(dx) >= r: continue
    half = sqrt(r*r - dx*dx)
    col = positiveModulo(c, cols)
    stagger = 0.5 + 0.5*(col & 1)
    start = ceil(cy-half-stagger)
    count = ceil(cy+half-stagger) - start
```

The y interval is lower inclusive and upper exclusive. Exact tangency at abs(dx) = r is skipped. Replacing the second ceil with floor changes boundary inclusion and can change compatibility fixtures.

Wrap start into [0,rows). If count is nonpositive, do nothing. If count is at least rows, add the entire column once. If start+count is at most rows, add [start,start+count). Otherwise split into [start,rows) and [0,start+count-rows).

For every half open interval [from,to), apply:

```text
diff[from*cols+col] += delta
diff[to*cols+col]   -= delta
```

The source uses int32 differences and int32 prefix sums, then converts the final integer height to float32.

## Wide disc limitation

The source caps the number of candidate columns to avoid updating a wrapped column twice. For radii approaching or exceeding half the horizontal circumference, this can retain a nonnearest horizontal image of a column and give an incorrect chord length. The cap is not a general shortest distance toroidal disc construction.

Source compatibility mode preserves that behavior. The rectangular adaptation below uses shortest periodic distance and visits each physical column once. Do not silently call these two modes bit identical.

## Calling parameters and coast detail

With N = cols\*rows and R = rows, the source Generate function builds:

| Field | Disc count | Minimum radius | Maximum radius | Falloff |
| - | - | - | - | - |
| Main height | integer N/4 | 1.5 | R/3 | 2.0 |
| Ruggedness | integer N/8 | 2 | R/14 | 2.5 |

The ruggedness field is generated first using domain `map.ruggedness`, sequence 0. Main height uses domain `map.elevation`, sequence 0. Each has a separately requested random source. The combined field is `height[i] += 0.02 * rugged[i]`.

Main height alone is integer valued. The second field adds finer coast variation and breaks some ties, but does not guarantee unique values. Preserve float32 combination semantics when producing a compatibility fixture.

The upstream test helper hashes the domain using FNV 1a 64 bit and returns `rand.NewPCG(seed XOR domainHash, sequence)`. That helper is explicitly a stand in for the engine seed paths, not a complete definition of production seed derivation.

The source comments say disc counts scale so coverage remains comparable with map size. Treat that as a heuristic: expected coverage per cell depends on both count and the radius distribution. Changing rows also changes maximum radius, so the defaults do not guarantee exact resolution invariance.

# Source processing after fracture

## Preliminary sea level

The source copies eligible heights, sorts them, selects index `int(n*(1-landFraction))` clamped into range, and defines land by a strict comparison `height > sea`. Before centering all cells are eligible. Later the source excludes its ice mask from the sea level calculation.

Ties mean this threshold does not guarantee an exact land count. The source subsequently fills tiny water bodies, changing land coverage again. Do not reproduce these steps while claiming exact 30 to 35 percent coverage of the whole rectangular domain.

## Recentering the largest component

Before adding ice, the source labels connected land components with both axes wrapping and six hex neighbors. The largest component wins; ties go to the first label encountered in row major scanning.

For cells in that component, sum sine and cosine of their column angles and row angles separately. Convert each circular mean to the nearest index with math.Round. The desired shift moves that index to size/2 using positive modulo. The column shift is rounded down to an even number to preserve hex parity.

The slide moves source cell (col,row) to `((col+shiftX)%cols,(row+shiftY)%rows)`. The combined height field slides, but the source ruggedness field used later for ice does not. Continental placement is therefore not an ordinary arithmetic centroid calculation.

If a circular resultant is near zero, its angle is numerically unstable. The upstream implementation has no explicit ambiguity policy. The rectangular adaptation MUST define one.

## Later source behavior outside this module

The enclosing generator adds wandering polar ice caps, recalculates sea level outside ice, identifies the largest water body as ocean, fills water bodies smaller than four hexes, labels islands, and derives elevation and climate.

For the fracture branch, smoothed land height is converted to rank, with a latitude dependent adjustment, then mapped approximately by:

```text
elevation = 300*e + 900*e^3 + 3800*e^12
```

This conversion creates a high elevation tail from rank. It does not define structured mountain belts. The new generator MUST NOT import this conversion as its mountain model. Its mountain geometry comes from the separate geological and landform stages.

# Rectangular integration

## Canonical physical lattice

Generate continental affinity on a canonical lattice independent of the final gameplay spacing. Initial project choice: approximately 25 km square cells. Set columns = ceil(Lx/25 km), rows = ceil(Ly/25 km), then use actual spacings sx = Lx/columns and sy = Ly/rows so the lattice tiles the domain exactly.

The canonical lattice has ordinary rectangular cell centers: x = (col+0.5)sx and y = (row+0.5)sy. It has no column stagger or even column restriction. Both axes wrap during fracture construction and optional recentring. This adaptation is a new version, `fracture-rect-v1`.

Initial project defaults preserve the upstream scale at roughly 25 km row spacing:

| Field | Count | Minimum radius | Maximum radius | Falloff |
| - | - | - | - | - |
| Continental | floor(canonical cell count/4) | 37.5 km | Ly/3 | 2.0 |
| Coast detail | floor(canonical cell count/8) | 50 km | Ly/14 | 2.5 |

Combine continental plus 0.02 times coast detail. If a physical domain is too small for these ranges, require explicit radius settings or reject it. Do not silently reverse minimum and maximum. These defaults are starting choices; square sampling and physical lattice spacing make them different from upstream hex output.

The parent specification uses a roughly 50 km raster for regional continent analysis. That analysis raster is derived from this canonical source; it is not the fracture generation lattice. Holding the canonical lattice and physical domain fixed makes continent features independent of final gameplay resolution.

## Rectangular disc rasterization

Sample center x uniformly in [0,Lx), center y uniformly in [0,Ly), radius in km, and sign in the same logical order as the source. For each physical column touched by the disc, use its nearest periodic x displacement:

```text
dx = centerX(col)-cx
dx -= Lx * floor(dx/Lx + 0.5)
if abs(dx) >= radius: skip
half = sqrt(radius*radius-dx*dx)

start = ceil((cy-half)/sy - 0.5)
count = ceil((cy+half)/sy - 0.5) - start
```

Apply the same wrapped row interval rules as the source. A full height interval touches each row only once. This gives nearest image toroidal discs rather than the sum over all periodic disc images.

For radii below Lx/2, enumerate the inclusive unwrapped candidate column range from floor((cx-radius)/sx-0.5) through ceil((cx+radius)/sx-0.5), deduplicate wrapped columns if necessary, and apply the distance predicate. For radius at least Lx/2, scan each physical column exactly once. Use a reusable visitation stamp array for deduplication rather than clearing a full bool array for each disc. Alternatively retain only the all columns scan as a slow correctness oracle.

Integer interval endpoints are authoritative. Do not replace constant discs with Gaussian kernels, add radial weighting, or smooth the source without an explicit new version.

## Sampling contract

Expose a bilinear Sample(xKM,yKM) over the canonical scalar raster using center coordinates. Wrap both axes internally while generating and recentring. After recentring, the public ContinentSource used by later cylindrical stages wraps x and clamps y to the first or last source row center. The stored source raster remains the same; the access policy changes.

For bilinear interpolation compute tx = x/sx-0.5 and ty = y/sy-0.5 before taking integer floor indices. Exact periodic x queries MUST agree at x, x+Lx, and x-Lx. Opposite edge columns need not have equal values.

Keep the raw canonical field as an artifact. Optional mean and standard deviation normalization may produce a separate affinity field; it MUST preserve ordering and MUST NOT reinterpret values as meters. A flat field produces zero standardized affinity and a diagnostic rather than division by zero.

## Recentering policy

Recentering is optional and defaults on. Build a provisional mask containing exactly round(0.325\*canonical cell count) highest affinity cells, breaking ties by index. Label components using eight rectangular neighbors with both axes wrapping. Select the largest, ties by first row major label.

Compute circular means from rectangular cell center angles. Choose integer shifts that place the mean nearest the domain center. Any integer x shift is valid because there is no stagger. If resultant magnitude divided by component size is below 0.000001 for an axis, use shift zero on that axis. Record both shifts. Apply the same shifts to every retained source layer.

The provisional mask is only for placement. The final ocean mask and final land count remain the responsibility of the parent pipeline after regional terrain construction. At that transition remove north south adjacency permanently. The largest component can split when the torus is cut; label final landmasses again on the cylinder.

# Go module contract and execution order

Use a small module that has no dependency on game state, a hex library, plate tectonics, climate, rendering, or deposit generation.

```go
type FractureConfig struct {
    Columns, Rows int
    WidthKM, HeightKM float64
    Bumps int
    MinRadiusKM, MaxRadiusKM float64
    Falloff float64
}
type FractureField struct {
    Columns, Rows int
    WidthKM, HeightKM float64
    Values []float32
}
func BuildFracture(
    ctx context.Context,
    cfg FractureConfig,
    rng *rand.Rand,
) (*FractureField, error)
```

The signature describes the rectangular core. A wrapper creates continental and coast detail streams, combines them, optionally recenters, and supplies the ContinentSource adapter. Core BuildFracture does not choose sea level, classify mountains, or create an ocean mask.

Validate positive finite physical dimensions, positive raster dimensions, allocation size arithmetic, nonnegative disc count, nonnil RNG, positive finite radii with max at least min, and finite falloff. Handle p = 1 and equal radius bounds explicitly. Use a numerically stable logarithmic branch when abs(1-p) < 0.00000001. Include that threshold in the version contract.

For int32 differences require Bumps no greater than floor(MaxInt32/2), a conservative bound for intermediate endpoint accumulation. If higher counts are needed, use int64. Bound float32 exact integer output to Bumps at most 2 raised to 24, or explicitly permit quantized output in a different version. Product and byte count overflow MUST be checked before allocation.

Generate disc records in stable order or perform rasterization sequentially with one RNG. Version one SHOULD use sequential interval updates. Check context cancellation at least every 1024 discs and between prefix sum tiles.

The prefix stage can iterate rows outermost and columns innermost using one int32 running sum per column, improving contiguous access without changing exact integer results. Do not convert sums to floating point before interval accumulation is complete.

Record canonical dimensions, actual spacing, all radius parameters, count, combination coefficient, seed derivation version, PRNG algorithm, source commit, adaptation version, centering shifts, and output checksum. The seed derivation must be specified by the implementation manifest, not left to Go global random state.

# Cost and correctness checks

Let N = rows\*cols and B be the disc count. The difference raster has (rows+1)\*cols int32 entries and the output has N float32 entries, approximately 8N bytes together. Two retained source layers, a difference raster, combination buffers, and component labeling increase peak memory; reuse scratch storage where possible.

Work is O(N + sum of candidate columns over discs), bounded by O(N+B*cols). It is not O(B*N) unless an implementation incorrectly scans every cell for every disc. An optimized rectangular implementation visits approximately min(cols, 2r/sx plus a small endpoint allowance) columns per disc. Land threshold sorting and component labeling are separate costs.

Required checks:

1. Single disc fixtures cover interior, x wrap, y wrap, both wraps, tangency, empty chords, full height chords, and radii above half circumference.

2. Compare optimized difference rasterization against a direct slow oracle using the same sampled disc records and the same lower inclusive upper exclusive interval convention. Do not compare two independently sampled RNG streams.

3. Every column difference sum including the sentinel is zero. After prefix reconstruction the sentinel returns the running sum to zero.

4. Integer field values lie in [-B,B]. Zero discs give an all zero field.

5. Radius sampling returns finite values in configured bounds; verify empirical quantiles against the CDF with a fixed large sample and a declared tolerance.

6. Torus generation queries are periodic in both axes. Public cylindrical sampling wraps x and clamps y. Test them separately.

7. Canonical field and shifts are unchanged when final game spacing changes from 10 to 25 km at fixed physical extent and source configuration.

8. Centering a known component across both seams places it near the intended center and preserves all values.

9. Falloff 1, equal radii, invalid sizes, invalid numeric values, cancellation, and allocation overflow have defined behavior.

10. Compatibility fixtures match pinned upstream fracture output on staggered hex inputs, using the same PCG states, operation order, geometry, and float32 mixing. This is a separate test from rectangular adaptation.

11. Inspect thresholded continent maps and scalar gradients at the actual x seam. Do not require first and last columns to be equal.

The upstream tests include wrapping, sliding, a statistical north south seam comparison, deterministic whole world generation, and centering. These are useful references but do not establish correctness of the new square geometry or wide disc handling.

Completion means that other stages can obtain stable continental affinity through ContinentSource, the raw and combined fields are inspectable, tests distinguish upstream compatibility from rectangular behavior, and no fracture field has been mislabeled as geological mountain elevation.

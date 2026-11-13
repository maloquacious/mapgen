// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package terrain

import (
	"context"
	"fmt"
	"math"

	"github.com/maloquacious/mapgen/internal/continent"
	"github.com/maloquacious/mapgen/internal/fracture"
	"github.com/maloquacious/mapgen/internal/prng"
	"github.com/maloquacious/mapgen/internal/structure"
)

// The foundation is the surface with no features: a coastal plain rising to
// an interior base level on land, and a shelf, slope, and abyssal plain on
// water. See "The continental foundation" in the design.
const (
	FoundationAlgorithm = "foundation-v1"
	DomainProvinces     = "terrain-v1/foundation/provinces"
	DomainFields        = "terrain-v1/foundation/fields"
)

// Smooth field numbers: the sequence of each field's stream in DomainFields.
const (
	fieldVariation = iota
	fieldShelfDepth
	fieldShelfWidth
	fieldAbyssalDepth
	fieldCount
)

// FoundationParams are the calibration values behind the foundation. They
// are recorded in the manifest.
type FoundationParams struct {
	Algorithm string `json:"algorithm"`
	// RiseKM is the coast distance over which land rises from 0 m to the
	// interior base level.
	RiseKM float64 `json:"rise_km"`
	// BaseM is the range of a land province's interior base level, by
	// province type. Each province draws once from its own stream.
	BaseM map[string][2]float64 `json:"base_m"`
	// IslandBaseM is the base level of land farther than the smoothing
	// reaches from any land province: small islands in an oceanic province.
	IslandBaseM float64 `json:"island_base_m"`
	// Province base levels are averaged on a working grid near
	// WorkingSpacingKM, smoothed with a Gaussian of standard deviation
	// SmoothKM on the torus, and interpolated bilinearly to cells. The
	// kernel is cut off at three standard deviations.
	WorkingSpacingKM float64 `json:"working_spacing_km"`
	SmoothKM         float64 `json:"smooth_km"`
	// VariationShare scales the interior base level by 1 ± this share,
	// following a smooth field.
	VariationShare float64 `json:"variation_share"`
	// MinWavelengthKM bounds the smooth fields' features from below: their
	// discs have radii from half this to all of it.
	MinWavelengthKM float64 `json:"min_wavelength_km"`
	// Water: the shelf falls from 0 m to ShelfDepthM over ShelfWidthKM, the
	// slope to AbyssalDepthM over SlopeKM, then the abyssal plain is flat.
	// Each range is spanned by its own smooth field.
	ShelfDepthM   [2]float64 `json:"shelf_depth_m"`
	ShelfWidthKM  [2]float64 `json:"shelf_width_km"`
	SlopeKM       float64    `json:"slope_km"`
	AbyssalDepthM [2]float64 `json:"abyssal_depth_m"`
	// Distance names the coast distance the profiles use.
	Distance string `json:"distance"`
}

// DefaultFoundationParams returns the foundation-v1 defaults.
func DefaultFoundationParams() FoundationParams {
	return FoundationParams{
		Algorithm: FoundationAlgorithm,
		RiseKM:    250,
		BaseM: map[string][2]float64{
			structure.TypeShield:      {300, 600},
			structure.TypeSedimentary: {100, 300},
			structure.TypeVolcanic:    {400, 800},
		},
		IslandBaseM:      200,
		WorkingSpacingKM: 50, SmoothKM: 150,
		VariationShare: 0.15, MinWavelengthKM: 400,
		ShelfDepthM: [2]float64{50, 200}, ShelfWidthKM: [2]float64{50, 200},
		SlopeKM: 150, AbyssalDepthM: [2]float64{3500, 5500},
		Distance: "exact Euclidean distance from each cell center to the nearest cell of the other kind of the provisional mask, on the torus; positive on land",
	}
}

// smoothstep is 0 below a, 1 above b, and t²(3−2t) between.
func smoothstep(a, b, x float64) float64 {
	t := min(max((x-a)/(b-a), 0), 1)
	return t * t * (3 - 2*t)
}

// LandProfile is the elevation at coast distance d km on land with interior
// base level b m.
func LandProfile(p FoundationParams, b, d float64) float64 {
	return b * smoothstep(0, p.RiseKM, d)
}

// WaterProfile is the elevation at distance x km offshore, for shelf depth s
// m, shelf width w km, and abyssal depth a m. Each segment eases into the
// next, so the profile has no kinks.
func WaterProfile(p FoundationParams, s, w, a, x float64) float64 {
	return -s*smoothstep(0, w, x) - (a-s)*smoothstep(w, w+p.SlopeKM, x)
}

// lerp maps v in [0,1] into r.
func lerp(r [2]float64, v float64) float64 { return r[0] + (r[1]-r[0])*v }

// foundation is the inputs of the profiles, as smooth fields on coarse
// grids that tile the world. Sample them with SampleTorus.
type foundation struct {
	base   *fracture.FractureField             // interior base level, m, before variation
	fields [fieldCount]*fracture.FractureField // each in [0,1]
}

// buildFoundation draws the province base levels and builds the smooth
// fields.
func buildFoundation(ctx context.Context, seed uint64, p FoundationParams, g continent.Grid, pf *structure.ProvinceFile, owner []uint32) (*foundation, error) {
	base, err := baseLevels(ctx, seed, p, g, pf, owner)
	if err != nil {
		return nil, err
	}
	fd := &foundation{base: base}
	for k := range fieldCount {
		if fd.fields[k], err = smoothField(ctx, seed, p, g, k); err != nil {
			return nil, err
		}
	}
	return fd, nil
}

// baseLevels returns the smoothed interior base level on the working grid.
// Only land provinces contribute: the result at a working cell is the
// Gaussian-weighted mean of the land provinces' levels near it.
func baseLevels(ctx context.Context, seed uint64, p FoundationParams, g continent.Grid, pf *structure.ProvinceFile, owner []uint32) (*fracture.FractureField, error) {
	level := make([]float64, len(pf.Provinces)+1) // by province ID
	isLand := make([]bool, len(pf.Provinces)+1)
	for _, pr := range pf.Provinces {
		r, ok := p.BaseM[pr.Type]
		if !ok {
			if pr.Type == structure.TypeOceanic {
				continue
			}
			return nil, fmt.Errorf("foundation parameters have no base level for %s provinces", pr.Type)
		}
		level[pr.ID] = lerp(r, prng.New(seed, DomainProvinces, uint64(pr.ID)).Float64())
		isLand[pr.ID] = true
	}
	wc := min(g.Columns, max(1, int(math.Round(g.WidthKM/p.WorkingSpacingKM))))
	wr := min(g.Rows, max(1, int(math.Round(g.HeightKM/p.WorkingSpacingKM))))
	sum, weight := make([]float64, wc*wr), make([]float64, wc*wr)
	for i, o := range owner {
		if !isLand[o] {
			continue
		}
		// Integer arithmetic maps each cell to exactly one working cell.
		w := (i/g.Columns*wr/g.Rows)*wc + i%g.Columns*wc/g.Columns
		sum[w] += level[o]
		weight[w]++
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sx, sy := p.SmoothKM*float64(wc)/g.WidthKM, p.SmoothKM*float64(wr)/g.HeightKM
	sum, weight = blur(sum, wc, wr, sx, sy), blur(weight, wc, wr, sx, sy)
	out := make([]float32, wc*wr)
	for i := range out {
		// Beyond the kernel's reach of any land province the weight is
		// exactly zero.
		if weight[i] > 0 {
			out[i] = float32(sum[i] / weight[i])
		} else {
			out[i] = float32(p.IslandBaseM)
		}
	}
	return &fracture.FractureField{Columns: wc, Rows: wr, WidthKM: g.WidthKM, HeightKM: g.HeightKM, Values: out}, ctx.Err()
}

// blur convolves a cols x rows torus raster with a Gaussian of standard
// deviation sx cells across and sy cells down, cut off at three standard
// deviations. A kernel wider than the raster wraps onto it more than once.
func blur(in []float64, cols, rows int, sx, sy float64) []float64 {
	kernel := func(s float64) []float64 {
		r := int(math.Ceil(3 * s))
		k := make([]float64, 2*r+1)
		total := 0.0
		for i := range k {
			x := float64(i - r)
			k[i] = math.Exp(-x * x / (2 * s * s))
			total += k[i]
		}
		for i := range k {
			k[i] /= total
		}
		return k
	}
	mod := func(a, n int) int { return (a%n + n) % n }
	kx, ky := kernel(sx), kernel(sy)
	rx, ry := len(kx)/2, len(ky)/2
	tmp := make([]float64, len(in))
	for r := range rows {
		for c := range cols {
			v := 0.0
			for i, w := range kx {
				v += w * in[r*cols+mod(c+i-rx, cols)]
			}
			tmp[r*cols+c] = v
		}
	}
	out := make([]float64, len(in))
	for r := range rows {
		for c := range cols {
			v := 0.0
			for i, w := range ky {
				v += w * tmp[mod(r+i-ry, rows)*cols+c]
			}
			out[r*cols+c] = v
		}
	}
	return out
}

// elevation returns the foundation elevation of every cell from its signed
// coast distance.
func (fd *foundation) elevation(ctx context.Context, p FoundationParams, g continent.Grid, dist []float32) ([]float32, error) {
	out := make([]float32, len(dist))
	for i, d := range dist {
		if i%g.Columns == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		x, y := (float64(i%g.Columns)+0.5)*g.CellWidthKM, (float64(i/g.Columns)+0.5)*g.CellHeightKM
		at := func(k int) float64 { return fd.fields[k].SampleTorus(x, y) }
		var e float64
		if d > 0 {
			b := fd.base.SampleTorus(x, y) * (1 + p.VariationShare*(2*at(fieldVariation)-1))
			e = LandProfile(p, b, float64(d))
		} else {
			e = WaterProfile(p, lerp(p.ShelfDepthM, at(fieldShelfDepth)), lerp(p.ShelfWidthKM, at(fieldShelfWidth)),
				lerp(p.AbyssalDepthM, at(fieldAbyssalDepth)), -float64(d))
		}
		out[i] = float32(e)
	}
	return out, nil
}

// smoothField builds smooth field k, normalized to [0,1] by its extremes. A
// constant field is 0.5 everywhere.
func smoothField(ctx context.Context, seed uint64, p FoundationParams, g continent.Grid, k int) (*fracture.FractureField, error) {
	spacing := p.MinWavelengthKM / 2
	cc := max(2, int(math.Round(g.WidthKM/spacing)))
	rc := max(2, int(math.Round(g.HeightKM/spacing)))
	f, err := fracture.BuildFracture(ctx, fracture.FractureConfig{
		Columns: cc, Rows: rc, WidthKM: g.WidthKM, HeightKM: g.HeightKM,
		Bumps: max(1, cc*rc/2), MinRadiusKM: spacing, MaxRadiusKM: p.MinWavelengthKM, Falloff: 2,
	}, prng.New(seed, DomainFields, uint64(k)))
	if err != nil {
		return nil, fmt.Errorf("foundation field %d: %w", k, err)
	}
	lo, hi := f.Values[0], f.Values[0]
	for _, v := range f.Values {
		lo, hi = min(lo, v), max(hi, v)
	}
	for i, v := range f.Values {
		if hi > lo {
			f.Values[i] = (v - lo) / (hi - lo)
		} else {
			f.Values[i] = 0.5
		}
	}
	return f, nil
}

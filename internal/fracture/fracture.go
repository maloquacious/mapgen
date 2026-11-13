// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package fracture builds a continental affinity field as the signed sum of
// randomly placed discs on a rectangular lattice that wraps in both axes.
//
// This is the rectangular adaptation ("fracture-rect-v1") of the fracture
// generator in mdhender/hamwold, internal/worldgen/fracture.go at commit
// b7b600ecf8d96f5dcc61487db453af39356c9570 (AGPL-3.0-or-later, Michael D
// Henderson). The upstream implementation rasterized discs onto flat-top hexes
// in odd-column offset coordinates; this version uses square cells with
// centers at ((col+0.5)*sx, (row+0.5)*sy) and nearest-image periodic distance.
//
// The output is dimensionless continental affinity. Large values are land
// candidates. It is not elevation, geological age, or a mountain model.
package fracture

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"

	"github.com/maloquacious/mapgen/internal/cerrs"
)

const (
	// Version names this adaptation. It is recorded in manifests.
	Version = "fracture-rect-v1"

	// UpstreamRepository, UpstreamCommit and UpstreamFile identify the
	// implementation this package adapts.
	UpstreamRepository = "https://github.com/mdhender/hamwold"
	UpstreamCommit     = "b7b600ecf8d96f5dcc61487db453af39356c9570"
	UpstreamFile       = "internal/worldgen/fracture.go"

	// FalloffLogEpsilon is the distance from a falloff of 1 below which radius
	// sampling uses the logarithmic branch. It is part of the version contract.
	FalloffLogEpsilon = 1e-8

	// MaxBumps bounds the disc count so every field value is an integer that
	// float32 represents exactly. It is well below MaxInt32/2, the bound for
	// int32 difference accumulation.
	MaxBumps = 1 << 24
)

const (
	ErrInvalidDimensions = cerrs.Error("invalid raster dimensions")
	ErrInvalidExtent     = cerrs.Error("invalid physical extent")
	ErrInvalidBumps      = cerrs.Error("invalid disc count")
	ErrInvalidRadius     = cerrs.Error("invalid radius bounds")
	ErrInvalidFalloff    = cerrs.Error("invalid falloff")
	ErrNilRNG            = cerrs.Error("nil random source")
	ErrTooLarge          = cerrs.Error("raster too large")
	ErrMismatchedFields  = cerrs.Error("fields have different shapes")
	ErrSentinel          = cerrs.Error("difference raster did not return to zero")
)

// FractureConfig describes one fracture field.
type FractureConfig struct {
	Columns, Rows            int
	WidthKM, HeightKM        float64
	Bumps                    int
	MinRadiusKM, MaxRadiusKM float64
	Falloff                  float64
}

// FractureField is a row-major raster: Values[row*Columns+col]. Row 0 is
// north. Cell centers are at ((col+0.5)*WidthKM/Columns, (row+0.5)*HeightKM/Rows).
type FractureField struct {
	Columns, Rows     int
	WidthKM, HeightKM float64
	Values            []float32
}

// Disc is one sampled disc. X and Y are its center in km, R its radius in km,
// and Delta is +1 or -1.
type Disc struct {
	X, Y, R float64
	Delta   int32
}

// Validate reports whether cfg can be built.
func (cfg FractureConfig) Validate() error {
	if cfg.Columns <= 0 || cfg.Rows <= 0 {
		return fmt.Errorf("%w: %d x %d", ErrInvalidDimensions, cfg.Columns, cfg.Rows)
	}
	// The difference raster has (Rows+1)*Columns int32 entries; check the
	// product and its byte count before anything is allocated.
	if cfg.Rows >= math.MaxInt-1 || cfg.Columns > math.MaxInt/4/(cfg.Rows+1) {
		return fmt.Errorf("%w: %d x %d", ErrTooLarge, cfg.Columns, cfg.Rows)
	}
	if !positiveFinite(cfg.WidthKM) || !positiveFinite(cfg.HeightKM) {
		return fmt.Errorf("%w: %v x %v km", ErrInvalidExtent, cfg.WidthKM, cfg.HeightKM)
	}
	if cfg.Bumps < 0 || cfg.Bumps > MaxBumps {
		return fmt.Errorf("%w: %d not in [0, %d]", ErrInvalidBumps, cfg.Bumps, MaxBumps)
	}
	if !positiveFinite(cfg.MinRadiusKM) || !positiveFinite(cfg.MaxRadiusKM) || cfg.MaxRadiusKM < cfg.MinRadiusKM {
		return fmt.Errorf("%w: [%v, %v] km", ErrInvalidRadius, cfg.MinRadiusKM, cfg.MaxRadiusKM)
	}
	if math.IsNaN(cfg.Falloff) || math.IsInf(cfg.Falloff, 0) {
		return fmt.Errorf("%w: %v", ErrInvalidFalloff, cfg.Falloff)
	}
	return nil
}

func positiveFinite(v float64) bool {
	return v > 0 && !math.IsInf(v, 0) && !math.IsNaN(v)
}

// SampleRadius maps u in [0,1) to a radius in [a,b] with density proportional
// to r^-p, by inverse CDF sampling.
func SampleRadius(u, a, b, p float64) float64 {
	if a == b {
		return a
	}
	var r float64
	if e := 1 - p; math.Abs(e) < FalloffLogEpsilon {
		r = a * math.Exp(u*math.Log(b/a))
	} else {
		ae, be := math.Pow(a, e), math.Pow(b, e)
		r = math.Pow(ae+u*(be-ae), 1/e)
	}
	// Rounding can step just outside the bounds.
	return min(max(r, a), b)
}

// SampleDisc draws one disc, consuming the stream in the upstream order:
// center x, center y, radius variate, sign.
func SampleDisc(rng *rand.Rand, cfg FractureConfig) Disc {
	x := rng.Float64() * cfg.WidthKM
	y := rng.Float64() * cfg.HeightKM
	u := rng.Float64()
	r := SampleRadius(u, cfg.MinRadiusKM, cfg.MaxRadiusKM, cfg.Falloff)
	delta := int32(1)
	if rng.IntN(2) == 0 {
		delta = -1
	}
	return Disc{X: x, Y: y, R: r, Delta: delta}
}

// BuildFracture samples cfg.Bumps discs from rng and returns their sum.
func BuildFracture(ctx context.Context, cfg FractureConfig, rng *rand.Rand) (*FractureField, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if rng == nil {
		return nil, ErrNilRNG
	}
	return build(ctx, cfg, cfg.Bumps, func(int) Disc { return SampleDisc(rng, cfg) })
}

// BuildFromDiscs rasterizes explicit discs. It is used for fixtures and for
// comparing against a direct oracle with identical disc records.
func BuildFromDiscs(ctx context.Context, cfg FractureConfig, discs []Disc) (*FractureField, error) {
	cfg.Bumps = len(discs)
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return build(ctx, cfg, len(discs), func(i int) Disc { return discs[i] })
}

func build(ctx context.Context, cfg FractureConfig, n int, next func(int) Disc) (*FractureField, error) {
	r := newRasterizer(cfg)
	for i := range n {
		if i%1024 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		r.add(next(i))
	}
	return r.field(ctx)
}

// rasterizer records each disc as interval endpoint changes in a difference
// raster with one sentinel row, then reconstructs the field by prefix sums
// down each column.
type rasterizer struct {
	cfg        FractureConfig
	cols, rows int
	sx, sy     float64
	diff       []int32
	stamp      []uint32 // column visitation stamps, to visit each column once per disc
	epoch      uint32
}

func newRasterizer(cfg FractureConfig) *rasterizer {
	return &rasterizer{
		cfg:   cfg,
		cols:  cfg.Columns,
		rows:  cfg.Rows,
		sx:    cfg.WidthKM / float64(cfg.Columns),
		sy:    cfg.HeightKM / float64(cfg.Rows),
		diff:  make([]int32, (cfg.Rows+1)*cfg.Columns),
		stamp: make([]uint32, cfg.Columns),
	}
}

func (r *rasterizer) add(d Disc) {
	if d.R >= r.cfg.WidthKM/2 {
		for col := range r.cols {
			r.column(col, d)
		}
		return
	}
	r.epoch++
	if r.epoch == 0 {
		clear(r.stamp)
		r.epoch = 1
	}
	first := int(math.Floor((d.X-d.R)/r.sx - 0.5))
	last := int(math.Ceil((d.X+d.R)/r.sx - 0.5))
	for c := first; c <= last; c++ {
		col := mod(c, r.cols)
		if r.stamp[col] == r.epoch {
			continue
		}
		r.stamp[col] = r.epoch
		r.column(col, d)
	}
}

// column adds the disc's chord through col. Rows are covered when their
// center lies in [cy-half, cy+half): lower inclusive, upper exclusive.
func (r *rasterizer) column(col int, d Disc) {
	lx := r.cfg.WidthKM
	dx := (float64(col)+0.5)*r.sx - d.X
	dx -= lx * math.Floor(dx/lx+0.5)
	if math.Abs(dx) >= d.R {
		return
	}
	half := math.Sqrt(d.R*d.R - dx*dx)
	start := int(math.Ceil((d.Y-half)/r.sy - 0.5))
	count := int(math.Ceil((d.Y+half)/r.sy-0.5)) - start
	switch {
	case count <= 0:
	case count >= r.rows:
		r.interval(col, 0, r.rows, d.Delta)
	default:
		start = mod(start, r.rows)
		if start+count <= r.rows {
			r.interval(col, start, start+count, d.Delta)
		} else {
			r.interval(col, start, r.rows, d.Delta)
			r.interval(col, 0, start+count-r.rows, d.Delta)
		}
	}
}

func (r *rasterizer) interval(col, from, to int, delta int32) {
	r.diff[from*r.cols+col] += delta
	r.diff[to*r.cols+col] -= delta
}

// field prefix-sums the difference raster, rows outermost for contiguous
// access, and converts to float32 only after accumulation is complete.
func (r *rasterizer) field(ctx context.Context) (*FractureField, error) {
	values := make([]float32, r.rows*r.cols)
	run := make([]int32, r.cols)
	for row := range r.rows {
		if row%256 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		base := row * r.cols
		for col := range r.cols {
			run[col] += r.diff[base+col]
			values[base+col] = float32(run[col])
		}
	}
	base := r.rows * r.cols
	for col := range r.cols {
		if run[col]+r.diff[base+col] != 0 {
			return nil, fmt.Errorf("%w: column %d", ErrSentinel, col)
		}
	}
	return &FractureField{
		Columns:  r.cols,
		Rows:     r.rows,
		WidthKM:  r.cfg.WidthKM,
		HeightKM: r.cfg.HeightKM,
		Values:   values,
	}, nil
}

// Combine returns base + weight*detail computed in float32, matching the
// upstream mixing of the elevation and ruggedness fields.
func Combine(base, detail *FractureField, weight float32) (*FractureField, error) {
	if !base.sameShape(detail) {
		return nil, ErrMismatchedFields
	}
	out := base.clone()
	for i, v := range detail.Values {
		out.Values[i] += weight * v
	}
	return out, nil
}

func (f *FractureField) sameShape(g *FractureField) bool {
	return f.Columns == g.Columns && f.Rows == g.Rows && f.WidthKM == g.WidthKM && f.HeightKM == g.HeightKM
}

func (f *FractureField) clone() *FractureField {
	c := *f
	c.Values = slices.Clone(f.Values)
	return &c
}

// CellWidthKM returns the actual column spacing.
func (f *FractureField) CellWidthKM() float64 { return f.WidthKM / float64(f.Columns) }

// CellHeightKM returns the actual row spacing.
func (f *FractureField) CellHeightKM() float64 { return f.HeightKM / float64(f.Rows) }

func mod(a, n int) int {
	a %= n
	if a < 0 {
		a += n
	}
	return a
}

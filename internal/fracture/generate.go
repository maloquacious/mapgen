// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package fracture

import (
	"context"
	"fmt"
	"math"

	"github.com/maloquacious/mapgen/internal/prng"
)

const (
	// DefaultCoastWeight mixes coast detail into the continental field.
	DefaultCoastWeight float32 = 0.02

	// Random stream domains. They are part of the output contract.
	DomainContinental = Version + "/continental"
	DomainCoast       = Version + "/coast"
)

// Layer holds the disc parameters for one field. Radii are in cells.
type Layer struct {
	Bumps                          int
	MinRadiusCells, MaxRadiusCells float64
	Falloff                        float64
}

// Params configures a complete continental affinity build on a raster of
// Columns x Rows square cells of CellKM.
type Params struct {
	Seed          uint64
	Columns, Rows int
	CellKM        float64
	Continental   Layer
	Coast         Layer
	CoastWeight   float32
	Recenter      bool
	// ProvisionalLandFraction sizes the mask used to choose the recentering
	// shift. It does not set sea level.
	ProvisionalLandFraction float64
}

// WidthKM returns the east-west extent.
func (p Params) WidthKM() float64 { return float64(p.Columns) * p.CellKM }

// HeightKM returns the north-south extent.
func (p Params) HeightKM() float64 { return float64(p.Rows) * p.CellKM }

// DefaultParams returns the fracture-rect-v1 defaults, which follow the
// upstream generator in cell units. With N = columns*rows:
//
//	continental: floor(N/4) discs, radius 1.5 to rows/3 cells, falloff 2.0
//	coast:       floor(N/8) discs, radius 2 to rows/14 cells, falloff 2.5
//
// A raster too short for these ranges is rejected; set radii explicitly.
func DefaultParams(seed uint64, columns, rows int, cellKM float64) (Params, error) {
	if columns <= 0 || rows <= 0 || columns > math.MaxInt/rows {
		return Params{}, fmt.Errorf("%w: %d x %d", ErrInvalidDimensions, columns, rows)
	}
	if !positiveFinite(cellKM) {
		return Params{}, fmt.Errorf("%w: cell %v km", ErrInvalidExtent, cellKM)
	}
	n := columns * rows
	p := Params{
		Seed:    seed,
		Columns: columns, Rows: rows,
		CellKM: cellKM,
		Continental: Layer{
			Bumps:          n / 4,
			MinRadiusCells: 1.5,
			MaxRadiusCells: float64(rows) / 3,
			Falloff:        2.0,
		},
		Coast: Layer{
			Bumps:          n / 8,
			MinRadiusCells: 2,
			MaxRadiusCells: float64(rows) / 14,
			Falloff:        2.5,
		},
		CoastWeight:             DefaultCoastWeight,
		Recenter:                true,
		ProvisionalLandFraction: DefaultProvisionalLandFraction,
	}
	for _, l := range []struct {
		name string
		Layer
	}{{"continental", p.Continental}, {"coast", p.Coast}} {
		if l.Bumps > MaxBumps {
			return Params{}, fmt.Errorf("%w: %s default %d exceeds %d; set bumps explicitly", ErrInvalidBumps, l.name, l.Bumps, MaxBumps)
		}
		if l.MaxRadiusCells < l.MinRadiusCells {
			return Params{}, fmt.Errorf("%w: %s default range [%v, %v] cells does not fit %d rows; set radii explicitly",
				ErrInvalidRadius, l.name, l.MinRadiusCells, l.MaxRadiusCells, rows)
		}
	}
	return p, nil
}

func (p Params) config(l Layer) FractureConfig {
	return FractureConfig{
		Columns: p.Columns, Rows: p.Rows,
		WidthKM: p.WidthKM(), HeightKM: p.HeightKM(),
		Bumps:       l.Bumps,
		MinRadiusKM: l.MinRadiusCells * p.CellKM,
		MaxRadiusKM: l.MaxRadiusCells * p.CellKM,
		Falloff:     l.Falloff,
	}
}

// Result holds the retained layers. All three share the same recentering.
type Result struct {
	Params      Params
	Continental *FractureField // raw integer-valued continental field
	Coast       *FractureField // raw integer-valued coast detail field
	Affinity    *FractureField // Continental + CoastWeight*Coast
	Centering   Centering
}

// Generate builds both layers from independent streams, combines them, and
// optionally recenters every layer by the same shift.
func Generate(ctx context.Context, p Params) (*Result, error) {
	coast, err := BuildFracture(ctx, p.config(p.Coast), prng.New(p.Seed, DomainCoast, 0))
	if err != nil {
		return nil, fmt.Errorf("coast: %w", err)
	}
	cont, err := BuildFracture(ctx, p.config(p.Continental), prng.New(p.Seed, DomainContinental, 0))
	if err != nil {
		return nil, fmt.Errorf("continental: %w", err)
	}
	aff, err := Combine(cont, coast, p.CoastWeight)
	if err != nil {
		return nil, err
	}
	res := &Result{Params: p, Continental: cont, Coast: coast, Affinity: aff}
	if !p.Recenter {
		return res, nil
	}
	c, err := FindCentering(ctx, aff, p.ProvisionalLandFraction)
	if err != nil {
		return nil, fmt.Errorf("recenter: %w", err)
	}
	c.Applied = true
	res.Centering = c
	if c.X != 0 || c.Y != 0 {
		res.Continental = cont.Slide(c.X, c.Y)
		res.Coast = coast.Slide(c.X, c.Y)
		res.Affinity = aff.Slide(c.X, c.Y)
	}
	return res, nil
}

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
	// ReferenceSpacingKM is the canonical lattice spacing that sets default
	// disc density. Disc counts derive from the physical area, not from the
	// output raster, so the continental layout keeps the same statistics at
	// any output spacing.
	ReferenceSpacingKM = 25.0

	// DefaultCoastWeight mixes coast detail into the continental field.
	DefaultCoastWeight float32 = 0.02

	// Random stream domains. They are part of the output contract.
	DomainContinental = Version + "/continental"
	DomainCoast       = Version + "/coast"
)

// Layer holds the disc parameters for one field.
type Layer struct {
	Bumps                    int
	MinRadiusKM, MaxRadiusKM float64
	Falloff                  float64
}

// Params configures a complete continental affinity build.
type Params struct {
	Seed              uint64
	Columns, Rows     int
	WidthKM, HeightKM float64
	Continental       Layer
	Coast             Layer
	CoastWeight       float32
	Recenter          bool
	// ProvisionalLandFraction sizes the mask used to choose the recentering
	// shift. It does not set sea level.
	ProvisionalLandFraction float64
}

// DefaultParams returns the fracture-rect-v1 defaults for a raster of
// columns x rows square cells of cellKM:
//
//	continental: floor(C/4) discs, radius 37.5 km to Ly/3, falloff 2.0
//	coast:       floor(C/8) discs, radius 50 km to Ly/14, falloff 2.5
//
// where C = ceil(Lx/25 km) * ceil(Ly/25 km) is the canonical cell count.
// A domain too short for these ranges is rejected; set radii explicitly.
func DefaultParams(seed uint64, columns, rows int, cellKM float64) (Params, error) {
	if columns <= 0 || rows <= 0 {
		return Params{}, fmt.Errorf("%w: %d x %d", ErrInvalidDimensions, columns, rows)
	}
	if !positiveFinite(cellKM) {
		return Params{}, fmt.Errorf("%w: cell %v km", ErrInvalidExtent, cellKM)
	}
	lx, ly := float64(columns)*cellKM, float64(rows)*cellKM
	canonical := math.Ceil(lx/ReferenceSpacingKM) * math.Ceil(ly/ReferenceSpacingKM)
	if canonical/4 > MaxBumps {
		return Params{}, fmt.Errorf("%w: domain needs %v discs", ErrInvalidBumps, canonical/4)
	}
	p := Params{
		Seed:    seed,
		Columns: columns, Rows: rows,
		WidthKM: lx, HeightKM: ly,
		Continental: Layer{
			Bumps:       int(canonical / 4),
			MinRadiusKM: 1.5 * ReferenceSpacingKM,
			MaxRadiusKM: ly / 3,
			Falloff:     2.0,
		},
		Coast: Layer{
			Bumps:       int(canonical / 8),
			MinRadiusKM: 2 * ReferenceSpacingKM,
			MaxRadiusKM: ly / 14,
			Falloff:     2.5,
		},
		CoastWeight:             DefaultCoastWeight,
		Recenter:                true,
		ProvisionalLandFraction: DefaultProvisionalLandFraction,
	}
	for _, l := range []struct {
		name string
		Layer
	}{{"continental", p.Continental}, {"coast", p.Coast}} {
		if l.MaxRadiusKM < l.MinRadiusKM {
			return Params{}, fmt.Errorf("%w: %s default range [%v, %v] km does not fit a %v km tall domain; set radii explicitly",
				ErrInvalidRadius, l.name, l.MinRadiusKM, l.MaxRadiusKM, ly)
		}
	}
	return p, nil
}

func (p Params) config(l Layer) FractureConfig {
	return FractureConfig{
		Columns: p.Columns, Rows: p.Rows,
		WidthKM: p.WidthKM, HeightKM: p.HeightKM,
		Bumps:       l.Bumps,
		MinRadiusKM: l.MinRadiusKM, MaxRadiusKM: l.MaxRadiusKM,
		Falloff: l.Falloff,
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

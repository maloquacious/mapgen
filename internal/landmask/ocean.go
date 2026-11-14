// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package landmask

import (
	"context"
	"fmt"
	"math"

	"github.com/maloquacious/mapgen/internal/cerrs"
	"github.com/maloquacious/mapgen/internal/fracture"
	"github.com/maloquacious/mapgen/internal/terrain"
)

// Layout failures of the terrain-input mode. Each names the measure that
// failed; the user varies the terrain or structure seed.
const (
	ErrRepairFraction = cerrs.Error("ocean connectivity repair fraction exceeds the limit")
	ErrClamp          = cerrs.Error("clamping to the frozen coast moves a cell too far")
)

// OceanAlgorithm names the terrain-input selection, section 7 of the
// cylindrical specification on a torus.
const OceanAlgorithm = "ocean-v1"

// OceanParams are the limits of the terrain-input selection. They are
// recorded in the manifest.
type OceanParams struct {
	Algorithm string `json:"algorithm"`
	// MaxRepairFraction bounds the symmetric difference between the
	// connected ocean and the quantile mask, as a fraction of all cells.
	MaxRepairFraction float64 `json:"max_repair_fraction"`
	// Ocean cells are clamped to at most OceanMaxM and other cells to at
	// least LandMinM; no cell may move more than MaxClampM.
	OceanMaxM float64 `json:"ocean_max_m"`
	LandMinM  float64 `json:"land_min_m"`
	MaxClampM float64 `json:"max_clamp_m"`
}

// DefaultOceanParams returns the ocean-v1 defaults.
func DefaultOceanParams() OceanParams {
	return OceanParams{Algorithm: OceanAlgorithm, MaxRepairFraction: 0.01, OceanMaxM: -1, LandMinM: 1, MaxClampM: 500}
}

// Clamp summarizes the elevation changes the frozen coast requires.
type Clamp struct {
	// RaisedCells are non-ocean cells raised to LandMinM, by at most
	// MaxRaiseM and MeanRaiseM on average.
	RaisedCells int     `json:"raised_cells"`
	MaxRaiseM   float64 `json:"max_raise_m"`
	MeanRaiseM  float64 `json:"mean_raise_m"`
	// LoweredCells are ocean cells lowered to OceanMaxM.
	LoweredCells int     `json:"lowered_cells"`
	MaxLowerM    float64 `json:"max_lower_m"`
	MeanLowerM   float64 `json:"mean_lower_m"`
}

// selection is the frozen ocean and the clamped surface.
type selection struct {
	land                  []byte // 1 for non-ocean, 0 for ocean
	level                 float64
	components, component int // quantile ocean components and the largest one's cells
	repair                int // symmetric difference with the quantile mask
	depressions           int // non-ocean cells at or below sea level before clamping
	clamp                 Clamp
}

// selectOcean freezes the ocean from elev, relative to the provisional
// datum, leaving k non-ocean cells, and shifts and clamps elev and high in
// place. It returns an error naming the measure when the layout fails.
func selectOcean(ctx context.Context, p OceanParams, elev, high []float32, cols, rows, k int) (*selection, error) {
	n := cols * rows
	if len(elev) != n || len(high) != n || k < 0 || k > n {
		return nil, fmt.Errorf("select ocean: invalid size")
	}
	o, err := fracture.ConnectedOcean(ctx, elev, cols, rows, k)
	if err != nil {
		return nil, err
	}
	s := &selection{land: make([]byte, n), level: terrain.QuantileLevel(elev, o.Quantile),
		components: o.Components, component: o.Largest, repair: o.Repair}
	for i, l := range o.Land {
		if l {
			s.land[i] = 1
		}
	}
	if fr := float64(s.repair) / float64(n); fr > p.MaxRepairFraction {
		return nil, fmt.Errorf("%w: %d cells (%.4f) against %.4f", ErrRepairFraction, s.repair, fr, p.MaxRepairFraction)
	}

	// Shift the level to 0 m, then clamp; high moves with elevation, so
	// relief is unchanged.
	var over int
	var worst, raise, lower float64
	cl := &s.clamp
	for i := range n {
		e := float64(elev[i]) - s.level
		t := e
		if s.land[i] == 1 {
			if e <= 0 {
				s.depressions++
			}
			t = max(e, p.LandMinM)
		} else {
			t = min(e, p.OceanMaxM)
		}
		d := t - e
		switch {
		case d > 0:
			cl.RaisedCells++
			cl.MaxRaiseM = max(cl.MaxRaiseM, d)
			raise += d
		case d < 0:
			cl.LoweredCells++
			cl.MaxLowerM = max(cl.MaxLowerM, -d)
			lower -= d
		}
		if math.Abs(d) > p.MaxClampM {
			over++
			worst = max(worst, math.Abs(d))
		}
		elev[i] = float32(t)
		high[i] = max(float32(float64(high[i])-s.level+d), elev[i])
	}
	if over > 0 {
		return nil, fmt.Errorf("%w: %d cells, up to %.1f m against %.1f m", ErrClamp, over, worst, p.MaxClampM)
	}
	if cl.RaisedCells > 0 {
		cl.MeanRaiseM = raise / float64(cl.RaisedCells)
	}
	if cl.LoweredCells > 0 {
		cl.MeanLowerM = lower / float64(cl.LoweredCells)
	}
	return s, ctx.Err()
}

// connected reports whether the ocean cells (land 0) form at most one
// eight-connected component on the torus.
func connected(land []byte, cols, rows int) bool {
	return fracture.OceanConnected(func(i int) bool { return land[i] != 0 }, cols, rows)
}

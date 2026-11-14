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
)

// Residual detail: variation the structure does not specify. See "Residual
// detail" in the design.
const (
	ResidualAlgorithm = "residual-v1"
	DomainResidual    = "terrain-v1/residual"
	DomainPlateau     = "terrain-v1/plateau"
)

// ResidualParams are the calibration values behind residual detail. They
// are recorded in the manifest.
type ResidualParams struct {
	Algorithm string `json:"algorithm"`
	// Octave k has wavelength WavelengthKM / 2^k and weight Persistence^k.
	// It is
	// built on a grid of a quarter of its wavelength, and an octave whose
	// grid would be finer than half of MinWavelengthCells cells is left
	// out, so the noise has no wavelength under MinWavelengthCells cells.
	WavelengthKM       float64 `json:"wavelength_km"`
	Octaves            int     `json:"octaves"`
	Persistence        float64 `json:"persistence"`
	MinWavelengthCells float64 `json:"min_wavelength_cells"`
	// A cell's amplitude is Share of its structured relief, at least
	// FloorM. The floor is at most DatumShare of the cell's distance from
	// the datum, so it never takes a quiet cell across the provisional
	// coast; only relief, which features make, can.
	Share      float64 `json:"share"`
	FloorM     float64 `json:"floor_m"`
	DatumShare float64 `json:"datum_share"`
	// Within a belt the noise is the mean of GrainSamples samples along the
	// backbone direction over ±GrainKM, blended with the plain noise by the
	// belt's kernel weight, so it reads as grain along the belt. The belt's
	// maturity times its weight lowers the amplitude by up to
	// MaturityDamping.
	GrainKM         float64 `json:"grain_km"`
	GrainSamples    int     `json:"grain_samples"`
	MaturityDamping float64 `json:"maturity_damping"`
	// Shorter wavelengths are not added to the elevation: they add
	// ShortShare of the amplitude to the relief and half of that to the
	// high.
	ShortShare float64 `json:"short_share"`
}

// DefaultResidualParams returns the residual-v1 defaults.
func DefaultResidualParams() ResidualParams {
	return ResidualParams{
		Algorithm:    ResidualAlgorithm,
		WavelengthKM: 192, Octaves: 3, Persistence: 0.7, MinWavelengthCells: 4,
		Share: 0.1, FloorM: 20, DatumShare: 0.5,
		GrainKM: 150, GrainSamples: 13, MaturityDamping: 0.5,
		ShortShare: 0.5,
	}
}

// noise is a sum of normalized fracture fields, in [-1, 1].
type noise struct {
	fields  []*fracture.FractureField
	weights []float64
}

func (n *noise) at(x, y float64) float64 {
	v, total := 0.0, 0.0
	for k, f := range n.fields {
		v += n.weights[k] * f.SampleTorus(x, y)
		total += n.weights[k]
	}
	return v / total
}

// noiseField builds a fracture field with discs of a quarter to a half of
// wl km on a grid of a quarter of wl, normalized to [-1, 1] by its
// extremes.
func noiseField(ctx context.Context, g continent.Grid, wl float64, seed uint64, domain string, seq uint64) (*fracture.FractureField, error) {
	spacing := wl / 4
	cc := max(2, int(math.Round(g.WidthKM/spacing)))
	rc := max(2, int(math.Round(g.HeightKM/spacing)))
	f, err := fracture.BuildFracture(ctx, fracture.FractureConfig{
		Columns: cc, Rows: rc, WidthKM: g.WidthKM, HeightKM: g.HeightKM,
		Bumps: max(1, cc*rc/2), MinRadiusKM: spacing, MaxRadiusKM: 2 * spacing, Falloff: 2,
	}, prng.New(seed, domain, seq))
	if err != nil {
		return nil, fmt.Errorf("%s field %d: %w", domain, seq, err)
	}
	lo, hi := f.Values[0], f.Values[0]
	for _, v := range f.Values {
		lo, hi = min(lo, v), max(hi, v)
	}
	for i, v := range f.Values {
		if hi > lo {
			f.Values[i] = 2*(v-lo)/(hi-lo) - 1
		} else {
			f.Values[i] = 0
		}
	}
	return f, nil
}

// residualNoise builds the residual octaves the grid can carry.
func residualNoise(ctx context.Context, seed uint64, p ResidualParams, g continent.Grid) (*noise, error) {
	n := &noise{}
	cell := max(g.CellWidthKM, g.CellHeightKM)
	for k := range p.Octaves {
		wl := p.WavelengthKM / math.Ldexp(1, k)
		if wl/4 < p.MinWavelengthCells/2*cell {
			break
		}
		f, err := noiseField(ctx, g, wl, seed, DomainResidual, uint64(k))
		if err != nil {
			return nil, err
		}
		n.fields, n.weights = append(n.fields, f), append(n.weights, math.Pow(p.Persistence, float64(k)))
	}
	if len(n.fields) == 0 {
		return nil, fmt.Errorf("the grid is too coarse for any residual octave")
	}
	return n, nil
}

// grain is the belt that most strongly affects a cell's residual: its
// kernel weight, the backbone direction, and its maturity.
type grain struct {
	w, tx, ty, maturity float64
}

// residual returns the residual for a cell centered at (x, y) km with
// structured elevation elev and relief, and the unresolved roughness it
// adds to the relief.
func (n *noise) residual(p ResidualParams, x, y, elev, relief float64, gr grain) (float64, float64) {
	v := n.at(x, y)
	if gr.w > 0 {
		g := 0.0
		m := p.GrainSamples
		for j := range m {
			t := p.GrainKM * (2*float64(j)/float64(m-1) - 1)
			g += n.at(x+gr.tx*t, y+gr.ty*t)
		}
		v = (1-gr.w)*v + gr.w*g/float64(m)
	}
	amp := max(min(p.FloorM, p.DatumShare*math.Abs(elev)), p.Share*relief) * (1 - p.MaturityDamping*gr.maturity*gr.w)
	return amp * v, p.ShortShare * amp
}

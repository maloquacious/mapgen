// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package fracture

import "math"

// At returns the value of cell (col,row) with both indices wrapped.
func (f *FractureField) At(col, row int) float32 {
	return f.Values[mod(row, f.Rows)*f.Columns+mod(col, f.Columns)]
}

// SampleTorus interpolates bilinearly between cell centers, wrapping both
// axes. Use it during generation, before the torus is cut.
func (f *FractureField) SampleTorus(xKM, yKM float64) float64 {
	return f.sample(xKM, yKM, true)
}

// ContinentSource is the public access policy for later cylindrical stages:
// x wraps, y clamps to the first and last row centers.
type ContinentSource struct {
	f *FractureField
}

// NewContinentSource wraps f. The field is not copied.
func NewContinentSource(f *FractureField) *ContinentSource {
	return &ContinentSource{f: f}
}

// Field returns the underlying raster.
func (s *ContinentSource) Field() *FractureField { return s.f }

// Sample returns continental affinity at (xKM, yKM).
func (s *ContinentSource) Sample(xKM, yKM float64) float64 {
	return s.f.sample(xKM, yKM, false)
}

func (f *FractureField) sample(x, y float64, wrapY bool) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) || math.IsNaN(y) || math.IsInf(y, 0) {
		return math.NaN()
	}
	cols, rows := f.Columns, f.Rows
	// Reduce x into [0, Lx) first so x, x+Lx and x-Lx agree to rounding.
	x -= f.WidthKM * math.Floor(x/f.WidthKM)
	tx := x/f.CellWidthKM() - 0.5
	x0 := math.Floor(tx)
	fx := tx - x0
	c0 := mod(int(x0), cols)
	c1 := mod(c0+1, cols)

	var r0, r1 int
	var fy float64
	if wrapY {
		y -= f.HeightKM * math.Floor(y/f.HeightKM)
		ty := y/f.CellHeightKM() - 0.5
		y0 := math.Floor(ty)
		fy = ty - y0
		r0 = mod(int(y0), rows)
		r1 = mod(r0+1, rows)
	} else {
		ty := min(max(y/f.CellHeightKM()-0.5, 0), float64(rows-1))
		y0 := math.Floor(ty)
		fy = ty - y0
		r0 = int(y0)
		r1 = min(r0+1, rows-1)
	}

	v00 := float64(f.Values[r0*cols+c0])
	v10 := float64(f.Values[r0*cols+c1])
	v01 := float64(f.Values[r1*cols+c0])
	v11 := float64(f.Values[r1*cols+c1])
	top := v00 + (v10-v00)*fx
	bot := v01 + (v11-v01)*fx
	return top + (bot-top)*fy
}

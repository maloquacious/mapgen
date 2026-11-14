// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package terrain

import (
	"math"
	"math/cmplx"
	"testing"

	"github.com/maloquacious/mapgen/internal/continent"
	"github.com/maloquacious/mapgen/internal/structure"
)

// withResidual runs feature synthesis at one sample per cell over the flat
// foundation f, with and without residual detail.
func withResidual(t *testing.T, g continent.Grid, f []float32, features ...structure.Feature) (on, off *synthesis) {
	t.Helper()
	dom := structure.Domain{WidthKM: g.WidthKM, HeightKM: g.HeightKM}
	ff := &structure.FeatureFile{Format: structure.FeaturesFormat, Domain: dom, Features: features}
	if err := ff.Validate(dom); err != nil {
		t.Fatal(err)
	}
	rp := DefaultResidualParams()
	run := func(res *ResidualParams) *synthesis {
		sp := DefaultSubcellParams()
		sp.Samples = 1
		bp := DefaultBeltParams()
		syn, err := regional(t.Context(), synthInput{
			seed: 11, regional: DefaultRegionalParams(), belts: &bp, residual: res, subcell: sp, grid: g, features: ff,
			base: func(_, _ float64, cell int) float64 { return float64(f[cell]) }, center: f, workers: 2,
		})
		if err != nil {
			t.Fatal(err)
		}
		return syn
	}
	return run(&rp), run(nil)
}

// The residual moves a cell's elevation by at most its share of the
// structured relief, or the floor, and only raises its relief.
func TestResidualBounds(t *testing.T) {
	p := DefaultResidualParams()
	on, off := withResidual(t, bigGrid, bigFlat(300), bigBelt("collision", 0.3))
	moved := 0
	for i := range on.elevation {
		d := math.Abs(float64(on.elevation[i] - off.elevation[i]))
		if bound := max(min(p.FloorM, p.DatumShare*math.Abs(float64(off.elevation[i]))), p.Share*float64(off.relief[i])); d > bound+1e-3 {
			t.Fatalf("cell %d moved %g, bound %g", i, d, bound)
		}
		if on.relief[i] < off.relief[i] || on.high[i] < on.elevation[i] {
			t.Fatalf("cell %d: relief %g from %g, high %g, elevation %g", i, on.relief[i], off.relief[i], on.high[i], on.elevation[i])
		}
		if d > 1 {
			moved++
		}
	}
	if moved < len(on.elevation)/2 {
		t.Errorf("the residual moved only %d of %d cells", moved, len(on.elevation))
	}
}

// The residual added to the elevation has almost no power at wavelengths
// under four cells, along either axis.
func TestResidualWavelengths(t *testing.T) {
	const cols, rows = 256, 128
	g := continent.Grid{Columns: cols, Rows: rows, WidthKM: cols * 6, HeightKM: rows * 6, CellWidthKM: 6, CellHeightKM: 6}
	f := make([]float32, cols*rows)
	for i := range f {
		f[i] = 300
	}
	on, _ := withResidual(t, g, f)
	short := func(n int, at func(k, j int) float64, lines int) float64 {
		var all, high float64
		for j := range lines {
			for k := 1; k <= n/2; k++ {
				var c complex128
				for x := range n {
					c += complex(at(x, j), 0) * cmplx.Exp(complex(0, -2*math.Pi*float64(k*x)/float64(n)))
				}
				pw := real(c)*real(c) + imag(c)*imag(c)
				all += pw
				if k > n/4 {
					high += pw
				}
			}
		}
		return high / all
	}
	rowsShare := short(cols, func(x, r int) float64 { return float64(on.elevation[r*cols+x]) }, rows)
	colsShare := short(rows, func(y, c int) float64 { return float64(on.elevation[y*cols+c]) }, cols)
	t.Logf("power under four cells: %.4f along rows, %.4f along columns", rowsShare, colsShare)
	if rowsShare > 0.02 || colsShare > 0.02 {
		t.Errorf("power under four cells: %g along rows, %g along columns", rowsShare, colsShare)
	}
}

// Within a straight belt the residual varies less along the belt than
// across it.
func TestResidualGrain(t *testing.T) {
	b := belt(1, "old_orogen", rng(1000, 1000), structure.Sample{X: 20, Y: 300, WidthKM: 400}, structure.Sample{X: 580, Y: 300, WidthKM: 400},
		structure.Sample{X: 1140, Y: 300, WidthKM: 400})
	on, off := withResidual(t, bigGrid, bigFlat(300), b)
	d := func(c, r int) float64 {
		i := ((r+bigRows)%bigRows)*bigCols + (c+bigCols)%bigCols
		return float64(on.elevation[i] - off.elevation[i])
	}
	const lag = 4
	var along, across float64
	for r := 40; r < 60; r++ { // the core, within 60 km of the backbone
		for c := range bigCols {
			along += math.Abs(d(c+lag, r) - d(c, r))
			across += math.Abs(d(c, r+lag) - d(c, r))
		}
	}
	t.Logf("mean change over %d cells: %g along, %g across", lag, along/(20*bigCols), across/(20*bigCols))
	if !(along < 0.6*across) {
		t.Errorf("the residual changes %g along the belt and %g across it", along, across)
	}
}

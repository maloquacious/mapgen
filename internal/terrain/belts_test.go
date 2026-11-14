// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package terrain

import (
	"math"
	"testing"

	"github.com/maloquacious/mapgen/internal/structure"
	"github.com/maloquacious/mapgen/internal/trench"
)

// A 1200 x 600 km torus of 6 km cells, roomy enough for belt components.
const bigCols, bigRows = 200, 100

var (
	bigGrid = testGrid
	bigDom  = structure.Domain{WidthKM: 1200, HeightKM: 600}
)

func init() {
	bigGrid.Columns, bigGrid.Rows, bigGrid.WidthKM, bigGrid.HeightKM = bigCols, bigRows, 1200, 600
}

func bigFlat(v float32) []float32 {
	f := make([]float32, bigCols*bigRows)
	for i := range f {
		f[i] = v
	}
	return f
}

func bigCenter(cell int) structure.Point {
	return structure.Point{X: (float64(cell%bigCols) + 0.5) * 6, Y: (float64(cell/bigCols) + 0.5) * 6}
}

// bigSynth builds the surface on the large torus.
func bigSynth(t *testing.T, seed uint64, bp *BeltParams, tf *trench.TrenchFile, land []byte, f []float32, features ...structure.Feature) *synthesis {
	t.Helper()
	ff := &structure.FeatureFile{Format: structure.FeaturesFormat, Domain: bigDom, Features: features}
	if err := ff.Validate(bigDom); err != nil {
		t.Fatal(err)
	}
	syn := synth(t, seed, bp, 1, bigGrid, ff, tf, land, f)
	if syn.ridges != nil {
		if err := syn.ridges.Validate(ff); err != nil {
			t.Fatal(err)
		}
	}
	return syn
}

// A long collision belt bending across the large torus.
func bigBelt(kind string, maturity float64) structure.Feature {
	b := belt(1, kind, rng(2000, 2000),
		structure.Sample{X: 100, Y: 300, WidthKM: 240}, structure.Sample{X: 400, Y: 220, WidthKM: 260},
		structure.Sample{X: 700, Y: 320, WidthKM: 220}, structure.Sample{X: 1000, Y: 260, WidthKM: 240})
	b.Maturity = new(maturity)
	b.Targets.ReliefM = rng(2000, 3000)
	return b
}

// Chains stay inside the belt, ridges only raise the regional surface, and
// the chain count is in its range.
func TestChains(t *testing.T) {
	bp := DefaultBeltParams()
	bp.TroughShare = [2]float64{0, 0}
	for seed := range uint64(8) {
		b := bigBelt("collision", 0.1)
		base := bigSynth(t, seed, nil, nil, nil, bigFlat(100), b)
		syn := bigSynth(t, seed, &bp, nil, nil, bigFlat(100), b)
		chains := 0
		l := structure.NewLine(bigDom, b.Backbone)
		for _, r := range syn.ridges.Ridges {
			if r.Kind != RidgeChain {
				continue
			}
			chains++
			for _, v := range r.Backbone {
				if u := l.Cross(structure.Point{X: v.X, Y: v.Y}); u > bp.ChainReach {
					t.Fatalf("seed %d: chain %d vertex at %g half widths", seed, r.ID, u)
				}
			}
		}
		if cr := bp.Chains["collision"]; chains < cr[0] || chains > cr[1] {
			t.Errorf("seed %d: %d chains", seed, chains)
		}
		raised := 0.0
		for i, e := range syn.elevation {
			d := float64(e - base.elevation[i])
			if d < -1e-3 || d > 3000+1e-3 {
				t.Fatalf("seed %d cell %d: ridges added %g", seed, i, d)
			}
			raised = max(raised, d)
		}
		if raised < 1000 {
			t.Errorf("seed %d: the highest ridge rises %g", seed, raised)
		}
	}
}

// Corridors lower ridges, and every saddle stays above the regional surface.
func TestCorridors(t *testing.T) {
	bp := DefaultBeltParams()
	flat := bp
	flat.SaddleShare = [2]float64{0.001, 0.001} // nearly open; a share must be positive
	b := bigBelt("collision", 0)
	base := bigSynth(t, 3, nil, nil, nil, bigFlat(100), b)
	syn := bigSynth(t, 3, &bp, nil, nil, bigFlat(100), b)
	open := bigSynth(t, 3, &flat, nil, nil, bigFlat(100), b)
	if len(syn.ridges.Corridors) == 0 {
		t.Fatal("no corridors")
	}
	lowered := 0
	for i, e := range syn.elevation {
		if e > open.elevation[i]+1e-3 {
			t.Fatalf("cell %d: a corridor raised the surface", i)
		}
		if e < open.elevation[i]-1 {
			lowered++
		}
	}
	if lowered == 0 {
		t.Error("corridors lowered nothing")
	}
	saddles := 0
	for _, c := range syn.ridges.Corridors {
		for _, sd := range c.Saddles {
			saddles++
			cell := int(sd.Y/6)*bigCols + int(sd.X/6)
			if sd.ElevationM < float64(base.elevation[cell])-1e-3 || sd.ElevationM != round3(float64(syn.elevation[cell])) {
				t.Errorf("corridor %d: saddle on ridge %d at %g, regional %g", c.ID, sd.Ridge, sd.ElevationM, base.elevation[cell])
			}
		}
	}
	if saddles == 0 {
		t.Error("no saddles")
	}
}

// Mature belts have more of their chains in gaps.
func TestMaturityGaps(t *testing.T) {
	bp := DefaultBeltParams()
	share := func(maturity float64) float64 {
		gap, total := 0.0, 0.0
		for seed := range uint64(10) {
			syn := bigSynth(t, seed, &bp, nil, nil, bigFlat(100), bigBelt("old_orogen", maturity))
			for _, r := range syn.ridges.Ridges {
				if r.Kind != RidgeChain {
					continue
				}
				total += structure.NewLine(bigDom, r.Backbone).Length()
				for _, g := range r.GapsKM {
					gap += g[1] - g[0]
				}
			}
		}
		return gap / total
	}
	young, old := share(0), share(1)
	t.Logf("gap share %g at maturity 0, %g at maturity 1", young, old)
	if !(old > 2*young) {
		t.Errorf("gap share %g at maturity 0, %g at maturity 1", young, old)
	}
}

// An island arc breaks into separate peaks, spaced and sized within their
// ranges, rising from the submarine ridge.
func TestArcPeaks(t *testing.T) {
	bp := DefaultBeltParams()
	arc := structure.Feature{ID: 1, Type: structure.TypeIslandArc, Key: new(uint64(4)),
		Backbone: []structure.Sample{{X: 100, Y: 300, WidthKM: 80}, {X: 500, Y: 300, WidthKM: 80}, {X: 900, Y: 300, WidthKM: 80}},
		Targets:  &structure.Targets{RegionalElevationM: rng(-1000, -1000), ReliefM: rng(2000, 2000)}}
	base := bigSynth(t, 2, nil, nil, nil, bigFlat(-4000), arc)
	syn := bigSynth(t, 2, &bp, nil, nil, bigFlat(-4000), arc)
	pk := syn.ridges.Peaks
	if len(pk) < 800/150 {
		t.Fatalf("%d peaks", len(pk))
	}
	for i, p := range pk {
		if p.RadiusKM < 5 || p.RadiusKM > 20 || p.HeightM < 1200-1e-3 || p.HeightM > 2000+1e-3 || p.Y != 300 {
			t.Errorf("peak %+v", p)
		}
		if i > 0 {
			if d := p.X - pk[i-1].X; d < 50-1e-3 || d > 150+1e-3 {
				t.Errorf("peaks %d and %d are %g km apart", i, i+1, d)
			}
		}
	}
	above := 0
	for i, e := range syn.elevation {
		d := float64(e - base.elevation[i])
		if d < -1e-3 || d > 2000+1e-3 {
			t.Fatalf("cell %d: peaks added %g", i, d)
		}
		if e > 0 {
			above++
		}
	}
	if above == 0 {
		t.Error("no peak breaks the datum")
	}
}

// A trench deepens the seabed to its floor on its axis and leaves land
// and the seabed beyond its width alone.
func TestTrenchDepression(t *testing.T) {
	bp := DefaultBeltParams()
	tr := trench.Trench{ID: 1, Parent: 1, Key: new(uint64(9)), Side: "left",
		Backbone: []structure.Sample{{X: 100, Y: 150, WidthKM: 90}, {X: 500, Y: 150, WidthKM: 90}, {X: 900, Y: 150, WidthKM: 90}},
		Targets:  structure.Targets{RegionalElevationM: rng(-7200, -6800)}}
	tf := &trench.TrenchFile{Domain: bigDom, Trenches: []trench.Trench{tr}}
	land := make([]byte, bigCols*bigRows)
	// A small island on the axis.
	for _, cell := range []int{25*bigCols + 80, 25*bigCols + 81} {
		land[cell] = 1
	}
	f := bigFlat(-4000)
	for i := range land {
		if land[i] == 1 {
			f[i] = 50
		}
	}
	syn := bigSynth(t, 1, &bp, tf, land, f)
	d := syn.trenches[0]
	if d.FloorM[0] < -7200 || d.FloorM[1] > -6800 {
		t.Errorf("floor draws %v", *d.FloorM)
	}
	l := structure.NewLine(bigDom, tr.Backbone)
	for i, e := range syn.elevation {
		p := bigCenter(i)
		s, _, _ := l.Nearest(p)
		u := l.Cross(p)
		switch {
		case land[i] == 1 && e != 50:
			t.Fatalf("cell %d: the trench lowered land to %g", i, e)
		case u >= 1 && e != f[i]:
			t.Fatalf("cell %d beyond the trench at %g", i, e)
		case land[i] == 0 && u < 0.1 && s > 150 && s < 650 && (float64(e) > d.FloorM[1]+100 || float64(e) < d.FloorM[0]-1e-3):
			t.Fatalf("axis cell %d at %g, floor %v", i, e, *d.FloorM)
		}
	}
}

// With belt components, a feature crossing either seam still gives the same
// surface as the same feature translated by whole cells.
func TestComponentSeams(t *testing.T) {
	bp := DefaultBeltParams()
	place := func(dx, dy float64) structure.Feature {
		b := bigBelt("active_margin", 0.2)
		b.ElevatedFlank = "left"
		b.Backbone = append([]structure.Sample(nil), b.Backbone...)
		ox := math.Floor((b.Backbone[0].X+dx)/1200) * 1200
		oy := math.Floor((b.Backbone[0].Y+dy)/600) * 600
		for j := range b.Backbone {
			b.Backbone[j].X += dx - ox
			b.Backbone[j].Y += dy - oy
		}
		return b
	}
	const kx, ky = 150, 70 // cells; the moved belt crosses both seams and tiles
	for _, samples := range []int{1, 4} {
		ff := func(f structure.Feature) *structure.FeatureFile {
			return &structure.FeatureFile{Format: structure.FeaturesFormat, Domain: bigDom, Features: []structure.Feature{f}}
		}
		a := synth(t, 5, &bp, samples, bigGrid, ff(place(0, 0)), nil, nil, bigFlat(100))
		b := synth(t, 5, &bp, samples, bigGrid, ff(place(kx*6, ky*6)), nil, nil, bigFlat(100))
		if len(a.ridges.Ridges) != len(b.ridges.Ridges) || len(a.ridges.Peaks) != len(b.ridges.Peaks) {
			t.Fatalf("components differ: %d and %d ridges", len(a.ridges.Ridges), len(b.ridges.Ridges))
		}
		for i, e := range a.elevation {
			c, r := i%bigCols, i/bigCols
			j := ((r+ky)%bigRows)*bigCols + (c+kx)%bigCols
			if math.Abs(float64(e-b.elevation[j])) > 1 || math.Abs(float64(a.high[i]-b.high[j])) > 1 {
				t.Fatalf("%d samples, cell (%d, %d): %g, translated %g", samples, c, r, e, b.elevation[j])
			}
		}
	}
}

// Ridge records round-trip and validate against their features, and bad
// records are rejected.
func TestRidgeRecords(t *testing.T) {
	bp := DefaultBeltParams()
	b := bigBelt("collision", 0.2)
	ff := &structure.FeatureFile{Format: structure.FeaturesFormat, Domain: bigDom, Features: []structure.Feature{b}}
	syn := bigSynth(t, 1, &bp, nil, nil, bigFlat(100), b)
	data, err := syn.ridges.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseRidges(data)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := back.Marshal()
	if string(again) != string(data) {
		t.Fatal("ridges do not round-trip")
	}
	for name, change := range map[string]func(rf *RidgeFile){
		"feature": func(rf *RidgeFile) { rf.Ridges[0].Feature = 7 },
		"id":      func(rf *RidgeFile) { rf.Ridges[0].ID = 9 },
		"parent":  func(rf *RidgeFile) { rf.Ridges[0].Parent = 2 },
		"gap":     func(rf *RidgeFile) { rf.Ridges[0].GapsKM = [][2]float64{{5, 5}} },
		"saddle":  func(rf *RidgeFile) { rf.Corridors[0].Saddles = append(rf.Corridors[0].Saddles, Saddle{Ridge: 99}) },
		"share":   func(rf *RidgeFile) { rf.Corridors[0].SaddleShare = 1 },
		"domain":  func(rf *RidgeFile) { rf.Domain.WidthKM++ },
	} {
		rf, _ := ParseRidges(data)
		change(rf)
		if err := rf.Validate(ff); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// With 4 x 4 samples a cell's elevation is the mean of the surface at its
// samples, its high their largest, and its relief their spread.
func TestSubcellSamples(t *testing.T) {
	b := belt(1, "collision", rng(2000, 2000), structure.Sample{X: 30, Y: 150, WidthKM: 40}, structure.Sample{X: 270, Y: 150, WidthKM: 40},
		structure.Sample{X: 510, Y: 150, WidthKM: 40})
	ff := &structure.FeatureFile{Format: structure.FeaturesFormat, Domain: testDom, Features: []structure.Feature{b}}
	syn := synth(t, 1, nil, 4, testGrid, ff, nil, nil, flat(100))
	l := structure.NewLine(testDom, b.Backbone)
	for cell := range testCols * testRows {
		c, r := cell%testCols, cell/testCols
		sum, hi, lo, reached := 0.0, math.Inf(-1), math.Inf(1), false
		for j := range 4 {
			for k := range 4 {
				p := structure.Point{X: (float64(4*c+k) + 0.5) * 1.5, Y: (float64(4*r+j) + 0.5) * 1.5}
				u := l.Cross(p)
				v := 100 + kernel(u)*1900
				reached = reached || u < 1
				sum, hi, lo = sum+v, max(hi, v), min(lo, v)
			}
		}
		e, h, rl := float64(syn.elevation[cell]), float64(syn.high[cell]), float64(syn.relief[cell])
		if !reached {
			if e != 100 || h != 100 || rl != 0 {
				t.Fatalf("cell %d beyond the belt: %g %g %g", cell, e, h, rl)
			}
			continue
		}
		if !near(e, sum/16) || !near(h, hi) || math.Abs(rl-(hi-lo)) > 1e-2 {
			t.Fatalf("cell %d: elevation %g high %g relief %g, want %g %g %g", cell, e, h, rl, sum/16, hi, hi-lo)
		}
	}
}

// A peak narrower than the sample spacing still sets its cell's high.
func TestSubcellPeak(t *testing.T) {
	bp := DefaultBeltParams()
	bp.PeakDiameterKM = [2]float64{1, 1}
	arc := structure.Feature{ID: 1, Type: structure.TypeIslandArc, Key: new(uint64(4)),
		Backbone: []structure.Sample{{X: 30, Y: 150, WidthKM: 40}, {X: 270, Y: 150, WidthKM: 40}, {X: 510, Y: 150, WidthKM: 40}},
		Targets:  &structure.Targets{RegionalElevationM: rng(-1000, -1000), ReliefM: rng(2000, 2000)}}
	ff := &structure.FeatureFile{Format: structure.FeaturesFormat, Domain: testDom, Features: []structure.Feature{arc}}
	syn := synth(t, 2, &bp, 4, testGrid, ff, nil, nil, flat(-4000))
	if len(syn.ridges.Peaks) == 0 {
		t.Fatal("no peaks")
	}
	for _, pk := range syn.ridges.Peaks {
		cell := int(pk.Y/6)*testCols + int(pk.X/6)
		if h := float64(syn.high[cell]); h < -1000+pk.HeightM-50 {
			t.Errorf("peak %d of %g m: cell high %g, elevation %g", pk.ID, pk.HeightM, h, syn.elevation[cell])
		}
	}
}

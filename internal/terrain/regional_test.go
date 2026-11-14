// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package terrain

import (
	"math"
	"testing"

	"github.com/maloquacious/mapgen/internal/continent"
	"github.com/maloquacious/mapgen/internal/structure"
	"github.com/maloquacious/mapgen/internal/trench"
)

// A 600 x 300 km torus of 6 km cells.
const testCols, testRows = 100, 50

var (
	testGrid = continent.Grid{Columns: testCols, Rows: testRows, WidthKM: 600, HeightKM: 300, CellWidthKM: 6, CellHeightKM: 6}
	testDom  = structure.Domain{WidthKM: 600, HeightKM: 300}
)

func flat(v float32) []float32 {
	f := make([]float32, testCols*testRows)
	for i := range f {
		f[i] = v
	}
	return f
}

func center(cell int) structure.Point {
	return structure.Point{X: (float64(cell%testCols) + 0.5) * 6, Y: (float64(cell/testCols) + 0.5) * 6}
}

func rng(lo, hi float64) *[2]float64 { return &[2]float64{lo, hi} }

func belt(id uint32, kind string, regional *[2]float64, pts ...structure.Sample) structure.Feature {
	return structure.Feature{ID: id, Type: structure.TypeMountainBelt, Kind: kind, Key: new(uint64(id)), Maturity: new(0.0), ElevatedFlank: "none",
		Backbone: pts, Targets: &structure.Targets{RegionalElevationM: regional}}
}

func area(id uint32, typ string, regional *[2]float64, edge float64, outline ...structure.Point) structure.Feature {
	f := structure.Feature{ID: id, Type: typ, Key: new(uint64(id)), Outline: outline, EdgeKM: edge, Targets: &structure.Targets{RegionalElevationM: regional}}
	if typ == structure.TypeBasin {
		f.Kind = "interior"
	}
	return f
}

func square(x0, y0, x1, y1 float64) []structure.Point {
	return []structure.Point{{X: x0, Y: y0}, {X: x1, Y: y0}, {X: x1, Y: y1}, {X: x0, Y: y1}}
}

// surface validates the features and builds the regional surface over f.
func surface(t *testing.T, seed uint64, f []float32, features ...structure.Feature) ([]float32, []FeatureDiagnostics) {
	t.Helper()
	ff := &structure.FeatureFile{Format: structure.FeaturesFormat, Domain: testDom, Features: features}
	if err := ff.Validate(testDom); err != nil {
		t.Fatal(err)
	}
	syn := synthesize(t, seed, nil, nil, nil, f, ff)
	return syn.elevation, syn.features
}

// synthesize builds the surface over f with belt parameters bp, or the
// regional surface alone when bp is nil, and trenches tf.
func synthesize(t *testing.T, seed uint64, bp *BeltParams, tf *trench.TrenchFile, land []byte, f []float32, ff *structure.FeatureFile) *synthesis {
	t.Helper()
	return synth(t, seed, bp, 1, testGrid, ff, tf, land, f)
}

// synth runs feature synthesis with samples x samples points per cell,
// over a foundation that is f[cell] throughout each cell.
func synth(t *testing.T, seed uint64, bp *BeltParams, samples int, g continent.Grid, ff *structure.FeatureFile, tf *trench.TrenchFile, land []byte, f []float32) *synthesis {
	t.Helper()
	sp := DefaultSubcellParams()
	sp.Samples = samples
	syn, err := regional(t.Context(), synthInput{
		seed: seed, regional: DefaultRegionalParams(), belts: bp, subcell: sp, grid: g, features: ff, trenches: tf, land: land,
		base: func(_, _ float64, cell int) float64 { return float64(f[cell]) }, center: f, workers: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	return syn
}

func near(a, b float64) bool { return math.Abs(a-b) <= 1e-3*max(1, math.Abs(b)) }

// Across a straight belt of constant width the surface is F + K(t)(T − F).
func TestKernelProfile(t *testing.T) {
	b := belt(1, "collision", rng(2000, 2000), structure.Sample{X: 30, Y: 150, WidthKM: 120}, structure.Sample{X: 270, Y: 150, WidthKM: 120},
		structure.Sample{X: 510, Y: 150, WidthKM: 120})
	out, diags := surface(t, 1, flat(100), b)
	l := structure.NewLine(testDom, b.Backbone)
	for i, e := range out {
		if want := 100 + kernel(l.Cross(center(i)))*1900; !near(float64(e), want) {
			t.Fatalf("cell %d: %g, want %g", i, e, want)
		}
	}
	if d := diags[0]; d.Core.Cells == 0 || d.Core.MinM < 100+0.9*1900-1e-3 || d.Core.MaxM > 2000 || d.Core.MaxM < 1980 {
		t.Errorf("core %+v", d.Core)
	}
}

// Every draw lies within its range, and an uplift never exceeds its target.
func TestDrawsWithinRanges(t *testing.T) {
	b := belt(1, "collision", rng(1500, 3000), structure.Sample{X: 30, Y: 150, WidthKM: 200}, structure.Sample{X: 270, Y: 100, WidthKM: 120},
		structure.Sample{X: 510, Y: 160, WidthKM: 160})
	for seed := range uint64(20) {
		out, diags := surface(t, seed, flat(0), b)
		d := diags[0]
		if r := d.RegionalM; r[0] < 1500 || r[1] > 3000 {
			t.Fatalf("seed %d: regional draws %v", seed, *r)
		}
		if r := d.ReliefM; r[0] < 2500 || r[1] > 5000 {
			t.Fatalf("seed %d: relief draws %v outside the default range", seed, *r)
		}
		for i, e := range out {
			if e < 0 || float64(e) > d.RegionalM[1]+1e-3 {
				t.Fatalf("seed %d cell %d: %g", seed, i, e)
			}
		}
	}
}

func TestPlateauAndBasinTargets(t *testing.T) {
	p := area(1, structure.TypePlateau, rng(2500, 4500), 20, square(160, 80, 440, 220)...)
	p.Parent, p.Side = 2, "left"
	host := belt(2, "collision", rng(2000, 2000), structure.Sample{X: 30, Y: 280, WidthKM: 20}, structure.Sample{X: 90, Y: 280, WidthKM: 20})
	host.ElevatedFlank = "left"
	out, diags := surface(t, 3, flat(200), p, host)
	draw := diags[0].RegionalM
	if draw[0] != draw[1] || draw[0] < 2500 || draw[0] > 4500 {
		t.Fatalf("plateau draw %v", *draw)
	}
	r, hl := structure.NewRegion(testDom, p.Outline, p.EdgeKM), structure.NewLine(testDom, host.Backbone)
	for i, e := range out {
		// The interior rolls by up to PlateauRolling of the relief draw.
		if m := r.Mask(center(i)); m == 1 && math.Abs(float64(e)-draw[0]) > DefaultRegionalParams().PlateauRolling*diags[0].ReliefM[0]+1e-3 {
			t.Fatalf("plateau interior cell %d at %g, want %g", i, e, draw[0])
		} else if m == 0 && e != 200 && hl.Cross(center(i)) >= 1 {
			t.Fatalf("cell %d outside the plateau at %g", i, e)
		}
	}

	bs := area(1, structure.TypeBasin, rng(100, 300), 30, square(160, 80, 440, 220)...)
	out, diags = surface(t, 3, flat(800), bs)
	draw = diags[0].RegionalM
	r = structure.NewRegion(testDom, bs.Outline, bs.EdgeKM)
	for i, e := range out {
		if m := r.Mask(center(i)); m == 1 && !near(float64(e), draw[0]) {
			t.Fatalf("basin floor cell %d at %g, want %g", i, e, draw[0])
		} else if m == 0 && e != 800 {
			t.Fatalf("cell %d outside the basin at %g", i, e)
		}
	}
	// A basin above the surface does nothing.
	out, _ = surface(t, 3, flat(50), bs)
	for i, e := range out {
		if e != 50 {
			t.Fatalf("cell %d: a basin above the ground changed it to %g", i, e)
		}
	}
}

// A rift's axis lies below its shoulders by the relief draw.
func TestRiftProfile(t *testing.T) {
	const a = 0.4
	rf := structure.Feature{ID: 1, Type: structure.TypeRift, Key: new(uint64(1)), AxisFraction: new(a),
		Backbone: []structure.Sample{{X: 30, Y: 150, WidthKM: 150}, {X: 270, Y: 150, WidthKM: 150}, {X: 510, Y: 150, WidthKM: 150}},
		Targets:  &structure.Targets{RegionalElevationM: rng(1000, 1000), ReliefM: rng(600, 600)}}
	out, _ := surface(t, 1, flat(200), rf)
	l := structure.NewLine(testDom, rf.Backbone)
	lowest := math.Inf(1)
	for i, e := range out {
		u := l.Cross(center(i))
		want := 200 + kernel((u-a)/(1-a))*800
		if u < a {
			want = 1000 - kernel(u/a)*600
		}
		if !near(float64(e), want) {
			t.Fatalf("cell %d at cross position %g: %g, want %g", i, u, e, want)
		}
		lowest = min(lowest, float64(e))
	}
	if lowest > 400+30 {
		t.Errorf("axis floor %g, want near 400", lowest)
	}
}

// A rift's axis stops RiftFloorM below the datum on land, and below the
// foundation at sea.
func TestRiftFloor(t *testing.T) {
	const a = 0.4
	rf := structure.Feature{ID: 1, Type: structure.TypeRift, Key: new(uint64(1)), AxisFraction: new(a),
		Backbone: []structure.Sample{{X: 30, Y: 150, WidthKM: 150}, {X: 270, Y: 150, WidthKM: 150}, {X: 510, Y: 150, WidthKM: 150}},
		Targets:  &structure.Targets{RegionalElevationM: rng(500, 500), ReliefM: rng(2000, 2000)}}
	floor := DefaultRegionalParams().RiftFloorM
	for _, base := range []float32{200, -500} {
		out, _ := surface(t, 1, flat(base), rf)
		l := structure.NewLine(testDom, rf.Backbone)
		for i, e := range out {
			u := l.Cross(center(i))
			if u >= a {
				continue
			}
			shoulder := max(500, float64(base))
			want := shoulder - kernel(u/a)*(shoulder-(min(float64(base), 0)-floor))
			if !near(float64(e), want) {
				t.Fatalf("base %g, cell %d at cross position %g: %g, want %g", base, i, u, e, want)
			}
		}
	}
}

// Two crossing belts reach the larger uplift plus a quarter of the smaller,
// and a basin inside a belt's flank lowers it to the basin's level.
func TestCombination(t *testing.T) {
	h := belt(1, "collision", rng(2000, 2000), structure.Sample{X: 30, Y: 150, WidthKM: 120}, structure.Sample{X: 270, Y: 150, WidthKM: 120},
		structure.Sample{X: 510, Y: 150, WidthKM: 120})
	v := belt(2, "old_orogen", rng(1000, 1000), structure.Sample{X: 300, Y: 20, WidthKM: 100}, structure.Sample{X: 300, Y: 150, WidthKM: 100},
		structure.Sample{X: 300, Y: 280, WidthKM: 100})
	out, _ := surface(t, 1, flat(0), h, v)
	lh, lv := structure.NewLine(testDom, h.Backbone), structure.NewLine(testDom, v.Backbone)
	for i, e := range out {
		a, b := kernel(lh.Cross(center(i)))*2000, kernel(lv.Cross(center(i)))*1000
		if want := max(a, b) + 0.25*min(a, b); !near(float64(e), want) {
			t.Fatalf("cell %d: %g, want %g", i, e, want)
		}
	}

	bs := area(3, structure.TypeBasin, rng(300, 300), 12, square(60, 160, 240, 200)...)
	bs.Kind, bs.Parent, bs.Side = "foreland", 1, "right"
	out, _ = surface(t, 1, flat(0), h, bs)
	r := structure.NewRegion(testDom, bs.Outline, bs.EdgeKM)
	lowered := 0
	for i, e := range out {
		if r.Mask(center(i)) == 1 {
			if want := min(300, kernel(lh.Cross(center(i)))*2000); !near(float64(e), want) {
				t.Fatalf("cell %d in the basin: %g, want %g", i, e, want)
			}
			if e < float32(kernel(lh.Cross(center(i)))*2000)-1 {
				lowered++
			}
		}
	}
	if lowered == 0 {
		t.Error("the basin lowered no part of the belt")
	}
}

// Faults: a strike-slip trough, a normal step with its downthrown side, and
// a thrust ridge, each confined to the fault's width.
func TestFaults(t *testing.T) {
	bb := []structure.Sample{{X: 30, Y: 150, WidthKM: 40}, {X: 270, Y: 150, WidthKM: 40}, {X: 510, Y: 150, WidthKM: 40}}
	l := structure.NewLine(testDom, bb)
	p := DefaultRegionalParams()
	for _, sense := range []string{"strike_slip", "normal", "thrust"} {
		f := structure.Feature{ID: 1, Type: structure.TypeFault, Key: new(uint64(5)), Sense: sense, Backbone: bb}
		out, diags := surface(t, 1, flat(500), f)
		off := diags[0].OffsetM
		if r := p.FaultM[sense]; off < r[0] || off > r[1] || diags[0].Kind != sense {
			t.Fatalf("%s: offset %g", sense, off)
		}
		var above, below int
		for i, e := range out {
			u := l.Cross(center(i))
			if u >= 1 && e != 500 {
				t.Fatalf("%s: cell %d outside the fault at %g", sense, i, e)
			}
			d := float64(e) - 500
			switch sense {
			case "strike_slip":
				if !near(d, -kernel(u)*off) {
					t.Fatalf("strike_slip cell %d: %g", i, d)
				}
			case "thrust":
				if !near(d, kernel(u)*off) {
					t.Fatalf("thrust cell %d: %g", i, d)
				}
			case "normal":
				if !near(math.Abs(d), kernel(u)*off/2) {
					t.Fatalf("normal cell %d: %g", i, d)
				}
			}
			if d > 0 {
				above++
			} else if d < 0 {
				below++
			}
		}
		if sense == "normal" && (above == 0 || below == 0) {
			t.Errorf("normal fault: %d cells raised, %d lowered", above, below)
		}
	}
}

// A feature crossing either seam gives the same surface as the same feature
// translated by whole cells, compared through the periodic translation.
func TestSeams(t *testing.T) {
	// place translates every feature by (dx, dy) km, keeping each one's
	// first vertex in the base domain.
	place := func(dx, dy float64) []structure.Feature {
		wrap := func(v, size float64) float64 { return v - size*math.Floor(v/size) - v }
		b := belt(1, "collision", rng(1500, 3000), structure.Sample{X: 500, Y: 250, WidthKM: 100}, structure.Sample{X: 700, Y: 290, WidthKM: 140},
			structure.Sample{X: 800, Y: 420, WidthKM: 80})
		rf := structure.Feature{ID: 2, Type: structure.TypeRift, Key: new(uint64(2)), AxisFraction: new(0.3),
			Backbone: []structure.Sample{{X: 100, Y: 200, WidthKM: 90}, {X: 130, Y: 330, WidthKM: 90}}}
		bs := area(3, structure.TypeBasin, rng(0, 300), 20, structure.Point{X: 560, Y: 10}, structure.Point{X: 680, Y: -20}, structure.Point{X: 650, Y: 60})
		out := []structure.Feature{b, rf, bs}
		for k := range out {
			f := &out[k]
			if f.Linear() {
				f.Backbone = append([]structure.Sample(nil), f.Backbone...)
				ox, oy := wrap(f.Backbone[0].X+dx, 600), wrap(f.Backbone[0].Y+dy, 300)
				for j := range f.Backbone {
					f.Backbone[j].X += dx + ox
					f.Backbone[j].Y += dy + oy
				}
			} else {
				f.Outline = append([]structure.Point(nil), f.Outline...)
				ox, oy := wrap(f.Outline[0].X+dx, 600), wrap(f.Outline[0].Y+dy, 300)
				for j := range f.Outline {
					f.Outline[j].X += dx + ox
					f.Outline[j].Y += dy + oy
				}
			}
		}
		return out
	}
	const kx, ky = 37, 21 // cells
	base, _ := surface(t, 9, flat(400), place(0, 0)...)
	moved, _ := surface(t, 9, flat(400), place(-kx*6, -ky*6)...)
	differs := 0
	for i, e := range base {
		c, r := i%testCols, i/testCols
		j := ((r-ky+testRows)%testRows)*testCols + (c-kx+testCols)%testCols
		if math.Abs(float64(e-moved[j])) > 1e-2 {
			t.Fatalf("cell (%d, %d): %g, translated %g", c, r, e, moved[j])
		}
		if e != 400 {
			differs++
		}
	}
	if differs == 0 {
		t.Fatal("the features changed nothing")
	}
}

// The seed changes target draws but not where the envelopes reach, and
// adding a feature leaves the cells beyond its reach unchanged.
func TestSeedAndIndependence(t *testing.T) {
	b := belt(1, "collision", rng(1500, 3000), structure.Sample{X: 30, Y: 150, WidthKM: 120}, structure.Sample{X: 270, Y: 120, WidthKM: 120})
	a1, _ := surface(t, 1, flat(0), b)
	a2, _ := surface(t, 2, flat(0), b)
	same := true
	for i := range a1 {
		if (a1[i] != 0) != (a2[i] != 0) {
			t.Fatalf("cell %d: the seed moved the envelope", i)
		}
		same = same && a1[i] == a2[i]
	}
	if same {
		t.Error("the seed changes nothing")
	}
	p := area(2, structure.TypeBasin, rng(0, 300), 20, square(400, 80, 560, 220)...)
	with, _ := surface(t, 1, flat(500), b, p)
	without, _ := surface(t, 1, flat(500), b)
	box := structure.NewRegion(testDom, p.Outline, p.EdgeKM).Bounds()
	for i := range with {
		pt := center(i)
		if (pt.X < box.MinX || pt.X > box.MaxX || pt.Y < box.MinY || pt.Y > box.MaxY) && with[i] != without[i] {
			t.Fatalf("cell %d beyond the basin changed", i)
		}
	}
}

// An active margin or island arc narrows the shelf near it.
func TestShelfNarrowing(t *testing.T) {
	p := DefaultFoundationParams()
	arc := structure.Feature{ID: 1, Type: structure.TypeIslandArc, Key: new(uint64(1)),
		Backbone: []structure.Sample{{X: 30, Y: 150, WidthKM: 60}, {X: 270, Y: 150, WidthKM: 60}, {X: 510, Y: 150, WidthKM: 60}}}
	ff := &structure.FeatureFile{Domain: testDom, Features: []structure.Feature{arc}}
	narrow, err := shelfNarrowing(t.Context(), p, testGrid, ff)
	if err != nil {
		t.Fatal(err)
	}
	l := structure.NewLine(testDom, arc.Backbone)
	for i, v := range narrow {
		u := l.Cross(center(i))
		if want := 1 - smoothstep(p.NarrowShelfReach[0], p.NarrowShelfReach[1], u); math.Abs(float64(v)-want) > 1e-6 {
			t.Fatalf("cell %d at %g half widths: %g, want %g", i, u, v, want)
		}
	}
	empty, err := shelfNarrowing(t.Context(), p, testGrid, &structure.FeatureFile{Domain: testDom})
	if err != nil || empty != nil {
		t.Errorf("no margins: %v %v", empty, err)
	}

	// Fully narrowed, a 200 km shelf is 50 km wide.
	const cols, rows = 4, 200
	g := continent.Grid{Columns: cols, Rows: rows, WidthKM: 24, HeightKM: 1200, CellWidthKM: 6, CellHeightKM: 6}
	land := band(cols, rows, 50, 150)
	dist, err := coastDistance(t.Context(), land, cols, rows, 6, 6)
	if err != nil {
		t.Fatal(err)
	}
	fd := &foundation{base: constant(g, 400)}
	fd.fields[fieldVariation] = constant(g, 0.5)
	fd.fields[fieldShelfDepth] = constant(g, 0.5)
	fd.fields[fieldShelfWidth] = constant(g, 1) // 200 km
	fd.fields[fieldAbyssalDepth] = constant(g, 1)
	all := make([]float32, cols*rows)
	for i := range all {
		all[i] = 1
	}
	elev, err := fd.elevation(t.Context(), p, g, dist, all)
	if err != nil {
		t.Fatal(err)
	}
	for r := range rows {
		if land[r*cols] == 1 {
			continue
		}
		y := (float64(r) + 0.5) * 6
		d := min(math.Abs(y-300), math.Abs(y-900))
		if want := WaterProfile(p, 125, 50, 5500, d); !near(float64(elev[r*cols]), want) {
			t.Fatalf("row %d: %g, want %g", r, elev[r*cols], want)
		}
	}
}

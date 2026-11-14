// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package structure_test

import (
	"math"
	"testing"

	"github.com/maloquacious/mapgen/internal/structure"
)

var dom = structure.Domain{WidthKM: 1000, HeightKM: 800}

func near(a, b float64) bool { return math.Abs(a-b) <= 1e-9 }

func shiftLine(b []structure.Sample, dx, dy float64) []structure.Sample {
	out := make([]structure.Sample, len(b))
	for i, s := range b {
		out[i] = structure.Sample{X: s.X + dx, Y: s.Y + dy, WidthKM: s.WidthKM}
	}
	return out
}

func shiftRing(o []structure.Point, dx, dy float64) []structure.Point {
	out := make([]structure.Point, len(o))
	for i, p := range o {
		out[i] = structure.Point{X: p.X + dx, Y: p.Y + dy}
	}
	return out
}

// A straight belt has exact answers, including beyond both ends.
func TestStraightBelt(t *testing.T) {
	l := structure.NewLine(dom, []structure.Sample{{X: 100, Y: 400, WidthKM: 100}, {X: 500, Y: 400, WidthKM: 200}})
	if !near(l.Length(), 400) {
		t.Fatalf("length %g", l.Length())
	}
	for _, c := range []struct {
		p    structure.Point
		s, d float64
		side structure.Side
	}{
		{structure.Point{X: 300, Y: 430}, 200, 30, structure.Right}, // south of an eastward belt
		{structure.Point{X: 300, Y: 370}, 200, 30, structure.Left},
		{structure.Point{X: 300, Y: 400}, 200, 0, structure.On},
		{structure.Point{X: 50, Y: 400}, 0, 50, structure.On}, // straight behind the start
		{structure.Point{X: 560, Y: 480}, 400, 100, structure.Right},
		{structure.Point{X: 70, Y: 360}, 0, 50, structure.Left},
	} {
		s, d, side := l.Nearest(c.p)
		if !near(s, c.s) || !near(d, c.d) || side != c.side {
			t.Errorf("Nearest(%v) = %g, %g, %v; want %g, %g, %v", c.p, s, d, side, c.s, c.d, c.side)
		}
	}
	for s, want := range map[float64]float64{-10: 100, 0: 100, 100: 125, 200: 150, 400: 200, 999: 200} {
		if got := l.WidthAt(s); !near(got, want) {
			t.Errorf("WidthAt(%g) = %g, want %g", s, got, want)
		}
	}
	// Half width at s=200 is 75 km.
	if u := l.Cross(structure.Point{X: 300, Y: 430}); !near(u, 0.4) {
		t.Errorf("Cross = %g, want 0.4", u)
	}
	if b := l.Bounds(); b != (structure.Rect{MinX: 0, MinY: 300, MaxX: 600, MaxY: 500}) {
		t.Errorf("Bounds = %+v", b)
	}
}

// The outside of a sharp bend stays on one side all the way round the vertex.
func TestSideAroundBend(t *testing.T) {
	// East, then back to the north-west: a 135 degree left turn at (500,400).
	l := structure.NewLine(dom, []structure.Sample{{X: 300, Y: 400, WidthKM: 50}, {X: 500, Y: 400, WidthKM: 50}, {X: 400, Y: 300, WidthKM: 50}})
	for deg := -40.0; deg <= 90; deg += 10 { // between the two segments' right normals
		// Points 20 km from the vertex, sweeping the outside of the bend.
		a := deg * math.Pi / 180
		p := structure.Point{X: 500 + 20*math.Cos(a), Y: 400 + 20*math.Sin(a)}
		if s, d, side := l.Nearest(p); !near(s, 200) || !near(d, 20) || side != structure.Right {
			t.Errorf("at %g degrees: s=%g d=%g side=%v", deg, s, d, side)
		}
	}
	if _, _, side := l.Nearest(structure.Point{X: 440, Y: 380}); side != structure.Left {
		t.Errorf("inside of the bend is %v, want left", side)
	}
}

func TestRectangularPlateau(t *testing.T) {
	r := structure.NewRegion(dom, []structure.Point{{X: 100, Y: 100}, {X: 300, Y: 100}, {X: 300, Y: 200}, {X: 100, Y: 200}}, 20)
	for _, c := range []struct {
		p            structure.Point
		inside, mask float64
	}{
		{structure.Point{X: 200, Y: 150}, 50, 1},
		{structure.Point{X: 110, Y: 150}, 10, 0.5},
		{structure.Point{X: 90, Y: 150}, -10, 0},
		{structure.Point{X: 310, Y: 210}, -math.Sqrt(200), 0},
		{structure.Point{X: 100, Y: 150}, 0, 0},
	} {
		if got := r.Inside(c.p); !near(got, c.inside) {
			t.Errorf("Inside(%v) = %g, want %g", c.p, got, c.inside)
		}
		if got := r.Mask(c.p); !near(got, c.mask) {
			t.Errorf("Mask(%v) = %g, want %g", c.p, got, c.mask)
		}
	}
}

// probes covers the whole domain, on and off cell centers.
func probes() []structure.Point {
	var ps []structure.Point
	for y := 3.0; y < dom.HeightKM; y += 47 {
		for x := 5.0; x < dom.WidthKM; x += 53 {
			ps = append(ps, structure.Point{X: x, Y: y})
		}
	}
	return ps
}

// A feature answers identically wherever its unwrapped copy is stored, and
// a query finds a feature stored on the far side of either seam.
func TestSeamInvariance(t *testing.T) {
	belt := []structure.Sample{{X: 900, Y: 700, WidthKM: 120}, {X: 1100, Y: 760, WidthKM: 180}, {X: 1150, Y: 900, WidthKM: 100}}
	ring := []structure.Point{{X: 950, Y: 750}, {X: 1080, Y: 760}, {X: 1060, Y: 880}, {X: 940, Y: 850}}
	l, r := structure.NewLine(dom, belt), structure.NewRegion(dom, ring, 30)
	for _, k := range [][2]float64{{1, 0}, {0, 1}, {1, 1}, {-1, -1}, {3, -2}} {
		dx, dy := k[0]*dom.WidthKM, k[1]*dom.HeightKM
		l2, r2 := structure.NewLine(dom, shiftLine(belt, dx, dy)), structure.NewRegion(dom, shiftRing(ring, dx, dy), 30)
		for _, p := range probes() {
			s, d, side := l.Nearest(p)
			s2, d2, side2 := l2.Nearest(p)
			// The same point named by another image must agree too.
			s3, d3, side3 := l.Nearest(structure.Point{X: p.X - dx, Y: p.Y - dy})
			if !near(s, s2) || !near(d, d2) || side != side2 || !near(s, s3) || !near(d, d3) || side != side3 {
				t.Fatalf("shift %v: Nearest(%v) differs: (%g %g %v) (%g %g %v) (%g %g %v)", k, p, s, d, side, s2, d2, side2, s3, d3, side3)
			}
			if a, b, c := r.Inside(p), r2.Inside(p), r.Inside(structure.Point{X: p.X - dx, Y: p.Y - dy}); !near(a, b) || !near(a, c) {
				t.Fatalf("shift %v: Inside(%v) differs: %g %g %g", k, p, a, b, c)
			}
		}
	}
	// A belt stored just west of the east edge is 20 km from a point just
	// east of the west edge; likewise across the north/south seam.
	ew := structure.NewLine(dom, []structure.Sample{{X: 990, Y: 100, WidthKM: 10}, {X: 990, Y: 300, WidthKM: 10}})
	if _, d, side := ew.Nearest(structure.Point{X: 10, Y: 200}); !near(d, 20) || side != structure.Left {
		t.Errorf("east/west seam: d=%g side=%v", d, side)
	}
	ns := structure.NewLine(dom, []structure.Sample{{X: 100, Y: 790, WidthKM: 10}, {X: 300, Y: 790, WidthKM: 10}})
	if _, d, side := ns.Nearest(structure.Point{X: 200, Y: 10}); !near(d, 20) || side != structure.Right {
		t.Errorf("north/south seam: d=%g side=%v", d, side)
	}
}

// Recentering moves features and cells together by whole cells. Every
// relationship must survive it.
func TestTranslationInvariance(t *testing.T) {
	const cols, rows = 125, 100 // 8 km cells
	belt := []structure.Sample{{X: 200, Y: 300, WidthKM: 120}, {X: 420, Y: 380, WidthKM: 200}, {X: 500, Y: 560, WidthKM: 90}}
	ring := []structure.Point{{X: 250, Y: 150}, {X: 480, Y: 170}, {X: 430, Y: 300}, {X: 240, Y: 260}}
	l, r := structure.NewLine(dom, belt), structure.NewRegion(dom, ring, 30)
	for _, shift := range [][2]int{{37, 11}, {124, 99}, {60, 0}, {0, 55}} {
		dx, dy := float64(shift[0])*dom.WidthKM/cols, float64(shift[1])*dom.HeightKM/rows
		l2, r2 := structure.NewLine(dom, shiftLine(belt, dx, dy)), structure.NewRegion(dom, shiftRing(ring, dx, dy), 30)
		for _, p := range probes() {
			q := structure.Point{X: p.X + dx, Y: p.Y + dy}
			s, d, side := l.Nearest(p)
			s2, d2, side2 := l2.Nearest(q)
			if !near(s, s2) || !near(d, d2) || side != side2 || !near(l.Cross(p), l2.Cross(q)) {
				t.Fatalf("shift %v at %v: (%g %g %v) became (%g %g %v)", shift, p, s, d, side, s2, d2, side2)
			}
			if a, b := r.Inside(p), r2.Inside(q); !near(a, b) {
				t.Fatalf("shift %v at %v: Inside %g became %g", shift, p, a, b)
			}
		}
		// The rasters translate cyclically with the features.
		before, after := structure.Cover(dom, cols, rows, l), structure.Cover(dom, cols, rows, l2)
		for i, v := range before {
			j := ((i/cols+shift[1])%rows)*cols + (i%cols+shift[0])%cols
			if after[j] != v {
				t.Fatalf("shift %v: envelope raster was not translated intact", shift)
			}
		}
	}
}

// Rasters at two cell sizes disagree only where the envelope boundary passes
// through the coarser cell.
func TestResolutionIndependence(t *testing.T) {
	const cols, rows, half = 100, 80, 60.0 // 10 km cells; fine cells are 5 km
	l := structure.NewLine(dom, []structure.Sample{{X: 850, Y: 650, WidthKM: 2 * half}, {X: 1080, Y: 720, WidthKM: 2 * half}, {X: 1150, Y: 900, WidthKM: 2 * half}})
	coarse, fine := structure.Cover(dom, cols, rows, l), structure.Cover(dom, 2*cols, 2*rows, l)
	covered, differ := 0, 0
	reach := math.Hypot(10, 10) / 2 // farthest point of a coarse cell from its center
	for i, v := range fine {
		col, row := i%(2*cols)/2, i/(2*cols)/2
		if v {
			covered++
		}
		if v == coarse[row*cols+col] {
			continue
		}
		differ++
		_, d, _ := l.Nearest(structure.Point{X: (float64(col) + 0.5) * 10, Y: (float64(row) + 0.5) * 10})
		if math.Abs(d-half) > reach {
			t.Fatalf("cell (%d,%d) disagrees %g km from the envelope boundary", col, row, math.Abs(d-half))
		}
	}
	if covered == 0 || covered == len(fine) || differ == 0 {
		t.Fatalf("test does not exercise the boundary: covered %d of %d, %d differ", covered, len(fine), differ)
	}
	// Both rasters estimate the same physical area.
	n := 0
	for _, v := range coarse {
		if v {
			n++
		}
	}
	if a, b := float64(n)*100, float64(covered)*25; math.Abs(a-b)/b > 0.02 {
		t.Errorf("areas differ: %g and %g square km", a, b)
	}
}

// Rotate moves every cell by the shift, wrapping both axes, in place.
func TestRotate(t *testing.T) {
	const cols, rows = 7, 5
	for _, sh := range [][2]int{{0, 0}, {3, 0}, {0, 2}, {6, 4}, {-1, -3}, {15, 11}} {
		v := make([]int, cols*rows)
		for i := range v {
			v[i] = i
		}
		structure.Rotate(v, cols, rows, sh[0], sh[1])
		for i := range cols * rows {
			c, r := ((i%cols+sh[0])%cols+cols)%cols, ((i/cols+sh[1])%rows+rows)%rows
			if v[r*cols+c] != i {
				t.Fatalf("shift %v: cell %d holds %d at (%d, %d)", sh, i, v[r*cols+c], c, r)
			}
		}
	}
}

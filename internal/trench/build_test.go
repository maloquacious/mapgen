// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package trench

import (
	"math"
	"strings"
	"testing"

	"github.com/maloquacious/mapgen/internal/structure"
)

// The synthetic world is 3000 x 1500 km of 10 km cells: land north of
// y = 600 km, water from there to the southern edge, where the land
// resumes across the seam.
var synthDom = structure.Domain{WidthKM: 3000, HeightKM: 1500}

const synthCell = 10.0

type box struct{ x0, y0, x1, y1 float64 }

func synthGrid(extra ...box) *grid {
	cols, rows := int(synthDom.WidthKM/synthCell), int(synthDom.HeightKM/synthCell)
	land := make([]byte, cols*rows)
	for row := range rows {
		for col := range cols {
			x, y := (float64(col)+0.5)*synthCell, (float64(row)+0.5)*synthCell
			if y < 600 {
				land[row*cols+col] = 1
			}
			for _, b := range extra {
				if x >= b.x0 && x < b.x1 && y >= b.y0 && y < b.y1 {
					land[row*cols+col] = 1
				}
			}
		}
	}
	return newGrid(synthDom, cols, rows, land)
}

// line returns a straight backbone from (x0, y) to (x1, y), sampled every
// 100 km.
func line(x0, x1, y, width float64) []structure.Sample {
	var b []structure.Sample
	step := 100.0
	if x1 < x0 {
		step = -step
	}
	for x := x0; (step > 0 && x <= x1) || (step < 0 && x >= x1); x += step {
		b = append(b, structure.Sample{X: x, Y: y, WidthKM: width})
	}
	return b
}

func margin(id uint32, b []structure.Sample, flank string) structure.Feature {
	key, m := uint64(id), 0.2
	return structure.Feature{ID: id, Type: structure.TypeMountainBelt, Kind: "active_margin", Key: &key,
		Maturity: &m, ElevatedFlank: flank, Backbone: b}
}

func arc(id uint32, b []structure.Sample) structure.Feature {
	key := uint64(id)
	return structure.Feature{ID: id, Type: structure.TypeIslandArc, Key: &key, Backbone: b}
}

func run1(t *testing.T, g *grid, features ...structure.Feature) (*TrenchFile, Diagnostics) {
	t.Helper()
	ff := &structure.FeatureFile{Format: structure.FeaturesFormat, Domain: synthDom, Features: features}
	if err := ff.Validate(synthDom); err != nil {
		t.Fatal(err)
	}
	var d Diagnostics
	tf, err := build(t.Context(), 1, DefaultParams(), g, ff, &d)
	if err != nil {
		t.Fatal(err)
	}
	if err := tf.Validate(ff); err != nil {
		t.Fatal(err)
	}
	return tf, d
}

// footprintWater checks that every cell whose center lies in a trench's
// envelope is water.
func footprintWater(t *testing.T, g *grid, tf *TrenchFile) {
	t.Helper()
	for _, tr := range tf.Trenches {
		for i, in := range structure.Cover(g.dom, g.cols, g.rows, structure.NewLine(g.dom, tr.Backbone)) {
			if in && g.land[i] == 1 {
				t.Fatalf("trench %d covers land cell %d", tr.ID, i)
			}
		}
	}
}

func TestMarginSubductsOppositeItsElevatedFlank(t *testing.T) {
	g := synthGrid()
	// Eastward, right is south: the ocean. The range stands highest north.
	b := line(500, 2500, 450, 200)
	tf, d := run1(t, g, margin(1, b, "left"))
	if got := tf.Subduction; len(got) != 1 || got[0] != (Subduction{Feature: 1, Side: "right", Basis: BasisFlank}) {
		t.Fatalf("subduction %+v", got)
	}
	if len(tf.Trenches) != 1 || d.Trenches != 1 {
		t.Fatalf("%d trenches; omitted %v", len(tf.Trenches), d.Omitted)
	}
	tr := tf.Trenches[0]
	p := DefaultParams()
	half := tr.Backbone[0].WidthKM / 2
	if half < p.WidthKM[0]/2 || half > p.WidthKM[1]/2 {
		t.Errorf("width %g", 2*half)
	}
	for _, s := range tr.Backbone {
		// Offshore of the coast by the least coast gap and half the width.
		if s.Y < 600+p.CoastGapKM[0]+half-1e-6 {
			t.Errorf("vertex (%g, %g) is too near the coast", s.X, s.Y)
		}
		if s.WidthKM != tr.Backbone[0].WidthKM {
			t.Errorf("width varies along the trench")
		}
	}
	r := tr.Targets.RegionalElevationM
	if r[1]-r[0] != 2*p.DepthSpreadM || -r[1] < p.DepthM[0]-p.DepthSpreadM || -r[0] > p.DepthM[1]+p.DepthSpreadM {
		t.Errorf("targets %v", *r)
	}
	footprintWater(t, g, tf)
}

func TestArcSubductsTowardOpenWater(t *testing.T) {
	g := synthGrid()
	// 200 km offshore: the land is near to the north and far to the south.
	for _, tc := range []struct {
		b    []structure.Sample
		want string
	}{{line(500, 2500, 800, 60), "right"}, {line(2500, 500, 800, 60), "left"}} {
		tf, _ := run1(t, g, arc(1, tc.b))
		if got := tf.Subduction[0]; got.Side != tc.want || got.Basis != BasisOcean {
			t.Errorf("subduction %+v, want side %s", got, tc.want)
		}
		for _, tr := range tf.Trenches {
			for _, s := range tr.Backbone {
				if s.Y <= 800 {
					t.Errorf("trench vertex (%g, %g) is on the landward side", s.X, s.Y)
				}
			}
		}
		footprintWater(t, g, tf)
	}
}

func TestTrenchSwingsAroundHeadland(t *testing.T) {
	g := synthGrid(box{1450, 600, 1550, 800})
	tf, _ := run1(t, g, margin(1, line(500, 2500, 450, 200), "left"))
	if len(tf.Trenches) != 1 {
		t.Fatalf("%d trenches", len(tf.Trenches))
	}
	b := tf.Trenches[0].Backbone
	p := DefaultParams()
	half := b[0].WidthKM / 2
	for i, s := range b {
		if s.X >= 1450 && s.X <= 1550 && s.Y < 800+p.CoastGapKM[0]+half-1e-6 {
			t.Errorf("vertex (%g, %g) does not clear the headland", s.X, s.Y)
		}
		if i > 0 {
			if dy := math.Abs(s.Y - b[i-1].Y); dy > p.MaxOffsetSlope*math.Abs(s.X-b[i-1].X)+1e-6 {
				t.Errorf("offset changes by %g km over %g km", dy, math.Abs(s.X-b[i-1].X))
			}
		}
	}
	footprintWater(t, g, tf)
}

func TestTrenchSplitsAroundObstacles(t *testing.T) {
	// A narrow island no probe sees, across the trench's path.
	g := synthGrid(box{1440, 650, 1460, 1300})
	tf, _ := run1(t, g, margin(1, line(500, 2500, 450, 200), "left"))
	if len(tf.Trenches) != 2 || tf.Trenches[0].Parent != 1 || tf.Trenches[1].Parent != 1 {
		t.Fatalf("trenches %+v", tf.Trenches)
	}
	footprintWater(t, g, tf)

	// An unrelated feature blocks; a child of the parent does not.
	g = synthGrid()
	outline := []structure.Point{{X: 1400, Y: 700}, {X: 1600, Y: 700}, {X: 1600, Y: 1100}, {X: 1400, Y: 1100}}
	k := uint64(2)
	basin := structure.Feature{ID: 2, Type: structure.TypeBasin, Kind: "interior", Key: &k, Outline: outline, EdgeKM: 10}
	tf, _ = run1(t, g, margin(1, line(500, 2500, 450, 200), "left"), basin)
	if len(tf.Trenches) != 2 {
		t.Errorf("an unrelated basin: %d trenches, want 2", len(tf.Trenches))
	}
	basin.Kind, basin.Parent, basin.Side = "foreland", 1, "right"
	tf, _ = run1(t, g, margin(1, line(500, 2500, 450, 200), "left"), basin)
	if len(tf.Trenches) != 1 {
		t.Errorf("the parent's own basin: %d trenches, want 1", len(tf.Trenches))
	}
}

func TestShortTrenchIsDropped(t *testing.T) {
	g := synthGrid()
	tf, d := run1(t, g, margin(1, line(500, 700, 450, 200), "left"))
	if len(tf.Subduction) != 1 || len(tf.Trenches) != 0 {
		t.Fatalf("%d subduction records, %d trenches", len(tf.Subduction), len(tf.Trenches))
	}
	if len(d.Omitted) != 1 || !strings.Contains(d.Omitted[0], "shorter than") {
		t.Errorf("omitted %v", d.Omitted)
	}
	// A margin facing land beyond the coast reach has nowhere for a trench.
	g = synthGrid(box{0, 600, 3000, 1400})
	tf, d = run1(t, g, margin(1, line(500, 2500, 300, 100), "left"))
	if len(tf.Trenches) != 0 || len(d.Omitted) != 1 || !strings.Contains(d.Omitted[0], "no clear water") {
		t.Errorf("%d trenches; omitted %v", len(tf.Trenches), d.Omitted)
	}
}

func TestClip(t *testing.T) {
	g := synthGrid()
	axis := []point{{X: 0, Y: 1000}, {X: 600, Y: 1000}, {X: 1000, Y: 1000}}
	// Each station checks 40 km around it: half the width and one cell.
	runs := clip(g, axis, 60, func(q point) bool { return q.X > 395 && q.X < 455 })
	if len(runs) != 2 {
		t.Fatalf("%d runs", len(runs))
	}
	if r := runs[0]; len(r.pts) != 2 || r.pts[0] != (point{X: 0, Y: 1000}) || r.length != 350 {
		t.Errorf("first run %+v", r)
	}
	if r := runs[1]; len(r.pts) != 3 || r.pts[0] != (point{X: 500, Y: 1000}) || r.pts[1] != (point{X: 600, Y: 1000}) || r.length != 500 {
		t.Errorf("second run %+v", r)
	}
	// The width is checked, not only the axis.
	if runs := clip(g, axis, 60, func(q point) bool { return q.Y > 1035 }); len(runs) != 0 {
		t.Errorf("%d runs beside a blocked flank", len(runs))
	}
}

func TestToBase(t *testing.T) {
	got := toBase(synthDom, []point{{X: -10, Y: 1600}, {X: 90, Y: 1700}})
	if got[0] != (point{X: 2990, Y: 100}) || got[1] != (point{X: 3090, Y: 200}) {
		t.Errorf("toBase %v", got)
	}
}

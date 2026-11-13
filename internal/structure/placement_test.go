// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package structure

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/maloquacious/mapgen/internal/continent"
)

// testWorld is a 10,000 x 6,000 km torus of 50 km cells with one
// 6,000 x 3,000 km continent whose north-west corner is at cell (x0, y0).
func testWorld(t *testing.T, x0, y0 int) (Domain, *Coast) {
	t.Helper()
	g := continent.Grid{Columns: 200, Rows: 120, CellWidthKM: 50, CellHeightKM: 50, WidthKM: 10000, HeightKM: 6000}
	c, err := interpretCoast(t.Context(), DefaultCoastParams(), g, boxes(200, 120, [4]int{x0, y0, x0 + 119, y0 + 59}))
	if err != nil {
		t.Fatal(err)
	}
	return Domain{g.WidthKM, g.HeightKM}, c
}

// placeTest places features as if the land were 100 million square km, so
// several of every family are requested.
func placeTest(t *testing.T, seed uint64, dom Domain, c *Coast) ([]Feature, *Diagnostics) {
	t.Helper()
	d := &Diagnostics{Omitted: []string{}}
	fs, err := place(t.Context(), seed, DefaultPlacementParams(), dom, c, 100e6, d)
	if err != nil {
		t.Fatal(err)
	}
	return fs, d
}

func TestPlacement(t *testing.T) {
	// The continent sits across both seams.
	dom, c := testWorld(t, 150, 90)
	p := DefaultPlacementParams()
	fs, d := placeTest(t, 7, dom, c)
	if want := map[string]int{FamilyCollision: 3, FamilyOldOrogen: 5, FamilyRift: 2}; !reflect.DeepEqual(d.Requested, want) {
		t.Fatalf("requested %v, want %v", d.Requested, want)
	}
	ff := &FeatureFile{Format: FeaturesFormat, Domain: dom, Features: fs}
	if err := ff.Validate(dom); err != nil {
		t.Fatal(err)
	}
	if w := ff.Warnings(); len(w) != 0 {
		t.Errorf("warnings: %v", w)
	}
	count := map[string]int{}
	keys := map[uint64]bool{}
	covers := make([][]bool, len(fs))
	for i := range fs {
		f := &fs[i]
		if f.ID != uint32(i+1) || keys[*f.Key] {
			t.Errorf("feature %d: id or key out of order or repeated", f.ID)
		}
		keys[*f.Key] = true
		covers[i] = Cover(dom, c.Columns, c.Rows, f.Shape(dom))
		if !f.Linear() {
			count[f.Type]++
			parent := fs[f.Parent-1]
			if parent.Kind != "collision" || f.Parent >= f.ID {
				t.Errorf("feature %d: parent %d is not an earlier collision belt", f.ID, f.Parent)
			}
			if (f.Type == TypePlateau) != (f.Side == parent.ElevatedFlank) {
				t.Errorf("feature %d: %s on the %s of a belt elevated on the %s", f.ID, f.Type, f.Side, parent.ElevatedFlank)
			}
			continue
		}
		family, fp := FamilyRift, p.Rift
		switch f.Kind {
		case "collision":
			family, fp = FamilyCollision, p.Collision
		case "old_orogen":
			family, fp = FamilyOldOrogen, p.OldOrogen
		}
		count[family]++
		l := NewLine(dom, f.Backbone)
		// Smoothing shortens a backbone a little.
		if got := l.Length(); got < 0.9*fp.LengthKM[0] || got > fp.LengthKM[1] {
			t.Errorf("feature %d: %s is %v km long", f.ID, family, got)
		}
		wide := lineWidth(f.Backbone)
		if wide > fp.WidthKM[1] || wide < 0.9*fp.WidthKM[0] {
			t.Errorf("feature %d: %s is %v km wide", f.ID, family, wide)
		}
		for k, s := range f.Backbone {
			// Inland, with room for at least the end width.
			if got, floor := c.DistanceAt(Point{s.X, s.Y}), 0.8*p.EndWidth*s.WidthKM/2; got < floor {
				t.Errorf("feature %d sample %d: %v km from the coast, width %v", f.ID, k, got, s.WidthKM)
			}
			if s.WidthKM < 0.99*p.EndWidth*wide/(1+1e-9) && s.WidthKM < p.EndWidth*fp.WidthKM[0] {
				t.Errorf("feature %d sample %d: width %v below the end width", f.ID, k, s.WidthKM)
			}
			// No bend tighter than a right angle between samples.
			if k > 0 && k+1 < len(f.Backbone) {
				a, b := f.Backbone[k-1], f.Backbone[k+1]
				if (s.X-a.X)*(b.X-s.X)+(s.Y-a.Y)*(b.Y-s.Y) <= 0 {
					t.Errorf("feature %d: sharp bend at sample %d", f.ID, k)
				}
			}
		}
	}
	// Each family is placed or accounted for, and each collision belt has
	// its plateau and basin or an omission.
	omitted := strings.Join(d.Omitted, "\n")
	for family, n := range d.Requested {
		if got := count[family] + strings.Count(omitted, family+" ") - strings.Count(omitted, "no room for a"); family != FamilyCollision && got != n {
			t.Errorf("%s: %d placed of %d requested; omitted:\n%s", family, count[family], n, omitted)
		}
	}
	if count[FamilyCollision] == 0 || count[FamilyOldOrogen] == 0 || count[TypePlateau] == 0 || count[TypeBasin] == 0 {
		t.Errorf("placed %v on a continent with room for every family", count)
	}
	// Unrelated features do not overlap; a plateau or basin may overlap
	// only its parent.
	for i := range fs {
		for j := i + 1; j < len(fs); j++ {
			if fs[j].Parent == fs[i].ID || fs[i].Parent != 0 && fs[i].Parent == fs[j].Parent {
				continue
			}
			for cell := range covers[i] {
				if covers[i][cell] && covers[j][cell] {
					t.Fatalf("features %d and %d overlap", fs[i].ID, fs[j].ID)
				}
			}
		}
	}
}

func TestPlacementRepeats(t *testing.T) {
	dom, c := testWorld(t, 40, 30)
	a, da := placeTest(t, 7, dom, c)
	b, db := placeTest(t, 7, dom, c)
	if !reflect.DeepEqual(a, b) || !reflect.DeepEqual(da, db) {
		t.Error("the same seed placed different features")
	}
	other, _ := placeTest(t, 8, dom, c)
	if reflect.DeepEqual(a, other) {
		t.Error("a different seed placed the same features")
	}
}

func TestPlacementWithoutRoom(t *testing.T) {
	// No land, and land too small for any belt: every request is omitted
	// after a bounded number of attempts, and nothing is invented.
	g := continent.Grid{Columns: 40, Rows: 20, CellWidthKM: 50, CellHeightKM: 50, WidthKM: 2000, HeightKM: 1000}
	for name, mask := range map[string][]byte{"ocean": make([]byte, 800), "island": boxes(40, 20, [4]int{10, 5, 17, 12}), "land": boxes(40, 20, [4]int{0, 0, 39, 19})} {
		c, err := interpretCoast(t.Context(), DefaultCoastParams(), g, mask)
		if err != nil {
			t.Fatal(err)
		}
		d := &Diagnostics{Omitted: []string{}}
		fs, err := place(t.Context(), 1, DefaultPlacementParams(), Domain{2000, 1000}, c, 1e6, d)
		if err != nil {
			t.Fatal(err)
		}
		ff := &FeatureFile{Format: FeaturesFormat, Domain: Domain{2000, 1000}, Features: fs}
		if err := ff.Validate(ff.Domain); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		// One collision belt and one old orogen are always requested.
		if name != "land" && (len(fs) != 0 || len(d.Omitted) != 2) {
			t.Errorf("%s: %d features, omitted %v", name, len(fs), d.Omitted)
		}
	}
}

func TestResample(t *testing.T) {
	pts := []Point{{0, 0}, {30, 0}, {30, 40}, {100, 40}}
	out := resample(pts, 25)
	if len(out) != 7 || out[0] != pts[0] || out[6] != pts[3] {
		t.Fatalf("resampled to %v", out)
	}
	// 140 km in six equal parts along the path.
	for k, want := range []Point{{0, 0}, {140.0 / 6, 0}, {30, 2*140.0/6 - 30}, {30, 40}, {4*140.0/6 - 70 + 30, 40}} {
		if !near(out[k].X, want.X) || !near(out[k].Y, want.Y) {
			t.Errorf("point %d is %v, want %v", k, out[k], want)
		}
	}
	// Smoothing keeps the ends and straightens the corners.
	s := smoothOpen([]Point{{0, 0}, {10, 0}, {10, 10}, {20, 10}}, 1)
	if want := []Point{{0, 0}, {7.5, 2.5}, {12.5, 7.5}, {20, 10}}; !reflect.DeepEqual(s, want) {
		t.Errorf("smoothed to %v, want %v", s, want)
	}
	if math.Abs(torusDistance(Domain{100, 50}, Point{5, 5}, Point{195, -45})-10) > 1e-9 {
		t.Error("torus distance does not wrap")
	}
}

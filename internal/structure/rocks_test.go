// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package structure

import (
	"math"
	"testing"

	"github.com/maloquacious/mapgen/internal/continent"
)

func TestClassifyRock(t *testing.T) {
	p := DefaultRockParams()
	for _, c := range []struct {
		name                     string
		pressure, shear, ign, rv float64
		want                     Rock
	}{
		{"quiet, low noise", 0, 0, 0, 0.1, RockSedimentary},
		{"quiet, high noise", 0, 0, 0, 0.56, RockMetamorphic},
		{"quiet, middle noise", 0, 0, 0, 0.45, RockIgneous},
		{"igneous influence", 0, 0, 0.3, 0.1, RockIgneous},
		{"igneous influence at the limit", 0, 0, 0.25, 0.1, RockSedimentary},
		{"compression", 1, 0.3, 0, 0.25, RockMetamorphic},
		{"extension counts as stress", -1, 0.3, 0, 0.25, RockMetamorphic},
		// Stress 0.1 and rv 0.4975 sum to 0.095: over the transform limit
		// of 0.09, under the ordinary limit of 0.10.
		{"transform", 0, 0.2, 0, 0.4975, RockMetamorphic},
		{"not transform", 0.2, 0, 0, 0.4975, RockIgneous},
	} {
		if got := ClassifyRock(p, c.pressure, c.shear, c.ign, c.rv); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

// lineCellsAgree checks lineCells against Line.Cross on every cell.
func lineCellsAgree(t *testing.T, dom Domain, cols, rows int, b []Sample, reach float64) int {
	t.Helper()
	got := map[int]float64{}
	if err := lineCells(t.Context(), dom, cols, rows, b, reach, func(cell int, u float64) {
		if _, dup := got[cell]; dup {
			t.Errorf("cell %d reported twice", cell)
		}
		got[cell] = u
	}); err != nil {
		t.Fatal(err)
	}
	half := 0.0
	for _, s := range b {
		half = max(half, s.WidthKM/2)
	}
	l := NewLine(dom, b)
	cw, ch := dom.WidthKM/float64(cols), dom.HeightKM/float64(rows)
	for cell := range cols * rows {
		p := Point{(float64(cell%cols) + 0.5) * cw, (float64(cell/cols) + 0.5) * ch}
		_, d, _ := l.Nearest(p)
		u, ok := got[cell]
		switch {
		case d > reach*half+1e-9 && ok:
			t.Errorf("cell %d at %v km reported beyond %v km", cell, d, reach*half)
		case d < reach*half-1e-9 && !ok:
			t.Errorf("cell %d at %v km missed within %v km", cell, d, reach*half)
		case ok && math.Abs(u-l.Cross(p)) > 1e-9:
			t.Errorf("cell %d: u %v, Cross %v", cell, u, l.Cross(p))
		}
	}
	return len(got)
}

func TestLineCells(t *testing.T) {
	dom := Domain{WidthKM: 1200, HeightKM: 600}
	// A bent belt of varying width across both seams.
	bent := []Sample{{1100, 550, 80}, {1250, 620, 140}, {1400, 600, 60}}
	if n := lineCellsAgree(t, dom, 120, 60, bent, 2); n == 0 {
		t.Error("no cells reported")
	}
	// A belt longer than the world is wide, so its local raster holds
	// several images of some cells.
	long := []Sample{{100, 300, 100}, {500, 320, 100}, {900, 300, 100}, {1300, 280, 100}, {1700, 300, 100}}
	lineCellsAgree(t, dom, 120, 60, long, 1.5)
}

// rockWorld is a 1,200 x 600 km torus of 10 km cells that is all land or
// all ocean, with one province of the given type.
func rockWorld(t *testing.T, kind byte, typ string) (continent.Grid, *ProvinceFile, []uint32, *Coast) {
	t.Helper()
	g := continent.Grid{Columns: 120, Rows: 60, CellWidthKM: 10, CellHeightKM: 10, WidthKM: 1200, HeightKM: 600}
	mask := make([]byte, 120*60)
	owner := make([]uint32, len(mask))
	for i := range mask {
		mask[i], owner[i] = kind, 1
	}
	c, err := interpretCoast(t.Context(), DefaultCoastParams(), g, mask)
	if err != nil {
		t.Fatal(err)
	}
	pf := &ProvinceFile{Format: ProvincesFormat, Provinces: []Province{{ID: 1, Type: typ, Cells: len(mask)}}}
	return g, pf, owner, c
}

func belt(id uint32, kind string, y float64) Feature {
	return Feature{ID: id, Type: TypeMountainBelt, Kind: kind, Key: new(uint64(id)), Maturity: new(0.2), ElevatedFlank: "none",
		Backbone: []Sample{{100, y, 100}, {1100, y, 100}}}
}

func rockFeatures(t *testing.T, g continent.Grid, typ string, features ...Feature) *geology {
	t.Helper()
	g2, pf, owner, c := rockWorld(t, 1, typ)
	if g2 != g {
		t.Fatal("grid mismatch")
	}
	ff := &FeatureFile{Format: FeaturesFormat, Domain: Domain{g.WidthKM, g.HeightKM}, Features: features}
	geo, err := classifyRocks(t.Context(), 1, DefaultRockParams(), g, ff, pf, owner, c)
	if err != nil {
		t.Fatal(err)
	}
	return geo
}

func TestProxyProfiles(t *testing.T) {
	g, _, _, _ := rockWorld(t, 1, TypeShield)
	at := func(geo *geology, name string, x, y float64) float64 {
		return float64(geo.fields[name].Values[int(y/10)*g.Columns+int(x/10)])
	}
	// A collision belt 100 km wide along y = 305: cell centers lie 0, 10,
	// 20, ... km from its backbone, so u is 0, 0.2, 0.4, ...
	geo := rockFeatures(t, g, TypeShield, belt(1, "collision", 305))
	stress := &[2]float64{0.6, 1.5}
	for _, c := range []struct {
		dy, u float64
	}{{0, 0}, {30, 0.6}, {50, 1}, {80, 1.6}, {100, 2}} {
		strength := fade(stress, c.u)
		if c.u <= 0.6 && strength != 1 || c.u >= 1.5 && strength != 0 {
			t.Fatalf("profile at u %v: %v", c.u, strength)
		}
		pressure := 2 * strength
		for _, y := range []float64{305 + c.dy, 305 - c.dy} {
			if p := at(geo, FieldPressure, 600, y); math.Abs(p-pressure) > 1e-6 {
				t.Errorf("pressure at y %v: %v, want %v", y, p, pressure)
			}
			if s := at(geo, FieldShear, 600, y); math.Abs(s-0.3*strength) > 1e-6 {
				t.Errorf("shear at y %v: %v, want %v", y, s, 0.3*strength)
			}
			if i := at(geo, FieldIgneous, 600, y); i != 0 {
				t.Errorf("collision igneous at y %v: %v, want 0", y, i)
			}
		}
	}
	// An active margin's igneous zone reaches farther than its stress.
	geo = rockFeatures(t, g, TypeShield, belt(1, "active_margin", 305))
	for _, c := range []struct{ dy, u float64 }{{0, 0}, {50, 1}, {80, 1.6}, {120, 2.4}, {150, 3}} {
		want := fade(&[2]float64{1, 3}, c.u)
		if i := at(geo, FieldIgneous, 600, 305+c.dy); math.Abs(i-want) > 1e-6 {
			t.Errorf("margin igneous at u %v: %v, want %v", c.u, i, want)
		}
		if c.u >= 1.5 && at(geo, FieldPressure, 600, 305+c.dy) != 0 {
			t.Errorf("margin pressure at u %v is not 0", c.u)
		}
	}
	// A rift across the belt: compression and extension offset each other,
	// and igneous influence is the greater of the two.
	rift := Feature{ID: 2, Type: TypeRift, Key: new(uint64(2)), AxisFraction: new(0.3), Backbone: []Sample{{605, 100, 80}, {605, 500, 80}}}
	geo = rockFeatures(t, g, TypeShield, belt(1, "collision", 305), rift)
	if p := at(geo, FieldPressure, 605, 305); math.Abs(p-0.8) > 1e-6 {
		t.Errorf("pressure where a rift crosses a collision belt: %v, want 0.8", p)
	}
	if i := at(geo, FieldIgneous, 605, 305); math.Abs(i-0.7) > 1e-6 {
		t.Errorf("igneous where a rift crosses a collision belt: %v, want 0.7", i)
	}
	if p := at(geo, FieldPressure, 605, 150); math.Abs(p+1.2) > 1e-6 {
		t.Errorf("pressure in the rift alone: %v, want -1.2", p)
	}
	// A strike-slip fault adds shear only.
	fault := Feature{ID: 3, Type: TypeFault, Sense: "strike_slip", Key: new(uint64(3)), Backbone: []Sample{{105, 100, 40}, {105, 500, 40}}}
	geo = rockFeatures(t, g, TypeShield, fault)
	if s, p := at(geo, FieldShear, 105, 305), at(geo, FieldPressure, 105, 305); s != 1 || p != 0 {
		t.Errorf("strike-slip fault: shear %v, pressure %v", s, p)
	}
	// A collision core is metamorphic throughout.
	geo = rockFeatures(t, g, TypeShield, belt(1, "collision", 305))
	for x := 200.0; x < 1000; x += 10 {
		for _, y := range []float64{285, 305, 325} {
			if r := Rock(geo.rock[int(y/10)*g.Columns+int(x/10)]); r != RockMetamorphic {
				t.Fatalf("collision core at (%v, %v) is %v", x, y, r)
			}
		}
	}
	// No feature: zero stress, and the classification follows the noise.
	geo = rockFeatures(t, g, TypeShield)
	for i, r := range geo.rock {
		rv := float64(geo.fields[FieldRockNoise].Values[i])
		if want := ClassifyRock(DefaultRockParams(), 0, 0, 0, rv); Rock(r) != want {
			t.Fatalf("quiet cell %d is %v, want %v", i, Rock(r), want)
		}
	}
	// A volcanic province.
	geo = rockFeatures(t, g, TypeVolcanic)
	if i := at(geo, FieldIgneous, 600, 300); math.Abs(i-0.6) > 1e-6 {
		t.Errorf("volcanic province igneous %v, want 0.6", i)
	}
}

func TestOceanicIgneous(t *testing.T) {
	// Open ocean with no coast has no shelf, so every cell is igneous or
	// metamorphic.
	g, pf, owner, c := rockWorld(t, 0, TypeOceanic)
	ff := &FeatureFile{Format: FeaturesFormat, Domain: Domain{g.WidthKM, g.HeightKM}}
	geo, err := classifyRocks(t.Context(), 1, DefaultRockParams(), g, ff, pf, owner, c)
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range geo.rock {
		if v := geo.fields[FieldIgneous].Values[i]; math.Abs(float64(v)-0.3) > 1e-6 {
			t.Fatalf("cell %d: igneous %v, want 0.3", i, v)
		}
		if Rock(r) == RockSedimentary {
			t.Fatalf("cell %d of open ocean is sedimentary", i)
		}
	}
	// With a coast, the shelf withholds it.
	g = continent.Grid{Columns: 120, Rows: 60, CellWidthKM: 10, CellHeightKM: 10, WidthKM: 1200, HeightKM: 600}
	mask := boxes(120, 60, [4]int{0, 0, 29, 59}) // a band 300 km wide
	c, err = interpretCoast(t.Context(), DefaultCoastParams(), g, mask)
	if err != nil {
		t.Fatal(err)
	}
	for i := range owner {
		owner[i] = 2 - uint32(mask[i])
	}
	pf = &ProvinceFile{Format: ProvincesFormat, Provinces: []Province{{ID: 1, Type: TypeShield}, {ID: 2, Type: TypeOceanic}}}
	geo, err = classifyRocks(t.Context(), 1, DefaultRockParams(), g, ff, pf, owner, c)
	if err != nil {
		t.Fatal(err)
	}
	ig := geo.fields[FieldIgneous].Values
	// Ocean runs from x = 300 to 1200 km; its middle, at 750, is 450 km
	// offshore, and x = 405 is 105 km offshore.
	if v := ig[30*120+75]; math.Abs(float64(v)-0.3) > 1e-6 {
		t.Errorf("open ocean igneous %v, want 0.3", v)
	}
	if v := ig[30*120+40]; v != 0 {
		t.Errorf("shelf igneous %v, want 0", v)
	}
	if v := ig[30*120+10]; v != 0 {
		t.Errorf("shield igneous %v, want 0", v)
	}
}

func TestRockNoise(t *testing.T) {
	g := continent.Grid{Columns: 200, Rows: 100, CellWidthKM: 20, CellHeightKM: 20, WidthKM: 4000, HeightKM: 2000}
	a, err := rockNoise(t.Context(), 5, DefaultRockParams(), g)
	if err != nil {
		t.Fatal(err)
	}
	b, err := rockNoise(t.Context(), 5, DefaultRockParams(), g)
	if err != nil {
		t.Fatal(err)
	}
	c, err := rockNoise(t.Context(), 6, DefaultRockParams(), g)
	if err != nil {
		t.Fatal(err)
	}
	sum, differ := 0.0, 0
	for i, v := range a {
		if v != b[i] {
			t.Fatalf("cell %d differs between runs", i)
		}
		if !(v >= 0 && v <= 1) {
			t.Fatalf("cell %d: rv %v outside [0, 1]", i, v)
		}
		if v != c[i] {
			differ++
		}
		sum += float64(v)
	}
	// rv is the square of a uniform mid-rank, so its mean is about 1/3.
	if mean := sum / float64(len(a)); math.Abs(mean-1.0/3) > 0.01 {
		t.Errorf("mean rv %v, want about 1/3", mean)
	}
	if differ < len(a)/2 {
		t.Errorf("seeds 5 and 6 differ in only %d of %d cells", differ, len(a))
	}
}

func TestProxiesTranslate(t *testing.T) {
	// Shifting features by whole cells shifts the proxies by the same cells,
	// which is what recentering relies on.
	g, _, _, _ := rockWorld(t, 1, TypeShield)
	// The margin crosses the east/west seam; the shifted copy is left
	// unwrapped, which the queries must not notice.
	f := func(dx, dy float64) []Feature {
		b := belt(1, "active_margin", 205)
		b.Backbone = []Sample{{1000 + dx, 205 + dy, 120}, {1300 + dx, 260 + dy, 160}, {1500 + dx, 240 + dy, 100}}
		return []Feature{b}
	}
	a := rockFeatures(t, g, TypeShield, f(0, 0)...)
	b := rockFeatures(t, g, TypeShield, f(370, 130)...)
	for _, name := range []string{FieldPressure, FieldShear, FieldIgneous} {
		va, vb := a.fields[name].Values, b.fields[name].Values
		for row := range g.Rows {
			for col := range g.Columns {
				sc, sr := (col+37)%g.Columns, (row+13)%g.Rows
				if d := math.Abs(float64(va[row*g.Columns+col] - vb[sr*g.Columns+sc])); d > 1e-5 {
					t.Fatalf("%s: cell (%d, %d) differs by %v after shifting", name, col, row, d)
				}
			}
		}
	}
}

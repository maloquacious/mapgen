// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package structure

import (
	"slices"
	"testing"

	"github.com/maloquacious/mapgen/internal/continent"
)

// paint sets a rectangle of cells, wrapping on both axes.
func paint(mask []byte, cols, rows, x0, y0, w, h int, v byte) {
	for y := y0; y < y0+h; y++ {
		for x := x0; x < x0+w; x++ {
			mask[(y%rows+rows)%rows*cols+(x%cols+cols)%cols] = v
		}
	}
}

// Regions join across both seams; land joins diagonally and ocean does not.
func TestLabelComponents(t *testing.T) {
	const cols, rows = 8, 6
	mask := make([]byte, cols*rows)
	paint(mask, cols, rows, -1, -1, 2, 2, 1) // one block over all four corners
	mask[2*cols+3], mask[3*cols+4] = 1, 1    // a diagonal pair
	c := labelComponents(mask, cols, rows)
	if c.land != 2 || c.ocean != 1 {
		t.Fatalf("%d land and %d ocean regions, want 2 and 1", c.land, c.ocean)
	}
	if a := c.label[0]; a != 1 || c.label[cols-1] != a || c.label[(rows-1)*cols] != a || c.label[rows*cols-1] != a {
		t.Error("corner block is not one region across both seams")
	}
	if c.label[2*cols+3] != 2 || c.label[3*cols+4] != 2 || c.cells[2] != 2 || c.first[2] != 2*cols+3 {
		t.Error("diagonal cells are not one landmass")
	}
	// A diagonal wall of land all the way round the torus splits the ocean
	// into four-connected pieces even though they touch at corners.
	wall := make([]byte, 6*6)
	for i := range 6 {
		wall[i*6+i], wall[i*6+(i+3)%6] = 1, 1
	}
	if c := labelComponents(wall, 6, 6); c.land != 2 || c.ocean != 2 {
		t.Errorf("diagonal walls: %d land and %d ocean regions, want 2 and 2", c.land, c.ocean)
	}
}

func TestSimplifyMask(t *testing.T) {
	const cols, rows = 30, 20
	mask := make([]byte, cols*rows)
	paint(mask, cols, rows, 25, 15, 12, 10, 1) // a landmass over both seams
	paint(mask, cols, rows, 28, 17, 2, 2, 0)   // a puddle in it
	paint(mask, cols, rows, 0, 0, 1, 1, 0)     // and a one-cell puddle
	paint(mask, cols, rows, 12, 8, 3, 3, 1)    // an island of 9 cells
	paint(mask, cols, rows, 18, 3, 2, 1, 1)    // a speck of 2 cells
	out, c := simplifyMask(mask, cols, rows, 5)
	if c.land != 2 || c.ocean != 1 || c.cells[1] != 120 || c.cells[2] != 9 {
		t.Fatalf("regions: %d land %d ocean, sizes %v", c.land, c.ocean, c.cells)
	}
	if out[18*cols+29] != 1 || out[0] != 1 || out[3*cols+18] != 0 || out[9*cols+13] != 1 {
		t.Error("puddles were not filled, the speck not removed, or the island not kept")
	}
	if mask[18*cols+29] != 0 {
		t.Error("input mask was changed")
	}
}

// Two seeds on a uniform strip meet halfway; the tie goes to the lower owner.
func TestGrowMeetsHalfway(t *testing.T) {
	const cols, rows = 60, 9
	land := make([]byte, cols*rows)
	paint(land, cols, rows, 5, 3, 41, 3, 1) // columns 5..45, rows 3..5
	owner := make([]uint32, len(land))
	seeds := map[int32]uint32{4*cols + 10: 7, 4*cols + 40: 3}
	if err := grow(t.Context(), land, cols, rows, 6, 6, seeds, func(int) float64 { return 1 }, owner); err != nil {
		t.Fatal(err)
	}
	for i, o := range owner {
		x := i % cols
		var want uint32
		switch {
		case land[i] == 0:
		case x < 25:
			want = 7
		default: // column 25 is equidistant
			want = 3
		}
		if o != want {
			t.Fatalf("cell (%d,%d) owner %d, want %d", x, i/cols, o, want)
		}
	}
	// A costly band moves the boundary toward the seed behind it.
	owner = make([]uint32, len(land))
	slow := func(cell int) float64 {
		if x := cell % cols; x >= 30 && x < 40 {
			return 3
		}
		return 1
	}
	if err := grow(t.Context(), land, cols, rows, 6, 6, seeds, slow, owner); err != nil {
		t.Fatal(err)
	}
	if owner[4*cols+30] != 7 || owner[4*cols+38] != 3 {
		t.Errorf("costly band: owners %d and %d", owner[4*cols+30], owner[4*cols+38])
	}
}

// world is 200 x 100 cells of 50 km: a continent of 18 million square km
// over both seams, an island, a speck, and a lake.
func world() (continent.Grid, []byte, []float32) {
	const cols, rows = 200, 100
	g := continent.Grid{Columns: cols, Rows: rows, WidthKM: 10000, HeightKM: 5000, CellWidthKM: 50, CellHeightKM: 50}
	land := make([]byte, cols*rows)
	paint(land, cols, rows, 150, 70, 120, 60, 1)
	paint(land, cols, rows, 100, 40, 6, 6, 1) // 90,000 square km
	paint(land, cols, rows, 120, 50, 1, 1, 1) // 2,500 square km
	paint(land, cols, rows, 10, 10, 2, 2, 0)  // 10,000 square km
	// Continental values fall away from the continent's center at (10, 0).
	values := make([]float32, cols*rows)
	for i := range values {
		dx, dy := (i%cols-10+cols+cols/2)%cols-cols/2, (i/cols+rows+rows/2)%rows-rows/2
		values[i] = float32(1000 - dx*dx - dy*dy)
	}
	return g, land, values
}

func TestBuildProvinces(t *testing.T) {
	g, land, values := world()
	p := DefaultProvinceParams()
	pf, owner, err := buildProvinces(t.Context(), 1, p, g, land, values)
	if err != nil {
		t.Fatal(err)
	}
	if err := pf.validate(owner); err != nil {
		t.Fatal(err)
	}
	// 18 million square km at one seed per 1.5 million, the island, the ocean.
	if len(pf.Provinces) != 14 {
		t.Fatalf("%d provinces, want 14", len(pf.Provinces))
	}
	island, ocean := pf.Provinces[12], pf.Provinces[13]
	if island.Component != 2 || island.Cells != 36 || island.Type != TypeSedimentary || ocean.Type != TypeOceanic || ocean.Component != 0 {
		t.Errorf("island %+v, ocean %+v", island, ocean)
	}
	if owner[50*g.Columns+120] != ocean.ID || owner[10*g.Columns+10] == ocean.ID {
		t.Error("the speck is not in the ocean province, or the lake is not in a land province")
	}
	total, shields := 0, 0
	for _, pr := range pf.Provinces {
		total += pr.Cells
		if pr.AreaKM2 != float64(pr.Cells)*2500 {
			t.Errorf("province %d area %g for %d cells", pr.ID, pr.AreaKM2, pr.Cells)
		}
		if pr.Component == 1 {
			if pr.Cells < 100 {
				t.Errorf("province %d has only %d cells", pr.ID, pr.Cells)
			}
			if pr.Type == TypeShield {
				shields++
			}
		}
		// The seed lies in its own province.
		if o := owner[int(pr.Seed.Y/50)*g.Columns+int(pr.Seed.X/50)]; o != pr.ID {
			t.Errorf("province %d's seed is owned by %d", pr.ID, o)
		}
	}
	if total != len(land) {
		t.Errorf("provinces cover %d of %d cells", total, len(land))
	}
	if shields < 4 || shields > 6 { // half of 12, less any volcanic draws
		t.Errorf("%d shield provinces", shields)
	}
	// Every province is one connected region: land through eight neighbors.
	// A filled lake rides along with the land around it.
	for _, pr := range pf.Provinces {
		mask := make([]byte, len(owner))
		for i, o := range owner {
			if o == pr.ID {
				mask[i] = 1
			}
		}
		if c := labelComponents(mask, g.Columns, g.Rows); c.land != 1 {
			t.Errorf("province %d is in %d pieces", pr.ID, c.land)
		}
	}
	// Shields are the provinces nearest the high-valued center.
	mean := func(pr Province) float64 {
		s := 0.0
		for i, o := range owner {
			if o == pr.ID {
				s += float64(values[i])
			}
		}
		return s / float64(pr.Cells)
	}
	lowShield, highOther := 1e18, -1e18
	for _, pr := range pf.Provinces[:12] {
		switch pr.Type {
		case TypeShield:
			lowShield = min(lowShield, mean(pr))
		case TypeSedimentary:
			highOther = max(highOther, mean(pr))
		}
	}
	if lowShield < highOther {
		t.Errorf("a sedimentary province (%g) outranks a shield (%g)", highOther, lowShield)
	}

	// The same seed repeats exactly; another seed moves the boundaries.
	pf2, owner2, err := buildProvinces(t.Context(), 1, p, g, land, values)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(owner, owner2) || !slices.Equal(pf.Provinces, pf2.Provinces) {
		t.Error("the same seed gave a different partition")
	}
	_, owner3, err := buildProvinces(t.Context(), 2, p, g, land, values)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Equal(owner, owner3) {
		t.Error("a different seed gave the same partition")
	}
}

// Worlds with no coast are a single province.
func TestProvincesWithoutCoast(t *testing.T) {
	g := continent.Grid{Columns: 20, Rows: 10, WidthKM: 1000, HeightKM: 500, CellWidthKM: 50, CellHeightKM: 50}
	values := make([]float32, 200)
	for kind, want := range map[byte]string{0: TypeOceanic, 1: TypeShield} {
		land := make([]byte, 200)
		for i := range land {
			land[i] = kind
		}
		pf, owner, err := buildProvinces(t.Context(), 1, DefaultProvinceParams(), g, land, values)
		if err != nil {
			t.Fatal(err)
		}
		if len(pf.Provinces) != 1 || pf.Provinces[0].Type != want || pf.Provinces[0].Cells != 200 || pf.validate(owner) != nil {
			t.Errorf("mask of %d: %+v", kind, pf.Provinces)
		}
	}
}

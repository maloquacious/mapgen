// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package landmask

import (
	"errors"
	"slices"
	"testing"
)

// basin is an 8 by 6 torus: an ocean band in rows 0 and 5 that is one band
// across the north/south seam, land at 100 m with a low coastal cell at
// 30 m, and a one-cell inland basin at -40 m. The datum is 10 m off
// sea level so the shift shows.
func basin() (elev, high []float32, cols, rows int) {
	cols, rows = 8, 6
	elev = make([]float32, cols*rows)
	for i := range elev {
		switch y := i / cols; {
		case y == 0 || y == rows-1:
			elev[i] = -90
		default:
			elev[i] = 110
		}
	}
	elev[1*cols+7] = 30  // coastal, next to row 0
	elev[3*cols+3] = -40 // basin
	high = make([]float32, len(elev))
	for i, e := range elev {
		high[i] = e + 25
	}
	return elev, high, cols, rows
}

func TestSelectOcean(t *testing.T) {
	elev, high, cols, rows := basin()
	n := cols * rows
	// The 16 ocean-band cells and the basin cell are the 17 lowest.
	k := n - 17
	p := DefaultOceanParams()
	p.MaxRepairFraction = 1
	s, err := selectOcean(t.Context(), p, elev, high, cols, rows, k)
	if err != nil {
		t.Fatal(err)
	}
	// The quantile's lowest land cell is at 30 m and its highest water at
	// -40 m, so the level is -5 m.
	if s.level != -5 {
		t.Fatalf("level %g, want -5", s.level)
	}
	if s.components != 2 || s.component != 16 {
		t.Fatalf("components %d, largest %d; want 2 and 16", s.components, s.component)
	}
	ocean := 0
	for i, l := range s.land {
		if l == 0 {
			ocean++
			if y := i / cols; y != 0 && y != rows-1 && i != 1*cols+7 {
				t.Errorf("cell %d joined the ocean", i)
			}
		}
	}
	if ocean != n-k {
		t.Fatalf("%d ocean cells, want %d", ocean, n-k)
	}
	// The basin stays out, and the coastal cell, the lowest on the
	// frontier, joins: two cells differ from the quantile mask.
	if s.repair != 2 || s.land[3*cols+3] != 1 || s.land[1*cols+7] != 0 {
		t.Fatalf("repair %d, basin %d, coastal %d", s.repair, s.land[3*cols+3], s.land[1*cols+7])
	}
	if s.depressions != 1 {
		t.Fatalf("%d depressions, want 1", s.depressions)
	}
	if !connected(s.land, cols, rows) {
		t.Fatal("ocean is not connected")
	}
	// Shifted by 5 m and clamped: the basin rises from -35 m to +1 m, the
	// coastal cell falls from 35 m to -1 m; high moves with elevation.
	for i, want := range map[int]float32{3*cols + 3: 1, 1*cols + 7: -1, 0: -85, 2 * cols: 115} {
		if elev[i] != want {
			t.Errorf("cell %d: elevation %g, want %g", i, elev[i], want)
		}
		if high[i] != elev[i]+25 {
			t.Errorf("cell %d: high %g, want %g", i, high[i], elev[i]+25)
		}
	}
	c := s.clamp
	if c.RaisedCells != 1 || c.MaxRaiseM != 36 || c.LoweredCells != 1 || c.MaxLowerM != 36 {
		t.Fatalf("clamp %+v", c)
	}
}

func TestSelectOceanTies(t *testing.T) {
	// Every cell is tied: the quantile ocean is the last n-k cells, by
	// index, and growth breaks ties by index too.
	cols, rows := 5, 4
	n := cols * rows
	elev, high := make([]float32, n), make([]float32, n)
	p := DefaultOceanParams()
	s, err := selectOcean(t.Context(), p, slices.Clone(elev), high, cols, rows, 12)
	if err != nil {
		t.Fatal(err)
	}
	want := make([]byte, n)
	for i := range 12 {
		want[i] = 1
	}
	if !slices.Equal(s.land, want) || s.repair != 0 {
		t.Fatalf("land %v, repair %d", s.land, s.repair)
	}
	for _, k := range []int{0, n} {
		s, err := selectOcean(t.Context(), p, slices.Clone(elev), slices.Clone(high), cols, rows, k)
		if err != nil {
			t.Fatal(err)
		}
		if c := slices.Index(s.land, byte(1-min(k, 1))); c >= 0 {
			t.Fatalf("k=%d: cell %d on the wrong side", k, c)
		}
	}
}

func TestSelectOceanFailures(t *testing.T) {
	elev, high, cols, rows := basin()
	k := cols*rows - 17
	// Two cells of 48 is far over 1 percent.
	_, err := selectOcean(t.Context(), DefaultOceanParams(), slices.Clone(elev), slices.Clone(high), cols, rows, k)
	if !errors.Is(err, ErrRepairFraction) {
		t.Fatalf("got %v, want %v", err, ErrRepairFraction)
	}
	// A basin 600 m down cannot be raised to +1 m.
	elev[3*cols+3] = -600
	p := DefaultOceanParams()
	p.MaxRepairFraction = 1
	_, err = selectOcean(t.Context(), p, elev, high, cols, rows, k)
	if !errors.Is(err, ErrClamp) {
		t.Fatalf("got %v, want %v", err, ErrClamp)
	}
}

func TestConnected(t *testing.T) {
	cols, rows := 6, 4
	land := make([]byte, cols*rows)
	for i := range land {
		land[i] = 1
	}
	// Two ocean cells touching diagonally across both seams.
	land[0], land[cols*rows-1] = 0, 0
	if !connected(land, cols, rows) {
		t.Fatal("diagonal across the corner seams is not connected")
	}
	land[cols*rows-1], land[2*cols+3] = 1, 0
	if connected(land, cols, rows) {
		t.Fatal("separate cells are connected")
	}
}

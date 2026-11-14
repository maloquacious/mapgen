// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package fracture_test

import (
	"testing"

	"github.com/maloquacious/mapgen/internal/fracture"
)

// An enclosed lake stays land, and the ocean takes the lowest land on its
// edge instead, so exactly k cells are land and the ocean is connected.
func TestConnectedOcean(t *testing.T) {
	cols, rows := 7, 5
	// Ocean in column 0 and column 6, one band across the east-west seam;
	// land at 5 with a coastal cell at 2 in column 5 and a lake at -1.
	v := make([]float32, cols*rows)
	for i := range v {
		switch x := i % cols; x {
		case 0, cols - 1:
			v[i] = -3
		default:
			v[i] = 5
		}
	}
	v[2*cols+5] = 2  // coastal
	v[2*cols+3] = -1 // lake
	n := cols * rows
	k := n - 11 // the 10 band cells and the lake
	o, err := fracture.ConnectedOcean(t.Context(), v, cols, rows, k)
	if err != nil {
		t.Fatal(err)
	}
	if o.Components != 2 || o.Largest != 10 || o.Repair != 2 {
		t.Fatalf("components %d, largest %d, repair %d", o.Components, o.Largest, o.Repair)
	}
	land := 0
	for _, l := range o.Land {
		if l {
			land++
		}
	}
	if land != k || !o.Land[2*cols+3] || o.Land[2*cols+5] || o.Quantile[2*cols+3] {
		t.Fatalf("land %d, lake %v, coastal %v", land, o.Land[2*cols+3], o.Land[2*cols+5])
	}
	if !fracture.OceanConnected(func(i int) bool { return o.Land[i] }, cols, rows) ||
		fracture.OceanConnected(func(i int) bool { return o.Quantile[i] }, cols, rows) {
		t.Fatal("connectivity")
	}
	for _, k := range []int{0, n} {
		o, err := fracture.ConnectedOcean(t.Context(), v, cols, rows, k)
		if err != nil {
			t.Fatal(err)
		}
		if o.Repair != 0 {
			t.Fatalf("k=%d: repair %d", k, o.Repair)
		}
	}
	if _, err := fracture.ConnectedOcean(t.Context(), v, cols, rows, n+1); err == nil {
		t.Fatal("accepted k > n")
	}
}

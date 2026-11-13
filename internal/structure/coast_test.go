// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package structure

import (
	"math"
	"slices"
	"testing"

	"github.com/maloquacious/mapgen/internal/continent"
)

// boxes returns a cols x rows mask with the listed inclusive cell boxes
// {x0, y0, x1, y1} set to land, wrapping on both axes.
func boxes(cols, rows int, list ...[4]int) []byte {
	m := make([]byte, cols*rows)
	for _, b := range list {
		for y := b[1]; y <= b[3]; y++ {
			for x := b[0]; x <= b[2]; x++ {
				m[(y%rows+rows)%rows*cols+(x%cols+cols)%cols] = 1
			}
		}
	}
	return m
}

// shift moves a raster dx cells east and dy cells south on the torus.
func shift[T any](in []T, cols, rows, dx, dy int) []T {
	out := make([]T, len(in))
	for i, v := range in {
		out[(i/cols+dy)%rows*cols+(i%cols+dx)%cols] = v
	}
	return out
}

// area is the shoelace area of a closed chain: positive when it runs
// clockwise on a map with north up, which puts its inside on the right.
func area(pts []Point) float64 {
	a := 0.0
	for k, p := range pts {
		q := pts[(k+1)%len(pts)]
		a += p.X*q.Y - q.X*p.Y
	}
	return a / 2
}

func near(a, b float64) bool { return math.Abs(a-b) <= 1e-9*max(1, math.Abs(a), math.Abs(b)) }

func TestCoastDistanceBand(t *testing.T) {
	// Land in rows 3..6 of 12, all the way round: two straight coasts.
	const cols, rows, cw, ch = 8, 12, 50.0, 40.0
	d, err := coastDistance(t.Context(), boxes(cols, rows, [4]int{0, 3, cols - 1, 6}), cols, rows, cw, ch)
	if err != nil {
		t.Fatal(err)
	}
	// Cell centers of rows 0..11 against interfaces at y = 3 and y = 7 rows.
	want := []float64{-2.5, -1.5, -0.5, 0.5, 1.5, 1.5, 0.5, -0.5, -1.5, -2.5, -3.5, -3.5}
	for y, w := range want {
		for x := range cols {
			if got := d[y*cols+x]; !near(got, w*ch) {
				t.Fatalf("row %d col %d: distance %v, want %v", y, x, got, w*ch)
			}
		}
	}
}

func TestCoastDistanceIsland(t *testing.T) {
	// One land cell: the interface is its four edges.
	const cols, rows, cw, ch = 9, 7, 50.0, 30.0
	d, err := coastDistance(t.Context(), boxes(cols, rows, [4]int{4, 3, 4, 3}), cols, rows, cw, ch)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		x, y int
		want float64
	}{
		{4, 3, min(cw, ch) / 2},             // the island: nearest edge
		{5, 3, -cw / 2},                     // east neighbor
		{6, 3, -1.5 * cw},                   // two east
		{4, 2, -ch / 2},                     // north neighbor
		{5, 4, -math.Hypot(cw/2, ch/2)},     // diagonal: nearest point is the corner
		{6, 5, -math.Hypot(1.5*cw, 1.5*ch)}, // two diagonal
		{0, 3, -3.5 * cw},                   // west along the row
		{8, 3, -3.5 * cw},                   // and east, the same distance
		{4, 0, -2.5 * ch},                   // north along the column
		{4, 6, -2.5 * ch},
	} {
		if got := d[c.y*cols+c.x]; !near(got, c.want) {
			t.Errorf("cell (%d,%d): distance %v, want %v", c.x, c.y, got, c.want)
		}
	}
}

func TestCoastDistanceTranslates(t *testing.T) {
	const cols, rows, cw, ch = 20, 14, 50.0, 45.0
	mask := boxes(cols, rows, [4]int{2, 2, 7, 5}, [4]int{6, 6, 9, 10}, [4]int{14, 1, 15, 1})
	d, err := coastDistance(t.Context(), mask, cols, rows, cw, ch)
	if err != nil {
		t.Fatal(err)
	}
	// Brute force: the nearest point of every cell of the other kind, over
	// the nine periodic images.
	for i := range mask {
		best := math.Inf(1)
		for j := range mask {
			if mask[j] == mask[i] {
				continue
			}
			dx, dy := math.Abs(float64(i%cols-j%cols)), math.Abs(float64(i/cols-j/cols))
			dx, dy = min(dx, cols-dx), min(dy, rows-dy)
			best = min(best, math.Hypot(max(dx-0.5, 0)*cw, max(dy-0.5, 0)*ch))
		}
		if mask[i] == 0 {
			best = -best
		}
		if !near(d[i], best) {
			t.Fatalf("cell %d: distance %v, brute force %v", i, d[i], best)
		}
	}
	// Moving the mask across both seams moves the distances with it.
	moved, err := coastDistance(t.Context(), shift(mask, cols, rows, 15, 11), cols, rows, cw, ch)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(moved, shift(d, cols, rows, 15, 11)) {
		t.Error("coast distance changed when the mask moved across the seams")
	}
}

func TestChainRectangle(t *testing.T) {
	const cols, rows, cw, ch = 12, 10, 50.0, 40.0
	// A 4 x 3 island, and the same island across the corner of the map.
	for _, x0 := range []int{3, 10} {
		y0 := x0 - 1
		chains := traceChains(boxes(cols, rows, [4]int{x0, y0, x0 + 3, y0 + 2}), cols, rows, cw, ch, 0)
		if len(chains) != 1 {
			t.Fatalf("island at %d: %d chains, want 1", x0, len(chains))
		}
		c := chains[0]
		// Edge midpoints: 14 edges; each corner cuts across half a cell.
		want := 2*3*cw + 2*2*ch + 4*math.Hypot(cw/2, ch/2)
		if len(c.Points) != 14 || !near(c.LengthKM, want) || c.Wrap != [2]int{} {
			t.Errorf("island at %d: %d points, length %v (want %v), wrap %v", x0, len(c.Points), c.LengthKM, want, c.Wrap)
		}
		if !near(c.LandAreaKM2, 12*cw*ch) || !near(c.WaterAreaKM2, (cols*rows-12)*cw*ch) {
			t.Errorf("island at %d: land %v, water %v", x0, c.LandAreaKM2, c.WaterAreaKM2)
		}
		if a := area(c.Points); a <= 0 {
			t.Errorf("island at %d: area %v; land is not on the right", x0, a)
		}
		// Land on the right, by the same rule belts use.
		p, q := c.Points[0], c.Points[1]
		l := NewLine(Domain{cols * cw, rows * ch}, []Sample{{p.X, p.Y, 10}, {q.X, q.Y, 10}})
		center := Point{(float64(x0) + 2) * cw, (float64(y0) + 1.5) * ch}
		if _, _, side := l.Nearest(center); side != Right {
			t.Errorf("island at %d: its center is on the %v of the chain", x0, side)
		}
	}
}

func TestChainLake(t *testing.T) {
	// All land but a 2 x 2 lake: one chain, running the other way.
	const cols, rows = 8, 8
	mask := boxes(cols, rows, [4]int{0, 0, cols - 1, rows - 1})
	for _, i := range []int{3*cols + 3, 3*cols + 4, 4*cols + 3, 4*cols + 4} {
		mask[i] = 0
	}
	chains := traceChains(mask, cols, rows, 50, 50, 0)
	if len(chains) != 1 {
		t.Fatalf("%d chains, want 1", len(chains))
	}
	if a := area(chains[0].Points); a >= 0 {
		t.Errorf("lake chain area %v; land is not on the right", a)
	}
	if got := chains[0].WaterAreaKM2; !near(got, 4*50*50) {
		t.Errorf("lake area %v", got)
	}
}

func TestChainDiagonals(t *testing.T) {
	const cols, rows = 8, 8
	// Two land cells touching at a corner are one landmass: one chain.
	land := boxes(cols, rows, [4]int{2, 2, 2, 2}, [4]int{3, 3, 3, 3})
	if chains := traceChains(land, cols, rows, 50, 50, 0); len(chains) != 1 || len(chains[0].Points) != 8 {
		t.Errorf("diagonal land: %d chains, want one of 8 edges", len(chains))
	}
	// Two water cells touching at a corner are separate waters: two chains.
	water := boxes(cols, rows, [4]int{0, 0, cols - 1, rows - 1})
	water[2*cols+2], water[3*cols+3] = 0, 0
	chains := traceChains(water, cols, rows, 50, 50, 0)
	if len(chains) != 2 || len(chains[0].Points) != 4 || len(chains[1].Points) != 4 {
		t.Errorf("diagonal water: %d chains, want two of 4 edges", len(chains))
	}
	// Either way the chains agree with the region labels.
	for _, mask := range [][]byte{land, water} {
		c := labelComponents(mask, cols, rows)
		if got := len(traceChains(mask, cols, rows, 50, 50, 0)); got != c.land+c.ocean-1 {
			t.Errorf("%d chains for %d land and %d water regions", got, c.land, c.ocean)
		}
	}
}

func TestChainRoundTheWorld(t *testing.T) {
	// A band of land right round the world has two coasts that never turn;
	// each closes only by wrapping, in opposite directions.
	const cols, rows, cw, ch = 10, 8, 50.0, 40.0
	for _, vertical := range []bool{false, true} {
		box, want, axis := [4]int{0, 2, cols - 1, 4}, cols*cw, 0
		if vertical {
			box, want, axis = [4]int{3, 0, 5, rows - 1}, rows*ch, 1
		}
		for _, passes := range []int{0, 5} {
			chains := traceChains(boxes(cols, rows, box), cols, rows, cw, ch, passes)
			if len(chains) != 2 {
				t.Fatalf("vertical %v: %d chains, want 2", vertical, len(chains))
			}
			a, b := chains[0], chains[1]
			if a.Wrap[axis]*b.Wrap[axis] != -1 || a.Wrap[1-axis] != 0 || b.Wrap[1-axis] != 0 {
				t.Errorf("vertical %v: wraps %v and %v", vertical, a.Wrap, b.Wrap)
			}
			if !near(a.LengthKM, want) || !near(b.LengthKM, want) {
				t.Errorf("vertical %v, %d passes: lengths %v and %v, want %v", vertical, passes, a.LengthKM, b.LengthKM, want)
			}
		}
	}
}

func TestChainsTranslate(t *testing.T) {
	const cols, rows = 20, 14
	mask := boxes(cols, rows, [4]int{2, 2, 7, 5}, [4]int{6, 6, 9, 10}, [4]int{14, 1, 15, 1})
	mask[3*cols+4] = 0 // a lake
	lengths := func(m []byte) []float64 {
		var out []float64
		for _, c := range traceChains(m, cols, rows, 50, 45, 4) {
			out = append(out, math.Round(c.LengthKM*1e6)/1e6)
		}
		slices.Sort(out)
		return out
	}
	want := lengths(mask)
	if len(want) != 3 {
		t.Fatalf("%d chains, want 3", len(want))
	}
	if got := lengths(shift(mask, cols, rows, 15, 11)); !slices.Equal(got, want) {
		t.Errorf("chain lengths %v after moving across the seams, want %v", got, want)
	}
}

func TestSmoothingStaysNearTheCoast(t *testing.T) {
	// A large island: smoothing rounds the corners but keeps every point
	// within a cell or two of the interface, and keeps land on the right.
	const cols, rows, cw, ch = 40, 30, 50.0, 50.0
	mask := boxes(cols, rows, [4]int{8, 6, 27, 21})
	c := &Coast{Columns: cols, Rows: rows, CellWidthKM: cw, CellHeightKM: ch, Land: mask}
	var err error
	if c.Distance, err = coastDistance(t.Context(), mask, cols, rows, cw, ch); err != nil {
		t.Fatal(err)
	}
	c.Chains = traceChains(mask, cols, rows, cw, ch, 5)
	raw := traceChains(mask, cols, rows, cw, ch, 0)[0]
	smooth := c.Chains[0]
	if smooth.LengthKM >= raw.LengthKM || smooth.LengthKM < 0.95*raw.LengthKM {
		t.Errorf("smoothed length %v, raw %v", smooth.LengthKM, raw.LengthKM)
	}
	if area(smooth.Points) <= 0 {
		t.Error("smoothing reversed the chain")
	}
	for _, p := range smooth.Points {
		if d := c.DistanceAt(p); math.Abs(d) > 1.5*cw {
			t.Fatalf("smoothed point %v is %v km from the coast", p, d)
		}
	}
}

func TestInterpretCoast(t *testing.T) {
	// 6 km cells on a 600 x 300 km world: a 12 x 6 working grid of 50 km
	// cells, each covering 8 or 9 full-resolution cells a side.
	g := continent.Grid{Columns: 100, Rows: 50, CellWidthKM: 6, CellHeightKM: 6, WidthKM: 600, HeightKM: 300}
	land := boxes(100, 50, [4]int{20, 10, 59, 39})
	land[0] = 1 // a speck: far below half of its working cell
	c, err := interpretCoast(t.Context(), DefaultCoastParams(), g, land)
	if err != nil {
		t.Fatal(err)
	}
	if c.Columns != 12 || c.Rows != 6 || !near(c.CellWidthKM, 50) || !near(c.CellHeightKM, 50) {
		t.Fatalf("working grid %d x %d of %v x %v km", c.Columns, c.Rows, c.CellWidthKM, c.CellHeightKM)
	}
	s := c.Summary()
	if s.LandCells == 0 || c.Land[0] != 0 || len(c.Chains) != 1 {
		t.Fatalf("%d working land cells, %d chains, speck kept: %v", s.LandCells, len(c.Chains), c.Land[0] == 1)
	}
	// The working mask's area is close to the land's.
	if got, want := float64(s.LandCells)*2500, 40*30*36.0; math.Abs(got-want) > 0.2*want {
		t.Errorf("working land area %v, land area %v", got, want)
	}
	if s.MaxInlandKM <= 0 || s.MaxOffshoreKM <= 0 || !near(s.TotalLengthKM, c.Chains[0].LengthKM) {
		t.Errorf("summary %+v", s)
	}
	// Interpolation: positive well inside, negative well outside.
	if d := c.DistanceAt(Point{240, 150}); d <= 0 {
		t.Errorf("distance inside the land is %v", d)
	}
	if d := c.DistanceAt(Point{540, 20}); d >= 0 {
		t.Errorf("distance in the ocean is %v", d)
	}
}

func TestNoCoast(t *testing.T) {
	g := continent.Grid{Columns: 40, Rows: 20, CellWidthKM: 6, CellHeightKM: 6, WidthKM: 240, HeightKM: 120}
	for kind, sign := range map[byte]int{0: -1, 1: 1} {
		mask := make([]byte, 40*20)
		for i := range mask {
			mask[i] = kind
		}
		c, err := interpretCoast(t.Context(), DefaultCoastParams(), g, mask)
		if err != nil {
			t.Fatal(err)
		}
		if len(c.Chains) != 0 {
			t.Errorf("kind %d: %d chains on a world with no coast", kind, len(c.Chains))
		}
		if d := c.DistanceAt(Point{100, 50}); !math.IsInf(d, sign) {
			t.Errorf("kind %d: distance %v, want infinite", kind, d)
		}
		if s := c.Summary(); s.MaxInlandKM != 0 || s.MaxOffshoreKM != 0 || s.Chains == nil {
			t.Errorf("kind %d: summary %+v", kind, s)
		}
	}
}

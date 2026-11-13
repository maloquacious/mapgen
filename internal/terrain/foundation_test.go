// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package terrain

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/maloquacious/mapgen/internal/continent"
	"github.com/maloquacious/mapgen/internal/fracture"
	"github.com/maloquacious/mapgen/internal/structure"
)

// bruteDistance measures every cell against every cell of the other kind.
func bruteDistance(land []byte, cols, rows int, cw, ch float64) []float64 {
	out := make([]float64, len(land))
	for i := range land {
		best := math.Inf(1)
		for j := range land {
			if land[j] == land[i] {
				continue
			}
			di := abs(i%cols - j%cols)
			dj := abs(i/cols - j/cols)
			di, dj = min(di, cols-di), min(dj, rows-dj)
			x, y := cw*max(0, float64(di)-0.5), ch*max(0, float64(dj)-0.5)
			best = min(best, x*x+y*y)
		}
		out[i] = math.Sqrt(best)
		if land[i] == 0 {
			out[i] = -out[i]
		}
	}
	return out
}

func abs(v int) int { return max(v, -v) }

func TestCoastDistanceMatchesBruteForce(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for trial := range 40 {
		cols, rows := 1+rng.IntN(24), 1+rng.IntN(24)
		cw, ch := 1+5*rng.Float64(), 1+5*rng.Float64()
		share := rng.Float64()
		land := make([]byte, cols*rows)
		for i := range land {
			if rng.Float64() < share {
				land[i] = 1
			}
		}
		got, err := coastDistance(t.Context(), land, cols, rows, cw, ch)
		if err != nil {
			t.Fatal(err)
		}
		want := bruteDistance(land, cols, rows, cw, ch)
		for i := range got {
			if g, w := float64(got[i]), want[i]; !(g == w || math.Abs(g-w) <= 1e-5*max(1, math.Abs(w))) {
				t.Fatalf("trial %d (%d x %d, cells %g x %g km): cell %d is %g, want %g", trial, cols, rows, cw, ch, i, g, w)
			}
		}
	}
}

// band returns a cols x rows mask with land in rows [r0, r1).
func band(cols, rows, r0, r1 int) []byte {
	land := make([]byte, cols*rows)
	for r := r0; r < r1; r++ {
		for c := range cols {
			land[r*cols+c] = 1
		}
	}
	return land
}

func TestCoastDistanceBand(t *testing.T) {
	const cols, rows, cw, ch = 7, 40, 6, 5
	land := band(cols, rows, 10, 30)
	got, err := coastDistance(t.Context(), land, cols, rows, cw, ch)
	if err != nil {
		t.Fatal(err)
	}
	for r := range rows {
		// The interfaces lie on the lines y = 10ch and y = 30ch, and wrap.
		y := (float64(r) + 0.5) * ch
		d := min(math.Abs(y-10*ch), math.Abs(y-30*ch), math.Abs(y+(rows-30)*ch))
		if land[r*cols] == 0 {
			d = -d
		}
		for c := range cols {
			if g := float64(got[r*cols+c]); math.Abs(g-d) > 1e-9 {
				t.Fatalf("row %d: distance %g, want %g", r, g, d)
			}
		}
	}
}

func TestCoastDistanceNoCoast(t *testing.T) {
	for _, v := range []byte{0, 1} {
		land := make([]byte, 12)
		for i := range land {
			land[i] = v
		}
		got, err := coastDistance(t.Context(), land, 4, 3, 6, 6)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range got {
			if !math.IsInf(float64(d), 2*int(v)-1) {
				t.Fatalf("all %d: distance %g", v, d)
			}
		}
	}
}

func TestCoastDistanceTranslates(t *testing.T) {
	const cols, rows = 30, 20
	rng := rand.New(rand.NewPCG(3, 4))
	land := make([]byte, cols*rows)
	for i := range land {
		if rng.IntN(3) == 0 {
			land[i] = 1
		}
	}
	base, err := coastDistance(t.Context(), land, cols, rows, 6, 6)
	if err != nil {
		t.Fatal(err)
	}
	const dx, dy = 13, 7
	moved := make([]byte, len(land))
	for i, v := range land {
		moved[(i/cols+dy)%rows*cols+(i%cols+dx)%cols] = v
	}
	got, err := coastDistance(t.Context(), moved, cols, rows, 6, 6)
	if err != nil {
		t.Fatal(err)
	}
	for i, v := range base {
		if j := (i/cols+dy)%rows*cols + (i%cols+dx)%cols; got[j] != v {
			t.Fatalf("cell %d: %g, translated %g", i, v, got[j])
		}
	}
}

func TestProfiles(t *testing.T) {
	p := DefaultFoundationParams()
	if LandProfile(p, 400, 0) != 0 || LandProfile(p, 400, p.RiseKM) != 400 || LandProfile(p, 400, math.Inf(1)) != 400 {
		t.Error("land profile ends")
	}
	if LandProfile(p, 400, p.RiseKM/2) != 200 {
		t.Errorf("land profile midpoint %g", LandProfile(p, 400, p.RiseKM/2))
	}
	s, w, a := 120.0, 80.0, 4000.0
	for _, c := range []struct{ x, want float64 }{
		{0, 0}, {w / 2, -s / 2}, {w, -s}, {w + p.SlopeKM/2, -(s + a) / 2}, {w + p.SlopeKM, -a}, {1e6, -a}, {math.Inf(1), -a},
	} {
		if got := WaterProfile(p, s, w, a, c.x); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("water profile at %g km: %g, want %g", c.x, got, c.want)
		}
	}
	// No kinks: the profile's slope is continuous, and zero, at the joins.
	const h = 1e-4
	for _, x := range []float64{w, w + p.SlopeKM} {
		left := (WaterProfile(p, s, w, a, x) - WaterProfile(p, s, w, a, x-h)) / h
		right := (WaterProfile(p, s, w, a, x+h) - WaterProfile(p, s, w, a, x)) / h
		if math.Abs(left) > 1e-2 || math.Abs(right) > 1e-2 {
			t.Errorf("slope at %g km: %g and %g", x, left, right)
		}
	}
}

// constant returns a 2 x 2 field of v over g's extent.
func constant(g continent.Grid, v float32) *fracture.FractureField {
	return &fracture.FractureField{Columns: 2, Rows: 2, WidthKM: g.WidthKM, HeightKM: g.HeightKM, Values: []float32{v, v, v, v}}
}

// TestFoundationStraightCoast checks the elevation of a straight coastline
// against the profiles at the analytic distances.
func TestFoundationStraightCoast(t *testing.T) {
	const cols, rows = 4, 200
	g := continent.Grid{Columns: cols, Rows: rows, WidthKM: 24, HeightKM: 1200, CellWidthKM: 6, CellHeightKM: 6}
	p := DefaultFoundationParams()
	land := band(cols, rows, 50, 150)
	dist, err := coastDistance(t.Context(), land, cols, rows, 6, 6)
	if err != nil {
		t.Fatal(err)
	}
	fd := &foundation{base: constant(g, 400)}
	fd.fields[fieldVariation] = constant(g, 0.5)  // no variation
	fd.fields[fieldShelfDepth] = constant(g, 0.5) // 125 m
	fd.fields[fieldShelfWidth] = constant(g, 0)   // 50 km
	fd.fields[fieldAbyssalDepth] = constant(g, 1) // 5500 m
	elev, err := fd.elevation(t.Context(), p, g, dist)
	if err != nil {
		t.Fatal(err)
	}
	for r := range rows {
		y := (float64(r) + 0.5) * 6
		d := min(math.Abs(y-300), math.Abs(y-900))
		var want float64
		if land[r*cols] == 1 {
			want = LandProfile(p, 400, d)
		} else {
			want = WaterProfile(p, 125, 50, 5500, d)
		}
		for c := range cols {
			if e := float64(elev[r*cols+c]); math.Abs(e-want) > 1e-3*max(1, math.Abs(want)) {
				t.Fatalf("row %d: elevation %g, want %g", r, e, want)
			}
		}
	}
	// The coast is at 0 m: the first land row is just above, the first
	// water row just below.
	if elev[50*cols] <= 0 || elev[49*cols] >= 0 {
		t.Errorf("coastal cells %g and %g", elev[50*cols], elev[49*cols])
	}
	if e := elev[100*cols]; math.Abs(float64(e)-400) > 1e-3 {
		t.Errorf("interior %g, want 400", e)
	}
	if e := elev[0]; math.Abs(float64(e)+5500) > 1e-3 {
		t.Errorf("abyssal plain %g, want -5500", e)
	}
}

func TestBlur(t *testing.T) {
	const cols, rows = 9, 5
	in := make([]float64, cols*rows)
	in[2*cols+4] = 1
	out := blur(in, cols, rows, 1.5, 4) // the vertical kernel wraps more than once
	sum := 0.0
	for _, v := range out {
		sum += v
	}
	if math.Abs(sum-1) > 1e-12 {
		t.Errorf("blur changed the total to %g", sum)
	}
	for i := range in {
		in[i] = 7
	}
	for i, v := range blur(in, cols, rows, 2, 2) {
		if math.Abs(v-7) > 1e-12 {
			t.Fatalf("cell %d: constant blurred to %g", i, v)
		}
	}
}

func TestBaseLevels(t *testing.T) {
	const cols, rows = 60, 30
	g := continent.Grid{Columns: cols, Rows: rows, WidthKM: 360, HeightKM: 180, CellWidthKM: 6, CellHeightKM: 6}
	p := DefaultFoundationParams()
	pf := &structure.ProvinceFile{Provinces: []structure.Province{
		{ID: 1, Type: structure.TypeOceanic}, {ID: 2, Type: structure.TypeShield}, {ID: 3, Type: structure.TypeSedimentary},
	}}
	owner := make([]uint32, cols*rows)
	for i := range owner {
		switch c := i % cols; {
		case c < 20:
			owner[i] = 1
		case c < 40:
			owner[i] = 2
		default:
			owner[i] = 3
		}
	}
	base, err := baseLevels(t.Context(), 5, p, g, pf, owner)
	if err != nil {
		t.Fatal(err)
	}
	// Every value is a weighted mean of the two land provinces' draws.
	lo, hi := p.BaseM[structure.TypeSedimentary][0], p.BaseM[structure.TypeShield][1]
	for i, v := range base.Values {
		if float64(v) < lo || float64(v) > hi {
			t.Fatalf("working cell %d: base level %g outside [%g, %g]", i, v, lo, hi)
		}
	}
	// A different seed draws different levels.
	other, err := baseLevels(t.Context(), 6, p, g, pf, owner)
	if err != nil {
		t.Fatal(err)
	}
	if other.Values[0] == base.Values[0] {
		t.Error("base levels do not depend on the seed")
	}
	// Ocean everywhere: the island level.
	for i := range owner {
		owner[i] = 1
	}
	if base, err = baseLevels(t.Context(), 5, p, g, pf, owner); err != nil {
		t.Fatal(err)
	}
	for _, v := range base.Values {
		if float64(v) != p.IslandBaseM {
			t.Fatalf("base level %g with no land provinces", v)
		}
	}
}

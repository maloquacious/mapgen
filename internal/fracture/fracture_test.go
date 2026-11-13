// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package fracture

import (
	"context"
	"errors"
	"math"
	"math/rand/v2"
	"slices"
	"testing"
)

// grid20x10 is a 20 x 10 raster of 1 km cells.
var grid20x10 = FractureConfig{Columns: 20, Rows: 10, WidthKM: 20, HeightKM: 10, MinRadiusKM: 1, MaxRadiusKM: 1, Falloff: 2}

// oracle rasterizes discs by testing every cell directly, using the same
// lower-inclusive, upper-exclusive convention in row units.
func oracle(cfg FractureConfig, discs []Disc) []int32 {
	sx, sy := cfg.WidthKM/float64(cfg.Columns), cfg.HeightKM/float64(cfg.Rows)
	out := make([]int32, cfg.Columns*cfg.Rows)
	for _, d := range discs {
		for col := range cfg.Columns {
			dx := (float64(col)+0.5)*sx - d.X
			dx -= cfg.WidthKM * math.Floor(dx/cfg.WidthKM+0.5)
			if math.Abs(dx) >= d.R {
				continue
			}
			half := math.Sqrt(d.R*d.R - dx*dx)
			lo, hi := (d.Y-half)/sy-0.5, (d.Y+half)/sy-0.5
			full := int(math.Ceil(hi))-int(math.Ceil(lo)) >= cfg.Rows
			for row := range cfg.Rows {
				hit := full
				for m := -2; m <= 2 && !hit; m++ {
					r := float64(row + m*cfg.Rows)
					hit = r >= lo && r < hi
				}
				if hit {
					out[row*cfg.Columns+col] += d.Delta
				}
			}
		}
	}
	return out
}

func build1(t *testing.T, cfg FractureConfig, discs ...Disc) *FractureField {
	t.Helper()
	f, err := BuildFromDiscs(t.Context(), cfg, discs)
	if err != nil {
		t.Fatal(err)
	}
	want := oracle(cfg, discs)
	for i, v := range f.Values {
		if int32(v) != want[i] {
			t.Fatalf("cell (%d,%d) = %v, oracle %d", i%cfg.Columns, i/cfg.Columns, v, want[i])
		}
	}
	return f
}

func covered(f *FractureField) (cells [][2]int) {
	for i, v := range f.Values {
		if v != 0 {
			cells = append(cells, [2]int{i % f.Columns, i / f.Columns})
		}
	}
	return cells
}

func TestSingleDiscFixtures(t *testing.T) {
	for _, tc := range []struct {
		name  string
		disc  Disc
		cells int
		check func(t *testing.T, f *FractureField)
	}{
		{"interior", Disc{X: 10, Y: 5, R: 1.2}, 4, func(t *testing.T, f *FractureField) {
			for _, c := range [][2]int{{9, 4}, {10, 4}, {9, 5}, {10, 5}} {
				if f.At(c[0], c[1]) != 1 {
					t.Errorf("cell %v not covered", c)
				}
			}
		}},
		{"x wrap", Disc{X: 0.1, Y: 5, R: 1.2}, 4, func(t *testing.T, f *FractureField) {
			if f.At(19, 4) != 1 || f.At(0, 4) != 1 {
				t.Errorf("seam cells not covered: %v", covered(f))
			}
		}},
		{"y wrap", Disc{X: 10, Y: 0.1, R: 1.2}, 4, func(t *testing.T, f *FractureField) {
			if f.At(10, 9) != 1 || f.At(10, 0) != 1 {
				t.Errorf("seam cells not covered: %v", covered(f))
			}
		}},
		{"both wrap", Disc{X: 19.9, Y: 9.9, R: 1.2}, 4, func(t *testing.T, f *FractureField) {
			for _, c := range [][2]int{{19, 9}, {0, 9}, {19, 0}, {0, 0}} {
				if f.At(c[0], c[1]) != 1 {
					t.Errorf("cell %v not covered", c)
				}
			}
		}},
		// Column centers 9.5 and 11.5 lie exactly at distance R: skipped.
		{"tangency", Disc{X: 10.5, Y: 5.5, R: 1}, 2, func(t *testing.T, f *FractureField) {
			if f.At(9, 5) != 0 || f.At(11, 5) != 0 || f.At(10, 5) != 1 || f.At(10, 4) != 1 {
				t.Errorf("tangency: %v", covered(f))
			}
		}},
		// The chord through column 10 spans [4.6,5.4): no row center inside.
		{"empty chord", Disc{X: 10.5, Y: 5, R: 0.4}, 0, nil},
		// In column 10 row center 3.5 equals cy-half (included) and row
		// center 6.5 equals cy+half (excluded).
		{"lower inclusive upper exclusive", Disc{X: 10.5, Y: 5, R: 1.5}, 7, func(t *testing.T, f *FractureField) {
			if f.At(10, 3) != 1 || f.At(10, 6) != 0 {
				t.Errorf("interval ends: %v", covered(f))
			}
		}},
		{"full height", Disc{X: 10.5, Y: 5, R: 6}, -1, func(t *testing.T, f *FractureField) {
			for row := range f.Rows {
				if f.At(10, row) != 1 {
					t.Errorf("row %d of center column = %v", row, f.At(10, row))
				}
			}
			if v := slices.Max(f.Values); v != 1 {
				t.Errorf("max %v, want each cell touched once", v)
			}
		}},
		{"above half circumference", Disc{X: 3, Y: 2, R: 12, Delta: -1}, 200, func(t *testing.T, f *FractureField) {
			for i, v := range f.Values {
				if v != -1 {
					t.Fatalf("cell %d = %v, want -1", i, v)
				}
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.disc.Delta == 0 {
				tc.disc.Delta = 1
			}
			f := build1(t, grid20x10, tc.disc)
			if tc.cells >= 0 {
				if got := len(covered(f)); got != tc.cells {
					t.Errorf("covered %d cells, want %d: %v", got, tc.cells, covered(f))
				}
			}
			if tc.check != nil {
				tc.check(t, f)
			}
		})
	}
}

func TestRandomDiscsMatchOracle(t *testing.T) {
	cfg := FractureConfig{Columns: 37, Rows: 23, WidthKM: 37 * 1.7, HeightKM: 23 * 2.3, MinRadiusKM: 0.5, MaxRadiusKM: 60, Falloff: 1.5}
	rng := rand.New(rand.NewPCG(1, 2))
	discs := make([]Disc, 3000)
	for i := range discs {
		discs[i] = SampleDisc(rng, cfg)
	}
	build1(t, cfg, discs...)
}

func TestDifferenceColumnsSumToZero(t *testing.T) {
	cfg := FractureConfig{Columns: 31, Rows: 17, WidthKM: 31, HeightKM: 17, Bumps: 500, MinRadiusKM: 0.5, MaxRadiusKM: 20, Falloff: 2}
	r := newRasterizer(cfg)
	rng := rand.New(rand.NewPCG(3, 4))
	for range cfg.Bumps {
		r.add(SampleDisc(rng, cfg))
	}
	for col := range cfg.Columns {
		var sum int32
		for row := range cfg.Rows + 1 {
			sum += r.diff[row*cfg.Columns+col]
		}
		if sum != 0 {
			t.Fatalf("column %d sums to %d", col, sum)
		}
	}
	if _, err := r.field(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestFieldBounds(t *testing.T) {
	cfg := FractureConfig{Columns: 40, Rows: 20, WidthKM: 400, HeightKM: 200, Bumps: 300, MinRadiusKM: 15, MaxRadiusKM: 70, Falloff: 2}
	f, err := BuildFracture(t.Context(), cfg, rand.New(rand.NewPCG(5, 6)))
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range f.Values {
		if v < -float32(cfg.Bumps) || v > float32(cfg.Bumps) || v != float32(math.Trunc(float64(v))) {
			t.Fatalf("value %v outside integer range [-%d,%d]", v, cfg.Bumps, cfg.Bumps)
		}
	}

	cfg.Bumps = 0
	f, err = BuildFracture(t.Context(), cfg, rand.New(rand.NewPCG(5, 6)))
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(f.Values, func(v float32) bool { return v != 0 }) {
		t.Fatal("zero discs gave a nonzero field")
	}
}

func TestSampleRadius(t *testing.T) {
	if r := SampleRadius(0.7, 3, 3, 2); r != 3 {
		t.Errorf("equal bounds: %v", r)
	}
	for _, p := range []float64{1, 1 + 1e-10, 2, 2.5, 3, 0.5} {
		a, b := 1.5, 400.0
		cdf := func(r float64) float64 {
			if math.Abs(1-p) < FalloffLogEpsilon {
				return math.Log(r/a) / math.Log(b/a)
			}
			e := 1 - p
			return (math.Pow(r, e) - math.Pow(a, e)) / (math.Pow(b, e) - math.Pow(a, e))
		}
		if r := SampleRadius(0, a, b, p); math.Abs(r-a) > 1e-9 {
			t.Errorf("p=%v u=0: %v", p, r)
		}
		if r := SampleRadius(1, a, b, p); math.Abs(r-b) > 1e-9*b {
			t.Errorf("p=%v u=1: %v", p, r)
		}
		// F(R) is uniform for R drawn from the distribution; compare deciles.
		const n = 100_000
		rng := rand.New(rand.NewPCG(7, uint64(p*1000)))
		us := make([]float64, n)
		for i := range us {
			r := SampleRadius(rng.Float64(), a, b, p)
			if r < a || r > b || math.IsNaN(r) {
				t.Fatalf("p=%v: radius %v out of [%v,%v]", p, r, a, b)
			}
			us[i] = cdf(r)
		}
		slices.Sort(us)
		for q := 1; q < 10; q++ {
			if got := us[q*n/10]; math.Abs(got-float64(q)/10) > 0.01 {
				t.Errorf("p=%v decile %d: F = %.4f", p, q, got)
			}
		}
	}
}

func TestValidate(t *testing.T) {
	ok := FractureConfig{Columns: 4, Rows: 4, WidthKM: 4, HeightKM: 4, Bumps: 1, MinRadiusKM: 1, MaxRadiusKM: 2, Falloff: 2}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		edit func(*FractureConfig)
		want error
	}{
		{"zero cols", func(c *FractureConfig) { c.Columns = 0 }, ErrInvalidDimensions},
		{"negative rows", func(c *FractureConfig) { c.Rows = -1 }, ErrInvalidDimensions},
		{"overflow", func(c *FractureConfig) { c.Columns = math.MaxInt / 2 }, ErrTooLarge},
		{"nan width", func(c *FractureConfig) { c.WidthKM = math.NaN() }, ErrInvalidExtent},
		{"inf height", func(c *FractureConfig) { c.HeightKM = math.Inf(1) }, ErrInvalidExtent},
		{"negative bumps", func(c *FractureConfig) { c.Bumps = -1 }, ErrInvalidBumps},
		{"too many bumps", func(c *FractureConfig) { c.Bumps = MaxBumps + 1 }, ErrInvalidBumps},
		{"reversed radii", func(c *FractureConfig) { c.MinRadiusKM, c.MaxRadiusKM = 2, 1 }, ErrInvalidRadius},
		{"zero radius", func(c *FractureConfig) { c.MinRadiusKM = 0 }, ErrInvalidRadius},
		{"nan falloff", func(c *FractureConfig) { c.Falloff = math.NaN() }, ErrInvalidFalloff},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := ok
			tc.edit(&cfg)
			if err := cfg.Validate(); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
	if _, err := BuildFracture(t.Context(), ok, nil); !errors.Is(err, ErrNilRNG) {
		t.Fatalf("nil rng: %v", err)
	}
}

func TestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	cfg := FractureConfig{Columns: 8, Rows: 8, WidthKM: 8, HeightKM: 8, Bumps: 5000, MinRadiusKM: 1, MaxRadiusKM: 2, Falloff: 2}
	if _, err := BuildFracture(ctx, cfg, rand.New(rand.NewPCG(1, 1))); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

func TestSampling(t *testing.T) {
	f := &FractureField{Columns: 4, Rows: 3, WidthKM: 8, HeightKM: 6, Values: []float32{
		0, 1, 2, 3,
		4, 5, 6, 7,
		8, 9, 10, 11,
	}}
	// Cell centers are at (1+2c, 1+2r).
	near := func(a, b float64) bool { return math.Abs(a-b) < 1e-9 }
	if v := f.SampleTorus(3, 3); !near(v, 5) {
		t.Errorf("center of (1,1) = %v", v)
	}
	if v := f.SampleTorus(4, 3); !near(v, 5.5) {
		t.Errorf("midpoint = %v", v)
	}
	src := NewContinentSource(f)
	for _, x := range []float64{0, 0.3, 3.7, 7.99, 8, -1.25} {
		for _, y := range []float64{0.2, 2.5, 5.9} {
			for _, s := range []func(x, y float64) float64{f.SampleTorus, src.Sample} {
				if a, b, c := s(x, y), s(x+8, y), s(x-8, y); !near(a, b) || !near(a, c) {
					t.Errorf("x periodicity at (%v,%v): %v %v %v", x, y, a, b, c)
				}
			}
			if a, b := f.SampleTorus(x, y), f.SampleTorus(x, y+6); !near(a, b) {
				t.Errorf("torus y periodicity at (%v,%v): %v %v", x, y, a, b)
			}
		}
	}
	// x seam: between column 3 (center 7) and column 0 (center 9 = 1+8).
	if v := src.Sample(8, 3); !near(v, 5.5) {
		t.Errorf("x seam = %v, want 5.5", v)
	}
	// Cylinder clamps y: above the first row center and below the last.
	if v := src.Sample(3, 0); !near(v, 1) {
		t.Errorf("north clamp = %v", v)
	}
	if v := src.Sample(3, -50); !near(v, 1) {
		t.Errorf("far north clamp = %v", v)
	}
	if v := src.Sample(3, 6); !near(v, 9) {
		t.Errorf("south clamp = %v", v)
	}
	// The torus instead wraps north to south.
	if v := f.SampleTorus(3, 0); !near(v, 5) {
		t.Errorf("torus y seam = %v, want 5", v)
	}
	if v := src.Sample(math.NaN(), 1); !math.IsNaN(v) {
		t.Errorf("NaN input = %v", v)
	}
}

func TestTopMask(t *testing.T) {
	values := []float32{3, 1, 3, 2, 3, 0}
	got := TopMask(values, 2)
	if want := []bool{true, false, true, false, false, false}; !slices.Equal(got, want) {
		t.Errorf("ties: %v", got)
	}
	if n := len(slices.DeleteFunc(TopMask(values, 4), func(b bool) bool { return !b })); n != 4 {
		t.Errorf("count %d", n)
	}
}

func TestCenteringAcrossSeams(t *testing.T) {
	const cols, rows = 40, 20
	f := &FractureField{Columns: cols, Rows: rows, WidthKM: 40, HeightKM: 20, Values: make([]float32, cols*rows)}
	// A 6 x 4 block straddling both seams, centered on the corner, plus a
	// smaller decoy block elsewhere.
	for dy := -2; dy < 2; dy++ {
		for dx := -3; dx < 3; dx++ {
			f.Values[mod(dy, rows)*cols+mod(dx, cols)] = 100 + float32(dx*7+dy)
		}
	}
	for dy := range 2 {
		for dx := range 2 {
			f.Values[(12+dy)*cols+(20+dx)] = 5
		}
	}
	land := 28.0 / (cols * rows)
	c, err := FindCentering(t.Context(), f, land)
	if err != nil {
		t.Fatal(err)
	}
	if c.ComponentCells != 24 || c.Components != 2 || c.XAmbiguous || c.YAmbiguous {
		t.Fatalf("centering %+v", c)
	}
	g := f.Slide(c.X, c.Y)
	// The block's center (the corner, continuous index -0.5) must land on the
	// domain center (cols/2-0.5, rows/2-0.5).
	if c.X != cols/2 || c.Y != rows/2 {
		t.Errorf("shift (%d,%d), want (%d,%d)", c.X, c.Y, cols/2, rows/2)
	}
	for row := range rows {
		for col := range cols {
			if g.At(col+c.X, row+c.Y) != f.At(col, row) {
				t.Fatalf("slide moved (%d,%d) incorrectly", col, row)
			}
		}
	}
	a, b := slices.Sorted(slices.Values(f.Values)), slices.Sorted(slices.Values(g.Values))
	if !slices.Equal(a, b) {
		t.Error("slide changed values")
	}
}

func TestCenteringAmbiguous(t *testing.T) {
	// A full-width band has no defined x mean.
	f := &FractureField{Columns: 16, Rows: 8, WidthKM: 16, HeightKM: 8, Values: make([]float32, 128)}
	for col := range 16 {
		f.Values[1*16+col] = 1
	}
	c, err := FindCentering(t.Context(), f, 16.0/128)
	if err != nil {
		t.Fatal(err)
	}
	if !c.XAmbiguous || c.X != 0 || c.YAmbiguous || c.Y != mod(4-1, 8) {
		t.Errorf("centering %+v", c)
	}
}

func TestGenerateDeterministic(t *testing.T) {
	p, err := DefaultParams(99, 120, 60, 6)
	if err != nil {
		t.Fatal(err)
	}
	a, err := Generate(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Generate(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(a.Affinity.Values, b.Affinity.Values) || a.Centering != b.Centering {
		t.Fatal("same seed gave different output")
	}
	p.Seed++
	c, err := Generate(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Equal(a.Affinity.Values, c.Affinity.Values) {
		t.Fatal("different seeds gave identical output")
	}
	// Every retained layer shares the shift: affinity is still the mix.
	for i, v := range a.Affinity.Values {
		if v != a.Continental.Values[i]+p.CoastWeight*a.Coast.Values[i] {
			t.Fatalf("layers misaligned at %d", i)
		}
	}
}

func TestDefaultParams(t *testing.T) {
	p, err := DefaultParams(1, 6666, 3333, 6)
	if err != nil {
		t.Fatal(err)
	}
	// N = 6666 * 3333 = 22217778.
	if p.Continental.Bumps != 5554444 || p.Coast.Bumps != 2777222 {
		t.Errorf("bumps %d %d", p.Continental.Bumps, p.Coast.Bumps)
	}
	if cfg := p.config(p.Continental); cfg.MinRadiusKM != 9 || cfg.MaxRadiusKM != 6666 {
		t.Errorf("continental radii %v..%v km", cfg.MinRadiusKM, cfg.MaxRadiusKM)
	}
	// rows/14 < 2 cells for fewer than 28 rows.
	if _, err := DefaultParams(1, 100, 20, 6); !errors.Is(err, ErrInvalidRadius) {
		t.Errorf("small domain: %v", err)
	}
}

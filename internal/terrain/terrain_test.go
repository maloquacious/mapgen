// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package terrain_test

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maloquacious/mapgen/internal/continent"
	"github.com/maloquacious/mapgen/internal/fracture"
	"github.com/maloquacious/mapgen/internal/structure"
	"github.com/maloquacious/mapgen/internal/terrain"
	"github.com/maloquacious/mapgen/internal/trench"
)

// inputDir writes a 96 x 48 continental artifact of 6 km cells, a structure
// artifact from it with the structure fixture, and a trench artifact from
// that, and returns the trench artifact.
func inputDir(t *testing.T) string { return inputDirOpts(t, false) }

// inputDirOpts is inputDir with the continents left off center and the
// trench stage recentering them, when recenter is set.
func inputDirOpts(t *testing.T, recenter bool) string { return inputDirFull(t, !recenter, recenter) }

// inputDirFull is inputDir with the continents centered or not, and the
// trench stage recentering or not.
func inputDirFull(t *testing.T, centered, recenter bool) string {
	t.Helper()
	p, err := fracture.DefaultParams(42, 96, 48, 6)
	if err != nil {
		t.Fatal(err)
	}
	p.Recenter = centered
	cr, err := fracture.Generate(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	in := filepath.Join(t.TempDir(), "continents")
	if err := continent.Write(in, continent.NewManifest("test", cr), cr); err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile("../structure/testdata/fixture.json")
	if err != nil {
		t.Fatal(err)
	}
	sr, err := structure.Generate(t.Context(), structure.Options{In: in, Seed: new(uint64(7)), Fraction: 0.325, Features: fixture, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	sdir := filepath.Join(t.TempDir(), "structure")
	if err := structure.Write(t.Context(), sdir, sr, 0); err != nil {
		t.Fatal(err)
	}
	tr, err := trench.Generate(t.Context(), trench.Options{In: sdir, Version: "test", Recenter: recenter})
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "trench")
	if err := trench.Write(t.Context(), out, tr, 0); err != nil {
		t.Fatal(err)
	}
	return out
}

func generate(t *testing.T, in string, seed *uint64) *terrain.Result {
	t.Helper()
	r, err := terrain.Generate(t.Context(), terrain.Options{In: in, Seed: seed, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// structured generates without residual detail.
func structured(t *testing.T, in string) *terrain.Result {
	t.Helper()
	r, err := terrain.Generate(t.Context(), terrain.Options{In: in, NoResidual: true, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func write(t *testing.T, r *terrain.Result) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "terrain")
	if err := terrain.Write(t.Context(), out, r, 1); err != nil {
		t.Fatal(err)
	}
	return out
}

// files returns the contents of every file in dir, by name.
func files(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		out[e.Name()] = string(b)
	}
	return out
}

func sameFiles(t *testing.T, a, b map[string]string) {
	t.Helper()
	if len(a) != len(b) {
		t.Fatalf("file sets differ: %d and %d files", len(a), len(b))
	}
	for name, data := range a {
		if b[name] != data {
			t.Errorf("%s differs", name)
		}
	}
}

// The foundation keeps the provisional coast at 0 m; features change the
// surface only within their reach.
func TestFoundationKeepsProvisionalCoast(t *testing.T) {
	in := inputDir(t)
	r := structured(t, in)
	land := r.Structure.Land
	fd := terrain.Foundation(r)
	ff := r.Structure.Features
	g := r.Manifest.Grid
	reach := make([]bool, len(land))
	shapes := []structure.Shape{}
	for i := range ff.Features {
		shapes = append(shapes, ff.Features[i].Shape(ff.Domain))
	}
	// Ridges and peaks may reach a little past a narrow envelope.
	for _, rd := range r.Ridges.Ridges {
		shapes = append(shapes, structure.NewLine(ff.Domain, rd.Backbone))
	}
	for _, pk := range r.Ridges.Peaks {
		shapes = append(shapes, structure.NewLine(ff.Domain, []structure.Sample{{X: pk.X, Y: pk.Y, WidthKM: 2 * pk.RadiusKM}, {X: pk.X + 1e-3, Y: pk.Y, WidthKM: 2 * pk.RadiusKM}}))
	}
	for _, sh := range shapes {
		for k, in := range structure.Cover(ff.Domain, g.Columns, g.Rows, sh) {
			reach[k] = reach[k] || in
		}
	}
	// A cell is sampled within, so a feature may reach it without reaching
	// its center: widen the reach by one cell.
	near := make([]bool, len(reach))
	for k := range reach {
		c, r := k%g.Columns, k/g.Columns
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				near[k] = near[k] || reach[((r+dy+g.Rows)%g.Rows)*g.Columns+(c+dx+g.Columns)%g.Columns]
			}
		}
	}
	changed, rugged := 0, 0
	for i, e := range fd {
		if (e > 0) != (land[i] == 1) || e == 0 {
			t.Fatalf("cell %d: foundation %g, provisional land %d", i, e, land[i])
		}
		if r.High[i] < r.Elevation[i] || r.Relief[i] < 0 {
			t.Fatalf("cell %d: elevation %g, high %g, relief %g", i, r.Elevation[i], r.High[i], r.Relief[i])
		}
		if r.Relief[i] > 0 {
			rugged++
		}
		if r.Elevation[i] != e || r.Relief[i] != 0 || r.High[i] != e {
			changed++
			if !near[i] {
				t.Fatalf("cell %d beyond every envelope changed from %g to %g", i, e, r.Elevation[i])
			}
		}
	}
	if changed == 0 || rugged == 0 {
		t.Errorf("the features changed %d cells, %d with relief", changed, rugged)
	}
	d := r.Diagnostics
	if d.Shift.LandCells != r.Manifest.ProvisionalLandCells {
		t.Errorf("shift %+v", d.Shift)
	}
	above := 0
	for i, a := range r.Above {
		if a {
			above++
			if float64(r.Elevation[i]) < d.Shift.LevelM {
				t.Fatalf("cell %d above the level at %g", i, r.Elevation[i])
			}
		} else if float64(r.Elevation[i]) > d.Shift.LevelM {
			t.Fatalf("cell %d below the level at %g", i, r.Elevation[i])
		}
	}
	if above != d.Shift.LandCells {
		t.Errorf("%d cells above the level, want %d", above, d.Shift.LandCells)
	}
	if d.ElevationLand.Cells+d.ElevationWater.Cells != len(land) || d.ElevationWater.MinM < -5500 {
		t.Errorf("elevation statistics %+v %+v", d.ElevationLand, d.ElevationWater)
	}
	if r.Manifest.Seed != 7 || r.Manifest.SeedSource != terrain.SeedFromInput {
		t.Errorf("seed %d from %s", r.Manifest.Seed, r.Manifest.SeedSource)
	}
}

// Every fixture feature shows in the surface, and each core conforms to its
// targets.
func TestFixtureHandoff(t *testing.T) {
	r := structured(t, inputDir(t))
	ff := r.Structure.Features
	params := r.Manifest.RegionalParameters
	if len(r.Diagnostics.Features) != len(ff.Features) {
		t.Fatalf("%d feature diagnostics for %d features", len(r.Diagnostics.Features), len(ff.Features))
	}
	for k, d := range r.Diagnostics.Features {
		f := &ff.Features[k]
		if d.ID != f.ID || d.Core.Cells == 0 {
			t.Errorf("feature %d: no core cells", f.ID)
			continue
		}
		if f.Type == structure.TypeFault {
			if r := params.FaultM[f.Sense]; d.OffsetM < r[0] || d.OffsetM > r[1] {
				t.Errorf("fault %d: offset %g outside %v", f.ID, d.OffsetM, r)
			}
			continue
		}
		key := f.Type
		if f.Type == structure.TypeMountainBelt || f.Type == structure.TypeBasin {
			key += "/" + f.Kind
		}
		want := params.DefaultTargets[key]
		if f.Targets != nil && f.Targets.RegionalElevationM != nil {
			want.RegionalElevationM = *f.Targets.RegionalElevationM
		}
		if f.Targets != nil && f.Targets.ReliefM != nil {
			want.ReliefM = *f.Targets.ReliefM
		}
		within := func(got *[2]float64, r [2]float64) bool { return got != nil && got[0] >= r[0] && got[1] <= r[1] }
		if !within(d.RegionalM, want.RegionalElevationM) || !within(d.ReliefM, want.ReliefM) {
			t.Errorf("feature %d: draws %v and %v outside %+v", f.ID, d.RegionalM, d.ReliefM, want)
		}
		// A plateau stands far above any lowland, so its core lies at its
		// draw, give or take its rolling surface and dissected rim.
		if f.Type == structure.TypePlateau && math.Abs(d.Core.MeanM-d.RegionalM[0]) > (params.PlateauRolling+params.PlateauRim)*d.ReliefM[0] {
			t.Errorf("plateau %d: core %+v, draw %g", f.ID, d.Core, d.RegionalM[0])
		}
	}
}

func TestArtifactRoundTrip(t *testing.T) {
	in := inputDir(t)
	r := generate(t, in, nil)
	out := write(t, r)
	want := files(t, out)
	// Repeatable.
	sameFiles(t, want, files(t, write(t, generate(t, in, nil))))
	// Load and write again: the same bytes.
	loaded, err := terrain.Load(t.Context(), out)
	if err != nil {
		t.Fatal(err)
	}
	sameFiles(t, want, files(t, write(t, loaded)))
	// Retained records are the input's bytes; the continental fields and
	// earlier diagnostics are not kept.
	input := files(t, in)
	for _, f := range r.Manifest.Retained {
		if want[f.File] != input[f.File] {
			t.Errorf("%s was not retained unchanged", f.File)
		}
	}
	for _, name := range []string{"affinity.f32", "continental.f32", "coast.f32"} {
		if _, ok := want[name]; ok {
			t.Errorf("retained %s", name)
		}
	}
	for _, name := range []string{terrain.ManifestFile, terrain.DiagnosticsFile, terrain.PreviewFile, terrain.PreviewStructureFile, terrain.RidgesFile, "elevation.f32", "high.f32", "relief.f32", structure.FeaturesFile, structure.RockFile, trench.TrenchesFile} {
		if _, ok := want[name]; !ok {
			t.Errorf("missing %s", name)
		}
	}
	// The input path is not recorded.
	if strings.Contains(want[terrain.ManifestFile], in) {
		t.Error("manifest records the input path")
	}
	// Corruption is detected.
	if err := os.WriteFile(filepath.Join(out, structure.FeaturesFile), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := terrain.Load(t.Context(), out); err == nil {
		t.Error("loaded a corrupted artifact")
	}
	// An existing output is never replaced.
	if err := terrain.Write(t.Context(), out, r, 0); err == nil {
		t.Error("replaced an existing output")
	}
}

// --residual=false is recorded, and leaves out the residual alone.
func TestResidualSwitch(t *testing.T) {
	in := inputDir(t)
	on, off := generate(t, in, nil), structured(t, in)
	if !on.Manifest.Residual || off.Manifest.Residual {
		t.Errorf("residual recorded as %v and %v", on.Manifest.Residual, off.Manifest.Residual)
	}
	p := on.Manifest.ResidualParameters
	moved := 0
	for i, e := range on.Elevation {
		o := float64(off.Elevation[i])
		if d := math.Abs(float64(e) - o); d > max(min(p.FloorM, p.DatumShare*math.Abs(o)), p.Share*float64(off.Relief[i]))+1e-3 {
			t.Fatalf("cell %d moved %g", i, d)
		} else if d > 0 {
			moved++
		}
		// Without relief, the residual never crosses the datum.
		if off.Relief[i] == 0 && (e > 0) != (o > 0) {
			t.Fatalf("cell %d crossed the datum: %g to %g", i, o, e)
		}
	}
	if moved == 0 {
		t.Error("the residual changed nothing")
	}
	loaded, err := terrain.Load(t.Context(), write(t, off))
	if err != nil || loaded.Manifest.Residual {
		t.Errorf("load: %v, residual %v", err, loaded.Manifest.Residual)
	}
}

// The number of workers never changes the output.
func TestWorkers(t *testing.T) {
	in := inputDir(t)
	var want map[string]string
	for _, w := range []int{1, 8} {
		r, err := terrain.Generate(t.Context(), terrain.Options{In: in, Workers: w, Version: "test"})
		if err != nil {
			t.Fatal(err)
		}
		got := files(t, write(t, r))
		if want == nil {
			want = got
			continue
		}
		sameFiles(t, want, got)
	}
}

func TestSeed(t *testing.T) {
	in := inputDir(t)
	a, b := generate(t, in, new(uint64(1))), generate(t, in, new(uint64(2)))
	if b.Manifest.SeedSource != terrain.SeedExplicit {
		t.Errorf("seed source %s", b.Manifest.SeedSource)
	}
	fa, fb := terrain.Foundation(a), terrain.Foundation(b)
	differ := 0
	for i := range a.Elevation {
		if (fa[i] > 0) != (fb[i] > 0) {
			t.Fatalf("cell %d of the foundation changed sides with the seed", i)
		}
		if a.Elevation[i] != b.Elevation[i] {
			differ++
		}
	}
	if differ == 0 {
		t.Error("the seed changes nothing")
	}
}

// Terrain reads a trench artifact whose structure records were recentered
// by the trench stage.
func TestRecenteredTrenchInput(t *testing.T) {
	in := inputDirOpts(t, true)
	tr, err := trench.Load(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	if c := tr.Manifest.Recentering; c.ShiftX == 0 && c.ShiftY == 0 || tr.Manifest.RecenteredRecords == nil {
		t.Fatalf("the trench input was not moved: %+v", c)
	}
	r := generate(t, in, nil)
	out := write(t, r)
	loaded, err := terrain.Load(t.Context(), out)
	if err != nil {
		t.Fatal(err)
	}
	sameFiles(t, files(t, out), files(t, write(t, loaded)))
	input := files(t, in)
	for _, f := range r.Manifest.Retained {
		if files(t, out)[f.File] != input[f.File] {
			t.Errorf("%s was not retained unchanged", f.File)
		}
	}
}

// Recentering moves the complete result by whole cells to center the land
// terrain implies, and the moved records still load through the trench
// and structure checks.
func TestRecenter(t *testing.T) {
	in := inputDirFull(t, false, false)
	base := generate(t, in, nil)
	r, err := terrain.Generate(t.Context(), terrain.Options{In: in, Recenter: true, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	g := r.Manifest.Grid
	c := r.Manifest.Recentering
	f := &fracture.FractureField{Columns: g.Columns, Rows: g.Rows, WidthKM: g.WidthKM, HeightKM: g.HeightKM, Values: base.Elevation}
	want, err := fracture.FindCentering(t.Context(), f, r.Manifest.ProvisionalLandFraction)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Applied || c.ShiftX != want.X || c.ShiftY != want.Y || c.ShiftX == 0 && c.ShiftY == 0 {
		t.Fatalf("shift (%d, %d), want (%d, %d)", c.ShiftX, c.ShiftY, want.X, want.Y)
	}
	for _, pair := range [][2][]float32{{base.Elevation, r.Elevation}, {base.High, r.High}, {base.Relief, r.Relief}} {
		for i, v := range pair[0] {
			col, row := (i%g.Columns+c.ShiftX)%g.Columns, (i/g.Columns+c.ShiftY)%g.Rows
			if pair[1][row*g.Columns+col] != v {
				t.Fatalf("cell %d did not move with the shift", i)
			}
		}
	}
	d := r.Diagnostics
	if d.Shift != base.Diagnostics.Shift {
		t.Errorf("shift diagnostics changed: %+v, %+v", d.Shift, base.Diagnostics.Shift)
	}
	moved := func(a, b float64, size float64, by float64) bool {
		v := math.Mod(a+by-b+2*size, size)
		return min(v, size-v) < 1e-6
	}
	for i, p := range base.Ridges.Peaks {
		q := r.Ridges.Peaks[i]
		if !moved(p.X, q.X, g.WidthKM, c.ShiftXKM) || !moved(p.Y, q.Y, g.HeightKM, c.ShiftYKM) {
			t.Errorf("peak %d at (%g, %g), from (%g, %g)", p.ID, q.X, q.Y, p.X, p.Y)
		}
	}
	out := write(t, r)
	loaded, err := terrain.Load(t.Context(), out)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Manifest.RecenteredRecords == nil || loaded.Manifest.Recentering != c {
		t.Errorf("recentering %+v, records %v", loaded.Manifest.Recentering, loaded.Manifest.RecenteredRecords != nil)
	}
	sameFiles(t, files(t, out), files(t, write(t, loaded)))
	// The structure records moved as structure would move them.
	if sr := loaded.Structure.Manifest.Recentering; sr.ShiftX%g.Columns != c.ShiftX || sr.ShiftY%g.Rows != c.ShiftY {
		t.Errorf("structure records recentered by (%d, %d)", sr.ShiftX, sr.ShiftY)
	}
	// A recentering record that does not match the records is rejected.
	data, err := os.ReadFile(filepath.Join(out, terrain.ManifestFile))
	if err != nil {
		t.Fatal(err)
	}
	bad := strings.Replace(string(data), fmt.Sprintf(`"shift_x": %d,`, c.ShiftX), fmt.Sprintf(`"shift_x": %d,`, (c.ShiftX+1)%g.Columns), 1)
	if bad == string(data) {
		t.Fatal("no shift to change")
	}
	if err := os.WriteFile(filepath.Join(out, terrain.ManifestFile), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := terrain.Load(t.Context(), out); err == nil {
		t.Error("loaded a recentering that does not match the records")
	}

	// A trench input that was itself recentered accumulates.
	chain := inputDirOpts(t, true)
	r2, err := terrain.Generate(t.Context(), terrain.Options{In: chain, Recenter: true, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	out2 := write(t, r2)
	if _, err := terrain.Load(t.Context(), out2); err != nil {
		t.Fatalf("chained recentering: %v (shift %+v)", err, r2.Manifest.Recentering)
	}
	t.Logf("shifts (%d, %d) and chained (%d, %d)", c.ShiftX, c.ShiftY, r2.Manifest.Recentering.ShiftX, r2.Manifest.Recentering.ShiftY)
}

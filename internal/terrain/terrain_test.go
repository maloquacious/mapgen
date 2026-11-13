// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package terrain_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maloquacious/mapgen/internal/continent"
	"github.com/maloquacious/mapgen/internal/fracture"
	"github.com/maloquacious/mapgen/internal/structure"
	"github.com/maloquacious/mapgen/internal/terrain"
)

// structureDir writes a 96 x 48 continental artifact of 6 km cells and a
// structure artifact from it with the structure fixture.
func structureDir(t *testing.T) string {
	t.Helper()
	p, err := fracture.DefaultParams(42, 96, 48, 6)
	if err != nil {
		t.Fatal(err)
	}
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
	out := filepath.Join(t.TempDir(), "structure")
	if err := structure.Write(t.Context(), out, sr, 0); err != nil {
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

func TestFoundationKeepsProvisionalCoast(t *testing.T) {
	in := structureDir(t)
	r := generate(t, in, nil)
	land := r.Structure.Land
	for i, e := range r.Elevation {
		if (e > 0) != (land[i] == 1) || e == 0 {
			t.Fatalf("cell %d: elevation %g, provisional land %d", i, e, land[i])
		}
		if r.High[i] != e || r.Relief[i] != 0 || r.Above[i] != (land[i] == 1) {
			t.Fatalf("cell %d: high %g, relief %g, above %v", i, r.High[i], r.Relief[i], r.Above[i])
		}
	}
	d := r.Diagnostics
	if d.Shift.WaterToLand != 0 || d.Shift.LandToWater != 0 || d.Shift.LandCells != r.Manifest.ProvisionalLandCells {
		t.Errorf("shift %+v", d.Shift)
	}
	// Midway between the lowest land and the highest water.
	if want := (d.ElevationLand.MinM + d.ElevationWater.MaxM) / 2; d.Shift.LevelM != want {
		t.Errorf("level %g, want %g", d.Shift.LevelM, want)
	}
	if d.ElevationLand.Cells+d.ElevationWater.Cells != len(land) || d.ElevationLand.MaxM > 800*1.15 || d.ElevationWater.MinM < -5500 {
		t.Errorf("elevation statistics %+v %+v", d.ElevationLand, d.ElevationWater)
	}
	if r.Manifest.Seed != 7 || r.Manifest.SeedSource != terrain.SeedFromInput {
		t.Errorf("seed %d from %s", r.Manifest.Seed, r.Manifest.SeedSource)
	}
}

func TestArtifactRoundTrip(t *testing.T) {
	in := structureDir(t)
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
	// structure diagnostics are not kept.
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
	for _, name := range []string{terrain.ManifestFile, terrain.DiagnosticsFile, terrain.PreviewFile, "elevation.f32", "high.f32", "relief.f32", structure.FeaturesFile, structure.RockFile} {
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

func TestSeed(t *testing.T) {
	in := structureDir(t)
	a, b := generate(t, in, new(uint64(1))), generate(t, in, new(uint64(2)))
	if b.Manifest.SeedSource != terrain.SeedExplicit {
		t.Errorf("seed source %s", b.Manifest.SeedSource)
	}
	differ := 0
	for i := range a.Elevation {
		if (a.Elevation[i] > 0) != (b.Elevation[i] > 0) {
			t.Fatalf("cell %d changed sides with the seed", i)
		}
		if a.Elevation[i] != b.Elevation[i] {
			differ++
		}
	}
	if differ == 0 {
		t.Error("the seed changes nothing")
	}
}

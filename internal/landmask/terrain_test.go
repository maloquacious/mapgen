// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package landmask_test

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/maloquacious/mapgen/internal/continent"
	"github.com/maloquacious/mapgen/internal/fracture"
	"github.com/maloquacious/mapgen/internal/landmask"
	"github.com/maloquacious/mapgen/internal/structure"
	"github.com/maloquacious/mapgen/internal/terrain"
	"github.com/maloquacious/mapgen/internal/trench"
)

// terrainDir builds a small terrain artifact through every stage before
// landmask, with the structure fixture's features.
func terrainDir(t *testing.T) string {
	t.Helper()
	p, err := fracture.DefaultParams(42, 96, 48, 6)
	if err != nil {
		t.Fatal(err)
	}
	p.Recenter = true
	cr, err := fracture.Generate(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	cdir := filepath.Join(t.TempDir(), "continents")
	if err := continent.Write(cdir, continent.NewManifest("test", cr), cr); err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile("../structure/testdata/fixture.json")
	if err != nil {
		t.Fatal(err)
	}
	sr, err := structure.Generate(t.Context(), structure.Options{In: cdir, Seed: new(uint64(7)), Fraction: 0.325, Features: fixture, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	sdir := filepath.Join(t.TempDir(), "structure")
	if err := structure.Write(t.Context(), sdir, sr, 0); err != nil {
		t.Fatal(err)
	}
	tr, err := trench.Generate(t.Context(), trench.Options{In: sdir, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	tdir := filepath.Join(t.TempDir(), "trench")
	if err := trench.Write(t.Context(), tdir, tr, 0); err != nil {
		t.Fatal(err)
	}
	r, err := terrain.Generate(t.Context(), terrain.Options{In: tdir, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "terrain")
	if err := terrain.Write(t.Context(), dir, r, 0); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestTerrainInput(t *testing.T) {
	in := terrainDir(t)
	if kind, err := landmask.InputKind(in); err != nil || kind != landmask.InputTerrain {
		t.Fatalf("input kind %q, %v", kind, err)
	}
	tr, err := terrain.Load(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	r, err := landmask.GenerateFromTerrain(t.Context(), in, nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	m, d := r.Manifest, r.Diagnostics
	// With no fraction given, the provisional fraction is used, and sea
	// level is the shift terrain reported.
	if m.LandCells != tr.Manifest.ProvisionalLandCells || m.SeaLevelM != tr.Diagnostics.Shift.LevelM {
		t.Fatalf("land cells %d, level %g; terrain reports %d, %g", m.LandCells, m.SeaLevelM, tr.Manifest.ProvisionalLandCells, tr.Diagnostics.Shift.LevelM)
	}
	if !(m.RepairFraction <= m.Parameters.MaxRepairFraction) || d.OceanCells+d.NonOceanCells != len(r.Land) ||
		d.DryLandCells+d.DepressionCells != d.NonOceanCells {
		t.Fatalf("diagnostics %+v", d)
	}
	t.Logf("level %.1f m, repair %d cells, %d components, %d depressions, clamp %+v", m.SeaLevelM, d.RepairCells, d.QuantileComponents, d.DepressionCells, d.Clamp)
	// Away from the repair and the clamp, every cell is the terrain cell
	// shifted by the sea level, and relief is unchanged.
	for i, e := range r.Elevation {
		want := float32(float64(tr.Elevation[i]) - m.SeaLevelM)
		if e != want && !(e == 1 || e == -1) {
			t.Fatalf("cell %d: %g, want %g", i, e, want)
		}
		if r.Relief[i] != tr.Relief[i] || r.High[i] < e {
			t.Fatalf("cell %d: relief or high changed", i)
		}
	}

	out := filepath.Join(t.TempDir(), "landmask")
	if err := landmask.WriteTerrain(t.Context(), out, r, 1); err != nil {
		t.Fatal(err)
	}
	got, err := landmask.LoadTerrain(t.Context(), out)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Land, r.Land) || !slices.Equal(got.Elevation, r.Elevation) || got.Diagnostics != r.Diagnostics {
		t.Fatal("loaded result differs")
	}
	// The artifact is self-contained and repeatable: a loaded result
	// writes the same bytes, and a second run gives the same bytes.
	again := filepath.Join(t.TempDir(), "again")
	if err := landmask.WriteTerrain(t.Context(), again, got, 1); err != nil {
		t.Fatal(err)
	}
	r2, err := landmask.GenerateFromTerrain(t.Context(), in, nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	rerun := filepath.Join(t.TempDir(), "rerun")
	if err := landmask.WriteTerrain(t.Context(), rerun, r2, 1); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		a, _ := os.ReadFile(filepath.Join(out, e.Name()))
		for _, dir := range []string{again, rerun} {
			if b, err := os.ReadFile(filepath.Join(dir, e.Name())); err != nil || !bytes.Equal(a, b) {
				t.Fatalf("%s differs in %s", e.Name(), filepath.Base(dir))
			}
		}
	}
	// An explicit fraction is honored.
	fr := 0.3
	r3, err := landmask.GenerateFromTerrain(t.Context(), in, &fr, "test")
	if err != nil {
		t.Fatal(err)
	}
	if r3.Manifest.LandCells != 1382 || r3.Manifest.LandFraction != fr {
		t.Fatalf("explicit fraction: %d land cells", r3.Manifest.LandCells)
	}
	// Tampering is rejected: a flipped mask cell, and a moved cell.
	for _, name := range []string{landmask.MaskFile, "elevation.f32"} {
		dir := filepath.Join(t.TempDir(), "tampered")
		if err := os.CopyFS(dir, os.DirFS(out)); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, name)
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		b[0] ^= 1
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := landmask.LoadTerrain(t.Context(), dir); err == nil {
			t.Fatalf("accepted a tampered %s", name)
		}
	}
	if _, err := landmask.LoadTerrain(t.Context(), in); err == nil {
		t.Fatal("loaded a terrain artifact as a landmask")
	}
}

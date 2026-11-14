// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/maloquacious/mapgen/internal/continent"
	"github.com/maloquacious/mapgen/internal/fracture"
	"github.com/maloquacious/mapgen/internal/landmask"
	"github.com/maloquacious/mapgen/internal/structure"
	"github.com/maloquacious/mapgen/internal/terrain"
	"github.com/maloquacious/mapgen/internal/trench"
)

func TestCommandArtifactAndPreview(t *testing.T) {
	in, out := filepath.Join(t.TempDir(), "continents"), filepath.Join(t.TempDir(), "landmask")
	p, err := fracture.DefaultParams(42, 96, 48, 6)
	if err != nil {
		t.Fatal(err)
	}
	r, err := fracture.Generate(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	if err := continent.Write(in, continent.NewManifest("test", r), r); err != nil {
		t.Fatal(err)
	}
	if err := run(t.Context(), []string{"--in", in, "--out", out, "--land-fraction", "0.325", "--recenter"}); err != nil {
		t.Fatal(err)
	}
	got, err := landmask.Load(out)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(filepath.Join(out, "preview.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 96 || img.Bounds().Dy() != 48 {
		t.Fatal("wrong preview size")
	}
	for i, land := range got.Land {
		r, g, b, _ := img.At(i%96, i/96).RGBA()
		if land == 1 && !(g > r && g > b) || land == 0 && !(b > r && b > g) {
			t.Fatalf("preview disagrees with saved mask at %d", i)
		}
	}
	// The preview scale is irrelevant when no preview is written.
	noPreview := filepath.Join(t.TempDir(), "nopreview")
	if err := run(t.Context(), []string{"--in", in, "--out", noPreview, "--png=false", "--png-scale", "0"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(noPreview, landmask.PreviewFile)); !os.IsNotExist(err) {
		t.Fatal("wrote a preview with --png=false")
	}
	for _, args := range [][]string{{"--land-fraction", "NaN"}, {"--png-scale", "0"}, {"unexpected"}} {
		if err := run(t.Context(), args); err == nil {
			t.Fatalf("accepted invalid args %v", args)
		}
	}
}

func TestTerrainInput(t *testing.T) {
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
	// Automatic placement on so small a world makes inland basins large
	// enough to fail the repair limit; the fixture does not.
	fixture, err := os.ReadFile("../../internal/structure/testdata/fixture.json")
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
	in := filepath.Join(t.TempDir(), "terrain")
	if err := terrain.Write(t.Context(), in, r, 0); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "landmask")
	if err := run(t.Context(), []string{"--in", in, "--out", out}); err != nil {
		t.Fatal(err)
	}
	got, err := landmask.LoadTerrain(t.Context(), out)
	if err != nil {
		t.Fatal(err)
	}
	if got.Manifest.LandCells != r.Manifest.ProvisionalLandCells {
		t.Fatalf("%d land cells, want the provisional %d", got.Manifest.LandCells, r.Manifest.ProvisionalLandCells)
	}
	if _, err := os.Stat(filepath.Join(out, landmask.PreviewFile)); err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(t.TempDir(), "moved")
	if err := run(t.Context(), []string{"--in", in, "--out", moved, "--recenter"}); err != nil {
		t.Fatal(err)
	}
	if got, err := landmask.LoadTerrain(t.Context(), moved); err != nil || !got.Manifest.Recentering.Applied {
		t.Fatalf("recentered run: %v", err)
	}
}

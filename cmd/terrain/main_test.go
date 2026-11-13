// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"image/png"
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

func TestCommand(t *testing.T) {
	cdir, sdir := filepath.Join(t.TempDir(), "continents"), filepath.Join(t.TempDir(), "structure")
	p, err := fracture.DefaultParams(42, 96, 48, 6)
	if err != nil {
		t.Fatal(err)
	}
	cr, err := fracture.Generate(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	if err := continent.Write(cdir, continent.NewManifest("test", cr), cr); err != nil {
		t.Fatal(err)
	}
	sr, err := structure.Generate(t.Context(), structure.Options{In: cdir, Fraction: 0.325, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
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
	out := filepath.Join(t.TempDir(), "terrain")
	args := []string{"--in", tdir, "--out", out, "--seed", "9", "--png-scale", "2"}
	if err := run(t.Context(), args); err != nil {
		t.Fatal(err)
	}
	got, err := terrain.Load(t.Context(), out)
	if err != nil {
		t.Fatal(err)
	}
	if got.Manifest.Seed != 9 || got.Manifest.SeedSource != terrain.SeedExplicit {
		t.Errorf("seed %d from %s", got.Manifest.Seed, got.Manifest.SeedSource)
	}
	f, err := os.Open(filepath.Join(out, terrain.PreviewFile))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 48 || img.Bounds().Dy() != 24 {
		t.Errorf("preview is %v", img.Bounds())
	}
	if err := run(t.Context(), args); err == nil {
		t.Error("replaced an existing output directory")
	}
	noPreview := filepath.Join(t.TempDir(), "nopreview")
	if err := run(t.Context(), []string{"--in", tdir, "--out", noPreview, "--png=false"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(noPreview, terrain.PreviewFile)); !os.IsNotExist(err) {
		t.Error("wrote the preview with --png=false")
	}
	// A structure artifact is not a trench artifact.
	err = run(t.Context(), []string{"--in", sdir, "--out", filepath.Join(t.TempDir(), "wrong")})
	if err == nil || !strings.Contains(err.Error(), trench.Format) {
		t.Errorf("structure input: %v", err)
	}
}

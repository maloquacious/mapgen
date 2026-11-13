// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/maloquacious/mapgen/internal/continent"
	"github.com/maloquacious/mapgen/internal/fracture"
	"github.com/maloquacious/mapgen/internal/structure"
)

func TestCommand(t *testing.T) {
	in, out := filepath.Join(t.TempDir(), "continents"), filepath.Join(t.TempDir(), "structure")
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
	fixture := "../../internal/structure/testdata/fixture.json"
	args := []string{"--in", in, "--out", out, "--seed", "9", "--features", fixture, "--png-scale", "2"}
	if err := run(t.Context(), args); err != nil {
		t.Fatal(err)
	}
	got, err := structure.Load(t.Context(), out)
	if err != nil {
		t.Fatal(err)
	}
	if got.Manifest.Seed != 9 || got.Manifest.FeatureCount != 7 {
		t.Errorf("manifest: seed %d, %d features", got.Manifest.Seed, got.Manifest.FeatureCount)
	}
	f, err := os.Open(filepath.Join(out, structure.PreviewFile))
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
	if err := run(t.Context(), []string{"--in", in, "--out", filepath.Join(t.TempDir(), "none")}); !errors.Is(err, structure.ErrNoPlacement) {
		t.Errorf("without --features: %v", err)
	}
	noPreview := filepath.Join(t.TempDir(), "nopreview")
	if err := run(t.Context(), []string{"--in", in, "--out", noPreview, "--features", fixture, "--png=false"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(noPreview, structure.PreviewFile)); !os.IsNotExist(err) {
		t.Error("wrote a preview with --png=false")
	}
}

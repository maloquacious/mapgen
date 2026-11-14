// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"image/png"
	"io"
	"os"
	"path/filepath"
	"reflect"
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
	if got.Manifest.Seed != 9 || got.Manifest.FeatureCount != 10 {
		t.Errorf("manifest: seed %d, %d features", got.Manifest.Seed, got.Manifest.FeatureCount)
	}
	// 576 x 288 km at a 50 km working spacing.
	if c := got.Diagnostics.Coast; c == nil || c.Columns != 12 || c.Rows != 6 || len(c.Chains) == 0 {
		t.Errorf("coast diagnostics: %+v", c)
	}
	if _, err := os.Stat(filepath.Join(out, structure.CoastPreviewFile)); err != nil {
		t.Error(err)
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
	auto := filepath.Join(t.TempDir(), "auto")
	if err := run(t.Context(), []string{"--in", in, "--out", auto}); err != nil {
		t.Errorf("without --features: %v", err)
	} else if got, err := structure.Load(t.Context(), auto); err != nil || got.Manifest.Mode != structure.ModeAutomatic {
		t.Errorf("without --features: %v, %+v", err, got)
	}
	// A configuration file is used and recorded; one that changes an
	// algorithm is refused.
	cfg := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(cfg, []byte(`{"format":"mapgen-structure-config-v1","rocks":{"metamorphic_limit":0.2}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	configured := filepath.Join(t.TempDir(), "configured")
	if err := run(t.Context(), []string{"--in", in, "--out", configured, "--png=false", "--config", cfg}); err != nil {
		t.Fatal(err)
	}
	if c, err := structure.Load(t.Context(), configured); err != nil || c.Manifest.RockParameters.MetamorphicLimit != 0.2 {
		t.Fatalf("configured run: %v", err)
	}
	if err := os.WriteFile(cfg, []byte(`{"format":"mapgen-structure-config-v1","rocks":{"algorithm":"x"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run(t.Context(), []string{"--in", in, "--out", filepath.Join(t.TempDir(), "bad"), "--config", cfg}); err == nil {
		t.Error("accepted a configuration that changes an algorithm")
	}
	noPreview := filepath.Join(t.TempDir(), "nopreview")
	if err := run(t.Context(), []string{"--in", in, "--out", noPreview, "--features", fixture, "--png=false"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{structure.PreviewFile, structure.CoastPreviewFile} {
		if _, err := os.Stat(filepath.Join(noPreview, name)); !os.IsNotExist(err) {
			t.Errorf("wrote %s with --png=false", name)
		}
	}
}

// The printed defaults are a valid configuration file equal to the defaults.
func TestPrintConfig(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = w
	err = run(t.Context(), []string{"--print-config"})
	os.Stdout = stdout
	w.Close()
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	c, err := structure.ParseConfig(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c, structure.DefaultConfig()) {
		t.Fatal("printed configuration differs from the defaults")
	}
}

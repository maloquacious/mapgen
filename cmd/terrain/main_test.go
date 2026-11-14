// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"image/png"
	"io"
	"os"
	"path/filepath"
	"reflect"
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
	for _, name := range []string{terrain.PreviewFile, terrain.PreviewStructureFile, terrain.PreviewReliefFile, terrain.PreviewChangeFile} {
		f, err := os.Open(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(f)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		if img.Bounds().Dx() != 48 || img.Bounds().Dy() != 24 {
			t.Errorf("%s is %v", name, img.Bounds())
		}
	}
	if err := run(t.Context(), args); err == nil {
		t.Error("replaced an existing output directory")
	}
	// A configuration file is used and recorded; one that changes an
	// algorithm is refused.
	cfg := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(cfg, []byte(`{"format":"mapgen-terrain-config-v1","regional":{"rift_floor_m":150}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	configured := filepath.Join(t.TempDir(), "configured")
	if err := run(t.Context(), []string{"--in", tdir, "--out", configured, "--png=false", "--config", cfg}); err != nil {
		t.Fatal(err)
	}
	if c, err := terrain.Load(t.Context(), configured); err != nil || c.Manifest.RegionalParameters.RiftFloorM != 150 {
		t.Fatalf("configured run: %v", err)
	}
	if err := os.WriteFile(cfg, []byte(`{"format":"mapgen-terrain-config-v1","regional":{"algorithm":"x"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run(t.Context(), []string{"--in", tdir, "--out", filepath.Join(t.TempDir(), "bad"), "--config", cfg}); err == nil {
		t.Error("accepted a configuration that changes an algorithm")
	}
	noPreview := filepath.Join(t.TempDir(), "nopreview")
	if err := run(t.Context(), []string{"--in", tdir, "--out", noPreview, "--png=false"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{terrain.PreviewFile, terrain.PreviewStructureFile, terrain.PreviewReliefFile, terrain.PreviewChangeFile} {
		if _, err := os.Stat(filepath.Join(noPreview, name)); !os.IsNotExist(err) {
			t.Errorf("wrote %s with --png=false", name)
		}
	}
	// A structure artifact is not a trench artifact.
	err = run(t.Context(), []string{"--in", sdir, "--out", filepath.Join(t.TempDir(), "wrong")})
	if err == nil || !strings.Contains(err.Error(), trench.Format) {
		t.Errorf("structure input: %v", err)
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
	c, err := terrain.ParseConfig(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c, terrain.DefaultConfig()) {
		t.Fatal("printed configuration differs from the defaults")
	}
}

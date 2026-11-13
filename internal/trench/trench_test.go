// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package trench_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maloquacious/mapgen/internal/continent"
	"github.com/maloquacious/mapgen/internal/fracture"
	"github.com/maloquacious/mapgen/internal/structure"
	"github.com/maloquacious/mapgen/internal/trench"
)

// structureDir writes a 480 x 240 continental artifact of 12 km cells and an
// automatically placed structure artifact from it. Seed 4 places an active
// margin; a world this small has no room for island arcs.
func structureDir(t *testing.T) string {
	t.Helper()
	p, err := fracture.DefaultParams(4, 480, 240, 12)
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
	sr, err := structure.Generate(t.Context(), structure.Options{In: in, Fraction: 0.325, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "structure")
	if err := structure.Write(t.Context(), out, sr, 0); err != nil {
		t.Fatal(err)
	}
	return out
}

func generate(t *testing.T, in string, seed *uint64) *trench.Result {
	t.Helper()
	r, err := trench.Generate(t.Context(), trench.Options{In: in, Seed: seed, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func write(t *testing.T, r *trench.Result) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "trench")
	if err := trench.Write(t.Context(), out, r, 1); err != nil {
		t.Fatal(err)
	}
	return out
}

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

func TestGenerate(t *testing.T) {
	in := structureDir(t)
	r := generate(t, in, nil)
	m := r.Manifest
	if m.Seed != 4 || m.SeedSource != trench.SeedFromInput {
		t.Errorf("seed %d from %s", m.Seed, m.SeedSource)
	}
	ff := r.Structure.Features
	subducting := 0
	for i := range ff.Features {
		if trench.Subducts(&ff.Features[i]) {
			subducting++
		}
	}
	if subducting == 0 || len(r.Trenches.Subduction) != subducting {
		t.Fatalf("%d subduction records for %d active margins and arcs", len(r.Trenches.Subduction), subducting)
	}
	if len(r.Trenches.Trenches) == 0 {
		t.Fatalf("no trenches; omitted %v", r.Diagnostics.Omitted)
	}
	g := m.Grid
	dom := r.Domain()
	for _, tr := range r.Trenches.Trenches {
		for i, in := range structure.Cover(dom, g.Columns, g.Rows, structure.NewLine(dom, tr.Backbone)) {
			if in && r.Structure.Land[i] == 1 {
				t.Fatalf("trench %d covers provisional land at cell %d", tr.ID, i)
			}
		}
	}
	t.Logf("%d subduction zones %v, %d trenches, %g km; omitted %v",
		subducting, r.Diagnostics.Subduction, r.Diagnostics.Trenches, r.Diagnostics.LengthKM, r.Diagnostics.Omitted)
}

func TestArtifactRoundTrip(t *testing.T) {
	in := structureDir(t)
	r := generate(t, in, nil)
	out := write(t, r)
	want := files(t, out)
	// Repeatable.
	sameFiles(t, want, files(t, write(t, generate(t, in, nil))))
	// Load and write again: the same bytes.
	loaded, err := trench.Load(t.Context(), out)
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
	for _, name := range []string{trench.ManifestFile, trench.TrenchesFile, trench.DiagnosticsFile, trench.PreviewFile, structure.FeaturesFile} {
		if _, ok := want[name]; !ok {
			t.Errorf("missing %s", name)
		}
	}
	if strings.Contains(want[trench.ManifestFile], in) {
		t.Error("manifest records the input path")
	}
	// Corruption is detected.
	if err := os.WriteFile(filepath.Join(out, trench.TrenchesFile), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := trench.Load(t.Context(), out); err == nil {
		t.Error("loaded a corrupted artifact")
	}
	// An existing output is never replaced.
	if err := trench.Write(t.Context(), out, r, 0); err == nil {
		t.Error("replaced an existing output")
	}
}

func TestSeed(t *testing.T) {
	in := structureDir(t)
	a, b := generate(t, in, new(uint64(1))), generate(t, in, new(uint64(2)))
	if b.Manifest.SeedSource != trench.SeedExplicit {
		t.Errorf("seed source %s", b.Manifest.SeedSource)
	}
	for i, s := range a.Trenches.Subduction {
		if s.Basis == trench.BasisFlank && b.Trenches.Subduction[i] != s {
			t.Errorf("the seed changed the polarity of feature %d", s.Feature)
		}
	}
	ja, _ := a.Trenches.Marshal()
	jb, _ := b.Trenches.Marshal()
	if string(ja) == string(jb) {
		t.Error("the seed changes nothing")
	}
}

// A trench record that does not match the retained features is rejected.
func TestValidateAgainstFeatures(t *testing.T) {
	in := structureDir(t)
	r := generate(t, in, nil)
	ff := r.Structure.Features
	for name, change := range map[string]func(tf *trench.TrenchFile){
		"side": func(tf *trench.TrenchFile) {
			tf.Trenches[0].Side = map[string]string{"left": "right", "right": "left"}[tf.Trenches[0].Side]
		},
		"parent":  func(tf *trench.TrenchFile) { tf.Trenches[0].Parent = 9999 },
		"id":      func(tf *trench.TrenchFile) { tf.Trenches[0].ID = 5 },
		"missing": func(tf *trench.TrenchFile) { tf.Subduction = tf.Subduction[1:] },
		"target":  func(tf *trench.TrenchFile) { tf.Trenches[0].Targets.RegionalElevationM = &[2]float64{-100, 100} },
	} {
		data, _ := r.Trenches.Marshal()
		tf, err := trench.ParseTrenches(data)
		if err != nil {
			t.Fatal(err)
		}
		change(tf)
		if err := tf.Validate(ff); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

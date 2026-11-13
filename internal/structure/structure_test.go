// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package structure_test

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/maloquacious/mapgen/internal/continent"
	"github.com/maloquacious/mapgen/internal/fracture"
	"github.com/maloquacious/mapgen/internal/structure"
)

// continents writes a 96 x 48 artifact of 6 km cells: 576 x 288 km, the
// domain testdata/fixture.json is drawn for.
func continents(t *testing.T) string {
	t.Helper()
	p, err := fracture.DefaultParams(42, 96, 48, 6)
	if err != nil {
		t.Fatal(err)
	}
	r, err := fracture.Generate(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "continents")
	if err := continent.Write(dir, continent.NewManifest("test", r), r); err != nil {
		t.Fatal(err)
	}
	return dir
}

func fixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/fixture.json")
	if err != nil {
		t.Fatal(err)
	}
	return data
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

func write(t *testing.T, in string, features []byte) string {
	t.Helper()
	r, err := structure.Generate(t.Context(), structure.Options{In: in, Seed: new(uint64(7)), Fraction: 0.325, Features: features, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "structure")
	if err := structure.Write(t.Context(), out, r, 1); err != nil {
		t.Fatal(err)
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

// Feeding an artifact's features.json back as the fixture reproduces the
// artifact byte for byte, the input is left untouched, and the result does
// not depend on where the input is stored.
func TestRoundTrip(t *testing.T) {
	in := continents(t)
	before := files(t, in)
	first := write(t, in, fixture(t))
	sameFiles(t, before, files(t, in))

	got := files(t, first)
	for _, name := range []string{"manifest.json", "features.json", "diagnostics.json", "provinces.json", "provinces.u32", "provisional-land.u8", "affinity.f32", "continental.f32", "coast.f32", "preview.png", "preview-coast.png"} {
		if _, ok := got[name]; !ok {
			t.Errorf("missing %s", name)
		}
	}
	second := write(t, in, []byte(got[structure.FeaturesFile]))
	sameFiles(t, got, files(t, second))

	moved := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.CopyFS(moved, os.DirFS(in)); err != nil {
		t.Fatal(err)
	}
	sameFiles(t, got, files(t, write(t, moved, fixture(t))))
	if strings.Contains(got[structure.ManifestFile], filepath.Dir(in)) {
		t.Error("manifest records an input path")
	}
}

// An automatically placed artifact's features.json is a fixture that gives
// the same features and provinces back.
func TestAutomaticRoundTrip(t *testing.T) {
	p, err := fracture.DefaultParams(42, 480, 240, 12)
	if err != nil {
		t.Fatal(err)
	}
	fr, err := fracture.Generate(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	in := filepath.Join(t.TempDir(), "continents")
	if err := continent.Write(in, continent.NewManifest("test", fr), fr); err != nil {
		t.Fatal(err)
	}
	o := structure.Options{In: in, Fraction: 0.325, Version: "test"}
	auto, err := structure.Generate(t.Context(), o)
	if err != nil {
		t.Fatal(err)
	}
	if auto.Manifest.FeatureCount == 0 || auto.Manifest.Mode != structure.ModeAutomatic {
		t.Fatalf("placed %d features in %s mode; omitted %v", auto.Manifest.FeatureCount, auto.Manifest.Mode, auto.Diagnostics.Omitted)
	}
	again, err := structure.Generate(t.Context(), o)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := auto.Features.Marshal()
	if got, _ := again.Features.Marshal(); string(got) != string(want) {
		t.Error("automatic placement is not repeatable")
	}
	o.Features = want
	fixed, err := structure.Generate(t.Context(), o)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := fixed.Features.Marshal(); string(got) != string(want) || fixed.Manifest.Mode != structure.ModeFixture {
		t.Error("features.json from automatic placement does not reproduce itself as a fixture")
	}
}

func TestLoad(t *testing.T) {
	in := continents(t)
	out := write(t, in, fixture(t))
	r, err := structure.Load(t.Context(), out)
	if err != nil {
		t.Fatal(err)
	}
	m := r.Manifest
	if m.Format != structure.Format || m.Mode != structure.ModeFixture || m.Seed != 7 || m.SeedSource != structure.SeedExplicit ||
		m.RockClassification != structure.RockAlgorithm {
		t.Errorf("manifest: %+v", m)
	}
	if m.FeatureCount != 7 || len(r.Features.Features) != 7 || r.Diagnostics.Placed[structure.TypeBasin] != 2 {
		t.Errorf("features: %d in manifest, diagnostics %+v", m.FeatureCount, r.Diagnostics)
	}
	// Meaningful zeros and the full key range survive storage.
	belt, rift := r.Features.Features[0], r.Features.Features[2]
	if belt.Maturity == nil || *belt.Maturity != 0 || rift.Key == nil || *rift.Key != 1<<64-1 {
		t.Errorf("maturity %v, key %v", belt.Maturity, rift.Key)
	}
	// Features list the provinces under their envelopes, in ascending order.
	if len(r.Provinces.Provinces) != m.ProvinceCount || m.ProvinceCount == 0 {
		t.Errorf("%d provinces, manifest %d", len(r.Provinces.Provinces), m.ProvinceCount)
	}
	for _, f := range r.Features.Features {
		if len(f.Provinces) == 0 || !slices.IsSorted(f.Provinces) || int(f.Provinces[len(f.Provinces)-1]) > m.ProvinceCount {
			t.Errorf("feature %d provinces %v", f.ID, f.Provinces)
		}
	}
	// The fault's first vertex lies in a province the fault lists.
	if o := r.Owner[int(50/6)*96+int(50/6)]; !slices.Contains(r.Features.Features[3].Provinces, o) {
		t.Errorf("fault does not list province %d under its first vertex", o)
	}
	land := 0
	for _, v := range r.Land {
		land += int(v)
	}
	if want := 1498; land != want || m.ProvisionalLandCells != want { // round(0.325 * 96 * 48)
		t.Errorf("provisional land: %d cells, manifest %d, want %d", land, m.ProvisionalLandCells, want)
	}
	// The loaded features answer queries: the plateau is on the belt's left.
	l := structure.NewLine(r.Domain(), belt.Backbone)
	if _, _, side := l.Nearest(structure.Point{X: 600, Y: 60}); side != structure.Left {
		t.Errorf("plateau side is %v", side)
	}

	if err := structure.Write(t.Context(), out, r, 0); err == nil {
		t.Error("replaced an existing artifact")
	}
	for _, name := range []string{structure.FeaturesFile, structure.MaskFile, structure.DiagnosticsFile, structure.ProvincesFile, structure.OwnershipFile, "coast.f32", structure.RockFile, "pressure.f32", "rock-noise.f32"} {
		path := filepath.Join(out, name)
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		b[len(b)/2] ^= 1
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := structure.Load(t.Context(), out); err == nil {
			t.Errorf("loaded an artifact with a corrupted %s", name)
		}
		b[len(b)/2] ^= 1
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLoadChecksRocks(t *testing.T) {
	// A rock raster that disagrees with its saved proxies is rejected even
	// when its checksum is updated to match.
	out := write(t, continents(t), fixture(t))
	path := filepath.Join(out, structure.RockFile)
	rock, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join(out, structure.ManifestFile))
	if err != nil {
		t.Fatal(err)
	}
	old := sha256.Sum256(rock)
	rock[100] = 1 + rock[100]%3 // a different classified rock
	sum := sha256.Sum256(rock)
	manifest = []byte(strings.Replace(string(manifest), hex.EncodeToString(old[:]), hex.EncodeToString(sum[:]), 1))
	if err := os.WriteFile(path, rock, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, structure.ManifestFile), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := structure.Load(t.Context(), out); err == nil || !strings.Contains(err.Error(), "saved proxies") {
		t.Errorf("loaded an inconsistent rock raster: %v", err)
	}
}

func TestGenerateRejects(t *testing.T) {
	in := continents(t)
	o := structure.Options{In: in, Fraction: 0.325, Version: "test"}
	// Without a fixture, features are placed automatically. This world is
	// too small for any belt, and says so; a fault fits.
	if r, err := structure.Generate(t.Context(), o); err != nil {
		t.Errorf("without a fixture: %v", err)
	} else if m, d := r.Manifest, r.Diagnostics; m.Mode != structure.ModeAutomatic || m.PlacementParameters == nil || m.FeatureCount != d.Placed[structure.TypeFault] || m.FeatureCount+len(d.Omitted) != 4 {
		t.Errorf("without a fixture: mode %q, %d features, omitted %v", m.Mode, m.FeatureCount, d.Omitted)
	}
	o.Features = fixture(t)
	// Without a seed, the continental input's seed is used and said so.
	r, err := structure.Generate(t.Context(), o)
	if err != nil {
		t.Fatal(err)
	}
	if r.Manifest.Seed != 42 || r.Manifest.SeedSource != structure.SeedFromInput {
		t.Errorf("default seed %d from %q", r.Manifest.Seed, r.Manifest.SeedSource)
	}
	o.Fraction = 1.5
	if _, err := structure.Generate(t.Context(), o); err == nil {
		t.Error("accepted fraction 1.5")
	}
}

// Each edit breaks one rule of the fixture; the error must name the rule.
func TestValidation(t *testing.T) {
	base := string(fixture(t))
	dom := structure.Domain{WidthKM: 576, HeightKM: 288}
	for _, c := range []struct{ name, old, new, want string }{
		{"format", `features-v1"`, `features-v2"`, "unsupported format"},
		{"domain", `"width_km": 576,`, `"width_km": 600,`, "does not match the input"},
		{"unknown field", `"id": 4,`, `"id": 4, "depth": 1,`, "depth"},
		{"zero id", `"id": 4,`, `"id": 0,`, "id 0"},
		{"repeated id", `"id": 4,`, `"id": 3,`, "repeated"},
		{"unknown type", `"type": "fault"`, `"type": "volcano"`, "unknown type"},
		{"missing key", `"key": "4", `, ``, "key is required"},
		{"missing parent", `"key": "2", "parent": 1,`, `"key": "2", "parent": 9,`, "does not exist"},
		{"wrong parent type", `"key": "2", "parent": 1,`, `"key": "2", "parent": 4,`, "must be a mountain_belt"},
		{"plateau on the wrong flank", `"parent": 1, "side": "left"`, `"parent": 1, "side": "right"`, "elevated_flank"},
		{"basin without a parent", `"kind": "interior"`, `"kind": "foreland"`, "parent must be"},
		{"missing maturity", `"maturity": 0, `, ``, "maturity is required"},
		{"maturity range", `"maturity": 0,`, `"maturity": 1.5,`, "maturity is required"},
		{"belt kind", `"kind": "collision"`, `"kind": "fold"`, "kind"},
		{"axis fraction", `"axis_fraction": 0.4`, `"axis_fraction": 1`, "axis_fraction"},
		{"fault sense", `"sense": "strike_slip"`, `"sense": "sideways"`, "sense"},
		{"field of another type", `"sense": "strike_slip"`, `"sense": "strike_slip", "maturity": 0.5`, "does not apply"},
		{"fault targets", `"sense": "strike_slip"`, `"sense": "strike_slip", "targets": {}`, "does not apply"},
		{"target range", `[2500, 4500]`, `[4500, 2500]`, "min <= max"},
		{"width", `"x": 50, "y": 50, "width_km": 20`, `"x": 50, "y": 50, "width_km": 0`, "positive width_km"},
		{"edge", `"edge_km": 10`, `"edge_km": 0`, "edge_km"},
		{"first vertex", `"x": 50, "y": 50,`, `"x": 650, "y": 50,`, "outside the base domain"},
		{"first vertex y", `"x": 200, "y": 250,`, `"x": 200, "y": 288,`, "outside the base domain"},
		{"half the world east", `"x": 120, "y": 90,`, `"x": 340, "y": 90,`, "half the world"},
		{"half the world south", `"x": 120, "y": 90,`, `"x": 120, "y": 200,`, "half the world"},
		{"zero-length segment", `"x": 120, "y": 90,`, `"x": 50, "y": 50,`, "zero-length"},
		{"one sample", `, { "x": 120, "y": 90, "width_km": 20 }`, ``, "at least two"},
		{"outline not simple", `{ "x": 380, "y": 150 }, { "x": 380, "y": 210 }`, `{ "x": 380, "y": 210 }, { "x": 380, "y": 150 }`, "not simple"},
		{"two vertices", `, { "x": 500, "y": 200 }`, ``, "at least three"},
	} {
		edited := strings.Replace(base, c.old, c.new, 1)
		if edited == base {
			t.Fatalf("%s: edit did not apply", c.name)
		}
		ff, err := structure.ParseFeatures([]byte(edited))
		if err == nil {
			err = ff.Validate(dom)
		}
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v, want an error containing %q", c.name, err, c.want)
		}
	}
	ff, err := structure.ParseFeatures([]byte(base))
	if err != nil {
		t.Fatal(err)
	}
	if err := ff.Validate(dom); err != nil {
		t.Fatal(err)
	}
	if w := ff.Warnings(); len(w) != 0 {
		t.Errorf("unexpected warnings: %v", w)
	}
}

// A backbone that crosses itself loads, with a warning.
func TestSelfCrossingWarns(t *testing.T) {
	edited := strings.Replace(string(fixture(t)),
		`{ "x": 50, "y": 50, "width_km": 20 }, { "x": 120, "y": 90, "width_km": 20 }`,
		`{ "x": 50, "y": 50, "width_km": 20 }, { "x": 120, "y": 90, "width_km": 20 }, { "x": 120, "y": 50, "width_km": 20 }, { "x": 60, "y": 90, "width_km": 20 }`, 1)
	r, err := structure.Generate(t.Context(), structure.Options{In: continents(t), Fraction: 0.325, Features: []byte(edited), Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if w := r.Diagnostics.Warnings; len(w) != 1 || !strings.Contains(w[0], "feature 4") {
		t.Errorf("warnings: %v", w)
	}
}

// uncentered writes the 96 x 48 artifact of continents without its own
// recentering, so the structure stage has a shift to make.
func uncentered(t *testing.T) string {
	t.Helper()
	p, err := fracture.DefaultParams(42, 96, 48, 6)
	if err != nil {
		t.Fatal(err)
	}
	p.Recenter = false
	r, err := fracture.Generate(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "uncentered")
	if err := continent.Write(dir, continent.NewManifest("test", r), r); err != nil {
		t.Fatal(err)
	}
	return dir
}

func generate(t *testing.T, o structure.Options) *structure.Result {
	t.Helper()
	o.Seed, o.Fraction, o.Version = new(uint64(7)), 0.325, "test"
	r, err := structure.Generate(t.Context(), o)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// shifted reports whether raster b is raster a with cell (col, row) moved to
// ((col+dx) mod cols, (row+dy) mod rows).
func shifted[T comparable](a, b []T, cols, rows, dx, dy int) bool {
	for row := range rows {
		for col := range cols {
			if a[row*cols+col] != b[(row+dy)%rows*cols+(col+dx)%cols] {
				return false
			}
		}
	}
	return true
}

func TestRecenter(t *testing.T) {
	in := uncentered(t)
	for _, features := range [][]byte{nil, fixture(t)} {
		a := generate(t, structure.Options{In: in, Features: features})
		b := generate(t, structure.Options{In: in, Features: features, Recenter: true})
		g := a.Manifest.Grid
		cols, rows := g.Columns, g.Rows
		c := b.Manifest.Recentering
		if a.Manifest.Recentering.Applied || !c.Applied || c.ShiftX == 0 || c.ShiftY == 0 ||
			c.ShiftXKM != float64(c.ShiftX)*6 || c.ShiftYKM != float64(c.ShiftY)*6 {
			t.Fatalf("recentering %+v, unrecentered %+v", c, a.Manifest.Recentering)
		}
		// Every raster moves by the same whole cells.
		if !shifted(a.Land, b.Land, cols, rows, c.ShiftX, c.ShiftY) || !shifted(a.Owner, b.Owner, cols, rows, c.ShiftX, c.ShiftY) ||
			!shifted(a.Rock, b.Rock, cols, rows, c.ShiftX, c.ShiftY) {
			t.Error("mask, ownership, or rock did not move with the shift")
		}
		for _, set := range []struct {
			a, b map[string]*fracture.FractureField
		}{{a.Fields, b.Fields}, {a.Geology, b.Geology}} {
			for name, f := range set.a {
				if !shifted(f.Values, set.b[name].Values, cols, rows, c.ShiftX, c.ShiftY) {
					t.Errorf("field %s did not move with the shift", name)
				}
			}
		}
		// Geometry moves by the shift in km, then by whole worlds; IDs and
		// keys are kept, and every feature answers queries as before.
		dom := a.Domain()
		for i, fa := range a.Features.Features {
			fb := b.Features.Features[i]
			if fa.ID != fb.ID || *fa.Key != *fb.Key || !slices.Equal(fa.Provinces, fb.Provinces) {
				t.Errorf("feature %d: identity changed", fa.ID)
			}
			sa, sb := fa.Shape(dom), fb.Shape(dom)
			for x := 3.0; x < dom.WidthKM; x += 37 {
				for y := 3.0; y < dom.HeightKM; y += 29 {
					if sa.Contains(structure.Point{X: x, Y: y}) != sb.Contains(structure.Point{X: x + c.ShiftXKM, Y: y + c.ShiftYKM}) {
						t.Fatalf("feature %d: envelope differs at (%v, %v) after shifting", fa.ID, x, y)
					}
				}
			}
		}
		for i, pa := range a.Provinces.Provinces {
			pb := b.Provinces.Provinces[i]
			x := math.Mod(pa.Seed.X+c.ShiftXKM, dom.WidthKM)
			y := math.Mod(pa.Seed.Y+c.ShiftYKM, dom.HeightKM)
			if pb.ID != pa.ID || pb.Type != pa.Type || pb.Component != pa.Component || math.Abs(pb.Seed.X-x) > 1e-9 || math.Abs(pb.Seed.Y-y) > 1e-9 {
				t.Errorf("province %d: %+v, want %+v moved to (%v, %v)", pa.ID, pb, pa, x, y)
			}
		}
		if !reflect.DeepEqual(a.Diagnostics.Rocks, b.Diagnostics.Rocks) || !reflect.DeepEqual(a.Diagnostics.Placed, b.Diagnostics.Placed) {
			t.Error("diagnostics changed")
		}
		// The coast moves too.
		if da, db := a.Coast.DistanceAt(structure.Point{X: 100, Y: 100}), b.Coast.DistanceAt(structure.Point{X: 100 + c.ShiftXKM, Y: 100 + c.ShiftYKM}); math.Abs(da-db) > 1e-9 {
			t.Errorf("coast distance %v, moved %v", da, db)
		}
		// The result is centered: centering it again would not move it.
		again, err := fracture.FindCentering(t.Context(), b.Fields[continent.FieldAffinity], 0.325)
		if err != nil {
			t.Fatal(err)
		}
		if again.X != 0 || again.Y != 0 {
			t.Errorf("recentered result would move again by (%d, %d)", again.X, again.Y)
		}
		// It is written, loaded, and repeatable byte for byte.
		dir := filepath.Join(t.TempDir(), "a")
		if err := structure.Write(t.Context(), dir, b, 1); err != nil {
			t.Fatal(err)
		}
		if _, err := structure.Load(t.Context(), dir); err != nil {
			t.Fatal(err)
		}
		dir2 := filepath.Join(t.TempDir(), "b")
		if err := structure.Write(t.Context(), dir2, generate(t, structure.Options{In: in, Features: features, Recenter: true}), 1); err != nil {
			t.Fatal(err)
		}
		sameFiles(t, files(t, dir), files(t, dir2))
	}
}

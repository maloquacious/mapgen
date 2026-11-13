// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package landmask_test

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json/v2"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/maloquacious/mapgen/internal/continent"
	"github.com/maloquacious/mapgen/internal/fracture"
	"github.com/maloquacious/mapgen/internal/landmask"
)

func fixture(t *testing.T) string {
	t.Helper()
	// A tied plateau crossing the west edge, plus a higher cell at its edge.
	values := []float32{9, 8, 0, 0, 0, 8, 8, 8, 0, 0, 0, 8, 0, 0, 0, 0, 0, 0}
	f := &fracture.FractureField{Columns: 6, Rows: 3, WidthKM: 36, HeightKM: 18, Values: values}
	coast := *f
	coast.Values = make([]float32, len(values))
	for i := range coast.Values {
		coast.Values[i] = float32(i)
	}
	r := &fracture.Result{Params: fracture.Params{Seed: 42, Columns: 6, Rows: 3, CellKM: 6}, Affinity: f, Continental: f, Coast: &coast}
	dir := t.TempDir()
	if err := continent.Write(dir, continent.NewManifest("fixture", r), r); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestSelectionAndValidation(t *testing.T) {
	dir := fixture(t)
	for _, tc := range []struct {
		fraction float64
		count    int
	}{{0, 0}, {1, 18}, {0.325, 6}, {0.25, 5}} {
		r, err := landmask.Generate(t.Context(), dir, tc.fraction, false, "test")
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, v := range r.Land {
			count += int(v)
		}
		if count != tc.count {
			t.Fatalf("fraction %g: got %d, want %d", tc.fraction, count, tc.count)
		}
		if tc.fraction == 0.25 {
			// The sixth equal-valued candidate loses to earlier input indices.
			want := []byte{1, 1, 0, 0, 0, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
			if !slices.Equal(r.Land, want) {
				t.Fatalf("tied selection: %v", r.Land)
			}
		}
	}
	for _, f := range []float64{-0.1, 1.1, math.NaN(), math.Inf(1)} {
		if _, err := landmask.Generate(t.Context(), dir, f, false, "test"); err == nil {
			t.Errorf("accepted fraction %g", f)
		}
	}
	path := filepath.Join(dir, "coast.f32")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	b[0] ^= 1
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := landmask.Generate(t.Context(), dir, 0.325, false, "test"); err == nil {
		t.Fatal("accepted corrupted input field")
	}
}

func TestRecenterAndSelfContainedCheckpoint(t *testing.T) {
	dir := fixture(t)
	before, err := landmask.Generate(t.Context(), dir, 0.25, false, "test")
	if err != nil {
		t.Fatal(err)
	}
	r, err := landmask.Generate(t.Context(), dir, 0.25, true, "test")
	if err != nil {
		t.Fatal(err)
	}
	c := r.Manifest.Recentering
	if !c.Applied || c.X == 0 && c.Y == 0 {
		t.Fatalf("fixture was not moved: %+v", c)
	}
	g := r.Manifest.Grid
	for i, v := range before.Land {
		j := ((i/g.Columns+c.Y)%g.Rows)*g.Columns + (i%g.Columns+c.X)%g.Columns
		if r.Land[j] != v {
			t.Fatal("mask was not translated intact")
		}
		for name, f := range before.Fields {
			if r.Fields[name].Values[j] != f.Values[i] {
				t.Fatalf("%s is misaligned", name)
			}
		}
	}
	out := filepath.Join(t.TempDir(), "checkpoint")
	if err := landmask.Write(t.Context(), out, r, 1); err != nil {
		t.Fatal(err)
	}
	if err := landmask.Write(t.Context(), out, r, 1); err == nil {
		t.Fatal("overwrote accepted checkpoint")
	}
	// Loading no longer needs the source artifact to exist.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	got, err := landmask.Load(out)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Land, r.Land) || got.Manifest.LandCells != 5 || got.Manifest.Input.Manifest.Seed != 42 {
		t.Fatal("checkpoint changed")
	}
	for name, f := range r.Fields {
		if !slices.Equal(got.Fields[name].Values, f.Values) {
			t.Fatalf("%s changed in round trip", name)
		}
	}
	path := filepath.Join(out, "landmask.u8")
	bad := slices.Clone(r.Land)
	bad[0] ^= 1
	if err := os.WriteFile(path, bad, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := landmask.Load(out); err == nil {
		t.Fatal("accepted corrupted mask")
	}
}

func TestRecenterEmptyMask(t *testing.T) {
	r, err := landmask.Generate(t.Context(), fixture(t), 0, true, "test")
	if err != nil {
		t.Fatal(err)
	}
	c := r.Manifest.Recentering
	if c.X != 0 || c.Y != 0 || !c.XAmbiguous || !c.YAmbiguous || r.Manifest.LandCells != 0 {
		t.Fatalf("empty mask moved or counted land: %+v", c)
	}
}

func TestIndependentOfInputPath(t *testing.T) {
	a := fixture(t)
	b := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.CopyFS(b, os.DirFS(a)); err != nil {
		t.Fatal(err)
	}
	var manifests [][]byte
	for _, in := range []string{a, b} {
		r, err := landmask.Generate(t.Context(), in, 0.25, true, "test")
		if err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(t.TempDir(), "out")
		if err := landmask.Write(t.Context(), out, r, 1); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(out, landmask.ManifestFile))
		if err != nil {
			t.Fatal(err)
		}
		manifests = append(manifests, data)
	}
	if !slices.Equal(manifests[0], manifests[1]) {
		t.Fatal("manifest depends on the input directory")
	}
}

func TestWriteRejectsIncompleteResult(t *testing.T) {
	r, err := landmask.Generate(t.Context(), fixture(t), 0.25, false, "test")
	if err != nil {
		t.Fatal(err)
	}
	delete(r.Fields, continent.FieldCoast)
	out := filepath.Join(t.TempDir(), "out")
	if err := landmask.Write(t.Context(), out, r, 0); err == nil {
		t.Fatal("wrote a result missing a field")
	}
	if _, err := os.Lstat(out); !os.IsNotExist(err) {
		t.Fatal("failed write left an output directory")
	}
}

// TestLoadRejectsInvalidCheckpoint edits a saved checkpoint so that every
// checksum still matches but its contents or metadata are inconsistent.
func TestLoadRejectsInvalidCheckpoint(t *testing.T) {
	dir := fixture(t)
	for name, corrupt := range map[string]func(t *testing.T, out string, m *landmask.Manifest){
		"nonfinite field": func(t *testing.T, out string, m *landmask.Manifest) {
			fd := &m.Fields[len(m.Fields)-1]
			path := filepath.Join(out, fd.File)
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			binary.LittleEndian.PutUint32(b, math.Float32bits(float32(math.Inf(1))))
			if err := os.WriteFile(path, b, 0o644); err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(b)
			fd.SHA256 = hex.EncodeToString(sum[:])
		},
		"cell spacing": func(t *testing.T, out string, m *landmask.Manifest) { m.Grid.CellWidthKM *= 2 },
		"field list": func(t *testing.T, out string, m *landmask.Manifest) {
			slices.Reverse(m.Input.Manifest.Fields)
		},
	} {
		t.Run(name, func(t *testing.T) {
			r, err := landmask.Generate(t.Context(), dir, 0.25, false, "test")
			if err != nil {
				t.Fatal(err)
			}
			out := filepath.Join(t.TempDir(), "out")
			if err := landmask.Write(t.Context(), out, r, 0); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(out, landmask.ManifestFile)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var m landmask.Manifest
			if err := json.Unmarshal(data, &m); err != nil {
				t.Fatal(err)
			}
			corrupt(t, out, &m)
			if data, err = json.Marshal(m); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := landmask.Load(out); err == nil {
				t.Fatal("accepted invalid checkpoint")
			}
		})
	}
}

// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package continent_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/maloquacious/mapgen/internal/continent"
	"github.com/maloquacious/mapgen/internal/fracture"
)

func TestRoundTrip(t *testing.T) {
	p, err := fracture.DefaultParams(7, 96, 48, 6)
	if err != nil {
		t.Fatal(err)
	}
	res, err := fracture.Generate(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	m := continent.NewManifest("test", res)
	if err := continent.Write(dir, m, res); err != nil {
		t.Fatal(err)
	}

	src, got, err := continent.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Seed != 7 || got.Grid.Columns != 96 || got.Grid.Rows != 48 || got.Recentering.ShiftX != res.Centering.X {
		t.Errorf("manifest %+v", got)
	}
	if !slices.Equal(src.Field().Values, res.Affinity.Values) {
		t.Error("affinity changed in round trip")
	}
	for _, name := range []string{continent.FieldContinental, continent.FieldCoast} {
		if _, err := continent.ReadField(dir, got, name); err != nil {
			t.Error(err)
		}
	}
	if x, y := 1234.5, 600.0; src.Sample(x, y) != fracture.NewContinentSource(res.Affinity).Sample(x, y) {
		t.Error("sample differs")
	}

	// Corruption is detected.
	path := filepath.Join(dir, "affinity.f32")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[100] ^= 1
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := continent.Load(dir); !errors.Is(err, continent.ErrChecksum) {
		t.Errorf("corrupt file: %v", err)
	}
	if _, err := continent.ReadField(dir, got, "nope"); !errors.Is(err, continent.ErrNoField) {
		t.Errorf("missing field: %v", err)
	}
}

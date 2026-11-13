// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package trench

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"math"

	"github.com/maloquacious/mapgen/internal/structure"
)

// recenter translates the complete result, as structure's --recenter does:
// the structure records by structure's own translation, and the trench
// records by the same distance in km, each backbone then renormalized so
// its first vertex is in the base domain. Nothing is regenerated, and IDs
// and keys are kept.
//
// The centering depends only on the provisional mask, so recentering here
// moves an unrecentered structure exactly as structure's --recenter would,
// and leaves a recentered one where it is.
func recenter(ctx context.Context, r *Result) error {
	step, err := r.Structure.Recenter(ctx)
	if err != nil {
		return err
	}
	r.Manifest.Recentering = step
	if step.ShiftX == 0 && step.ShiftY == 0 {
		return nil
	}
	dom := r.Domain()
	for i := range r.Trenches.Trenches {
		translate(dom, r.Trenches.Trenches[i].Backbone, step.ShiftXKM, step.ShiftYKM)
	}
	if err := r.Trenches.Validate(r.Structure.Features); err != nil {
		return fmt.Errorf("recenter: %w", err)
	}
	// Write saves the translated records and fills in their checksums.
	rm := r.Structure.Manifest
	r.Manifest.RecenteredRecords, r.records = &rm, nil
	return nil
}

// translate moves a backbone by (dx, dy) km, then by whole world widths and
// heights so its first vertex lies in the base domain, as structure moves
// its features.
func translate(dom structure.Domain, b []structure.Sample, dx, dy float64) {
	kx := wraps(b[0].X+dx, dom.WidthKM) * dom.WidthKM
	ky := wraps(b[0].Y+dy, dom.HeightKM) * dom.HeightKM
	for i := range b {
		b[i].X = (b[i].X + dx) - kx
		b[i].Y = (b[i].Y + dy) - ky
	}
}

// wraps returns the whole number of periods k such that v - k*period lies in
// [0, period), guarding against rounding at the ends.
func wraps(v, period float64) float64 {
	k := math.Floor(v / period)
	switch w := v - k*period; {
	case w >= period:
		k++
	case w < 0:
		k--
	}
	return k
}

// checkRecentering verifies that the recentering record and the records
// manifest agree: translated records are described exactly when this stage
// moved them, and their manifest differs from the input's only in the
// record files and the recentering, accumulated from the input's.
func (m *Manifest) checkRecentering() error {
	c := m.Recentering
	moved := c.Applied && (c.ShiftX != 0 || c.ShiftY != 0)
	if !c.Applied && c != (structure.Recentering{}) {
		return fmt.Errorf("recentering is not applied but records a translation")
	}
	if moved != (m.RecenteredRecords != nil) {
		return fmt.Errorf("recentered records do not match the recentering record")
	}
	if !moved {
		return nil
	}
	g := m.Grid
	if c.ShiftX < 0 || c.ShiftX >= g.Columns || c.ShiftY < 0 || c.ShiftY >= g.Rows ||
		c.ShiftXKM != float64(c.ShiftX)*g.CellWidthKM || c.ShiftYKM != float64(c.ShiftY)*g.CellHeightKM {
		return fmt.Errorf("invalid recentering shift (%d, %d)", c.ShiftX, c.ShiftY)
	}
	in, got := m.Input.Manifest, *m.RecenteredRecords
	want := c
	if p := in.Recentering; p.Applied {
		want.ShiftX, want.ShiftY = (p.ShiftX+c.ShiftX)%g.Columns, (p.ShiftY+c.ShiftY)%g.Rows
		want.ShiftXKM, want.ShiftYKM = float64(want.ShiftX)*g.CellWidthKM, float64(want.ShiftY)*g.CellHeightKM
	}
	if got.Recentering != want {
		return fmt.Errorf("recentered records do not accumulate the input's recentering")
	}
	got.Recentering = in.Recentering
	got.ProvisionalLand, got.Features, got.Provinces, got.ProvinceOwnership = in.ProvisionalLand, in.Features, in.Provinces, in.ProvinceOwnership
	got.Rock, got.GeologyFields = in.Rock, in.GeologyFields
	a, err := json.Marshal(got, json.Deterministic(true))
	if err != nil {
		return err
	}
	b, err := json.Marshal(in, json.Deterministic(true))
	if err != nil {
		return err
	}
	if !bytes.Equal(a, b) {
		return fmt.Errorf("recentered records differ from the input manifest beyond their files and recentering")
	}
	return nil
}

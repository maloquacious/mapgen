// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package terrain

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/maloquacious/mapgen/internal/continent"
	"github.com/maloquacious/mapgen/internal/fracture"
	"github.com/maloquacious/mapgen/internal/structure"
	"github.com/maloquacious/mapgen/internal/trench"
)

// recenter translates the complete result by whole cells so that the
// largest landmass terrain implies is centered: the largest eight-connected
// component of the cells above the provisional-fraction quantile, with the
// convention continents, structure, and trench use. Nothing is
// regenerated, and IDs and keys are kept.
//
// The rasters shift cyclically; the structure records move by structure's
// own translation, and the trench and ridge records by the same distance in
// km, each backbone then renormalized so its first vertex is in the base
// domain, and each point into the base domain.
func recenter(ctx context.Context, r *Result) error {
	g := r.Manifest.Grid
	f := &fracture.FractureField{Columns: g.Columns, Rows: g.Rows, WidthKM: g.WidthKM, HeightKM: g.HeightKM, Values: r.Elevation}
	c, err := fracture.FindCentering(ctx, f, r.Manifest.ProvisionalLandFraction)
	if err != nil {
		return fmt.Errorf("recenter: %w", err)
	}
	step := structure.Recentering{
		Applied: true, ShiftX: c.X, ShiftY: c.Y, ShiftXKM: float64(c.X) * g.CellWidthKM, ShiftYKM: float64(c.Y) * g.CellHeightKM,
		XAmbiguous: c.XAmbiguous, YAmbiguous: c.YAmbiguous, ComponentCells: c.ComponentCells, Components: c.Components,
		Transform: structure.RecenterTransform,
	}
	r.Manifest.Recentering = step
	if c.X == 0 && c.Y == 0 {
		return nil
	}
	tm, err := MoveRecords(ctx, r.Structure, r.Trenches, r.Ridges, r.Manifest.Input.Manifest, step)
	if err != nil {
		return err
	}
	for _, v := range [][]float32{r.Elevation, r.High, r.Relief, r.foundation} {
		if v != nil {
			structure.Rotate(v, g.Columns, g.Rows, c.X, c.Y)
		}
	}
	// Write saves the moved records and fills in their files and checksums.
	r.Manifest.RecenteredRecords, r.source = &tm, ""
	return ctx.Err()
}

// MoveRecords translates retained structure, trench, and ridge records by
// a later stage's recentering step, and returns the trench manifest that
// describes them: base, the manifest that described them before, as the
// trench stage would have written it had it recentered by the combined
// shift, its own recentered_records the moved structure manifest.
// WriteMovedRecords saves them and fills in their files and checksums.
func MoveRecords(ctx context.Context, s *structure.Result, tf *trench.TrenchFile, rf *RidgeFile, base trench.Manifest, step structure.Recentering) (trench.Manifest, error) {
	if err := s.RecenterBy(ctx, step); err != nil {
		return trench.Manifest{}, err
	}
	dx, dy := step.ShiftXKM, step.ShiftYKM
	tf.Translate(dx, dy)
	if err := tf.Validate(s.Features); err != nil {
		return trench.Manifest{}, fmt.Errorf("recenter: %w", err)
	}
	rf.Translate(dx, dy)
	if err := rf.Validate(s.Features); err != nil {
		return trench.Manifest{}, fmt.Errorf("recenter: %w", err)
	}
	g := base.Grid
	tm := base
	acc := step
	if prev := tm.Recentering; prev.Applied {
		acc.ShiftX, acc.ShiftY = (prev.ShiftX+step.ShiftX)%g.Columns, (prev.ShiftY+step.ShiftY)%g.Rows
		acc.ShiftXKM, acc.ShiftYKM = float64(acc.ShiftX)*g.CellWidthKM, float64(acc.ShiftY)*g.CellHeightKM
	}
	tm.Recentering = acc
	sm := s.Manifest
	tm.RecenteredRecords = &sm
	return tm, ctx.Err()
}

// WriteMovedRecords writes moved structure and trench records to dir and
// returns tm, from MoveRecords, with their files and checksums.
func WriteMovedRecords(dir string, s *structure.Result, tf *trench.TrenchFile, tm trench.Manifest) (trench.Manifest, error) {
	rm, err := structure.WriteRecords(dir, s)
	if err != nil {
		return tm, err
	}
	tdata, err := tf.Marshal()
	if err != nil {
		return tm, err
	}
	if err := os.WriteFile(filepath.Join(dir, trench.TrenchesFile), tdata, 0o644); err != nil {
		return tm, err
	}
	tm.RecenteredRecords, tm.Retained = &rm, rm.Records()
	tm.Trenches.SHA256 = checksum(tdata)
	return tm, nil
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

// Translate moves every record by (dx, dy) km, each backbone then
// renormalized so its first vertex is in the base domain, and each point
// into the base domain.
func (rf *RidgeFile) Translate(dx, dy float64) {
	dom := rf.Domain
	line := func(b []structure.Sample) {
		kx := wraps(b[0].X+dx, dom.WidthKM) * dom.WidthKM
		ky := wraps(b[0].Y+dy, dom.HeightKM) * dom.HeightKM
		for i := range b {
			b[i].X, b[i].Y = (b[i].X+dx)-kx, (b[i].Y+dy)-ky
		}
	}
	point := func(x, y float64) (float64, float64) {
		x, y = x+dx, y+dy
		return x - wraps(x, dom.WidthKM)*dom.WidthKM, y - wraps(y, dom.HeightKM)*dom.HeightKM
	}
	for i := range rf.Ridges {
		line(rf.Ridges[i].Backbone)
	}
	for i := range rf.Troughs {
		line(rf.Troughs[i].Backbone)
	}
	for i := range rf.Corridors {
		c := &rf.Corridors[i]
		line(c.Backbone)
		for j := range c.Saddles {
			c.Saddles[j].X, c.Saddles[j].Y = point(c.Saddles[j].X, c.Saddles[j].Y)
		}
	}
	for i := range rf.Peaks {
		rf.Peaks[i].X, rf.Peaks[i].Y = point(rf.Peaks[i].X, rf.Peaks[i].Y)
	}
}

// RecordsManifest returns the trench manifest that describes the retained
// records: the input's, or the recentered one.
func (m *Manifest) RecordsManifest() trench.Manifest {
	if m.RecenteredRecords != nil {
		return *m.RecenteredRecords
	}
	return m.Input.Manifest
}

// checkRecentering verifies that the recentering record and the records
// manifest agree: moved records are described exactly when this stage moved
// them, and their manifest differs from the input's only in the record
// files and the recentering, accumulated from the input's.
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
	return CheckMovedRecords(m.Grid, m.Input.Manifest, *m.RecenteredRecords, c)
}

// CheckMovedRecords verifies that got describes base's records moved by a
// later stage's recentering step c, as MoveRecords describes them: c is a
// whole-cell shift, got's recentering accumulates base's and c, and got
// differs from base in nothing else but the record files.
func CheckMovedRecords(g continent.Grid, base, got trench.Manifest, c structure.Recentering) error {
	if c.ShiftX < 0 || c.ShiftX >= g.Columns || c.ShiftY < 0 || c.ShiftY >= g.Rows ||
		c.ShiftXKM != float64(c.ShiftX)*g.CellWidthKM || c.ShiftYKM != float64(c.ShiftY)*g.CellHeightKM {
		return fmt.Errorf("invalid recentering shift (%d, %d)", c.ShiftX, c.ShiftY)
	}
	in := base
	want := c
	if p := in.Recentering; p.Applied {
		want.ShiftX, want.ShiftY = (p.ShiftX+c.ShiftX)%g.Columns, (p.ShiftY+c.ShiftY)%g.Rows
		want.ShiftXKM, want.ShiftYKM = float64(want.ShiftX)*g.CellWidthKM, float64(want.ShiftY)*g.CellHeightKM
	}
	if got.Recentering != want {
		return fmt.Errorf("recentered records do not accumulate the input's recentering")
	}
	got.Recentering, got.RecenteredRecords, got.Retained, got.Trenches = in.Recentering, in.RecenteredRecords, in.Retained, in.Trenches
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

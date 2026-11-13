// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package structure

import (
	"context"
	"fmt"
	"math"

	"github.com/maloquacious/mapgen/internal/continent"
	"github.com/maloquacious/mapgen/internal/fracture"
)

// RecenterTransform describes the translation a recentered artifact applied.
const RecenterTransform = "cell (col, row) moved to ((col+shift_x) mod columns, (row+shift_y) mod rows); " +
	"geometry moved by (shift_x_km, shift_y_km), then each polyline, ring, and seed by whole world widths and heights so its first vertex is in the base domain"

// Recentering records whether, and by how much, the complete result was
// translated. Shifts are relative to the continental input, in cells east
// and south, and in km.
type Recentering struct {
	Applied        bool    `json:"applied"`
	ShiftX         int     `json:"shift_x"`
	ShiftY         int     `json:"shift_y"`
	ShiftXKM       float64 `json:"shift_x_km"`
	ShiftYKM       float64 `json:"shift_y_km"`
	XAmbiguous     bool    `json:"x_ambiguous,omitzero"`
	YAmbiguous     bool    `json:"y_ambiguous,omitzero"`
	ComponentCells int     `json:"component_cells,omitzero"`
	Components     int     `json:"components,omitzero"`
	Transform      string  `json:"transform,omitempty"`
}

// recenter translates every part of r so that the largest provisional land
// component is centered, with the convention continents and landmask use.
// It runs after generation is complete, so a recentered result is the
// unrecentered one moved by whole cells: nothing is regenerated, and IDs,
// keys, and component numbers are kept.
func recenter(ctx context.Context, r *Result) error {
	c, err := fracture.FindCentering(ctx, r.Fields[continent.FieldAffinity], r.Manifest.ProvisionalLandFraction)
	if err != nil {
		return fmt.Errorf("recenter: %w", err)
	}
	r.Manifest.Recentering = record(r.Manifest.Grid, c, c.X, c.Y)
	return translate(ctx, r, c.X, c.Y)
}

// Recenter translates a result read with LoadRecords so that its largest
// provisional land component is centered, as --recenter does, and returns
// this translation's record. The manifest's Recentering accumulates it, so
// it stays relative to the continental input. Later stages that retain the
// records use it to recenter without the continental fields.
//
// The centering depends only on the provisional mask, which is the mask
// --recenter centers, so an unrecentered result moves exactly as structure
// would have moved it, and a recentered one does not move.
func (r *Result) Recenter(ctx context.Context) (Recentering, error) {
	g := r.Manifest.Grid
	v := make([]float32, len(r.Land))
	for i, x := range r.Land {
		v[i] = float32(x)
	}
	f := &fracture.FractureField{Columns: g.Columns, Rows: g.Rows, WidthKM: g.WidthKM, HeightKM: g.HeightKM, Values: v}
	c, err := fracture.FindCentering(ctx, f, r.Manifest.ProvisionalLandFraction)
	if err != nil {
		return Recentering{}, fmt.Errorf("recenter: %w", err)
	}
	step := record(g, c, c.X, c.Y)
	x, y := c.X, c.Y
	if prev := r.Manifest.Recentering; prev.Applied {
		x, y = (prev.ShiftX+c.X)%g.Columns, (prev.ShiftY+c.Y)%g.Rows
	}
	r.Manifest.Recentering = record(g, c, x, y)
	return step, translate(ctx, r, c.X, c.Y)
}

// record describes a translation by (x, y) cells, with c's diagnostics.
func record(g continent.Grid, c fracture.Centering, x, y int) Recentering {
	return Recentering{
		Applied: true, ShiftX: x, ShiftY: y, ShiftXKM: float64(x) * g.CellWidthKM, ShiftYKM: float64(y) * g.CellHeightKM,
		XAmbiguous: c.XAmbiguous, YAmbiguous: c.YAmbiguous,
		ComponentCells: c.ComponentCells, Components: c.Components, Transform: RecenterTransform,
	}
}

// translate moves every part of r by (cx, cy) whole cells.
func translate(ctx context.Context, r *Result, cx, cy int) error {
	if cx == 0 && cy == 0 {
		return nil
	}
	g := r.Manifest.Grid
	c := struct{ X, Y int }{cx, cy}
	dx, dy := float64(c.X)*g.CellWidthKM, float64(c.Y)*g.CellHeightKM
	cols, rows := g.Columns, g.Rows
	r.Land = slide(r.Land, cols, rows, c.X, c.Y)
	r.Owner = slide(r.Owner, cols, rows, c.X, c.Y)
	if r.Rock != nil {
		r.Rock = slide(r.Rock, cols, rows, c.X, c.Y)
	}
	for _, fields := range []map[string]*fracture.FractureField{r.Fields, r.Geology} {
		for name, f := range fields {
			fields[name] = f.Slide(c.X, c.Y)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	dom := r.Domain()
	for i := range r.Features.Features {
		r.Features.Features[i].translate(dom, dx, dy)
	}
	for i := range r.Provinces.Provinces {
		s := &r.Provinces.Provinces[i].Seed
		*s = Point{s.X + dx, s.Y + dy}
		*s = Point{s.X - wraps(s.X, dom.WidthKM)*dom.WidthKM, s.Y - wraps(s.Y, dom.HeightKM)*dom.HeightKM}
	}
	if r.Coast != nil {
		r.Coast.translate(dx, dy)
	}
	// The translated features must still be valid stored geometry.
	if err := r.Features.Validate(dom); err != nil {
		return fmt.Errorf("recenter: %w", err)
	}
	return nil
}

// slide returns a copy of a row-major raster with cell (col, row) moved to
// ((col+dx) mod cols, (row+dy) mod rows).
func slide[T any](in []T, cols, rows, dx, dy int) []T {
	out := make([]T, len(in))
	dx, dy = (dx%cols+cols)%cols, (dy%rows+rows)%rows
	for row := range rows {
		src := in[row*cols : (row+1)*cols]
		dst := out[(row+dy)%rows*cols:][:cols]
		copy(dst[dx:], src[:cols-dx])
		copy(dst[:dx], src[cols-dx:])
	}
	return out
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

// translate moves a feature by (dx, dy) km, then by whole world widths and
// heights so its first vertex lies in the base domain.
func (f *Feature) translate(dom Domain, dx, dy float64) {
	if f.Linear() {
		kx := wraps(f.Backbone[0].X+dx, dom.WidthKM) * dom.WidthKM
		ky := wraps(f.Backbone[0].Y+dy, dom.HeightKM) * dom.HeightKM
		for i := range f.Backbone {
			f.Backbone[i].X = (f.Backbone[i].X + dx) - kx
			f.Backbone[i].Y = (f.Backbone[i].Y + dy) - ky
		}
		return
	}
	kx := wraps(f.Outline[0].X+dx, dom.WidthKM) * dom.WidthKM
	ky := wraps(f.Outline[0].Y+dy, dom.HeightKM) * dom.HeightKM
	for i := range f.Outline {
		f.Outline[i].X = (f.Outline[i].X + dx) - kx
		f.Outline[i].Y = (f.Outline[i].Y + dy) - ky
	}
}

// translate moves the coast by (dx, dy) km. The working grid need not hold a
// whole number of shifted cells, so the distance raster is not moved; queries
// are offset instead.
func (c *Coast) translate(dx, dy float64) {
	c.offset = Point{c.offset.X + dx, c.offset.Y + dy}
	for i := range c.Chains {
		for j := range c.Chains[i].Points {
			p := &c.Chains[i].Points[j]
			*p = Point{p.X + dx, p.Y + dy}
		}
	}
}

// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package structure

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
)

// Shape is the part of a feature's geometry that rasterization needs.
type Shape interface {
	// Bounds is an unwrapped box containing every point Contains accepts.
	Bounds() Rect
	// Contains reports whether p lies in the feature's envelope.
	Contains(p Point) bool
}

// Contains reports whether p lies within the belt's width envelope.
func (l *Line) Contains(p Point) bool { return l.Cross(p) <= 1 }

// Contains reports whether p lies inside or on the outline.
func (r *Region) Contains(p Point) bool { return r.Inside(p) >= 0 }

// Shape returns the geometry queries for a validated feature.
func (f *Feature) Shape(dom Domain) Shape {
	if f.Linear() {
		return NewLine(dom, f.Backbone)
	}
	return NewRegion(dom, f.Outline, f.EdgeKM)
}

// visit calls fn for every cell of a cols x rows raster over dom whose
// center lies in box, taking each wrapped cell once. Cell (col, row) has its
// center at ((col+0.5)*cw, (row+0.5)*ch).
func visit(dom Domain, cols, rows int, box Rect, fn func(col, row int, center Point)) {
	cw, ch := dom.WidthKM/float64(cols), dom.HeightKM/float64(rows)
	span := func(lo, hi, cell float64, n int) (first, count int) {
		a, b := int(math.Ceil(lo/cell-0.5)), int(math.Floor(hi/cell-0.5))
		if b-a+1 >= n {
			return 0, n
		}
		return a, max(b-a+1, 0)
	}
	c0, nc := span(box.MinX, box.MaxX, cw, cols)
	r0, nr := span(box.MinY, box.MaxY, ch, rows)
	for j := range nr {
		row := ((r0+j)%rows + rows) % rows
		for i := range nc {
			col := ((c0+i)%cols + cols) % cols
			fn(col, row, Point{(float64(col) + 0.5) * cw, (float64(row) + 0.5) * ch})
		}
	}
}

// Cover rasterizes a shape's envelope: cell (col, row) of the result is true
// when its center lies in the envelope. The raster is derived from the
// geometry; the geometry never depends on the raster.
func Cover(dom Domain, cols, rows int, s Shape) []bool {
	out := make([]bool, cols*rows)
	visit(dom, cols, rows, s.Bounds(), func(col, row int, p Point) {
		if s.Contains(p) {
			out[row*cols+col] = true
		}
	})
	return out
}

// Preview colors: a pale tone per province type, darker lines for province
// boundaries and the provisional coast, one tint per feature type, and
// backbones.
var (
	previewProvince = map[string]color.RGBA{
		TypeOceanic:     {214, 224, 232, 255},
		TypeShield:      {236, 226, 206, 255},
		TypeSedimentary: {228, 236, 208, 255},
		TypeVolcanic:    {240, 212, 206, 255},
	}
	previewBoundary = color.RGBA{176, 170, 156, 255}
	previewCoast    = color.RGBA{120, 130, 140, 255}
	previewLine     = color.RGBA{40, 30, 30, 255}
	previewTint     = map[string]color.RGBA{
		TypeMountainBelt: {150, 90, 50, 255},
		TypeRift:         {190, 60, 120, 255},
		TypeIslandArc:    {210, 120, 30, 255},
		TypeFault:        {110, 110, 110, 255},
		TypePlateau:      {120, 130, 60, 255},
		TypeBasin:        {70, 130, 170, 255},
	}
)

// writePreview draws provinces by type, province boundaries, the provisional
// coast, each feature's envelope, and each backbone, sampling every scale-th
// cell.
func writePreview(ctx context.Context, path string, r *Result, scale int) error {
	g := r.Manifest.Grid
	dom := r.Domain()
	w, h := (g.Columns-1)/scale+1, (g.Rows-1)/scale+1
	// The sampled raster covers the same extent with larger cells; sample
	// centers are close enough to cell centers for a diagnostic image.
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			// Compare with the next sampled cell east and south, wrapping.
			i := y*scale*g.Columns + x*scale
			e := y*scale*g.Columns + (x+1)%w*scale
			s := (y+1)%h*scale*g.Columns + x*scale
			c := previewProvince[r.Provinces.Provinces[r.Owner[i]-1].Type]
			switch {
			case r.Land[i] != r.Land[e] || r.Land[i] != r.Land[s]:
				c = previewCoast
			case r.Owner[i] != r.Owner[e] || r.Owner[i] != r.Owner[s]:
				c = previewBoundary
			}
			img.SetRGBA(x, y, c)
		}
	}
	blend := func(x, y int, c color.RGBA) {
		o := img.RGBAAt(x, y)
		img.SetRGBA(x, y, color.RGBA{uint8((int(o.R) + int(c.R)) / 2), uint8((int(o.G) + int(c.G)) / 2), uint8((int(o.B) + int(c.B)) / 2), 255})
	}
	// Areas first, so belts and their backbones stay visible above them.
	for _, linear := range []bool{false, true} {
		for i := range r.Features.Features {
			f := &r.Features.Features[i]
			if f.Linear() != linear {
				continue
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			s := f.Shape(dom)
			visit(dom, w, h, s.Bounds(), func(x, y int, p Point) {
				if s.Contains(p) {
					blend(x, y, previewTint[f.Type])
				}
			})
		}
	}
	half := 0.75 * dom.WidthKM / float64(w)
	for i := range r.Features.Features {
		f := &r.Features.Features[i]
		if !f.Linear() {
			continue
		}
		l := NewLine(dom, f.Backbone)
		visit(dom, w, h, l.Bounds(), func(x, y int, p Point) {
			if _, d, _ := l.Nearest(p); d <= half {
				img.SetRGBA(x, y, previewLine)
			}
		})
	}
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(out, img); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

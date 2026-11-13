// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package trench

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"os"

	"github.com/maloquacious/mapgen/internal/structure"
)

var (
	previewLand   = color.RGBA{214, 206, 184, 255}
	previewWater  = color.RGBA{170, 200, 224, 255}
	previewParent = color.RGBA{208, 120, 40, 255}
	previewTrench = color.RGBA{16, 32, 90, 255}
)

// writePreview draws the land and water trenches were placed against (the
// simplified provisional mask), the envelopes of active margins and island
// arcs, and trench footprints, sampling every scale-th cell.
func writePreview(ctx context.Context, path string, r *Result, scale int) error {
	g := r.Manifest.Grid
	dom := r.Domain()
	w, h := (g.Columns-1)/scale+1, (g.Rows-1)/scale+1
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	land := structure.Simplify(r.Manifest.Input.Manifest.ProvinceParameters, g, r.Structure.Land)
	for y := range h {
		for x := range w {
			c := previewWater
			if land[y*scale*g.Columns+x*scale] == 1 {
				c = previewLand
			}
			img.SetRGBA(x, y, c)
		}
	}
	paint := func(s structure.Shape, c color.RGBA) {
		for i, in := range structure.Cover(dom, w, h, s) {
			if in {
				img.SetRGBA(i%w, i/w, c)
			}
		}
	}
	for i := range r.Structure.Features.Features {
		if f := &r.Structure.Features.Features[i]; Subducts(f) {
			paint(f.Shape(dom), previewParent)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	for i := range r.Trenches.Trenches {
		paint(structure.NewLine(dom, r.Trenches.Trenches[i].Backbone), previewTrench)
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

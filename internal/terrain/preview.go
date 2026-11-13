// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package terrain

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
)

// stop is one entry of a hypsometric tint: elevation in m above the shifted
// level, and its color.
type stop struct {
	m float64
	c [3]float64
}

var (
	waterTint = []stop{
		{-6000, [3]float64{8, 24, 70}},
		{-3500, [3]float64{20, 56, 120}},
		{-1000, [3]float64{44, 96, 166}},
		{-150, [3]float64{86, 150, 206}},
		{0, [3]float64{150, 200, 232}},
	}
	landTint = []stop{
		{0, [3]float64{86, 140, 84}},
		{200, [3]float64{132, 170, 96}},
		{600, [3]float64{196, 186, 120}},
		{1500, [3]float64{176, 136, 92}},
		{3000, [3]float64{140, 104, 84}},
		{5000, [3]float64{238, 236, 232}},
	}
	previewCoastLine = color.RGBA{24, 24, 24, 255}
)

// tint interpolates the stops at m, clamping beyond the ends.
func tint(stops []stop, m float64) [3]float64 {
	if m <= stops[0].m {
		return stops[0].c
	}
	for i := 1; i < len(stops); i++ {
		if m <= stops[i].m {
			a, b := stops[i-1], stops[i]
			t := (m - a.m) / (b.m - a.m)
			return [3]float64{a.c[0] + t*(b.c[0]-a.c[0]), a.c[1] + t*(b.c[1]-a.c[1]), a.c[2] + t*(b.c[2]-a.c[2])}
		}
	}
	return stops[len(stops)-1].c
}

// shadeExaggeration multiplies slopes for hill shading, so broad foundation
// slopes still show.
const shadeExaggeration = 20

// writePreview draws a hypsometric tint with hill shading, relative to the
// level landmask's quantile would move to 0 m, and the coast that level
// implies as a line, sampling every scale-th cell.
func writePreview(ctx context.Context, path string, r *Result, scale int) error {
	g := r.Manifest.Grid
	w, h := (g.Columns-1)/scale+1, (g.Rows-1)/scale+1
	level := r.Diagnostics.Shift.LevelM
	at := func(x, y int) int { return ((y%h+h)%h)*scale*g.Columns + ((x%w+w)%w)*scale }
	// Sample spacing in m; the last sample on an axis may be closer to the
	// first, which only affects shading along the seam.
	dx, dy := 1000*g.CellWidthKM*float64(scale), 1000*g.CellHeightKM*float64(scale)
	// Light from the north-west, 45 degrees up. y increases southward.
	lx, ly, lz := -1/math.Sqrt(2)/math.Sqrt(2), -1/math.Sqrt(2)/math.Sqrt(2), 1/math.Sqrt(2)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		if err := ctx.Err(); err != nil {
			return err
		}
		for x := range w {
			i := at(x, y)
			e := float64(r.Elevation[i]) - level
			var c [3]float64
			if r.Above[i] {
				c = tint(landTint, e)
			} else {
				c = tint(waterTint, e)
			}
			gx := shadeExaggeration * float64(r.Elevation[at(x+1, y)]-r.Elevation[at(x-1, y)]) / (2 * dx)
			gy := shadeExaggeration * float64(r.Elevation[at(x, y+1)]-r.Elevation[at(x, y-1)]) / (2 * dy)
			// The surface normal is (-gx, -gy, 1), normalized.
			shade := (-gx*lx - gy*ly + lz) / math.Sqrt(gx*gx+gy*gy+1) / lz
			shade = min(max(shade, 0.55), 1.35)
			px := color.RGBA{A: 255}
			for k, v := range c {
				b := uint8(min(max(v*shade, 0), 255))
				switch k {
				case 0:
					px.R = b
				case 1:
					px.G = b
				case 2:
					px.B = b
				}
			}
			if r.Above[i] != r.Above[at(x+1, y)] || r.Above[i] != r.Above[at(x, y+1)] {
				px = previewCoastLine
			}
			img.SetRGBA(x, y, px)
		}
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

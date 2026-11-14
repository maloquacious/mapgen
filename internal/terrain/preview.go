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

	"github.com/maloquacious/mapgen/internal/structure"
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
	// Envelope outlines by feature type, backbones, and trench backbones in
	// the structure preview.
	previewEnvelope = map[string]color.RGBA{
		structure.TypeMountainBelt: {120, 40, 20, 255},
		structure.TypeRift:         {190, 40, 130, 255},
		structure.TypeIslandArc:    {230, 110, 0, 255},
		structure.TypeFault:        {90, 90, 90, 255},
		structure.TypePlateau:      {110, 120, 20, 255},
		structure.TypeBasin:        {20, 110, 170, 255},
	}
	previewBackbone = color.RGBA{30, 20, 20, 255}
	previewTrench   = color.RGBA{230, 30, 30, 255}
	previewChain    = color.RGBA{250, 250, 245, 255}
	previewBranch   = color.RGBA{200, 200, 190, 255}
	previewTrough   = color.RGBA{40, 90, 200, 255}
	previewCorridor = color.RGBA{0, 200, 220, 255}
	previewPeak     = color.RGBA{200, 0, 0, 255}
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

// rgba converts a tint to an opaque pixel, scaled by shade.
func rgba(c [3]float64, shade float64) color.RGBA {
	b := func(v float64) uint8 { return uint8(min(max(v*shade, 0), 255)) }
	return color.RGBA{b(c[0]), b(c[1]), b(c[2]), 255}
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
			px := rgba(c, min(max(shade, 0.55), 1.35))
			if r.Above[i] != r.Above[at(x+1, y)] || r.Above[i] != r.Above[at(x, y+1)] {
				px = previewCoastLine
			}
			img.SetRGBA(x, y, px)
		}
	}
	return encode(path, img)
}

// writeStructurePreview draws the surface in the same tint without shading,
// each feature's envelope outline, every backbone, and every trench backbone,
// ridge chains, branches, troughs, corridors, and volcanic peaks, sampling
// every scale-th cell.
func writeStructurePreview(ctx context.Context, path string, r *Result, scale int) error {
	g := r.Manifest.Grid
	dom := r.Structure.Features.Domain
	w, h := (g.Columns-1)/scale+1, (g.Rows-1)/scale+1
	level := r.Diagnostics.Shift.LevelM
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			i := y*scale*g.Columns + x*scale
			e := float64(r.Elevation[i]) - level
			c := tint(waterTint, e)
			if r.Above[i] {
				c = tint(landTint, e)
			}
			img.SetRGBA(x, y, rgba(c, 1))
		}
	}
	// A preview cell is on an outline when it is in the envelope and a
	// neighbor is not. stamp marks the cells of the feature being drawn with
	// its position in the list, plus one.
	ff := r.Structure.Features
	stamp := make([]int32, w*h)
	for i := range ff.Features {
		f := &ff.Features[i]
		s, id := f.Shape(dom), int32(i+1)
		structure.Visit(dom, w, h, s.Bounds(), func(x, y int, p structure.Point) {
			if s.Contains(p) {
				stamp[y*w+x] = id
			}
		})
		in := func(x, y int) bool { return stamp[(y+h)%h*w+(x+w)%w] == id }
		structure.Visit(dom, w, h, s.Bounds(), func(x, y int, _ structure.Point) {
			if in(x, y) && !(in(x+1, y) && in(x-1, y) && in(x, y+1) && in(x, y-1)) {
				img.SetRGBA(x, y, previewEnvelope[f.Type])
			}
		})
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	line := func(pts []structure.Sample, c color.RGBA) {
		// Step along each segment at a quarter of a preview cell.
		cw, ch := dom.WidthKM/float64(w), dom.HeightKM/float64(h)
		for k := 0; k+1 < len(pts); k++ {
			a, b := pts[k], pts[k+1]
			n := int(math.Ceil(4*max(math.Abs(b.X-a.X)/cw, math.Abs(b.Y-a.Y)/ch))) + 1
			for j := range n + 1 {
				t := float64(j) / float64(n)
				x := int(math.Floor((a.X + t*(b.X-a.X)) / cw))
				y := int(math.Floor((a.Y + t*(b.Y-a.Y)) / ch))
				img.SetRGBA((x%w+w)%w, (y%h+h)%h, c)
			}
		}
	}
	for i := range ff.Features {
		line(ff.Features[i].Backbone, previewBackbone)
	}
	for i := range r.Trenches.Trenches {
		line(r.Trenches.Trenches[i].Backbone, previewTrench)
	}
	if rf := r.Ridges; rf != nil {
		for _, t := range rf.Troughs {
			line(t.Backbone, previewTrough)
		}
		for _, rd := range rf.Ridges {
			c := previewChain
			if rd.Kind == RidgeBranch {
				c = previewBranch
			}
			line(rd.Backbone, c)
		}
		for _, cr := range rf.Corridors {
			line(cr.Backbone, previewCorridor)
		}
		cw, ch := dom.WidthKM/float64(w), dom.HeightKM/float64(h)
		for _, pk := range rf.Peaks {
			x, y := int(pk.X/cw), int(pk.Y/ch)
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					img.SetRGBA(((x+dx)%w+w)%w, ((y+dy)%h+h)%h, previewPeak)
				}
			}
		}
	}
	return encode(path, img)
}

// encode writes img as a PNG file.
func encode(path string, img image.Image) error {
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

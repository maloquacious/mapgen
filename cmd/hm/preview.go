// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bufio"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"slices"

	"github.com/maloquacious/mapgen/internal/fracture"
)

// writePreview renders f as an inspection image: cells above the provisional
// land threshold are land, shaded by rank; the rest are water. It is a debug
// artifact, not authoritative data, and its threshold is not sea level.
func writePreview(path string, f *fracture.FractureField, landFraction float64, scale int) error {
	sorted := slices.Clone(f.Values)
	slices.Sort(sorted)
	n := len(sorted)
	k := min(max(int(math.Round(landFraction*float64(n))), 0), n)
	// quantile maps a value to its rank in [0,1] by binary search.
	quantile := func(v float32) float64 {
		i, _ := slices.BinarySearch(sorted, v)
		return float64(i) / float64(max(n-1, 1))
	}
	sea := 1 - float64(k)/float64(n)

	const shades = 64
	pal := make(color.Palette, 0, 2*shades)
	for i := range shades { // water: deep to shallow
		t := float64(i) / (shades - 1)
		pal = append(pal, color.RGBA{lerp(8, 70, t), lerp(24, 130, t), lerp(70, 190, t), 255})
	}
	for i := range shades { // land: lowland green, upland tan, high white
		t := float64(i) / (shades - 1)
		var c color.RGBA
		switch {
		case t < 0.6:
			u := t / 0.6
			c = color.RGBA{lerp(60, 170, u), lerp(130, 160, u), lerp(60, 90, u), 255}
		default:
			u := (t - 0.6) / 0.4
			c = color.RGBA{lerp(170, 245, u), lerp(160, 245, u), lerp(90, 245, u), 255}
		}
		pal = append(pal, c)
	}

	w, h := (f.Columns+scale-1)/scale, (f.Rows+scale-1)/scale
	img := image.NewPaletted(image.Rect(0, 0, w, h), pal)
	for y := range h {
		for x := range w {
			q := quantile(f.Values[y*scale*f.Columns+x*scale])
			var idx int
			if q < sea {
				idx = int(q / sea * (shades - 1))
			} else {
				idx = shades + int((q-sea)/max(1-sea, 1e-9)*(shades-1))
			}
			img.Pix[y*img.Stride+x] = uint8(min(idx, 2*shades-1))
		}
	}

	tmp := path + ".tmp"
	fh, err := os.Create(tmp)
	if err != nil {
		return err
	}
	bw := bufio.NewWriterSize(fh, 1<<20)
	if err := png.Encode(bw, img); err != nil {
		_ = fh.Close()
		return err
	}
	if err := bw.Flush(); err != nil {
		_ = fh.Close()
		return err
	}
	if err := fh.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func lerp(a, b uint8, t float64) uint8 {
	return uint8(math.Round(float64(a) + (float64(b)-float64(a))*t))
}

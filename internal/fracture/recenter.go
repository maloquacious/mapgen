// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package fracture

import (
	"context"
	"fmt"
	"math"
	"slices"
)

const (
	// DefaultProvisionalLandFraction sizes the provisional mask used only to
	// choose the recentering shift. It does not set sea level.
	DefaultProvisionalLandFraction = 0.325

	// AmbiguityThreshold is the circular resultant magnitude per cell below
	// which an axis is treated as having no defined mean, and is not shifted.
	AmbiguityThreshold = 1e-6
)

// Centering records how a field was recentred. A source cell (col,row) moves
// to ((col+X) mod Columns, (row+Y) mod Rows).
type Centering struct {
	Applied                bool // false when recentering was disabled
	X, Y                   int
	XAmbiguous, YAmbiguous bool
	MaskCells              int // cells in the provisional mask
	ComponentCells         int // cells in the largest provisional component
	Components             int // provisional components, eight neighbors, both axes wrapping
}

// FindCentering builds a provisional mask of the round(landFraction*N) highest
// cells (ties broken by lower index), labels its components with eight
// neighbors wrapping both axes, and returns the integer shifts that move the
// circular mean of the largest component (ties: first in row-major order)
// nearest the domain center.
func FindCentering(ctx context.Context, f *FractureField, landFraction float64) (Centering, error) {
	if !(landFraction >= 0 && landFraction <= 1) {
		return Centering{}, fmt.Errorf("land fraction %v not in [0,1]", landFraction)
	}
	n := len(f.Values)
	k := int(math.Round(landFraction * float64(n)))
	mask := TopMask(f.Values, k)
	if err := ctx.Err(); err != nil {
		return Centering{}, err
	}

	cols, rows := f.Columns, f.Rows
	labels := make([]int32, n)
	var queue []int
	var sizes []int // sizes[label-1]
	for start := range n {
		if !mask[start] || labels[start] != 0 {
			continue
		}
		if len(sizes)%1024 == 0 {
			if err := ctx.Err(); err != nil {
				return Centering{}, err
			}
		}
		label := int32(len(sizes) + 1)
		labels[start] = label
		queue = append(queue[:0], start)
		for head := 0; head < len(queue); head++ {
			i := queue[head]
			row, col := i/cols, i%cols
			for dy := -1; dy <= 1; dy++ {
				r := mod(row+dy, rows)
				for dx := -1; dx <= 1; dx++ {
					j := r*cols + mod(col+dx, cols)
					if mask[j] && labels[j] == 0 {
						labels[j] = label
						queue = append(queue, j)
					}
				}
			}
		}
		sizes = append(sizes, len(queue))
	}

	c := Centering{MaskCells: k, Components: len(sizes)}
	if len(sizes) == 0 {
		c.XAmbiguous, c.YAmbiguous = true, true
		return c, nil
	}
	best := 0
	for l, s := range sizes {
		if s > sizes[best] {
			best = l
		}
	}
	c.ComponentCells = sizes[best]
	want := int32(best + 1)

	colCount := make([]int, cols)
	rowCount := make([]int, rows)
	for i, l := range labels {
		if l == want {
			colCount[i%cols]++
			rowCount[i/cols]++
		}
	}
	c.X, c.XAmbiguous = circularShift(colCount, c.ComponentCells)
	c.Y, c.YAmbiguous = circularShift(rowCount, c.ComponentCells)
	return c, nil
}

// circularShift returns the shift that moves the circular mean of the
// histogram nearest the center of its axis. Index i has angle
// 2*pi*(i+0.5)/n; the center of the axis is continuous index n/2-0.5.
func circularShift(count []int, total int) (int, bool) {
	n := len(count)
	var sumSin, sumCos float64
	for i, c := range count {
		if c == 0 {
			continue
		}
		theta := 2 * math.Pi * (float64(i) + 0.5) / float64(n)
		sumSin += float64(c) * math.Sin(theta)
		sumCos += float64(c) * math.Cos(theta)
	}
	if math.Hypot(sumSin, sumCos)/float64(total) < AmbiguityThreshold {
		return 0, true
	}
	pos := math.Atan2(sumSin, sumCos)/(2*math.Pi)*float64(n) - 0.5
	shift := int(math.Round(float64(n)/2 - 0.5 - pos))
	return mod(shift, n), false
}

// TopMask marks exactly k cells: those with the highest values, breaking ties
// toward lower indices.
func TopMask(values []float32, k int) []bool {
	n := len(values)
	k = min(max(k, 0), n)
	mask := make([]bool, n)
	if k == 0 {
		return mask
	}
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	t := sorted[n-k]
	need := k
	for _, v := range values {
		if v > t {
			need--
		}
	}
	for i, v := range values {
		switch {
		case v > t:
			mask[i] = true
		case v == t && need > 0:
			mask[i] = true
			need--
		}
	}
	return mask
}

// Slide returns a copy of f with source cell (col,row) moved to
// ((col+dx) mod Columns, (row+dy) mod Rows).
func (f *FractureField) Slide(dx, dy int) *FractureField {
	out := *f
	out.Values = make([]float32, len(f.Values))
	cols, rows := f.Columns, f.Rows
	dx, dy = mod(dx, cols), mod(dy, rows)
	for row := range rows {
		src := f.Values[row*cols : (row+1)*cols]
		dst := out.Values[mod(row+dy, rows)*cols:][:cols]
		copy(dst[dx:], src[:cols-dx])
		copy(dst[:dx], src[cols-dx:])
	}
	return &out
}

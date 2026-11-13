// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package terrain

import (
	"context"
	"math"
)

// coastDistance returns, for every cell center, the exact distance in km to
// the nearest land/water interface of land on the torus: positive on land,
// negative on water. A cell next to the other kind is half a cell from it.
// With no interface, land is +Inf and water -Inf.
//
// The interface nearest a cell center is the nearest point of the nearest
// cell of the other kind, a cw x ch square. The squared distance to the
// square of a cell (i, j) cells away is
//
//	F(i) + G(j),  F(i) = (cw * max(0, |i| - 1/2))^2,  G likewise with ch,
//
// which separates by axis, so the transform runs in two passes as in
// Felzenszwalb and Huttenlocher's. Down each column, the nearest cell of the
// other kind is the vertically nearest one, because G grows with |j|. Along
// each row, the lower envelope of the translates F(x - c) + G(gap(c)) gives
// the minimum. F is convex, so any two translates cross at most once, and
// the envelope is found with a stack and a binary search for each crossing
// in place of the parabola formula.
func coastDistance(ctx context.Context, land []byte, cols, rows int, cw, ch float64) ([]float32, error) {
	gap, err := columnGaps(ctx, land, cols, rows)
	if err != nil {
		return nil, err
	}
	out := make([]float32, cols*rows)
	env := newEnvelope(cols, cw)
	g := make([]float64, cols)
	vertical := func(n int32) float64 {
		if n == noGap {
			return math.Inf(1)
		}
		d := ch * max(0, float64(n)-0.5)
		return d * d
	}
	for r := range rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		row := land[r*cols : (r+1)*cols]
		// For each target kind, a cell of that kind is at gap 0; a cell of
		// the other kind is at its gap to the nearest cell unlike itself.
		for _, target := range []byte{0, 1} {
			for c, v := range row {
				if v == target {
					g[c] = 0
				} else {
					g[c] = vertical(gap[r*cols+c])
				}
			}
			env.build(g)
			sign := float32(1) // land, measured to water
			if target == 1 {
				sign = -1
			}
			env.eval(func(c int, d2 float64) {
				if row[c] != target {
					out[r*cols+c] = sign * float32(math.Sqrt(d2))
				}
			})
		}
	}
	return out, nil
}

// noGap marks a column with no cell of the other kind.
const noGap = math.MaxInt32

// columnGaps returns, for every cell, the number of rows to the nearest cell
// of the other kind in its column, wrapping, or noGap.
func columnGaps(ctx context.Context, land []byte, cols, rows int) ([]int32, error) {
	gap := make([]int32, cols*rows)
	for i := range gap {
		gap[i] = noGap
	}
	for c := range cols {
		if c%256 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		at := func(t int) byte { return land[(t%rows)*cols+c] }
		// last[k] is the latest position, unwrapped, of a cell of kind k.
		// Two laps in each direction let every cell see around the seam.
		last := [2]int{-1, -1}
		for t := range 2 * rows {
			k := at(t)
			if o := last[1-k]; t >= rows && o >= 0 {
				i := (t%rows)*cols + c
				gap[i] = min(gap[i], int32(t-o))
			}
			last[k] = t
		}
		last = [2]int{-1, -1}
		for t := 2*rows - 1; t >= 0; t-- {
			k := at(t)
			if o := last[1-k]; t < rows && o >= 0 {
				i := t*cols + c
				gap[i] = min(gap[i], int32(o-t))
			}
			last[k] = t
		}
	}
	return gap, nil
}

// envelope is the lower envelope of F(x - p) + g(p) over sources p on one
// periodic row of n cells, evaluated at x in [0, n). Each source appears at
// every position within half a row of [0, n), which includes the image
// nearest any x.
type envelope struct {
	n     int
	cw    float64
	h     int       // images reach h cells past either end
	pos   []int     // positions of the sources on the stack
	val   []float64 // their g
	start []int     // first x each one wins
}

func newEnvelope(n int, cw float64) *envelope {
	return &envelope{n: n, cw: cw, h: (n + 1) / 2}
}

// f is F(x - p) + g.
func (e *envelope) f(x, p int, g float64) float64 {
	d := e.cw * max(0, math.Abs(float64(x-p))-0.5)
	return d*d + g
}

// build replaces the envelope with that of g. Infinite values are not
// sources.
func (e *envelope) build(g []float64) {
	e.pos, e.val, e.start = e.pos[:0], e.val[:0], e.start[:0]
	for p := -e.h; p < e.n+e.h; p++ {
		v := g[((p%e.n)+e.n)%e.n]
		if math.IsInf(v, 1) {
			continue
		}
		x0 := 0
		for len(e.pos) > 0 {
			top := len(e.pos) - 1
			// The new source is further right, so f(new) - f(top) does not
			// increase with x: find the first x where the new one wins.
			tp, tv, lo := e.pos[top], e.val[top], e.start[top]
			wins := func(x int) bool { return e.f(x, p, v) <= e.f(x, tp, tv) }
			if wins(lo) {
				e.pos, e.val, e.start = e.pos[:top], e.val[:top], e.start[:top]
				x0 = 0
				continue
			}
			// wins(lo) is false; the answer is in (lo, n], n meaning never.
			a, b := lo, e.n
			for b-a > 1 {
				m := a + (b-a)/2
				if wins(m) {
					b = m
				} else {
					a = m
				}
			}
			x0 = b
			break
		}
		if x0 >= e.n {
			continue // never below the envelope within the row
		}
		e.pos, e.val, e.start = append(e.pos, p), append(e.val, v), append(e.start, x0)
	}
}

// eval calls fn with the envelope's value at every x in [0, n), in order. An
// empty envelope gives +Inf.
func (e *envelope) eval(fn func(x int, d2 float64)) {
	k := 0
	for x := range e.n {
		if len(e.pos) == 0 {
			fn(x, math.Inf(1))
			continue
		}
		for k+1 < len(e.pos) && e.start[k+1] <= x {
			k++
		}
		fn(x, e.f(x, e.pos[k], e.val[k]))
	}
}

// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package fracture

import (
	"container/heap"
	"context"
	"fmt"
)

// Ocean is a land/ocean selection with one connected ocean, section 7 of
// the cylindrical specification on a torus.
type Ocean struct {
	// Land marks exactly k cells; the others form one eight-connected
	// ocean, both axes wrapping.
	Land []bool
	// Quantile is TopMask(values, k), the selection before the ocean was
	// made connected.
	Quantile []bool
	// Components counts the eight-connected components of the quantile's
	// ocean; Largest is the size of the one grown into the ocean.
	Components, Largest int
	// Repair counts the cells where Land and Quantile differ.
	Repair int
}

// cell is a frontier entry, ordered by value then index.
type cell struct {
	v float32
	i int32
}

type frontier []cell

func (f frontier) Len() int { return len(f) }
func (f frontier) Less(a, b int) bool {
	return f[a].v < f[b].v || f[a].v == f[b].v && f[a].i < f[b].i
}
func (f frontier) Swap(a, b int) { f[a], f[b] = f[b], f[a] }
func (f *frontier) Push(x any)   { *f = append(*f, x.(cell)) }
func (f *frontier) Pop() any {
	old := *f
	c := old[len(old)-1]
	*f = old[:len(old)-1]
	return c
}

// neighbors calls fn with the eight neighbors of i on a cols by rows torus.
func neighbors(i, cols, rows int, fn func(int)) {
	x, y := i%cols, i/cols
	for dy := -1; dy <= 1; dy++ {
		ny := (y + dy + rows) % rows
		for dx := -1; dx <= 1; dx++ {
			if dx != 0 || dy != 0 {
				fn(ny*cols + (x+dx+cols)%cols)
			}
		}
	}
}

// ConnectedOcean selects k land cells from values on a cols by rows torus
// so that the rest is one connected ocean: the lowest n-k cells by
// TopMask's rule, ties by index; their largest eight-connected component
// (ties to the one with the lowest index), grown to exactly n-k cells
// through a frontier keyed by value then index. Low ground cut off from
// that component stays land, and the ocean takes the lowest cells on its
// edge instead.
func ConnectedOcean(ctx context.Context, values []float32, cols, rows, k int) (*Ocean, error) {
	n := cols * rows
	if cols < 1 || rows < 1 || len(values) != n || k < 0 || k > n {
		return nil, fmt.Errorf("connected ocean: invalid size")
	}
	o := &Ocean{Quantile: TopMask(values, k)}
	above := o.Quantile
	label := make([]int32, n)
	for i := range label {
		label[i] = -1
	}
	var queue []int32
	best := int32(-1)
	for i := range n {
		if above[i] || label[i] >= 0 {
			continue
		}
		id := int32(o.Components)
		o.Components++
		label[i] = id
		queue = append(queue[:0], int32(i))
		for h := 0; h < len(queue); h++ {
			neighbors(int(queue[h]), cols, rows, func(j int) {
				if !above[j] && label[j] < 0 {
					label[j] = id
					queue = append(queue, int32(j))
				}
			})
		}
		if len(queue) > o.Largest {
			best, o.Largest = id, len(queue)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	// state is 0 for cells not yet reached, 1 for ocean, and 2 for cells on
	// the frontier.
	state := make([]byte, n)
	ocean := 0
	for i, l := range label {
		if l == best && best >= 0 {
			state[i] = 1
			ocean++
		}
	}
	label = nil
	var f frontier
	push := func(i int) {
		neighbors(i, cols, rows, func(j int) {
			if state[j] == 0 {
				state[j] = 2
				heap.Push(&f, cell{values[j], int32(j)})
			}
		})
	}
	for i := range n {
		if state[i] == 1 {
			push(i)
		}
	}
	for ocean < n-k {
		c := heap.Pop(&f).(cell)
		state[c.i] = 1
		ocean++
		push(int(c.i))
	}
	o.Land = make([]bool, n)
	for i := range n {
		o.Land[i] = state[i] != 1
		if o.Land[i] != above[i] {
			o.Repair++
		}
	}
	return o, ctx.Err()
}

// OceanConnected reports whether the cells land does not mark form at most
// one eight-connected component on a cols by rows torus.
func OceanConnected(land func(i int) bool, cols, rows int) bool {
	n := cols * rows
	seen := make([]bool, n)
	var queue []int32
	for i := range n {
		if land(i) || seen[i] {
			continue
		}
		if len(queue) > 0 {
			return false
		}
		seen[i] = true
		queue = append(queue, int32(i))
		for h := 0; h < len(queue); h++ {
			neighbors(int(queue[h]), cols, rows, func(j int) {
				if !land(j) && !seen[j] {
					seen[j] = true
					queue = append(queue, int32(j))
				}
			})
		}
	}
	return true
}

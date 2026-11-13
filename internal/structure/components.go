// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package structure

// components labels the connected regions of a provisional mask on the
// torus. Land regions are eight-connected and ocean regions four-connected,
// so a diagonal chain of land cells is one landmass and does not also leave
// the ocean on either side of it joined.
type components struct {
	cols, rows int
	// label holds a region number for every cell. Land regions are numbered
	// 1..land and ocean regions land+1..land+ocean, each group in order of
	// the region's lowest row-major cell index.
	label       []uint32
	land, ocean int
	cells       []int   // cells[n] is the size of region n; cells[0] is unused
	first       []int32 // first[n] is the lowest cell index of region n
}

func labelComponents(mask []byte, cols, rows int) *components {
	c := &components{cols: cols, rows: rows, label: make([]uint32, len(mask)), cells: []int{0}, first: []int32{0}}
	var stack []int32
	fill := func(start int, kind byte, diagonal bool) {
		n := uint32(len(c.cells))
		c.cells, c.first = append(c.cells, 0), append(c.first, int32(start))
		c.label[start] = n
		stack = append(stack[:0], int32(start))
		for len(stack) > 0 {
			i := int(stack[len(stack)-1])
			stack = stack[:len(stack)-1]
			c.cells[n]++
			x, y := i%cols, i/cols
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					if dx == 0 && dy == 0 || !diagonal && dx != 0 && dy != 0 {
						continue
					}
					j := (y+dy+rows)%rows*cols + (x+dx+cols)%cols
					if mask[j] == kind && c.label[j] == 0 {
						c.label[j] = n
						stack = append(stack, int32(j))
					}
				}
			}
		}
	}
	for i, v := range mask {
		if v == 1 && c.label[i] == 0 {
			fill(i, 1, true)
		}
	}
	c.land = len(c.cells) - 1
	for i, v := range mask {
		if v == 0 && c.label[i] == 0 {
			fill(i, 0, false)
		}
	}
	c.ocean = len(c.cells) - 1 - c.land
	return c
}

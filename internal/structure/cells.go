// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package structure

import "context"

// LineCells calls fn for every cell of a cols x rows raster over dom whose
// center lies within reach half widths (of the backbone's widest sample) of
// a validated backbone. fn receives the cell's cross-belt position u, equal
// to Line.Cross at the cell center, and the arc-length position s of the
// backbone point nearest it. Each cell is reported once.
func LineCells(ctx context.Context, dom Domain, cols, rows int, backbone []Sample, reach float64, fn func(cell int, u, s float64)) error {
	return lineCells(ctx, dom, cols, rows, backbone, reach, fn)
}

// Visit calls fn for every cell of a cols x rows raster over dom whose center
// lies in the unwrapped box, taking each wrapped cell once. Cell (col, row)
// has its center at ((col+0.5)*cw, (row+0.5)*ch).
func Visit(dom Domain, cols, rows int, box Rect, fn func(col, row int, center Point)) {
	visit(dom, cols, rows, box, fn)
}

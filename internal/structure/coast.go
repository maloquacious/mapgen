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

	"github.com/maloquacious/mapgen/internal/continent"
)

const (
	CoastAlgorithm   = "coast-v1"
	CoastPreviewFile = "preview-coast.png"
	// CoastDistance records how coast distance is measured.
	CoastDistance = "signed km from a working-grid cell center to the nearest land/water interface of the working-grid mask; exact on the torus; positive on land"
)

// CoastParams are the calibration values behind the coast interpretation.
// They are recorded in the manifest.
type CoastParams struct {
	Algorithm string `json:"algorithm"`
	// WorkingSpacingKM is the target cell size of the working grid. The
	// actual size is the nearest that tiles the world.
	WorkingSpacingKM float64 `json:"working_spacing_km"`
	// LandCoverage is the fraction of a working cell that must be land, on
	// the simplified provisional mask, for the working cell to be land.
	LandCoverage float64 `json:"land_coverage"`
	// SmoothKM is the standard deviation of the smoothing applied along
	// each coast chain.
	SmoothKM float64 `json:"smooth_km"`
	Distance string  `json:"distance"`
}

// DefaultCoastParams returns the coast-v1 defaults.
func DefaultCoastParams() CoastParams {
	return CoastParams{Algorithm: CoastAlgorithm, WorkingSpacingKM: 50, LandCoverage: 0.5, SmoothKM: 75, Distance: CoastDistance}
}

// Chain is one closed coastline of the working-grid mask. Every coastline on
// a torus is closed. Walking the points in order keeps land on the right.
type Chain struct {
	ID int `json:"id"`
	// LandAreaKM2 and WaterAreaKM2 are the areas of the working-grid land
	// region and water region the chain separates. A small water area marks
	// the shore of an enclosed sea; a small land area marks an island.
	LandAreaKM2  float64 `json:"land_area_km2"`
	WaterAreaKM2 float64 `json:"water_area_km2"`
	LengthKM     float64 `json:"length_km"`
	// Wrap counts how many times the chain circles the world east and
	// south before it closes: the point after the last is the first moved
	// by Wrap[0] world widths and Wrap[1] world heights.
	Wrap [2]int `json:"wrap"`
	// Points are smoothed and unwrapped on both axes.
	Points []Point `json:"-"`
}

// Coast is the provisional coast interpreted on the working grid. It is
// placement data derived from the provisional mask, not a saved raster: Load
// does not rebuild it.
type Coast struct {
	Columns, Rows             int
	CellWidthKM, CellHeightKM float64
	Land                      []byte    // row-major; 1 where the working cell is land
	Distance                  []float64 // row-major; see CoastDistance. ±Inf when there is no coast
	Chains                    []Chain
}

// CoastSummary is the coast section of the diagnostics.
type CoastSummary struct {
	Columns       int     `json:"columns"`
	Rows          int     `json:"rows"`
	CellWidthKM   float64 `json:"cell_width_km"`
	CellHeightKM  float64 `json:"cell_height_km"`
	LandCells     int     `json:"land_cells"`
	MaxInlandKM   float64 `json:"max_inland_km"`
	MaxOffshoreKM float64 `json:"max_offshore_km"`
	TotalLengthKM float64 `json:"total_length_km"`
	Chains        []Chain `json:"chains"`
}

// Summary describes the coast for the diagnostics. The maxima are zero when
// there is no coast.
func (c *Coast) Summary() *CoastSummary {
	s := &CoastSummary{Columns: c.Columns, Rows: c.Rows, CellWidthKM: c.CellWidthKM, CellHeightKM: c.CellHeightKM, Chains: c.Chains}
	if s.Chains == nil {
		s.Chains = []Chain{}
	}
	for i, v := range c.Land {
		s.LandCells += int(v)
		if len(c.Chains) > 0 {
			s.MaxInlandKM, s.MaxOffshoreKM = max(s.MaxInlandKM, c.Distance[i]), max(s.MaxOffshoreKM, -c.Distance[i])
		}
	}
	for _, ch := range c.Chains {
		s.TotalLengthKM += ch.LengthKM
	}
	return s
}

// DistanceAt returns the coast distance at p, interpolated between working
// cell centers on the torus.
func (c *Coast) DistanceAt(p Point) float64 {
	fx, fy := p.X/c.CellWidthKM-0.5, p.Y/c.CellHeightKM-0.5
	x0, y0 := math.Floor(fx), math.Floor(fy)
	tx, ty := fx-x0, fy-y0
	at := func(dx, dy int) float64 {
		return c.Distance[((int(y0)+dy)%c.Rows+c.Rows)%c.Rows*c.Columns+((int(x0)+dx)%c.Columns+c.Columns)%c.Columns]
	}
	if len(c.Chains) == 0 {
		return at(0, 0) // infinite everywhere; do not interpolate
	}
	return (at(0, 0)*(1-tx)+at(1, 0)*tx)*(1-ty) + (at(0, 1)*(1-tx)+at(1, 1)*tx)*ty
}

// workingGrid returns the size of the grid nearest spacing that tiles the
// world and is no finer than the full-resolution grid.
func workingGrid(g continent.Grid, spacing float64) (cols, rows int) {
	return min(g.Columns, max(1, int(math.Round(g.WidthKM/spacing)))), min(g.Rows, max(1, int(math.Round(g.HeightKM/spacing))))
}

// interpretCoast builds the working-grid mask, coast distance, and coast
// chains from the simplified provisional mask. A working cell summarizes the
// land coverage of the cells inside it, so a single sample cannot erase or
// invent land.
func interpretCoast(ctx context.Context, p CoastParams, g continent.Grid, mask []byte) (*Coast, error) {
	cc, rc := workingGrid(g, p.WorkingSpacingKM)
	c := &Coast{Columns: cc, Rows: rc, CellWidthKM: g.WidthKM / float64(cc), CellHeightKM: g.HeightKM / float64(rc)}
	landCount, total := make([]int32, cc*rc), make([]int32, cc*rc)
	for i, v := range mask {
		// Integer arithmetic maps each cell to exactly one working cell.
		ci := (i/g.Columns*rc/g.Rows)*cc + i%g.Columns*cc/g.Columns
		landCount[ci] += int32(v)
		total[ci]++
	}
	c.Land = make([]byte, cc*rc)
	for i := range c.Land {
		if float64(landCount[i]) >= p.LandCoverage*float64(total[i]) {
			c.Land[i] = 1
		}
	}
	var err error
	if c.Distance, err = coastDistance(ctx, c.Land, cc, rc, c.CellWidthKM, c.CellHeightKM); err != nil {
		return nil, err
	}
	passes := int(math.Round(2 * p.SmoothKM * p.SmoothKM / (c.CellWidthKM * c.CellHeightKM)))
	c.Chains = traceChains(c.Land, cc, rc, c.CellWidthKM, c.CellHeightKM, passes)
	return c, ctx.Err()
}

// coastDistance returns, for every cell center, the distance in km to the
// nearest land/water interface on the torus: positive on land, negative on
// water, and infinite when the mask is all one kind.
//
// The interface nearest a cell is the nearest point of the nearest cell of
// the other kind. Within one column that is the vertically nearest such
// cell, so each cell needs only one candidate per column.
func coastDistance(ctx context.Context, land []byte, cols, rows int, cw, ch float64) ([]float64, error) {
	// gap is the number of rows to the nearest cell of the other kind in the
	// same column, or none.
	const none = math.MaxInt32
	gap := make([]int32, len(land))
	for i := range gap {
		gap[i] = none
	}
	for x := range cols {
		// Sweep twice round the column in each direction, remembering where
		// each kind was last seen.
		for _, dir := range []int{1, -1} {
			last := [2]int{-1, -1}
			for k := range 2 * rows {
				y := k % rows
				if dir < 0 {
					y = rows - 1 - y
				}
				i := y*cols + x
				last[land[i]] = k
				if o := last[1-land[i]]; o >= 0 {
					gap[i] = min(gap[i], int32(k-o))
				}
			}
		}
	}
	// Squared distances in km to the near edge of a cell k columns or n rows
	// away; zero for the cell's own column or row.
	dx2, dy2 := make([]float64, cols/2+1), make([]float64, rows+1)
	for k := 1; k < len(dx2); k++ {
		dx2[k] = (float64(k) - 0.5) * cw * (float64(k) - 0.5) * cw
	}
	for n := 1; n < len(dy2); n++ {
		dy2[n] = (float64(n) - 0.5) * ch * (float64(n) - 0.5) * ch
	}
	out := make([]float64, len(land))
	for y := range rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for x := range cols {
			v := land[y*cols+x]
			best := math.Inf(1)
			// Work outward; stop once the columns alone are too far away.
			for k := 0; k <= cols/2 && dx2[k] < best; k++ {
				for _, xp := range [2]int{(x + k) % cols, (x - k + cols) % cols} {
					j := y*cols + xp
					n := int32(0)
					if land[j] == v {
						n = gap[j]
					}
					if n != none {
						best = min(best, dx2[k]+dy2[n])
					}
				}
			}
			out[y*cols+x] = math.Sqrt(best)
			if v == 0 {
				out[y*cols+x] = -out[y*cols+x]
			}
		}
	}
	return out, nil
}

// Directions of travel along cell edges, clockwise on a map with north up.
var chainStep = [4][2]int{{1, 0}, {0, 1}, {-1, 0}, {0, -1}} // east, south, west, north

// traceChains follows every land/water interface of the mask on the torus
// and returns each as a closed chain with land on its right, in order of
// each chain's lowest starting corner (row-major, then direction).
//
// Where two land cells touch only at a corner the chain turns left, which
// keeps them joined: land is eight-connected and water four-connected, as in
// labelComponents. A chain's points are the midpoints of its edges, smoothed
// by the given number of passes of a 1-2-1 filter along the chain.
func traceChains(land []byte, cols, rows int, cw, ch float64, passes int) []Chain {
	cell := func(x, y int) int { return (y%rows+rows)%rows*cols + (x%cols+cols)%cols }
	// sides returns the cells right (land) and left (water) of the edge that
	// leaves corner (i, j) in direction d. Corner (i, j) is the north-west
	// corner of cell (i, j).
	sides := func(i, j, d int) (right, left int) {
		switch d {
		case 0:
			return cell(i, j), cell(i, j-1)
		case 1:
			return cell(i-1, j), cell(i, j)
		case 2:
			return cell(i-1, j-1), cell(i-1, j)
		default:
			return cell(i, j-1), cell(i-1, j-1)
		}
	}
	has := func(i, j, d int) bool {
		r, l := sides(i, j, d)
		return land[r] == 1 && land[l] == 0
	}
	regions := labelComponents(land, cols, rows)
	cellKM2 := cw * ch
	visited := make([]bool, 4*cols*rows)
	var chains []Chain
	for start := range visited {
		i, j, d := start/4%cols, start/4/cols, start%4
		if visited[start] || !has(i, j, d) {
			continue
		}
		r, l := sides(i, j, d)
		chain := Chain{ID: len(chains) + 1,
			LandAreaKM2:  float64(regions.cells[regions.label[r]]) * cellKM2,
			WaterAreaKM2: float64(regions.cells[regions.label[l]]) * cellKM2}
		ux, uy, cd := i, j, d // unwrapped corner and direction
		for {
			visited[cell(ux, uy)*4+cd] = true
			chain.Points = append(chain.Points, Point{
				(float64(ux) + 0.5*float64(chainStep[cd][0])) * cw,
				(float64(uy) + 0.5*float64(chainStep[cd][1])) * ch})
			ux, uy = ux+chainStep[cd][0], uy+chainStep[cd][1]
			// Left, straight, right: exactly one exists except where land
			// meets land at a corner, and there left is the rule.
			for _, nd := range [3]int{(cd + 3) % 4, cd, (cd + 1) % 4} {
				if has(ux, uy, nd) {
					cd = nd
					break
				}
			}
			if cell(ux, uy)*4+cd == start {
				break
			}
		}
		chain.Wrap = [2]int{(ux - i) / cols, (uy - j) / rows}
		period := Point{float64(chain.Wrap[0]*cols) * cw, float64(chain.Wrap[1]*rows) * ch}
		chain.Points = smoothClosed(chain.Points, period, passes)
		n := len(chain.Points)
		for k, p := range chain.Points {
			q := chain.Points[(k+1)%n]
			if k == n-1 {
				q = Point{q.X + period.X, q.Y + period.Y}
			}
			chain.LengthKM += math.Hypot(q.X-p.X, q.Y-p.Y)
		}
		chains = append(chains, chain)
	}
	return chains
}

// smoothClosed applies passes of a 1-2-1 filter to a closed polyline whose
// point after the last is the first moved by period.
func smoothClosed(pts []Point, period Point, passes int) []Point {
	n := len(pts)
	next := make([]Point, n)
	for range passes {
		for k, p := range pts {
			a, b := pts[(k+n-1)%n], pts[(k+1)%n]
			if k == 0 {
				a = Point{a.X - period.X, a.Y - period.Y}
			}
			if k == n-1 {
				b = Point{b.X + period.X, b.Y + period.Y}
			}
			next[k] = Point{(a.X + 2*p.X + b.X) / 4, (a.Y + 2*p.Y + b.Y) / 4}
		}
		pts, next = next, pts
	}
	return pts
}

// Coast preview colors: bands every coastBandKM, darker farther from the
// coast, and the chains as dark lines.
const coastBandKM = 100

var (
	coastShore = [2]color.RGBA{{206, 222, 236, 255}, {242, 234, 208, 255}} // water, land at the coast
	coastFar   = [2]color.RGBA{{52, 84, 136, 255}, {122, 92, 56, 255}}     // water, land 1500 km away
)

// writeCoastPreview draws the working grid's coast distance in bands and the
// smoothed coast chains over it.
func writeCoastPreview(path string, dom Domain, c *Coast) error {
	scale := max(2, (1199+c.Columns)/c.Columns) // pixels per working cell
	w, h := c.Columns*scale, c.Rows*scale
	kmx, kmy := dom.WidthKM/float64(w), dom.HeightKM/float64(h)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			d := c.DistanceAt(Point{(float64(x) + 0.5) * kmx, (float64(y) + 0.5) * kmy})
			kind := 0
			if d > 0 {
				kind = 1
			}
			t := 0.0
			if !math.IsInf(d, 0) {
				t = min(math.Floor(math.Abs(d)/coastBandKM)*coastBandKM/1500, 1)
			}
			a, b := coastShore[kind], coastFar[kind]
			mix := func(a, b uint8) uint8 { return uint8(math.Round(float64(a) + t*(float64(b)-float64(a)))) }
			img.SetRGBA(x, y, color.RGBA{mix(a.R, b.R), mix(a.G, b.G), mix(a.B, b.B), 255})
		}
	}
	for _, chain := range c.Chains {
		n := len(chain.Points)
		for k, p := range chain.Points {
			q := chain.Points[(k+1)%n]
			if k == n-1 {
				q = Point{q.X + float64(chain.Wrap[0])*dom.WidthKM, q.Y + float64(chain.Wrap[1])*dom.HeightKM}
			}
			steps := max(1, int(math.Ceil(2*max(math.Abs(q.X-p.X)/kmx, math.Abs(q.Y-p.Y)/kmy))))
			for s := range steps {
				t := float64(s) / float64(steps)
				px, py := int(math.Floor((p.X+t*(q.X-p.X))/kmx)), int(math.Floor((p.Y+t*(q.Y-p.Y))/kmy))
				img.SetRGBA((px%w+w)%w, (py%h+h)%h, previewLine)
			}
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

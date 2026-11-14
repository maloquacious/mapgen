// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package terrain

import (
	"context"
	"fmt"
	"math"
	"sync"
	"sync/atomic"

	"github.com/maloquacious/mapgen/internal/continent"
	"github.com/maloquacious/mapgen/internal/prng"
	"github.com/maloquacious/mapgen/internal/structure"
	"github.com/maloquacious/mapgen/internal/trench"
)

// Subcell rasterization: a cell that any feature, ridge, peak, or trench
// reaches is sampled on a fixed grid of subcell points; other cells take
// the foundation at their center. See "Rasterization" in the design.
const SubcellAlgorithm = "subcell-v1"

// tileCells is the side of the square tiles of cells the work is divided
// into. It never affects the output.
const tileCells = 64

// SubcellParams are the calibration values behind subcell rasterization.
// They are recorded in the manifest.
type SubcellParams struct {
	Algorithm string `json:"algorithm"`
	// Samples is the number of subcell points along each axis of a cell.
	Samples int    `json:"samples"`
	Model   string `json:"model"`
}

// DefaultSubcellParams returns the subcell-v1 defaults.
func DefaultSubcellParams() SubcellParams {
	return SubcellParams{
		Algorithm: SubcellAlgorithm,
		Samples:   4,
		Model: "a cell any feature, ridge, peak, or trench reaches is sampled at Samples x Samples points: elevation is their mean, " +
			"high their largest plus the part of the crest of a ridge passing within half a sample diagonal, or of a peak centered in the cell, " +
			"that the samples missed, and relief high minus their smallest; other cells take the foundation at their center",
	}
}

// synthInput is what feature synthesis needs.
type synthInput struct {
	seed     uint64
	regional RegionalParams
	belts    *BeltParams // nil: the regional surface alone
	subcell  SubcellParams
	grid     continent.Grid
	features *structure.FeatureFile
	trenches *trench.TrenchFile // nil: none
	land     []byte             // provisional land by cell; nil for none
	// base is the foundation at (x, y) km, a point in cell; center is the
	// foundation at each cell center.
	base    func(x, y float64, cell int) float64
	center  []float32
	workers int
}

// synthesis is what feature synthesis produces.
type synthesis struct {
	elevation, high, relief []float32
	features                []FeatureDiagnostics
	ridges                  *RidgeFile
	trenches                []TrenchDiagnostics
}

// Item kinds, in the order the index lists them.
const (
	itemFeature = iota
	itemTrough
	itemRidge
	itemPeak
	itemTrench
)

// geom is a linear item's geometry.
type geom struct {
	bb      []structure.Sample
	line    *structure.Line
	cum     []float64
	box     Rect // backbone vertices, unwrapped, without width
	maxHalf float64
}

// Rect is an unwrapped box in km.
type Rect = structure.Rect

func newGeom(dom structure.Domain, bb []structure.Sample) *geom {
	gm := &geom{bb: bb, line: structure.NewLine(dom, bb), cum: make([]float64, len(bb)),
		box: Rect{MinX: math.Inf(1), MinY: math.Inf(1), MaxX: math.Inf(-1), MaxY: math.Inf(-1)}}
	for i, s := range bb {
		if i > 0 {
			gm.cum[i] = gm.cum[i-1] + math.Hypot(s.X-bb[i-1].X, s.Y-bb[i-1].Y)
		}
		gm.box = Rect{MinX: min(gm.box.MinX, s.X), MinY: min(gm.box.MinY, s.Y), MaxX: max(gm.box.MaxX, s.X), MaxY: max(gm.box.MaxY, s.Y)}
		gm.maxHalf = max(gm.maxHalf, s.WidthKM/2)
	}
	return gm
}

func (gm *geom) bounds() Rect {
	h := gm.maxHalf
	return Rect{MinX: gm.box.MinX - h, MinY: gm.box.MinY - h, MaxX: gm.box.MaxX + h, MaxY: gm.box.MaxY + h}
}

// trenchPlan is one trench's drawn floor.
type trenchPlan struct {
	rec  *trench.Trench
	gm   *geom
	pl   *plan
	fade float64
}

// engine holds everything the tiles share. Nothing in it changes while
// tiles run.
type engine struct {
	in        synthInput
	dom       structure.Domain
	cols      int
	rows      int
	s         int // samples per axis
	fcols     int
	frows     int
	fx, fy    float64 // sample spacing, km
	plans     []*plan
	geoms     []*geom // by feature, nil for area features
	comp      *components
	ridges    []ridgeShape
	ridgeGeom []*geom
	trough    []*geom
	corridors map[uint32][]int // corridor indices by feature
	trenches  []trenchPlan
	// index lists, for each tile, the items that reach it as (kind, index)
	// pairs, in kind order and then index order.
	tx, ty int
	index  [][][2]int32
}

// regional runs feature synthesis. Every per-cell result depends only on
// that cell, so the output does not depend on the number of workers or on
// the order tiles finish in.
func regional(ctx context.Context, in synthInput) (*synthesis, error) {
	g, ff := in.grid, in.features
	e := &engine{in: in, dom: ff.Domain, cols: g.Columns, rows: g.Rows, s: in.subcell.Samples}
	if e.s < 1 {
		return nil, fmt.Errorf("subcell samples must be at least 1")
	}
	e.fcols, e.frows = e.cols*e.s, e.rows*e.s
	e.fx, e.fy = g.CellWidthKM/float64(e.s), g.CellHeightKM/float64(e.s)
	e.plans = make([]*plan, len(ff.Features))
	e.geoms = make([]*geom, len(ff.Features))
	for i := range ff.Features {
		f := &ff.Features[i]
		var err error
		if e.plans[i], err = newPlan(in.seed, in.regional, e.dom, f); err != nil {
			return nil, fmt.Errorf("feature %d: %w", f.ID, err)
		}
		if f.Linear() {
			e.geoms[i] = newGeom(e.dom, f.Backbone)
		}
	}
	if in.belts != nil {
		var err error
		if e.comp, err = buildComponents(*in.belts, e.dom, e.plans); err != nil {
			return nil, err
		}
		e.shapes()
		if err := e.planTrenches(); err != nil {
			return nil, err
		}
	}
	e.buildIndex()

	n := e.cols * e.rows
	res := &synthesis{elevation: make([]float32, n), high: make([]float32, n), relief: make([]float32, n)}
	workers := max(1, in.workers)
	var next atomic.Int64
	var wg sync.WaitGroup
	errs := make([]error, workers)
	for w := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tb := &tileBuf{}
			for {
				t := int(next.Add(1) - 1)
				if t >= e.tx*e.ty {
					return
				}
				if err := ctx.Err(); err != nil {
					errs[w] = err
					return
				}
				e.tile(t, tb, res)
			}
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	if e.comp != nil {
		e.comp.saddles(e.dom, g, res.elevation)
		res.ridges = e.comp.file
	}
	var err error
	if res.features, res.trenches, err = e.diagnose(ctx, res.elevation); err != nil {
		return nil, err
	}
	return res, ctx.Err()
}

// shapes prepares the components for rasterizing.
func (e *engine) shapes() {
	c := e.comp
	for i := range c.file.Ridges {
		rec := &c.file.Ridges[i]
		gm := newGeom(e.dom, rec.Backbone)
		e.ridgeGeom = append(e.ridgeGeom, gm)
		e.ridges = append(e.ridges, ridgeShape{rec: rec, amp: c.amps[i], cum: gm.cum, fade: c.fades[i]})
	}
	for _, t := range c.file.Troughs {
		e.trough = append(e.trough, newGeom(e.dom, t.Backbone))
	}
	e.corridors = map[uint32][]int{}
	for i, cr := range c.file.Corridors {
		e.corridors[cr.Feature] = append(e.corridors[cr.Feature], i)
	}
}

// planTrenches draws each trench's floor from its stream: the control
// spacing, then a floor value at each control sample.
func (e *engine) planTrenches() error {
	tf := e.in.trenches
	if tf == nil {
		return nil
	}
	for i := range tf.Trenches {
		tr := &tf.Trenches[i]
		if tr.Targets.RegionalElevationM == nil || tr.Key == nil {
			return fmt.Errorf("trench %d has no floor target or key", tr.ID)
		}
		rng := prng.New(e.in.seed, DomainTrench, *tr.Key)
		gm := newGeom(e.dom, tr.Backbone)
		l := gm.cum[len(gm.cum)-1]
		n := max(1, int(math.Round(l/lerp(e.in.regional.ControlSpacingKM, rng.Float64()))))
		pl := &plan{step: l / float64(n)}
		for range n + 1 {
			pl.regional = append(pl.regional, lerp(*tr.Targets.RegionalElevationM, rng.Float64()))
		}
		e.trenches = append(e.trenches, trenchPlan{rec: tr, gm: gm, pl: pl, fade: min(e.in.belts.TrenchFadeKM, l/3)})
	}
	return nil
}

// buildIndex bins every item by its bounds into the tiles its cells reach.
func (e *engine) buildIndex() {
	e.tx, e.ty = (e.cols+tileCells-1)/tileCells, (e.rows+tileCells-1)/tileCells
	e.index = make([][][2]int32, e.tx*e.ty)
	g := e.in.grid
	add := func(kind, i int, b Rect) {
		span := func(lo, hi, cell float64, n, tiles int) []int {
			a, z := int(math.Floor(lo/cell)), int(math.Floor(hi/cell))
			if z-a+1 >= n {
				a, z = 0, n-1
			}
			var out []int
			seen := make(map[int]bool)
			for c := a; c <= z; c++ {
				t := ((c%n + n) % n) / tileCells
				if !seen[t] {
					seen[t] = true
					out = append(out, t)
				}
			}
			return out
		}
		for _, ty := range span(b.MinY, b.MaxY, g.CellHeightKM, e.rows, e.ty) {
			for _, tx := range span(b.MinX, b.MaxX, g.CellWidthKM, e.cols, e.tx) {
				t := ty*e.tx + tx
				e.index[t] = append(e.index[t], [2]int32{int32(kind), int32(i)})
			}
		}
	}
	for i, pl := range e.plans {
		if gm := e.geoms[i]; gm != nil {
			add(itemFeature, i, gm.bounds())
		} else {
			add(itemFeature, i, pl.region.Bounds())
		}
	}
	if e.comp != nil {
		for i, gm := range e.trough {
			add(itemTrough, i, gm.bounds())
		}
		for i, gm := range e.ridgeGeom {
			add(itemRidge, i, gm.bounds())
		}
		for i, pk := range e.comp.file.Peaks {
			r := pk.RadiusKM
			add(itemPeak, i, Rect{MinX: pk.X - r, MinY: pk.Y - r, MaxX: pk.X + r, MaxY: pk.Y + r})
		}
		for i, tp := range e.trenches {
			add(itemTrench, i, tp.gm.bounds())
		}
	}
}

// tileBuf holds one worker's per-tile arrays, by sample point of the tile.
type tileBuf struct {
	base, up1, up2, dep, ridge, basin []float64
	touched                           []bool
	crest, comp                       []float64 // by cell of the tile
	best, tt                          []float64 // line scratch
	seg                               []int32
}

func grow[T any](v []T, n int) []T {
	if cap(v) < n {
		return make([]T, n)
	}
	v = v[:n]
	clear(v)
	return v
}

// window is a tile's block of sample points: columns wc0 to wc0+wn-1 and
// rows wr0 to wr0+wm-1 of the sample raster.
type window struct{ wc0, wr0, wn, wm int }

// tile computes one tile's cells.
func (e *engine) tile(t int, tb *tileBuf, res *synthesis) {
	in := &e.in
	tx, ty := t%e.tx, t/e.tx
	c0, r0 := tx*tileCells, ty*tileCells
	nc, nr := min(tileCells, e.cols-c0), min(tileCells, e.rows-r0)
	w := window{c0 * e.s, r0 * e.s, nc * e.s, nr * e.s}
	np := w.wn * w.wm
	items := e.index[t]
	if len(items) == 0 {
		for r := r0; r < r0+nr; r++ {
			for c := c0; c < c0+nc; c++ {
				i := r*e.cols + c
				res.elevation[i], res.high[i], res.relief[i] = in.center[i], in.center[i], 0
			}
		}
		return
	}
	tb.base, tb.up1, tb.up2, tb.dep = grow(tb.base, np), grow(tb.up1, np), grow(tb.up2, np), grow(tb.dep, np)
	tb.ridge, tb.basin, tb.touched = grow(tb.ridge, np), grow(tb.basin, np), grow(tb.touched, np)
	tb.crest, tb.comp = grow(tb.crest, nc*nr), grow(tb.comp, nc*nr)
	point := func(local int) structure.Point {
		return structure.Point{X: (float64(w.wc0+local%w.wn) + 0.5) * e.fx, Y: (float64(w.wr0+local/w.wn) + 0.5) * e.fy}
	}
	cellOf := func(local int) int { return ((w.wr0+local/w.wn)/e.s)*e.cols + (w.wc0+local%w.wn)/e.s }
	for i := range np {
		p := point(i)
		tb.base[i] = in.base(p.X, p.Y, cellOf(i))
	}
	line := func(gm *geom, fn func(i int, u, s float64)) {
		e.lineWindow(w, gm, tb, func(i int, u, s float64) {
			if u < 1 {
				tb.touched[i] = true
				fn(i, u, s)
			}
		})
	}
	area := func(pl *plan, fn func(i int, wt float64)) {
		e.areaWindow(w, pl.region.Bounds(), func(i int, p structure.Point) {
			if m := pl.region.Mask(p); m > 0 {
				tb.touched[i] = true
				fn(i, smoothstep(0, 1, m))
			}
		})
	}
	downside := func(pl *plan, i int) bool {
		_, _, side := pl.line.Nearest(point(i))
		return side == structure.Left && pl.downLeft || side == structure.Right && !pl.downLeft
	}
	uplift := func(i int, v float64) {
		if v > tb.up1[i] {
			tb.up1[i], tb.up2[i] = v, tb.up1[i]
		} else if v > tb.up2[i] {
			tb.up2[i] = v
		}
	}
	deepen := func(i int, v float64) { tb.dep[i] = max(tb.dep[i], v) }

	// Uplifts: the two largest at each point.
	for _, it := range items {
		if it[0] != itemFeature {
			continue
		}
		k := int(it[1])
		pl, gm := e.plans[k], e.geoms[k]
		f := tb.base
		switch ft := pl.f.Type; {
		case ft == structure.TypeMountainBelt || ft == structure.TypeIslandArc:
			line(gm, func(i int, u, s float64) { uplift(i, kernel(u)*max(0, pl.at(pl.regional, s)-f[i])) })
		case ft == structure.TypeRift:
			a := *pl.f.AxisFraction
			line(gm, func(i int, u, s float64) {
				uplift(i, kernel(max(0, u-a)/(1-a))*max(0, pl.at(pl.regional, s)-f[i]))
			})
		case ft == structure.TypePlateau:
			area(pl, func(i int, wt float64) { uplift(i, wt*max(0, pl.regional[0]-f[i])) })
		case ft == structure.TypeFault && pl.f.Sense == "thrust":
			line(gm, func(i int, u, _ float64) { uplift(i, kernel(u)*pl.offset) })
		case ft == structure.TypeFault && pl.f.Sense == "normal":
			line(gm, func(i int, u, _ float64) {
				if !downside(pl, i) {
					uplift(i, kernel(u)*pl.offset/2)
				}
			})
		}
	}
	// The uplifted surface takes up1's place.
	out := tb.up1
	for i := range np {
		out[i] = tb.base[i] + tb.up1[i] + in.regional.SecondUpliftShare*tb.up2[i]
	}

	// Depressions: the largest at each point, measured from the uplifted
	// surface.
	for _, it := range items {
		k := int(it[1])
		switch it[0] {
		case itemFeature:
			pl, gm := e.plans[k], e.geoms[k]
			switch ft := pl.f.Type; {
			case ft == structure.TypeBasin:
				area(pl, func(i int, wt float64) {
					deepen(i, wt*max(0, out[i]-pl.regional[0]))
					tb.basin[i] = max(tb.basin[i], wt)
				})
			case ft == structure.TypeRift:
				a := *pl.f.AxisFraction
				line(gm, func(i int, u, s float64) {
					if u < a {
						floor := max(pl.at(pl.regional, s), tb.base[i]) - pl.at(pl.relief, s)
						deepen(i, kernel(u/a)*max(0, out[i]-floor))
					}
				})
			case ft == structure.TypeFault && pl.f.Sense == "strike_slip":
				line(gm, func(i int, u, _ float64) { deepen(i, kernel(u)*pl.offset) })
			case ft == structure.TypeFault && pl.f.Sense == "normal":
				line(gm, func(i int, u, _ float64) {
					if downside(pl, i) {
						deepen(i, kernel(u)*pl.offset/2)
					}
				})
			}
		case itemTrough:
			gm, depth := e.trough[k], e.comp.file.Troughs[k].DepthM
			l := gm.cum[len(gm.cum)-1]
			fade := min(in.belts.FadeKM[0], l/3)
			line(gm, func(i int, u, s float64) {
				deepen(i, kernel(u)*min(smoothstep(0, fade, s), smoothstep(0, fade, l-s))*depth)
			})
		}
	}
	for i := range np {
		out[i] -= tb.dep[i]
	}

	// Ridges and peaks combine by maximum and add to the surface. crest
	// records, by cell, the crest of a ridge passing near a sample or of a
	// peak centered in the cell; comp the largest component sampled.
	local := func(i int) int { return ((w.wr0+i/w.wn)/e.s-r0)*nc + (w.wc0+i%w.wn)/e.s - c0 }
	near := math.Hypot(e.fx, e.fy) / 2
	put := func(i int, v float64) {
		v *= 1 - tb.basin[i]
		tb.ridge[i] = max(tb.ridge[i], v)
	}
	for _, it := range items {
		k := int(it[1])
		switch it[0] {
		case itemRidge:
			r, gm := &e.ridges[k], e.ridgeGeom[k]
			cors := e.corridors[r.rec.Feature]
			line(gm, func(i int, u, s float64) {
				crest := r.ampAt(s) * r.along(s)
				if crest <= 0 {
					return
				}
				low := 0.0
				p := point(i)
				for _, j := range cors {
					cr := &e.comp.file.Corridors[j]
					a, b := cr.Backbone[0], cr.Backbone[1]
					low = max(low, cr.SaddleShare*kernel(segmentDistance(e.dom, p, a, b)/(a.WidthKM/2)))
				}
				crest *= (1 - low) * (1 - tb.basin[i])
				tb.ridge[i] = max(tb.ridge[i], crest*kernel(u))
				if u*gm.line.WidthAt(s)/2 <= near {
					c := local(i)
					tb.crest[c] = max(tb.crest[c], crest)
				}
			})
		case itemPeak:
			pk := &e.comp.file.Peaks[k]
			r := pk.RadiusKM
			e.areaWindow(w, Rect{MinX: pk.X - r, MinY: pk.Y - r, MaxX: pk.X + r, MaxY: pk.Y + r}, func(i int, p structure.Point) {
				dx := p.X - pk.X
				dx -= math.Round(dx/e.dom.WidthKM) * e.dom.WidthKM
				dy := p.Y - pk.Y
				dy -= math.Round(dy/e.dom.HeightKM) * e.dom.HeightKM
				if d := math.Hypot(dx, dy); d < r {
					tb.touched[i] = true
					put(i, pk.HeightM*kernel(d/r))
				}
			})
			// The sample point holding the peak's center, if in this tile.
			pc, pr := int(pk.X/e.fx), int(pk.Y/e.fy)
			if pc >= w.wc0 && pc < w.wc0+w.wn && pr >= w.wr0 && pr < w.wr0+w.wm {
				i := (pr-w.wr0)*w.wn + pc - w.wc0
				c := local(i)
				tb.crest[c] = max(tb.crest[c], pk.HeightM*(1-tb.basin[i]))
			}
		}
	}
	for i := range np {
		out[i] += tb.ridge[i]
		c := local(i)
		tb.comp[c] = max(tb.comp[c], tb.ridge[i])
	}

	// Trenches lower the seabed toward their floor, never provisional land.
	for _, it := range items {
		if it[0] != itemTrench {
			continue
		}
		tp := &e.trenches[it[1]]
		l := tp.gm.cum[len(tp.gm.cum)-1]
		e.lineWindow(w, tp.gm, tb, func(i int, u, s float64) {
			if u >= 1 || in.land != nil && in.land[cellOf(i)] == 1 {
				return
			}
			tb.touched[i] = true
			wt := kernel(u) * min(smoothstep(0, tp.fade, s), smoothstep(0, tp.fade, l-s))
			out[i] -= wt * max(0, out[i]-tp.pl.at(tp.pl.regional, s))
		})
	}

	// Cells: the mean, largest, and smallest of their samples.
	for r := range nr {
		for c := range nc {
			cell := (r0+r)*e.cols + c0 + c
			sum, hi, lo, any := 0.0, math.Inf(-1), math.Inf(1), false
			for j := range e.s {
				row := (r*e.s + j) * w.wn
				for k := range e.s {
					i := row + c*e.s + k
					v := out[i]
					sum += v
					hi, lo = max(hi, v), min(lo, v)
					any = any || tb.touched[i]
				}
			}
			if !any {
				res.elevation[cell], res.high[cell], res.relief[cell] = in.center[cell], in.center[cell], 0
				continue
			}
			mean := sum / float64(e.s*e.s)
			lc := r*nc + c
			high := max(hi+max(0, tb.crest[lc]-tb.comp[lc]), mean)
			res.elevation[cell] = float32(mean)
			res.high[cell] = max(float32(high), float32(mean))
			res.relief[cell] = max(0, res.high[cell]-float32(lo))
		}
	}
}

// lineWindow calls fn for every sample point of the window within the
// widest half width of a linear item, with the point's index in the window,
// its cross-belt position u, and the arc length s of the nearest backbone
// point. Equal distances resolve to the lowest image, then the lowest
// segment.
func (e *engine) lineWindow(w window, gm *geom, tb *tileBuf, fn func(i int, u, s float64)) {
	R := gm.maxHalf
	first := func(lo, cell float64) int { return int(math.Ceil(lo/cell - 0.5)) }
	last := func(hi, cell float64) int { return int(math.Floor(hi/cell - 0.5)) }
	c0, c1 := first(gm.box.MinX-R, e.fx), last(gm.box.MaxX+R, e.fx)
	r0, r1 := first(gm.box.MinY-R, e.fy), last(gm.box.MaxY+R, e.fy)
	ceilDiv := func(a, b int) int { return -floorDiv(-a, b) }
	kx0, kx1 := ceilDiv(c0-(w.wc0+w.wn-1), e.fcols), floorDiv(c1-w.wc0, e.fcols)
	ky0, ky1 := ceilDiv(r0-(w.wr0+w.wm-1), e.frows), floorDiv(r1-w.wr0, e.frows)
	// The window-local box the images reach.
	lx0, ly0, lx1, ly1 := w.wn, w.wm, -1, -1
	for ky := ky0; ky <= ky1; ky++ {
		for kx := kx0; kx <= kx1; kx++ {
			ax, bx := max(c0, w.wc0+kx*e.fcols), min(c1, w.wc0+w.wn-1+kx*e.fcols)
			ay, by := max(r0, w.wr0+ky*e.frows), min(r1, w.wr0+w.wm-1+ky*e.frows)
			if ax > bx || ay > by {
				continue
			}
			lx0, lx1 = min(lx0, ax-kx*e.fcols-w.wc0), max(lx1, bx-kx*e.fcols-w.wc0)
			ly0, ly1 = min(ly0, ay-ky*e.frows-w.wr0), max(ly1, by-ky*e.frows-w.wr0)
		}
	}
	if lx1 < lx0 || ly1 < ly0 {
		return
	}
	bw, bh := lx1-lx0+1, ly1-ly0+1
	tb.best, tb.tt, tb.seg = grow(tb.best, bw*bh), grow(tb.tt, bw*bh), grow(tb.seg, bw*bh)
	for k := range tb.best {
		tb.best[k] = math.Inf(1)
	}
	for ky := ky0; ky <= ky1; ky++ {
		for kx := kx0; kx <= kx1; kx++ {
			ox, oy := kx*e.fcols+w.wc0+lx0, ky*e.frows+w.wr0+ly0 // unwrapped index of box (0, 0)
			ax, bx := max(c0, ox), min(c1, ox+bw-1)
			ay, by := max(r0, oy), min(r1, oy+bh-1)
			if ax > bx || ay > by {
				continue
			}
			for si := 0; si+1 < len(gm.bb); si++ {
				a, b := gm.bb[si], gm.bb[si+1]
				ex, ey := b.X-a.X, b.Y-a.Y
				ee := ex*ex + ey*ey
				ca, cb := max(ax, first(min(a.X, b.X)-R, e.fx)), min(bx, last(max(a.X, b.X)+R, e.fx))
				ra, rb := max(ay, first(min(a.Y, b.Y)-R, e.fy)), min(by, last(max(a.Y, b.Y)+R, e.fy))
				for r := ra; r <= rb; r++ {
					qy := (float64(r)+0.5)*e.fy - a.Y
					k := (r-oy)*bw + (ca - ox)
					for c := ca; c <= cb; c, k = c+1, k+1 {
						qx := (float64(c)+0.5)*e.fx - a.X
						t := min(max((qx*ex+qy*ey)/ee, 0), 1)
						vx, vy := qx-t*ex, qy-t*ey
						if d2 := vx*vx + vy*vy; d2 < tb.best[k] {
							tb.best[k], tb.seg[k], tb.tt[k] = d2, int32(si), t
						}
					}
				}
			}
		}
	}
	for k, d2 := range tb.best {
		if d2 > R*R {
			continue
		}
		si := tb.seg[k]
		s := gm.cum[si] + tb.tt[k]*(gm.cum[si+1]-gm.cum[si])
		fn((ly0+k/bw)*w.wn+lx0+k%bw, math.Sqrt(d2)/(gm.line.WidthAt(s)/2), s)
	}
}

func floorDiv(a, b int) int {
	q := a / b
	if (a%b != 0) && (a < 0) != (b < 0) {
		q--
	}
	return q
}

// areaWindow calls fn for every sample point of the window whose center
// lies in an unwrapped box narrower and shorter than the world, with the
// point's index in the window and its center in the base domain.
func (e *engine) areaWindow(w window, box Rect, fn func(i int, p structure.Point)) {
	W, H := e.dom.WidthKM, e.dom.HeightKM
	x0, x1 := float64(w.wc0)*e.fx, float64(w.wc0+w.wn)*e.fx
	y0, y1 := float64(w.wr0)*e.fy, float64(w.wr0+w.wm)*e.fy
	for ky := math.Ceil((box.MinY - y1) / H); ky <= math.Floor((box.MaxY-y0)/H); ky++ {
		for kx := math.Ceil((box.MinX - x1) / W); kx <= math.Floor((box.MaxX-x0)/W); kx++ {
			r := Rect{MinX: max(box.MinX-kx*W, x0), MinY: max(box.MinY-ky*H, y0), MaxX: min(box.MaxX-kx*W, x1), MaxY: min(box.MaxY-ky*H, y1)}
			if r.MinX > r.MaxX || r.MinY > r.MaxY {
				continue
			}
			structure.Visit(e.dom, e.fcols, e.frows, r, func(col, row int, p structure.Point) {
				if col >= w.wc0 && col < w.wc0+w.wn && row >= w.wr0 && row < w.wr0+w.wm {
					fn((row-w.wr0)*w.wn+col-w.wc0, p)
				}
			})
		}
	}
}

// diagnose reports each feature's drawn targets and core elevation, and
// each trench's floor and axis elevation, from cell centers.
func (e *engine) diagnose(ctx context.Context, elev []float32) ([]FeatureDiagnostics, []TrenchDiagnostics, error) {
	g, p := e.in.grid, e.in.regional
	cols, rows := g.Columns, g.Rows
	eachLine := func(bb []structure.Sample, fn func(cell int, u float64)) error {
		return structure.LineCells(ctx, e.dom, cols, rows, bb, 1, func(cell int, u, _ float64) {
			if u < 1 {
				fn(cell, u)
			}
		})
	}
	features := make([]FeatureDiagnostics, len(e.plans))
	for k, pl := range e.plans {
		var core []int32
		keep := func(cell int, w float64) {
			if w >= p.CoreWeight {
				core = append(core, int32(cell))
			}
		}
		var err error
		switch pl.f.Type {
		case structure.TypePlateau, structure.TypeBasin:
			structure.Visit(e.dom, cols, rows, pl.region.Bounds(), func(col, row int, pt structure.Point) {
				if m := pl.region.Mask(pt); m > 0 {
					keep(row*cols+col, smoothstep(0, 1, m))
				}
			})
		case structure.TypeRift:
			a := *pl.f.AxisFraction
			err = eachLine(pl.f.Backbone, func(cell int, u float64) {
				if u >= a {
					keep(cell, kernel((u-a)/(1-a)))
				}
			})
		default:
			err = eachLine(pl.f.Backbone, func(cell int, u float64) { keep(cell, kernel(u)) })
		}
		if err != nil {
			return nil, nil, err
		}
		d := FeatureDiagnostics{ID: pl.f.ID, Type: pl.f.Type, Kind: pl.f.Kind, RegionalM: span(pl.regional), ReliefM: span(pl.relief), OffsetM: pl.offset, Core: cellStats(elev, core)}
		if pl.f.Type == structure.TypeFault {
			d.Kind = pl.f.Sense
		}
		features[k] = d
	}
	var trenches []TrenchDiagnostics
	if e.in.trenches != nil && e.in.belts != nil {
		trenches = make([]TrenchDiagnostics, 0, len(e.trenches))
		for _, tp := range e.trenches {
			var axis []int32
			err := eachLine(tp.rec.Backbone, func(cell int, u float64) {
				if kernel(u) >= 0.9 && (e.in.land == nil || e.in.land[cell] == 0) {
					axis = append(axis, int32(cell))
				}
			})
			if err != nil {
				return nil, nil, err
			}
			trenches = append(trenches, TrenchDiagnostics{ID: tp.rec.ID, Parent: tp.rec.Parent, FloorM: span(tp.pl.regional), Axis: cellStats(elev, axis)})
		}
	}
	return features, trenches, ctx.Err()
}

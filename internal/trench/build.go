// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package trench

import (
	"context"
	"fmt"
	"math"

	"github.com/maloquacious/mapgen/internal/prng"
	"github.com/maloquacious/mapgen/internal/structure"
)

const (
	Algorithm    = "trench-v1"
	DomainTrench = "trench-v1/trench"
)

type (
	point = structure.Point
	side  = structure.Side
)

// Params are the calibration values of trench placement. They are
// recorded in the manifest.
type Params struct {
	Algorithm string `json:"algorithm"`
	// WidthKM is the range of a trench's full width.
	WidthKM [2]float64 `json:"width_km"`
	// EnvelopeGapKM is the range of the least water between the parent's
	// envelope and the trench edge.
	EnvelopeGapKM [2]float64 `json:"envelope_gap_km"`
	// CoastGapKM is the range of the least distance from the last
	// provisional land on the subducting side to the trench edge, and
	// CoastReachKM how far beyond the envelope that land is looked for.
	CoastGapKM   [2]float64 `json:"coast_gap_km"`
	CoastReachKM float64    `json:"coast_reach_km"`
	// MaxOffsetSlope is the fastest the trench's distance from the backbone
	// may change along it, in km per km.
	MaxOffsetSlope float64 `json:"max_offset_slope"`
	// ProbeKM is the reach of the open-water probe that decides polarity.
	ProbeKM float64 `json:"probe_km"`
	// DepthM is the range of the middle of a trench's floor range below the
	// datum, and DepthSpreadM half that floor range.
	DepthM       [2]float64 `json:"depth_m"`
	DepthSpreadM float64    `json:"depth_spread_m"`
	// MinLengthKM is the shortest trench kept.
	MinLengthKM float64 `json:"min_length_km"`
}

// DefaultParams returns the trench-v1 defaults.
func DefaultParams() Params {
	return Params{
		Algorithm: Algorithm, WidthKM: [2]float64{60, 120}, EnvelopeGapKM: [2]float64{20, 60},
		CoastGapKM: [2]float64{60, 150}, CoastReachKM: 400, MaxOffsetSlope: 0.5, ProbeKM: 1500,
		DepthM: [2]float64{6500, 7500}, DepthSpreadM: 500, MinLengthKM: 300,
	}
}

// Diagnostics are derived, reviewable facts about a run, with no timings.
type Diagnostics struct {
	// Subduction counts subduction records by basis.
	Subduction map[string]int `json:"subduction"`
	Trenches   int            `json:"trenches"`
	LengthKM   float64        `json:"length_km"`
	Omitted    []string       `json:"omitted"`
}

// grid answers provisional-land queries on the torus.
type grid struct {
	dom        structure.Domain
	cols, rows int
	cw, ch     float64
	step       float64 // one cell: the spacing of probes and stations
	land       []byte
}

func newGrid(dom structure.Domain, cols, rows int, land []byte) *grid {
	cw, ch := dom.WidthKM/float64(cols), dom.HeightKM/float64(rows)
	return &grid{dom: dom, cols: cols, rows: rows, cw: cw, ch: ch, step: min(cw, ch), land: land}
}

// landAt reports whether the cell containing p is provisional land.
func (g *grid) landAt(p point) bool {
	col := (int(math.Floor(p.X/g.cw))%g.cols + g.cols) % g.cols
	row := (int(math.Floor(p.Y/g.ch))%g.rows + g.rows) % g.rows
	return g.land[row*g.cols+col] == 1
}

// blocker is a shape that a trench may not enter, with its bounds.
type blocker struct {
	box   structure.Rect
	shape structure.Shape
}

func newBlocker(s structure.Shape) *blocker { return &blocker{box: s.Bounds(), shape: s} }

// contains reports whether p, or any periodic image of it, is in the shape.
func (b *blocker) contains(dom structure.Domain, p point) bool {
	x := b.box.MinX + mod(p.X-b.box.MinX, dom.WidthKM)
	y := b.box.MinY + mod(p.Y-b.box.MinY, dom.HeightKM)
	return x <= b.box.MaxX && y <= b.box.MaxY && b.shape.Contains(p)
}

func mod(v, period float64) float64 {
	v = math.Mod(v, period)
	if v < 0 {
		v += period
	}
	return v
}

func lerp(r [2]float64, u float64) float64 { return r[0] + u*(r[1]-r[0]) }

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }

// normal returns the unit normal at sample i of a backbone, pointing to s.
func normal(b []structure.Sample, i int, s side) point {
	a, z := b[max(i-1, 0)], b[min(i+1, len(b)-1)]
	tx, ty := z.X-a.X, z.Y-a.Y
	l := math.Hypot(tx, ty)
	// Right of the direction of travel is (-ty, tx); see structure.Side.
	if s == structure.Left {
		return point{X: ty / l, Y: -tx / l}
	}
	return point{X: -ty / l, Y: tx / l}
}

// related reports whether o may lie in the way of f's trench without
// blocking it: o is f, f's parent, or a child of f.
func related(f, o *structure.Feature) bool {
	return o.ID == f.ID || o.ID == f.Parent || o.Parent == f.ID
}

// build decides every subduction zone's polarity and places its trenches.
func build(ctx context.Context, seed uint64, p Params, g *grid, ff *structure.FeatureFile, diag *Diagnostics) (*TrenchFile, error) {
	tf := &TrenchFile{Format: RecordsFormat, Domain: ff.Domain, Subduction: []Subduction{}, Trenches: []Trench{}}
	diag.Subduction, diag.Omitted = map[string]int{}, []string{}
	shapes := make([]*blocker, len(ff.Features))
	for i := range ff.Features {
		// A fault may cross anything, so it blocks nothing.
		if f := &ff.Features[i]; f.Type != structure.TypeFault {
			shapes[i] = newBlocker(f.Shape(g.dom))
		}
	}
	var accepted []*blocker
	length := 0.0
	for i := range ff.Features {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		f := &ff.Features[i]
		if !Subducts(f) {
			continue
		}
		// Every draw is made, in a fixed order, whatever is used.
		rng := prng.New(seed, DomainTrench, *f.Key)
		width := round3(lerp(p.WidthKM, rng.Float64()))
		envGap := lerp(p.EnvelopeGapKM, rng.Float64())
		coastGap := lerp(p.CoastGapKM, rng.Float64())
		depth := math.Round(lerp(p.DepthM, rng.Float64()))
		tie := rng.Float64()
		s, basis := polarity(p, g, f, tie)
		tf.Subduction = append(tf.Subduction, Subduction{Feature: f.ID, Side: s.String(), Basis: basis})
		diag.Subduction[basis]++
		blocked := func(q point) bool {
			if g.landAt(q) {
				return true
			}
			for j, b := range shapes {
				if b != nil && !related(f, &ff.Features[j]) && b.contains(g.dom, q) {
					return true
				}
			}
			for _, b := range accepted {
				if b.contains(g.dom, q) {
					return true
				}
			}
			return false
		}
		runs := clip(g, course(p, g, f.Backbone, s, width, envGap, coastGap), width, blocked)
		for _, r := range runs {
			if r.length < p.MinLengthKM {
				diag.Omitted = append(diag.Omitted, fmt.Sprintf("feature %d: a %.0f km trench is shorter than %g km", f.ID, r.length, p.MinLengthKM))
				continue
			}
			key := rng.Uint64()
			pts := toBase(g.dom, r.pts)
			b := make([]structure.Sample, len(pts))
			for k, q := range pts {
				b[k] = structure.Sample{X: q.X, Y: q.Y, WidthKM: width}
			}
			t := Trench{ID: uint32(len(tf.Trenches) + 1), Parent: f.ID, Key: &key, Side: s.String(), Backbone: b,
				Targets: structure.Targets{RegionalElevationM: &[2]float64{-(depth + p.DepthSpreadM), -(depth - p.DepthSpreadM)}}}
			tf.Trenches = append(tf.Trenches, t)
			accepted = append(accepted, newBlocker(structure.NewLine(g.dom, b)))
			length += r.length
		}
		if len(runs) == 0 {
			diag.Omitted = append(diag.Omitted, fmt.Sprintf("feature %d: no clear water for a trench on its %s side", f.ID, s))
		}
	}
	diag.Trenches, diag.LengthKM = len(tf.Trenches), math.Round(length)
	return tf, nil
}

// polarity returns the side f subducts on, and why. tie breaks an exact
// tie of open water.
func polarity(p Params, g *grid, f *structure.Feature, tie float64) (side, string) {
	if f.Type == structure.TypeMountainBelt {
		switch f.ElevatedFlank {
		case structure.Left.String():
			return structure.Right, BasisFlank
		case structure.Right.String():
			return structure.Left, BasisFlank
		}
	}
	var open [2]float64
	for k, s := range []side{structure.Left, structure.Right} {
		for i, smp := range f.Backbone {
			n := normal(f.Backbone, i, s)
			edge := smp.WidthKM / 2
			d := edge
			for d < edge+p.ProbeKM && !g.landAt(point{X: smp.X + n.X*d, Y: smp.Y + n.Y*d}) {
				d += g.step
			}
			open[k] += min(d, edge+p.ProbeKM) - edge
		}
	}
	switch {
	case open[0] > open[1]:
		return structure.Left, BasisOcean
	case open[1] > open[0]:
		return structure.Right, BasisOcean
	case tie < 0.5:
		return structure.Left, BasisOcean
	}
	return structure.Right, BasisOcean
}

// course returns the trench axis beside backbone b on side s: one vertex
// per sample, at the least offset that clears the envelope and the coast,
// raised so it changes no faster than MaxOffsetSlope along the backbone.
// Vertices whose step runs backward against the backbone's are dropped.
func course(p Params, g *grid, b []structure.Sample, s side, width, envGap, coastGap float64) []point {
	n := len(b)
	need, cum := make([]float64, n), make([]float64, n)
	normals := make([]point, n)
	for i, smp := range b {
		if i > 0 {
			cum[i] = cum[i-1] + math.Hypot(smp.X-b[i-1].X, smp.Y-b[i-1].Y)
		}
		nm := normal(b, i, s)
		normals[i] = nm
		edge := smp.WidthKM / 2
		req := edge + envGap
		for d := edge; d <= edge+p.CoastReachKM; d += g.step {
			if g.landAt(point{X: smp.X + nm.X*d, Y: smp.Y + nm.Y*d}) {
				req = max(req, d+coastGap)
			}
		}
		need[i] = req + width/2
	}
	off := make([]float64, n)
	for i := range n {
		for j := range n {
			off[i] = max(off[i], need[j]-p.MaxOffsetSlope*math.Abs(cum[i]-cum[j]))
		}
	}
	var pts []point
	prev := -1
	for i, smp := range b {
		q := point{X: smp.X + normals[i].X*off[i], Y: smp.Y + normals[i].Y*off[i]}
		if prev >= 0 {
			last := pts[len(pts)-1]
			if (q.X-last.X)*(smp.X-b[prev].X)+(q.Y-last.Y)*(smp.Y-b[prev].Y) <= 0 {
				continue
			}
		}
		pts, prev = append(pts, q), i
	}
	return pts
}

// run is one clear stretch of a trench axis.
type run struct {
	pts    []point
	length float64
}

// clip walks the axis in stations about one cell apart and returns its
// clear stretches. A station is blocked when any point of a lattice one
// cell apart, within half the width plus one cell of the station, is
// blocked. The disks of a run's stations cover its whole envelope,
// including the rounded ends, and the extra cell catches every cell whose
// center the envelope reaches.
func clip(g *grid, axis []point, width float64, blocked func(point) bool) []run {
	type station struct {
		p      point
		s      float64
		vertex bool
		clear  bool
	}
	var st []station
	cum := 0.0
	var disk []point
	reach := width/2 + g.step
	n := int(math.Floor(reach / g.step))
	for j := -n; j <= n; j++ {
		for i := -n; i <= n; i++ {
			if dx, dy := float64(i)*g.step, float64(j)*g.step; dx*dx+dy*dy <= reach*reach {
				disk = append(disk, point{X: dx, Y: dy})
			}
		}
	}
	for i := 0; i+1 < len(axis); i++ {
		a, b := axis[i], axis[i+1]
		l := math.Hypot(b.X-a.X, b.Y-a.Y)
		k := max(1, int(math.Ceil(l/g.step)))
		last := k - 1
		if i+2 == len(axis) {
			last = k // the final vertex
		}
		for j := 0; j <= last; j++ {
			t := float64(j) / float64(k)
			q := point{X: a.X + t*(b.X-a.X), Y: a.Y + t*(b.Y-a.Y)}
			clear := true
			for _, d := range disk {
				if blocked(point{X: q.X + d.X, Y: q.Y + d.Y}) {
					clear = false
					break
				}
			}
			st = append(st, station{p: q, s: cum + t*l, vertex: j == 0 || j == k, clear: clear})
		}
		cum += l
	}
	var runs []run
	for a := 0; a < len(st); {
		if !st[a].clear {
			a++
			continue
		}
		z := a
		for z+1 < len(st) && st[z+1].clear {
			z++
		}
		if z > a {
			pts := []point{st[a].p}
			for k := a + 1; k < z; k++ {
				if st[k].vertex {
					pts = append(pts, st[k].p)
				}
			}
			end := st[z].p
			// Keep segments long enough to survive rounding.
			if q := pts[len(pts)-1]; len(pts) > 1 && math.Hypot(end.X-q.X, end.Y-q.Y) < 0.01 {
				pts = pts[:len(pts)-1]
			}
			runs = append(runs, run{pts: append(pts, end), length: st[z].s - st[a].s})
		}
		a = z + 1
	}
	return runs
}

// toBase moves unwrapped points by whole world widths and heights so the
// first lies in the base domain, rounding them as saved geometry is.
// Points closer than rounding allows to their predecessor are dropped.
func toBase(dom structure.Domain, pts []point) []point {
	ox, oy := math.Floor(pts[0].X/dom.WidthKM)*dom.WidthKM, math.Floor(pts[0].Y/dom.HeightKM)*dom.HeightKM
	out := make([]point, 0, len(pts))
	for _, q := range pts {
		r := point{X: round3(q.X - ox), Y: round3(q.Y - oy)}
		if len(out) > 0 && r == out[len(out)-1] {
			continue
		}
		out = append(out, r)
	}
	// Rounding can carry the first vertex onto the far edge.
	if out[0].X >= dom.WidthKM || out[0].Y >= dom.HeightKM {
		dx, dy := 0.0, 0.0
		if out[0].X >= dom.WidthKM {
			dx = dom.WidthKM
		}
		if out[0].Y >= dom.HeightKM {
			dy = dom.HeightKM
		}
		for k := range out {
			out[k] = point{X: round3(out[k].X - dx), Y: round3(out[k].Y - dy)}
		}
	}
	return out
}

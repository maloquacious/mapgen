// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package structure

import (
	"math"
	"sort"
)

// Side says where a point lies relative to a backbone, travelling along it
// in vertex order and viewing the map with north up.
type Side int8

const (
	On Side = iota
	Left
	Right
)

func (s Side) String() string { return [...]string{"on", "left", "right"}[s] }

// Rect is an unwrapped bounding box in km.
type Rect struct{ MinX, MinY, MaxX, MaxY float64 }

// Line answers geometry queries for a linear feature. Queries treat the
// domain as a torus: a point and the same point shifted by any whole number
// of world widths and heights give the same answer.
type Line struct {
	dom   Domain
	pts   []Point
	width []float64
	cum   []float64 // arc length at each vertex; cum[0] == 0
}

// NewLine prepares queries for a validated backbone.
func NewLine(dom Domain, backbone []Sample) *Line {
	l := &Line{dom: dom, pts: make([]Point, len(backbone)), width: make([]float64, len(backbone)), cum: make([]float64, len(backbone))}
	for i, s := range backbone {
		l.pts[i], l.width[i] = Point{s.X, s.Y}, s.WidthKM
		if i > 0 {
			l.cum[i] = l.cum[i-1] + math.Hypot(s.X-backbone[i-1].X, s.Y-backbone[i-1].Y)
		}
	}
	return l
}

// Length returns the backbone length in km.
func (l *Line) Length() float64 { return l.cum[len(l.cum)-1] }

// Nearest returns the arc-length position s of the backbone point nearest to
// p, the distance d to it, and the side p lies on. Beyond an end of the
// backbone, s is that end's position and d the distance to the end point.
//
// Equal distances resolve to the lowest segment index, then to the lowest y
// image offset, then to the lowest x image offset.
func (l *Line) Nearest(p Point) (s, d float64, side Side) {
	best := math.Inf(1)
	var bi int
	var bt, bvx, bvy float64
	for i := 0; i+1 < len(l.pts); i++ {
		a, b := l.pts[i], l.pts[i+1]
		ex, ey := b.X-a.X, b.Y-a.Y
		ee := ex*ex + ey*ey
		// The image of p nearest the segment's midpoint, and its neighbors.
		kx := math.Round(((a.X+b.X)/2 - p.X) / l.dom.WidthKM)
		ky := math.Round(((a.Y+b.Y)/2 - p.Y) / l.dom.HeightKM)
		for oy := -1.0; oy <= 1; oy++ {
			for ox := -1.0; ox <= 1; ox++ {
				qx, qy := p.X+(kx+ox)*l.dom.WidthKM-a.X, p.Y+(ky+oy)*l.dom.HeightKM-a.Y
				t := min(max((qx*ex+qy*ey)/ee, 0), 1)
				vx, vy := qx-t*ex, qy-t*ey
				if d2 := vx*vx + vy*vy; d2 < best {
					best, bi, bt, bvx, bvy = d2, i, t, vx, vy
				}
			}
		}
	}
	a, b := l.pts[bi], l.pts[bi+1]
	n := l.cum[bi+1] - l.cum[bi]
	tx, ty := (b.X-a.X)/n, (b.Y-a.Y)/n
	// At a vertex shared with a neighboring segment, judge the side against
	// the mean of the two tangents, so the outside of a bend stays outside.
	if j := bi + 1; bt == 1 && j+1 < len(l.pts) {
		m := l.cum[j+1] - l.cum[j]
		tx, ty = tx+(l.pts[j+1].X-b.X)/m, ty+(l.pts[j+1].Y-b.Y)/m
	} else if bt == 0 && bi > 0 {
		m := l.cum[bi] - l.cum[bi-1]
		tx, ty = tx+(a.X-l.pts[bi-1].X)/m, ty+(a.Y-l.pts[bi-1].Y)/m
	}
	switch c := cross(tx, ty, bvx, bvy); {
	case c > 0:
		side = Right
	case c < 0:
		side = Left
	}
	return l.cum[bi] + bt*n, math.Sqrt(best), side
}

// WidthAt returns the full width in km at arc-length position s, clamped to
// the backbone's ends.
func (l *Line) WidthAt(s float64) float64 {
	last := len(l.cum) - 1
	if s <= 0 {
		return l.width[0]
	}
	if s >= l.cum[last] {
		return l.width[last]
	}
	i := sort.SearchFloat64s(l.cum, s) // first vertex at or beyond s; i >= 1
	t := (s - l.cum[i-1]) / (l.cum[i] - l.cum[i-1])
	return l.width[i-1] + t*(l.width[i]-l.width[i-1])
}

// Cross returns the normalized cross-belt position of p: its distance from
// the backbone divided by the half width there. The envelope is Cross <= 1.
func (l *Line) Cross(p Point) float64 {
	s, d, _ := l.Nearest(p)
	return d / (l.WidthAt(s) / 2)
}

// Bounds returns the backbone's unwrapped bounding box, expanded by the
// largest half width.
func (l *Line) Bounds() Rect {
	r := Rect{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	half := 0.0
	for i, p := range l.pts {
		r = Rect{min(r.MinX, p.X), min(r.MinY, p.Y), max(r.MaxX, p.X), max(r.MaxY, p.Y)}
		half = max(half, l.width[i]/2)
	}
	return Rect{r.MinX - half, r.MinY - half, r.MaxX + half, r.MaxY + half}
}

// Region answers geometry queries for an area feature, on the torus.
type Region struct {
	dom    Domain
	pts    []Point
	edgeKM float64
	box    Rect
}

// NewRegion prepares queries for a validated outline.
func NewRegion(dom Domain, outline []Point, edgeKM float64) *Region {
	r := &Region{dom: dom, pts: outline, edgeKM: edgeKM, box: Rect{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}}
	for _, p := range outline {
		r.box = Rect{min(r.box.MinX, p.X), min(r.box.MinY, p.Y), max(r.box.MaxX, p.X), max(r.box.MaxY, p.Y)}
	}
	return r
}

// Bounds returns the outline's unwrapped bounding box.
func (r *Region) Bounds() Rect { return r.box }

// Inside returns the signed distance in km from p to the outline, positive
// inside. It is the greatest signed distance over the periodic images of p,
// so a point is inside when any of its images is.
func (r *Region) Inside(p Point) float64 {
	// An outline is narrower than the world, so the image of p nearest the
	// box center, with its neighbors, includes the nearest image.
	kx := math.Round(((r.box.MinX+r.box.MaxX)/2 - p.X) / r.dom.WidthKM)
	ky := math.Round(((r.box.MinY+r.box.MaxY)/2 - p.Y) / r.dom.HeightKM)
	best := math.Inf(-1)
	for oy := -1.0; oy <= 1; oy++ {
		for ox := -1.0; ox <= 1; ox++ {
			best = max(best, r.signed(Point{p.X + (kx+ox)*r.dom.WidthKM, p.Y + (ky+oy)*r.dom.HeightKM}))
		}
	}
	return best
}

// signed is the signed distance to the outline without wrapping.
func (r *Region) signed(q Point) float64 {
	d2 := math.Inf(1)
	inside := false
	n := len(r.pts)
	for i, a := range r.pts {
		b := r.pts[(i+1)%n]
		ex, ey := b.X-a.X, b.Y-a.Y
		qx, qy := q.X-a.X, q.Y-a.Y
		t := min(max((qx*ex+qy*ey)/(ex*ex+ey*ey), 0), 1)
		vx, vy := qx-t*ex, qy-t*ey
		d2 = min(d2, vx*vx+vy*vy)
		// Even-odd rule: count edges crossed by a ray toward +x.
		if (a.Y > q.Y) != (b.Y > q.Y) && q.X < a.X+(q.Y-a.Y)/ey*ex {
			inside = !inside
		}
	}
	if inside {
		return math.Sqrt(d2)
	}
	return -math.Sqrt(d2)
}

// Mask returns how fully the feature is present at p: 0 outside the outline,
// rising to 1 at edge_km inside it.
func (r *Region) Mask(p Point) float64 { return min(max(r.Inside(p)/r.edgeKM, 0), 1) }

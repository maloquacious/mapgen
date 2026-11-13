// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package structure

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"math"
	"slices"
)

// FeaturesFormat names the feature file schema, shared by features.json in an
// artifact and by the --features fixture.
const FeaturesFormat = "mapgen-structure-features-v1"

// Feature types.
const (
	TypeMountainBelt = "mountain_belt"
	TypeRift         = "rift"
	TypeIslandArc    = "island_arc"
	TypeFault        = "fault"
	TypePlateau      = "plateau"
	TypeBasin        = "basin"
)

// Domain is the physical extent of the world in km. Both axes are periodic.
type Domain struct {
	WidthKM  float64 `json:"width_km"`
	HeightKM float64 `json:"height_km"`
}

// Point is a position in km: X east from the western edge, Y south from the
// northern edge. Stored geometry is unwrapped, so a point may lie outside
// the base domain.
type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Sample is one backbone vertex with the feature's full width there.
type Sample struct {
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	WidthKM float64 `json:"width_km"`
}

// Targets are constraints for the terrain stage, as [min, max] in meters.
// They are not generated elevations.
type Targets struct {
	RegionalElevationM *[2]float64 `json:"regional_elevation_m,omitzero"`
	ReliefM            *[2]float64 `json:"relief_m,omitzero"`
}

// Feature is one geological event record. Which optional fields are required,
// and which are forbidden, depends on Type; see Validate.
type Feature struct {
	ID            uint32   `json:"id"`
	Type          string   `json:"type"`
	Kind          string   `json:"kind,omitempty"`
	Name          string   `json:"name,omitempty"`
	Key           *uint64  `json:"key,string"`
	Parent        uint32   `json:"parent,omitzero"`
	Side          string   `json:"side,omitempty"`
	Sense         string   `json:"sense,omitempty"`
	Maturity      *float64 `json:"maturity,omitzero"`
	ElevatedFlank string   `json:"elevated_flank,omitempty"`
	AxisFraction  *float64 `json:"axis_fraction,omitzero"`
	Backbone      []Sample `json:"backbone,omitempty"`
	Outline       []Point  `json:"outline,omitempty"`
	EdgeKM        float64  `json:"edge_km,omitzero"`
	Provinces     []uint32 `json:"provinces,omitempty"`
	Targets       *Targets `json:"targets,omitzero"`
}

// Linear reports whether f is described by a backbone.
func (f *Feature) Linear() bool {
	switch f.Type {
	case TypeMountainBelt, TypeRift, TypeIslandArc, TypeFault:
		return true
	}
	return false
}

// Area reports whether f is described by an outline.
func (f *Feature) Area() bool { return f.Type == TypePlateau || f.Type == TypeBasin }

// FeatureFile is the content of features.json or a fixture.
type FeatureFile struct {
	Format   string    `json:"format"`
	Domain   Domain    `json:"domain"`
	Features []Feature `json:"features"`
}

// ParseFeatures decodes a feature file, rejecting unknown fields. It does not
// validate the features; call Validate with the input artifact's domain.
func ParseFeatures(data []byte) (*FeatureFile, error) {
	var ff FeatureFile
	if err := json.Unmarshal(data, &ff, json.RejectUnknownMembers(true)); err != nil {
		return nil, fmt.Errorf("features: %w", err)
	}
	if ff.Format != FeaturesFormat {
		return nil, fmt.Errorf("features: unsupported format %q", ff.Format)
	}
	return &ff, nil
}

// Marshal encodes ff in its canonical form: the bytes stored as features.json.
func (ff *FeatureFile) Marshal() ([]byte, error) {
	data, err := json.Marshal(ff, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// Validate checks every rule that makes a feature file unusable, naming the
// feature at fault. dom is the physical extent of the input artifact.
//
// Rules that belong to automatic placement, such as self-intersection, are
// not errors here; see Warnings.
func (ff *FeatureFile) Validate(dom Domain) error {
	if ff.Format != FeaturesFormat {
		return fmt.Errorf("features: unsupported format %q", ff.Format)
	}
	if ff.Domain != dom {
		return fmt.Errorf("features: domain %g x %g km does not match the input artifact's %g x %g km",
			ff.Domain.WidthKM, ff.Domain.HeightKM, dom.WidthKM, dom.HeightKM)
	}
	byID := make(map[uint32]*Feature, len(ff.Features))
	for i := range ff.Features {
		f := &ff.Features[i]
		if f.ID == 0 {
			return fmt.Errorf("features: entry %d has id 0", i)
		}
		if byID[f.ID] != nil {
			return fmt.Errorf("feature %d: id is repeated", f.ID)
		}
		byID[f.ID] = f
	}
	for i := range ff.Features {
		f := &ff.Features[i]
		if err := f.validate(dom, byID); err != nil {
			return fmt.Errorf("feature %d: %w", f.ID, err)
		}
	}
	return nil
}

func oneOf(field, v string, allowed ...string) error {
	if !slices.Contains(allowed, v) {
		if v == "" {
			return fmt.Errorf("%s is required", field)
		}
		return fmt.Errorf("%s %q is not one of %v", field, v, allowed)
	}
	return nil
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// forbid returns an error naming the first field that is set but does not
// belong to the feature's type.
func (f *Feature) forbid(fields ...string) error {
	set := map[string]bool{
		"kind": f.Kind != "", "side": f.Side != "", "sense": f.Sense != "",
		"maturity": f.Maturity != nil, "elevated_flank": f.ElevatedFlank != "",
		"axis_fraction": f.AxisFraction != nil, "backbone": f.Backbone != nil,
		"outline": f.Outline != nil, "edge_km": f.EdgeKM != 0, "targets": f.Targets != nil,
	}
	for _, name := range fields {
		if set[name] {
			return fmt.Errorf("%s does not apply to a %s", name, f.Type)
		}
	}
	return nil
}

func (f *Feature) validate(dom Domain, byID map[uint32]*Feature) error {
	if f.Key == nil {
		return fmt.Errorf("key is required")
	}
	var parent *Feature
	if f.Parent != 0 {
		parent = byID[f.Parent]
		if parent == nil {
			return fmt.Errorf("parent %d does not exist", f.Parent)
		}
		if parent == f {
			return fmt.Errorf("feature is its own parent")
		}
	}
	if t := f.Targets; t != nil {
		for _, r := range []struct {
			name string
			v    *[2]float64
		}{{"regional_elevation_m", t.RegionalElevationM}, {"relief_m", t.ReliefM}} {
			if r.v != nil && !(finite(r.v[0]) && finite(r.v[1]) && r.v[0] <= r.v[1]) {
				return fmt.Errorf("targets.%s must be a finite [min, max] with min <= max", r.name)
			}
		}
	}
	var err error
	switch f.Type {
	case TypeMountainBelt:
		if err = oneOf("kind", f.Kind, "collision", "active_margin", "old_orogen"); err == nil {
			err = oneOf("elevated_flank", f.ElevatedFlank, "left", "right", "none")
		}
		// A belt names another as its parent when both are segments of one
		// interrupted belt.
		if err == nil && parent != nil && (parent.Type != TypeMountainBelt || parent.Kind != f.Kind) {
			err = fmt.Errorf("parent must be a mountain_belt of the same kind")
		}
		if err == nil && (f.Maturity == nil || !(*f.Maturity >= 0 && *f.Maturity <= 1)) {
			err = fmt.Errorf("maturity is required, in [0, 1]")
		}
		if err == nil {
			err = f.forbid("side", "sense", "axis_fraction", "outline", "edge_km")
		}
	case TypeRift:
		if f.AxisFraction == nil || !(*f.AxisFraction > 0 && *f.AxisFraction < 1) {
			err = fmt.Errorf("axis_fraction is required, in (0, 1)")
		} else {
			err = f.forbid("kind", "side", "sense", "maturity", "elevated_flank", "outline", "edge_km")
		}
	case TypeIslandArc:
		err = f.forbid("kind", "side", "sense", "maturity", "elevated_flank", "axis_fraction", "outline", "edge_km")
	case TypeFault:
		if err = oneOf("sense", f.Sense, "strike_slip", "normal", "thrust"); err == nil {
			err = f.forbid("kind", "side", "maturity", "elevated_flank", "axis_fraction", "outline", "edge_km", "targets")
		}
	case TypePlateau:
		switch {
		case parent == nil || parent.Type != TypeMountainBelt:
			err = fmt.Errorf("parent is required and must be a mountain_belt")
		case oneOf("side", f.Side, "left", "right") != nil:
			err = oneOf("side", f.Side, "left", "right")
		case f.Side != parent.ElevatedFlank:
			err = fmt.Errorf("side %q does not match parent %d's elevated_flank %q", f.Side, parent.ID, parent.ElevatedFlank)
		default:
			err = f.forbid("kind", "sense", "maturity", "elevated_flank", "axis_fraction", "backbone")
		}
	case TypeBasin:
		if err = oneOf("kind", f.Kind, "foreland", "intermontane", "interior"); err != nil {
			break
		}
		if f.Kind != "interior" || parent != nil || f.Side != "" {
			if parent == nil || !parent.Linear() || parent.Type == TypeFault {
				err = fmt.Errorf("parent must be a mountain_belt, rift, or island_arc")
			} else {
				err = oneOf("side", f.Side, "left", "right")
			}
		}
		if err == nil {
			err = f.forbid("sense", "maturity", "elevated_flank", "axis_fraction", "backbone")
		}
	case "":
		err = fmt.Errorf("type is required")
	default:
		err = fmt.Errorf("unknown type %q", f.Type)
	}
	if err != nil {
		return err
	}
	if f.Linear() {
		return validateBackbone(dom, f.Backbone)
	}
	if !(finite(f.EdgeKM) && f.EdgeKM > 0) {
		return fmt.Errorf("edge_km is required and must be positive")
	}
	return validateOutline(dom, f.Outline)
}

// validateStep checks one edge of unwrapped geometry.
func validateStep(dom Domain, a, b Point) error {
	dx, dy := b.X-a.X, b.Y-a.Y
	if dx == 0 && dy == 0 {
		return fmt.Errorf("zero-length segment at (%g, %g)", a.X, a.Y)
	}
	if !(math.Abs(dx) < dom.WidthKM/2 && math.Abs(dy) < dom.HeightKM/2) {
		return fmt.Errorf("segment from (%g, %g) spans half the world or more", a.X, a.Y)
	}
	return nil
}

func validateFirst(dom Domain, p Point) error {
	if !(p.X >= 0 && p.X < dom.WidthKM && p.Y >= 0 && p.Y < dom.HeightKM) {
		return fmt.Errorf("first vertex (%g, %g) is outside the base domain", p.X, p.Y)
	}
	return nil
}

// ValidateBackbone checks a backbone by the rules of a linear feature's:
// at least two finite samples with positive widths, the first in the base
// domain, and no zero-length segment or one spanning half the world. Later
// stages use it for linear records of their own.
func ValidateBackbone(dom Domain, b []Sample) error { return validateBackbone(dom, b) }

func validateBackbone(dom Domain, b []Sample) error {
	if len(b) < 2 {
		return fmt.Errorf("backbone needs at least two samples")
	}
	for i, s := range b {
		if !(finite(s.X) && finite(s.Y) && finite(s.WidthKM) && s.WidthKM > 0) {
			return fmt.Errorf("backbone sample %d must be finite with a positive width_km", i)
		}
	}
	if err := validateFirst(dom, Point{b[0].X, b[0].Y}); err != nil {
		return err
	}
	for i := 1; i < len(b); i++ {
		if err := validateStep(dom, Point{b[i-1].X, b[i-1].Y}, Point{b[i].X, b[i].Y}); err != nil {
			return err
		}
	}
	return nil
}

func validateOutline(dom Domain, o []Point) error {
	if len(o) < 3 {
		return fmt.Errorf("outline needs at least three vertices")
	}
	for i, p := range o {
		if !(finite(p.X) && finite(p.Y)) {
			return fmt.Errorf("outline vertex %d is not finite", i)
		}
	}
	if err := validateFirst(dom, o[0]); err != nil {
		return err
	}
	minP, maxP := o[0], o[0]
	for i, p := range o {
		if err := validateStep(dom, p, o[(i+1)%len(o)]); err != nil {
			return err
		}
		minP.X, minP.Y = min(minP.X, p.X), min(minP.Y, p.Y)
		maxP.X, maxP.Y = max(maxP.X, p.X), max(maxP.Y, p.Y)
	}
	if !(maxP.X-minP.X < dom.WidthKM && maxP.Y-minP.Y < dom.HeightKM) {
		return fmt.Errorf("outline is as wide or as tall as the world")
	}
	n := len(o)
	for i := range n {
		for j := i + 1; j < n; j++ {
			// Adjacent edges share a vertex; they cross only by overlapping.
			adjacent := j == i+1 || i == 0 && j == n-1
			if segmentsCross(o[i], o[(i+1)%n], o[j], o[(j+1)%n], adjacent) {
				return fmt.Errorf("outline is not simple: edges %d and %d cross", i, j)
			}
		}
	}
	return nil
}

func cross(ax, ay, bx, by float64) float64 { return ax*by - ay*bx }

func sign(v float64) int {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}

// onSegment reports whether p, known to be collinear with ab, lies within it.
func onSegment(a, b, p Point) bool {
	return min(a.X, b.X) <= p.X && p.X <= max(a.X, b.X) && min(a.Y, b.Y) <= p.Y && p.Y <= max(a.Y, b.Y)
}

// segmentsCross reports whether segments ab and cd intersect. For adjacent
// edges, which share exactly one endpoint, only a collinear overlap counts.
func segmentsCross(a, b, c, d Point, adjacent bool) bool {
	d1 := sign(cross(b.X-a.X, b.Y-a.Y, c.X-a.X, c.Y-a.Y))
	d2 := sign(cross(b.X-a.X, b.Y-a.Y, d.X-a.X, d.Y-a.Y))
	d3 := sign(cross(d.X-c.X, d.Y-c.Y, a.X-c.X, a.Y-c.Y))
	d4 := sign(cross(d.X-c.X, d.Y-c.Y, b.X-c.X, b.Y-c.Y))
	if adjacent {
		// Collinear and folding back over the shared vertex.
		if d1 != 0 || d2 != 0 || d3 != 0 || d4 != 0 {
			return false
		}
		shared, p, q := b, a, d // edges i, i+1 share b == c
		if a == d {
			shared, p, q = a, b, c // edges 0 and n-1 share a == d
		}
		return (p.X-shared.X)*(q.X-shared.X)+(p.Y-shared.Y)*(q.Y-shared.Y) > 0
	}
	if d1 != d2 && d3 != d4 {
		return true
	}
	return d1 == 0 && onSegment(a, b, c) || d2 == 0 && onSegment(a, b, d) ||
		d3 == 0 && onSegment(c, d, a) || d4 == 0 && onSegment(c, d, b)
}

// Warnings lists placement rules the features break. A fixture may break
// them deliberately, so they are recorded in diagnostics, not rejected.
// Version one reports backbones that cross themselves.
func (ff *FeatureFile) Warnings() []string {
	warnings := []string{}
	for i := range ff.Features {
		f := &ff.Features[i]
		b := f.Backbone
		for i := 0; i+1 < len(b) && f.Linear(); i++ {
			for j := i + 2; j+1 < len(b); j++ {
				if segmentsCross(Point{b[i].X, b[i].Y}, Point{b[i+1].X, b[i+1].Y}, Point{b[j].X, b[j].Y}, Point{b[j+1].X, b[j+1].Y}, false) {
					warnings = append(warnings, fmt.Sprintf("feature %d: backbone segments %d and %d cross", f.ID, i, j))
				}
			}
		}
	}
	return warnings
}

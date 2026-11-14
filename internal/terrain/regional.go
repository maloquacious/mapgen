// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package terrain

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"

	"github.com/maloquacious/mapgen/internal/continent"
	"github.com/maloquacious/mapgen/internal/prng"
	"github.com/maloquacious/mapgen/internal/structure"
)

// The regional surface pulls the foundation toward each feature's regional
// target through the feature's envelope weight. See "Regional surface" in
// the design.
const (
	RegionalAlgorithm = "regional-v1"
	DomainFeature     = "terrain-v1/feature"
)

// TargetRange is a feature's target ranges, in m.
type TargetRange struct {
	RegionalElevationM [2]float64 `json:"regional_elevation_m"`
	ReliefM            [2]float64 `json:"relief_m"`
}

// RegionalParams are the calibration values behind the regional surface.
// They are recorded in the manifest.
type RegionalParams struct {
	Algorithm string `json:"algorithm"`
	// Kernel and AreaWeight describe the envelope weights.
	Kernel     string `json:"kernel"`
	AreaWeight string `json:"area_weight"`
	Rift       string `json:"rift"`
	// ControlSpacingKM is the range of the spacing of a linear feature's
	// target control samples along its backbone.
	ControlSpacingKM [2]float64 `json:"control_spacing_km"`
	// SecondUpliftShare is the share of the second-largest uplift added to
	// the largest.
	SecondUpliftShare float64 `json:"second_uplift_share"`
	// DefaultTargets are the targets of a feature whose record gives none,
	// keyed by type, or by type, "/", and kind for belts and basins. A
	// record that gives one target takes the other from here.
	DefaultTargets map[string]TargetRange `json:"default_targets"`
	// FaultM is the range of a fault's offset by sense: the depth of a
	// strike-slip trough, the height of a normal fault's step, and the
	// height of a thrust ridge.
	FaultM map[string][2]float64 `json:"fault_m"`
	// CoreWeight is the least envelope weight of the cells whose elevation
	// the diagnostics report as a feature's core.
	CoreWeight float64 `json:"core_weight"`
}

// DefaultRegionalParams returns the regional-v1 defaults. The default
// targets are those structure's automatic placement assigns.
func DefaultRegionalParams() RegionalParams {
	return RegionalParams{
		Algorithm:         RegionalAlgorithm,
		Kernel:            "K(t) = (1 - t^2)^2 for |t| < 1, t the cross-belt position",
		AreaWeight:        "smoothstep of the outline mask",
		Rift:              "shoulders weigh K((t - a) / (1 - a)) beyond the axis fraction a and 1 within it; the axis is a depression weighing K(t / a) to the shoulder level minus the relief draw",
		ControlSpacingKM:  [2]float64{150, 400},
		SecondUpliftShare: 0.25,
		DefaultTargets: map[string]TargetRange{
			"mountain_belt/collision":     {[2]float64{1500, 3000}, [2]float64{2500, 5000}},
			"mountain_belt/active_margin": {[2]float64{1000, 2500}, [2]float64{2000, 4000}},
			"mountain_belt/old_orogen":    {[2]float64{300, 1200}, [2]float64{500, 2500}},
			structure.TypeRift:            {[2]float64{300, 1500}, [2]float64{500, 2000}},
			structure.TypeIslandArc:       {[2]float64{-1500, 500}, [2]float64{1000, 3000}},
			structure.TypePlateau:         {[2]float64{2500, 4500}, [2]float64{200, 800}},
			"basin/foreland":              {[2]float64{0, 400}, [2]float64{0, 200}},
			"basin/intermontane":          {[2]float64{500, 2000}, [2]float64{0, 400}},
			"basin/interior":              {[2]float64{0, 300}, [2]float64{0, 150}},
		},
		FaultM: map[string][2]float64{
			"strike_slip": {50, 200},
			"normal":      {100, 300},
			"thrust":      {100, 300},
		},
		CoreWeight: 0.9,
	}
}

// kernel is the compact cross-belt kernel K.
func kernel(t float64) float64 {
	if t >= 1 || t <= -1 {
		return 0
	}
	v := 1 - t*t
	return v * v
}

// targetKey names a feature's entry in DefaultTargets.
func targetKey(f *structure.Feature) string {
	if f.Type == structure.TypeMountainBelt || f.Type == structure.TypeBasin {
		return f.Type + "/" + f.Kind
	}
	return f.Type
}

// targets returns a feature's target ranges, the record's where it gives
// them.
func (p RegionalParams) targets(f *structure.Feature) (TargetRange, error) {
	tr, ok := p.DefaultTargets[targetKey(f)]
	if !ok {
		return tr, fmt.Errorf("regional parameters have no default targets for %s", targetKey(f))
	}
	if t := f.Targets; t != nil {
		if t.RegionalElevationM != nil {
			tr.RegionalElevationM = *t.RegionalElevationM
		}
		if t.ReliefM != nil {
			tr.ReliefM = *t.ReliefM
		}
	}
	return tr, nil
}

// plan is one feature's drawn values and geometry.
type plan struct {
	f      *structure.Feature
	line   *structure.Line
	region *structure.Region
	// Linear features: control samples every step km along the backbone,
	// from s = 0 to its length. Area features: one value each.
	step             float64
	regional, relief []float64
	// Faults: the offset, and whether the left side is downthrown.
	offset   float64
	downLeft bool
}

// newPlan draws a feature's values from its terrain stream. The order of
// draws is part of terrain-v1: a fault draws its offset and then its
// downthrown side; a linear feature draws its control spacing and then, at
// each control sample, its regional and relief values; an area feature
// draws its regional and relief values.
func newPlan(seed uint64, p RegionalParams, dom structure.Domain, f *structure.Feature) (*plan, error) {
	rng := prng.New(seed, DomainFeature, *f.Key)
	pl := &plan{f: f}
	if f.Linear() {
		pl.line = structure.NewLine(dom, f.Backbone)
	} else {
		pl.region = structure.NewRegion(dom, f.Outline, f.EdgeKM)
	}
	if f.Type == structure.TypeFault {
		r, ok := p.FaultM[f.Sense]
		if !ok {
			return nil, fmt.Errorf("regional parameters have no fault offset for %s", f.Sense)
		}
		pl.offset = lerp(r, rng.Float64())
		pl.downLeft = rng.Float64() < 0.5
		return pl, nil
	}
	tr, err := p.targets(f)
	if err != nil {
		return nil, err
	}
	draw := func(rng *rand.Rand) {
		pl.regional = append(pl.regional, lerp(tr.RegionalElevationM, rng.Float64()))
		pl.relief = append(pl.relief, lerp(tr.ReliefM, rng.Float64()))
	}
	if pl.line == nil {
		draw(rng)
		return pl, nil
	}
	length := pl.line.Length()
	n := max(1, int(math.Round(length/lerp(p.ControlSpacingKM, rng.Float64()))))
	pl.step = length / float64(n)
	for range n + 1 {
		draw(rng)
	}
	return pl, nil
}

// at interpolates control samples v at arc-length position s, easing from
// each sample to the next, so the value stays within the range the samples
// span.
func (pl *plan) at(v []float64, s float64) float64 {
	if len(v) == 1 {
		return v[0]
	}
	x := min(max(s/pl.step, 0), float64(len(v)-1))
	i := min(int(x), len(v)-2)
	return v[i] + (v[i+1]-v[i])*smoothstep(0, 1, x-float64(i))
}

// span returns the least and greatest of v.
func span(v []float64) *[2]float64 {
	if len(v) == 0 {
		return nil
	}
	r := [2]float64{v[0], v[0]}
	for _, x := range v {
		r = [2]float64{min(r[0], x), max(r[1], x)}
	}
	return &r
}

// FeatureDiagnostics reports one feature's drawn targets and the elevation
// of its core, the cells where its own weight is at least CoreWeight. A
// rift's core is its shoulders.
type FeatureDiagnostics struct {
	ID        uint32      `json:"id"`
	Type      string      `json:"type"`
	Kind      string      `json:"kind,omitempty"`
	RegionalM *[2]float64 `json:"regional_m,omitzero"`
	ReliefM   *[2]float64 `json:"relief_m,omitzero"`
	OffsetM   float64     `json:"offset_m,omitzero"`
	Core      Stats       `json:"core"`
}

// regional builds the regional surface over the foundation f. Every per-cell
// result depends only on that cell, and features are visited in order, so
// the result does not depend on how the work is divided.
func regional(ctx context.Context, seed uint64, p RegionalParams, g continent.Grid, ff *structure.FeatureFile, f []float32) ([]float32, []FeatureDiagnostics, error) {
	dom := ff.Domain
	cols, rows := g.Columns, g.Rows
	plans := make([]*plan, len(ff.Features))
	for i := range ff.Features {
		var err error
		if plans[i], err = newPlan(seed, p, dom, &ff.Features[i]); err != nil {
			return nil, nil, fmt.Errorf("feature %d: %w", ff.Features[i].ID, err)
		}
	}
	n := len(f)
	cores := make([][]int32, len(plans))
	// Linear features visit the cells their envelope reaches; area features
	// the cells whose centers lie in their outline's box.
	eachLine := func(pl *plan, fn func(cell int, u, s float64)) error {
		return structure.LineCells(ctx, dom, cols, rows, pl.f.Backbone, 1, func(cell int, u, s float64) {
			if u < 1 {
				fn(cell, u, s)
			}
		})
	}
	eachArea := func(pl *plan, fn func(cell int, w float64)) error {
		structure.Visit(dom, cols, rows, pl.region.Bounds(), func(col, row int, pt structure.Point) {
			if m := pl.region.Mask(pt); m > 0 {
				fn(row*cols+col, smoothstep(0, 1, m))
			}
		})
		return ctx.Err()
	}
	// downside reports whether a cell lies on a normal fault's downthrown
	// side. A cell on the line counts as the upthrown side.
	downside := func(pl *plan, cell int) bool {
		pt := structure.Point{X: (float64(cell%cols) + 0.5) * g.CellWidthKM, Y: (float64(cell/cols) + 0.5) * g.CellHeightKM}
		_, _, side := pl.line.Nearest(pt)
		return side == structure.Left && pl.downLeft || side == structure.Right && !pl.downLeft
	}
	core := func(k int, cell int, w float64) {
		if w >= p.CoreWeight {
			cores[k] = append(cores[k], int32(cell))
		}
	}

	// Uplifts: the two largest at each cell.
	up1, up2 := make([]float32, n), make([]float32, n)
	uplift := func(cell int, v float64) {
		x := float32(v)
		if x > up1[cell] {
			up1[cell], up2[cell] = x, up1[cell]
		} else if x > up2[cell] {
			up2[cell] = x
		}
	}
	for k, pl := range plans {
		var err error
		switch ft := pl.f.Type; {
		case ft == structure.TypeMountainBelt || ft == structure.TypeIslandArc:
			err = eachLine(pl, func(cell int, u, s float64) {
				w := kernel(u)
				uplift(cell, w*max(0, pl.at(pl.regional, s)-float64(f[cell])))
				core(k, cell, w)
			})
		case ft == structure.TypeRift:
			a := *pl.f.AxisFraction
			err = eachLine(pl, func(cell int, u, s float64) {
				w := kernel(max(0, u-a) / (1 - a))
				uplift(cell, w*max(0, pl.at(pl.regional, s)-float64(f[cell])))
				if u >= a {
					core(k, cell, w)
				}
			})
		case ft == structure.TypePlateau:
			err = eachArea(pl, func(cell int, w float64) {
				uplift(cell, w*max(0, pl.regional[0]-float64(f[cell])))
				core(k, cell, w)
			})
		case ft == structure.TypeFault && pl.f.Sense == "thrust":
			err = eachLine(pl, func(cell int, u, _ float64) {
				w := kernel(u)
				uplift(cell, w*pl.offset)
				core(k, cell, w)
			})
		case ft == structure.TypeFault && pl.f.Sense == "normal":
			err = eachLine(pl, func(cell int, u, _ float64) {
				w := kernel(u)
				if !downside(pl, cell) {
					uplift(cell, w*pl.offset/2)
				}
				core(k, cell, w)
			})
		}
		if err != nil {
			return nil, nil, err
		}
	}
	// The uplifted surface takes up1's place, and the depressions up2's.
	out, dep := up1, up2
	for i := range out {
		out[i] = float32(float64(f[i]) + float64(up1[i]) + p.SecondUpliftShare*float64(up2[i]))
		dep[i] = 0
	}

	// Depressions: the largest at each cell, measured from the uplifted
	// surface.
	deepen := func(cell int, v float64) { dep[cell] = max(dep[cell], float32(v)) }
	for k, pl := range plans {
		var err error
		switch ft := pl.f.Type; {
		case ft == structure.TypeBasin:
			err = eachArea(pl, func(cell int, w float64) {
				deepen(cell, w*max(0, float64(out[cell])-pl.regional[0]))
				core(k, cell, w)
			})
		case ft == structure.TypeRift:
			a := *pl.f.AxisFraction
			err = eachLine(pl, func(cell int, u, s float64) {
				if u < a {
					floor := max(pl.at(pl.regional, s), float64(f[cell])) - pl.at(pl.relief, s)
					deepen(cell, kernel(u/a)*max(0, float64(out[cell])-floor))
				}
			})
		case ft == structure.TypeFault && pl.f.Sense == "strike_slip":
			err = eachLine(pl, func(cell int, u, _ float64) {
				w := kernel(u)
				deepen(cell, w*pl.offset)
				core(k, cell, w)
			})
		case ft == structure.TypeFault && pl.f.Sense == "normal":
			err = eachLine(pl, func(cell int, u, _ float64) {
				if downside(pl, cell) {
					deepen(cell, kernel(u)*pl.offset/2)
				}
			})
		}
		if err != nil {
			return nil, nil, err
		}
	}
	for i := range out {
		out[i] -= dep[i]
	}

	diags := make([]FeatureDiagnostics, len(plans))
	for k, pl := range plans {
		d := FeatureDiagnostics{ID: pl.f.ID, Type: pl.f.Type, Kind: pl.f.Kind, RegionalM: span(pl.regional), ReliefM: span(pl.relief), OffsetM: pl.offset}
		if pl.f.Type == structure.TypeFault {
			d.Kind = pl.f.Sense
		}
		if c := cores[k]; len(c) > 0 {
			d.Core = Stats{Cells: len(c), MinM: math.Inf(1), MaxM: math.Inf(-1)}
			sum := 0.0
			for _, cell := range c {
				e := float64(out[cell])
				d.Core.MinM, d.Core.MaxM = min(d.Core.MinM, e), max(d.Core.MaxM, e)
				sum += e
			}
			d.Core.MeanM = sum / float64(len(c))
		}
		diags[k] = d
	}
	return out, diags, ctx.Err()
}

// shelfNarrowing returns, for every cell, how far the shelf is narrowed
// toward NarrowShelfKM: 1 within the first NarrowShelfReach half widths of
// an active margin or island arc, easing to 0 at the second. It is nil when
// there is no such feature.
func shelfNarrowing(ctx context.Context, p FoundationParams, g continent.Grid, ff *structure.FeatureFile) ([]float32, error) {
	var out []float32
	for i := range ff.Features {
		f := &ff.Features[i]
		if !(f.Type == structure.TypeIslandArc || f.Type == structure.TypeMountainBelt && f.Kind == "active_margin") {
			continue
		}
		if out == nil {
			out = make([]float32, g.Columns*g.Rows)
		}
		r := p.NarrowShelfReach
		err := structure.LineCells(ctx, ff.Domain, g.Columns, g.Rows, f.Backbone, r[1], func(cell int, u, _ float64) {
			out[cell] = max(out[cell], float32(1-smoothstep(r[0], r[1], u)))
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

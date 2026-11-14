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
	RegionalAlgorithm = "regional-v3"
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
	// A plateau's surface rolls by PlateauRolling of its relief draw and
	// its edge band is dissected by up to PlateauRim of it, following two
	// smooth fields of PlateauWavelengthKM.
	PlateauRolling      float64    `json:"plateau_rolling"`
	PlateauRim          float64    `json:"plateau_rim"`
	PlateauWavelengthKM [2]float64 `json:"plateau_wavelength_km"`
	// RiftFloorM is how far a rift's axis may reach below the lower of the
	// foundation and the datum, so a rift on land ends no deeper than a dry
	// continental depression and landmask can raise a rift cut off from the
	// ocean to the frozen coast within its limit.
	RiftFloorM float64 `json:"rift_floor_m"`
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
		Rift:              "shoulders weigh K((t - a) / (1 - a)) beyond the axis fraction a and 1 within it; the axis is a depression weighing K(t / a) to the shoulder level minus the relief draw, at least rift_floor_m below min(foundation, 0)",
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
		PlateauRolling: 0.25, PlateauRim: 0.5, PlateauWavelengthKM: [2]float64{120, 48},
		RiftFloorM: 300,
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
	reliefRange      [2]float64
	// Faults: the offset, and whether the left side is downthrown.
	offset   float64
	downLeft bool
	// rng is the feature's stream after these draws; belt components
	// continue it.
	rng *rand.Rand
}

// newPlan draws a feature's values from its terrain stream. The order of
// draws is part of terrain-v1: a fault draws its offset and then its
// downthrown side; a linear feature draws its control spacing and then, at
// each control sample, its regional and relief values; an area feature
// draws its regional and relief values.
func newPlan(seed uint64, p RegionalParams, dom structure.Domain, f *structure.Feature) (*plan, error) {
	rng := prng.New(seed, DomainFeature, *f.Key)
	pl := &plan{f: f, rng: rng}
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
	pl.reliefRange = tr.ReliefM
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

// cellStats summarizes v over the given cells.
func cellStats(v []float32, cells []int32) Stats {
	if len(cells) == 0 {
		return Stats{}
	}
	st := Stats{Cells: len(cells), MinM: math.Inf(1), MaxM: math.Inf(-1)}
	sum := 0.0
	for _, cell := range cells {
		e := float64(v[cell])
		st.MinM, st.MaxM = min(st.MinM, e), max(st.MaxM, e)
		sum += e
	}
	st.MeanM = sum / float64(len(cells))
	return st
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

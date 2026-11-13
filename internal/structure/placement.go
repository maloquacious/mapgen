// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package structure

import (
	"context"
	"fmt"
	"maps"
	"math"
	"math/rand/v2"
	"slices"
	"strings"

	"github.com/maloquacious/mapgen/internal/prng"
)

const (
	PlacementAlgorithm = "placement-v1"
	ModeAutomatic      = "automatic"
	DomainRequests     = "structure-v1/requests"
	DomainPlace        = "structure-v1/place/" // followed by the family name

	FamilyCollision    = "collision"
	FamilyActiveMargin = "active_margin"
	FamilyOldOrogen    = "old_orogen"
	FamilyRift         = "rift"
	FamilyIslandArc    = "island_arc"
	FamilyFault        = "fault"
)

// familyNumbers are the sequence numbers of each family's request stream.
// They are fixed, so adding a family does not disturb the others.
var familyNumbers = map[string]uint64{FamilyCollision: 0, FamilyActiveMargin: 1, FamilyOldOrogen: 2, FamilyRift: 3, FamilyIslandArc: 4, FamilyFault: 5}

// FamilyParams are the calibration values for one family of events.
type FamilyParams struct {
	// One event is requested per this much provisional land, in million
	// square km, rounded, and never fewer than Minimum.
	AreaPerEvent float64    `json:"area_per_event_million_km2"`
	Minimum      int        `json:"minimum"`
	LengthKM     [2]float64 `json:"length_km"`
	// WidthKM is the range of the full width at the middle of the feature.
	WidthKM  [2]float64  `json:"width_km"`
	Maturity *[2]float64 `json:"maturity,omitzero"`
}

// PlacementParams are the calibration values behind automatic placement.
// They are recorded in the manifest.
type PlacementParams struct {
	Algorithm string `json:"algorithm"`
	// Attempts is the number of candidates tried for each requested event.
	Attempts int `json:"attempts"`
	// StepKM is the length of one step of a growing backbone, and SampleKM
	// the spacing of the saved backbone samples.
	StepKM   float64 `json:"step_km"`
	SampleKM float64 `json:"sample_km"`
	// GapKM is the least distance between the envelopes of unrelated
	// features.
	GapKM float64 `json:"gap_km"`
	// EndWidth is the width at a feature's ends as a fraction of the width
	// at its middle.
	EndWidth float64 `json:"end_width"`
	// A backbone bends no tighter than BendRadius times its width, and
	// never tighter than MinBendKM.
	BendRadius float64 `json:"bend_radius"`
	MinBendKM  float64 `json:"min_bend_km"`
	// MaxSwingDeg is how far either half of a backbone may turn from the
	// heading it started with, so a belt curves but does not hook back.
	MaxSwingDeg float64 `json:"max_swing_deg"`
	// PlateauDepth and ForelandDepth are how far a collision belt's plateau
	// and foreland basin reach beyond the belt, as multiples of its width.
	PlateauDepth  [2]float64 `json:"plateau_depth"`
	ForelandDepth [2]float64 `json:"foreland_depth"`
	AxisFraction  [2]float64 `json:"axis_fraction"`
	// Coastal features are placed against the major coast: the coast
	// between landmasses of at least MainlandKM2 and waters of at least
	// OceanKM2. Smaller waters count as land and smaller islands as water.
	MainlandKM2 float64 `json:"mainland_km2"`
	OceanKM2    float64 `json:"ocean_km2"`
	// An active margin's backbone follows the major coast MarginOffsetKM
	// inland, and never nearer than half its width. An island arc's follows
	// it ArcOffsetKM offshore. Either ends where it strays more than
	// DriftKM from that distance. Neither half of a margin may turn more
	// than CoastSwingDeg from the heading it started with; an arc is held
	// to MaxSwingDeg.
	MarginOffsetKM [2]float64 `json:"margin_offset_km"`
	ArcOffsetKM    [2]float64 `json:"arc_offset_km"`
	DriftKM        float64    `json:"drift_km"`
	CoastSwingDeg  float64    `json:"coast_swing_deg"`
	// MarginBasin is the share of active margins given an inland basin.
	MarginBasin float64 `json:"margin_basin"`
	// FaultSwingDeg is MaxSwingDeg for faults, and FaultSense the shares
	// that are strike-slip and normal; the rest are thrusts.
	FaultSwingDeg float64      `json:"fault_swing_deg"`
	FaultSense    [2]float64   `json:"fault_sense"`
	Collision     FamilyParams `json:"collision"`
	ActiveMargin  FamilyParams `json:"active_margin"`
	OldOrogen     FamilyParams `json:"old_orogen"`
	Rift          FamilyParams `json:"rift"`
	IslandArc     FamilyParams `json:"island_arc"`
	Fault         FamilyParams `json:"fault"`
}

// DefaultPlacementParams returns the placement-v1 defaults. Counts and
// widths follow the cylindrical specification; the rest are starting values.
func DefaultPlacementParams() PlacementParams {
	return PlacementParams{
		Algorithm: PlacementAlgorithm, Attempts: 32, StepKM: 50, SampleKM: 100, GapKM: 100,
		EndWidth: 0.55, BendRadius: 1, MinBendKM: 300, MaxSwingDeg: 60,
		PlateauDepth: [2]float64{0.5, 1}, ForelandDepth: [2]float64{0.4, 0.8}, AxisFraction: [2]float64{0.2, 0.4},
		MainlandKM2: 1e6, OceanKM2: 2e6, MarginOffsetKM: [2]float64{50, 200}, ArcOffsetKM: [2]float64{200, 600},
		DriftKM: 100, CoastSwingDeg: 90, MarginBasin: 0.5, FaultSwingDeg: 20, FaultSense: [2]float64{0.5, 0.25},
		Collision:    FamilyParams{AreaPerEvent: 35, Minimum: 1, LengthKM: [2]float64{1500, 5000}, WidthKM: [2]float64{300, 1200}, Maturity: &[2]float64{0, 0.3}},
		ActiveMargin: FamilyParams{AreaPerEvent: 30, Minimum: 1, LengthKM: [2]float64{1500, 6000}, WidthKM: [2]float64{100, 400}, Maturity: &[2]float64{0, 0.4}},
		OldOrogen:    FamilyParams{AreaPerEvent: 20, Minimum: 1, LengthKM: [2]float64{1000, 3500}, WidthKM: [2]float64{100, 600}, Maturity: &[2]float64{0.6, 1}},
		Rift:         FamilyParams{AreaPerEvent: 45, Minimum: 0, LengthKM: [2]float64{800, 3000}, WidthKM: [2]float64{50, 300}},
		IslandArc:    FamilyParams{AreaPerEvent: 50, Minimum: 0, LengthKM: [2]float64{600, 2500}, WidthKM: [2]float64{30, 150}},
		Fault:        FamilyParams{AreaPerEvent: 25, Minimum: 1, LengthKM: [2]float64{300, 2000}, WidthKM: [2]float64{20, 100}},
	}
}

// Reasons a candidate is rejected; they are tallied in the diagnostics.
const (
	reasonNoRoom   = "no room"
	reasonNarrow   = "land too narrow"
	reasonShort    = "too short"
	reasonCrossing = "crosses itself"
	reasonOverlap  = "overlaps another feature"
)

// course says how a family's backbone finds its way.
type course struct {
	// width is the full width at the middle, or zero to draw it from what
	// the land at the anchor allows.
	width float64
	// land keeps the backbone on land, with room for its end width.
	land bool
	// With a field, the backbone follows the field's contour at target.
	field  []float64
	target float64
	// free lets the backbone cross other features.
	free bool
	// swing is how far, in degrees, either half may turn from its start.
	swing float64
}

// majorDistance returns the signed distance in km from every working cell
// center to the major coast: positive on the land side. The major coast is
// the working mask's coast after waters smaller than an ocean are turned to
// land and then landmasses smaller than a mainland to water.
func majorDistance(ctx context.Context, p PlacementParams, c *Coast) ([]float64, error) {
	mask := slices.Clone(c.Land)
	cellKM2 := c.CellWidthKM * c.CellHeightKM
	for kind, least := range [2]float64{p.OceanKM2, p.MainlandKM2} {
		r := labelComponents(mask, c.Columns, c.Rows)
		for i, v := range mask {
			if int(v) == kind && float64(r.cells[r.label[i]])*cellKM2 < least {
				mask[i] = 1 - v
			}
		}
	}
	return coastDistance(ctx, mask, c.Columns, c.Rows, c.CellWidthKM, c.CellHeightKM)
}

// inland returns the side of a backbone that lies farther from the major
// coast.
func (pl *placer) inland(b []Sample) Side {
	sum := 0.0
	for i, s := range b {
		a, z := b[max(i-1, 0)], b[min(i+1, len(b)-1)]
		tx, ty := z.X-a.X, z.Y-a.Y
		l := math.Hypot(tx, ty)
		// Right of the direction of travel is (-ty, tx); see Side.
		nx, ny := -ty/l*s.WidthKM/2, tx/l*s.WidthKM/2
		sum += pl.coast.sample(pl.major, Point{s.X + nx, s.Y + ny}) - pl.coast.sample(pl.major, Point{s.X - nx, s.Y - ny})
	}
	if sum > 0 {
		return Right
	}
	return Left
}

// placer holds the state of one automatic placement run.
type placer struct {
	p        PlacementParams
	seed     uint64
	dom      Domain
	coast    *Coast
	major    []float64 // working grid: signed distance to the major coast
	occupied []uint32  // working grid: the feature covering each cell, or 0
	features []Feature
	diag     *Diagnostics
}

// place runs automatic placement and returns the features in placement
// order: collision belts, each followed by its plateau and foreland basin,
// then active margins, each followed by its inland basin if it has one, old
// orogens, rifts, island arcs, and faults. Larger and more constrained
// events choose first. Requests that cannot be placed are recorded in diag
// as omissions.
func place(ctx context.Context, seed uint64, p PlacementParams, dom Domain, coast *Coast, landKM2 float64, diag *Diagnostics) ([]Feature, error) {
	pl := &placer{p: p, seed: seed, dom: dom, coast: coast, occupied: make([]uint32, len(coast.Land)), features: []Feature{}, diag: diag}
	var err error
	if pl.major, err = majorDistance(ctx, p, coast); err != nil {
		return nil, err
	}
	diag.Requested = map[string]int{}
	for _, fam := range []struct {
		name string
		fp   FamilyParams
	}{{FamilyCollision, p.Collision}, {FamilyActiveMargin, p.ActiveMargin}, {FamilyOldOrogen, p.OldOrogen},
		{FamilyRift, p.Rift}, {FamilyIslandArc, p.IslandArc}, {FamilyFault, p.Fault}} {
		n := max(fam.fp.Minimum, int(math.Round(landKM2/1e6/fam.fp.AreaPerEvent)))
		diag.Requested[fam.name] = n
		requests := prng.New(seed, DomainRequests, familyNumbers[fam.name])
		for i := range n {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			// Every request draws the same number of attributes, whatever
			// its family uses.
			var u [8]float64
			for k := range u {
				u[k] = requests.Float64()
			}
			pl.request(fam.name, fam.fp, i, u)
		}
	}
	return pl.features, nil
}

func lerp(r [2]float64, u float64) float64 { return r[0] + u*(r[1]-r[0]) }

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }

// request tries to place one event and whatever belongs to it.
func (pl *placer) request(family string, fp FamilyParams, index int, u [8]float64) {
	rng := prng.New(pl.seed, DomainPlace+family, uint64(index))
	key := rng.Uint64()
	length := lerp(fp.LengthKM, u[0])
	// The course, and the anchors: working cells a backbone may start in.
	c, p := pl.coast, pl.p
	co := course{land: true, swing: p.MaxSwingDeg}
	near := max(c.CellWidthKM, c.CellHeightKM) / 2
	eligible := func(i int) bool { return c.Distance[i] >= fp.WidthKM[0]/2 && pl.occupied[i] == 0 }
	switch family {
	case FamilyActiveMargin:
		co.width, co.field, co.swing = lerp(fp.WidthKM, u[1]), pl.major, p.CoastSwingDeg
		co.target = lerp([2]float64{max(p.MarginOffsetKM[0], co.width/2), max(p.MarginOffsetKM[1], co.width/2)}, u[3])
		eligible = func(i int) bool {
			return c.Distance[i] > 0 && math.Abs(pl.major[i]-co.target) <= near && pl.occupied[i] == 0
		}
	case FamilyIslandArc:
		// An arc is a gentle curve: it swings no farther than an inland belt.
		co = course{width: lerp(fp.WidthKM, u[1]), field: pl.major, target: -lerp(p.ArcOffsetKM, u[3]), swing: p.MaxSwingDeg}
		eligible = func(i int) bool { return math.Abs(pl.major[i]-co.target) <= near && pl.occupied[i] == 0 }
	case FamilyFault:
		co.width, co.free, co.swing = lerp(fp.WidthKM, u[1]), true, p.FaultSwingDeg
		eligible = func(i int) bool { return c.Distance[i] >= 50 }
	}
	var anchors []int
	for i := range c.Distance {
		if eligible(i) {
			anchors = append(anchors, i)
		}
	}
	reasons := map[string]int{}
	var backbone []Sample
	for range pl.p.Attempts {
		if len(anchors) == 0 {
			reasons[reasonNoRoom] = pl.p.Attempts
			break
		}
		var reason string
		if backbone, reason = pl.backbone(rng, fp, co, anchors, length, u[1]); backbone != nil {
			break
		}
		reasons[reason]++
	}
	if backbone == nil {
		var parts []string
		for _, r := range slices.Sorted(maps.Keys(reasons)) {
			parts = append(parts, fmt.Sprintf("%s %d", r, reasons[r]))
		}
		pl.diag.Omitted = append(pl.diag.Omitted, fmt.Sprintf("%s %d: not placed in %d attempts (%s)", family, index+1, pl.p.Attempts, strings.Join(parts, ", ")))
		return
	}
	f := Feature{ID: uint32(len(pl.features) + 1), Key: &key, Backbone: backbone}
	switch family {
	case FamilyIslandArc:
		f.Type = TypeIslandArc
		f.Targets = &Targets{RegionalElevationM: &[2]float64{-1500, 500}, ReliefM: &[2]float64{1000, 3000}}
	case FamilyFault:
		f.Type, f.Sense = TypeFault, "thrust"
		if u[2] < p.FaultSense[0] {
			f.Sense = "strike_slip"
		} else if u[2] < p.FaultSense[0]+p.FaultSense[1] {
			f.Sense = "normal"
		}
	case FamilyActiveMargin:
		// The range stands highest on its inland flank.
		f.Type, f.Kind, f.Maturity = TypeMountainBelt, "active_margin", new(round3(lerp(*fp.Maturity, u[2])))
		f.ElevatedFlank = pl.inland(backbone).String()
		f.Targets = &Targets{RegionalElevationM: &[2]float64{1000, 2500}, ReliefM: &[2]float64{2000, 4000}}
	case FamilyRift:
		f.Type = TypeRift
		f.AxisFraction = new(round3(lerp(pl.p.AxisFraction, u[2])))
		f.Targets = &Targets{RegionalElevationM: &[2]float64{300, 1500}, ReliefM: &[2]float64{500, 2000}}
	case FamilyOldOrogen:
		m := round3(lerp(*fp.Maturity, u[2]))
		f.Type, f.Kind, f.Maturity, f.ElevatedFlank = TypeMountainBelt, "old_orogen", &m, "none"
		// The most mature belts are worn lowest.
		worn := (m - fp.Maturity[0]) / (fp.Maturity[1] - fp.Maturity[0])
		f.Targets = &Targets{RegionalElevationM: &[2]float64{300, 1200}, ReliefM: &[2]float64{500, math.Round(2500 - 1500*worn)}}
	case FamilyCollision:
		f.Type, f.Kind, f.Maturity = TypeMountainBelt, "collision", new(round3(lerp(*fp.Maturity, u[2])))
		f.ElevatedFlank = Left.String()
		if u[3] >= 0.5 {
			f.ElevatedFlank = Right.String()
		}
		f.Targets = &Targets{RegionalElevationM: &[2]float64{1500, 3000}, ReliefM: &[2]float64{2500, 5000}}
	}
	pl.features = append(pl.features, f)
	if family == FamilyFault {
		return // a fault may cross anything, so it claims no cells
	}
	pl.mark(NewLine(pl.dom, backbone), f.ID)
	if family == FamilyActiveMargin && u[4] < p.MarginBasin {
		side := pl.inland(backbone)
		if basin, edge := pl.flank(backbone, f.ID, side, 0.15, 0.85, lerp(p.ForelandDepth, u[5])*lineWidth(backbone), 0); basin != nil {
			bkey := rng.Uint64()
			pl.add(Feature{Type: TypeBasin, Kind: "foreland", Key: &bkey, Parent: f.ID, Side: side.String(), Outline: basin, EdgeKM: edge,
				Targets: &Targets{RegionalElevationM: &[2]float64{0, 400}, ReliefM: &[2]float64{0, 200}}})
		} else {
			pl.diag.Omitted = append(pl.diag.Omitted, fmt.Sprintf("%s %d: no room for an inland basin", family, index+1))
		}
	}
	if family != FamilyCollision {
		return
	}
	// The plateau takes the drawn flank, or the other if only that has
	// room; the foreland basin takes the flank opposite the elevated one.
	parent := f.ID
	width := lineWidth(backbone)
	sides := []Side{Left, Right}
	if f.ElevatedFlank == Right.String() {
		sides = []Side{Right, Left}
	}
	var plateau []Point
	var edge float64
	for _, side := range sides {
		if plateau, edge = pl.flank(backbone, parent, side, 0.2, 0.8, lerp(pl.p.PlateauDepth, u[4])*width, 50); plateau != nil {
			pl.features[parent-1].ElevatedFlank = side.String()
			pkey := rng.Uint64()
			pl.add(Feature{Type: TypePlateau, Key: &pkey, Parent: parent, Side: side.String(), Outline: plateau, EdgeKM: edge,
				Targets: &Targets{RegionalElevationM: &[2]float64{2500, 4500}, ReliefM: &[2]float64{200, 800}}})
			break
		}
	}
	if plateau == nil {
		pl.diag.Omitted = append(pl.diag.Omitted, fmt.Sprintf("%s %d: no room for a plateau on either flank", family, index+1))
	}
	low := Left
	if pl.features[parent-1].ElevatedFlank == Left.String() {
		low = Right
	}
	if basin, edge := pl.flank(backbone, parent, low, 0.15, 0.85, lerp(pl.p.ForelandDepth, u[5])*width, 0); basin != nil {
		bkey := rng.Uint64()
		pl.add(Feature{Type: TypeBasin, Kind: "foreland", Key: &bkey, Parent: parent, Side: low.String(), Outline: basin, EdgeKM: edge,
			Targets: &Targets{RegionalElevationM: &[2]float64{0, 400}, ReliefM: &[2]float64{0, 200}}})
	} else {
		pl.diag.Omitted = append(pl.diag.Omitted, fmt.Sprintf("%s %d: no room for a foreland basin", family, index+1))
	}
}

// add appends an area feature and marks the cells it covers.
func (pl *placer) add(f Feature) {
	f.ID = uint32(len(pl.features) + 1)
	pl.features = append(pl.features, f)
	pl.mark(NewRegion(pl.dom, f.Outline, f.EdgeKM), f.ID)
}

// mark records id on every free working cell under the shape.
func (pl *placer) mark(s Shape, id uint32) {
	c := pl.coast
	visit(pl.dom, c.Columns, c.Rows, s.Bounds(), func(col, row int, p Point) {
		if i := row*c.Columns + col; pl.occupied[i] == 0 && s.Contains(p) {
			pl.occupied[i] = id
		}
	})
}

// owner returns the feature covering the working cell that contains p.
func (pl *placer) owner(p Point) uint32 {
	c := pl.coast
	col := (int(math.Floor(p.X/c.CellWidthKM))%c.Columns + c.Columns) % c.Columns
	row := (int(math.Floor(p.Y/c.CellHeightKM))%c.Rows + c.Rows) % c.Rows
	return pl.occupied[row*c.Columns+col]
}

// lineWidth is the greatest width along a backbone.
func lineWidth(b []Sample) float64 {
	w := 0.0
	for _, s := range b {
		w = max(w, s.WidthKM)
	}
	return w
}

// torusDistance is the distance between a and b on the torus.
func torusDistance(dom Domain, a, b Point) float64 {
	dx, dy := math.Abs(math.Mod(a.X-b.X, dom.WidthKM)), math.Abs(math.Mod(a.Y-b.Y, dom.HeightKM))
	return math.Hypot(min(dx, dom.WidthKM-dx), min(dy, dom.HeightKM-dy))
}

// backbone grows one candidate and returns its samples, or nil and the
// reason it was rejected.
//
// The candidate starts at a random anchor with a random heading and grows
// from both ends in steps, to the requested length or until both ends are
// blocked. Its heading drifts smoothly, within a limit either side of the
// heading it started with; when the land ahead is too narrow
// for the belt's full width it turns toward the land farther from the coast.
// An end is blocked where the land is too narrow for the belt's end width,
// where another feature lies, or where the belt would come back on itself.
// The width at the middle is drawn between the family's minimum and what the
// land at the anchor allows, and narrows wherever the coast is closer.
//
// A coastal course differs: the width is fixed, the candidate starts along
// the contour of the course's field through the anchor, and at every step it
// takes the turn that best keeps the field at the course's target. An end is
// also blocked where the field strays from the target by more than the
// drift allowed.
func (pl *placer) backbone(rng *rand.Rand, fp FamilyParams, co course, anchors []int, length, widthU float64) ([]Sample, string) {
	c, p := pl.coast, pl.p
	cell := anchors[rng.IntN(len(anchors))]
	anchor := Point{(float64(cell%c.Columns) + rng.Float64()) * c.CellWidthKM, (float64(cell/c.Columns) + rng.Float64()) * c.CellHeightKM}
	heading := rng.Float64() * 2 * math.Pi
	width := co.width
	if width == 0 {
		limit := min(fp.WidthKM[1], 2*c.DistanceAt(anchor))
		if limit < fp.WidthKM[0] {
			return nil, reasonNarrow
		}
		width = lerp([2]float64{fp.WidthKM[0], limit}, widthU)
	}
	need := p.EndWidth * width / 2
	if co.land && c.DistanceAt(anchor) < need {
		return nil, reasonNarrow
	}
	// The belt keeps clear of itself by its width; a coastal one, which may
	// turn farther, by at least the tightest bend.
	clear := width
	if co.field != nil {
		// Along the contour, one way or the other.
		gx := c.sample(co.field, Point{anchor.X + c.CellWidthKM, anchor.Y}) - c.sample(co.field, Point{anchor.X - c.CellWidthKM, anchor.Y})
		gy := c.sample(co.field, Point{anchor.X, anchor.Y + c.CellHeightKM}) - c.sample(co.field, Point{anchor.X, anchor.Y - c.CellHeightKM})
		if gx == 0 && gy == 0 {
			return nil, reasonNarrow
		}
		if along := math.Atan2(gy*c.CellWidthKM, gx*c.CellHeightKM) + math.Pi/2; heading < math.Pi {
			heading = along
		} else {
			heading = along + math.Pi
		}
		clear = max(width, p.MinBendKM)
	}
	maxTurn := p.StepKM / max(p.BendRadius*width, p.MinBendKM)
	// Points nearer than this along the backbone are not a return on itself.
	skip := int(math.Ceil(1.5*max(p.BendRadius*width, p.MinBendKM)/p.StepKM)) + 1

	type tip struct {
		pts     []Point // outward from the anchor, which is not included
		at      Point
		heading float64 // current
		origin  float64 // at the anchor
		turn    float64
		dead    bool
	}
	tips := [2]*tip{{at: anchor, heading: heading, origin: heading}, {at: anchor, heading: heading + math.Pi, origin: heading + math.Pi}}
	swing := co.swing * math.Pi / 180
	ahead := func(from Point, h, dist float64) Point {
		return Point{from.X + dist*math.Cos(h), from.Y + dist*math.Sin(h)}
	}
	steps := int(math.Round(length / p.StepKM))
	for n := 0; n < steps && !(tips[0].dead && tips[1].dead); {
		for k, t := range tips {
			if t.dead || n >= steps {
				continue
			}
			bound := func(h float64) float64 { return min(max(h, t.origin-swing), t.origin+swing) }
			var h float64
			if co.field != nil {
				best := math.Inf(1)
				for _, k := range [5]float64{0, -0.5, 0.5, -1, 1} {
					a := bound(t.heading + k*maxTurn)
					if e := math.Abs(c.sample(co.field, ahead(t.at, a, 2*p.StepKM)) - co.target); e < best {
						best, h = e, a
					}
				}
			} else {
				t.turn = min(max(0.8*t.turn+0.35*maxTurn*rng.NormFloat64(), -maxTurn), maxTurn)
				h = bound(t.heading + t.turn)
				if best := c.DistanceAt(ahead(t.at, h, 3*p.StepKM)); best < width/2 {
					for _, alt := range [2]float64{-maxTurn, maxTurn} {
						a := bound(t.heading + alt)
						if d := c.DistanceAt(ahead(t.at, a, 3*p.StepKM)); d > best {
							best, h = d, a
						}
					}
				}
				t.turn = h - t.heading
			}
			next := ahead(t.at, h, p.StepKM)
			ok := co.free || pl.owner(next) == 0
			if co.land {
				ok = ok && c.DistanceAt(next) >= need
			}
			if co.field != nil {
				ok = ok && math.Abs(c.sample(co.field, next)-co.target) <= p.DriftKM
			}
			// The belt must not come back on itself, on either arm.
			other := tips[1-k]
			for j := 0; ok && j < len(t.pts)-skip; j++ {
				ok = torusDistance(pl.dom, next, t.pts[j]) > clear
			}
			for j := max(0, skip-len(t.pts)-1); ok && j < len(other.pts); j++ {
				ok = torusDistance(pl.dom, next, other.pts[j]) > clear
			}
			if !ok {
				t.dead = true
				continue
			}
			t.pts, t.at, t.heading = append(t.pts, next), next, h
			n++
		}
	}
	pts := slices.Clone(tips[1].pts)
	slices.Reverse(pts)
	pts = append(append(pts, anchor), tips[0].pts...)
	if float64(len(pts)-1)*p.StepKM < fp.LengthKM[0] {
		return nil, reasonShort
	}
	pts = resample(smoothOpen(pts, 2), p.SampleKM)
	// Width: widest in the middle, never wider than the land, never below
	// the end width.
	widths := make([]float64, len(pts))
	for i, q := range pts {
		taper := p.EndWidth + (1-p.EndWidth)*math.Sin(math.Pi*float64(i)/float64(len(pts)-1))
		widths[i] = width * taper
		if co.land {
			widths[i] = max(min(widths[i], 2*c.DistanceAt(q)), p.EndWidth*width)
		}
	}
	// Store geometry with the first vertex in the base domain.
	ox, oy := math.Floor(pts[0].X/pl.dom.WidthKM)*pl.dom.WidthKM, math.Floor(pts[0].Y/pl.dom.HeightKM)*pl.dom.HeightKM
	out := make([]Sample, len(pts))
	for i, q := range pts {
		w := widths[i]
		if i > 0 && i < len(pts)-1 {
			w = (widths[i-1] + 2*w + widths[i+1]) / 4
		}
		out[i] = Sample{round3(q.X - ox), round3(q.Y - oy), round3(w)}
		pts[i] = Point{out[i].X, out[i].Y}
	}
	for i := 0; i+1 < len(pts); i++ {
		for j := i + 2; j+1 < len(pts); j++ {
			if segmentsCross(pts[i], pts[i+1], pts[j], pts[j+1], false) {
				return nil, reasonCrossing
			}
		}
	}
	if co.free {
		return out, ""
	}
	// The whole envelope, widened by the gap, must be free.
	wide := slices.Clone(out)
	for i := range wide {
		wide[i].WidthKM += 2 * p.GapKM
	}
	test := NewLine(pl.dom, wide)
	free := true
	visit(pl.dom, c.Columns, c.Rows, test.Bounds(), func(col, row int, q Point) {
		if free && pl.occupied[row*c.Columns+col] != 0 && test.Contains(q) {
			free = false
		}
	})
	if !free {
		return nil, reasonOverlap
	}
	return out, ""
}

// smoothOpen applies passes of a 1-2-1 filter to a polyline, keeping its
// end points.
func smoothOpen(pts []Point, passes int) []Point {
	next := slices.Clone(pts)
	for range passes {
		for k := 1; k+1 < len(pts); k++ {
			next[k] = Point{(pts[k-1].X + 2*pts[k].X + pts[k+1].X) / 4, (pts[k-1].Y + 2*pts[k].Y + pts[k+1].Y) / 4}
		}
		pts, next = next, pts
		copy(next, pts)
	}
	return pts
}

// resample returns points evenly spaced along a polyline, about spacing km
// apart, including both ends.
func resample(pts []Point, spacing float64) []Point {
	cum := make([]float64, len(pts))
	for i := 1; i < len(pts); i++ {
		cum[i] = cum[i-1] + math.Hypot(pts[i].X-pts[i-1].X, pts[i].Y-pts[i-1].Y)
	}
	total := cum[len(cum)-1]
	n := max(1, int(math.Round(total/spacing)))
	out := make([]Point, 0, n+1)
	j := 1
	for i := range n {
		s := total * float64(i) / float64(n)
		for cum[j] < s {
			j++
		}
		t := (s - cum[j-1]) / (cum[j] - cum[j-1])
		out = append(out, Point{pts[j-1].X + t*(pts[j].X-pts[j-1].X), pts[j-1].Y + t*(pts[j].Y-pts[j-1].Y)})
	}
	return append(out, pts[len(pts)-1])
}

// flank builds the outline of an area beside a belt: on the given side,
// along the part of the backbone between fractions from and to of its
// samples, reaching depth km beyond the belt's edge at its deepest. It
// returns nil when there is no room.
//
// The inner edge runs inside the belt so the area and the belt blend. The
// outer edge is pulled in wherever it would come within minCoast km of the
// coast, cross the coast, or reach another feature. The result may cover
// only its parent's cells and free ones.
func (pl *placer) flank(b []Sample, parent uint32, side Side, from, to, depth, minCoast float64) ([]Point, float64) {
	c := pl.coast
	i0, i1 := int(math.Ceil(from*float64(len(b)-1))), int(math.Floor(to*float64(len(b)-1)))
	if i1-i0 < 2 {
		return nil, 0
	}
	n := i1 - i0 + 1
	inner, normal, reach := make([]Point, n), make([]Point, n), make([]float64, n)
	for k := range n {
		i := i0 + k
		a, z := b[max(i-1, 0)], b[min(i+1, len(b)-1)]
		tx, ty := z.X-a.X, z.Y-a.Y
		l := math.Hypot(tx, ty)
		// Right of the direction of travel is (-ty, tx); see Side.
		normal[k] = Point{-ty / l, tx / l}
		if side == Left {
			normal[k] = Point{ty / l, -tx / l}
		}
		inner[k] = Point{b[i].X + 0.35*b[i].WidthKM*normal[k].X, b[i].Y + 0.35*b[i].WidthKM*normal[k].Y}
		// Blunt ends, deepest in the middle.
		want := depth * (0.35 + 0.65*math.Sin(math.Pi*float64(k)/float64(n-1)))
		for _, share := range [4]float64{1, 0.75, 0.5, 0.25} {
			off := b[i].WidthKM/2 + share*want
			q := Point{b[i].X + off*normal[k].X, b[i].Y + off*normal[k].Y}
			if o := pl.owner(q); c.DistanceAt(q) >= minCoast && (o == 0 || o == parent) {
				reach[k] = share * want
				break
			}
		}
	}
	// Smooth the reach without letting it grow past what was found to fit.
	mean := 0.0
	smooth := make([]float64, n)
	for k := range n {
		smooth[k] = min(reach[k], (reach[max(k-1, 0)]+2*reach[k]+reach[min(k+1, n-1)])/4)
		mean += smooth[k] / float64(n)
	}
	if mean < 50 {
		return nil, 0
	}
	outline := make([]Point, 0, 2*n)
	outline = append(outline, inner...)
	for k := n - 1; k >= 0; k-- {
		off := b[i0+k].WidthKM/2 + smooth[k]
		outline = append(outline, Point{b[i0+k].X + off*normal[k].X, b[i0+k].Y + off*normal[k].Y})
	}
	ox, oy := math.Floor(outline[0].X/pl.dom.WidthKM)*pl.dom.WidthKM, math.Floor(outline[0].Y/pl.dom.HeightKM)*pl.dom.HeightKM
	for k, q := range outline {
		outline[k] = Point{round3(q.X - ox), round3(q.Y - oy)}
	}
	if validateOutline(pl.dom, outline) != nil {
		return nil, 0
	}
	edge := round3(min(max(0.2*mean, 20), 100))
	region := NewRegion(pl.dom, outline, edge)
	box := region.Bounds()
	gap := pl.p.GapKM
	free := true
	visit(pl.dom, c.Columns, c.Rows, Rect{box.MinX - gap, box.MinY - gap, box.MaxX + gap, box.MaxY + gap}, func(col, row int, q Point) {
		if o := pl.occupied[row*c.Columns+col]; free && o != 0 && o != parent && region.Inside(q) >= -gap {
			free = false
		}
	})
	if !free {
		return nil, 0
	}
	return outline, edge
}

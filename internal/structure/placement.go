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
	PlacementAlgorithm = "placement-v2"
	ModeAutomatic      = "automatic"
	DomainRequests     = "structure-v1/requests"
	DomainPlace        = "structure-v1/place/" // followed by the family name

	FamilyCollision    = "collision"
	FamilyActiveMargin = "active_margin"
	FamilyOldOrogen    = "old_orogen"
	FamilyRift         = "rift"
	FamilyIslandArc    = "island_arc"
	FamilyFault        = "fault"
	// FamilyInteriorBasin places interior basins: area features with no
	// parent, so not grown from a backbone.
	FamilyInteriorBasin = "interior_basin"
)

// familyNumbers are the sequence numbers of each family's request stream.
// They are fixed, so adding a family does not disturb the others.
var familyNumbers = map[string]uint64{FamilyCollision: 0, FamilyActiveMargin: 1, FamilyOldOrogen: 2, FamilyRift: 3, FamilyIslandArc: 4, FamilyFault: 5, FamilyInteriorBasin: 6}

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
	FaultSwingDeg float64    `json:"fault_swing_deg"`
	FaultSense    [2]float64 `json:"fault_sense"`
	// InterruptedShare is the share of old orogens cut into segments, with
	// InterruptGapKM of clear ground between the envelopes of neighbors.
	InterruptedShare float64    `json:"interrupted_share"`
	InterruptGapKM   [2]float64 `json:"interrupt_gap_km"`
	// IntermontaneShare is the share of collision belts and active margins
	// at least IntermontaneWidthKM wide given an intermontane basin.
	IntermontaneShare   float64 `json:"intermontane_share"`
	IntermontaneWidthKM float64 `json:"intermontane_width_km"`
	// An interior basin keeps InteriorCoastKM from the provisional coast.
	InteriorCoastKM float64      `json:"interior_coast_km"`
	Collision       FamilyParams `json:"collision"`
	ActiveMargin    FamilyParams `json:"active_margin"`
	OldOrogen       FamilyParams `json:"old_orogen"`
	Rift            FamilyParams `json:"rift"`
	IslandArc       FamilyParams `json:"island_arc"`
	Fault           FamilyParams `json:"fault"`
	// InteriorBasin's LengthKM and WidthKM are the ranges of a basin's long
	// and short axes.
	InteriorBasin FamilyParams `json:"interior_basin"`
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
		InterruptedShare: 0.5, InterruptGapKM: [2]float64{100, 300}, IntermontaneShare: 0.5, IntermontaneWidthKM: 250, InteriorCoastKM: 100,
		Collision:     FamilyParams{AreaPerEvent: 35, Minimum: 1, LengthKM: [2]float64{1500, 5000}, WidthKM: [2]float64{300, 1200}, Maturity: &[2]float64{0, 0.3}},
		ActiveMargin:  FamilyParams{AreaPerEvent: 30, Minimum: 1, LengthKM: [2]float64{1500, 6000}, WidthKM: [2]float64{100, 400}, Maturity: &[2]float64{0, 0.4}},
		OldOrogen:     FamilyParams{AreaPerEvent: 20, Minimum: 1, LengthKM: [2]float64{1000, 3500}, WidthKM: [2]float64{100, 600}, Maturity: &[2]float64{0.6, 1}},
		Rift:          FamilyParams{AreaPerEvent: 45, Minimum: 0, LengthKM: [2]float64{800, 3000}, WidthKM: [2]float64{50, 300}},
		IslandArc:     FamilyParams{AreaPerEvent: 50, Minimum: 0, LengthKM: [2]float64{600, 2500}, WidthKM: [2]float64{30, 150}},
		Fault:         FamilyParams{AreaPerEvent: 25, Minimum: 1, LengthKM: [2]float64{300, 2000}, WidthKM: [2]float64{20, 100}},
		InteriorBasin: FamilyParams{AreaPerEvent: 25, Minimum: 1, LengthKM: [2]float64{400, 1500}, WidthKM: [2]float64{300, 1000}},
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
// orogens, an interrupted one as consecutive segments, rifts, island arcs,
// interior basins, and faults. A collision belt or active margin with an
// intermontane basin is followed by it after its other basins. Larger and
// more constrained events choose first. Requests that cannot be placed are recorded in diag
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
		{FamilyRift, p.Rift}, {FamilyIslandArc, p.IslandArc}, {FamilyInteriorBasin, p.InteriorBasin}, {FamilyFault, p.Fault}} {
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
			if fam.name == FamilyInteriorBasin {
				pl.interior(fam.fp, i, u)
			} else {
				pl.request(fam.name, fam.fp, i, u)
			}
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
		pl.omit(family, index, reasons)
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
	if family == FamilyOldOrogen && u[3] < p.InterruptedShare {
		if segments := pl.interrupt(backbone, u); segments != nil {
			// The segments after the first name it as their parent.
			first := f.ID
			for k, seg := range segments {
				g := f
				g.ID, g.Backbone = uint32(len(pl.features)+1), seg
				if k > 0 {
					skey := rng.Uint64()
					g.Key, g.Parent = &skey, first
				}
				pl.features = append(pl.features, g)
				pl.mark(NewLine(pl.dom, seg), g.ID)
			}
			return
		}
	}
	pl.features = append(pl.features, f)
	if family == FamilyFault {
		return // a fault may cross anything, so it claims no cells
	}
	pl.mark(NewLine(pl.dom, backbone), f.ID)
	// An intermontane basin is drawn after the belt's other basins, so their
	// keys do not depend on it.
	defer func() {
		if family != FamilyCollision && family != FamilyActiveMargin || u[6] >= p.IntermontaneShare || lineWidth(backbone) < p.IntermontaneWidthKM {
			return
		}
		side := Left
		if u[7] >= 0.5 {
			side = Right
		}
		if basin, edge := pl.within(backbone, f.ID, side); basin != nil {
			ikey := rng.Uint64()
			pl.add(Feature{Type: TypeBasin, Kind: "intermontane", Key: &ikey, Parent: f.ID, Side: side.String(), Outline: basin, EdgeKM: edge,
				Targets: &Targets{RegionalElevationM: &[2]float64{500, 2000}, ReliefM: &[2]float64{0, 400}}})
		} else {
			pl.diag.Omitted = append(pl.diag.Omitted, fmt.Sprintf("%s %d: no room for an intermontane basin", family, index+1))
		}
	}()
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
		normal[k] = sideNormal(b, i, side)
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
	outline = toBase(pl.dom, outline)
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

// sideNormal returns the unit normal at sample i of a backbone, pointing to
// the given side.
func sideNormal(b []Sample, i int, side Side) Point {
	a, z := b[max(i-1, 0)], b[min(i+1, len(b)-1)]
	tx, ty := z.X-a.X, z.Y-a.Y
	l := math.Hypot(tx, ty)
	// Right of the direction of travel is (-ty, tx); see Side.
	if side == Left {
		return Point{ty / l, -tx / l}
	}
	return Point{-ty / l, tx / l}
}

// toBase moves unwrapped points by whole world widths and heights so the
// first lies in the base domain, rounding them as saved geometry is.
func toBase(dom Domain, pts []Point) []Point {
	ox, oy := math.Floor(pts[0].X/dom.WidthKM)*dom.WidthKM, math.Floor(pts[0].Y/dom.HeightKM)*dom.HeightKM
	out := make([]Point, len(pts))
	for k, q := range pts {
		out[k] = Point{round3(q.X - ox), round3(q.Y - oy)}
	}
	return out
}

// related reports whether the working cell owner o may lie under an area
// that belongs to parent: o is free, the parent, or another of its children.
func (pl *placer) related(o, parent uint32) bool {
	return o == 0 || o == parent || pl.features[o-1].Parent == parent
}

// minSegment is the fewest samples a segment of an interrupted belt keeps.
const minSegment = 4

// interrupt cuts an old orogen's backbone into two or three segments with
// clear ground between them. Each gap is centered at a drawn fraction of the
// samples and widened until the straight distance between the neighboring
// segment ends, less their half widths, is at least the drawn gap. The width
// at a cut tapers to the belt's end width over two samples. interrupt returns
// nil when a segment would keep fewer than minSegment samples; the belt then
// stays whole.
func (pl *placer) interrupt(b []Sample, u [8]float64) [][]Sample {
	p := pl.p
	end := p.EndWidth * lineWidth(b)
	gap := lerp(p.InterruptGapKM, u[7])
	centers := []float64{0.35 + 0.3*u[5]}
	if u[4] < 0.5 {
		centers = []float64{0.25 + 0.15*u[5], 0.6 + 0.15*u[6]}
	}
	clear := func(a, z int) float64 { return math.Hypot(b[z].X-b[a].X, b[z].Y-b[a].Y) - end }
	n := len(b)
	var cuts [][2]int
	start := 0
	for _, f := range centers {
		c := int(math.Round(f * float64(n-1)))
		h := 1
		for c-h > start && c+h < n-1 && clear(c-h, c+h) < gap {
			h++
		}
		a, z := c-h, c+h
		if a-start+1 < minSegment || clear(a, z) < gap {
			return nil
		}
		cuts = append(cuts, [2]int{a, z})
		start = z
	}
	if n-start < minSegment {
		return nil
	}
	taper := func(seg []Sample, at, in int) {
		seg[at].WidthKM = round3(min(seg[at].WidthKM, end))
		seg[in].WidthKM = round3(min(seg[in].WidthKM, (seg[in].WidthKM+end)/2))
	}
	segs := make([][]Sample, 0, len(cuts)+1)
	start = 0
	for k := range len(cuts) + 1 {
		stop := n - 1
		if k < len(cuts) {
			stop = cuts[k][0]
		}
		seg := slices.Clone(b[start : stop+1])
		if k > 0 {
			taper(seg, 0, 1)
		}
		if k < len(cuts) {
			taper(seg, len(seg)-1, len(seg)-2)
			start = cuts[k][1]
		}
		pts := make([]Point, len(seg))
		for i, s := range seg {
			pts[i] = Point{s.X, s.Y}
		}
		for i, q := range toBase(pl.dom, pts) {
			seg[i].X, seg[i].Y = q.X, q.Y
		}
		segs = append(segs, seg)
	}
	return segs
}

// within builds the outline of an intermontane basin inside a belt's
// envelope: on the given side, along the middle half of the belt, from 15
// percent of the half width out from the backbone to as much as 75 percent,
// reaching farthest in the middle. The outer edge is pulled toward the
// backbone wherever it would leave the provisional land or reach a feature
// other than the belt and its other children. within returns nil when the
// basin is on average less than 40 km across or comes within the gap of an
// unrelated feature.
func (pl *placer) within(b []Sample, parent uint32, side Side) ([]Point, float64) {
	c := pl.coast
	i0, i1 := int(math.Ceil(0.25*float64(len(b)-1))), int(math.Floor(0.75*float64(len(b)-1)))
	if i1-i0 < 2 {
		return nil, 0
	}
	n := i1 - i0 + 1
	inner, normal, reach := make([]Point, n), make([]Point, n), make([]float64, n)
	for k := range n {
		i := i0 + k
		normal[k] = sideNormal(b, i, side)
		half := b[i].WidthKM / 2
		inner[k] = Point{b[i].X + 0.15*half*normal[k].X, b[i].Y + 0.15*half*normal[k].Y}
		want := 0.6 * half * (0.35 + 0.65*math.Sin(math.Pi*float64(k)/float64(n-1)))
		for _, share := range [3]float64{1, 0.75, 0.5} {
			off := 0.15*half + share*want
			q := Point{b[i].X + off*normal[k].X, b[i].Y + off*normal[k].Y}
			if c.DistanceAt(q) > 0 && pl.related(pl.owner(q), parent) {
				reach[k] = share * want
				break
			}
		}
	}
	mean := 0.0
	smooth := make([]float64, n)
	for k := range n {
		smooth[k] = min(reach[k], (reach[max(k-1, 0)]+2*reach[k]+reach[min(k+1, n-1)])/4)
		mean += smooth[k] / float64(n)
	}
	if mean < 40 {
		return nil, 0
	}
	outline := make([]Point, 0, 2*n)
	outline = append(outline, inner...)
	for k := n - 1; k >= 0; k-- {
		off := 0.15*b[i0+k].WidthKM/2 + smooth[k]
		outline = append(outline, Point{b[i0+k].X + off*normal[k].X, b[i0+k].Y + off*normal[k].Y})
	}
	outline = toBase(pl.dom, outline)
	if validateOutline(pl.dom, outline) != nil {
		return nil, 0
	}
	edge := round3(min(max(0.3*mean, 10), 60))
	if !pl.clearOf(NewRegion(pl.dom, outline, edge), parent) {
		return nil, 0
	}
	return outline, edge
}

// clearOf reports whether no feature other than parent and its children
// lies within the gap of the region. With parent 0, no feature may.
func (pl *placer) clearOf(region *Region, parent uint32) bool {
	c := pl.coast
	box := region.Bounds()
	gap := pl.p.GapKM
	free := true
	visit(pl.dom, c.Columns, c.Rows, Rect{box.MinX - gap, box.MinY - gap, box.MaxX + gap, box.MaxY + gap}, func(col, row int, q Point) {
		if o := pl.occupied[row*c.Columns+col]; free && o != 0 && (parent == 0 || !pl.related(o, parent)) && region.Inside(q) >= -gap {
			free = false
		}
	})
	return free
}

// interiorVertices is the number of vertices in an interior basin's outline.
const interiorVertices = 32

// interior tries to place one interior basin: an area feature with no
// parent, a broad sag in a continental interior.
//
// The basin is an ellipse with its long and short axes drawn from the
// family's ranges, a random orientation, and a gentle wobble of its radius.
// It is centered in a random free working cell at least half its short axis
// inland. Each vertex is pulled toward the center, in steps down to 40
// percent of its radius, until it lies at least InteriorCoastKM inland and
// on a free cell. A candidate is rejected when a vertex finds no such place,
// when its mean radius falls below 60 percent of the ellipse's, or when it
// comes within the gap of another feature.
func (pl *placer) interior(fp FamilyParams, index int, u [8]float64) {
	rng := prng.New(pl.seed, DomainPlace+FamilyInteriorBasin, uint64(index))
	key := rng.Uint64()
	c, p := pl.coast, pl.p
	long := lerp(fp.LengthKM, u[0])
	short := min(lerp(fp.WidthKM, u[1]), long)
	nominal := (long + short) / 4
	var anchors []int
	for i := range c.Distance {
		if c.Distance[i] >= short/2 && pl.occupied[i] == 0 {
			anchors = append(anchors, i)
		}
	}
	reasons := map[string]int{}
	var outline []Point
	var edge float64
	for range p.Attempts {
		if len(anchors) == 0 {
			reasons[reasonNoRoom] = p.Attempts
			break
		}
		cell := anchors[rng.IntN(len(anchors))]
		center := Point{(float64(cell%c.Columns) + rng.Float64()) * c.CellWidthKM, (float64(cell/c.Columns) + rng.Float64()) * c.CellHeightKM}
		turn := rng.Float64() * math.Pi
		var amp, phase [2]float64
		for k := range amp {
			amp[k], phase[k] = 0.08*rng.Float64(), 2*math.Pi*rng.Float64()
		}
		var reach [interiorVertices]float64
		fits := true
		for k := range reach {
			t := 2 * math.Pi * float64(k) / interiorVertices
			ca, sa := math.Cos(t-turn), math.Sin(t-turn)
			r := (long / 2) * (short / 2) / math.Hypot(short/2*ca, long/2*sa)
			r *= 1 + amp[0]*math.Cos(2*t+phase[0]) + amp[1]*math.Cos(3*t+phase[1])
			for _, share := range [5]float64{1, 0.85, 0.7, 0.55, 0.4} {
				q := Point{center.X + share*r*math.Cos(t), center.Y + share*r*math.Sin(t)}
				if c.DistanceAt(q) >= p.InteriorCoastKM && pl.owner(q) == 0 {
					reach[k] = share * r
					break
				}
			}
			fits = fits && reach[k] > 0
		}
		if !fits {
			reasons[reasonNarrow]++
			continue
		}
		mean := 0.0
		pts := make([]Point, interiorVertices)
		for k := range reach {
			// Smooth around the ring without growing past what fit.
			r := min(reach[k], (reach[(k+interiorVertices-1)%interiorVertices]+2*reach[k]+reach[(k+1)%interiorVertices])/4)
			mean += r / interiorVertices
			t := 2 * math.Pi * float64(k) / interiorVertices
			pts[k] = Point{center.X + r*math.Cos(t), center.Y + r*math.Sin(t)}
		}
		if mean < 0.6*nominal {
			reasons[reasonNarrow]++
			continue
		}
		pts = toBase(pl.dom, pts)
		if validateOutline(pl.dom, pts) != nil {
			reasons[reasonCrossing]++
			continue
		}
		e := round3(min(max(0.4*mean, 50), 250))
		if !pl.clearOf(NewRegion(pl.dom, pts, e), 0) {
			reasons[reasonOverlap]++
			continue
		}
		outline, edge = pts, e
		break
	}
	if outline == nil {
		pl.omit(FamilyInteriorBasin, index, reasons)
		return
	}
	pl.add(Feature{Type: TypeBasin, Kind: "interior", Key: &key, Outline: outline, EdgeKM: edge,
		Targets: &Targets{RegionalElevationM: &[2]float64{0, 300}, ReliefM: &[2]float64{0, 150}}})
}

// omit records a request that was not placed, with a count of each reason
// its candidates were rejected.
func (pl *placer) omit(family string, index int, reasons map[string]int) {
	var parts []string
	for _, r := range slices.Sorted(maps.Keys(reasons)) {
		parts = append(parts, fmt.Sprintf("%s %d", r, reasons[r]))
	}
	pl.diag.Omitted = append(pl.diag.Omitted, fmt.Sprintf("%s %d: not placed in %d attempts (%s)", family, index+1, pl.p.Attempts, strings.Join(parts, ", ")))
}

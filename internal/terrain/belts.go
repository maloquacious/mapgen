// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package terrain

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"

	"github.com/maloquacious/mapgen/internal/continent"
	"github.com/maloquacious/mapgen/internal/prng"
	"github.com/maloquacious/mapgen/internal/structure"
	"github.com/maloquacious/mapgen/internal/trench"
)

// Belt components: ridge chains, branches, longitudinal troughs, corridors
// with their saddles, and volcanic peaks. See "Belt components" in the
// design.
const BeltAlgorithm = "belts-v1"

// BeltParams are the calibration values behind belt components, island arc
// peaks, and trench depressions. They are recorded in the manifest. Shares
// of a half width are of the belt's half width at that point.
type BeltParams struct {
	Algorithm string `json:"algorithm"`
	// StationKM is the spacing of the backbone stations ridges are built
	// from.
	StationKM float64 `json:"station_km"`
	// Chains is the range of the number of ridge chains, by belt kind.
	Chains map[string][2]int `json:"chains"`
	// Chains lie within ChainSpread of the half width of the backbone,
	// evenly spaced and moved by up to ChainJitter of their spacing.
	ChainSpread float64 `json:"chain_spread"`
	ChainJitter float64 `json:"chain_jitter"`
	// A chain's kernel half width is ChainHalf of the chain spacing, times
	// 1 − ValleyWidening × maturity, and at least MinChainHalfKM. Chains,
	// with their kernel and wander, stay within ChainReach of the half
	// width.
	ChainHalf      [2]float64 `json:"chain_half"`
	ValleyWidening float64    `json:"valley_widening"`
	MinChainHalfKM float64    `json:"min_chain_half_km"`
	ChainReach     float64    `json:"chain_reach"`
	// Each chain wanders across the belt by up to Wander of the half width,
	// as the mean of two sinusoids with wavelengths in WanderWavelengthKM.
	Wander             float64    `json:"wander"`
	WanderWavelengthKM [2]float64 `json:"wander_wavelength_km"`
	// The main chain has the full amplitude; the others ChainScale of it,
	// falling with distance from the main chain. The amplitude is the
	// relief draw, pulled toward the bottom of the relief range by
	// MaturityAmplitude × maturity.
	ChainScale        [2]float64 `json:"chain_scale"`
	MaturityAmplitude float64    `json:"maturity_amplitude"`
	// A chain starts and ends up to EndTrim of the backbone's length from
	// its ends, and fades in and out over FadeKM there and at gaps.
	EndTrim [2]float64 `json:"end_trim"`
	FadeKM  [2]float64 `json:"fade_km"`
	// Gaps occur GapsPer1000KM along a chain, from maturity 0 to 1, each
	// GapKM long times 1 + GapMaturity × maturity.
	GapsPer1000KM [2]float64 `json:"gaps_per_1000_km"`
	GapKM         [2]float64 `json:"gap_km"`
	GapMaturity   float64    `json:"gap_maturity"`
	// Branches leave a chain BranchesPer1000KM, from maturity 0 to 1, at
	// BranchAngleDeg from it, outward, reaching BranchReach of the room
	// left to ChainReach, at BranchScale of the chain's amplitude, tapering
	// to nothing. Shorter than MinBranchKM, a branch is not placed.
	BranchesPer1000KM [2]float64 `json:"branches_per_1000_km"`
	BranchAngleDeg    [2]float64 `json:"branch_angle_deg"`
	BranchReach       [2]float64 `json:"branch_reach"`
	BranchScale       [2]float64 `json:"branch_scale"`
	MinBranchKM       float64    `json:"min_branch_km"`
	// A trough lies between two adjacent chains with probability
	// TroughShare, from maturity 0 to 1, over TroughLengthKM, TroughDepthM
	// below the regional surface.
	TroughShare    [2]float64 `json:"trough_share"`
	TroughLengthKM [2]float64 `json:"trough_length_km"`
	TroughDepthM   [2]float64 `json:"trough_depth_m"`
	// Corridors cross a belt every CorridorSpacingKM, up to
	// CorridorAngleDeg from square, CorridorHalfKM wide on each side,
	// lowering every ridge by SaddleShare of its height on their line.
	CorridorSpacingKM [2]float64 `json:"corridor_spacing_km"`
	CorridorAngleDeg  float64    `json:"corridor_angle_deg"`
	CorridorHalfKM    [2]float64 `json:"corridor_half_km"`
	SaddleShare       [2]float64 `json:"saddle_share"`
	// An active margin carries volcanic peaks with probability
	// VolcanicShare, halfway from its backbone to its main chain. Peaks of
	// margins and arcs are PeakSpacingKM apart and PeakDiameterKM across,
	// their heights a share of the relief draw.
	VolcanicShare    float64    `json:"volcanic_share"`
	PeakSpacingKM    [2]float64 `json:"peak_spacing_km"`
	PeakDiameterKM   [2]float64 `json:"peak_diameter_km"`
	ArcPeakHeight    [2]float64 `json:"arc_peak_height"`
	MarginPeakHeight [2]float64 `json:"margin_peak_height"`
	// A trench deepens toward its floor over TrenchFadeKM at each end, or a
	// third of its length if shorter.
	TrenchFadeKM float64 `json:"trench_fade_km"`
	Combination  string  `json:"combination"`
}

// DefaultBeltParams returns the belts-v1 defaults.
func DefaultBeltParams() BeltParams {
	return BeltParams{
		Algorithm: BeltAlgorithm,
		StationKM: 15,
		Chains: map[string][2]int{
			"collision":     {3, 6},
			"active_margin": {2, 4},
			"old_orogen":    {2, 4},
		},
		ChainSpread: 0.6, ChainJitter: 0.2,
		ChainHalf: [2]float64{0.55, 0.8}, ValleyWidening: 0.35, MinChainHalfKM: 12, ChainReach: 0.9,
		Wander: 0.08, WanderWavelengthKM: [2]float64{200, 600},
		ChainScale: [2]float64{0.6, 0.95}, MaturityAmplitude: 0.7,
		EndTrim: [2]float64{0, 0.1}, FadeKM: [2]float64{50, 150},
		GapsPer1000KM: [2]float64{0.3, 2.5}, GapKM: [2]float64{20, 80}, GapMaturity: 2,
		BranchesPer1000KM: [2]float64{1.2, 0.2}, BranchAngleDeg: [2]float64{25, 50},
		BranchReach: [2]float64{0.5, 1}, BranchScale: [2]float64{0.4, 0.7}, MinBranchKM: 30,
		TroughShare: [2]float64{0.3, 0.7}, TroughLengthKM: [2]float64{100, 400}, TroughDepthM: [2]float64{100, 600},
		CorridorSpacingKM: [2]float64{300, 700}, CorridorAngleDeg: 20,
		CorridorHalfKM: [2]float64{15, 40}, SaddleShare: [2]float64{0.6, 0.85},
		VolcanicShare: 0.6, PeakSpacingKM: [2]float64{50, 150}, PeakDiameterKM: [2]float64{10, 40},
		ArcPeakHeight: [2]float64{0.6, 1}, MarginPeakHeight: [2]float64{0.7, 1.1},
		TrenchFadeKM: 100,
		Combination:  "chains, branches, and peaks combine by maximum, scaled down under basins and along corridors, and add to the regional surface; troughs are depressions of the regional surface",
	}
}

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }

// frame answers position queries along an unwrapped backbone.
type frame struct {
	pts  []structure.Sample
	cum  []float64
	line *structure.Line
}

func newFrame(dom structure.Domain, b []structure.Sample) *frame {
	fr := &frame{pts: b, cum: make([]float64, len(b)), line: structure.NewLine(dom, b)}
	for i := 1; i < len(b); i++ {
		fr.cum[i] = fr.cum[i-1] + math.Hypot(b[i].X-b[i-1].X, b[i].Y-b[i-1].Y)
	}
	return fr
}

func (fr *frame) length() float64 { return fr.cum[len(fr.cum)-1] }

// at returns the backbone point at arc length s, clamped to the ends.
func (fr *frame) at(s float64) (float64, float64) {
	s = min(max(s, 0), fr.length())
	i := max(1, min(sortSearch(fr.cum, s), len(fr.cum)-1))
	t := (s - fr.cum[i-1]) / (fr.cum[i] - fr.cum[i-1])
	a, b := fr.pts[i-1], fr.pts[i]
	return a.X + t*(b.X-a.X), a.Y + t*(b.Y-a.Y)
}

// tangent returns the unit direction from the point h km before s to the
// point h km after it, so it turns smoothly through vertices.
func (fr *frame) tangent(s, h float64) (float64, float64) {
	ax, ay := fr.at(s - h)
	bx, by := fr.at(s + h)
	n := math.Hypot(bx-ax, by-ay)
	return (bx - ax) / n, (by - ay) / n
}

func (fr *frame) half(s float64) float64 { return fr.line.WidthAt(s) / 2 }

// sortSearch returns the first index whose value is at least s.
func sortSearch(v []float64, s float64) int {
	i, _ := slices.BinarySearch(v, s)
	return i
}

// leftNormal is the unit normal on the left of direction (tx, ty), viewing
// the map with north up and y increasing southward.
func leftNormal(tx, ty float64) (float64, float64) { return ty, -tx }

// offsetCurve samples the backbone every station km from s0 to s1, moved to
// the left by frac(s) half widths, with full width 2 × halfKM(s). A vertex
// whose step runs backward along the backbone is dropped, which removes the
// loops an offset curve forms inside a tight bend. It returns the samples
// and the backbone position of each.
func offsetCurve(fr *frame, s0, s1, station float64, frac, halfKM func(s float64) float64) ([]structure.Sample, []float64) {
	var pts []structure.Sample
	var at []float64
	n := max(1, int(math.Ceil((s1-s0)/station)))
	for k := range n + 1 {
		s := s0 + (s1-s0)*float64(k)/float64(n)
		x, y := fr.at(s)
		tx, ty := fr.tangent(s, station)
		nx, ny := leftNormal(tx, ty)
		d := frac(s) * fr.half(s)
		p := structure.Sample{X: x + nx*d, Y: y + ny*d, WidthKM: 2 * halfKM(s)}
		if len(pts) > 0 {
			q := pts[len(pts)-1]
			if (p.X-q.X)*tx+(p.Y-q.Y)*ty <= 0.5 {
				continue
			}
		}
		pts, at = append(pts, p), append(at, s)
	}
	return pts, at
}

// toBase rounds samples to the metre and moves them by whole world sizes so
// the first lies in the base domain. It drops samples that round onto
// their predecessor, and keeps keep aligned with the samples.
func toBase(dom structure.Domain, pts []structure.Sample, keep []float64) ([]structure.Sample, []float64) {
	ox, oy := math.Floor(pts[0].X/dom.WidthKM)*dom.WidthKM, math.Floor(pts[0].Y/dom.HeightKM)*dom.HeightKM
	if round3(pts[0].X-ox) >= dom.WidthKM {
		ox += dom.WidthKM
	}
	if round3(pts[0].Y-oy) >= dom.HeightKM {
		oy += dom.HeightKM
	}
	var out []structure.Sample
	var kept []float64
	for i, p := range pts {
		q := structure.Sample{X: round3(p.X - ox), Y: round3(p.Y - oy), WidthKM: round3(p.WidthKM)}
		if len(out) > 0 && q.X == out[len(out)-1].X && q.Y == out[len(out)-1].Y {
			continue
		}
		out = append(out, q)
		if keep != nil {
			kept = append(kept, keep[i])
		}
	}
	return out, kept
}

// wrapPoint moves a point into the base domain and rounds it to the metre.
func wrapPoint(dom structure.Domain, x, y float64) (float64, float64) {
	x, y = x-math.Floor(x/dom.WidthKM)*dom.WidthKM, y-math.Floor(y/dom.HeightKM)*dom.HeightKM
	x, y = round3(x), round3(y)
	if x >= dom.WidthKM {
		x = 0
	}
	if y >= dom.HeightKM {
		y = 0
	}
	return x, y
}

// ridgeShape is what rasterizing one ridge needs beyond its record.
type ridgeShape struct {
	rec  *Ridge
	amp  []float64 // amplitude at each backbone sample, m
	cum  []float64 // arc length at each backbone sample
	fade float64   // fade length at the ends and gaps; 0 for none
}

// ampAt interpolates the amplitude at arc length s.
func (r *ridgeShape) ampAt(s float64) float64 {
	i := max(1, min(sortSearch(r.cum, s), len(r.cum)-1))
	t := min(max((s-r.cum[i-1])/(r.cum[i]-r.cum[i-1]), 0), 1)
	return r.amp[i-1] + t*(r.amp[i]-r.amp[i-1])
}

// along is how fully the ridge is present at arc length s: fading in and
// out at its ends and around its gaps.
func (r *ridgeShape) along(s float64) float64 {
	if r.fade == 0 {
		return 1
	}
	f, l := r.fade, r.cum[len(r.cum)-1]
	v := min(smoothstep(0, f, s), smoothstep(0, f, l-s))
	for _, g := range r.rec.GapsKM {
		v = min(v, 1-smoothstep(g[0]-f, g[0], s)*(1-smoothstep(g[1], g[1]+f, s)))
	}
	return v
}

// components holds every belt component, its records, and what
// rasterizing it needs. The slices run parallel to the records.
type components struct {
	file *RidgeFile
	// Ridges: amplitudes at the record's samples, fade lengths, and the
	// geometry before wrapping, in the parent backbone's frame.
	amps      [][]float64
	fades     []float64
	unwrapped [][]structure.Sample
	// Corridors: the segment before wrapping, in the same frame.
	corridorEnds [][2][2]float64
}

// expo draws an exponential distance with the given mean.
func expo(u, mean float64) float64 { return -math.Log(1-u) * mean }

// buildComponents draws the components of every mountain belt, island arc,
// and active margin, continuing each feature's stream after its targets.
// The order of draws is part of terrain-v1; see "Random keys and streams".
func buildComponents(p BeltParams, dom structure.Domain, plans []*plan) (*components, error) {
	c := &components{file: &RidgeFile{Format: RidgesFormat, Domain: dom, Ridges: []Ridge{}, Troughs: []Trough{}, Corridors: []Corridor{}, Peaks: []Peak{}}}
	for _, pl := range plans {
		switch {
		case pl.f.Type == structure.TypeMountainBelt:
			if err := c.belt(p, dom, pl); err != nil {
				return nil, err
			}
		case pl.f.Type == structure.TypeIslandArc:
			fr := newFrame(dom, pl.f.Backbone)
			c.peaks(p, dom, pl, fr, 0, p.ArcPeakHeight, func(s float64) float64 { return pl.at(pl.relief, s) })
		}
	}
	return c, nil
}

// peaks places volcanic peaks along a backbone at frac half widths to its
// left.
func (c *components) peaks(p BeltParams, dom structure.Domain, pl *plan, fr *frame, frac float64, height [2]float64, relief func(s float64) float64) {
	rng := pl.rng
	l := fr.length()
	for s := rng.Float64() * lerp(p.PeakSpacingKM, 0.5); s < l; s += lerp(p.PeakSpacingKM, rng.Float64()) {
		d, h := lerp(p.PeakDiameterKM, rng.Float64()), lerp(height, rng.Float64())*relief(s)
		x, y := fr.at(s)
		tx, ty := fr.tangent(s, p.StationKM)
		nx, ny := leftNormal(tx, ty)
		x, y = wrapPoint(dom, x+nx*frac*fr.half(s), y+ny*frac*fr.half(s))
		if h <= 0 {
			continue
		}
		c.file.Peaks = append(c.file.Peaks, Peak{ID: uint32(len(c.file.Peaks) + 1), Feature: pl.f.ID, X: x, Y: y, RadiusKM: round3(d / 2), HeightM: round3(h)})
	}
}

func (c *components) belt(p BeltParams, dom structure.Domain, pl *plan) error {
	f, rng := pl.f, pl.rng
	fr := newFrame(dom, f.Backbone)
	l := fr.length()
	mat := *f.Maturity
	// The relief draw, pulled toward the bottom of its range with maturity.
	amp := func(s float64) float64 {
		lo := pl.reliefRange[0]
		return lo + (pl.at(pl.relief, s)-lo)*(1-p.MaturityAmplitude*mat)
	}
	cr := p.Chains[f.Kind]
	n := min(cr[1], cr[0]+int(rng.Float64()*float64(cr[1]-cr[0]+1)))
	spacing := 2 * p.ChainSpread / float64(n)
	hf := lerp(p.ChainHalf, rng.Float64()) * spacing * (1 - p.ValleyWidening*mat)
	room := min(p.ChainSpread, max(0, p.ChainReach-hf-p.Wander))
	type chain struct {
		frac, scale      float64
		wl, ph           [2]float64
		trim0, trim1, fd float64
	}
	chains := make([]chain, n)
	for k := range chains {
		ch := &chains[k]
		base := 0.0
		if n > 1 {
			base = -room + 2*room*float64(k)/float64(n-1)
		}
		ch.frac = min(max(base+(2*rng.Float64()-1)*p.ChainJitter*spacing, -room), room)
		ch.scale = lerp(p.ChainScale, rng.Float64())
		for j := range 2 {
			ch.wl[j] = lerp(p.WanderWavelengthKM, rng.Float64())
			ch.ph[j] = 2 * math.Pi * rng.Float64()
		}
		ch.trim0, ch.trim1 = lerp(p.EndTrim, rng.Float64())*l, lerp(p.EndTrim, rng.Float64())*l
		ch.fd = lerp(p.FadeKM, rng.Float64())
	}
	slices.SortStableFunc(chains, func(a, b chain) int { return cmp.Compare(a.frac, b.frac) })
	// The main chain: on the coast side of an active margin, opposite its
	// elevated flank; otherwise the middle one. The others take the scales
	// drawn, largest nearest the main chain.
	main := (n - 1) / 2
	if f.Kind == "active_margin" {
		switch f.ElevatedFlank {
		case "left":
			main = 0 // the coast is on the right, at negative offsets
		case "right":
			main = n - 1
		}
	}
	scales := make([]float64, 0, n)
	order := make([]int, 0, n)
	for k := range chains {
		if k != main {
			scales = append(scales, chains[k].scale)
			order = append(order, k)
		}
	}
	slices.SortFunc(scales, func(a, b float64) int { return cmp.Compare(b, a) })
	slices.SortStableFunc(order, func(a, b int) int {
		return cmp.Compare(math.Abs(chains[a].frac-chains[main].frac), math.Abs(chains[b].frac-chains[main].frac))
	})
	chains[main].scale = 1
	for i, k := range order {
		chains[k].scale = scales[i]
	}
	halfKM := func(s float64) float64 { return max(p.MinChainHalfKM, hf*fr.half(s)) }

	// Chains, with their gaps. A chain too short to publish has ID 0, and
	// its branches are not published.
	ids := make([]uint32, n)
	type built struct {
		pts []structure.Sample // unwrapped
		at  []float64          // backbone position of each sample
		cum []float64
	}
	var made []built
	for k := range chains {
		ch := chains[k]
		frac := func(s float64) float64 {
			w := (math.Sin(2*math.Pi*s/ch.wl[0]+ch.ph[0]) + math.Sin(2*math.Pi*s/ch.wl[1]+ch.ph[1])) / 2
			return ch.frac + p.Wander*w
		}
		pts, at := offsetCurve(fr, ch.trim0, l-ch.trim1, p.StationKM, frac, halfKM)
		b := built{pts: pts, at: at, cum: make([]float64, len(pts))}
		for i := 1; i < len(pts); i++ {
			b.cum[i] = b.cum[i-1] + math.Hypot(pts[i].X-pts[i-1].X, pts[i].Y-pts[i-1].Y)
		}
		lc := b.cum[len(b.cum)-1]
		var gaps [][2]float64
		mean := 1000 / lerp(p.GapsPer1000KM, mat)
		for pos := expo(rng.Float64(), mean); pos < lc; pos += expo(rng.Float64(), mean) {
			g := lerp(p.GapKM, rng.Float64()) * (1 + p.GapMaturity*mat)
			gaps = append(gaps, [2]float64{round3(pos), round3(pos + g)})
			pos += g
		}
		made = append(made, b)
		amps := make([]float64, len(pts))
		for i, s := range at {
			amps[i] = amp(s) * ch.scale
		}
		ids[k] = c.addRidge(dom, Ridge{Feature: f.ID, Kind: RidgeChain, GapsKM: gaps}, pts, amps, ch.fd)
	}
	// Branches.
	for k := range chains {
		ch, b := chains[k], made[k]
		lc := b.cum[len(b.cum)-1]
		mean := 1000 / lerp(p.BranchesPer1000KM, mat)
		for pos := expo(rng.Float64(), mean); pos < lc; pos += expo(rng.Float64(), mean) {
			angle := lerp(p.BranchAngleDeg, rng.Float64()) * math.Pi / 180
			reach, scale, sideU := lerp(p.BranchReach, rng.Float64()), lerp(p.BranchScale, rng.Float64()), rng.Float64()
			i := max(1, min(sortSearch(b.cum, pos), len(b.cum)-1))
			t := (pos - b.cum[i-1]) / (b.cum[i] - b.cum[i-1])
			pa, pb := b.pts[i-1], b.pts[i]
			x, y := pa.X+t*(pb.X-pa.X), pa.Y+t*(pb.Y-pa.Y)
			s := b.at[i-1] + t*(b.at[i]-b.at[i-1])
			seg := math.Hypot(pb.X-pa.X, pb.Y-pa.Y)
			tx, ty := (pb.X-pa.X)/seg, (pb.Y-pa.Y)/seg
			nx, ny := leftNormal(tx, ty)
			side := math.Copysign(1, ch.frac)
			if ch.frac == 0 && sideU < 0.5 {
				side = -1
			}
			lateral := (p.ChainReach - math.Abs(ch.frac) - hf) * fr.half(s) * reach
			length := lateral / math.Sin(angle)
			if length < p.MinBranchKM || ids[k] == 0 {
				continue
			}
			dx, dy := math.Cos(angle)*tx+math.Sin(angle)*side*nx, math.Cos(angle)*ty+math.Sin(angle)*side*ny
			const steps = 5
			pts := make([]structure.Sample, steps+1)
			amps := make([]float64, steps+1)
			a0 := amp(s) * ch.scale * scale
			for j := range pts {
				u := float64(j) / steps
				pts[j] = structure.Sample{X: x + dx*length*u, Y: y + dy*length*u, WidthKM: 2 * 0.7 * halfKM(s)}
				amps[j] = a0 * (1 - smoothstep(0.3, 1, u))
			}
			c.addRidge(dom, Ridge{Feature: f.ID, Kind: RidgeBranch, Parent: ids[k]}, pts, amps, 0)
		}
	}
	// Troughs between adjacent chains.
	for k := 0; k+1 < n; k++ {
		present, start := rng.Float64(), rng.Float64()
		tl, depth := min(lerp(p.TroughLengthKM, rng.Float64()), l), lerp(p.TroughDepthM, rng.Float64())
		if present >= lerp(p.TroughShare, mat) {
			continue
		}
		mid, gap := (chains[k].frac+chains[k+1].frac)/2, (chains[k+1].frac-chains[k].frac)/2
		if gap <= 0 {
			continue
		}
		s0 := start * (l - tl)
		pts, _ := offsetCurve(fr, s0, s0+tl, p.StationKM, func(float64) float64 { return mid }, func(s float64) float64 { return max(p.MinChainHalfKM/2, gap*fr.half(s)) })
		if len(pts) < 2 {
			continue
		}
		bb, _ := toBase(dom, pts, nil)
		if len(bb) < 2 {
			continue
		}
		c.file.Troughs = append(c.file.Troughs, Trough{ID: uint32(len(c.file.Troughs) + 1), Feature: f.ID, Backbone: bb, DepthM: round3(depth)})
	}
	// Corridors.
	for s := rng.Float64() * lerp(p.CorridorSpacingKM, 0.5); s < l; s += lerp(p.CorridorSpacingKM, rng.Float64()) {
		angle := (2*rng.Float64() - 1) * p.CorridorAngleDeg * math.Pi / 180
		half, share := lerp(p.CorridorHalfKM, rng.Float64()), lerp(p.SaddleShare, rng.Float64())
		x, y := fr.at(s)
		tx, ty := fr.tangent(s, p.StationKM)
		nx, ny := leftNormal(tx, ty)
		dx, dy := nx*math.Cos(angle)+tx*math.Sin(angle), ny*math.Cos(angle)+ty*math.Sin(angle)
		r := 1.1 * fr.half(s)
		pts := []structure.Sample{{X: x - dx*r, Y: y - dy*r, WidthKM: 2 * half}, {X: x + dx*r, Y: y + dy*r, WidthKM: 2 * half}}
		bb, _ := toBase(dom, pts, nil)
		if len(bb) < 2 {
			continue
		}
		c.file.Corridors = append(c.file.Corridors, Corridor{ID: uint32(len(c.file.Corridors) + 1), Feature: f.ID, Backbone: bb, SaddleShare: round3(share), Saddles: []Saddle{}})
		c.corridorEnds = append(c.corridorEnds, [2][2]float64{{pts[0].X, pts[0].Y}, {pts[1].X, pts[1].Y}})
	}
	// Volcanic peaks of an active margin.
	if f.Kind == "active_margin" && rng.Float64() < p.VolcanicShare {
		c.peaks(p, dom, pl, fr, chains[main].frac/2, p.MarginPeakHeight, amp)
	}
	return nil
}

// addRidge publishes a ridge from unwrapped samples and their amplitudes,
// and returns its ID, or 0 if it is too short to publish.
func (c *components) addRidge(dom structure.Domain, r Ridge, pts []structure.Sample, amps []float64, fade float64) uint32 {
	if len(pts) < 2 {
		return 0
	}
	bb, kept := toBase(dom, pts, amps)
	if len(bb) < 2 {
		return 0
	}
	r.ID = uint32(len(c.file.Ridges) + 1)
	r.Backbone = bb
	c.file.Ridges = append(c.file.Ridges, r)
	c.amps, c.fades, c.unwrapped = append(c.amps, kept), append(c.fades, fade), append(c.unwrapped, pts)
	return r.ID
}

// segmentDistance returns the distance on the torus from p to the segment
// from a to b, which is shorter than half the world.
func segmentDistance(dom structure.Domain, p structure.Point, a, b structure.Sample) float64 {
	mx, my := (a.X+b.X)/2, (a.Y+b.Y)/2
	qx := p.X + math.Round((mx-p.X)/dom.WidthKM)*dom.WidthKM - a.X
	qy := p.Y + math.Round((my-p.Y)/dom.HeightKM)*dom.HeightKM - a.Y
	ex, ey := b.X-a.X, b.Y-a.Y
	t := min(max((qx*ex+qy*ey)/(ex*ex+ey*ey), 0), 1)
	return math.Hypot(qx-t*ex, qy-t*ey)
}

// rasterize combines ridges and peaks by maximum into ridge. Along a
// corridor every ridge of its belt is lowered by the corridor's saddle
// share, weighted by the kernel across the corridor; over a basin ridges
// and peaks fade out with the basin's weight.
func (c *components) rasterize(ctx context.Context, dom structure.Domain, g continent.Grid, basin []uint8, ridge []float32) error {
	cols, rows := g.Columns, g.Rows
	byFeature := map[uint32][]int{}
	for i, cr := range c.file.Corridors {
		byFeature[cr.Feature] = append(byFeature[cr.Feature], i)
	}
	put := func(cell int, v float64) {
		if b := basin[cell]; b > 0 {
			v *= 1 - float64(b)/255
		}
		ridge[cell] = max(ridge[cell], float32(v))
	}
	for i := range c.file.Ridges {
		rec := &c.file.Ridges[i]
		r := ridgeShape{rec: rec, amp: c.amps[i], cum: make([]float64, len(rec.Backbone)), fade: c.fades[i]}
		for j := 1; j < len(rec.Backbone); j++ {
			a, b := rec.Backbone[j-1], rec.Backbone[j]
			r.cum[j] = r.cum[j-1] + math.Hypot(b.X-a.X, b.Y-a.Y)
		}
		cors := byFeature[rec.Feature]
		err := structure.LineCells(ctx, dom, cols, rows, rec.Backbone, 1, func(cell int, u, s float64) {
			if u >= 1 {
				return
			}
			v := r.ampAt(s) * kernel(u) * r.along(s)
			if v <= 0 {
				return
			}
			pt := structure.Point{X: (float64(cell%cols) + 0.5) * g.CellWidthKM, Y: (float64(cell/cols) + 0.5) * g.CellHeightKM}
			low := 0.0
			for _, k := range cors {
				cr := &c.file.Corridors[k]
				a, b := cr.Backbone[0], cr.Backbone[1]
				low = max(low, cr.SaddleShare*kernel(segmentDistance(dom, pt, a, b)/(a.WidthKM/2)))
			}
			put(cell, v*(1-low))
		})
		if err != nil {
			return err
		}
	}
	for _, pk := range c.file.Peaks {
		r := pk.RadiusKM
		structure.Visit(dom, cols, rows, structure.Rect{MinX: pk.X - r, MinY: pk.Y - r, MaxX: pk.X + r, MaxY: pk.Y + r}, func(col, row int, pt structure.Point) {
			dx := pt.X - pk.X
			dx -= math.Round(dx/dom.WidthKM) * dom.WidthKM
			dy := pt.Y - pk.Y
			dy -= math.Round(dy/dom.HeightKM) * dom.HeightKM
			if d := math.Hypot(dx, dy); d < r {
				put(row*cols+col, pk.HeightM*kernel(d/r))
			}
		})
	}
	return ctx.Err()
}

// saddles records where each corridor crosses each ridge of its belt, with
// the surface elevation of the cell there.
func (c *components) saddles(dom structure.Domain, g continent.Grid, elev []float32) {
	for i := range c.file.Corridors {
		cr := &c.file.Corridors[i]
		e := c.corridorEnds[i]
		for j := range c.file.Ridges {
			if c.file.Ridges[j].Feature != cr.Feature {
				continue
			}
			pts := c.unwrapped[j]
			for k := 0; k+1 < len(pts); k++ {
				x, y, ok := intersect(e[0][0], e[0][1], e[1][0], e[1][1], pts[k].X, pts[k].Y, pts[k+1].X, pts[k+1].Y)
				if !ok {
					continue
				}
				x, y = wrapPoint(dom, x, y)
				col, row := min(int(x/g.CellWidthKM), g.Columns-1), min(int(y/g.CellHeightKM), g.Rows-1)
				cr.Saddles = append(cr.Saddles, Saddle{Ridge: c.file.Ridges[j].ID, X: x, Y: y, ElevationM: round3(float64(elev[row*g.Columns+col]))})
				break
			}
		}
	}
}

// intersect returns where segment ab crosses segment cd, if it does.
func intersect(ax, ay, bx, by, cx, cy, dx, dy float64) (float64, float64, bool) {
	ex, ey, fx, fy := bx-ax, by-ay, dx-cx, dy-cy
	den := ex*fy - ey*fx
	if den == 0 {
		return 0, 0, false
	}
	t := ((cx-ax)*fy - (cy-ay)*fx) / den
	u := ((cx-ax)*ey - (cy-ay)*ex) / den
	if t < 0 || t > 1 || u < 0 || u > 1 {
		return 0, 0, false
	}
	return ax + t*ex, ay + t*ey, true
}

// DomainTrench keys the stream of one trench.
const DomainTrench = "terrain-v1/trench"

// TrenchDiagnostics reports one trench's drawn floor and the elevation of
// its axis, the water cells where its weight is at least 0.9.
type TrenchDiagnostics struct {
	ID     uint32      `json:"id"`
	Parent uint32      `json:"parent"`
	FloorM *[2]float64 `json:"floor_m"`
	Axis   Stats       `json:"axis"`
}

// trenchDepressions lowers the surface along each trench toward its floor,
// drawn as control samples along its backbone from the trench's stream,
// deepest on the axis, easing to the surface at the edge of its width and
// over its end fades. It never lowers provisional land.
func trenchDepressions(ctx context.Context, seed uint64, p BeltParams, spacing [2]float64, dom structure.Domain, g continent.Grid, tf *trench.TrenchFile, land []byte, elev []float32) ([]TrenchDiagnostics, error) {
	if tf == nil {
		return nil, nil
	}
	out := make([]TrenchDiagnostics, 0, len(tf.Trenches))
	for _, tr := range tf.Trenches {
		if tr.Targets.RegionalElevationM == nil || tr.Key == nil {
			return nil, fmt.Errorf("trench %d has no floor target or key", tr.ID)
		}
		rng := prng.New(seed, DomainTrench, *tr.Key)
		l := structure.NewLine(dom, tr.Backbone).Length()
		n := max(1, int(math.Round(l/lerp(spacing, rng.Float64()))))
		pl := &plan{step: l / float64(n)}
		for range n + 1 {
			pl.regional = append(pl.regional, lerp(*tr.Targets.RegionalElevationM, rng.Float64()))
		}
		fade := min(p.TrenchFadeKM, l/3)
		var axis []int32
		err := structure.LineCells(ctx, dom, g.Columns, g.Rows, tr.Backbone, 1, func(cell int, u, s float64) {
			if u >= 1 || land != nil && land[cell] == 1 {
				return
			}
			k := kernel(u)
			w := k * min(smoothstep(0, fade, s), smoothstep(0, fade, l-s))
			elev[cell] -= float32(w * max(0, float64(elev[cell])-pl.at(pl.regional, s)))
			if k >= 0.9 {
				axis = append(axis, int32(cell))
			}
		})
		if err != nil {
			return nil, err
		}
		out = append(out, TrenchDiagnostics{ID: tr.ID, Parent: tr.Parent, FloorM: span(pl.regional), Axis: cellStats(elev, axis)})
	}
	return out, nil
}

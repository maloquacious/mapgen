// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package structure

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"slices"
	"sort"

	"github.com/maloquacious/mapgen/internal/continent"
	"github.com/maloquacious/mapgen/internal/fracture"
	"github.com/maloquacious/mapgen/internal/prng"
)

// Rock classification turns features and provinces into dimensionless
// geological proxies and applies the layer reference's rock rule to every
// cell. See "Geology proxies and rock classification" in the design.
const (
	RockAlgorithm   = "rocks-v1"
	ProxyAlgorithm  = "geology-proxy-v1"
	RockFile        = "rock.u8"
	RockPreviewFile = "preview-rock.png"
	DomainRockNoise = "structure-v1/rocks/noise"

	FieldPressure  = "pressure"
	FieldShear     = "shear"
	FieldIgneous   = "igneous"
	FieldRockNoise = "rock-noise"
)

// GeologyFields names the saved proxy fields, in the order they are written.
var GeologyFields = []string{FieldPressure, FieldShear, FieldIgneous, FieldRockNoise}

var geologyDescriptions = map[string]string{
	FieldPressure:  "geology-proxy-v1 pressure in [-2,2]: compressional context positive, extensional negative; dimensionless, not measured stress",
	FieldShear:     "geology-proxy-v1 shear in [0,1]: strike-slip and oblique context; dimensionless",
	FieldIgneous:   "geology-proxy-v1 igneous influence in [0,1]; dimensionless",
	FieldRockNoise: "rocks-v1 rock noise rv in [0,1]: the square of the mid-rank of four octaves of fracture noise",
}

// Rock is a cell's dominant rock type. Zero means not yet classified.
type Rock uint8

const (
	RockUnclassified Rock = iota
	RockSedimentary
	RockIgneous
	RockMetamorphic
)

func (r Rock) String() string {
	switch r {
	case RockSedimentary:
		return "sedimentary"
	case RockIgneous:
		return "igneous"
	case RockMetamorphic:
		return "metamorphic"
	}
	return "unclassified"
}

// Influence is what one kind of feature contributes to the proxies, before
// its profile is applied. The rock rule reacts to igneous influence only
// above IgneousInfluence, so an igneous profile sets how far an igneous zone
// reaches rather than grading it. A profile is [core, reach] in half widths from the
// backbone: full strength out to core, fading to nothing at reach. Area
// features have no profiles; they contribute their outline mask.
type Influence struct {
	Pressure       float64     `json:"pressure"`
	Shear          float64     `json:"shear"`
	Igneous        float64     `json:"igneous"`
	StressProfile  *[2]float64 `json:"stress_profile,omitzero"`
	IgneousProfile *[2]float64 `json:"igneous_profile,omitzero"`
}

// RockParams are the calibration values behind the proxies and the rock
// classification. They are recorded in the manifest.
type RockParams struct {
	Algorithm string `json:"algorithm"`
	Proxy     string `json:"proxy"`
	// Influences is keyed by feature type, then "/" and the kind of a belt
	// or the sense of a fault.
	Influences map[string]Influence `json:"influences"`
	// VolcanicIgneous is the igneous influence over a volcanic province, and
	// OceanicIgneous over an oceanic one beyond the shelf.
	VolcanicIgneous float64 `json:"volcanic_igneous"`
	OceanicIgneous  float64 `json:"oceanic_igneous"`
	// ShelfKM is the offshore coast distance over which the oceanic value
	// rises from absent to full.
	ShelfKM [2]float64 `json:"shelf_km"`
	// Rock noise: octave k has wavelength NoiseWavelengthKM/2^k and weight
	// 2^-k.
	NoiseWavelengthKM float64 `json:"noise_wavelength_km"`
	NoiseOctaves      int     `json:"noise_octaves"`
	// The rock rule's thresholds, from the layer reference.
	MetamorphicLimit float64 `json:"metamorphic_limit"`
	TransformLimit   float64 `json:"transform_limit"`
	IgneousNoise     float64 `json:"igneous_noise"`
	IgneousInfluence float64 `json:"igneous_influence"`
}

// DefaultRockParams returns the geology-proxy-v1 and rocks-v1 defaults.
func DefaultRockParams() RockParams {
	pr := func(core, reach float64) *[2]float64 { return &[2]float64{core, reach} }
	return RockParams{
		Algorithm: RockAlgorithm, Proxy: ProxyAlgorithm,
		Influences: map[string]Influence{
			"mountain_belt/collision":     {2.0, 0.3, 0, pr(0.6, 1.5), nil},
			"mountain_belt/active_margin": {1.6, 0.4, 1.0, pr(0.6, 1.5), pr(1.0, 3.0)},
			"mountain_belt/old_orogen":    {1.2, 0.2, 0, pr(0.5, 1.2), nil},
			"rift":                        {-1.2, 0.2, 0.7, pr(0.5, 1.5), pr(0.5, 2.0)},
			"island_arc":                  {0.8, 0.2, 1.0, pr(0.6, 1.5), pr(1.0, 2.0)},
			"fault/strike_slip":           {0, 1.0, 0, pr(0.5, 2.0), nil},
			"fault/thrust":                {1.0, 0.3, 0, pr(0.5, 2.0), nil},
			"fault/normal":                {-0.8, 0.3, 0, pr(0.5, 2.0), nil},
			"plateau":                     {0.8, 0, 0, nil, nil},
			"basin":                       {0, 0, 0, nil, nil},
		},
		VolcanicIgneous: 0.6, OceanicIgneous: 0.3, ShelfKM: [2]float64{150, 300},
		NoiseWavelengthKM: 400, NoiseOctaves: 4,
		MetamorphicLimit: 0.10, TransformLimit: 0.09, IgneousNoise: 0.4, IgneousInfluence: 0.25,
	}
}

// influenceKey returns the key of f's entry in RockParams.Influences.
func influenceKey(f *Feature) string {
	switch f.Type {
	case TypeMountainBelt:
		return f.Type + "/" + f.Kind
	case TypeFault:
		return f.Type + "/" + f.Sense
	}
	return f.Type
}

// smoothstep is 0 below a, 1 above b, and t²(3−2t) between.
func smoothstep(a, b, x float64) float64 {
	t := min(max((x-a)/(b-a), 0), 1)
	return t * t * (3 - 2*t)
}

// fade is a profile's strength at cross-belt position u.
func fade(profile *[2]float64, u float64) float64 {
	return 1 - smoothstep(profile[0], profile[1], u)
}

// ClassifyRock applies the rocks-v1 rule to one cell's proxies and rock
// noise. Resource placement and Load call it on the saved float32 values, so
// the rock raster can always be checked against them.
func ClassifyRock(p RockParams, pressure, shear, igneous, rv float64) Rock {
	stress := math.Abs(pressure)
	limit := p.MetamorphicLimit
	if shear > stress {
		limit = p.TransformLimit
	}
	switch {
	case (stress+shear)/2+(2*rv-1) >= limit:
		return RockMetamorphic
	case rv > p.IgneousNoise || igneous > p.IgneousInfluence:
		return RockIgneous
	}
	return RockSedimentary
}

// RockSummary counts each rock type on provisional land and ocean.
type RockSummary struct {
	Land  map[string]int `json:"land"`
	Ocean map[string]int `json:"ocean"`
}

// geology is the output of classifyRocks.
type geology struct {
	fields map[string]*fracture.FractureField
	rock   []byte
}

// classifyRocks builds the proxies and rock noise and classifies every cell.
func classifyRocks(ctx context.Context, seed uint64, p RockParams, g continent.Grid, ff *FeatureFile, pf *ProvinceFile, owner []uint32, coast *Coast) (*geology, error) {
	cols, rows := g.Columns, g.Rows
	n := cols * rows
	dom := Domain{WidthKM: g.WidthKM, HeightKM: g.HeightKM}
	pos, neg := make([]float32, n), make([]float32, n)
	shear, ign := make([]float32, n), make([]float32, n)
	add := func(cell int, in Influence, stress, igneous float64) {
		if c := float32(in.Pressure * stress); c > pos[cell] {
			pos[cell] = c
		} else if c < neg[cell] {
			neg[cell] = c
		}
		shear[cell] = max(shear[cell], float32(in.Shear*stress))
		ign[cell] = max(ign[cell], float32(in.Igneous*igneous))
	}
	for i := range ff.Features {
		f := &ff.Features[i]
		in, ok := p.Influences[influenceKey(f)]
		if !ok {
			return nil, fmt.Errorf("rock parameters have no influence for %s", influenceKey(f))
		}
		if f.Linear() {
			if in.StressProfile == nil {
				return nil, fmt.Errorf("rock parameters have no stress profile for %s", influenceKey(f))
			}
			reach := in.StressProfile[1]
			if in.IgneousProfile != nil {
				reach = max(reach, in.IgneousProfile[1])
			}
			err := lineCells(ctx, dom, cols, rows, f.Backbone, reach, func(cell int, u float64) {
				ig := 0.0
				if in.IgneousProfile != nil {
					ig = fade(in.IgneousProfile, u)
				}
				add(cell, in, fade(in.StressProfile, u), ig)
			})
			if err != nil {
				return nil, err
			}
			continue
		}
		r := NewRegion(dom, f.Outline, f.EdgeKM)
		visit(dom, cols, rows, r.Bounds(), func(col, row int, pt Point) {
			if m := r.Mask(pt); m > 0 {
				add(row*cols+col, in, m, m)
			}
		})
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	// Provinces add igneous influence without falloff.
	for i, o := range owner {
		var v float64
		switch pf.Provinces[o-1].Type {
		case TypeVolcanic:
			v = p.VolcanicIgneous
		case TypeOceanic:
			pt := Point{(float64(i%cols) + 0.5) * g.CellWidthKM, (float64(i/cols) + 0.5) * g.CellHeightKM}
			v = p.OceanicIgneous * smoothstep(p.ShelfKM[0], p.ShelfKM[1], -coast.DistanceAt(pt))
		default:
			continue
		}
		ign[i] = max(ign[i], float32(v))
		if i%cols == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
	}
	// pos becomes pressure.
	for i := range pos {
		pos[i] = min(max(pos[i]+neg[i], -2), 2)
	}
	noise, err := rockNoise(ctx, seed, p, g)
	if err != nil {
		return nil, err
	}
	rock := make([]byte, n)
	for i := range rock {
		rock[i] = byte(ClassifyRock(p, float64(pos[i]), float64(shear[i]), float64(ign[i]), float64(noise[i])))
	}
	field := func(v []float32) *fracture.FractureField {
		return &fracture.FractureField{Columns: cols, Rows: rows, WidthKM: g.WidthKM, HeightKM: g.HeightKM, Values: v}
	}
	return &geology{
		fields: map[string]*fracture.FractureField{
			FieldPressure: field(pos), FieldShear: field(shear), FieldIgneous: field(ign), FieldRockNoise: field(noise),
		},
		rock: rock,
	}, nil
}

// lineCells calls fn for every cell whose center lies within reach half
// widths (of the backbone's widest sample) of the backbone, with the cell's
// cross-belt position u, equal to Line.Cross at the cell center.
//
// Instead of querying every segment for every cell, it measures each segment
// only against the cells near it, on a local raster in the backbone's
// unwrapped coordinates. A local raster wider or taller than the world holds
// several images of one cell; the nearest is used.
func lineCells(ctx context.Context, dom Domain, cols, rows int, backbone []Sample, reach float64, fn func(cell int, u float64)) error {
	cw, ch := dom.WidthKM/float64(cols), dom.HeightKM/float64(rows)
	half := 0.0
	box := Rect{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for _, s := range backbone {
		half = max(half, s.WidthKM/2)
		box = Rect{min(box.MinX, s.X), min(box.MinY, s.Y), max(box.MaxX, s.X), max(box.MaxY, s.Y)}
	}
	R := reach * half
	first := func(lo, cell float64) int { return int(math.Ceil(lo/cell - 0.5)) }
	last := func(hi, cell float64) int { return int(math.Floor(hi/cell - 0.5)) }
	c0, c1 := first(box.MinX-R, cw), last(box.MaxX+R, cw)
	r0, r1 := first(box.MinY-R, ch), last(box.MaxY+R, ch)
	nc, nr := c1-c0+1, r1-r0+1
	if nc <= 0 || nr <= 0 {
		return nil
	}
	best := make([]float64, nc*nr) // squared distance
	seg := make([]int32, nc*nr)
	tt := make([]float64, nc*nr)
	for i := range best {
		best[i] = math.Inf(1)
	}
	for i := 0; i+1 < len(backbone); i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		a, b := backbone[i], backbone[i+1]
		ex, ey := b.X-a.X, b.Y-a.Y
		ee := ex*ex + ey*ey
		ca, cb := max(c0, first(min(a.X, b.X)-R, cw)), min(c1, last(max(a.X, b.X)+R, cw))
		ra, rb := max(r0, first(min(a.Y, b.Y)-R, ch)), min(r1, last(max(a.Y, b.Y)+R, ch))
		for r := ra; r <= rb; r++ {
			qy := (float64(r)+0.5)*ch - a.Y
			k := (r-r0)*nc + (ca - c0)
			for c := ca; c <= cb; c, k = c+1, k+1 {
				qx := (float64(c)+0.5)*cw - a.X
				t := min(max((qx*ex+qy*ey)/ee, 0), 1)
				vx, vy := qx-t*ex, qy-t*ey
				// Strictly less, so equal distances keep the lower segment.
				if d2 := vx*vx + vy*vy; d2 < best[k] {
					best[k], seg[k], tt[k] = d2, int32(i), t
				}
			}
		}
	}
	l := NewLine(dom, backbone)
	u := func(k int) float64 {
		i := seg[k]
		s := l.cum[i] + tt[k]*(l.cum[i+1]-l.cum[i])
		return math.Sqrt(best[k]) / (l.WidthAt(s) / 2)
	}
	global := func(k int) int {
		c, r := c0+k%nc, r0+k/nc
		return ((r%rows+rows)%rows)*cols + (c%cols+cols)%cols
	}
	if nc <= cols && nr <= rows {
		for k, d2 := range best {
			if d2 <= R*R {
				fn(global(k), u(k))
			}
		}
		return nil
	}
	// Several images of a cell: keep the nearest, the first on a tie.
	nearest := map[int]int{}
	for k, d2 := range best {
		if d2 > R*R {
			continue
		}
		g := global(k)
		if j, ok := nearest[g]; !ok || d2 < best[j] {
			nearest[g] = k
		}
	}
	cells := make([]int, 0, len(nearest))
	for g := range nearest {
		cells = append(cells, g)
	}
	slices.Sort(cells)
	for _, g := range cells {
		fn(g, u(nearest[g]))
	}
	return nil
}

// rockNoise returns rv for every cell: four octaves of fracture noise,
// converted to their mid-rank n over all cells, and squared.
func rockNoise(ctx context.Context, seed uint64, p RockParams, g continent.Grid) ([]float32, error) {
	octaves := make([]*fracture.FractureField, p.NoiseOctaves)
	for k := range octaves {
		wl := p.NoiseWavelengthKM / math.Ldexp(1, k)
		cc := max(2, int(math.Round(g.WidthKM/(wl/4))))
		rc := max(2, int(math.Round(g.HeightKM/(wl/4))))
		f, err := fracture.BuildFracture(ctx, fracture.FractureConfig{
			Columns: cc, Rows: rc, WidthKM: g.WidthKM, HeightKM: g.HeightKM,
			Bumps: max(1, cc*rc/2), MinRadiusKM: wl / 4, MaxRadiusKM: wl / 2, Falloff: 2,
		}, prng.New(seed, DomainRockNoise, uint64(k)))
		if err != nil {
			return nil, fmt.Errorf("rock noise octave %d: %w", k, err)
		}
		octaves[k] = f
	}
	cols := g.Columns
	raw := make([]float32, cols*g.Rows)
	for i := range raw {
		x, y := (float64(i%cols)+0.5)*g.CellWidthKM, (float64(i/cols)+0.5)*g.CellHeightKM
		v := 0.0
		for k, f := range octaves {
			v += math.Ldexp(f.SampleTorus(x, y), -k)
		}
		raw[i] = float32(v)
		if i%cols == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
	}
	sorted := slices.Clone(raw)
	slices.Sort(sorted)
	n := float64(len(raw))
	out := make([]float32, len(raw))
	for i, v := range raw {
		below := sort.Search(len(sorted), func(j int) bool { return sorted[j] >= v })
		upto := sort.Search(len(sorted), func(j int) bool { return sorted[j] > v })
		m := (float64(below) + float64(upto-below)/2) / n
		out[i] = float32(m * m)
		if i%cols == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// summarizeRocks counts each rock on provisional land and ocean.
func summarizeRocks(land, rock []byte) *RockSummary {
	s := &RockSummary{Land: map[string]int{}, Ocean: map[string]int{}}
	for i, r := range rock {
		if land[i] == 1 {
			s.Land[Rock(r).String()]++
		} else {
			s.Ocean[Rock(r).String()]++
		}
	}
	return s
}

// Rock preview colors. Ocean cells use paler tones of the same hues.
var previewRock = map[Rock][2]color.RGBA{
	RockSedimentary: {{222, 200, 140, 255}, {214, 220, 214, 255}},
	RockIgneous:     {{176, 72, 60, 255}, {150, 160, 190, 255}},
	RockMetamorphic: {{88, 108, 150, 255}, {120, 150, 180, 255}},
}

// writeRockPreview draws the rock types, paler offshore, with the
// provisional coast, sampling every scale-th cell.
func writeRockPreview(path string, r *Result, scale int) error {
	g := r.Manifest.Grid
	w, h := (g.Columns-1)/scale+1, (g.Rows-1)/scale+1
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			i := y*scale*g.Columns + x*scale
			e := y*scale*g.Columns + (x+1)%w*scale
			s := (y+1)%h*scale*g.Columns + x*scale
			c := previewRock[Rock(r.Rock[i])][1-r.Land[i]]
			if r.Land[i] != r.Land[e] || r.Land[i] != r.Land[s] {
				c = color.RGBA{40, 40, 40, 255}
			}
			img.SetRGBA(x, y, c)
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

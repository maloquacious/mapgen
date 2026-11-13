// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package structure

import (
	"cmp"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"maps"
	"math"
	"slices"

	"github.com/maloquacious/mapgen/internal/continent"
	"github.com/maloquacious/mapgen/internal/fracture"
	"github.com/maloquacious/mapgen/internal/prng"
)

const (
	ProvincesFormat    = "mapgen-structure-provinces-v1"
	ProvinceAlgorithm  = "provinces-v1"
	ProvincesFile      = "provinces.json"
	OwnershipFile      = "provinces.u32"
	TypeShield         = "shield"
	TypeSedimentary    = "sedimentary"
	TypeVolcanic       = "volcanic"
	TypeOceanic        = "oceanic"
	DomainProvinceSeed = "structure-v1/provinces/seeds"
	DomainProvinceCost = "structure-v1/provinces/cost"
	DomainProvinceType = "structure-v1/provinces/types"
)

// ProvinceParams are the calibration values behind a province partition.
// They are recorded in the manifest.
type ProvinceParams struct {
	Algorithm string `json:"algorithm"`
	// MinAreaKM2 is the smallest region that is its own province. Smaller
	// islands belong to the surrounding ocean province and smaller enclosed
	// waters to the surrounding land.
	MinAreaKM2 float64 `json:"min_area_km2"`
	// SeedAreaKM2 is the land area per province seed within a landmass.
	SeedAreaKM2 float64 `json:"seed_area_km2"`
	// WorkingSpacingKM is the target spacing of the coarse grid used to
	// choose seeds and to build the growth cost field.
	WorkingSpacingKM float64 `json:"working_spacing_km"`
	// CostAmplitude scales the smooth field: crossing a cell costs its
	// length times a factor from 1 to 1+CostAmplitude.
	CostAmplitude float64 `json:"cost_amplitude"`
	// ShieldShare is the fraction of a landmass's provinces, those with the
	// highest mean continental value, typed as shield.
	ShieldShare float64 `json:"shield_share"`
	// VolcanicChance is the chance that a land province is volcanic.
	VolcanicChance float64 `json:"volcanic_chance"`
}

// DefaultProvinceParams returns the provinces-v1 defaults.
func DefaultProvinceParams() ProvinceParams {
	return ProvinceParams{Algorithm: ProvinceAlgorithm, MinAreaKM2: 20000, SeedAreaKM2: 1.5e6,
		WorkingSpacingKM: 50, CostAmplitude: 0.6, ShieldShare: 0.5, VolcanicChance: 0.04}
}

// Province is one contiguous geological region.
type Province struct {
	ID   uint32 `json:"id"`
	Type string `json:"type"`
	// Component is the landmass the province belongs to, numbered from 1 in
	// order of each landmass's lowest row-major cell; 0 for oceanic.
	Component uint32 `json:"component"`
	// Seed is the cell center the province grew from. For a province that
	// is a whole region, it is the region's lowest row-major cell.
	Seed    Point   `json:"seed"`
	Cells   int     `json:"cells"`
	AreaKM2 float64 `json:"area_km2"`
}

// ProvinceFile is the content of provinces.json.
type ProvinceFile struct {
	Format    string     `json:"format"`
	Provinces []Province `json:"provinces"`
}

// Marshal encodes pf in its canonical form.
func (pf *ProvinceFile) Marshal() ([]byte, error) {
	data, err := json.Marshal(pf, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// simplifyMask returns mask with every enclosed water smaller than minCells
// turned to land and then every landmass smaller than minCells turned to
// ocean, with the regions of the result. Provinces are built on this mask so
// that specks and puddles do not each become a province.
func simplifyMask(mask []byte, cols, rows, minCells int) ([]byte, *components) {
	out := slices.Clone(mask)
	for _, kind := range []byte{0, 1} {
		c := labelComponents(out, cols, rows)
		for i, v := range out {
			if v == kind && c.cells[c.label[i]] < minCells {
				out[i] = 1 - kind
			}
		}
	}
	return out, labelComponents(out, cols, rows)
}

// entry is a frontier cell in province growth.
type entry struct {
	d     float32
	owner uint32
	cell  int32
}

func (a entry) less(b entry) bool {
	if a.d != b.d {
		return a.d < b.d
	}
	if a.owner != b.owner {
		return a.owner < b.owner
	}
	return a.cell < b.cell
}

type frontier []entry

func (h *frontier) push(e entry) {
	*h = append(*h, e)
	s := *h
	for i := len(s) - 1; i > 0; {
		p := (i - 1) / 2
		if !s[i].less(s[p]) {
			break
		}
		s[i], s[p] = s[p], s[i]
		i = p
	}
}

func (h *frontier) pop() entry {
	s := *h
	top := s[0]
	n := len(s) - 1
	s[0] = s[n]
	s = s[:n]
	for i := 0; ; {
		l, r, m := 2*i+1, 2*i+2, i
		if l < n && s[l].less(s[m]) {
			m = l
		}
		if r < n && s[r].less(s[m]) {
			m = r
		}
		if m == i {
			break
		}
		s[i], s[m] = s[m], s[i]
		i = m
	}
	*h = s
	return top
}

// grow assigns every land cell reachable from a seed to the seed that
// reaches it most cheaply, moving between eight-connected land cells on the
// torus. seeds maps seed cells to owners. Leaving a cell costs the step's
// length in km times factor(cell). Equal costs resolve to the lower owner.
// owner is written only where it is zero, so each province is connected.
func grow(ctx context.Context, land []byte, cols, rows int, cw, ch float64, seeds map[int32]uint32, factor func(cell int) float64, owner []uint32) error {
	dist := make([]float32, len(land))
	for i := range dist {
		dist[i] = float32(math.Inf(1))
	}
	var h frontier
	for _, cell := range slices.Sorted(maps.Keys(seeds)) {
		dist[cell] = 0
		h.push(entry{0, seeds[cell], cell})
	}
	var step [3][3]float64
	for dy := range 3 {
		for dx := range 3 {
			step[dy][dx] = math.Hypot(float64(dx-1)*cw, float64(dy-1)*ch)
		}
	}
	for n := 0; len(h) > 0; n++ {
		if n&0xfffff == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		e := h.pop()
		i := int(e.cell)
		if owner[i] != 0 {
			continue
		}
		owner[i] = e.owner
		x, y := i%cols, i/cols
		f := factor(i)
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				j := (y+dy+rows)%rows*cols + (x+dx+cols)%cols
				if land[j] != 1 || owner[j] != 0 {
					continue
				}
				if d := e.d + float32(step[dy+1][dx+1]*f); d < dist[j] {
					dist[j] = d
					h.push(entry{d, e.owner, int32(j)})
				}
			}
		}
	}
	return nil
}

// costField builds the smooth field that keeps province boundaries from
// being straight lines between seeds: a fracture field on the working grid,
// scaled to [0, 1].
func costField(ctx context.Context, seed uint64, g continent.Grid, spacing float64) (*fracture.FractureField, error) {
	cc, rc := max(1, int(math.Round(g.WidthKM/spacing))), max(1, int(math.Round(g.HeightKM/spacing)))
	minR := 1.5 * max(g.WidthKM/float64(cc), g.HeightKM/float64(rc))
	f, err := fracture.BuildFracture(ctx, fracture.FractureConfig{
		Columns: cc, Rows: rc, WidthKM: g.WidthKM, HeightKM: g.HeightKM,
		Bumps: max(1, cc*rc/4), MinRadiusKM: minR, MaxRadiusKM: max(g.HeightKM/6, 2*minR), Falloff: 2,
	}, prng.New(seed, DomainProvinceCost, 0))
	if err != nil {
		return nil, fmt.Errorf("province cost field: %w", err)
	}
	lo, hi := slices.Min(f.Values), slices.Max(f.Values)
	for i, v := range f.Values {
		if hi > lo {
			f.Values[i] = (v - lo) / (hi - lo)
		} else {
			f.Values[i] = 0
		}
	}
	return f, nil
}

// chooseSeeds picks k spatially separated seed cells in landmass n. It works
// on the coarse grid: each coarse cell the landmass touches is a candidate,
// represented by the landmass's lowest cell inside it. The first seed is
// drawn at random; each later one is the candidate farthest from the seeds
// so far, with a random factor so the layout is not purely geometric.
func chooseSeeds(seed uint64, g continent.Grid, c *components, n, k int, spacing float64) []int32 {
	cc, rc := max(1, int(math.Round(g.WidthKM/spacing))), max(1, int(math.Round(g.HeightKM/spacing)))
	rep := make([]int32, cc*rc)
	for i := range rep {
		rep[i] = -1
	}
	for i, l := range c.label {
		if int(l) != n {
			continue
		}
		// Integer arithmetic maps each cell to exactly one coarse cell.
		ci := (i/c.cols*rc/c.rows)*cc + i%c.cols*cc/c.cols
		if rep[ci] < 0 {
			rep[ci] = int32(i)
		}
	}
	type candidate struct {
		x, y   float64
		cell   int32
		weight float64
		far    float64
	}
	rng := prng.New(seed, DomainProvinceSeed, uint64(n))
	var cs []candidate
	for ci, cell := range rep {
		if cell >= 0 {
			cs = append(cs, candidate{(float64(ci%cc) + 0.5) * g.WidthKM / float64(cc), (float64(ci/cc) + 0.5) * g.HeightKM / float64(rc),
				cell, 0.75 + 0.5*rng.Float64(), math.Inf(1)})
		}
	}
	k = min(k, len(cs))
	seeds := make([]int32, 0, k)
	next := rng.IntN(len(cs))
	for {
		s := cs[next]
		seeds = append(seeds, s.cell)
		if len(seeds) == k {
			return seeds
		}
		best := -1.0
		for i := range cs {
			dx, dy := math.Abs(cs[i].x-s.x), math.Abs(cs[i].y-s.y)
			cs[i].far = min(cs[i].far, math.Hypot(min(dx, g.WidthKM-dx), min(dy, g.HeightKM-dy)))
			if score := cs[i].far * cs[i].weight; score > best {
				best, next = score, i
			}
		}
	}
}

// buildProvinces partitions every cell into provinces and types them.
//
// Each landmass of the simplified mask gets one seed per SeedAreaKM2, at
// least one, and its cells go to the cheapest seed. Each ocean region is one
// oceanic province. Land provinces are typed from the mean of the continental
// field: within a landmass the highest ShieldShare are shield and the rest
// sedimentary; a landmass that is a single province is shield when its mean
// is at least the mean over all land. A seeded draw then makes a few
// provinces volcanic.
func buildProvinces(ctx context.Context, seed uint64, p ProvinceParams, g continent.Grid, land []byte, continental []float32) (*ProvinceFile, []uint32, error) {
	cols, rows := g.Columns, g.Rows
	cellKM2 := g.CellWidthKM * g.CellHeightKM
	mask, c := simplifyMask(land, cols, rows, int(math.Ceil(p.MinAreaKM2/cellKM2)))
	center := func(cell int32) Point {
		return Point{(float64(int(cell)%cols) + 0.5) * g.CellWidthKM, (float64(int(cell)/cols) + 0.5) * g.CellHeightKM}
	}
	pf := &ProvinceFile{Format: ProvincesFormat, Provinces: []Province{}}
	whole := make([]uint32, len(c.cells)) // province of a region that is one province
	seeds := map[int32]uint32{}
	for n := 1; n < len(c.cells); n++ {
		id := uint32(len(pf.Provinces) + 1)
		if n > c.land {
			whole[n] = id
			pf.Provinces = append(pf.Provinces, Province{ID: id, Type: TypeOceanic, Seed: center(c.first[n])})
			continue
		}
		k := max(1, int(math.Round(float64(c.cells[n])*cellKM2/p.SeedAreaKM2)))
		if k == 1 {
			whole[n] = id
			pf.Provinces = append(pf.Provinces, Province{ID: id, Component: uint32(n), Seed: center(c.first[n])})
			continue
		}
		for i, cell := range chooseSeeds(seed, g, c, n, k, p.WorkingSpacingKM) {
			seeds[cell] = id + uint32(i)
			pf.Provinces = append(pf.Provinces, Province{ID: id + uint32(i), Component: uint32(n), Seed: center(cell)})
		}
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
	}
	owner := make([]uint32, len(land))
	for i, l := range c.label {
		owner[i] = whole[l]
	}
	if len(seeds) > 0 {
		field, err := costField(ctx, seed, g, p.WorkingSpacingKM)
		if err != nil {
			return nil, nil, err
		}
		factor := func(cell int) float64 {
			return 1 + p.CostAmplitude*field.SampleTorus((float64(cell%cols)+0.5)*g.CellWidthKM, (float64(cell/cols)+0.5)*g.CellHeightKM)
		}
		if err := grow(ctx, mask, cols, rows, g.CellWidthKM, g.CellHeightKM, seeds, factor, owner); err != nil {
			return nil, nil, err
		}
	}
	// Type land provinces from the continental field.
	sum := make([]float64, len(pf.Provinces)+1)
	landSum, landCells := 0.0, 0
	for i, o := range owner {
		pr := &pf.Provinces[o-1]
		pr.Cells++
		if pr.Type != TypeOceanic {
			sum[o] += float64(continental[i])
			landSum += float64(continental[i])
			landCells++
		}
	}
	mean := func(pr *Province) float64 { return sum[pr.ID] / float64(pr.Cells) }
	rng := prng.New(seed, DomainProvinceType, 0)
	for first := 0; first < len(pf.Provinces); {
		last := first + 1
		for last < len(pf.Provinces) && pf.Provinces[last].Component == pf.Provinces[first].Component && pf.Provinces[first].Component != 0 {
			last++
		}
		group := pf.Provinces[first:last]
		first = last
		if group[0].Type == TypeOceanic {
			group[0].AreaKM2 = float64(group[0].Cells) * cellKM2
			continue
		}
		// Rank the landmass's provinces by mean continental value.
		order := make([]*Province, len(group))
		for i := range group {
			order[i] = &group[i]
		}
		slices.SortFunc(order, func(a, b *Province) int {
			return cmp.Or(cmp.Compare(mean(b), mean(a)), cmp.Compare(a.ID, b.ID))
		})
		shields := int(math.Round(p.ShieldShare * float64(len(group))))
		if len(group) == 1 {
			shields = 0
			if mean(order[0]) >= landSum/float64(landCells) {
				shields = 1
			}
		}
		for i, pr := range order {
			pr.Type = TypeSedimentary
			if i < shields {
				pr.Type = TypeShield
			}
		}
		// Draw in ID order so the stream does not depend on the ranking.
		for i := range group {
			group[i].AreaKM2 = float64(group[i].Cells) * cellKM2
			if rng.Float64() < p.VolcanicChance {
				group[i].Type = TypeVolcanic
			}
		}
	}
	return pf, owner, nil
}

// touched lists, in ascending order, the provinces under a shape's envelope.
func touched(dom Domain, cols, rows int, s Shape, owner []uint32) []uint32 {
	seen := map[uint32]bool{}
	visit(dom, cols, rows, s.Bounds(), func(col, row int, p Point) {
		if o := owner[row*cols+col]; !seen[o] && s.Contains(p) {
			seen[o] = true
		}
	})
	ids := make([]uint32, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// validate checks a loaded province file against its ownership raster.
func (pf *ProvinceFile) validate(owner []uint32) error {
	if pf.Format != ProvincesFormat {
		return fmt.Errorf("unsupported provinces format %q", pf.Format)
	}
	cells := make([]int, len(pf.Provinces)+1)
	for _, o := range owner {
		if o == 0 || int(o) > len(pf.Provinces) {
			return fmt.Errorf("ownership raster names province %d of %d", o, len(pf.Provinces))
		}
		cells[o]++
	}
	for i, pr := range pf.Provinces {
		if pr.ID != uint32(i+1) || pr.Cells != cells[i+1] {
			return fmt.Errorf("province %d does not match the ownership raster", pr.ID)
		}
		if !slices.Contains([]string{TypeShield, TypeSedimentary, TypeVolcanic, TypeOceanic}, pr.Type) || (pr.Type == TypeOceanic) != (pr.Component == 0) {
			return fmt.Errorf("province %d has an invalid type or component", pr.ID)
		}
	}
	return nil
}

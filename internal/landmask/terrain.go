// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package landmask

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"image/png"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"slices"

	"github.com/maloquacious/mapgen/internal/artifact"
	"github.com/maloquacious/mapgen/internal/continent"
	"github.com/maloquacious/mapgen/internal/fracture"
	"github.com/maloquacious/mapgen/internal/structure"
	"github.com/maloquacious/mapgen/internal/terrain"
	"github.com/maloquacious/mapgen/internal/trench"
)

// The terrain-input mode freezes the coastline from a terrain artifact. See
// "Landmask integration" in the terrain design and section 7 of the
// cylindrical specification.
const (
	TerrainFormat    = "mapgen-landmask-terrain-v2"
	InputContinents  = "continents"
	InputTerrain     = "terrain"
	TerrainSelection = "lowest cells-k by elevation, ties by index, the level shifted to 0 m; the largest eight-connected torus component grown to cells-k by elevation then index"
	TerrainDatum     = "sea level: 0 m; ocean at most ocean_max_m, every other cell at least land_min_m"
	DiagnosticsFile  = "diagnostics.json"
)

// InputKind reports whether dir holds a continental or a terrain artifact,
// by the format of its manifest.
func InputKind(dir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, ManifestFile))
	if err != nil {
		return "", err
	}
	var m struct {
		Format string `json:"format"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return "", fmt.Errorf("%s: %w", ManifestFile, err)
	}
	switch m.Format {
	case continent.Format:
		return InputContinents, nil
	case terrain.Format:
		return InputTerrain, nil
	}
	return "", fmt.Errorf("input format %q is neither %q nor %q", m.Format, continent.Format, terrain.Format)
}

// TerrainInput identifies the terrain checkpoint by content, not location.
type TerrainInput struct {
	ManifestSHA256 string           `json:"manifest_sha256"`
	Manifest       terrain.Manifest `json:"manifest"`
}

// TerrainManifest describes a terrain-input landmask artifact.
type TerrainManifest struct {
	Format             string         `json:"format"`
	GeneratorVersion   string         `json:"generator_version"`
	InputKind          string         `json:"input_kind"`
	Input              TerrainInput   `json:"input"`
	Grid               continent.Grid `json:"grid"`
	Topology           string         `json:"topology"`
	Datum              string         `json:"datum"`
	LandFraction       float64        `json:"land_fraction"`
	ActualLandFraction float64        `json:"actual_land_fraction"`
	// LandCells counts the non-ocean cells, inland depressions included.
	LandCells  int         `json:"land_cells"`
	Selection  string      `json:"selection"`
	Parameters OceanParams `json:"parameters"`
	// SeaLevelM is the elevation, relative to terrain's provisional datum,
	// moved to 0 m. Elevations in ridges.json remain relative to the
	// provisional datum.
	SeaLevelM      float64 `json:"sea_level_m"`
	RepairFraction float64 `json:"repair_fraction"`
	// Recentering records this stage's translation, relative to the
	// terrain input; it is always present.
	Recentering structure.Recentering `json:"recentering"`
	// RecenteredRecords describes the retained structure and trench records
	// when this stage moved them: the trench manifest the terrain input's
	// records had, as if the trench stage had recentered by the combined
	// shift. It is absent when the records are the input's bytes.
	RecenteredRecords *trench.Manifest `json:"recentered_records,omitzero"`
	Mask              MaskField        `json:"mask"`
	// Retained lists the structure and trench records and ridges.json,
	// copied unchanged from the terrain input.
	Retained    []structure.File  `json:"retained"`
	Diagnostics structure.File    `json:"diagnostics"`
	Fields      []continent.Field `json:"fields"`
}

// TerrainDiagnostics are derived, reviewable facts about the selection.
type TerrainDiagnostics struct {
	OceanCells       int     `json:"ocean_cells"`
	OceanFraction    float64 `json:"ocean_fraction"`
	NonOceanCells    int     `json:"non_ocean_cells"`
	NonOceanFraction float64 `json:"non_ocean_fraction"`
	// DepressionCells are non-ocean cells at or below sea level before
	// clamping: low ground cut off from the ocean, where drainage may
	// place lakes. The rest is dry land.
	DepressionCells    int     `json:"depression_cells"`
	DepressionFraction float64 `json:"depression_fraction"`
	DryLandCells       int     `json:"dry_land_cells"`
	DryLandFraction    float64 `json:"dry_land_fraction"`
	// QuantileComponents counts the eight-connected components of the
	// quantile ocean; LargestComponentCells is the one grown into the ocean.
	QuantileComponents    int   `json:"quantile_components"`
	LargestComponentCells int   `json:"largest_component_cells"`
	RepairCells           int   `json:"repair_cells"`
	Clamp                 Clamp `json:"clamp"`
	// WaterToLand and LandToWater count the cells that changed sides of the
	// provisional coast.
	WaterToLand int `json:"water_to_land"`
	LandToWater int `json:"land_to_water"`
}

// TerrainResult is a terrain-input landmask: the frozen ocean, the clamped
// fields relative to sea level, and the retained records.
type TerrainResult struct {
	Manifest    TerrainManifest
	Land        []byte // 1 for non-ocean, 0 for ocean
	Elevation   []float32
	High        []float32
	Relief      []float32
	Structure   *structure.Result
	Trenches    *trench.TrenchFile
	Ridges      *terrain.RidgeFile
	Diagnostics TerrainDiagnostics
	// source is the directory the retained files are copied from, after
	// their checksums are verified again. It is never recorded.
	source string
}

var terrainFieldDescriptions = map[string]string{
	terrain.FieldElevation: "ElevationM: mean solid surface elevation, m, relative to sea level; ocean at most -1 m, other cells at least +1 m",
	terrain.FieldHigh:      "HighM: representative high elevation, m, relative to sea level, at least ElevationM",
	terrain.FieldRelief:    "ReliefM: representative high minus valley, m, at least 0",
}

// GenerateFromTerrain reads and verifies a terrain artifact and freezes the
// ocean leaving round(fraction*cells) non-ocean cells. A nil fraction uses
// the terrain's provisional land fraction. Input files are never changed.
//
// With recenter, the result is then translated by whole cells to center
// the largest eight-connected landmass of the frozen mask, records
// included, as terrain's --recenter moves its own; nothing is reselected.
func GenerateFromTerrain(ctx context.Context, dir string, fraction *float64, recenter bool, version string) (*TerrainResult, error) {
	data, err := os.ReadFile(filepath.Join(dir, ManifestFile))
	if err != nil {
		return nil, err
	}
	tr, err := terrain.Load(ctx, dir)
	if err != nil {
		return nil, fmt.Errorf("input: %w", err)
	}
	// The recorded checksum must describe the manifest that was used.
	var read terrain.Manifest
	if err := json.Unmarshal(data, &read); err != nil {
		return nil, err
	}
	if a, b := marshal(read), marshal(tr.Manifest); a == nil || !bytes.Equal(a, b) {
		return nil, fmt.Errorf("input: the manifest changed while it was read")
	}
	tm := tr.Manifest
	fr := tm.ProvisionalLandFraction
	if fraction != nil {
		fr = *fraction
	}
	if !(fr >= 0 && fr <= 1) {
		return nil, fmt.Errorf("land-fraction must be in [0,1]")
	}
	retained := append(slices.Clone(tm.Retained), tm.Ridges)
	if _, err := trench.ReadRecords(dir, []structure.File{tm.Ridges}); err != nil {
		return nil, fmt.Errorf("input: %w", err)
	}
	g := tm.Grid
	n := g.Columns * g.Rows
	k := int(math.Round(fr * float64(n)))
	p := DefaultOceanParams()
	s, err := selectOcean(ctx, p, tr.Elevation, tr.High, g.Columns, g.Rows, k)
	if err != nil {
		return nil, err
	}
	r := &TerrainResult{
		Manifest: TerrainManifest{
			Format: TerrainFormat, GeneratorVersion: version, InputKind: InputTerrain,
			Input: TerrainInput{ManifestSHA256: checksum(data), Manifest: tm},
			Grid:  g, Topology: tm.Topology, Datum: TerrainDatum,
			LandFraction: fr, ActualLandFraction: float64(k) / float64(n), LandCells: k,
			Selection: TerrainSelection, Parameters: p,
			SeaLevelM: s.level, RepairFraction: float64(s.repair) / float64(n),
			Retained: retained,
		},
		Land: s.land, Elevation: tr.Elevation, High: tr.High, Relief: tr.Relief,
		Structure: tr.Structure, Trenches: tr.Trenches, Ridges: tr.Ridges, source: dir,
	}
	d := &r.Diagnostics
	d.NonOceanCells, d.OceanCells = k, n-k
	d.DepressionCells, d.DryLandCells = s.depressions, k-s.depressions
	d.NonOceanFraction, d.OceanFraction = float64(k)/float64(n), float64(n-k)/float64(n)
	d.DepressionFraction, d.DryLandFraction = float64(d.DepressionCells)/float64(n), float64(d.DryLandCells)/float64(n)
	d.QuantileComponents, d.LargestComponentCells, d.RepairCells = s.components, s.component, s.repair
	d.Clamp = s.clamp
	for i, l := range s.land {
		switch prov := tr.Structure.Land[i]; {
		case l == 1 && prov == 0:
			d.WaterToLand++
		case l == 0 && prov == 1:
			d.LandToWater++
		}
	}
	if recenter {
		if err := r.recenter(ctx); err != nil {
			return nil, err
		}
	}
	return r, ctx.Err()
}

// recenter translates r to center the largest landmass of the frozen mask.
func (r *TerrainResult) recenter(ctx context.Context) error {
	g := r.Manifest.Grid
	v := make([]float32, len(r.Land))
	for i, l := range r.Land {
		v[i] = float32(l)
	}
	f := &fracture.FractureField{Columns: g.Columns, Rows: g.Rows, WidthKM: g.WidthKM, HeightKM: g.HeightKM, Values: v}
	c, err := fracture.FindCentering(ctx, f, r.Manifest.ActualLandFraction)
	if err != nil {
		return fmt.Errorf("recenter: %w", err)
	}
	step := structure.Recentering{
		Applied: true, ShiftX: c.X, ShiftY: c.Y, ShiftXKM: float64(c.X) * g.CellWidthKM, ShiftYKM: float64(c.Y) * g.CellHeightKM,
		XAmbiguous: c.XAmbiguous, YAmbiguous: c.YAmbiguous, ComponentCells: c.ComponentCells, Components: c.Components,
		Transform: structure.RecenterTransform,
	}
	r.Manifest.Recentering = step
	if c.X == 0 && c.Y == 0 {
		return nil
	}
	tm, err := terrain.MoveRecords(ctx, r.Structure, r.Trenches, r.Ridges, r.Manifest.Input.Manifest.RecordsManifest(), step)
	if err != nil {
		return err
	}
	structure.Rotate(r.Land, g.Columns, g.Rows, c.X, c.Y)
	for _, v := range [][]float32{r.Elevation, r.High, r.Relief} {
		structure.Rotate(v, g.Columns, g.Rows, c.X, c.Y)
	}
	// WriteTerrain saves the moved records and fills in their checksums.
	r.Manifest.RecenteredRecords, r.source = &tm, ""
	return ctx.Err()
}

// marshal returns the canonical bytes of v, or nil if it cannot.
func marshal(v any) []byte {
	b, err := json.Marshal(v, json.Deterministic(true))
	if err != nil {
		return nil
	}
	return b
}

func (r *TerrainResult) field(name string) []float32 {
	switch name {
	case terrain.FieldElevation:
		return r.Elevation
	case terrain.FieldHigh:
		return r.High
	case terrain.FieldRelief:
		return r.Relief
	}
	return nil
}

// WriteTerrain publishes a complete artifact using a sibling temporary
// directory. An existing output is rejected. previewScale is 0 to omit the
// preview, or >=1 to sample every Nth cell.
func WriteTerrain(ctx context.Context, dir string, r *TerrainResult, previewScale int) error {
	if previewScale < 0 {
		return fmt.Errorf("preview scale must not be negative")
	}
	dir = filepath.Clean(dir)
	if _, err := os.Lstat(dir); err == nil {
		return fmt.Errorf("output already exists: %s", dir)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dir), ".landmask-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	m := r.Manifest
	g := m.Grid
	n := g.Columns * g.Rows
	m.Fields = nil
	for _, name := range terrain.Fields {
		v := r.field(name)
		if len(v) != n {
			return fmt.Errorf("field %q does not match the grid", name)
		}
		f := &fracture.FractureField{Columns: g.Columns, Rows: g.Rows, WidthKM: g.WidthKM, HeightKM: g.HeightKM, Values: v}
		fd, err := continent.WriteField(tmp, name, terrainFieldDescriptions[name], f)
		if err != nil {
			return err
		}
		m.Fields = append(m.Fields, fd)
	}
	if len(r.Land) != n {
		return fmt.Errorf("mask does not match the grid")
	}
	m.Mask = MaskField{File: MaskFile, DType: "uint8", SHA256: checksum(r.Land), Description: "row-major; 0=ocean, 1=non-ocean (land and inland depressions); the frozen ocean mask"}
	if err := os.WriteFile(filepath.Join(tmp, MaskFile), r.Land, 0o644); err != nil {
		return err
	}
	if m.RecenteredRecords != nil {
		// Moved records are written afresh and described anew.
		tm, err := terrain.WriteMovedRecords(tmp, r.Structure, r.Trenches, *m.RecenteredRecords)
		if err != nil {
			return err
		}
		rdata, err := r.Ridges.Marshal()
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(tmp, terrain.RidgesFile), rdata, 0o644); err != nil {
			return err
		}
		ridges := m.Input.Manifest.Ridges
		ridges.SHA256 = checksum(rdata)
		m.RecenteredRecords, m.Retained = &tm, append(tm.Records(), ridges)
	} else {
		for _, f := range m.Retained {
			b, err := trench.ReadRecords(r.source, []structure.File{f})
			if err != nil {
				return fmt.Errorf("retained record: %w", err)
			}
			if err := os.WriteFile(filepath.Join(tmp, f.File), b[f.File], 0o644); err != nil {
				return err
			}
		}
	}
	diagnostics, err := json.Marshal(r.Diagnostics, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return err
	}
	diagnostics = append(diagnostics, '\n')
	m.Diagnostics = structure.File{File: DiagnosticsFile, Format: "json", SHA256: checksum(diagnostics),
		Description: "ocean, non-ocean, depression, and dry land counts; quantile ocean components and the connectivity repair; the clamp's changes; cells that changed sides of the provisional coast"}
	manifest, err := json.Marshal(m, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return err
	}
	for _, f := range []struct {
		name string
		data []byte
	}{{DiagnosticsFile, diagnostics}, {ManifestFile, append(manifest, '\n')}} {
		if err := os.WriteFile(filepath.Join(tmp, f.name), f.data, 0o644); err != nil {
			return err
		}
	}
	if previewScale > 0 {
		land := make([]bool, n)
		for i, l := range r.Land {
			land[i] = l == 1
		}
		img, err := terrain.Hypsometric(ctx, g, r.Elevation, 0, land, previewScale)
		if err != nil {
			return err
		}
		f, err := os.Create(filepath.Join(tmp, PreviewFile))
		if err != nil {
			return err
		}
		if err := png.Encode(f, img); err != nil {
			_ = f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		return err
	}
	// Never replace a directory that appeared after the check above.
	if err := artifact.RenameNoReplace(tmp, dir); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("output already exists: %s", dir)
		}
		return err
	}
	return nil
}

// LoadTerrain reads a terrain-input landmask artifact and verifies its
// checksums, retained records, mask, and the invariants of the frozen
// coast: exactly land_cells non-ocean cells, one connected ocean, ocean at
// most ocean_max_m and other cells at least land_min_m. It needs no other
// directory.
func LoadTerrain(ctx context.Context, dir string) (*TerrainResult, error) {
	data, err := os.ReadFile(filepath.Join(dir, ManifestFile))
	if err != nil {
		return nil, err
	}
	var m TerrainManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", ManifestFile, err)
	}
	if m.Format != TerrainFormat || m.InputKind != InputTerrain {
		return nil, fmt.Errorf("unsupported landmask format %q, input %q", m.Format, m.InputKind)
	}
	p := m.Parameters
	if p.Algorithm != OceanAlgorithm || !(p.OceanMaxM < 0 && p.LandMinM > 0) {
		return nil, fmt.Errorf("unsupported ocean selection %q", p.Algorithm)
	}
	in := m.Input.Manifest
	g := m.Grid
	if err := g.Validate(); err != nil {
		return nil, err
	}
	if g != in.Grid {
		return nil, fmt.Errorf("grid does not match the input manifest")
	}
	// The records are the input's, or the input's moved by this stage's
	// recentering; ridges.json is the last retained file either way.
	c := m.Recentering
	moved := c.Applied && (c.ShiftX != 0 || c.ShiftY != 0)
	if !c.Applied && c != (structure.Recentering{}) {
		return nil, fmt.Errorf("recentering is not applied but records a translation")
	}
	if moved != (m.RecenteredRecords != nil) {
		return nil, fmt.Errorf("recentered records do not match the recentering record")
	}
	tm, ridgesFile := in.RecordsManifest(), in.Ridges
	if moved {
		if err := terrain.CheckMovedRecords(g, tm, *m.RecenteredRecords, c); err != nil {
			return nil, err
		}
		tm = *m.RecenteredRecords
		if len(m.Retained) > 0 {
			if last := m.Retained[len(m.Retained)-1]; last.File == ridgesFile.File {
				ridgesFile.SHA256 = last.SHA256
			}
		}
	}
	if !slices.Equal(m.Retained, append(tm.Records(), ridgesFile)) {
		return nil, fmt.Errorf("retained records do not match the input manifest")
	}
	tr, err := trench.LoadRecords(ctx, dir, tm)
	if err != nil {
		return nil, err
	}
	records, err := trench.ReadRecords(dir, []structure.File{ridgesFile})
	if err != nil {
		return nil, err
	}
	ridges, err := terrain.ParseRidges(records[terrain.RidgesFile])
	if err != nil {
		return nil, err
	}
	if err := ridges.Validate(tr.Structure.Features); err != nil {
		return nil, err
	}
	r := &TerrainResult{Manifest: m, Structure: tr.Structure, Trenches: tr.Trenches, Ridges: ridges, source: dir}
	if m.Diagnostics.File != DiagnosticsFile {
		return nil, fmt.Errorf("manifest names %q where %q is expected", m.Diagnostics.File, DiagnosticsFile)
	}
	ddata, err := os.ReadFile(filepath.Join(dir, DiagnosticsFile))
	if err != nil {
		return nil, err
	}
	if checksum(ddata) != m.Diagnostics.SHA256 {
		return nil, fmt.Errorf("%s: checksum mismatch", DiagnosticsFile)
	}
	if err := json.Unmarshal(ddata, &r.Diagnostics); err != nil {
		return nil, fmt.Errorf("%s: %w", DiagnosticsFile, err)
	}
	n := g.Columns * g.Rows
	if m.Mask.File != MaskFile || m.Mask.DType != "uint8" {
		return nil, fmt.Errorf("invalid landmask metadata")
	}
	land, err := os.ReadFile(filepath.Join(dir, MaskFile))
	if err != nil {
		return nil, err
	}
	if len(land) != n || checksum(land) != m.Mask.SHA256 {
		return nil, fmt.Errorf("landmask size or checksum mismatch")
	}
	count := 0
	for _, v := range land {
		if v > 1 {
			return nil, fmt.Errorf("invalid landmask value %d", v)
		}
		count += int(v)
	}
	if !(m.LandFraction >= 0 && m.LandFraction <= 1) || count != m.LandCells ||
		count != int(math.Round(m.LandFraction*float64(n))) || m.ActualLandFraction != float64(count)/float64(n) {
		return nil, fmt.Errorf("landmask count mismatch")
	}
	if !connected(land, g.Columns, g.Rows) {
		return nil, fmt.Errorf("the ocean is not one connected component")
	}
	r.Land = land
	if len(m.Fields) != len(terrain.Fields) {
		return nil, fmt.Errorf("fields do not match %v", terrain.Fields)
	}
	cm := &continent.Manifest{Grid: g, Fields: m.Fields}
	for i, name := range terrain.Fields {
		fd := m.Fields[i]
		if fd.Name != name || fd.File != name+".f32" || fd.Columns != g.Columns || fd.Rows != g.Rows {
			return nil, fmt.Errorf("invalid field %q", fd.Name)
		}
		f, err := continent.ReadField(dir, cm, name)
		if err != nil {
			return nil, err
		}
		for _, v := range f.Values {
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
				return nil, fmt.Errorf("nonfinite value in %s", name)
			}
		}
		switch name {
		case terrain.FieldElevation:
			r.Elevation = f.Values
		case terrain.FieldHigh:
			r.High = f.Values
		case terrain.FieldRelief:
			r.Relief = f.Values
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	for i, e := range r.Elevation {
		if land[i] == 1 && float64(e) < p.LandMinM || land[i] == 0 && float64(e) > p.OceanMaxM {
			return nil, fmt.Errorf("cell %d: elevation %g m is on the wrong side of the frozen coast", i, e)
		}
		if r.High[i] < e || r.Relief[i] < 0 {
			return nil, fmt.Errorf("cell %d: high below elevation or negative relief", i)
		}
	}
	return r, nil
}

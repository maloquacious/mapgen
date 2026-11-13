// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package terrain creates, stores, and loads the terrain artifact: ground and
// seabed elevation from an accepted trench checkpoint, which carries the
// structure records and the trench records. See "Terrain Generation
// Design.md".
//
// The domain is a torus, as in every stage before climate. Elevations are
// meters relative to a provisional datum, the provisional coast at 0 m, not
// sea level; landmask chooses sea level later.
//
// So far it builds the continental foundation, the surface with no
// features, and publishes it with the structure and trench records it
// retains.
// Feature synthesis, ridges, subcell rasterization, residual detail, and
// recentering are later steps of the design.
package terrain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"slices"

	"github.com/maloquacious/mapgen/internal/artifact"
	"github.com/maloquacious/mapgen/internal/continent"
	"github.com/maloquacious/mapgen/internal/fracture"
	"github.com/maloquacious/mapgen/internal/prng"
	"github.com/maloquacious/mapgen/internal/structure"
	"github.com/maloquacious/mapgen/internal/trench"
)

const (
	Format          = "mapgen-terrain-v2"
	Topology        = "torus: x and y wrap; the map may be recentered until climate"
	Datum           = "provisional: 0 m is the provisional coast of the structure records, not sea level"
	Absent          = "absent"
	SeedExplicit    = "explicit"
	SeedFromInput   = "trench input"
	ManifestFile    = "manifest.json"
	DiagnosticsFile = "diagnostics.json"
	PreviewFile     = "preview.png"

	FieldElevation = "elevation"
	FieldHigh      = "high"
	FieldRelief    = "relief"
)

// Fields names the saved terrain fields, in the order they are written.
var Fields = []string{FieldElevation, FieldHigh, FieldRelief}

var fieldDescriptions = map[string]string{
	FieldElevation: "ElevationM: mean solid surface elevation, m, relative to the provisional datum",
	FieldHigh:      "HighM: representative high elevation, m, at least ElevationM",
	FieldRelief:    "ReliefM: representative high minus valley, m, at least 0",
}

// File describes one saved file other than a terrain field.
type File = structure.File

// Input identifies the trench checkpoint by content, not location. Its
// manifest records the structure checkpoint in turn.
type Input struct {
	ManifestSHA256 string          `json:"manifest_sha256"`
	Manifest       trench.Manifest `json:"manifest"`
}

// Manifest describes a terrain artifact. It holds only canonical generation
// data, so identical inputs give identical bytes.
type Manifest struct {
	Format                  string           `json:"format"`
	GeneratorVersion        string           `json:"generator_version"`
	Input                   Input            `json:"input"`
	Seed                    uint64           `json:"seed,string"`
	SeedSource              string           `json:"seed_source"`
	SeedDerivation          string           `json:"seed_derivation"`
	PRNG                    string           `json:"prng"`
	Grid                    continent.Grid   `json:"grid"`
	Topology                string           `json:"topology"`
	Datum                   string           `json:"datum"`
	ProvisionalLandFraction float64          `json:"provisional_land_fraction"`
	ProvisionalLandCells    int              `json:"provisional_land_cells"`
	FoundationParameters    FoundationParams `json:"foundation_parameters"`
	// FeatureSynthesis is Absent until features are rasterized: the
	// surface is the foundation alone, so HighM equals ElevationM and
	// ReliefM is 0.
	FeatureSynthesis string `json:"feature_synthesis"`
	// Retained lists the structure and trench records copied unchanged, as
	// the input manifest describes them.
	Retained    []File            `json:"retained"`
	Diagnostics File              `json:"diagnostics"`
	Fields      []continent.Field `json:"fields"`
}

// Stats summarizes a field over a set of cells.
type Stats struct {
	Cells int     `json:"cells"`
	MinM  float64 `json:"min_m"`
	MaxM  float64 `json:"max_m"`
	MeanM float64 `json:"mean_m"`
}

// Shift is what landmask's quantile selection would do at the provisional
// land fraction: the elevation it would move to 0 m, and how many cells
// would change sides of the provisional coast.
type Shift struct {
	LandCells   int     `json:"land_cells"`
	LevelM      float64 `json:"level_m"`
	WaterToLand int     `json:"water_to_land"`
	LandToWater int     `json:"land_to_water"`
}

// Diagnostics are derived, reviewable facts about a run, with no timings.
type Diagnostics struct {
	ElevationLand  Stats `json:"elevation_land"`
	ElevationWater Stats `json:"elevation_water"`
	ReliefLand     Stats `json:"relief_land"`
	ReliefWater    Stats `json:"relief_water"`
	Shift          Shift `json:"shift"`
}

// Result keeps everything in the input's coordinates.
type Result struct {
	Manifest  Manifest
	Structure *structure.Result  // retained records; no continental fields
	Trenches  *trench.TrenchFile // retained trench records
	Elevation []float32
	High      []float32
	Relief    []float32
	// Above marks the cells landmask's quantile selection would make land
	// at the provisional fraction. It is derived, not saved.
	Above       []bool
	Diagnostics Diagnostics
	records     map[string][]byte // retained files, by name, as read
}

// Options are the inputs to Generate.
type Options struct {
	In      string  // trench artifact directory
	Seed    *uint64 // terrain seed; nil uses the trench seed
	Version string  // generator version
}

func checksum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// Generate reads and verifies the trench input and builds the terrain.
// The input is never changed and its path is not recorded.
func Generate(ctx context.Context, o Options) (*Result, error) {
	data, err := os.ReadFile(filepath.Join(o.In, trench.ManifestFile))
	if err != nil {
		return nil, err
	}
	var tm trench.Manifest
	if err := trench.CheckFormat(data); err != nil {
		return nil, fmt.Errorf("input: %w", err)
	}
	if err := json.Unmarshal(data, &tm); err != nil {
		return nil, fmt.Errorf("input %s: %w", trench.ManifestFile, err)
	}
	tr, err := trench.LoadRecords(ctx, o.In, tm)
	if err != nil {
		return nil, fmt.Errorf("input: %w", err)
	}
	records, err := trench.ReadRecords(o.In, tm.Records())
	if err != nil {
		return nil, fmt.Errorf("input: %w", err)
	}
	s, sm := tr.Structure, tm.Input.Manifest
	seed, source := tm.Seed, SeedFromInput
	if o.Seed != nil {
		seed, source = *o.Seed, SeedExplicit
	}
	g := sm.Grid
	p := DefaultFoundationParams()
	r := &Result{
		Manifest: Manifest{
			Format: Format, GeneratorVersion: o.Version,
			Input: Input{ManifestSHA256: checksum(data), Manifest: tm},
			Seed:  seed, SeedSource: source, SeedDerivation: prng.DerivationVersion, PRNG: prng.Algorithm,
			Grid: g, Topology: Topology, Datum: Datum,
			ProvisionalLandFraction: sm.ProvisionalLandFraction, ProvisionalLandCells: sm.ProvisionalLandCells,
			FoundationParameters: p, FeatureSynthesis: Absent, Retained: tm.Records(),
		},
		Structure: s, Trenches: tr.Trenches, records: records,
	}
	dist, err := coastDistance(ctx, s.Land, g.Columns, g.Rows, g.CellWidthKM, g.CellHeightKM)
	if err != nil {
		return nil, err
	}
	fd, err := buildFoundation(ctx, seed, p, g, s.Provinces, s.Owner)
	if err != nil {
		return nil, err
	}
	if r.Elevation, err = fd.elevation(ctx, p, g, dist); err != nil {
		return nil, err
	}
	r.High = slices.Clone(r.Elevation)
	r.Relief = make([]float32, len(r.Elevation))
	r.diagnose()
	return r, ctx.Err()
}

// diagnose derives Above and the diagnostics from the fields.
func (r *Result) diagnose() {
	land := r.Structure.Land
	k := r.Manifest.ProvisionalLandCells
	r.Above = fracture.TopMask(r.Elevation, k)
	stats := func(v []float32, want byte) Stats {
		s := Stats{MinM: math.Inf(1), MaxM: math.Inf(-1)}
		sum := 0.0
		for i, x := range v {
			if land[i] == want {
				s.Cells++
				s.MinM, s.MaxM = min(s.MinM, float64(x)), max(s.MaxM, float64(x))
				sum += float64(x)
			}
		}
		if s.Cells == 0 {
			return Stats{}
		}
		s.MeanM = sum / float64(s.Cells)
		return s
	}
	d := &r.Diagnostics
	d.ElevationLand, d.ElevationWater = stats(r.Elevation, 1), stats(r.Elevation, 0)
	d.ReliefLand, d.ReliefWater = stats(r.Relief, 1), stats(r.Relief, 0)
	// The level is midway between the lowest cell selected and the highest
	// left out; with every cell on one side, it is that side's extreme.
	lowIn, highOut := math.Inf(1), math.Inf(-1)
	d.Shift = Shift{LandCells: k}
	for i, a := range r.Above {
		e := float64(r.Elevation[i])
		if a {
			lowIn = min(lowIn, e)
		} else {
			highOut = max(highOut, e)
		}
		switch {
		case a && land[i] == 0:
			d.Shift.WaterToLand++
		case !a && land[i] == 1:
			d.Shift.LandToWater++
		}
	}
	switch {
	case math.IsInf(lowIn, 1) && math.IsInf(highOut, -1):
	case math.IsInf(lowIn, 1):
		d.Shift.LevelM = highOut
	case math.IsInf(highOut, -1):
		d.Shift.LevelM = lowIn
	default:
		d.Shift.LevelM = (lowIn + highOut) / 2
	}
}

// Write publishes a complete artifact using a sibling temporary directory.
// An existing output is rejected so accepted checkpoints are not
// overwritten. previewScale is 0 to omit the previews, or >=1 to sample
// every Nth cell.
func Write(ctx context.Context, dir string, r *Result, previewScale int) error {
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
	tmp, err := os.MkdirTemp(filepath.Dir(dir), ".terrain-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	m := r.Manifest
	g := m.Grid
	n := g.Columns * g.Rows
	m.Fields = nil
	for _, name := range Fields {
		v := r.field(name)
		if len(v) != n {
			return fmt.Errorf("terrain field %q does not match the grid", name)
		}
		f := &fracture.FractureField{Columns: g.Columns, Rows: g.Rows, WidthKM: g.WidthKM, HeightKM: g.HeightKM, Values: v}
		fd, err := continent.WriteField(tmp, name, fieldDescriptions[name], f)
		if err != nil {
			return err
		}
		m.Fields = append(m.Fields, fd)
	}
	for _, f := range m.Retained {
		b, ok := r.records[f.File]
		if !ok || checksum(b) != f.SHA256 {
			return fmt.Errorf("retained record %s is missing or changed", f.File)
		}
		if err := os.WriteFile(filepath.Join(tmp, f.File), b, 0o644); err != nil {
			return err
		}
	}
	diagnostics, err := json.Marshal(r.Diagnostics, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return err
	}
	diagnostics = append(diagnostics, '\n')
	m.Diagnostics = File{File: DiagnosticsFile, Format: "json", SHA256: checksum(diagnostics),
		Description: "elevation and relief statistics on provisional land and water, and the datum shift at the provisional fraction"}
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
		if err := writePreview(ctx, filepath.Join(tmp, PreviewFile), r, previewScale); err != nil {
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

// field returns the named terrain field.
func (r *Result) field(name string) []float32 {
	switch name {
	case FieldElevation:
		return r.Elevation
	case FieldHigh:
		return r.High
	case FieldRelief:
		return r.Relief
	}
	return nil
}

// Load reads an artifact and verifies its checksums, retained records, and
// fields. It needs no other directory.
func Load(ctx context.Context, dir string) (*Result, error) {
	data, err := os.ReadFile(filepath.Join(dir, ManifestFile))
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", ManifestFile, err)
	}
	if m.Format != Format {
		return nil, fmt.Errorf("unsupported terrain format %q", m.Format)
	}
	if m.FeatureSynthesis != Absent {
		return nil, fmt.Errorf("unsupported feature synthesis %q", m.FeatureSynthesis)
	}
	tm := m.Input.Manifest
	if !slices.Equal(m.Retained, tm.Records()) {
		return nil, fmt.Errorf("retained records do not match the input manifest")
	}
	if m.Grid != tm.Grid || m.ProvisionalLandCells != tm.Input.Manifest.ProvisionalLandCells {
		return nil, fmt.Errorf("grid or provisional land does not match the input manifest")
	}
	tr, err := trench.LoadRecords(ctx, dir, tm)
	if err != nil {
		return nil, err
	}
	records, err := trench.ReadRecords(dir, m.Retained)
	if err != nil {
		return nil, err
	}
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
	r := &Result{Manifest: m, Structure: tr.Structure, Trenches: tr.Trenches, records: records}
	if err := json.Unmarshal(ddata, &r.Diagnostics); err != nil {
		return nil, fmt.Errorf("%s: %w", DiagnosticsFile, err)
	}
	if len(m.Fields) != len(Fields) {
		return nil, fmt.Errorf("terrain fields do not match %v", Fields)
	}
	g := m.Grid
	cm := &continent.Manifest{Grid: g, Fields: m.Fields}
	for i, name := range Fields {
		fd := m.Fields[i]
		if fd.Name != name || fd.File != name+".f32" || fd.Columns != g.Columns || fd.Rows != g.Rows {
			return nil, fmt.Errorf("invalid terrain field %q", fd.Name)
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
		case FieldElevation:
			r.Elevation = f.Values
		case FieldHigh:
			r.High = f.Values
		case FieldRelief:
			r.Relief = f.Values
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	for i, e := range r.Elevation {
		if r.High[i] < e || r.Relief[i] < 0 {
			return nil, fmt.Errorf("cell %d: high below elevation or negative relief", i)
		}
	}
	r.Above = fracture.TopMask(r.Elevation, m.ProvisionalLandCells)
	return r, nil
}

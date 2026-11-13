// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package structure creates, stores, and loads the geological structure
// artifact: the features terrain generation turns into mountains, plateaus,
// basins, and seabed. See "Structure Generation Design.md".
//
// The domain is a torus. Every stage before climate wraps in both directions
// so the map can be recentered without breaks.
//
// It also interprets the provisional coast (coast distance and coast chains
// on a working grid) for automatic placement to use.
//
// Features come from an explicit fixture file or from automatic placement.
// Rock classification converts features and provinces to geological proxies
// and classifies every cell; see rocks.go. Recentering is not implemented.
package structure

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"

	"github.com/maloquacious/mapgen/internal/artifact"
	"github.com/maloquacious/mapgen/internal/continent"
	"github.com/maloquacious/mapgen/internal/fracture"
	"github.com/maloquacious/mapgen/internal/prng"
)

const (
	Format          = "mapgen-structure-v1"
	Selection       = "highest-affinity; ties by input row-major index; round(fraction*cells)"
	Topology        = "torus: x and y wrap; the map may be recentered until climate"
	ModeFixture     = "fixture"
	SeedExplicit    = "explicit"
	SeedFromInput   = "continental input"
	Absent          = "absent"
	ManifestFile    = "manifest.json"
	MaskFile        = "provisional-land.u8"
	FeaturesFile    = "features.json"
	DiagnosticsFile = "diagnostics.json"
	PreviewFile     = "preview.png"
)

// Input identifies the continental checkpoint by content, not location.
type Input struct {
	ManifestSHA256 string             `json:"manifest_sha256"`
	Manifest       continent.Manifest `json:"manifest"`
}

// File describes one saved file other than a float32 field.
type File struct {
	File        string `json:"file"`
	Format      string `json:"format"`
	SHA256      string `json:"sha256"`
	Description string `json:"description"`
}

// Manifest describes a structure artifact. It holds only canonical
// generation data, so identical inputs give identical bytes.
type Manifest struct {
	Format                  string            `json:"format"`
	GeneratorVersion        string            `json:"generator_version"`
	Input                   Input             `json:"input"`
	Seed                    uint64            `json:"seed,string"`
	SeedSource              string            `json:"seed_source"`
	SeedDerivation          string            `json:"seed_derivation"`
	PRNG                    string            `json:"prng"`
	Grid                    continent.Grid    `json:"grid"`
	Topology                string            `json:"topology"`
	Mode                    string            `json:"mode"`
	ProvisionalLandFraction float64           `json:"provisional_land_fraction"`
	ProvisionalLandCells    int               `json:"provisional_land_cells"`
	ProvisionalSelection    string            `json:"provisional_selection"`
	ProvisionalLand         File              `json:"provisional_land"`
	Features                File              `json:"features"`
	FeatureCount            int               `json:"feature_count"`
	Provinces               File              `json:"provinces"`
	ProvinceOwnership       File              `json:"province_ownership"`
	ProvinceCount           int               `json:"province_count"`
	ProvinceParameters      ProvinceParams    `json:"province_parameters"`
	CoastParameters         CoastParams       `json:"coast_parameters"`
	PlacementParameters     *PlacementParams  `json:"placement_parameters,omitzero"`
	RockClassification      string            `json:"rock_classification"`
	RockParameters          *RockParams       `json:"rock_parameters,omitzero"`
	Rock                    *File             `json:"rock,omitzero"`
	GeologyFields           []continent.Field `json:"geology_fields,omitzero"`
	Diagnostics             File              `json:"diagnostics"`
	Fields                  []continent.Field `json:"fields"`
}

// Diagnostics are derived, reviewable facts about a run. They hold no
// timings, so they are as reproducible as the manifest.
type Diagnostics struct {
	Mode  string        `json:"mode"`
	Coast *CoastSummary `json:"coast,omitzero"`
	// Requested counts automatic placement's requests by family. Placed
	// counts the saved features by type.
	Requested map[string]int `json:"requested,omitzero"`
	Placed    map[string]int `json:"placed"`
	Omitted   []string       `json:"omitted"`
	Warnings  []string       `json:"warnings"`
	Rocks     *RockSummary   `json:"rocks,omitzero"`
}

// Result keeps everything in the same coordinates. In Land, 1 is provisional
// land and 0 provisional ocean; it is placement data, not a final landmask.
type Result struct {
	Manifest  Manifest
	Land      []byte
	Fields    map[string]*fracture.FractureField
	Features  *FeatureFile
	Provinces *ProvinceFile
	Owner     []uint32 // province ID of every cell, row-major
	// Geology holds the proxy fields and rock noise, by name; Rock holds a
	// Rock value per cell. Both are nil when rock classification is absent.
	Geology     map[string]*fracture.FractureField
	Rock        []byte
	Coast       *Coast // working-grid coast; nil in a loaded result
	Diagnostics Diagnostics
}

// Domain returns the physical extent of the artifact.
func (r *Result) Domain() Domain {
	return Domain{WidthKM: r.Manifest.Grid.WidthKM, HeightKM: r.Manifest.Grid.HeightKM}
}

// Options are the inputs to Generate.
type Options struct {
	In       string  // continental artifact directory
	Seed     *uint64 // structure seed; nil uses the input's seed
	Fraction float64 // provisional land fraction, in [0,1]
	Features []byte  // contents of a feature fixture; nil places features automatically
	Version  string  // generator version
}

func checksum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// Generate reads and verifies the continental input, interprets it with a
// provisional mask, and attaches the fixture's features or places features
// automatically. The input is never
// changed, and neither its path nor the fixture's is recorded.
func Generate(ctx context.Context, o Options) (*Result, error) {
	if !(o.Fraction >= 0 && o.Fraction <= 1) {
		return nil, fmt.Errorf("provisional-land-fraction must be in [0,1]")
	}
	// Parse and checksum the same bytes.
	data, err := os.ReadFile(filepath.Join(o.In, continent.ManifestFile))
	if err != nil {
		return nil, err
	}
	m, err := continent.ParseManifest(data)
	if err != nil {
		return nil, err
	}
	g := m.Grid
	if err := g.Validate(); err != nil {
		return nil, fmt.Errorf("input: %w", err)
	}
	// This stage is a torus whatever the input's manifest says of later ones.
	g.Access = "x and y wrap (torus)"
	fields, err := continent.ReadFields(ctx, o.In, m)
	if err != nil {
		return nil, fmt.Errorf("input: %w", err)
	}
	dom := Domain{WidthKM: g.WidthKM, HeightKM: g.HeightKM}
	mode := ModeAutomatic
	ff := &FeatureFile{Format: FeaturesFormat, Domain: dom}
	if o.Features != nil {
		mode = ModeFixture
		if ff, err = ParseFeatures(o.Features); err != nil {
			return nil, err
		}
		if err := ff.Validate(dom); err != nil {
			return nil, err
		}
	}
	values := fields[continent.FieldAffinity].Values
	k := int(math.Round(o.Fraction * float64(len(values))))
	land := make([]byte, len(values))
	for i, isLand := range fracture.TopMask(values, k) {
		if isLand {
			land[i] = 1
		}
	}
	// The input's seed is a safe default: each stage draws from streams keyed
	// by its own domain names, so sharing a seed does not correlate them.
	seed, source := m.Seed, SeedFromInput
	if o.Seed != nil {
		seed, source = *o.Seed, SeedExplicit
	}
	r := &Result{
		Manifest: Manifest{
			Format: Format, GeneratorVersion: o.Version,
			Input: Input{ManifestSHA256: checksum(data), Manifest: *m},
			Seed:  seed, SeedSource: source, SeedDerivation: prng.DerivationVersion, PRNG: prng.Algorithm,
			Grid: g, Topology: Topology, Mode: mode,
			ProvisionalLandFraction: o.Fraction, ProvisionalLandCells: k, ProvisionalSelection: Selection,
			RockClassification: Absent, ProvinceParameters: DefaultProvinceParams(),
			CoastParameters: DefaultCoastParams(),
		},
		Land: land, Fields: fields, Features: ff,
		Diagnostics: Diagnostics{Mode: mode, Placed: map[string]int{}, Omitted: []string{}},
	}
	// Provinces and the coast share one simplified mask, so specks and
	// puddles are neither provinces nor coastlines.
	mask, regions := simplify(r.Manifest.ProvinceParameters, g, land)
	r.Provinces, r.Owner, err = buildProvinces(ctx, seed, r.Manifest.ProvinceParameters, g, mask, regions, fields[continent.FieldContinental].Values)
	if err != nil {
		return nil, err
	}
	if r.Coast, err = interpretCoast(ctx, r.Manifest.CoastParameters, g, mask); err != nil {
		return nil, err
	}
	r.Diagnostics.Coast = r.Coast.Summary()
	if mode == ModeAutomatic {
		p := DefaultPlacementParams()
		r.Manifest.PlacementParameters = &p
		landKM2 := float64(k) * g.CellWidthKM * g.CellHeightKM
		if ff.Features, err = place(ctx, seed, p, dom, r.Coast, landKM2, &r.Diagnostics); err != nil {
			return nil, err
		}
		// Placement must only produce what a fixture could state.
		if err := ff.Validate(dom); err != nil {
			return nil, fmt.Errorf("automatic placement: %w", err)
		}
	}
	r.Manifest.FeatureCount = len(ff.Features)
	r.Diagnostics.Warnings = ff.Warnings()
	r.Manifest.ProvinceCount = len(r.Provinces.Provinces)
	// Province IDs in a fixture are not trusted; they are recomputed here.
	for i := range ff.Features {
		f := &ff.Features[i]
		f.Provinces = touched(r.Domain(), g.Columns, g.Rows, f.Shape(r.Domain()), r.Owner)
		r.Diagnostics.Placed[f.Type]++
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	rp := DefaultRockParams()
	geo, err := classifyRocks(ctx, seed, rp, g, ff, r.Provinces, r.Owner, r.Coast)
	if err != nil {
		return nil, err
	}
	r.Manifest.RockClassification, r.Manifest.RockParameters = RockAlgorithm, &rp
	r.Geology, r.Rock = geo.fields, geo.rock
	r.Diagnostics.Rocks = summarizeRocks(land, geo.rock)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r, nil
}

// Write publishes a complete artifact using a sibling temporary directory.
// An existing output is rejected so accepted checkpoints are not overwritten.
// previewScale is 0 to omit the previews, or >=1 to sample every Nth cell in
// preview.png. The coast preview has its own scale and is written only for a
// generated result.
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
	tmp, err := os.MkdirTemp(filepath.Dir(dir), ".structure-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	m := r.Manifest
	g := m.Grid
	m.Fields = nil
	for _, fd := range m.Input.Manifest.Fields {
		if err := ctx.Err(); err != nil {
			return err
		}
		f := r.Fields[fd.Name]
		if f == nil || f.Columns != g.Columns || f.Rows != g.Rows {
			return fmt.Errorf("result field %q is missing or does not match the grid", fd.Name)
		}
		field, err := continent.WriteField(tmp, fd.Name, fd.Description, f)
		if err != nil {
			return err
		}
		m.Fields = append(m.Fields, field)
	}
	m.GeologyFields, m.Rock = nil, nil
	if r.Rock != nil {
		for _, name := range GeologyFields {
			f := r.Geology[name]
			if f == nil || f.Columns != g.Columns || f.Rows != g.Rows {
				return fmt.Errorf("geology field %q is missing or does not match the grid", name)
			}
			field, err := continent.WriteField(tmp, name, geologyDescriptions[name], f)
			if err != nil {
				return err
			}
			m.GeologyFields = append(m.GeologyFields, field)
		}
		if len(r.Rock) != g.Columns*g.Rows {
			return fmt.Errorf("rock raster does not match the grid")
		}
		m.Rock = &File{File: RockFile, Format: "uint8", SHA256: checksum(r.Rock),
			Description: "row-major; 0=unclassified, 1=sedimentary, 2=igneous, 3=metamorphic"}
		if err := os.WriteFile(filepath.Join(tmp, RockFile), r.Rock, 0o644); err != nil {
			return err
		}
	}
	if len(r.Land) != g.Columns*g.Rows || len(r.Owner) != len(r.Land) {
		return fmt.Errorf("provisional mask or province ownership does not match the grid")
	}
	provinces, err := r.Provinces.Marshal()
	if err != nil {
		return err
	}
	ownership := make([]byte, 0, 4*len(r.Owner))
	for _, o := range r.Owner {
		ownership = binary.LittleEndian.AppendUint32(ownership, o)
	}
	m.Provinces = File{File: ProvincesFile, Format: ProvincesFormat, SHA256: checksum(provinces),
		Description: "geological province records"}
	m.ProvinceOwnership = File{File: OwnershipFile, Format: "uint32 little-endian", SHA256: checksum(ownership),
		Description: "row-major; the province ID of every cell, never 0"}
	features, err := r.Features.Marshal()
	if err != nil {
		return err
	}
	diagnostics, err := json.Marshal(r.Diagnostics, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return err
	}
	diagnostics = append(diagnostics, '\n')
	m.ProvisionalLand = File{File: MaskFile, Format: "uint8", SHA256: checksum(r.Land),
		Description: "row-major; 0=provisional ocean, 1=provisional land; placement data, not the final landmask"}
	m.Features = File{File: FeaturesFile, Format: FeaturesFormat, SHA256: checksum(features),
		Description: "authoritative feature records; geometry in km, unwrapped on both axes"}
	m.Diagnostics = File{File: DiagnosticsFile, Format: "json", SHA256: checksum(diagnostics),
		Description: "derived coast interpretation, placement counts, omissions, and warnings"}
	manifest, err := json.Marshal(m, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return err
	}
	for _, f := range []struct {
		name string
		data []byte
	}{{MaskFile, r.Land}, {ProvincesFile, provinces}, {OwnershipFile, ownership}, {FeaturesFile, features}, {DiagnosticsFile, diagnostics}, {ManifestFile, append(manifest, '\n')}} {
		if err := os.WriteFile(filepath.Join(tmp, f.name), f.data, 0o644); err != nil {
			return err
		}
	}
	if previewScale > 0 {
		if err := writePreview(ctx, filepath.Join(tmp, PreviewFile), r, previewScale); err != nil {
			return err
		}
		if r.Rock != nil {
			if err := writeRockPreview(filepath.Join(tmp, RockPreviewFile), r, previewScale); err != nil {
				return err
			}
		}
		if r.Coast != nil {
			if err := writeCoastPreview(filepath.Join(tmp, CoastPreviewFile), r.Domain(), r.Coast); err != nil {
				return err
			}
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

// Load reads an artifact and verifies its checksums, mask, fields, and
// features. The result does not depend on the continental input directory.
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
		return nil, fmt.Errorf("unsupported structure format %q", m.Format)
	}
	g := m.Grid
	if err := g.Validate(); err != nil {
		return nil, err
	}
	read := func(f File, name string) ([]byte, error) {
		if f.File != name {
			return nil, fmt.Errorf("manifest names %q where %q is expected", f.File, name)
		}
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		if checksum(b) != f.SHA256 {
			return nil, fmt.Errorf("%s: checksum mismatch", name)
		}
		return b, nil
	}
	land, err := read(m.ProvisionalLand, MaskFile)
	if err != nil {
		return nil, err
	}
	count := 0
	for _, v := range land {
		if v > 1 {
			return nil, fmt.Errorf("invalid provisional mask value %d", v)
		}
		count += int(v)
	}
	if len(land) != g.Columns*g.Rows || count != m.ProvisionalLandCells {
		return nil, fmt.Errorf("provisional mask size or count mismatch")
	}
	fdata, err := read(m.Features, FeaturesFile)
	if err != nil {
		return nil, err
	}
	ff, err := ParseFeatures(fdata)
	if err != nil {
		return nil, err
	}
	if err := ff.Validate(Domain{WidthKM: g.WidthKM, HeightKM: g.HeightKM}); err != nil {
		return nil, err
	}
	if len(ff.Features) != m.FeatureCount {
		return nil, fmt.Errorf("feature count mismatch")
	}
	pdata, err := read(m.Provinces, ProvincesFile)
	if err != nil {
		return nil, err
	}
	var pf ProvinceFile
	if err := json.Unmarshal(pdata, &pf, json.RejectUnknownMembers(true)); err != nil {
		return nil, fmt.Errorf("%s: %w", ProvincesFile, err)
	}
	odata, err := read(m.ProvinceOwnership, OwnershipFile)
	if err != nil {
		return nil, err
	}
	if len(odata) != 4*len(land) || len(pf.Provinces) != m.ProvinceCount {
		return nil, fmt.Errorf("province ownership size or count mismatch")
	}
	owner := make([]uint32, len(land))
	for i := range owner {
		owner[i] = binary.LittleEndian.Uint32(odata[4*i:])
	}
	if err := pf.validate(owner); err != nil {
		return nil, err
	}
	ddata, err := read(m.Diagnostics, DiagnosticsFile)
	if err != nil {
		return nil, err
	}
	var d Diagnostics
	if err := json.Unmarshal(ddata, &d); err != nil {
		return nil, fmt.Errorf("%s: %w", DiagnosticsFile, err)
	}
	fields, err := continent.ReadFields(ctx, dir, &continent.Manifest{Grid: g, Fields: m.Fields})
	if err != nil {
		return nil, err
	}
	r := &Result{Manifest: m, Land: land, Fields: fields, Features: ff, Provinces: &pf, Owner: owner, Diagnostics: d}
	switch m.RockClassification {
	case Absent:
		if m.Rock != nil || m.GeologyFields != nil || m.RockParameters != nil {
			return nil, fmt.Errorf("rock classification is absent but rock data is listed")
		}
	case RockAlgorithm:
		if err := loadRocks(ctx, dir, r, read); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported rock classification %q", m.RockClassification)
	}
	return r, nil
}

// loadRocks reads the geology fields and rock raster of a classified
// artifact, and checks every cell's rock against its saved proxies.
func loadRocks(ctx context.Context, dir string, r *Result, read func(File, string) ([]byte, error)) error {
	m := &r.Manifest
	p := m.RockParameters
	if m.Rock == nil || p == nil || p.Algorithm != RockAlgorithm || p.Proxy != ProxyAlgorithm {
		return fmt.Errorf("rock classification %s is missing its raster or parameters", RockAlgorithm)
	}
	if len(m.GeologyFields) != len(GeologyFields) {
		return fmt.Errorf("geology fields do not match %v", GeologyFields)
	}
	cm := &continent.Manifest{Grid: m.Grid, Fields: m.GeologyFields}
	r.Geology = map[string]*fracture.FractureField{}
	for i, name := range GeologyFields {
		fd := m.GeologyFields[i]
		if fd.Name != name || fd.File != name+".f32" || fd.Columns != m.Grid.Columns || fd.Rows != m.Grid.Rows {
			return fmt.Errorf("invalid geology field %q", fd.Name)
		}
		f, err := continent.ReadField(dir, cm, name)
		if err != nil {
			return err
		}
		for _, v := range f.Values {
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
				return fmt.Errorf("nonfinite value in %s", name)
			}
		}
		r.Geology[name] = f
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	rock, err := read(*m.Rock, RockFile)
	if err != nil {
		return err
	}
	if len(rock) != len(r.Land) {
		return fmt.Errorf("rock raster size mismatch")
	}
	pr, sh, ig, rv := r.Geology[FieldPressure].Values, r.Geology[FieldShear].Values, r.Geology[FieldIgneous].Values, r.Geology[FieldRockNoise].Values
	for i, v := range rock {
		if Rock(v) != ClassifyRock(*p, float64(pr[i]), float64(sh[i]), float64(ig[i]), float64(rv[i])) {
			return fmt.Errorf("rock raster does not match the saved proxies at cell %d", i)
		}
	}
	r.Rock = rock
	return nil
}

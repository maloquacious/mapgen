// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package trench creates, stores, and loads the trench artifact: the
// polarity of every subduction zone and the ocean trenches beside active
// margins and island arcs, from an accepted structure checkpoint. See
// "Trench Generation Design.md".
//
// The domain is a torus, as in every stage before climate. The artifact
// retains the structure records, so terrain needs no other directory.
package trench

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/maloquacious/mapgen/internal/artifact"
	"github.com/maloquacious/mapgen/internal/continent"
	"github.com/maloquacious/mapgen/internal/prng"
	"github.com/maloquacious/mapgen/internal/structure"
)

const (
	Format          = "mapgen-trench-v1"
	Topology        = "torus: x and y wrap; the map may be recentered until climate"
	SeedExplicit    = "explicit"
	SeedFromInput   = "structure input"
	ManifestFile    = "manifest.json"
	TrenchesFile    = "trenches.json"
	DiagnosticsFile = "diagnostics.json"
	PreviewFile     = "preview.png"
	LandMask        = "the structure input's simplified provisional mask: land and water regions smaller than province_parameters.min_area_km2 flipped"
)

// File describes one saved file.
type File = structure.File

// Input identifies the structure checkpoint by content, not location.
type Input struct {
	ManifestSHA256 string             `json:"manifest_sha256"`
	Manifest       structure.Manifest `json:"manifest"`
}

// Manifest describes a trench artifact. It holds only canonical generation
// data, so identical inputs give identical bytes.
type Manifest struct {
	Format           string         `json:"format"`
	GeneratorVersion string         `json:"generator_version"`
	Input            Input          `json:"input"`
	Seed             uint64         `json:"seed,string"`
	SeedSource       string         `json:"seed_source"`
	SeedDerivation   string         `json:"seed_derivation"`
	PRNG             string         `json:"prng"`
	Grid             continent.Grid `json:"grid"`
	Topology         string         `json:"topology"`
	Parameters       Params         `json:"trench_parameters"`
	// LandMask says which land blocks trenches and counts as coast.
	LandMask        string `json:"land_mask"`
	Trenches        File   `json:"trenches"`
	SubductionCount int    `json:"subduction_count"`
	TrenchCount     int    `json:"trench_count"`
	// Retained lists the structure records copied unchanged, as the input
	// manifest describes them.
	Retained    []File `json:"retained"`
	Diagnostics File   `json:"diagnostics"`
}

// Records lists the files a later stage retains from a trench artifact, in
// a fixed order: the structure records, then trenches.json.
func (m *Manifest) Records() []File {
	return append(slices.Clone(m.Retained), m.Trenches)
}

// Result keeps everything in the input's coordinates.
type Result struct {
	Manifest    Manifest
	Structure   *structure.Result // retained records; no continental fields
	Trenches    *TrenchFile
	Diagnostics Diagnostics
	records     map[string][]byte // retained files, by name, as read
}

// Domain returns the physical extent of the artifact.
func (r *Result) Domain() structure.Domain {
	return structure.Domain{WidthKM: r.Manifest.Grid.WidthKM, HeightKM: r.Manifest.Grid.HeightKM}
}

// Options are the inputs to Generate.
type Options struct {
	In      string  // structure artifact directory
	Seed    *uint64 // trench seed; nil uses the structure seed
	Version string  // generator version
}

// CheckFormat reports whether a manifest's bytes name this artifact format,
// so a later stage given the wrong directory says so plainly.
func CheckFormat(manifest []byte) error {
	var head struct {
		Format string `json:"format"`
	}
	if err := json.Unmarshal(manifest, &head); err != nil {
		return fmt.Errorf("%s: %w", ManifestFile, err)
	}
	if head.Format != Format {
		return fmt.Errorf("format %q is not %q: expected a trench artifact", head.Format, Format)
	}
	return nil
}

func checksum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// Generate reads and verifies the structure input, decides polarity, and
// places trenches. The input is never changed and its path is not recorded.
func Generate(ctx context.Context, o Options) (*Result, error) {
	data, err := os.ReadFile(filepath.Join(o.In, structure.ManifestFile))
	if err != nil {
		return nil, err
	}
	var head struct {
		Format string `json:"format"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, fmt.Errorf("input %s: %w", structure.ManifestFile, err)
	}
	if head.Format != structure.Format {
		return nil, fmt.Errorf("input: format %q is not %q: expected a structure artifact", head.Format, structure.Format)
	}
	var sm structure.Manifest
	if err := json.Unmarshal(data, &sm); err != nil {
		return nil, fmt.Errorf("input %s: %w", structure.ManifestFile, err)
	}
	s, err := structure.LoadRecords(ctx, o.In, sm)
	if err != nil {
		return nil, fmt.Errorf("input: %w", err)
	}
	records, err := ReadRecords(o.In, sm.Records())
	if err != nil {
		return nil, fmt.Errorf("input: %w", err)
	}
	seed, source := sm.Seed, SeedFromInput
	if o.Seed != nil {
		seed, source = *o.Seed, SeedExplicit
	}
	g, p := sm.Grid, DefaultParams()
	r := &Result{
		Manifest: Manifest{
			Format: Format, GeneratorVersion: o.Version,
			Input: Input{ManifestSHA256: checksum(data), Manifest: sm},
			Seed:  seed, SeedSource: source, SeedDerivation: prng.DerivationVersion, PRNG: prng.Algorithm,
			Grid: g, Topology: Topology, Parameters: p, LandMask: LandMask, Retained: sm.Records(),
		},
		Structure: s, records: records,
	}
	dom := r.Domain()
	// Specks and puddles neither block a trench nor count as coast, as
	// they are neither coast nor provinces in structure.
	mask := structure.Simplify(sm.ProvinceParameters, g, s.Land)
	r.Trenches, err = build(ctx, seed, p, newGrid(dom, g.Columns, g.Rows, mask), s.Features, &r.Diagnostics)
	if err != nil {
		return nil, err
	}
	// Placement must only produce what Load accepts.
	if err := r.Trenches.Validate(s.Features); err != nil {
		return nil, fmt.Errorf("trench placement: %w", err)
	}
	r.Manifest.SubductionCount, r.Manifest.TrenchCount = len(r.Trenches.Subduction), len(r.Trenches.Trenches)
	return r, ctx.Err()
}

// ReadRecords reads the named files of dir and checks their checksums.
func ReadRecords(dir string, files []File) (map[string][]byte, error) {
	out := map[string][]byte{}
	for _, f := range files {
		if filepath.Base(f.File) != f.File || f.File == "." || f.File == ".." {
			return nil, fmt.Errorf("invalid record file name %q", f.File)
		}
		b, err := os.ReadFile(filepath.Join(dir, f.File))
		if err != nil {
			return nil, err
		}
		if checksum(b) != f.SHA256 {
			return nil, fmt.Errorf("%s: checksum mismatch", f.File)
		}
		out[f.File] = b
	}
	return out, nil
}

// Write publishes a complete artifact using a sibling temporary directory.
// An existing output is rejected so accepted checkpoints are not
// overwritten. previewScale is 0 to omit the preview, or >=1 to sample
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
	tmp, err := os.MkdirTemp(filepath.Dir(dir), ".trench-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	m := r.Manifest
	for _, f := range m.Retained {
		b, ok := r.records[f.File]
		if !ok || checksum(b) != f.SHA256 {
			return fmt.Errorf("retained record %s is missing or changed", f.File)
		}
		if err := os.WriteFile(filepath.Join(tmp, f.File), b, 0o644); err != nil {
			return err
		}
	}
	trenches, err := r.Trenches.Marshal()
	if err != nil {
		return err
	}
	diagnostics, err := json.Marshal(r.Diagnostics, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return err
	}
	diagnostics = append(diagnostics, '\n')
	m.Trenches = File{File: TrenchesFile, Format: RecordsFormat, SHA256: checksum(trenches),
		Description: "subduction polarity and trench records; geometry in km, unwrapped on both axes"}
	m.Diagnostics = File{File: DiagnosticsFile, Format: "json", SHA256: checksum(diagnostics),
		Description: "subduction zones by basis, trench count and length, and omissions"}
	manifest, err := json.Marshal(m, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return err
	}
	for _, f := range []struct {
		name string
		data []byte
	}{{TrenchesFile, trenches}, {DiagnosticsFile, diagnostics}, {ManifestFile, append(manifest, '\n')}} {
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

// Load reads an artifact and verifies its checksums, retained records, and
// trench records. It needs no other directory.
func Load(ctx context.Context, dir string) (*Result, error) {
	data, err := os.ReadFile(filepath.Join(dir, ManifestFile))
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", ManifestFile, err)
	}
	r, err := LoadRecords(ctx, dir, m)
	if err != nil {
		return nil, err
	}
	if r.records, err = ReadRecords(dir, m.Retained); err != nil {
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
	if err := json.Unmarshal(ddata, &r.Diagnostics); err != nil {
		return nil, fmt.Errorf("%s: %w", DiagnosticsFile, err)
	}
	return r, nil
}

// LoadRecords reads, from dir, the records that a later stage's artifact
// retains, as m describes them: the structure records and the trench
// records, each verified. It does not read the diagnostics; the result's
// Diagnostics are empty.
func LoadRecords(ctx context.Context, dir string, m Manifest) (*Result, error) {
	if m.Format != Format {
		return nil, fmt.Errorf("unsupported trench format %q", m.Format)
	}
	if m.LandMask != LandMask {
		return nil, fmt.Errorf("unsupported land mask %q", m.LandMask)
	}
	if m.Parameters.Algorithm != Algorithm {
		return nil, fmt.Errorf("unsupported trench algorithm %q", m.Parameters.Algorithm)
	}
	sm := m.Input.Manifest
	if !slices.Equal(m.Retained, sm.Records()) {
		return nil, fmt.Errorf("retained records do not match the input manifest")
	}
	if m.Grid != sm.Grid {
		return nil, fmt.Errorf("grid does not match the input manifest")
	}
	s, err := structure.LoadRecords(ctx, dir, sm)
	if err != nil {
		return nil, err
	}
	if m.Trenches.File != TrenchesFile {
		return nil, fmt.Errorf("manifest names %q where %q is expected", m.Trenches.File, TrenchesFile)
	}
	tdata, err := os.ReadFile(filepath.Join(dir, TrenchesFile))
	if err != nil {
		return nil, err
	}
	if checksum(tdata) != m.Trenches.SHA256 {
		return nil, fmt.Errorf("%s: checksum mismatch", TrenchesFile)
	}
	tf, err := ParseTrenches(tdata)
	if err != nil {
		return nil, err
	}
	if err := tf.Validate(s.Features); err != nil {
		return nil, err
	}
	if len(tf.Subduction) != m.SubductionCount || len(tf.Trenches) != m.TrenchCount {
		return nil, fmt.Errorf("subduction or trench count mismatch")
	}
	return &Result{Manifest: m, Structure: s, Trenches: tf}, nil
}

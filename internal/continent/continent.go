// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package continent stores and loads the continental affinity artifact that
// later world generation stages consume.
//
// An artifact is a directory holding manifest.json and one raw raster file per
// field. Each raster is Columns*Rows little-endian IEEE-754 float32 values in
// row-major order (index = row*Columns + col), row 0 north, with no header.
// The manifest records dimensions, physical spacing, generation parameters,
// seed derivation, recentering, and a SHA-256 for every raster file.
package continent

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"

	"github.com/maloquacious/mapgen/internal/cerrs"
	"github.com/maloquacious/mapgen/internal/fracture"
	"github.com/maloquacious/mapgen/internal/prng"
)

const (
	// Format names the artifact layout.
	Format = "mapgen-continent-v1"

	ManifestFile = "manifest.json"

	// Field names.
	FieldAffinity    = "affinity"
	FieldContinental = "continental"
	FieldCoast       = "coast"

	ErrFormat   = cerrs.Error("unsupported artifact format")
	ErrNoField  = cerrs.Error("field not in manifest")
	ErrChecksum = cerrs.Error("checksum mismatch")
	ErrSize     = cerrs.Error("raster file has wrong size")
)

// Manifest describes an artifact.
type Manifest struct {
	Format           string     `json:"format"`
	GeneratorVersion string     `json:"generator_version"`
	Adaptation       string     `json:"adaptation"`
	Upstream         Upstream   `json:"upstream"`
	Seed             uint64     `json:"seed,string"`
	SeedDerivation   string     `json:"seed_derivation"`
	PRNG             string     `json:"prng"`
	Grid             Grid       `json:"grid"`
	Continental      Layer      `json:"continental"`
	Coast            Layer      `json:"coast"`
	CoastWeight      float32    `json:"coast_weight"`
	Recentering      Centering  `json:"recentering"`
	Fields           []Field    `json:"fields"`
	Diagnostics      []string   `json:"diagnostics"`
	TimingsMS        []TimingMS `json:"timings_ms"`
}

type Upstream struct {
	Repository string `json:"repository"`
	Commit     string `json:"commit"`
	File       string `json:"file"`
	License    string `json:"license"`
}

type Grid struct {
	Columns      int     `json:"columns"`
	Rows         int     `json:"rows"`
	WidthKM      float64 `json:"width_km"`
	HeightKM     float64 `json:"height_km"`
	CellWidthKM  float64 `json:"cell_width_km"`
	CellHeightKM float64 `json:"cell_height_km"`
	IndexOrder   string  `json:"index_order"`
	CellCenter   string  `json:"cell_center"`
	Access       string  `json:"access"`
}

type Layer struct {
	Domain         string  `json:"domain"`
	Bumps          int     `json:"bumps"`
	MinRadiusCells float64 `json:"min_radius_cells"`
	MaxRadiusCells float64 `json:"max_radius_cells"`
	MinRadiusKM    float64 `json:"min_radius_km"`
	MaxRadiusKM    float64 `json:"max_radius_km"`
	Falloff        float64 `json:"falloff"`
}

type Centering struct {
	Applied                 bool    `json:"applied"`
	ShiftX                  int     `json:"shift_x"`
	ShiftY                  int     `json:"shift_y"`
	XAmbiguous              bool    `json:"x_ambiguous"`
	YAmbiguous              bool    `json:"y_ambiguous"`
	ProvisionalLandFraction float64 `json:"provisional_land_fraction"`
	MaskCells               int     `json:"mask_cells"`
	ComponentCells          int     `json:"component_cells"`
	Components              int     `json:"components"`
}

type Field struct {
	Name        string  `json:"name"`
	File        string  `json:"file"`
	DType       string  `json:"dtype"`
	ByteOrder   string  `json:"byte_order"`
	Columns     int     `json:"columns"`
	Rows        int     `json:"rows"`
	SHA256      string  `json:"sha256"`
	Min         float64 `json:"min"`
	Max         float64 `json:"max"`
	Mean        float64 `json:"mean"`
	StdDev      float64 `json:"stddev"`
	Description string  `json:"description"`
}

type TimingMS struct {
	Stage string `json:"stage"`
	MS    int64  `json:"ms"`
}

// NewManifest describes res. Fields are filled in by Write.
func NewManifest(generatorVersion string, res *fracture.Result) *Manifest {
	p, c := res.Params, res.Centering
	f := res.Affinity
	layer := func(domain string, l fracture.Layer) Layer {
		return Layer{
			Domain: domain, Bumps: l.Bumps,
			MinRadiusCells: l.MinRadiusCells, MaxRadiusCells: l.MaxRadiusCells,
			MinRadiusKM: l.MinRadiusCells * p.CellKM, MaxRadiusKM: l.MaxRadiusCells * p.CellKM,
			Falloff: l.Falloff,
		}
	}
	return &Manifest{
		Format:           Format,
		GeneratorVersion: generatorVersion,
		Adaptation:       fracture.Version,
		Upstream: Upstream{
			Repository: fracture.UpstreamRepository,
			Commit:     fracture.UpstreamCommit,
			File:       fracture.UpstreamFile,
			License:    "AGPL-3.0-or-later",
		},
		Seed:           p.Seed,
		SeedDerivation: prng.DerivationVersion,
		PRNG:           prng.Algorithm,
		Grid: Grid{
			Columns: f.Columns, Rows: f.Rows,
			WidthKM: f.WidthKM, HeightKM: f.HeightKM,
			CellWidthKM: f.CellWidthKM(), CellHeightKM: f.CellHeightKM(),
			IndexOrder: "row-major: index = row*columns + col; row 0 is north",
			CellCenter: "((col+0.5)*cell_width_km, (row+0.5)*cell_height_km)",
			Access:     "x wraps; y clamps to first/last row center (cylinder)",
		},
		Continental: layer(fracture.DomainContinental, p.Continental),
		Coast:       layer(fracture.DomainCoast, p.Coast),
		CoastWeight: p.CoastWeight,
		Recentering: Centering{
			Applied: c.Applied,
			ShiftX:  c.X, ShiftY: c.Y,
			XAmbiguous: c.XAmbiguous, YAmbiguous: c.YAmbiguous,
			ProvisionalLandFraction: p.ProvisionalLandFraction,
			MaskCells:               c.MaskCells,
			ComponentCells:          c.ComponentCells,
			Components:              c.Components,
		},
	}
}

// Write stores the three fields of res and then the manifest in dir, each
// through a temporary file and rename. Fields are appended to m.
func Write(dir string, m *Manifest, res *fracture.Result) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	m.Fields = m.Fields[:0]
	for _, fd := range []struct {
		name, desc string
		f          *fracture.FractureField
	}{
		{FieldAffinity, "continental affinity: continental + coast_weight*coast (dimensionless, not elevation)", res.Affinity},
		{FieldContinental, "raw continental fracture field (integer valued)", res.Continental},
		{FieldCoast, "raw coast detail fracture field (integer valued)", res.Coast},
	} {
		field, err := WriteField(dir, fd.name, fd.desc, fd.f)
		if err != nil {
			return fmt.Errorf("%s: %w", fd.name, err)
		}
		if field.StdDev == 0 {
			m.Diagnostics = append(m.Diagnostics, fd.name+": field is flat (zero standard deviation)")
		}
		m.Fields = append(m.Fields, field)
	}
	data, err := json.Marshal(m, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return err
	}
	_, err = writeAtomic(filepath.Join(dir, ManifestFile), func(w io.Writer) error {
		_, err := w.Write(append(data, '\n'))
		return err
	})
	return err
}

// WriteField saves a float32 raster in an existing directory and describes it.
// name must be a simple filename stem, without directory components.
func WriteField(dir, name, description string, f *fracture.FractureField) (Field, error) {
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name {
		return Field{}, fmt.Errorf("invalid field name %q", name)
	}
	file := name + ".f32"
	sum, err := writeAtomic(filepath.Join(dir, file), func(w io.Writer) error { return encode(w, f.Values) })
	if err != nil {
		return Field{}, err
	}
	st := Stats(f.Values)
	return Field{
		Name: name, File: file, DType: "float32", ByteOrder: "little-endian",
		Columns: f.Columns, Rows: f.Rows, SHA256: sum,
		Min: st.Min, Max: st.Max, Mean: st.Mean, StdDev: st.StdDev,
		Description: description,
	}, nil
}

// ReadManifest loads dir/manifest.json.
func ReadManifest(dir string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, ManifestFile))
	if err != nil {
		return nil, err
	}
	return ParseManifest(data)
}

// ParseManifest decodes the contents of a manifest.json file. Callers that
// also checksum the manifest should hash the same bytes they parse.
func ParseManifest(data []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", ManifestFile, err)
	}
	if m.Format != Format {
		return nil, fmt.Errorf("%w: %q", ErrFormat, m.Format)
	}
	return &m, nil
}

// ReadField loads and verifies one field of the artifact in dir.
func ReadField(dir string, m *Manifest, name string) (*fracture.FractureField, error) {
	var fd *Field
	for i := range m.Fields {
		if m.Fields[i].Name == name {
			fd = &m.Fields[i]
		}
	}
	if fd == nil {
		return nil, fmt.Errorf("%w: %q", ErrNoField, name)
	}
	if fd.DType != "float32" || fd.ByteOrder != "little-endian" {
		return nil, fmt.Errorf("%w: %s is %s %s", ErrFormat, name, fd.ByteOrder, fd.DType)
	}
	data, err := os.ReadFile(filepath.Join(dir, fd.File))
	if err != nil {
		return nil, err
	}
	n := fd.Columns * fd.Rows
	if fd.Columns <= 0 || fd.Rows <= 0 || len(data) != 4*n {
		return nil, fmt.Errorf("%w: %s is %d bytes, want %d", ErrSize, fd.File, len(data), 4*n)
	}
	if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != fd.SHA256 {
		return nil, fmt.Errorf("%w: %s", ErrChecksum, fd.File)
	}
	values := make([]float32, n)
	for i := range values {
		values[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[4*i:]))
	}
	return &fracture.FractureField{
		Columns: fd.Columns, Rows: fd.Rows,
		WidthKM: m.Grid.WidthKM, HeightKM: m.Grid.HeightKM,
		Values: values,
	}, nil
}

// Load reads the affinity field from dir and returns it as a ContinentSource.
func Load(dir string) (*fracture.ContinentSource, *Manifest, error) {
	m, err := ReadManifest(dir)
	if err != nil {
		return nil, nil, err
	}
	f, err := ReadField(dir, m, FieldAffinity)
	if err != nil {
		return nil, nil, err
	}
	return fracture.NewContinentSource(f), m, nil
}

// FieldStats summarizes a raster in fixed order.
type FieldStats struct {
	Min, Max, Mean, StdDev float64
}

// Stats returns min, max, mean and population standard deviation.
func Stats(values []float32) FieldStats {
	if len(values) == 0 {
		return FieldStats{}
	}
	st := FieldStats{Min: math.Inf(1), Max: math.Inf(-1)}
	var sum float64
	for _, v := range values {
		x := float64(v)
		st.Min, st.Max = min(st.Min, x), max(st.Max, x)
		sum += x
	}
	st.Mean = sum / float64(len(values))
	var ss float64
	for _, v := range values {
		d := float64(v) - st.Mean
		ss += d * d
	}
	st.StdDev = math.Sqrt(ss / float64(len(values)))
	return st
}

func encode(w io.Writer, values []float32) error {
	buf := make([]byte, 0, 1<<16)
	for _, v := range values {
		buf = binary.LittleEndian.AppendUint32(buf, math.Float32bits(v))
		if len(buf) == cap(buf) {
			if _, err := w.Write(buf); err != nil {
				return err
			}
			buf = buf[:0]
		}
	}
	_, err := w.Write(buf)
	return err
}

// writeAtomic writes path through a temporary file in the same directory and
// renames it into place. It returns the hex SHA-256 of the bytes written.
func writeAtomic(path string, fill func(io.Writer) error) (sum string, err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	h := sha256.New()
	bw := bufio.NewWriterSize(io.MultiWriter(tmp, h), 1<<20)
	if err = fill(bw); err != nil {
		return "", err
	}
	if err = bw.Flush(); err != nil {
		return "", err
	}
	if err = tmp.Chmod(0o644); err != nil {
		return "", err
	}
	if err = tmp.Sync(); err != nil {
		return "", err
	}
	if err = tmp.Close(); err != nil {
		return "", err
	}
	if err = os.Rename(tmp.Name(), path); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func positive(v float64) bool  { return v > 0 && !math.IsInf(v, 0) && !math.IsNaN(v) }
func simpleName(s string) bool { return s != "" && s != "." && s != ".." && filepath.Base(s) == s }

// Validate rejects grids whose size or spacing is inconsistent.
func (g Grid) Validate() error {
	if g.Columns <= 0 || g.Rows <= 0 || g.Columns > math.MaxInt/4/g.Rows ||
		!positive(g.WidthKM) || !positive(g.HeightKM) ||
		!positive(g.CellWidthKM) || !positive(g.CellHeightKM) ||
		g.CellWidthKM != g.WidthKM/float64(g.Columns) || g.CellHeightKM != g.HeightKM/float64(g.Rows) {
		return fmt.Errorf("invalid grid")
	}
	return nil
}

// ReadFields loads and verifies every field listed in m, requiring unique
// simple names, the manifest's grid, finite values, and the three
// continental fields later stages depend on.
func ReadFields(ctx context.Context, dir string, m *Manifest) (map[string]*fracture.FractureField, error) {
	g := m.Grid
	fields := make(map[string]*fracture.FractureField, len(m.Fields))
	for _, fd := range m.Fields {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !simpleName(fd.Name) || fields[fd.Name] != nil || !simpleName(fd.File) ||
			fd.Columns != g.Columns || fd.Rows != g.Rows {
			return nil, fmt.Errorf("invalid field %q", fd.Name)
		}
		f, err := ReadField(dir, m, fd.Name)
		if err != nil {
			return nil, err
		}
		for _, v := range f.Values {
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
				return nil, fmt.Errorf("nonfinite value in %s", fd.Name)
			}
		}
		fields[fd.Name] = f
	}
	for _, name := range []string{FieldAffinity, FieldContinental, FieldCoast} {
		if fields[name] == nil {
			return nil, fmt.Errorf("missing field %s", name)
		}
	}
	return fields, nil
}

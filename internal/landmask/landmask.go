// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package landmask creates and stores authoritative land/ocean checkpoints.
package landmask

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"math"
	"os"
	"path/filepath"

	"github.com/maloquacious/mapgen/internal/continent"
	"github.com/maloquacious/mapgen/internal/fracture"
)

const (
	Format       = "mapgen-landmask-v1"
	Selection    = "highest-affinity; ties by input row-major index; round(fraction*cells)"
	ManifestFile = "manifest.json"
	MaskFile     = "landmask.u8"
	PreviewFile  = "preview.png"
)

// Input identifies the continental checkpoint by content, not location, so
// the same input produces the same landmask wherever it is stored.
type Input struct {
	ManifestSHA256 string             `json:"manifest_sha256"`
	Manifest       continent.Manifest `json:"manifest"`
}

type MaskField struct {
	File        string `json:"file"`
	DType       string `json:"dtype"`
	SHA256      string `json:"sha256"`
	Description string `json:"description"`
}

type Manifest struct {
	Format             string             `json:"format"`
	GeneratorVersion   string             `json:"generator_version"`
	Input              Input              `json:"input"`
	Grid               continent.Grid     `json:"grid"`
	LandFraction       float64            `json:"land_fraction"`
	ActualLandFraction float64            `json:"actual_land_fraction"`
	LandCells          int                `json:"land_cells"`
	Selection          string             `json:"selection"`
	Recentering        fracture.Centering `json:"recentering"`
	Mask               MaskField          `json:"mask"`
	Fields             []continent.Field  `json:"fields"`
}

// Result keeps all fields in the same coordinates. Land is 1; ocean is 0.
type Result struct {
	Manifest Manifest
	Land     []byte
	Fields   map[string]*fracture.FractureField
}

// Generate reads and verifies the continental input before selecting land.
// Input files are never changed. Selection precedes any optional translation.
func Generate(ctx context.Context, dir string, fraction float64, recenter bool, version string) (*Result, error) {
	if !(fraction >= 0 && fraction <= 1) {
		return nil, fmt.Errorf("land-fraction must be in [0,1]")
	}
	// Parse and checksum the same bytes so the recorded checksum always
	// describes the manifest that was actually used.
	data, err := os.ReadFile(filepath.Join(dir, continent.ManifestFile))
	if err != nil {
		return nil, err
	}
	m, err := continent.ParseManifest(data)
	if err != nil {
		return nil, err
	}
	g := m.Grid
	if err := validateGrid(g); err != nil {
		return nil, fmt.Errorf("input: %w", err)
	}
	fields, err := readFields(ctx, dir, m)
	if err != nil {
		return nil, fmt.Errorf("input: %w", err)
	}
	r := &Result{
		Manifest: Manifest{Format: Format, GeneratorVersion: version,
			Input: Input{ManifestSHA256: checksum(data), Manifest: *m},
			Grid:  g, LandFraction: fraction, Selection: Selection},
		Fields: fields,
	}
	f := r.Fields[continent.FieldAffinity]
	n := len(f.Values)
	k := int(math.Round(fraction * float64(n)))
	mask := fracture.TopMask(f.Values, k)
	r.Land = make([]byte, n)
	for i, land := range mask {
		if land {
			r.Land[i] = 1
		}
	}
	r.Manifest.LandCells = k
	r.Manifest.ActualLandFraction = float64(k) / float64(n)
	if recenter {
		c, err := fracture.FindCentering(ctx, f, fraction)
		if err != nil {
			return nil, err
		}
		c.Applied = true
		r.Manifest.Recentering = c
		shifted := make([]byte, n)
		for i, land := range r.Land {
			x, y := i%g.Columns, i/g.Columns
			shifted[((y+c.Y)%g.Rows)*g.Columns+(x+c.X)%g.Columns] = land
		}
		r.Land = shifted
		for name, field := range r.Fields {
			r.Fields[name] = field.Slide(c.X, c.Y)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r, nil
}

func positive(v float64) bool  { return v > 0 && !math.IsInf(v, 0) && !math.IsNaN(v) }
func simpleName(s string) bool { return s != "" && s != "." && s != ".." && filepath.Base(s) == s }
func checksum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// validateGrid rejects grids whose size or spacing is inconsistent.
func validateGrid(g continent.Grid) error {
	if g.Columns <= 0 || g.Rows <= 0 || g.Columns > math.MaxInt/4/g.Rows ||
		!positive(g.WidthKM) || !positive(g.HeightKM) ||
		!positive(g.CellWidthKM) || !positive(g.CellHeightKM) ||
		g.CellWidthKM != g.WidthKM/float64(g.Columns) || g.CellHeightKM != g.HeightKM/float64(g.Rows) {
		return fmt.Errorf("invalid grid")
	}
	return nil
}

// readFields loads and verifies every field listed in m, requiring unique
// simple names, the manifest's grid, finite values, and the three
// continental fields later stages depend on.
func readFields(ctx context.Context, dir string, m *continent.Manifest) (map[string]*fracture.FractureField, error) {
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
		f, err := continent.ReadField(dir, m, fd.Name)
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
	for _, name := range []string{continent.FieldAffinity, continent.FieldContinental, continent.FieldCoast} {
		if fields[name] == nil {
			return nil, fmt.Errorf("missing field %s", name)
		}
	}
	return fields, nil
}

// Write publishes a complete artifact using a sibling temporary directory.
// An existing output is rejected so accepted checkpoints are not overwritten.
// previewScale is 0 to omit the preview, or >=1 for nearest-cell sampling.
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
	tmp, err := os.MkdirTemp(filepath.Dir(dir), ".landmask-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	m := r.Manifest
	m.Fields = nil
	for _, fd := range m.Input.Manifest.Fields {
		if err := ctx.Err(); err != nil {
			return err
		}
		f := r.Fields[fd.Name]
		if f == nil {
			return fmt.Errorf("result is missing field %q", fd.Name)
		}
		if f.Columns != m.Grid.Columns || f.Rows != m.Grid.Rows {
			return fmt.Errorf("field %q does not match the grid", fd.Name)
		}
		field, err := continent.WriteField(tmp, fd.Name, fd.Description, f)
		if err != nil {
			return err
		}
		m.Fields = append(m.Fields, field)
	}
	if len(r.Land) != m.Grid.Columns*m.Grid.Rows {
		return fmt.Errorf("mask does not match the grid")
	}
	m.Mask = MaskField{File: MaskFile, DType: "uint8", SHA256: checksum(r.Land), Description: "row-major; 0=ocean, 1=land; same grid as continental fields"}
	if err := os.WriteFile(filepath.Join(tmp, m.Mask.File), r.Land, 0o644); err != nil {
		return err
	}
	if previewScale > 0 {
		if err := writePreview(filepath.Join(tmp, PreviewFile), r, previewScale); err != nil {
			return err
		}
	}
	data, err := json.Marshal(m, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(tmp, ManifestFile), append(data, '\n'), 0o644); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		return err
	}
	// Never replace a directory that appeared after the check above.
	if err := renameNoReplace(tmp, dir); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("output already exists: %s", dir)
		}
		return err
	}
	return nil
}

// Load verifies the mask, its counts, and all associated continental rasters.
func Load(dir string) (*Result, error) {
	data, err := os.ReadFile(filepath.Join(dir, ManifestFile))
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if m.Format != Format {
		return nil, fmt.Errorf("unsupported landmask format %q", m.Format)
	}
	g := m.Grid
	if err := validateGrid(g); err != nil {
		return nil, err
	}
	if !(m.LandFraction >= 0 && m.LandFraction <= 1) || m.Mask.File != MaskFile || m.Mask.DType != "uint8" {
		return nil, fmt.Errorf("invalid landmask metadata")
	}
	land, err := os.ReadFile(filepath.Join(dir, m.Mask.File))
	if err != nil {
		return nil, err
	}
	if len(land) != g.Columns*g.Rows || checksum(land) != m.Mask.SHA256 {
		return nil, fmt.Errorf("landmask size or checksum mismatch")
	}
	count := 0
	for _, v := range land {
		if v > 1 {
			return nil, fmt.Errorf("invalid landmask value %d", v)
		}
		count += int(v)
	}
	if count != m.LandCells || count != int(math.Round(m.LandFraction*float64(len(land)))) || m.ActualLandFraction != float64(count)/float64(len(land)) {
		return nil, fmt.Errorf("landmask count mismatch")
	}
	// Write saves exactly the input's fields, in order, so a loaded result
	// can be written again.
	in := m.Input.Manifest.Fields
	if len(in) != len(m.Fields) {
		return nil, fmt.Errorf("landmask fields do not match input fields")
	}
	for i := range in {
		if in[i].Name != m.Fields[i].Name {
			return nil, fmt.Errorf("landmask fields do not match input fields")
		}
	}
	fields, err := readFields(context.Background(), dir, &continent.Manifest{Grid: g, Fields: m.Fields})
	if err != nil {
		return nil, err
	}
	return &Result{Manifest: m, Land: land, Fields: fields}, nil
}

func writePreview(path string, r *Result, scale int) error {
	g := r.Manifest.Grid
	w, h := (g.Columns-1)/scale+1, (g.Rows-1)/scale+1
	img := image.NewPaletted(image.Rect(0, 0, w, h), color.Palette{
		color.RGBA{25, 70, 135, 255}, color.RGBA{90, 155, 75, 255},
	})
	for y := range h {
		for x := range w {
			img.SetColorIndex(x, y, r.Land[y*scale*g.Columns+x*scale])
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

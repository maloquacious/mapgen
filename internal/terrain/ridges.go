// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package terrain

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"math"

	"github.com/maloquacious/mapgen/internal/structure"
)

// RidgesFormat is the format of ridges.json.
const RidgesFormat = "mapgen-ridges-v1"

// Ridge kinds.
const (
	RidgeChain  = "chain"
	RidgeBranch = "branch"
)

// Ridge is a ridge chain along a mountain belt, or a branch leaving one.
// Its backbone follows structure's rules for backbones; a sample's width is
// the full width of the ridge's kernel there.
type Ridge struct {
	ID      uint32 `json:"id"`
	Feature uint32 `json:"feature"`
	Kind    string `json:"kind"`
	// Parent is the chain a branch leaves.
	Parent   uint32             `json:"parent,omitzero"`
	Backbone []structure.Sample `json:"backbone"`
	// GapsKM are the stretches, in km along the backbone, where the chain
	// fades out.
	GapsKM [][2]float64 `json:"gaps_km,omitempty"`
}

// Trough is a longitudinal basin between two adjacent chains of a belt.
type Trough struct {
	ID       uint32             `json:"id"`
	Feature  uint32             `json:"feature"`
	Backbone []structure.Sample `json:"backbone"`
	DepthM   float64            `json:"depth_m"`
}

// Saddle is where a corridor crosses a ridge.
type Saddle struct {
	Ridge      uint32  `json:"ridge"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	ElevationM float64 `json:"elevation_m"`
}

// Corridor is a low route across a mountain belt. Along it every ridge of
// the belt is lowered by up to SaddleShare of its height, so the saddles
// stay above the regional surface.
type Corridor struct {
	ID          uint32             `json:"id"`
	Feature     uint32             `json:"feature"`
	Backbone    []structure.Sample `json:"backbone"`
	SaddleShare float64            `json:"saddle_share"`
	Saddles     []Saddle           `json:"saddles"`
}

// Peak is a volcanic peak of an island arc or an active margin: a cone
// rising HeightM above the regional surface at its center.
type Peak struct {
	ID       uint32  `json:"id"`
	Feature  uint32  `json:"feature"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	RadiusKM float64 `json:"radius_km"`
	HeightM  float64 `json:"height_m"`
}

// RidgeFile is the content of ridges.json. Geometry is in km, in the
// structure coordinate convention: each backbone unwrapped with its first
// vertex in the base domain, and points in the base domain.
type RidgeFile struct {
	Format    string           `json:"format"`
	Domain    structure.Domain `json:"domain"`
	Ridges    []Ridge          `json:"ridges"`
	Troughs   []Trough         `json:"troughs"`
	Corridors []Corridor       `json:"corridors"`
	Peaks     []Peak           `json:"peaks"`
}

// Marshal returns the canonical bytes of the file.
func (rf *RidgeFile) Marshal() ([]byte, error) {
	b, err := json.Marshal(rf, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// ParseRidges decodes ridges.json.
func ParseRidges(data []byte) (*RidgeFile, error) {
	var rf RidgeFile
	if err := json.Unmarshal(data, &rf, json.RejectUnknownMembers(true)); err != nil {
		return nil, fmt.Errorf("ridges: %w", err)
	}
	if rf.Format != RidgesFormat {
		return nil, fmt.Errorf("ridges: format %q is not %q", rf.Format, RidgesFormat)
	}
	return &rf, nil
}

// Validate checks the records against the features they belong to.
func (rf *RidgeFile) Validate(ff *structure.FeatureFile) error {
	if rf.Domain != ff.Domain {
		return fmt.Errorf("ridges: domain does not match the features")
	}
	dom := rf.Domain
	byID := map[uint32]*structure.Feature{}
	for i := range ff.Features {
		byID[ff.Features[i].ID] = &ff.Features[i]
	}
	isBelt := func(id uint32) bool { f := byID[id]; return f != nil && f.Type == structure.TypeMountainBelt }
	inBase := func(x, y float64) bool { return x >= 0 && x < dom.WidthKM && y >= 0 && y < dom.HeightKM }
	ridges := map[uint32]*Ridge{}
	for i := range rf.Ridges {
		r := &rf.Ridges[i]
		switch {
		case r.ID != uint32(i+1):
			return fmt.Errorf("ridge %d: ids must run from 1 in order", r.ID)
		case !isBelt(r.Feature):
			return fmt.Errorf("ridge %d: feature %d is not a mountain belt", r.ID, r.Feature)
		case r.Kind == RidgeChain && r.Parent != 0,
			r.Kind == RidgeBranch && (ridges[r.Parent] == nil || ridges[r.Parent].Kind != RidgeChain || ridges[r.Parent].Feature != r.Feature),
			r.Kind != RidgeChain && r.Kind != RidgeBranch:
			return fmt.Errorf("ridge %d: invalid kind %q or parent %d", r.ID, r.Kind, r.Parent)
		}
		if err := structure.ValidateBackbone(dom, r.Backbone); err != nil {
			return fmt.Errorf("ridge %d: %w", r.ID, err)
		}
		for _, g := range r.GapsKM {
			if !(g[0] < g[1]) || math.IsNaN(g[0]) || math.IsInf(g[1], 0) {
				return fmt.Errorf("ridge %d: invalid gap %v", r.ID, g)
			}
		}
		ridges[r.ID] = r
	}
	for i, t := range rf.Troughs {
		if t.ID != uint32(i+1) || !isBelt(t.Feature) || !(t.DepthM > 0) {
			return fmt.Errorf("trough %d: invalid id, feature, or depth", t.ID)
		}
		if err := structure.ValidateBackbone(dom, t.Backbone); err != nil {
			return fmt.Errorf("trough %d: %w", t.ID, err)
		}
	}
	for i, c := range rf.Corridors {
		if c.ID != uint32(i+1) || !isBelt(c.Feature) || !(c.SaddleShare > 0 && c.SaddleShare < 1) {
			return fmt.Errorf("corridor %d: invalid id, feature, or saddle share", c.ID)
		}
		if err := structure.ValidateBackbone(dom, c.Backbone); err != nil {
			return fmt.Errorf("corridor %d: %w", c.ID, err)
		}
		for _, s := range c.Saddles {
			if r := ridges[s.Ridge]; r == nil || r.Feature != c.Feature || !inBase(s.X, s.Y) {
				return fmt.Errorf("corridor %d: invalid saddle on ridge %d", c.ID, s.Ridge)
			}
		}
	}
	for i, p := range rf.Peaks {
		f := byID[p.Feature]
		if p.ID != uint32(i+1) || f == nil || !(f.Type == structure.TypeIslandArc || f.Type == structure.TypeMountainBelt && f.Kind == "active_margin") {
			return fmt.Errorf("peak %d: invalid id or feature", p.ID)
		}
		if !inBase(p.X, p.Y) || !(p.RadiusKM > 0) || !(p.HeightM > 0) {
			return fmt.Errorf("peak %d: invalid position, radius, or height", p.ID)
		}
	}
	return nil
}

// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package trench

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"math"

	"github.com/maloquacious/mapgen/internal/structure"
)

// RecordsFormat names the schema of trenches.json.
const RecordsFormat = "mapgen-trenches-v1"

// Polarity bases: why a subduction zone subducts on its side.
const (
	// BasisFlank is the side opposite an active margin's elevated flank.
	BasisFlank = "elevated_flank"
	// BasisOcean is the side with more open water.
	BasisOcean = "open_ocean"
)

// Subduction records the polarity of one active margin or island arc.
type Subduction struct {
	Feature uint32 `json:"feature"`
	// Side is the side of the feature's backbone the sinking plate, and so
	// the trench, lies on: "left" or "right".
	Side  string `json:"side"`
	Basis string `json:"basis"`
}

// Trench is one linear trench record. Its geometry follows the rules of a
// structure backbone.
type Trench struct {
	ID       uint32             `json:"id"`
	Parent   uint32             `json:"parent"`
	Key      *uint64            `json:"key,string"`
	Side     string             `json:"side"`
	Backbone []structure.Sample `json:"backbone"`
	// Targets holds only the floor range, in m relative to the provisional
	// datum.
	Targets structure.Targets `json:"targets"`
}

// TrenchFile is the content of trenches.json.
type TrenchFile struct {
	Format     string           `json:"format"`
	Domain     structure.Domain `json:"domain"`
	Subduction []Subduction     `json:"subduction"`
	Trenches   []Trench         `json:"trenches"`
}

// Subducts reports whether f is a feature with a subduction record: an
// active margin or an island arc.
func Subducts(f *structure.Feature) bool {
	return f.Type == structure.TypeIslandArc || f.Type == structure.TypeMountainBelt && f.Kind == "active_margin"
}

// ParseTrenches decodes trenches.json, rejecting unknown fields. Call
// Validate with the features it belongs to.
func ParseTrenches(data []byte) (*TrenchFile, error) {
	var tf TrenchFile
	if err := json.Unmarshal(data, &tf, json.RejectUnknownMembers(true)); err != nil {
		return nil, fmt.Errorf("trenches: %w", err)
	}
	return &tf, nil
}

// Marshal encodes tf in its canonical form.
func (tf *TrenchFile) Marshal() ([]byte, error) {
	data, err := json.Marshal(tf, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// Validate checks tf against the structure features it was built from:
// exactly one subduction record per active margin and island arc, in
// feature order, and trenches numbered from 1 whose parent and side match a
// subduction record, with valid backbones and floor ranges below the datum.
func (tf *TrenchFile) Validate(ff *structure.FeatureFile) error {
	if tf.Format != RecordsFormat {
		return fmt.Errorf("trenches: unsupported format %q", tf.Format)
	}
	if tf.Domain != ff.Domain {
		return fmt.Errorf("trenches: domain does not match the features")
	}
	side := map[uint32]string{}
	k := 0
	for i := range ff.Features {
		f := &ff.Features[i]
		if !Subducts(f) {
			continue
		}
		if k >= len(tf.Subduction) || tf.Subduction[k].Feature != f.ID {
			return fmt.Errorf("trenches: subduction record %d does not name feature %d", k, f.ID)
		}
		s := tf.Subduction[k]
		if s.Side != structure.Left.String() && s.Side != structure.Right.String() {
			return fmt.Errorf("trenches: feature %d: side %q is not left or right", f.ID, s.Side)
		}
		switch s.Basis {
		case BasisFlank:
			if f.ElevatedFlank != structure.Left.String() && f.ElevatedFlank != structure.Right.String() || f.ElevatedFlank == s.Side {
				return fmt.Errorf("trenches: feature %d: side %q is not opposite elevated_flank %q", f.ID, s.Side, f.ElevatedFlank)
			}
		case BasisOcean:
		default:
			return fmt.Errorf("trenches: feature %d: unknown basis %q", f.ID, s.Basis)
		}
		side[f.ID] = s.Side
		k++
	}
	if k != len(tf.Subduction) {
		return fmt.Errorf("trenches: %d subduction records for %d features", len(tf.Subduction), k)
	}
	for i := range tf.Trenches {
		t := &tf.Trenches[i]
		if t.ID != uint32(i+1) {
			return fmt.Errorf("trench entry %d has id %d", i, t.ID)
		}
		if t.Key == nil {
			return fmt.Errorf("trench %d: key is required", t.ID)
		}
		if s, ok := side[t.Parent]; !ok || s != t.Side {
			return fmt.Errorf("trench %d: parent %d and side %q do not match a subduction record", t.ID, t.Parent, t.Side)
		}
		if i > 0 && t.Parent < tf.Trenches[i-1].Parent {
			return fmt.Errorf("trench %d: trenches are not in parent order", t.ID)
		}
		if err := structure.ValidateBackbone(tf.Domain, t.Backbone); err != nil {
			return fmt.Errorf("trench %d: %w", t.ID, err)
		}
		r := t.Targets.RegionalElevationM
		if r == nil || t.Targets.ReliefM != nil || !(finite(r[0]) && finite(r[1]) && r[0] <= r[1] && r[1] < 0) {
			return fmt.Errorf("trench %d: targets must be a regional_elevation_m [min, max] below 0 and nothing else", t.ID)
		}
	}
	return nil
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

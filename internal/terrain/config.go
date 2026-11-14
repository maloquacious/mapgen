// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package terrain

import (
	"encoding/json/v2"
	"fmt"
	"math"
	"reflect"
	"slices"
)

// ConfigFormat is the format of a terrain configuration file.
const ConfigFormat = "mapgen-terrain-config-v1"

// Config holds the calibration values of one run. A configuration file
// names its format and any of the sections, and within a section only the
// values it changes; everything else keeps its default. Map entries merge
// by key. The resolved values are recorded in the manifest, so a run is
// repeatable from its artifact alone.
type Config struct {
	Format     string           `json:"format"`
	Foundation FoundationParams `json:"foundation"`
	Regional   RegionalParams   `json:"regional"`
	Belts      BeltParams       `json:"belts"`
	Subcell    SubcellParams    `json:"subcell"`
	Residual   ResidualParams   `json:"residual"`
}

// DefaultConfig returns the default calibration.
func DefaultConfig() Config {
	return Config{
		Format:     ConfigFormat,
		Foundation: DefaultFoundationParams(), Regional: DefaultRegionalParams(), Belts: DefaultBeltParams(),
		Subcell: DefaultSubcellParams(), Residual: DefaultResidualParams(),
	}
}

// ParseConfig decodes a configuration file over the defaults and validates
// the result.
func ParseConfig(data []byte) (Config, error) {
	c := DefaultConfig()
	c.Format = ""
	if err := json.Unmarshal(data, &c, json.RejectUnknownMembers(true)); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	if c.Format != ConfigFormat {
		return Config{}, fmt.Errorf("config: format %q is not %q", c.Format, ConfigFormat)
	}
	if err := c.Validate(); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	return c, nil
}

// Validate checks values against the defaults' shape and the bounds the
// algorithms rely on:
//
//   - Algorithm names and descriptions are those of this version; a
//     configuration changes values, not algorithms.
//   - Maps have exactly the default keys.
//   - Every number is finite and keeps its default's sign, zero allowed: a
//     depth stays a depth and a width a width.
//   - Every pair keeps its default's order, so a range stays a range.
//   - Values the algorithms divide by or count with are positive, and
//     shares are at most 1.
func (c Config) Validate() error {
	d := DefaultConfig()
	if err := like(reflect.ValueOf(c), reflect.ValueOf(d), "config"); err != nil {
		return err
	}
	f, rg, b, s, r := c.Foundation, c.Regional, c.Belts, c.Subcell, c.Residual
	positive := map[string]float64{
		"foundation.rise_km": f.RiseKM, "foundation.working_spacing_km": f.WorkingSpacingKM, "foundation.smooth_km": f.SmoothKM,
		"foundation.min_wavelength_km": f.MinWavelengthKM, "foundation.slope_km": f.SlopeKM, "foundation.narrow_shelf_km": f.NarrowShelfKM,
		"belts.station_km": b.StationKM, "belts.min_chain_half_km": b.MinChainHalfKM, "belts.chain_reach": b.ChainReach,
		"belts.trench_fade_km":   b.TrenchFadeKM,
		"residual.wavelength_km": r.WavelengthKM, "residual.min_wavelength_cells": r.MinWavelengthCells,
		"regional.core_weight": rg.CoreWeight,
	}
	for _, pair := range []struct {
		name string
		v    [2]float64
	}{
		{"foundation.shelf_width_km", f.ShelfWidthKM}, {"regional.control_spacing_km", rg.ControlSpacingKM},
		{"regional.plateau_wavelength_km", rg.PlateauWavelengthKM}, {"belts.wander_wavelength_km", b.WanderWavelengthKM},
		{"belts.gap_km", b.GapKM}, {"belts.peak_spacing_km", b.PeakSpacingKM}, {"belts.peak_diameter_km", b.PeakDiameterKM},
		{"belts.corridor_spacing_km", b.CorridorSpacingKM}, {"belts.corridor_half_km", b.CorridorHalfKM},
	} {
		positive[pair.name+"[0]"], positive[pair.name+"[1]"] = pair.v[0], pair.v[1]
	}
	names := make([]string, 0, len(positive))
	for k := range positive {
		names = append(names, k)
	}
	slices.Sort(names)
	for _, k := range names {
		if !(positive[k] > 0) {
			return fmt.Errorf("%s must be positive", k)
		}
	}
	shares := map[string]float64{
		"regional.second_uplift_share": rg.SecondUpliftShare, "regional.core_weight": rg.CoreWeight,
		"belts.volcanic_share": b.VolcanicShare, "belts.maturity_amplitude": b.MaturityAmplitude,
		"residual.persistence": r.Persistence, "residual.maturity_damping": r.MaturityDamping,
		"foundation.variation_share": f.VariationShare,
	}
	names = names[:0]
	for k := range shares {
		names = append(names, k)
	}
	slices.Sort(names)
	for _, k := range names {
		if shares[k] > 1 {
			return fmt.Errorf("%s must be at most 1", k)
		}
	}
	switch {
	case !(b.SaddleShare[0] > 0 && b.SaddleShare[1] < 1):
		return fmt.Errorf("belts.saddle_share must lie strictly between 0 and 1")
	case s.Samples < 1 || s.Samples > 16:
		return fmt.Errorf("subcell.samples must be from 1 to 16")
	case r.Octaves < 1:
		return fmt.Errorf("residual.octaves must be at least 1")
	case r.GrainSamples < 2:
		return fmt.Errorf("residual.grain_samples must be at least 2")
	}
	for k, v := range b.Chains {
		if v[0] < 1 {
			return fmt.Errorf("belts.chains[%s] must be at least 1", k)
		}
	}
	return nil
}

// like checks that v has d's shape, strings, signs, and pair orders.
func like(v, d reflect.Value, path string) error {
	switch d.Kind() {
	case reflect.Struct:
		t := d.Type()
		for i := range t.NumField() {
			name := t.Field(i).Name
			if tag := t.Field(i).Tag.Get("json"); tag != "" {
				name = tag
			}
			if err := like(v.Field(i), d.Field(i), path+"."+name); err != nil {
				return err
			}
		}
	case reflect.Map:
		if v.Len() != d.Len() {
			return fmt.Errorf("%s: keys must be those of the defaults", path)
		}
		for _, k := range d.MapKeys() {
			e := v.MapIndex(k)
			if !e.IsValid() {
				return fmt.Errorf("%s: missing key %v", path, k)
			}
			if err := like(e, d.MapIndex(k), fmt.Sprintf("%s[%v]", path, k)); err != nil {
				return err
			}
		}
	case reflect.Array:
		for i := range d.Len() {
			if err := like(v.Index(i), d.Index(i), fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
		if d.Len() == 2 {
			a, b, da, db := num(v.Index(0)), num(v.Index(1)), num(d.Index(0)), num(d.Index(1))
			if da <= db && a > b || da > db && a < b {
				return fmt.Errorf("%s: the range must keep its order", path)
			}
		}
	case reflect.String:
		if v.String() != d.String() {
			return fmt.Errorf("%s: %q cannot be changed", path, d.String())
		}
	case reflect.Float64, reflect.Int:
		x, dx := num(v), num(d)
		if math.IsNaN(x) || math.IsInf(x, 0) || dx > 0 && x < 0 || dx < 0 && x > 0 {
			return fmt.Errorf("%s: %g must be finite with the sign of its default %g", path, x, dx)
		}
	}
	return nil
}

// num returns a float or int value as float64.
func num(v reflect.Value) float64 {
	if v.Kind() == reflect.Int {
		return float64(v.Int())
	}
	return v.Float()
}

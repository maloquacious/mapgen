// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package structure

import (
	"encoding/json/v2"
	"fmt"

	"github.com/maloquacious/mapgen/internal/calib"
)

// ConfigFormat is the format of a structure configuration file.
const ConfigFormat = "mapgen-structure-config-v1"

// Config holds the calibration values of one run. A configuration file
// names its format and any of the sections, and within a section only the
// values it changes; everything else keeps its default. Map entries merge
// by key. The resolved values are recorded in the manifest (placement only
// when features are placed automatically), so a run is repeatable from its
// artifact alone.
type Config struct {
	Format    string          `json:"format"`
	Provinces ProvinceParams  `json:"provinces"`
	Coast     CoastParams     `json:"coast"`
	Placement PlacementParams `json:"placement"`
	Rocks     RockParams      `json:"rocks"`
}

// DefaultConfig returns the default calibration.
func DefaultConfig() Config {
	return Config{
		Format:    ConfigFormat,
		Provinces: DefaultProvinceParams(), Coast: DefaultCoastParams(), Placement: DefaultPlacementParams(), Rocks: DefaultRockParams(),
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

// Validate checks the values against the defaults' shape (see calib.Like)
// and the bounds the algorithms rely on: values they divide or count by
// are positive, shares and fractions are at most 1, a rift's axis fraction
// lies strictly between 0 and 1, and the fault sense shares sum to at most
// 1.
func (c Config) Validate() error {
	if err := calib.Like(c, DefaultConfig(), "config", "config.placement.fault_sense"); err != nil {
		return err
	}
	pv, co, pl, rk := c.Provinces, c.Coast, c.Placement, c.Rocks
	positive := map[string]float64{
		"provinces.seed_area_km2": pv.SeedAreaKM2, "provinces.working_spacing_km": pv.WorkingSpacingKM,
		"coast.working_spacing_km": co.WorkingSpacingKM, "coast.smooth_km": co.SmoothKM,
		"placement.attempts": float64(pl.Attempts), "placement.step_km": pl.StepKM, "placement.sample_km": pl.SampleKM,
		"placement.bend_radius": pl.BendRadius, "placement.min_bend_km": pl.MinBendKM, "placement.max_swing_deg": pl.MaxSwingDeg,
		"placement.mainland_km2": pl.MainlandKM2, "placement.intermontane_width_km": pl.IntermontaneWidthKM,
		"rocks.noise_wavelength_km": rk.NoiseWavelengthKM, "rocks.noise_octaves": float64(rk.NoiseOctaves),
	}
	families := map[string]FamilyParams{
		"collision": pl.Collision, "active_margin": pl.ActiveMargin, "old_orogen": pl.OldOrogen, "rift": pl.Rift,
		"island_arc": pl.IslandArc, "fault": pl.Fault, "interior_basin": pl.InteriorBasin,
	}
	shares := map[string]float64{
		"provinces.shield_share": pv.ShieldShare, "provinces.volcanic_chance": pv.VolcanicChance,
		"coast.land_coverage": co.LandCoverage,
		"placement.end_width": pl.EndWidth, "placement.margin_basin": pl.MarginBasin,
		"placement.interrupted_share": pl.InterruptedShare, "placement.intermontane_share": pl.IntermontaneShare,
		"placement.plateau_depth[1]": pl.PlateauDepth[1], "placement.foreland_depth[1]": pl.ForelandDepth[1],
		"rocks.volcanic_igneous": rk.VolcanicIgneous, "rocks.oceanic_igneous": rk.OceanicIgneous,
		"rocks.igneous_noise": rk.IgneousNoise, "rocks.igneous_influence": rk.IgneousInfluence,
		"placement.fault_sense[0]+[1]": pl.FaultSense[0] + pl.FaultSense[1],
	}
	for name, f := range families {
		positive["placement."+name+".area_per_event_million_km2"] = f.AreaPerEvent
		positive["placement."+name+".length_km[0]"] = f.LengthKM[0]
		positive["placement."+name+".width_km[0]"] = f.WidthKM[0]
		if f.Maturity != nil {
			shares["placement."+name+".maturity[1]"] = f.Maturity[1]
		}
	}
	if err := calib.Positive(positive); err != nil {
		return err
	}
	if err := calib.Shares(shares); err != nil {
		return err
	}
	switch {
	case !(pl.AxisFraction[0] > 0 && pl.AxisFraction[1] < 1):
		return fmt.Errorf("placement.axis_fraction must lie strictly between 0 and 1")
	case !(rk.ShelfKM[0] < rk.ShelfKM[1]):
		return fmt.Errorf("rocks.shelf_km must be increasing")
	}
	return nil
}

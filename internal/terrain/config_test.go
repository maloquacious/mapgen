// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package terrain_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/maloquacious/mapgen/internal/terrain"
)

func TestConfig(t *testing.T) {
	if err := terrain.DefaultConfig().Validate(); err != nil {
		t.Fatalf("defaults: %v", err)
	}
	c, err := terrain.ParseConfig([]byte(`{"format":"mapgen-terrain-config-v1",
		"regional":{"rift_floor_m":200,"default_targets":{"rift":{"regional_elevation_m":[400,1200]}}},
		"subcell":{"samples":2}}`))
	if err != nil {
		t.Fatal(err)
	}
	d := terrain.DefaultConfig()
	if c.Regional.RiftFloorM != 200 || c.Subcell.Samples != 2 ||
		c.Regional.DefaultTargets["rift"].RegionalElevationM != [2]float64{400, 1200} ||
		c.Regional.DefaultTargets["rift"].ReliefM != d.Regional.DefaultTargets["rift"].ReliefM ||
		c.Regional.SecondUpliftShare != d.Regional.SecondUpliftShare || c.Belts.StationKM != d.Belts.StationKM {
		t.Fatalf("merged config %+v", c.Regional)
	}
	for _, bad := range []string{
		`{}`,
		`{"format":"mapgen-terrain-config-v1","regional":{"algorithm":"regional-v9"}}`,
		`{"format":"mapgen-terrain-config-v1","regional":{"rift_floor_m":-5}}`,
		`{"format":"mapgen-terrain-config-v1","regional":{"default_targets":{"volcano":{}}}}`,
		`{"format":"mapgen-terrain-config-v1","foundation":{"shelf_width_km":[200,50]}}`,
		`{"format":"mapgen-terrain-config-v1","foundation":{"rise_km":0}}`,
		`{"format":"mapgen-terrain-config-v1","belts":{"saddle_share":[0.2,1]}}`,
		`{"format":"mapgen-terrain-config-v1","subcell":{"samples":0}}`,
		`{"format":"mapgen-terrain-config-v1","residual":{"persistence":1.5}}`,
		`{"format":"mapgen-terrain-config-v1","unknown":{}}`,
	} {
		if _, err := terrain.ParseConfig([]byte(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		} else if !strings.HasPrefix(err.Error(), "config: ") {
			t.Errorf("error %v", err)
		}
	}
}

// The default configuration changes nothing; another changes the output
// and is recorded in the manifest; an invalid one is refused.
func TestConfigRun(t *testing.T) {
	in := inputDir(t)
	base := generate(t, in, nil)
	d := terrain.DefaultConfig()
	same, err := terrain.Generate(t.Context(), terrain.Options{In: in, Config: &d, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(same.Elevation, base.Elevation) {
		t.Fatal("the default configuration changed the elevation")
	}
	c := terrain.DefaultConfig()
	c.Residual.Share = 0.2
	r, err := terrain.Generate(t.Context(), terrain.Options{In: in, Config: &c, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Manifest.ResidualParameters.Share != 0.2 || slices.Equal(r.Elevation, base.Elevation) {
		t.Fatal("the configuration was not used or not recorded")
	}
	c.Subcell.Samples = 0
	if _, err := terrain.Generate(t.Context(), terrain.Options{In: in, Config: &c, Version: "test"}); err == nil {
		t.Fatal("accepted an invalid configuration")
	}
}

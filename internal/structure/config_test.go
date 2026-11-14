// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package structure_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/maloquacious/mapgen/internal/structure"
)

func TestConfig(t *testing.T) {
	if err := structure.DefaultConfig().Validate(); err != nil {
		t.Fatalf("defaults: %v", err)
	}
	c, err := structure.ParseConfig([]byte(`{"format":"mapgen-structure-config-v1",
		"provinces":{"min_area_km2":40000},
		"placement":{"fault_sense":[0.2,0.6],"rift":{"minimum":2}},
		"rocks":{"influences":{"rift":{"pressure":-2}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	d := structure.DefaultConfig()
	if c.Provinces.MinAreaKM2 != 40000 || c.Provinces.SeedAreaKM2 != d.Provinces.SeedAreaKM2 ||
		c.Placement.FaultSense != [2]float64{0.2, 0.6} || c.Placement.Rift.Minimum != 2 ||
		c.Placement.Rift.LengthKM != d.Placement.Rift.LengthKM ||
		c.Rocks.Influences["rift"].Pressure != -2 || c.Rocks.Influences["rift"].Shear != d.Rocks.Influences["rift"].Shear ||
		*c.Rocks.Influences["rift"].StressProfile != *d.Rocks.Influences["rift"].StressProfile {
		t.Fatalf("merged config %+v", c)
	}
	for _, bad := range []string{
		`{}`,
		`{"format":"mapgen-structure-config-v1","coast":{"algorithm":"coast-v9"}}`,
		`{"format":"mapgen-structure-config-v1","provinces":{"min_area_km2":-1}}`,
		`{"format":"mapgen-structure-config-v1","provinces":{"shield_share":1.5}}`,
		`{"format":"mapgen-structure-config-v1","placement":{"attempts":0}}`,
		`{"format":"mapgen-structure-config-v1","placement":{"fault_sense":[0.7,0.6]}}`,
		`{"format":"mapgen-structure-config-v1","placement":{"axis_fraction":[0,0.4]}}`,
		`{"format":"mapgen-structure-config-v1","placement":{"collision":{"length_km":[5000,1500]}}}`,
		`{"format":"mapgen-structure-config-v1","placement":{"rift":{"maturity":[0,1]}}}`,
		`{"format":"mapgen-structure-config-v1","rocks":{"influences":{"plateau":{"stress_profile":[0.5,1]}}}}`,
		`{"format":"mapgen-structure-config-v1","rocks":{"influences":{"volcano":{}}}}`,
		`{"format":"mapgen-structure-config-v1","rocks":{"shelf_km":[300,300]}}`,
		`{"format":"mapgen-structure-config-v1","unknown":{}}`,
	} {
		if _, err := structure.ParseConfig([]byte(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		} else if !strings.HasPrefix(err.Error(), "config: ") {
			t.Errorf("error %v", err)
		}
	}
}

// The default configuration changes nothing; another changes the output
// and is recorded in the manifest; an invalid one is refused.
func TestConfigRun(t *testing.T) {
	in := continents(t)
	base := generate(t, structure.Options{In: in})
	d := structure.DefaultConfig()
	if same := generate(t, structure.Options{In: in, Config: &d}); !slices.Equal(same.Rock, base.Rock) ||
		len(same.Features.Features) != len(base.Features.Features) {
		t.Fatal("the default configuration changed the result")
	}
	c := structure.DefaultConfig()
	c.Rocks.MetamorphicLimit = 0.05
	c.Placement.Fault.Minimum = 4
	r := generate(t, structure.Options{In: in, Config: &c})
	if r.Manifest.RockParameters.MetamorphicLimit != 0.05 || r.Manifest.PlacementParameters.Fault.Minimum != 4 ||
		slices.Equal(r.Rock, base.Rock) {
		t.Fatal("the configuration was not used or not recorded")
	}
	c.Placement.Attempts = 0
	if _, err := structure.Generate(t.Context(), structure.Options{In: in, Fraction: 0.325, Config: &c, Version: "test"}); err == nil {
		t.Fatal("accepted an invalid configuration")
	}
}

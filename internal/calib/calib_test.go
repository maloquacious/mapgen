// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package calib_test

import (
	"strings"
	"testing"

	"github.com/maloquacious/mapgen/internal/calib"
)

type inner struct {
	Core float64 `json:"core"`
}

type params struct {
	Algorithm string            `json:"algorithm"`
	Width     float64           `json:"width_km"`
	Depth     float64           `json:"depth_m"`
	Range     [2]float64        `json:"range"`
	Shares    [2]float64        `json:"shares"`
	Count     int               `json:"count"`
	Opt       *[2]float64       `json:"opt,omitzero"`
	None      *[2]float64       `json:"none,omitzero"`
	By        map[string]inner  `json:"by"`
	Pairs     map[string][2]int `json:"pairs"`
}

func defaults() params {
	return params{Algorithm: "a-v1", Width: 10, Depth: -100, Range: [2]float64{1, 2}, Shares: [2]float64{0.5, 0.25}, Count: 3,
		Opt: &[2]float64{1, 3}, By: map[string]inner{"x": {1}}, Pairs: map[string][2]int{"p": {1, 4}}}
}

func TestLike(t *testing.T) {
	ok := []func(*params){
		func(*params) {},
		func(p *params) { p.Width, p.Depth, p.Count = 0, 0, 0 },
		func(p *params) { p.Shares = [2]float64{0.1, 0.8} },
		func(p *params) { p.Opt = &[2]float64{2, 2} },
	}
	for i, f := range ok {
		p := defaults()
		f(&p)
		if err := calib.Like(p, defaults(), "c", "c.shares"); err != nil {
			t.Errorf("case %d: %v", i, err)
		}
	}
	bad := map[string]func(*params){
		"c.algorithm":  func(p *params) { p.Algorithm = "a-v2" },
		"c.width_km":   func(p *params) { p.Width = -1 },
		"c.depth_m":    func(p *params) { p.Depth = 5 },
		"c.range":      func(p *params) { p.Range = [2]float64{3, 2} },
		"c.count":      func(p *params) { p.Count = -1 },
		"c.opt":        func(p *params) { p.Opt = nil },
		"c.none":       func(p *params) { p.None = &[2]float64{1, 2} },
		"c.by":         func(p *params) { p.By = map[string]inner{"y": {1}} },
		"c.by[x].core": func(p *params) { p.By = map[string]inner{"x": {-1}} },
		"c.pairs[p]":   func(p *params) { p.Pairs = map[string][2]int{"p": {5, 4}} },
		"c.opt[0]":     func(p *params) { p.Opt = &[2]float64{-1, 3} },
	}
	for path, f := range bad {
		p := defaults()
		f(&p)
		err := calib.Like(p, defaults(), "c", "c.shares")
		if err == nil || !strings.HasPrefix(err.Error(), path) {
			t.Errorf("%s: got %v", path, err)
		}
	}
	if err := calib.Positive(map[string]float64{"b": 1, "a": 0}); err == nil || !strings.HasPrefix(err.Error(), "a ") {
		t.Errorf("positive: %v", err)
	}
	if err := calib.Shares(map[string]float64{"s": 1.01}); err == nil {
		t.Error("accepted a share above 1")
	}
}

// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package calib validates calibration files: a stage's parameters decoded
// over its defaults, so a file names only the values it changes.
package calib

import (
	"fmt"
	"math"
	"reflect"
	"slices"
)

// Like checks that v, a parameter struct decoded over the defaults d, keeps
// d's shape:
//
//   - strings (algorithm names and descriptions) are unchanged; a
//     configuration changes values, not algorithms;
//   - maps have exactly d's keys, and optional values (pointers) are
//     present exactly where d has them;
//   - every number is finite and keeps its default's sign, zero allowed;
//   - every two-element array keeps its default's order, so a range stays a
//     range, except the arrays at the paths in pairs, which are not ranges.
//
// Paths are root, then JSON names joined by dots, map keys and array
// indices in brackets, as errors report them.
func Like(v, d any, root string, pairs ...string) error {
	return like(reflect.ValueOf(v), reflect.ValueOf(d), root, pairs)
}

func like(v, d reflect.Value, path string, pairs []string) error {
	switch d.Kind() {
	case reflect.Struct:
		t := d.Type()
		for i := range t.NumField() {
			name := t.Field(i).Name
			if tag := t.Field(i).Tag.Get("json"); tag != "" {
				name = tag
				for j := range len(tag) {
					if tag[j] == ',' {
						name = tag[:j]
						break
					}
				}
			}
			if err := like(v.Field(i), d.Field(i), path+"."+name, pairs); err != nil {
				return err
			}
		}
	case reflect.Pointer:
		if v.IsNil() != d.IsNil() {
			return fmt.Errorf("%s cannot be added or removed", path)
		}
		if !d.IsNil() {
			return like(v.Elem(), d.Elem(), path, pairs)
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
			if err := like(e, d.MapIndex(k), fmt.Sprintf("%s[%v]", path, k), pairs); err != nil {
				return err
			}
		}
	case reflect.Array:
		for i := range d.Len() {
			if err := like(v.Index(i), d.Index(i), fmt.Sprintf("%s[%d]", path, i), pairs); err != nil {
				return err
			}
		}
		if d.Len() == 2 && !slices.Contains(pairs, path) {
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

// Positive requires every named value to be greater than 0, reporting the
// first failure in name order.
func Positive(values map[string]float64) error {
	for _, k := range sorted(values) {
		if !(values[k] > 0) {
			return fmt.Errorf("%s must be positive", k)
		}
	}
	return nil
}

// Shares requires every named value to be at most 1, reporting the first
// failure in name order. Like already keeps them from going negative.
func Shares(values map[string]float64) error {
	for _, k := range sorted(values) {
		if values[k] > 1 {
			return fmt.Errorf("%s must be at most 1", k)
		}
	}
	return nil
}

func sorted(m map[string]float64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

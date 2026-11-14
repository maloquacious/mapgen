// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Command terrain writes a terrain artifact from a trench artifact: ground
// and seabed elevation relative to the provisional coast.
//
//	terrain --in tmp/trench --out tmp/terrain
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"

	"github.com/maloquacious/mapgen"
	"github.com/maloquacious/mapgen/internal/terrain"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("terrain: ")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		log.Fatal(err)
	}
}

func run(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("terrain", flag.ContinueOnError)
	in := fs.String("in", "tmp/trench", "trench input artifact directory")
	out := fs.String("out", "tmp/terrain", "new output artifact directory (must not exist)")
	seedFlag := fs.String("seed", "", "terrain seed, unsigned 64-bit (default: the trench input's seed)")
	png := fs.Bool("png", true, "write the previews")
	scale := fs.Int("png-scale", 1, "preview sampling interval in cells; 1 shows every cell")
	recenterFlag := fs.Bool("recenter", false, "translate the complete result to center the largest landmass terrain implies")
	residual := fs.Bool("residual", true, "add residual detail; false publishes the structured surface alone")
	workers := fs.Int("workers", 0, "worker goroutines (default: one per CPU); never affects the output")
	version := fs.Bool("version", false, "print version and exit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if *version {
		fmt.Println(mapgen.Version().String())
		return nil
	}
	if *workers < 0 {
		return fmt.Errorf("workers must not be negative")
	}
	if *png && *scale < 1 {
		return fmt.Errorf("png-scale must be at least 1")
	}
	var seed *uint64
	if *seedFlag != "" {
		v, err := strconv.ParseUint(*seedFlag, 10, 64)
		if err != nil {
			return fmt.Errorf("seed: %w", err)
		}
		seed = &v
	}
	r, err := terrain.Generate(ctx, terrain.Options{In: *in, Seed: seed, Workers: *workers, NoResidual: !*residual, Recenter: *recenterFlag, Version: mapgen.Version().String()})
	if err != nil {
		return err
	}
	previewScale := 0
	if *png {
		previewScale = *scale
	}
	if err := terrain.Write(ctx, *out, r, previewScale); err != nil {
		return err
	}
	m, d := r.Manifest, r.Diagnostics
	log.Printf("seed %d (%s)", m.Seed, m.SeedSource)
	log.Printf("wrote %s: %d x %d, land %.0f to %.0f m, water %.0f to %.0f m", *out, m.Grid.Columns, m.Grid.Rows,
		d.ElevationLand.MinM, d.ElevationLand.MaxM, d.ElevationWater.MinM, d.ElevationWater.MaxM)
	log.Printf("at the provisional fraction: level %.2f m, %d cells water to land, %d land to water",
		d.Shift.LevelM, d.Shift.WaterToLand, d.Shift.LandToWater)
	return nil
}

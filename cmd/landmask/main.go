// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Command landmask selects an authoritative land/ocean mask from a continents
// artifact, by affinity, or from a terrain artifact, by elevation, freezing
// the ocean and clamping elevations to it.
//
//	landmask --in tmp/terrain --out tmp/landmask
//	landmask --in tmp/continents --land-fraction 0.325 --out tmp/landmask
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/maloquacious/mapgen"
	"github.com/maloquacious/mapgen/internal/landmask"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("landmask: ")
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
	fs := flag.NewFlagSet("landmask", flag.ContinueOnError)
	in := fs.String("in", "tmp/terrain", "input continents or terrain artifact directory")
	out := fs.String("out", "tmp/landmask", "new output artifact directory (must not exist)")
	fraction := fs.Float64("land-fraction", 0.325, "fraction of all cells selected as land (non-ocean), in [0,1]; for terrain input, default the terrain's provisional land fraction")
	recenter := fs.Bool("recenter", false, "center the largest landmass, translating mask and all continental fields together (continents input only)")
	png := fs.Bool("png", true, "write preview.png from the saved mask")
	scale := fs.Int("png-scale", 1, "preview sampling interval in cells; 1 shows every cell")
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
	if *png && *scale < 1 {
		return fmt.Errorf("png-scale must be at least 1")
	}
	kind, err := landmask.InputKind(*in)
	if err != nil {
		return err
	}
	previewScale := 0
	if *png {
		previewScale = *scale
	}
	explicit := false
	fs.Visit(func(f *flag.Flag) { explicit = explicit || f.Name == "land-fraction" })
	if kind == landmask.InputTerrain {
		if *recenter {
			return fmt.Errorf("--recenter is not available for terrain input; recenter with terrain --recenter")
		}
		var fr *float64
		if explicit {
			fr = fraction
		}
		r, err := landmask.GenerateFromTerrain(ctx, *in, fr, mapgen.Version().String())
		if err != nil {
			return err
		}
		if err := landmask.WriteTerrain(ctx, *out, r, previewScale); err != nil {
			return err
		}
		m, d := r.Manifest, r.Diagnostics
		log.Printf("wrote %s: %d x %d, %d non-ocean cells (%.4f%%), sea level %.1f m above the provisional datum",
			*out, m.Grid.Columns, m.Grid.Rows, m.LandCells, 100*m.ActualLandFraction, m.SeaLevelM)
		log.Printf("repair fraction %.5f; %d depression cells; raised %d cells (max %.1f m), lowered %d (max %.1f m)",
			m.RepairFraction, d.DepressionCells, d.Clamp.RaisedCells, d.Clamp.MaxRaiseM, d.Clamp.LoweredCells, d.Clamp.MaxLowerM)
		return nil
	}
	r, err := landmask.Generate(ctx, *in, *fraction, *recenter, mapgen.Version().String())
	if err != nil {
		return err
	}
	if err := landmask.Write(ctx, *out, r, previewScale); err != nil {
		return err
	}
	m := r.Manifest
	log.Printf("wrote %s: %d x %d, %d land cells (%.4f%%)", *out, m.Grid.Columns, m.Grid.Rows, m.LandCells, 100*m.ActualLandFraction)
	if c := m.Recentering; c.Applied {
		log.Printf("recentered by (%d, %d)", c.X, c.Y)
	}
	return nil
}

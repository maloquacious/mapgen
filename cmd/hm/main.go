// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Command hm generates the continental affinity layer of a world map using
// the rectangular fracture generator (fracture-rect-v1).
//
// The default raster is 6666 x 3333 square cells of 6 km, roughly Earth's
// circumference by its pole-to-pole extent. Output is a directory holding
// manifest.json, raw float32 rasters for later stages, and an optional PNG
// preview for inspection.
//
//	hm -seed 42 -out tmp/hm
package main

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"time"

	"github.com/maloquacious/mapgen"
	"github.com/maloquacious/mapgen/internal/continent"
	"github.com/maloquacious/mapgen/internal/fracture"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("hm: ")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(2)
		}
		log.Fatal(err)
	}
}

type layerFlags struct {
	bumps                  int
	minCells, maxCells, pf float64
}

func (l *layerFlags) register(fs *flag.FlagSet, name string) {
	fs.IntVar(&l.bumps, name+"-bumps", 0, name+" disc count (0 = default from cell count)")
	fs.Float64Var(&l.minCells, name+"-min-cells", 0, name+" minimum disc radius in cells (0 = default)")
	fs.Float64Var(&l.maxCells, name+"-max-cells", 0, name+" maximum disc radius in cells (0 = default)")
	fs.Float64Var(&l.pf, name+"-falloff", 0, name+" radius power-law falloff (0 = default)")
}

func (l *layerFlags) apply(dst *fracture.Layer) {
	if l.bumps != 0 {
		dst.Bumps = l.bumps
	}
	if l.minCells != 0 {
		dst.MinRadiusCells = l.minCells
	}
	if l.maxCells != 0 {
		dst.MaxRadiusCells = l.maxCells
	}
	if l.pf != 0 {
		dst.Falloff = l.pf
	}
}

func run(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("hm", flag.ContinueOnError)
	var (
		seedFlag     = fs.String("seed", "", "world seed, unsigned 64-bit (default: random, printed and recorded)")
		out          = fs.String("out", "tmp/hm", "output directory")
		cols         = fs.Int("cols", 6666, "raster columns (east-west, wraps)")
		rows         = fs.Int("rows", 3333, "raster rows (north-south)")
		cellKM       = fs.Float64("cell-km", 6, "cell size in km (cells are square)")
		coastWeight  = fs.Float64("coast-weight", float64(fracture.DefaultCoastWeight), "weight of coast detail in the combined affinity")
		recenter     = fs.Bool("recenter", true, "move the largest provisional landmass to the map center")
		landFraction = fs.Float64("land-fraction", fracture.DefaultProvisionalLandFraction, "provisional land fraction for recentering and the preview")
		png          = fs.Bool("png", true, "write preview.png")
		pngScale     = fs.Int("png-scale", 1, "preview downsampling factor")
		showVersion  = fs.Bool("version", false, "print version and exit")
		cont, coast  layerFlags
	)
	cont.register(fs, "continental")
	coast.register(fs, "coast")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if *showVersion {
		fmt.Println(mapgen.Version().String())
		return nil
	}
	if *pngScale < 1 {
		return fmt.Errorf("png-scale must be at least 1")
	}

	seed, err := parseSeed(*seedFlag)
	if err != nil {
		return err
	}
	p, err := fracture.DefaultParams(seed, *cols, *rows, *cellKM)
	if err != nil {
		return err
	}
	cont.apply(&p.Continental)
	coast.apply(&p.Coast)
	p.CoastWeight = float32(*coastWeight)
	p.Recenter = *recenter
	p.ProvisionalLandFraction = *landFraction

	log.Printf("seed %d, %d x %d cells of %g km (%.0f x %.0f km)", seed, p.Columns, p.Rows, p.CellKM, p.WidthKM(), p.HeightKM())
	for _, l := range []struct {
		name string
		fracture.Layer
	}{{"continental", p.Continental}, {"coast", p.Coast}} {
		log.Printf("%-11s: %d discs, r %.1f..%.1f cells (%.1f..%.1f km), falloff %g", l.name, l.Bumps,
			l.MinRadiusCells, l.MaxRadiusCells, l.MinRadiusCells*p.CellKM, l.MaxRadiusCells*p.CellKM, l.Falloff)
	}

	var timings []continent.TimingMS
	lap := func(stage string, start time.Time) {
		d := time.Since(start)
		timings = append(timings, continent.TimingMS{Stage: stage, MS: d.Milliseconds()})
		log.Printf("%s: %v", stage, d.Round(time.Millisecond))
	}

	start := time.Now()
	res, err := fracture.Generate(ctx, p)
	if err != nil {
		return err
	}
	lap("generate", start)
	c := res.Centering
	if c.Applied {
		log.Printf("recentered by (%d, %d); largest provisional landmass %d of %d mask cells in %d components",
			c.X, c.Y, c.ComponentCells, c.MaskCells, c.Components)
	}

	m := continent.NewManifest(mapgen.Version().String(), res)
	start = time.Now()
	m.TimingsMS = timings
	if err := continent.Write(*out, m, res); err != nil {
		return err
	}
	lap("write", start)

	if *png {
		start = time.Now()
		path := filepath.Join(*out, "preview.png")
		if err := writePreview(path, res.Affinity, *landFraction, *pngScale); err != nil {
			return err
		}
		lap("preview", start)
	}
	log.Printf("wrote %s", *out)
	return nil
}

func parseSeed(s string) (uint64, error) {
	if s == "" {
		var b [8]byte
		if _, err := rand.Read(b[:]); err != nil {
			return 0, err
		}
		return binary.LittleEndian.Uint64(b[:]), nil
	}
	seed, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("seed: %w", err)
	}
	return seed, nil
}

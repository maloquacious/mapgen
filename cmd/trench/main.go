// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Command trench writes a trench artifact from a structure artifact: the
// polarity of every active margin and island arc, and the ocean trenches
// beside them.
//
//	trench --in tmp/structure --out tmp/trench
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
	"github.com/maloquacious/mapgen/internal/trench"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("trench: ")
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
	fs := flag.NewFlagSet("trench", flag.ContinueOnError)
	in := fs.String("in", "tmp/structure", "structure input artifact directory")
	out := fs.String("out", "tmp/trench", "new output artifact directory (must not exist)")
	seedFlag := fs.String("seed", "", "trench seed, unsigned 64-bit (default: the structure input's seed)")
	recenter := fs.Bool("recenter", false, "translate the complete result, structure records included, to center the largest provisional landmass")
	png := fs.Bool("png", true, "write the preview")
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
	var seed *uint64
	if *seedFlag != "" {
		v, err := strconv.ParseUint(*seedFlag, 10, 64)
		if err != nil {
			return fmt.Errorf("seed: %w", err)
		}
		seed = &v
	}
	r, err := trench.Generate(ctx, trench.Options{In: *in, Seed: seed, Version: mapgen.Version().String(), Recenter: *recenter})
	if err != nil {
		return err
	}
	previewScale := 0
	if *png {
		previewScale = *scale
	}
	if err := trench.Write(ctx, *out, r, previewScale); err != nil {
		return err
	}
	m, d := r.Manifest, r.Diagnostics
	log.Printf("seed %d (%s)", m.Seed, m.SeedSource)
	if c := m.Recentering; c.Applied {
		log.Printf("recentered by (%d, %d) cells, (%g, %g) km", c.ShiftX, c.ShiftY, c.ShiftXKM, c.ShiftYKM)
	}
	log.Printf("wrote %s: %d subduction zones (%d by elevated flank, %d by open ocean), %d trenches, %.0f km",
		*out, m.SubductionCount, d.Subduction[trench.BasisFlank], d.Subduction[trench.BasisOcean], m.TrenchCount, d.LengthKM)
	for _, o := range d.Omitted {
		log.Printf("omitted: %s", o)
	}
	return nil
}

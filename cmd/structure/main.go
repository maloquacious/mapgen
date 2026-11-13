// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Command structure writes a geological structure artifact from a
// continental artifact: features from a fixture or placed automatically,
// provinces, and rock classification.
//
//	structure --in tmp/continents --features fixture.json --out tmp/structure
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
	"github.com/maloquacious/mapgen/internal/structure"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("structure: ")
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
	fs := flag.NewFlagSet("structure", flag.ContinueOnError)
	in := fs.String("in", "tmp/continents", "continental input artifact directory")
	out := fs.String("out", "tmp/structure", "new output artifact directory (must not exist)")
	seedFlag := fs.String("seed", "", "structure seed, unsigned 64-bit (default: the continental input's seed)")
	fraction := fs.Float64("provisional-land-fraction", 0.325, "fraction of cells treated as land while placing features, in [0,1]")
	features := fs.String("features", "", "feature fixture file; if omitted, features are placed automatically")
	png := fs.Bool("png", true, "write preview.png")
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
	var fixture []byte
	if *features != "" {
		var err error
		if fixture, err = os.ReadFile(*features); err != nil {
			return err
		}
		if fixture == nil {
			fixture = []byte{}
		}
	}
	var seed *uint64
	if *seedFlag != "" {
		v, err := strconv.ParseUint(*seedFlag, 10, 64)
		if err != nil {
			return fmt.Errorf("seed: %w", err)
		}
		seed = &v
	}
	r, err := structure.Generate(ctx, structure.Options{
		In: *in, Seed: seed, Fraction: *fraction, Features: fixture, Version: mapgen.Version().String(),
	})
	if err != nil {
		return err
	}
	previewScale := 0
	if *png {
		previewScale = *scale
	}
	if err := structure.Write(ctx, *out, r, previewScale); err != nil {
		return err
	}
	m := r.Manifest
	log.Printf("seed %d (%s)", m.Seed, m.SeedSource)
	log.Printf("wrote %s: %d x %d, %s mode, %d features, %d provisional land cells", *out, m.Grid.Columns, m.Grid.Rows, m.Mode, m.FeatureCount, m.ProvisionalLandCells)
	for _, o := range r.Diagnostics.Omitted {
		log.Printf("omitted: %s", o)
	}
	for _, w := range r.Diagnostics.Warnings {
		log.Printf("warning: %s", w)
	}
	return nil
}

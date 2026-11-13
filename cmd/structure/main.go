// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Command structure writes a geological structure artifact from a
// continental artifact and a feature fixture.
//
//	structure --in tmp/continents --features fixture.json --out tmp/structure
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
	seedFlag := fs.String("seed", "", "structure seed, unsigned 64-bit (default: random, printed and recorded)")
	fraction := fs.Float64("provisional-land-fraction", 0.325, "fraction of cells treated as land while placing features, in [0,1]")
	features := fs.String("features", "", "feature fixture file (required until automatic placement exists)")
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
	if *features == "" {
		return structure.ErrNoPlacement
	}
	fixture, err := os.ReadFile(*features)
	if err != nil {
		return err
	}
	seed, err := parseSeed(*seedFlag)
	if err != nil {
		return err
	}
	log.Printf("seed %d", seed)
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
	log.Printf("wrote %s: %d x %d, %d features, %d provisional land cells", *out, m.Grid.Columns, m.Grid.Rows, m.FeatureCount, m.ProvisionalLandCells)
	for _, w := range r.Diagnostics.Warnings {
		log.Printf("warning: %s", w)
	}
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

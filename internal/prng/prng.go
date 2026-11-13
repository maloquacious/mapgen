// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package prng derives independent, reproducible random streams from a world
// seed and a stable domain name.
//
// The derivation is: state = seed XOR FNV-1a-64(domain), and the stream is
// math/rand/v2's PCG seeded with (state, sequence). The derivation is part of
// the output contract; changing it changes every generated world, so bump
// DerivationVersion if it is ever altered.
package prng

import (
	"hash/fnv"
	"math/rand/v2"
)

const (
	// DerivationVersion names the seed derivation recorded in manifests.
	DerivationVersion = "fnv1a64-xor-pcg-v1"
	// Algorithm names the PRNG recorded in manifests.
	Algorithm = "math/rand/v2 PCG-DXSM"
)

// DomainHash returns the 64-bit FNV-1a hash of domain.
func DomainHash(domain string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(domain))
	return h.Sum64()
}

// New returns the random stream for seed, domain, and sequence.
func New(seed uint64, domain string, sequence uint64) *rand.Rand {
	return rand.New(rand.NewPCG(seed^DomainHash(domain), sequence))
}

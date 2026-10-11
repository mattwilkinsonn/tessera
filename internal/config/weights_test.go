package config

import (
	"math/rand/v2"
	"testing"
	"time"
)

// ratioSubsetsReference is the original exhaustive check over every subsequence, kept as the oracle.
func ratioSubsetsReference(weights []float64, index int, suffix float64, suffixLength int) bool {
	if index < 0 {
		return false
	}
	weight := weights[index]
	if suffixLength > 0 {
		ratio := weight / (weight + suffix)
		if ratio < minChainRatio || ratio > maxChainRatio {
			return true
		}
	}
	if ratioSubsetsReference(weights, index-1, suffix, suffixLength) {
		return true
	}
	return ratioSubsetsReference(weights, index-1, suffix+weight, suffixLength+1)
}

func TestChainRatiosMatchExhaustiveReference(t *testing.T) {
	rng := rand.New(rand.NewPCG(4487, 32))
	// Boundary values put ratios exactly on 0.1 and 0.9; the rest spread around them.
	pool := []float64{1, 9, 1.0 / 9, 0.5, 4.5, 3, 7, 1e-3, 1e3}
	agreeViolating, agreeValid := 0, 0
	for trial := range 20000 {
		length := 2 + rng.IntN(9)
		weights := make([]float64, length)
		for index := range weights {
			if rng.IntN(2) == 0 {
				weights[index] = pool[rng.IntN(len(pool))]
			} else {
				weights[index] = 0.05 + rng.Float64()*10
			}
		}
		got := chainRatiosOutOfRange(weights)
		want := ratioSubsetsReference(weights, len(weights)-1, 0, 0)
		if got != want {
			t.Fatalf("trial %d: chainRatiosOutOfRange(%v) = %t; reference = %t", trial, weights, got, want)
		}
		if got {
			agreeViolating++
		} else {
			agreeValid++
		}
	}
	// The comparison only means something if both verdicts occurred.
	if agreeViolating == 0 || agreeValid == 0 {
		t.Fatalf("degenerate sample: %d violating, %d valid", agreeViolating, agreeValid)
	}
}

// A valid chain is at most ~21 long, so this 64-weight vector must be rejected. A legal 21-weight geometric
// prefix padded with ones keeps short chains legal, so an exhaustive search walks many subsets before the violation.
func TestSixtyFourWeightsValidateQuickly(t *testing.T) {
	weights := make([]float64, 64)
	weight := 1.0
	for index := 20; index >= 0; index-- {
		weights[index] = weight
		weight *= 10.0 / 9
	}
	for index := 21; index < len(weights); index++ {
		weights[index] = 1
	}
	start := time.Now()
	violates := violatesChainRatios(weights)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("violatesChainRatios(64 weights) took %v", elapsed)
	}
	if !violates {
		t.Fatal("violatesChainRatios(64 weights) = false; want a chain ratio violation")
	}
}

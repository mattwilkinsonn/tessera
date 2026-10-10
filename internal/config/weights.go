package config

import (
	"fmt"
	"math"
	"strconv"
)

const (
	minChainRatio = 0.1
	maxChainRatio = 0.9
)

func weightDefaultViolations(where string, defaults WeightDefaults) []error {
	var violations []error
	for _, axis := range []struct {
		name   string
		values map[int][]float64
	}{
		{name: "columns", values: defaults.Columns},
		{name: "rows", values: defaults.Rows},
	} {
		for count, weights := range axis.values {
			at := fmt.Sprintf("%s weights.%s.%s", where, axis.name, strconv.Itoa(count))
			if count < 2 || count != len(weights) {
				violations = append(violations, fmt.Errorf("%s: key must equal vector length and be at least 2", at))
			}
			violations = append(violations, weightViolations(at, weights)...)
		}
	}
	return violations
}

func weightViolations(where string, weights []float64) []error {
	var violations []error
	for _, weight := range weights {
		if math.IsNaN(weight) || math.IsInf(weight, 0) || weight <= 0 {
			violations = append(violations, fmt.Errorf("%s: weights must be finite and greater than 0", where))
			break
		}
	}
	if violatesChainRatios(weights) {
		violations = append(violations, fmt.Errorf("%s: weight chain ratios must be between 0.1 and 0.9", where))
	}
	return violations
}

func violatesChainRatios(weights []float64) bool {
	if len(weights) < 2 {
		return false
	}
	maximum := 0.0
	for _, weight := range weights {
		if weight > maximum {
			maximum = weight
		}
	}
	if maximum <= 0 || math.IsInf(maximum, 0) || math.IsNaN(maximum) {
		return false
	}
	_, exponent := math.Frexp(maximum)
	scale := math.Ldexp(1, -exponent)
	normalized := make([]float64, len(weights))
	for index, weight := range weights {
		normalized[index] = weight * scale
	}
	return ratioSubsets(normalized, len(normalized)-1, 0, 0)
}

func ratioSubsets(weights []float64, index int, suffix float64, suffixLength int) bool {
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
	if ratioSubsets(weights, index-1, suffix, suffixLength) {
		return true
	}
	return ratioSubsets(weights, index-1, suffix+weight, suffixLength+1)
}

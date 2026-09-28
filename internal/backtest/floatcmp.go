package backtest

import (
	"math"

	"quantitative-trading/internal/config"
)

// NearlyEqual compares floats with relative/absolute tolerance from config.
func NearlyEqual(a, b float64, tol config.FloatTolConfig) bool {
	rel, abs := tol.Rel, tol.Abs
	if rel == 0 && abs == 0 {
		rel, abs = 1e-9, 1e-12
	}
	diff := math.Abs(a - b)
	if diff <= abs {
		return true
	}
	den := math.Max(math.Abs(a), math.Abs(b))
	if den == 0 {
		return diff <= abs
	}
	return diff/den <= rel
}

// NearlyEqualDefault uses Default config float_tol.
func NearlyEqualDefault(a, b float64) bool {
	return NearlyEqual(a, b, config.Default().Backtest.FloatTol)
}

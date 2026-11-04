package backtest

import "math"

// SampleMoneyUnit is the frozen cash rounding increment (CNY fen).
const SampleMoneyUnit = 0.01

// SampleAbsTol is the frozen absolute tolerance for small-sample ledger checks.
const SampleAbsTol = 0.01

// RoundMoney rounds to SampleMoneyUnit (0.01 CNY).
func RoundMoney(x float64) float64 {
	if SampleMoneyUnit <= 0 {
		return x
	}
	inv := 1.0 / SampleMoneyUnit
	return math.Round(x*inv) / inv
}

// NearlyEqualAbs reports |a-b| <= abs.
func NearlyEqualAbs(a, b, abs float64) bool {
	return math.Abs(a-b) <= abs
}

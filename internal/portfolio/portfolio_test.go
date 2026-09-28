package portfolio_test

import (
	"testing"

	"quantitative-trading/internal/config"
	"quantitative-trading/internal/portfolio"
	"quantitative-trading/internal/types"
)

func TestSelectHoldingsIndustryCap(t *testing.T) {
	cfg := config.Default()
	cfg.Portfolio.N = 5
	cfg.Portfolio.IndustryCapRatio = 0.15 // max(3, ceil(0.75))=3
	results := []types.CompositeResult{
		{TSCode: "A", Composite: 5, Industry: "X"},
		{TSCode: "B", Composite: 4, Industry: "X"},
		{TSCode: "C", Composite: 3, Industry: "X"},
		{TSCode: "D", Composite: 2, Industry: "X"}, // would be 4th in X — capped
		{TSCode: "E", Composite: 1.5, Industry: "Y"},
		{TSCode: "F", Composite: 1.0, Industry: "Y"},
	}
	h := portfolio.SelectHoldings(results, cfg.Portfolio.N, cfg.Portfolio.IndustryCapRatio)
	if len(h) != 5 {
		t.Fatalf("len=%d", len(h))
	}
	indX := 0
	for _, x := range h {
		for _, r := range results {
			if r.TSCode == x.TSCode && r.Industry == "X" {
				indX++
			}
		}
	}
	if indX > 3 {
		t.Fatalf("industry X count %d exceeds cap 3", indX)
	}
	// D should be skipped due to cap
	for _, x := range h {
		if x.TSCode == "D" {
			t.Fatal("D should be capped out")
		}
	}
}

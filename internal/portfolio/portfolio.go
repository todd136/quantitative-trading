// Package portfolio implements Composite synthesis, SelectHoldings, and rebalance schedule (§4).
package portfolio

import (
	"math"
	"sort"
	"time"

	"quantitative-trading/internal/config"
	"quantitative-trading/internal/types"
)

// CompositeEqualWeight computes equal-weight (or configured) composite from three-factor Z scores.
// When llmEnabled and llmWeight>0 and fs.LLM present, includes LLM in normalized weights (§9.5).
// When llm disabled / weight 0 / LLM nil: pure three-factor path (parity with v1.1).
func CompositeEqualWeight(cfg config.Config, scores map[types.SecurityID]types.FactorScores, industry map[types.SecurityID]string) []types.CompositeResult {
	wq := cfg.Factors.Quality.Weight
	wv := cfg.Factors.Value.Weight
	wm := cfg.Factors.Momentum.Weight
	useLLM := cfg.LLM.Enabled && cfg.LLM.Weight > 0
	wllm := 0.0
	if useLLM {
		wllm = cfg.LLM.Weight
	}

	out := make([]types.CompositeResult, 0, len(scores))
	for code, fs := range scores {
		var parts []float64
		var weights []float64
		if cfg.Factors.Quality.Enabled && fs.Quality != nil {
			parts = append(parts, *fs.Quality)
			weights = append(weights, wq)
		}
		if cfg.Factors.Value.Enabled && fs.Value != nil {
			parts = append(parts, *fs.Value)
			weights = append(weights, wv)
		}
		if cfg.Factors.Momentum.Enabled && fs.Momentum != nil {
			parts = append(parts, *fs.Momentum)
			weights = append(weights, wm)
		}
		if useLLM && fs.LLM != nil {
			parts = append(parts, *fs.LLM)
			weights = append(weights, wllm)
		}
		if len(parts) == 0 {
			continue
		}
		if cfg.Factors.RequireAll && (!cfg.Factors.Quality.Enabled || fs.Quality == nil ||
			!cfg.Factors.Value.Enabled || fs.Value == nil ||
			!cfg.Factors.Momentum.Enabled || fs.Momentum == nil) {
			continue
		}
		sumW := 0.0
		for _, w := range weights {
			sumW += w
		}
		if sumW == 0 {
			continue
		}
		comp := 0.0
		for i := range parts {
			comp += (weights[i] / sumW) * parts[i]
		}
		if math.IsNaN(comp) || math.IsInf(comp, 0) {
			continue
		}
		out = append(out, types.CompositeResult{
			TSCode:    code,
			Composite: comp,
			Industry:  industry[code],
		})
	}
	return out
}

// SelectHoldings picks Top-N by Composite with industry cap max(3, ceil(N*ratio)) (§4.2).
func SelectHoldings(results []types.CompositeResult, n int, indCapRatio float64) []types.TargetHolding {
	if n <= 0 {
		return nil
	}
	cp := append([]types.CompositeResult(nil), results...)
	sort.SliceStable(cp, func(i, j int) bool {
		if cp[i].Composite == cp[j].Composite {
			return string(cp[i].TSCode) < string(cp[j].TSCode)
		}
		return cp[i].Composite > cp[j].Composite
	})
	maxPer := maxPerIndustry(n, indCapRatio)
	indCount := map[string]int{}
	holdings := make([]types.TargetHolding, 0, n)
	for _, r := range cp {
		if indCount[r.Industry] >= maxPer {
			continue
		}
		holdings = append(holdings, types.TargetHolding{TSCode: r.TSCode})
		indCount[r.Industry]++
		if len(holdings) == n {
			break
		}
	}
	if len(holdings) == 0 {
		return holdings
	}
	w := 1.0 / float64(len(holdings))
	for i := range holdings {
		holdings[i].Weight = w
	}
	return holdings
}

func maxPerIndustry(n int, ratio float64) int {
	v := float64(n) * ratio
	ceil := int(math.Ceil(v))
	if ceil < 3 {
		return 3
	}
	return ceil
}

// IsRebalanceDay returns whether T is a rebalance signal day given freq (§4.3).
func IsRebalanceDay(t types.TradeDate, freq string, weekday int, cal []types.TradeDate, calIndex map[string]int) bool {
	switch freq {
	case "", "daily":
		return true
	case "weekly":
		// Go Weekday: Sunday=0 ...; config uses 0=Mon style from YAML comment.
		// Map config weekday 0=Mon .. 6=Sun to time.Weekday.
		want := time.Weekday((weekday + 1) % 7)
		return t.Time().Weekday() == want
	case "monthly":
		idx, ok := calIndex[t.String()]
		if !ok {
			return false
		}
		// month-end trading day: next cal day is different month or none
		if idx+1 >= len(cal) {
			return true
		}
		return cal[idx+1].Time().Month() != t.Time().Month()
	default:
		return true
	}
}

package backtest_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"quantitative-trading/internal/backtest"
	"quantitative-trading/internal/config"
	"quantitative-trading/internal/types"
	"quantitative-trading/internal/universe"
)

func TestAcceptanceNegPackStrictQuotesRejects1029(t *testing.T) {
	cache := filepath.Join("..", "..", "testdata", "samples", "cache_missing_20261029")
	matches, err := filepath.Glob(filepath.Join(cache, "bars_000623.SZ_*_2026-10-29.json"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("setup: %v matches=%v", err, matches)
	}
	path := matches[0]
	for _, m := range matches {
		if strings.Contains(m, "2025-08-11") {
			path = m
			break
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var bf struct {
		Bars []struct {
			TradeDate              string `json:"trade_date"`
			TSCode                 string `json:"ts_code"`
			Open, High, Low, Close float64
			PreClose               float64 `json:"pre_close"`
			HighLimit              float64 `json:"high_limit"`
			LowLimit               float64 `json:"low_limit"`
			AdjFactor              float64 `json:"adj_factor"`
		} `json:"bars"`
	}
	if err := json.Unmarshal(raw, &bf); err != nil {
		t.Fatal(err)
	}
	code := types.SecurityID(bf.Bars[0].TSCode)
	bars := map[types.SecurityID]map[string]types.Bar{code: {}}
	var cal []types.TradeDate
	has1029 := false
	for _, r := range bf.Bars {
		d, _ := types.ParseTradeDate(r.TradeDate)
		cal = append(cal, d)
		bars[code][d.String()] = types.Bar{TradeDate: d, TSCode: code, Open: r.Open, High: r.High, Low: r.Low, Close: r.Close, PreClose: r.PreClose, HighLimit: r.HighLimit, LowLimit: r.LowLimit, AdjFactor: r.AdjFactor}
		if r.TradeDate == "2026-10-29" {
			has1029 = true
		}
	}
	d28, _ := types.ParseTradeDate("2026-10-28")
	d29, _ := types.ParseTradeDate("2026-10-29")
	if !has1029 {
		cal = append(cal, d29)
	}
	cfg := config.Default()
	cfg.Data.StrictQuotes = true
	eng := &backtest.Engine{Cfg: cfg, Calendar: cal, CalIndex: universe.BuildIndex(cal), Bars: bars}
	state := &backtest.PortfolioState{Cash: 1e6, Lots: []types.Lot{{TSCode: code, Shares: 100, AvailableFrom: d28, CostBasis: 10}}}
	_, err = eng.MarkNAV(state, d29)
	report := map[string]any{"file": filepath.Base(path), "code": string(code), "has_bar_20261029": has1029, "err": fmtErr(err)}
	b, _ := json.MarshalIndent(report, "", "  ")
	t.Log(string(b))
	if has1029 {
		t.Fatalf("negative pack unexpectedly has 10/29 bar in %s", path)
	}
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("formal strict_quotes must reject missing 10/29, got %v", err)
	}
}

func fmtErr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

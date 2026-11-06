package akshare_test

import (
	"fmt"
	"os"
	"path/filepath"
	"quantitative-trading/internal/backtest"
	"quantitative-trading/internal/config"
	"quantitative-trading/internal/data/akshare"
	"quantitative-trading/internal/types"
	"quantitative-trading/internal/universe"
	"strings"
	"testing"
)

func reviewProvider(t *testing.T, adj string, close float64) (*akshare.Provider, types.TradeDate, types.TradeDate) {
	t.Helper()
	dir := t.TempDir()
	from := types.NewTradeDate(2026, 10, 22)
	to := types.NewTradeDate(2026, 10, 26)
	rows := fmt.Sprintf(`{"bars":[{"trade_date":"2026-10-22","ts_code":"000623.SZ","open":10,"high":11,"low":9,"close":%v,"volume":100000,"amount":1000000,"pre_close":0,"high_limit":11,"low_limit":9,"adj_factor":1},{"trade_date":"2026-10-23","ts_code":"000623.SZ","close":10,"volume":100000,"adj_factor":1},{"trade_date":"2026-10-26","ts_code":"000623.SZ","close":10,"volume":100000,"adj_factor":1}]}`, close)
	if err := os.WriteFile(filepath.Join(dir, "bars_000623.SZ_2026-10-22_2026-10-26.json"), []byte(rows), 0600); err != nil {
		t.Fatal(err)
	}
	if adj != "" {
		if err := os.WriteFile(filepath.Join(dir, "adj_000623.SZ.json"), []byte(adj), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return akshare.New(akshare.Config{CacheDir: dir, SkipNetwork: true, StrictPIT: true}), from, to
}

const reviewAdj = `{"adj":[{"trade_date":"2026-10-23","adj_factor":25.692302747539102},{"trade_date":"2026-10-18","adj_factor":25.392813287206}]}`

func TestConvergenceProviderSparseAdj(t *testing.T) {
	p, from, to := reviewProvider(t, reviewAdj, 10)
	bars, err := p.Bars("000623.SZ", from, to)
	if err != nil {
		t.Fatal(err)
	}
	want := []float64{25.392813287206, 25.692302747539102, 25.692302747539102}
	if len(bars) != len(want) {
		t.Fatal(len(bars))
	}
	for i, b := range bars {
		if b.AdjFactor != want[i] {
			t.Fatalf("%s factor %v want %v", b.TradeDate, b.AdjFactor, want[i])
		}
	}
}

func TestConvergenceProviderRejectsMissingHistoricalAdj(t *testing.T) {
	for _, adj := range []string{"", `{"adj":[{"trade_date":"2026-10-23","adj_factor":2}]}`, `{"adj":[{"trade_date":"2026-10-18","adj_factor":-1}]}`} {
		p, from, to := reviewProvider(t, adj, 10)
		_, err := p.Bars("000623.SZ", from, to)
		if err == nil || !strings.Contains(err.Error(), "adj") {
			t.Fatalf("strict provider must reject absent/invalid historical factor: %v", err)
		}
	}
}

func TestConvergenceCachedLimitsCannotReachStrictFill(t *testing.T) {
	for _, close := range []float64{10, 99} {
		p, from, to := reviewProvider(t, reviewAdj, close)
		bars, err := p.Bars("000623.SZ", from, to)
		if err != nil {
			t.Fatal(err)
		}
		b := bars[0]
		universe.EnsureLimitPrices(&b, types.BoardSZSEMain, false)
		if b.HighLimit != 0 || b.LowLimit != 0 {
			t.Fatalf("untrusted positive cached limits survived: %+v", b)
		}
		cfg := config.Default()
		cfg.Data.StrictPIT = true
		cfg.Data.StrictQuotes = true
		e := backtest.Engine{Cfg: cfg, Bars: map[types.SecurityID]map[string]types.Bar{"000623.SZ": {from.String(): b}}}
		state := &backtest.PortfolioState{Cash: 100000}
		err = e.ApplyTargets(state, from, []types.TargetHolding{{TSCode: "000623.SZ", Weight: 1}}, to)
		if err == nil || !strings.Contains(err.Error(), "limit") || len(state.Fills) != 0 {
			t.Fatalf("strict fill must reject: %v", err)
		}
	}
}

func TestConvergencePythonProviderToStrictExecution(t *testing.T) {
	for _, close := range []string{"10", "99"} {
		t.Setenv("REVIEW_CLOSE", close)
		p := akshare.New(akshare.Config{PythonBin: "python3", HelperPath: filepath.Join("testdata", "convergence_helper.py"), StrictPIT: true})
		d := types.NewTradeDate(2026, 10, 22)
		bars, err := p.Bars("000623.SZ", d, d)
		if err != nil {
			t.Fatal(err)
		}
		if len(bars) != 1 {
			t.Fatal(len(bars))
		}
		b := bars[0]
		universe.EnsureLimitPrices(&b, types.BoardSZSEMain, false)
		if b.HighLimit != 0 || b.LowLimit != 0 || b.LimitSource != "none" {
			t.Fatalf("Python leaked Close into open limits: %+v", b)
		}
		cfg := config.Default()
		cfg.Data.StrictPIT = true
		e := backtest.Engine{Cfg: cfg, Bars: map[types.SecurityID]map[string]types.Bar{"000623.SZ": {d.String(): b}}}
		state := &backtest.PortfolioState{Cash: 100000}
		err = e.ApplyTargets(state, d, []types.TargetHolding{{TSCode: "000623.SZ", Weight: 1}}, types.NewTradeDate(2026, 10, 23))
		if err == nil || !strings.Contains(err.Error(), "limit") || len(state.Fills) != 0 {
			t.Fatalf("Python/provider/execution must reject unknown limits: %v", err)
		}
	}
}

package backtest_test

import (
	"testing"

	"quantitative-trading/internal/backtest"
	"quantitative-trading/internal/config"
	"quantitative-trading/internal/types"
	"quantitative-trading/internal/universe"
)

func TestBuyCostNoStampSellIncludesStamp(t *testing.T) {
	cfg := config.Default()
	notional := 1_000_000.0
	buy := backtest.BuyCost(cfg, notional)
	sell := backtest.SellCost(cfg, notional)
	// buy = 8 bps; sell = 13 bps
	if !backtest.NearlyEqual(buy, notional*8e-4, cfg.Backtest.FloatTol) {
		t.Fatalf("buy cost %v", buy)
	}
	if !backtest.NearlyEqual(sell, notional*13e-4, cfg.Backtest.FloatTol) {
		t.Fatalf("sell cost %v", sell)
	}
	stamp := notional * cfg.Costs.StampTaxBps / 1e4
	if !backtest.NearlyEqual(sell-buy, stamp, cfg.Backtest.FloatTol) {
		t.Fatalf("sell-buy should equal stamp only: %v vs %v", sell-buy, stamp)
	}
}

func TestTPlus1CannotSellSameDay(t *testing.T) {
	cfg := config.Default()
	cfg.Backtest.InitialCash = 10_000_000
	cfg.Execution.LotSize = 100

	d0 := types.NewTradeDate(2025, 1, 10) // Fri
	d1 := types.NewTradeDate(2025, 1, 13) // Mon exec
	d2 := types.NewTradeDate(2025, 1, 14) // Tue earliest sell

	code := types.SecurityID("600000.SH")
	mkBar := func(d types.TradeDate, open float64) types.Bar {
		return types.Bar{
			TradeDate: d, TSCode: code,
			Open: open, High: open * 1.01, Low: open * 0.99, Close: open,
			HighLimit: open * 1.1, LowLimit: open * 0.9,
			AdjFactor: 1, Volume: 1e6, Amount: 5e7,
		}
	}
	bars := map[types.SecurityID]map[string]types.Bar{
		code: {
			d0.String(): mkBar(d0, 10),
			d1.String(): mkBar(d1, 10),
			d2.String(): mkBar(d2, 10),
		},
	}
	cal := []types.TradeDate{d0, d1, d2}
	eng := &backtest.Engine{
		Cfg:      cfg,
		Calendar: cal,
		CalIndex: universe.BuildIndex(cal),
		Bars:     bars,
	}
	state := &backtest.PortfolioState{Cash: cfg.Backtest.InitialCash}

	// Buy on d1, available from d2
	targets := []types.TargetHolding{{TSCode: code, Weight: 0.5}}
	if err := eng.ApplyTargets(state, d1, targets, d2); err != nil {
		t.Fatal(err)
	}
	bought := state.Shares(code)
	if bought <= 0 {
		t.Fatal("expected buy fill")
	}
	if state.SellableShares(code, d1) != 0 {
		t.Fatalf("T+1: sellable on buy day must be 0, got %v", state.SellableShares(code, d1))
	}

	// Attempt full sell on same exec day d1 — should not sell
	sellTargets := []types.TargetHolding{} // empty → want 0 weight
	cashBefore := state.Cash
	sharesBefore := state.Shares(code)
	if err := eng.ApplyTargets(state, d1, sellTargets, d2); err != nil {
		t.Fatal(err)
	}
	if state.Shares(code) != sharesBefore {
		t.Fatal("must not sell same-day purchase")
	}
	if state.Cash != cashBefore {
		t.Fatal("cash should be unchanged when sell blocked")
	}

	// On d2, can sell
	if state.SellableShares(code, d2) != bought {
		t.Fatalf("sellable on next day want %v got %v", bought, state.SellableShares(code, d2))
	}
	if err := eng.ApplyTargets(state, d2, sellTargets, types.TradeDate{}); err != nil {
		t.Fatal(err)
	}
	if state.Shares(code) != 0 {
		t.Fatalf("expected flat after sell, got %v", state.Shares(code))
	}
	// Verify sell fills include stamp in cost
	var sellFill *types.Fill
	for i := range state.Fills {
		if state.Fills[i].Side == "SELL" {
			sellFill = &state.Fills[i]
		}
	}
	if sellFill == nil {
		t.Fatal("expected sell fill")
	}
	wantCost := backtest.SellCost(cfg, sellFill.Amount)
	if !backtest.NearlyEqual(sellFill.Cost, wantCost, cfg.Backtest.FloatTol) {
		t.Fatalf("sell cost %v want %v", sellFill.Cost, wantCost)
	}
}

func TestMetricsDoNotInventNumbers(t *testing.T) {
	m := backtest.ComputeMetrics(nil, 0)
	if m.HasCurve {
		t.Fatal("empty curve must not claim metrics")
	}
	if m.Sharpe.Available || m.TotalReturn.Available {
		t.Fatal("metrics must be unavailable without curve")
	}
	snaps := []types.DailySnapshot{
		{NAV: 100}, {NAV: 110}, {NAV: 105},
	}
	m2 := backtest.ComputeMetrics(snaps, 0)
	if !m2.HasCurve {
		t.Fatal("expected curve")
	}
	if !m2.TotalReturn.Available || !backtest.NearlyEqualDefault(m2.TotalReturn.Value, 0.05) {
		t.Fatalf("total return %v", m2.TotalReturn)
	}
}

func TestNearlyEqual(t *testing.T) {
	tol := config.FloatTolConfig{Rel: 1e-9, Abs: 1e-12}
	if !backtest.NearlyEqual(1.0, 1.0+1e-12, tol) {
		t.Fatal("abs tol")
	}
	if backtest.NearlyEqual(1.0, 1.1, tol) {
		t.Fatal("should differ")
	}
}

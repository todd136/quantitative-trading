package backtest_test

import (
	"strings"
	"testing"

	"quantitative-trading/internal/backtest"
	"quantitative-trading/internal/config"
	"quantitative-trading/internal/types"
	"quantitative-trading/internal/universe"
)

func TestConfirmedHaltUsesHaltValued(t *testing.T) {
	cfg := config.Default()
	code := types.SecurityID("600000.SH")
	d0 := types.NewTradeDate(2025, 1, 10)
	d1 := types.NewTradeDate(2025, 1, 13)
	d2 := types.NewTradeDate(2025, 1, 14)
	mk := func(d types.TradeDate, close float64, susp bool) types.Bar {
		return types.Bar{
			TradeDate: d, TSCode: code,
			Open: close, High: close, Low: close, Close: close,
			HighLimit: close * 1.1, LowLimit: close * 0.9,
			AdjFactor: 1, Volume: 1e6, Amount: 1e8, Suspended: susp,
		}
	}
	bars := map[types.SecurityID]map[string]types.Bar{
		code: {d0.String(): mk(d0, 10, false), d1.String(): mk(d1, 0, true), d2.String(): mk(d2, 10, false)},
	}
	cal := []types.TradeDate{d0, d1, d2}
	eng := &backtest.Engine{Cfg: cfg, Calendar: cal, CalIndex: universe.BuildIndex(cal), Bars: bars}
	state := &backtest.PortfolioState{
		Cash: 1_000_000,
		Lots: []types.Lot{{TSCode: code, Shares: 1000, AvailableFrom: d0, CostBasis: 10}},
	}
	s1, err := eng.MarkNAV(state, d1)
	if err != nil {
		t.Fatal(err)
	}
	if s1.HaltValued[code] != 10 {
		t.Fatalf("confirmed halt want HaltValued=10, got %v", s1.HaltValued)
	}
	if s1.MissingValued != nil {
		t.Fatalf("confirmed halt must not be missing_bar: %v", s1.MissingValued)
	}
	if s1.CarryMarks[code].Reason != backtest.CarryReasonHalt {
		t.Fatalf("reason %+v", s1.CarryMarks[code])
	}
	if s1.NAV != 1_010_000 {
		t.Fatalf("nav %v", s1.NAV)
	}
}

func TestMissingBarDoesNotDropHoldingFromNAV(t *testing.T) {
	cfg := config.Default()
	code := types.SecurityID("600000.SH")
	d0 := types.NewTradeDate(2025, 1, 10)
	d1 := types.NewTradeDate(2025, 1, 13)
	bars := map[types.SecurityID]map[string]types.Bar{
		code: {d0.String(): {TradeDate: d0, TSCode: code, Open: 10, Close: 10, HighLimit: 11, LowLimit: 9, AdjFactor: 1}},
	}
	cal := []types.TradeDate{d0, d1}
	eng := &backtest.Engine{Cfg: cfg, Calendar: cal, CalIndex: universe.BuildIndex(cal), Bars: bars}
	state := &backtest.PortfolioState{
		Cash: 100,
		Lots: []types.Lot{{TSCode: code, Shares: 10, AvailableFrom: d0, CostBasis: 10}},
	}
	s1, err := eng.MarkNAV(state, d1)
	if err != nil {
		t.Fatal(err)
	}
	if s1.Holdings[code] != 10 {
		t.Fatalf("holding dropped: %v", s1.Holdings)
	}
	if s1.NAV != 200 {
		t.Fatalf("nav %v want 200 (cash 100 + 10*10 carry)", s1.NAV)
	}
}

func TestStrictMissingOpenLimitsIntercept(t *testing.T) {
	cfg := config.Default()
	cfg.Data.StrictPIT = true
	cfg.Backtest.InitialCash = 10_000_000
	cfg.Execution.LotSize = 100
	code := types.SecurityID("600000.SH")
	d0 := types.NewTradeDate(2025, 1, 10)
	d1 := types.NewTradeDate(2025, 1, 13)
	d2 := types.NewTradeDate(2025, 1, 14)
	bar := types.Bar{
		TradeDate: d1, TSCode: code,
		Open: 10, Close: 12, PreClose: 0, HighLimit: 0, LowLimit: 0, AdjFactor: 1, Volume: 1e6, Amount: 1e8,
	}
	bars := map[types.SecurityID]map[string]types.Bar{
		code: {d0.String(): bar, d1.String(): bar, d2.String(): bar},
	}
	cal := []types.TradeDate{d0, d1, d2}
	eng := &backtest.Engine{Cfg: cfg, Calendar: cal, CalIndex: universe.BuildIndex(cal), Bars: bars}
	state := &backtest.PortfolioState{Cash: cfg.Backtest.InitialCash}
	err := eng.ApplyTargets(state, d1, []types.TargetHolding{{TSCode: code, Weight: 0.5}}, d2)
	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("strict missing limits want intercept, got %v", err)
	}
	if state.Shares(code) != 0 {
		t.Fatal("must not fill without reliable open limits")
	}
}

func TestNonStrictMissingOpenLimitsSkipFill(t *testing.T) {
	cfg := config.Default()
	cfg.Data.StrictPIT = false
	cfg.Backtest.InitialCash = 10_000_000
	cfg.Execution.LotSize = 100
	code := types.SecurityID("600000.SH")
	d0 := types.NewTradeDate(2025, 1, 10)
	d1 := types.NewTradeDate(2025, 1, 13)
	d2 := types.NewTradeDate(2025, 1, 14)
	bar := types.Bar{
		TradeDate: d1, TSCode: code,
		Open: 10, Close: 12, PreClose: 0, HighLimit: 0, LowLimit: 0, AdjFactor: 1, Volume: 1e6, Amount: 1e8,
	}
	bars := map[types.SecurityID]map[string]types.Bar{code: {d1.String(): bar}}
	cal := []types.TradeDate{d0, d1, d2}
	eng := &backtest.Engine{Cfg: cfg, Calendar: cal, CalIndex: universe.BuildIndex(cal), Bars: bars}
	state := &backtest.PortfolioState{Cash: cfg.Backtest.InitialCash}
	if err := eng.ApplyTargets(state, d1, []types.TargetHolding{{TSCode: code, Weight: 0.5}}, d2); err != nil {
		t.Fatal(err)
	}
	if state.Shares(code) != 0 {
		t.Fatal("missing limits must skip fill")
	}
	if eng.SkipCounts.SkippedMissingLimit != 1 {
		t.Fatalf("skip %+v", eng.SkipCounts)
	}
}

func TestCostsCountedOnce(t *testing.T) {
	cfg := config.Default()
	cfg.Backtest.InitialCash = 1_000_000
	cfg.Execution.LotSize = 100
	code := types.SecurityID("600000.SH")
	d0 := types.NewTradeDate(2025, 1, 10)
	d1 := types.NewTradeDate(2025, 1, 13)
	d2 := types.NewTradeDate(2025, 1, 14)
	mk := func(d types.TradeDate) types.Bar {
		return types.Bar{
			TradeDate: d, TSCode: code, Open: 10, Close: 10,
			HighLimit: 11, LowLimit: 9, AdjFactor: 1, Volume: 1e6, Amount: 1e8, PreClose: 10,
		}
	}
	bars := map[types.SecurityID]map[string]types.Bar{
		code: {d0.String(): mk(d0), d1.String(): mk(d1), d2.String(): mk(d2)},
	}
	cal := []types.TradeDate{d0, d1, d2}
	eng := &backtest.Engine{Cfg: cfg, Calendar: cal, CalIndex: universe.BuildIndex(cal), Bars: bars}
	state := &backtest.PortfolioState{Cash: cfg.Backtest.InitialCash}
	if err := eng.ApplyTargets(state, d1, []types.TargetHolding{{TSCode: code, Weight: 0.5}}, d2); err != nil {
		t.Fatal(err)
	}
	if len(state.Fills) != 1 {
		t.Fatalf("fills %d", len(state.Fills))
	}
	s0, _ := eng.MarkNAV(state, d0)
	s1, _ := eng.MarkNAV(state, d1)
	snaps := []types.DailySnapshot{s0, s1}
	backtest.AnnotateDailyActivity(snaps, state.Fills)
	m := backtest.ComputeMetricsFull(backtest.MetricsInput{Snaps: snaps, Fills: state.Fills})
	want := state.Fills[0].Cost / s0.NAV
	if !m.CostRatio.Available || !backtest.NearlyEqualAbs(m.CostRatio.Value, want, 1e-9) {
		t.Fatalf("cost ratio %v want %v (once, not doubled)", m.CostRatio, want)
	}
}

package backtest_test

import (
	"errors"
	"testing"

	"quantitative-trading/internal/backtest"
	"quantitative-trading/internal/config"
	"quantitative-trading/internal/portfolio"
	"quantitative-trading/internal/types"
	"quantitative-trading/internal/universe"
)

func TestSelectHoldingsIgnoresExecUnbuyable(t *testing.T) {
	a := types.SecurityID("600000.SH")
	b := types.SecurityID("600001.SH")
	comp := []types.CompositeResult{
		{TSCode: a, Composite: 2, Industry: "X"},
		{TSCode: b, Composite: 1, Industry: "X"},
	}
	targets := portfolio.SelectHoldings(comp, 1, 1.0)
	if len(targets) != 1 || targets[0].TSCode != a {
		t.Fatalf("signal-day freeze want %s, got %+v", a, targets)
	}

	cfg := config.Default()
	cfg.Backtest.InitialCash = 10_000_000
	cfg.Execution.LotSize = 100
	d0 := types.NewTradeDate(2025, 1, 10)
	d1 := types.NewTradeDate(2025, 1, 13)
	d2 := types.NewTradeDate(2025, 1, 14)
	mk := func(code types.SecurityID, d types.TradeDate, open, highLim float64, limitUp bool) types.Bar {
		st := ""
		if limitUp {
			st = "一字涨停"
			open = highLim
		}
		return types.Bar{
			TradeDate: d, TSCode: code,
			Open: open, High: open, Low: open, Close: open,
			HighLimit: highLim, LowLimit: open * 0.9,
			AdjFactor: 1, Volume: 1e6, Amount: 5e7, LimitStatus: st, PreClose: 10,
		}
	}
	bars := map[types.SecurityID]map[string]types.Bar{
		a: {
			d0.String(): mk(a, d0, 10, 11, false),
			d1.String(): mk(a, d1, 11, 11, true), // T+1 limit-up
			d2.String(): mk(a, d2, 10, 11, false),
		},
		b: {
			d0.String(): mk(b, d0, 10, 11, false),
			d1.String(): mk(b, d1, 10, 11, false),
			d2.String(): mk(b, d2, 10, 11, false),
		},
	}
	cal := []types.TradeDate{d0, d1, d2}
	eng := &backtest.Engine{Cfg: cfg, Calendar: cal, CalIndex: universe.BuildIndex(cal), Bars: bars}
	state := &backtest.PortfolioState{Cash: cfg.Backtest.InitialCash}
	if err := eng.ApplyTargets(state, d1, targets, d2); err != nil {
		t.Fatal(err)
	}
	if state.Shares(a) != 0 {
		t.Fatal("limit-up at exec must skip fill; target still A")
	}
	if eng.SkipCounts.SkippedLimitBuy != 1 {
		t.Fatalf("skip counts %+v", eng.SkipCounts)
	}

	// Flip T+1 to tradable: same frozen targets now fill. Selection did not depend on exec bars.
	bars[a][d1.String()] = mk(a, d1, 10, 11, false)
	eng2 := &backtest.Engine{Cfg: cfg, Calendar: cal, CalIndex: universe.BuildIndex(cal), Bars: bars}
	state2 := &backtest.PortfolioState{Cash: cfg.Backtest.InitialCash}
	targets2 := portfolio.SelectHoldings(comp, 1, 1.0)
	if targets2[0].TSCode != targets[0].TSCode {
		t.Fatal("changing exec-day limit status must not change signal-day targets")
	}
	if err := eng2.ApplyTargets(state2, d1, targets2, d2); err != nil {
		t.Fatal(err)
	}
	if state2.Shares(a) <= 0 {
		t.Fatal("expected fill once exec bar is tradable")
	}
}

func TestFrozenTargetsLoopIgnoresNextDayLimit(t *testing.T) {
	cfg := config.Default()
	cfg.Portfolio.RebalanceFreq = "daily"
	cfg.Backtest.InitialCash = 10_000_000
	cfg.Execution.LotSize = 100
	code := types.SecurityID("600000.SH")
	d0 := types.NewTradeDate(2025, 1, 10)
	d1 := types.NewTradeDate(2025, 1, 13)
	d2 := types.NewTradeDate(2025, 1, 14)
	limitBar := types.Bar{
		TradeDate: d1, TSCode: code,
		Open: 11, High: 11, Low: 11, Close: 11,
		HighLimit: 11, LowLimit: 9.9, AdjFactor: 1, Volume: 1e6, Amount: 5e7,
		LimitStatus: "一字涨停", PreClose: 10,
	}
	live := func(d types.TradeDate) types.Bar {
		return types.Bar{
			TradeDate: d, TSCode: code,
			Open: 10, High: 10.1, Low: 9.9, Close: 10,
			HighLimit: 11, LowLimit: 9, AdjFactor: 1, Volume: 1e6, Amount: 5e7, PreClose: 10,
		}
	}
	bars := map[types.SecurityID]map[string]types.Bar{
		code: {d0.String(): live(d0), d1.String(): limitBar, d2.String(): live(d2)},
	}
	cal := []types.TradeDate{d0, d1, d2}
	eng := &backtest.Engine{Cfg: cfg, Calendar: cal, CalIndex: universe.BuildIndex(cal), Bars: bars}
	state := &backtest.PortfolioState{Cash: cfg.Backtest.InitialCash}

	frozen := []types.TargetHolding{{TSCode: code, Weight: 0.5}}
	var gotTargets []types.SecurityID
	snaps, err := backtest.RunCalendar(eng, state, 0, 2, func(sig, exec types.TradeDate, _ types.DailySnapshot) ([]types.TargetHolding, error) {
		gotTargets = append(gotTargets, frozen[0].TSCode)
		return frozen, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(gotTargets) == 0 || gotTargets[0] != code {
		t.Fatalf("frozen targets %v", gotTargets)
	}
	if state.Shares(code) != 0 {
		t.Fatal("exec-day limit-up must skip fill")
	}
	if err := backtest.AssertUniqueIncreasingDates(snaps); err != nil {
		t.Fatal(err)
	}
}

func TestHaltMissingBarNoFakePnL(t *testing.T) {
	cfg := config.Default()
	code := types.SecurityID("600000.SH")
	d0 := types.NewTradeDate(2025, 1, 10)
	d1 := types.NewTradeDate(2025, 1, 13) // halt: no K-line
	d2 := types.NewTradeDate(2025, 1, 14) // resume, same close
	mk := func(d types.TradeDate, close float64) types.Bar {
		return types.Bar{
			TradeDate: d, TSCode: code,
			Open: close, High: close, Low: close, Close: close,
			HighLimit: close * 1.1, LowLimit: close * 0.9,
			AdjFactor: 1, Volume: 1e6, Amount: 1e8,
		}
	}
	bars := map[types.SecurityID]map[string]types.Bar{
		code: {d0.String(): mk(d0, 10), d2.String(): mk(d2, 10)},
	}
	cal := []types.TradeDate{d0, d1, d2}
	eng := &backtest.Engine{Cfg: cfg, Calendar: cal, CalIndex: universe.BuildIndex(cal), Bars: bars}
	state := &backtest.PortfolioState{
		Cash: 1_000_000,
		Lots: []types.Lot{{TSCode: code, Shares: 1000, AvailableFrom: d0, CostBasis: 10}},
	}
	s0, err := eng.MarkNAV(state, d0)
	if err != nil {
		t.Fatal(err)
	}
	s1, err := eng.MarkNAV(state, d1)
	if err != nil {
		t.Fatal(err)
	}
	s2, err := eng.MarkNAV(state, d2)
	if err != nil {
		t.Fatal(err)
	}
	if s0.NAV != s1.NAV || s1.NAV != s2.NAV {
		t.Fatalf("halt gap must not invent PnL: d0=%v d1=%v d2=%v", s0.NAV, s1.NAV, s2.NAV)
	}
	if s1.HaltValued[code] != 10 {
		t.Fatalf("halt mark want last close 10, got %v", s1.HaltValued)
	}
	if s2.HaltValued != nil {
		t.Fatalf("resume should use live close, halt marks=%v", s2.HaltValued)
	}
}

func TestMissingPriceOnLiveBarForbidsFill(t *testing.T) {
	cfg := config.Default()
	cfg.Backtest.InitialCash = 10_000_000
	cfg.Execution.LotSize = 100
	code := types.SecurityID("600000.SH")
	d0 := types.NewTradeDate(2025, 1, 10)
	d1 := types.NewTradeDate(2025, 1, 13)
	d2 := types.NewTradeDate(2025, 1, 14)
	bars := map[types.SecurityID]map[string]types.Bar{
		code: {
			d0.String(): {TradeDate: d0, TSCode: code, Open: 10, Close: 10, HighLimit: 11, LowLimit: 9, AdjFactor: 1, Volume: 1e6, Amount: 1e8},
			d1.String(): {TradeDate: d1, TSCode: code, Open: 0, Close: 0, HighLimit: 11, LowLimit: 9, AdjFactor: 1, Volume: 1e6, Amount: 1e8},
			d2.String(): {TradeDate: d2, TSCode: code, Open: 10, Close: 10, HighLimit: 11, LowLimit: 9, AdjFactor: 1, Volume: 1e6, Amount: 1e8},
		},
	}
	cal := []types.TradeDate{d0, d1, d2}
	eng := &backtest.Engine{Cfg: cfg, Calendar: cal, CalIndex: universe.BuildIndex(cal), Bars: bars}
	state := &backtest.PortfolioState{Cash: cfg.Backtest.InitialCash}
	err := eng.ApplyTargets(state, d1, []types.TargetHolding{{TSCode: code, Weight: 0.5}}, d2)
	if !errors.Is(err, backtest.ErrMissingPrice) {
		t.Fatalf("want ErrMissingPrice, got %v", err)
	}
	if state.Shares(code) != 0 {
		t.Fatal("must not fill without a price")
	}

	_, err = eng.MarkNAV(state, d1) // no holdings: live zero close is fine (nothing to mark)
	if err != nil {
		t.Fatal(err)
	}
	state.Lots = []types.Lot{{TSCode: code, Shares: 100, AvailableFrom: d0, CostBasis: 10}}
	_, err = eng.MarkNAV(state, d1)
	if !errors.Is(err, backtest.ErrMissingPrice) {
		t.Fatalf("holding on live day with close<=0 must alarm, got %v", err)
	}
}

func TestMissingBarSkipsFillAsHalt(t *testing.T) {
	cfg := config.Default()
	cfg.Backtest.InitialCash = 10_000_000
	cfg.Execution.LotSize = 100
	code := types.SecurityID("600000.SH")
	d0 := types.NewTradeDate(2025, 1, 10)
	d1 := types.NewTradeDate(2025, 1, 13)
	d2 := types.NewTradeDate(2025, 1, 14)
	bars := map[types.SecurityID]map[string]types.Bar{
		code: {
			d0.String(): {TradeDate: d0, TSCode: code, Open: 10, Close: 10, HighLimit: 11, LowLimit: 9, AdjFactor: 1, Volume: 1e6, Amount: 1e8},
			d2.String(): {TradeDate: d2, TSCode: code, Open: 10, Close: 10, HighLimit: 11, LowLimit: 9, AdjFactor: 1, Volume: 1e6, Amount: 1e8},
		},
	}
	cal := []types.TradeDate{d0, d1, d2}
	eng := &backtest.Engine{Cfg: cfg, Calendar: cal, CalIndex: universe.BuildIndex(cal), Bars: bars}
	state := &backtest.PortfolioState{Cash: cfg.Backtest.InitialCash}
	if err := eng.ApplyTargets(state, d1, []types.TargetHolding{{TSCode: code, Weight: 0.5}}, d2); err != nil {
		t.Fatal(err)
	}
	if state.Shares(code) != 0 {
		t.Fatal("missing exec bar must not fill")
	}
	if eng.SkipCounts.IntendedBuyOrders != 1 || eng.SkipCounts.SkippedSuspend != 1 {
		t.Fatalf("skip %+v", eng.SkipCounts)
	}
}

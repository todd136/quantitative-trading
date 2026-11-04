package universe_test

import (
	"testing"

	"quantitative-trading/internal/config"
	"quantitative-trading/internal/types"
	"quantitative-trading/internal/universe"
)

func TestLimitUpDownPct(t *testing.T) {
	if universe.LimitUpDownPct(types.BoardSSEMain, false) != 0.10 {
		t.Fatal("main")
	}
	if universe.LimitUpDownPct(types.BoardSTAR, false) != 0.20 {
		t.Fatal("star")
	}
	if universe.LimitUpDownPct(types.BoardChiNext, false) != 0.20 {
		t.Fatal("chinext")
	}
	if universe.LimitUpDownPct(types.BoardSSEMain, true) != 0.05 {
		t.Fatal("st")
	}
}

func TestEnsureLimitPrices(t *testing.T) {
	b := types.Bar{PreClose: 10, Close: 10.1}
	universe.EnsureLimitPrices(&b, types.BoardSSEMain, false)
	if b.HighLimit != 11.0 || b.LowLimit != 9.0 {
		t.Fatalf("limits %+v", b)
	}
	b2 := types.Bar{PreClose: 10, HighLimit: 12, LowLimit: 8}
	universe.EnsureLimitPrices(&b2, types.BoardSSEMain, false)
	if b2.HighLimit != 12 || b2.LowLimit != 8 {
		t.Fatal("must keep vendor limits")
	}
}

func TestEnsureLimitPricesNoCloseFallback(t *testing.T) {
	b := types.Bar{PreClose: 0, Close: 10.5}
	universe.EnsureLimitPrices(&b, types.BoardSSEMain, false)
	if b.HighLimit != 0 || b.LowLimit != 0 {
		t.Fatalf("must not infer open limits from same-day close, got %+v", b)
	}
}

func TestBuildUniverseSetsUnbuyableFromExecBars(t *testing.T) {
	cfg := config.Default()
	cfg.Universe.MinADV20 = 0
	cfg.Universe.MinADVValidDays = 0
	cfg.Universe.MinListTradingDays = 0
	cfg.Market.ExcludeST = false

	td := types.NewTradeDate(2024, 1, 10)
	exec := types.NewTradeDate(2024, 1, 11)
	code := types.SecurityID("600000.SH")
	barT := types.Bar{
		TradeDate: td, TSCode: code,
		Open: 10, High: 10.5, Low: 9.5, Close: 10, Volume: 1e6, Amount: 1e8,
		AdjFactor: 1, PreClose: 10, HighLimit: 11, LowLimit: 9,
	}
	barExec := types.Bar{
		TradeDate: exec, TSCode: code,
		Open: 11, High: 11, Low: 11, Close: 11, Volume: 1e6, Amount: 1e8,
		AdjFactor: 1, PreClose: 10, HighLimit: 11, LowLimit: 9,
		LimitStatus: "一字涨停",
	}
	cal := []types.TradeDate{td, exec}
	uctx := &universe.Context{
		Cfg:      cfg,
		Calendar: cal,
		CalIndex: universe.BuildIndex(cal),
		Securities: map[types.SecurityID]types.SecurityMaster{
			code: {TSCode: code, Board: types.BoardSSEMain, ListDate: types.NewTradeDate(2000, 1, 1)},
		},
		Bars: map[types.SecurityID]map[string]types.Bar{
			code: {td.String(): barT, exec.String(): barExec},
		},
	}
	univ := universe.BuildUniverse(uctx, td, map[types.SecurityID]types.Bar{code: barExec})
	if len(univ) != 1 {
		t.Fatalf("univ=%d", len(univ))
	}
	if !univ[0].Unbuyable {
		t.Fatal("expected Unbuyable from T+1 limit-up open")
	}

	// Signal-day selection must not consult T+1 bars: nil execBars ⇒ Unbuyable stays false
	// even when the next session is limit-up in ctx.Bars.
	univT := universe.BuildUniverse(uctx, td, nil)
	if len(univT) != 1 {
		t.Fatalf("univT=%d", len(univT))
	}
	if univT[0].Unbuyable {
		t.Fatal("nil execBars must not leak T+1 limit-up into signal-day Unbuyable")
	}
}

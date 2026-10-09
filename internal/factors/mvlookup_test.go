package factors_test

import (
	"testing"

	"quantitative-trading/internal/factors"
	"quantitative-trading/internal/types"
)

func TestMarketValueAsOfNearestPrior(t *testing.T) {
	asof := types.NewTradeDate(2024, 1, 10)
	rows := []types.MarketValue{
		{TradeDate: types.NewTradeDate(2024, 1, 8), TotalMV: 100},
		{TradeDate: types.NewTradeDate(2024, 1, 9), TotalMV: 110},
		{TradeDate: types.NewTradeDate(2024, 1, 11), TotalMV: 999}, // future
	}
	mv, ok := factors.MarketValueAsOf(rows, asof, 10)
	if !ok || mv != 110 {
		t.Fatalf("got %v ok=%v", mv, ok)
	}
	_, ok = factors.MarketValueAsOf(rows, asof, 0) // exact only; no exact on asof
	if ok {
		t.Fatal("lookback 0 should miss when no exact date")
	}
}

func TestForwardCloseReturn(t *testing.T) {
	d0 := types.NewTradeDate(2024, 1, 10)
	d1 := types.NewTradeDate(2024, 1, 11)
	bars := map[string]types.Bar{
		d0.String(): {Close: 10, TradeDate: d0},
		d1.String(): {Close: 11, TradeDate: d1},
	}
	r, ok := factors.ForwardCloseReturn(bars, d0, d1)
	if !ok || r < 0.099 || r > 0.101 {
		t.Fatalf("r=%v ok=%v", r, ok)
	}
	_, ok = factors.ForwardCloseReturn(bars, d0, types.NewTradeDate(2024, 1, 12))
	if ok {
		t.Fatal("missing toDate must fail")
	}
}

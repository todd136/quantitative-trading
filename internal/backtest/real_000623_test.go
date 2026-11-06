package backtest_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"quantitative-trading/internal/backtest"
	"quantitative-trading/internal/config"
	"quantitative-trading/internal/types"
	"quantitative-trading/internal/universe"
	"testing"
)

// Fixed holdings, gross dividend only. This does not validate trading limits or selection.
func TestReal000623CashDividendGrossLedger(t *testing.T) {
	dir := samplePath(t, "real", "000623_202610")
	raw, err := os.ReadFile(filepath.Join(dir, "bars_raw.json"))
	if err != nil {
		t.Fatal(err)
	}
	var input struct {
		Bars []sampleBarJSON `json:"bars"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		t.Fatal(err)
	}
	if len(input.Bars) != 7 {
		t.Fatal("expected seven original rows")
	}
	code := types.SecurityID("000623.SZ")
	bars := map[types.SecurityID]map[string]types.Bar{code: {}}
	var cal []types.TradeDate
	for _, r := range input.Bars {
		d, err := types.ParseTradeDate(r.TradeDate)
		if err != nil {
			t.Fatal(err)
		}
		cal = append(cal, d)
		bars[code][d.String()] = types.Bar{TradeDate: d, TSCode: code, Open: r.Open, Close: r.Close, High: r.High, Low: r.Low, Volume: r.Volume, AdjFactor: r.AdjFactor}
	}
	actions, err := backtest.LoadCorpActionsFile(filepath.Join(dir, "event.json"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Data.StrictPIT = true
	cfg.Data.StrictQuotes = true
	eng := &backtest.Engine{Cfg: cfg, Calendar: cal, CalIndex: universe.BuildIndex(cal), Bars: bars, CorpActions: actions, CorpActionsLoaded: true}
	state := &backtest.PortfolioState{Lots: []types.Lot{{TSCode: code, Shares: 1000, AvailableFrom: cal[0]}}}
	snaps, err := backtest.RunCalendar(eng, state, 0, len(cal), nil)
	if err != nil {
		t.Fatal(err)
	}
	for i, s := range snaps {
		cash := 0.0
		if i > 0 {
			cash = 200
		}
		want := input.Bars[i].Close*1000 + cash
		if s.Cash != cash || s.Receivable != 0 || s.Holdings[code] != 1000 || !backtest.NearlyEqualAbs(s.NAV, want, .01) {
			t.Fatalf("%s cash=%v recv=%v shares=%v nav=%v want=%v", s.TradeDate, s.Cash, s.Receivable, s.Holdings[code], s.NAV, want)
		}
		t.Logf("%s shares=1000 cash=%.2f receivable=0 NAV=%.2f", s.TradeDate, s.Cash, s.NAV)
	}
	if len(state.Fills) != 0 {
		t.Fatal("fixed-holdings accounting case must not trade")
	}
}

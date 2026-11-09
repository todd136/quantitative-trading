package backtest_test

import (
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"quantitative-trading/internal/backtest"
	"quantitative-trading/internal/config"
	"quantitative-trading/internal/types"
	"quantitative-trading/internal/universe"
	"strconv"
	"strings"
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
	adjRaw, err := os.ReadFile(filepath.Join(dir, "adj.json"))
	if err != nil {
		t.Fatal(err)
	}
	var factors struct {
		Adj []struct {
			TradeDate string  `json:"trade_date"`
			Factor    float64 `json:"adj_factor"`
		} `json:"adj"`
	}
	if err = json.Unmarshal(adjRaw, &factors); err != nil {
		t.Fatal(err)
	}
	code := types.SecurityID("000623.SZ")
	bars := map[types.SecurityID]map[string]types.Bar{code: {}}
	var cal []types.TradeDate
	for _, r := range input.Bars {
		d, err := types.ParseTradeDate(r.TradeDate)
		if err != nil {
			t.Fatal(err)
		}
		asof := ""
		factor := 0.0
		for _, f := range factors.Adj {
			if f.TradeDate <= r.TradeDate && f.TradeDate > asof && f.Factor > 0 {
				asof = f.TradeDate
				factor = f.Factor
			}
		}
		if factor <= 0 {
			t.Fatalf("missing actual adjustment factor %s", r.TradeDate)
		}
		r.AdjFactor = factor
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
	t.Run("actual_factor_jump_requires_event", func(t *testing.T) {
		saved := eng.CorpActions
		eng.CorpActions = nil
		defer func() { eng.CorpActions = saved }()
		if err := eng.ValidateCorpActions(); err == nil || !strings.Contains(err.Error(), "without matching corp action") {
			t.Fatalf("expected missing event rejection, got %v", err)
		}
	})
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
	t.Run("raw_prices_do_not_authorize_trade", func(t *testing.T) {
		tradeState := &backtest.PortfolioState{Cash: 50000}
		err := eng.ApplyTargets(tradeState, cal[1], []types.TargetHolding{{TSCode: code, Weight: 1}}, cal[2])
		if err == nil || !strings.Contains(err.Error(), "missing reliable open limit") {
			t.Fatalf("expected missing limit rejection, got %v", err)
		}
		if len(tradeState.Fills) != 0 || tradeState.Cash != 50000 {
			t.Fatal("invalid quote changed ledger")
		}
	})
	// Export the actual run, then reconcile independent original prices and cash.
	out := t.TempDir()
	if _, err := backtest.WriteSnapshots(out, snaps); err != nil {
		t.Fatal(err)
	}
	readCSV := func(name string) [][]string {
		t.Helper()
		f, err := os.Open(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		rows, err := csv.NewReader(f).ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		return rows
	}
	vals := readCSV("holdings_valuation.csv")
	t.Logf("holdings_valuation.csv: %v", vals)
	claims := readCSV("corp_action_ledger.csv")
	t.Logf("corp_action_ledger.csv: %v", claims)
	if len(vals) != 8 || len(claims) != 8 {
		t.Fatalf("audit row counts: %d %d", len(vals), len(claims))
	}
	for i := 0; i < 7; i++ {
		v, c := vals[i+1], claims[i+1]
		mv, err := strconv.ParseFloat(v[7], 64)
		if err != nil {
			t.Fatal(err)
		}
		wantPaid := "0"
		if i > 0 {
			wantPaid = "200"
		}
		if v[0] != input.Bars[i].TradeDate || v[2] != "1000" || v[4] != v[0] || v[5] != "session_close" || !backtest.NearlyEqualAbs(mv, input.Bars[i].Close*1000, .000001) {
			t.Fatalf("valuation %v", v)
		}
		if c[0] != v[0] || c[8] != "1000" || c[9] != "200" || c[10] != "0" || c[11] != wantPaid {
			t.Fatalf("claim %v", c)
		}
	}
	// Historical marks must not alias mutable later entitlement state.
	state.Entitlements[0].ReceivableOutstanding = 999
	if snaps[0].CorpActionMarks[0].ReceivableOutstanding != 0 || snaps[0].CorpActionMarks[0].CashPaid != 0 {
		t.Fatal("historical claim mutated")
	}
	if len(state.Fills) != 0 {
		t.Fatal("fixed-holdings accounting case must not trade")
	}
}

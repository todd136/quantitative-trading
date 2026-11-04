package backtest_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"quantitative-trading/internal/backtest"
	"quantitative-trading/internal/config"
	"quantitative-trading/internal/types"
	"quantitative-trading/internal/universe"
)

type sampleLedger struct {
	Independent       bool `json:"independent"`
	GeneratedByEngine bool `json:"generated_by_engine"`
	Rounding          struct {
		MoneyUnit float64 `json:"money_unit"`
		AbsTol    float64 `json:"abs_tol"`
	} `json:"rounding"`
	Code string      `json:"code"`
	Days []sampleDay `json:"days"`
}

type sampleDay struct {
	Date            string  `json:"date"`
	SellableOpen    float64 `json:"sellable_open"`
	Fees            float64 `json:"fees"`
	NewShares       float64 `json:"new_shares"`
	CashCredit      float64 `json:"cash_credit"`
	ReceivableClose float64 `json:"receivable_close"`
	CashClose       float64 `json:"cash_close"`
	SharesClose     float64 `json:"shares_close"`
	SellableClose   float64 `json:"sellable_close"`
	NAV             float64 `json:"nav"`
}

type sampleBarJSON struct {
	TradeDate string  `json:"trade_date"`
	TSCode    string  `json:"ts_code"`
	Open      float64 `json:"open"`
	High      float64 `json:"high"`
	Low       float64 `json:"low"`
	Close     float64 `json:"close"`
	PreClose  float64 `json:"pre_close"`
	HighLimit float64 `json:"high_limit"`
	LowLimit  float64 `json:"low_limit"`
	AdjFactor float64 `json:"adj_factor"`
	Volume    float64 `json:"volume"`
	Amount    float64 `json:"amount"`
}

func samplePath(t *testing.T, parts ...string) string {
	t.Helper()
	p := filepath.Join(append([]string{"..", "..", "testdata", "samples"}, parts...)...)
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("sample %s: %v", p, err)
	}
	return p
}

func loadSamplePackage(t *testing.T, dir string) (cfg config.Config, eng *backtest.Engine, state *backtest.PortfolioState, ledger sampleLedger) {
	t.Helper()
	rawL, err := os.ReadFile(filepath.Join(dir, "expected_ledger.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(rawL, &ledger); err != nil {
		t.Fatal(err)
	}
	if !ledger.Independent || ledger.GeneratedByEngine {
		t.Fatal("expected ledger must be independent and not engine-generated")
	}
	if ledger.Rounding.AbsTol != backtest.SampleAbsTol || ledger.Rounding.MoneyUnit != backtest.SampleMoneyUnit {
		t.Fatalf("sample rounding must be frozen at 0.01, got %+v", ledger.Rounding)
	}
	actions, err := backtest.LoadCorpActionsFile(filepath.Join(dir, "corp_actions.json"))
	if err != nil {
		t.Fatal(err)
	}
	rawB, err := os.ReadFile(filepath.Join(dir, "bars.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []sampleBarJSON
	if err := json.Unmarshal(rawB, &rows); err != nil {
		t.Fatal(err)
	}
	code := types.SecurityID(ledger.Code)
	bars := map[types.SecurityID]map[string]types.Bar{code: {}}
	var cal []types.TradeDate
	for _, r := range rows {
		d, err := types.ParseTradeDate(r.TradeDate)
		if err != nil {
			t.Fatal(err)
		}
		cal = append(cal, d)
		bars[code][d.String()] = types.Bar{
			TradeDate: d, TSCode: types.SecurityID(r.TSCode),
			Open: r.Open, High: r.High, Low: r.Low, Close: r.Close,
			PreClose: r.PreClose, HighLimit: r.HighLimit, LowLimit: r.LowLimit,
			AdjFactor: r.AdjFactor, Volume: r.Volume, Amount: r.Amount,
		}
	}
	cfg = config.Default()
	cfg.Data.StrictPIT = true
	cfg.Costs.CommissionBps = 0
	cfg.Costs.StampTaxBps = 0
	cfg.Costs.SlippageBps = 0
	cfg.Portfolio.RebalanceFreq = "weekly"
	cfg.Portfolio.RebalanceWeekday = 2
	cfg.Backtest.InitialCash = 1_000_000
	eng = &backtest.Engine{
		Cfg:               cfg,
		Calendar:          cal,
		CalIndex:          universe.BuildIndex(cal),
		Bars:              bars,
		CorpActions:       actions,
		CorpActionsLoaded: true,
	}
	state = &backtest.PortfolioState{
		Cash: 1_000_000,
		Lots: []types.Lot{{TSCode: code, Shares: 1000, AvailableFrom: cal[0], CostBasis: 50}},
	}
	return cfg, eng, state, ledger
}

func assertLedger(t *testing.T, eng *backtest.Engine, state *backtest.PortfolioState, ledger sampleLedger, snaps []types.DailySnapshot) {
	t.Helper()
	code := types.SecurityID(ledger.Code)
	tol := ledger.Rounding.AbsTol
	if len(snaps) != len(ledger.Days) {
		t.Fatalf("nav rows %d want %d", len(snaps), len(ledger.Days))
	}
	if err := backtest.AssertUniqueIncreasingDates(snaps); err != nil {
		t.Fatal(err)
	}
	for i, d := range ledger.Days {
		s := snaps[i]
		if s.TradeDate.String() != d.Date {
			t.Fatalf("day %d date %s want %s", i, s.TradeDate, d.Date)
		}
		if !backtest.NearlyEqualAbs(s.NAV, d.NAV, tol) {
			t.Fatalf("%s nav %v want %v", d.Date, s.NAV, d.NAV)
		}
		if !backtest.NearlyEqualAbs(s.Cash, d.CashClose, tol) {
			t.Fatalf("%s cash %v want %v", d.Date, s.Cash, d.CashClose)
		}
		if !backtest.NearlyEqualAbs(s.Receivable, d.ReceivableClose, tol) {
			t.Fatalf("%s receivable %v want %v", d.Date, s.Receivable, d.ReceivableClose)
		}
		if !backtest.NearlyEqualAbs(s.Holdings[code], d.SharesClose, tol) {
			t.Fatalf("%s shares %v want %v", d.Date, s.Holdings[code], d.SharesClose)
		}
		if !backtest.NearlyEqualAbs(s.CostTotal, d.Fees, tol) && d.Fees != 0 {
			t.Fatalf("%s fees on snap", d.Date)
		}
		wantSell := d.SellableClose
		if wantSell == 0 && i < len(ledger.Days)-1 {
			wantSell = d.SharesClose // default: fully sellable unless specified
		}
		if d.SellableClose != 0 || i == len(ledger.Days)-1 {
			gotSell := state.SellableShares(code, s.TradeDate)
			if i == len(ledger.Days)-1 && d.SellableClose != 0 {
				if !backtest.NearlyEqualAbs(gotSell, d.SellableClose, tol) {
					t.Fatalf("%s sellable %v want %v", d.Date, gotSell, d.SellableClose)
				}
			} else if d.SellableClose != 0 && i == len(snaps)-1 {
				if !backtest.NearlyEqualAbs(gotSell, wantSell, tol) {
					t.Fatalf("%s sellable %v want %v", d.Date, gotSell, wantSell)
				}
			}
		}
	}
	// Fills must use raw open, never CloseAdj.
	for _, f := range state.Fills {
		bar := eng.Bars[f.TSCode][f.TradeDate.String()]
		if f.Price != bar.Open {
			t.Fatalf("fill price %v must equal raw open %v, not CloseAdj %v", f.Price, bar.Open, bar.CloseAdj())
		}
	}
}

func TestSyntheticCashDivTransferLedger(t *testing.T) {
	dir := samplePath(t, "synthetic", "cash_div_transfer")
	_, eng, state, ledger := loadSamplePackage(t, dir)
	code := types.SecurityID(ledger.Code)
	snaps, err := backtest.RunCalendar(eng, state, 0, len(eng.Calendar), nil)
	if err != nil {
		t.Fatal(err)
	}
	assertLedger(t, eng, state, ledger, snaps)
	if state.Shares(code) != 1400 {
		t.Fatalf("end shares %v", state.Shares(code))
	}
	last := eng.Calendar[len(eng.Calendar)-1]
	if state.SellableShares(code, last) != 1400 {
		t.Fatalf("ex-day transfer shares should be sellable, got %v", state.SellableShares(code, last))
	}
}

func TestStaggeredCashAndSharesLedger(t *testing.T) {
	dir := samplePath(t, "synthetic", "staggered_cash_shares")
	_, eng, state, ledger := loadSamplePackage(t, dir)
	code := types.SecurityID(ledger.Code)
	snaps, err := backtest.RunCalendar(eng, state, 0, len(eng.Calendar), nil)
	if err != nil {
		t.Fatal(err)
	}
	assertLedger(t, eng, state, ledger, snaps)
	ex, _ := types.ParseTradeDate("2026-07-17")
	if state.SellableShares(code, ex) != 1000 {
		t.Fatalf("ex-day sellable want 1000 (new shares locked), got %v", state.SellableShares(code, ex))
	}
	pay, _ := types.ParseTradeDate("2026-07-20")
	var paySnap types.DailySnapshot
	for _, s := range snaps {
		if s.TradeDate.Equal(pay) {
			paySnap = s
		}
	}
	if paySnap.Receivable != 0 || !backtest.NearlyEqualAbs(paySnap.Cash, 1_000_600, backtest.SampleAbsTol) {
		t.Fatalf("pay-day cash/receivable %+v", paySnap)
	}
	td, _ := types.ParseTradeDate("2026-07-21")
	if state.SellableShares(code, td) != 1400 {
		t.Fatalf("tradable-day sellable %v", state.SellableShares(code, td))
	}
}

func TestReal688059EventFacts(t *testing.T) {
	p := samplePath(t, "real", "688059_202607", "event.json")
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var ev struct {
		TSCode             string  `json:"ts_code"`
		RecordDate         string  `json:"record_date"`
		ExDate             string  `json:"ex_date"`
		CashPayDate        string  `json:"cash_pay_date"`
		SharesTradableDate string  `json:"shares_tradable_date"`
		CashPerSharePretax float64 `json:"cash_per_share_pretax"`
		TransferRatio      float64 `json:"transfer_ratio"`
		BonusRatio         float64 `json:"bonus_ratio"`
		PricesInThisDir    bool    `json:"prices_in_this_dir"`
	}
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.Fatal(err)
	}
	if ev.TSCode != "688059.SH" || ev.RecordDate != "2026-07-16" || ev.ExDate != "2026-07-17" {
		t.Fatalf("event dates %+v", ev)
	}
	if ev.CashPayDate != "2026-07-17" || ev.SharesTradableDate != "2026-07-17" {
		t.Fatalf("pay/tradable %+v", ev)
	}
	if ev.CashPerSharePretax != 0.60 || ev.TransferRatio != 0.40 || ev.BonusRatio != 0 {
		t.Fatalf("ratios %+v", ev)
	}
	if ev.PricesInThisDir {
		t.Fatal("real event dir must not mix constructed bars")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(p), "bars.json")); err == nil {
		t.Fatal("real event dir must not contain constructed bars.json")
	}
}

func TestReal688059EventAccounting(t *testing.T) {
	// Prices are constructed identity-check values in this test, not claimed prints.
	p := samplePath(t, "real", "688059_202607", "event.json")
	raw, _ := os.ReadFile(p)
	var ev struct {
		TSCode             string  `json:"ts_code"`
		RecordDate         string  `json:"record_date"`
		ExDate             string  `json:"ex_date"`
		CashPayDate        string  `json:"cash_pay_date"`
		SharesTradableDate string  `json:"shares_tradable_date"`
		CashPerSharePretax float64 `json:"cash_per_share_pretax"`
		TransferRatio      float64 `json:"transfer_ratio"`
	}
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.Fatal(err)
	}
	code := types.SecurityID(ev.TSCode)
	rec, _ := types.ParseTradeDate(ev.RecordDate)
	ex, _ := types.ParseTradeDate(ev.ExDate)
	pre := types.NewTradeDate(2026, 7, 15)
	pxEx := (50.0 - ev.CashPerSharePretax) / (1 + ev.TransferRatio)
	mk := func(d types.TradeDate, px, preClose float64) types.Bar {
		return types.Bar{
			TradeDate: d, TSCode: code, Open: px, High: px, Low: px, Close: px,
			PreClose: preClose, HighLimit: preClose * 1.2, LowLimit: preClose * 0.8,
			AdjFactor: 1, Volume: 1, Amount: 1,
		}
	}
	cal := []types.TradeDate{pre, rec, ex}
	bars := map[types.SecurityID]map[string]types.Bar{
		code: {
			pre.String(): mk(pre, 50, 50),
			rec.String(): mk(rec, 50, 50),
			ex.String():  mk(ex, pxEx, 50),
		},
	}
	cfg := config.Default()
	cfg.Data.StrictPIT = true
	cfg.Costs.CommissionBps, cfg.Costs.StampTaxBps, cfg.Costs.SlippageBps = 0, 0, 0
	pay, _ := types.ParseTradeDate(ev.CashPayDate)
	tradable, _ := types.ParseTradeDate(ev.SharesTradableDate)
	eng := &backtest.Engine{
		Cfg:      cfg,
		Calendar: cal,
		CalIndex: universe.BuildIndex(cal),
		Bars:     bars,
		CorpActions: []types.CorpAction{{
			TSCode: code, Kind: backtest.CorpKindProfitDistribution,
			RecordDate: rec, ExDate: ex, CashPayDate: pay, SharesTradableDate: tradable,
			CashPerShare: ev.CashPerSharePretax, TransferRatio: ev.TransferRatio,
		}},
		CorpActionsLoaded: true,
	}
	state := &backtest.PortfolioState{
		Cash: 1_000_000,
		Lots: []types.Lot{{TSCode: code, Shares: 1000, AvailableFrom: pre, CostBasis: 50}},
	}
	snaps, err := backtest.RunCalendar(eng, state, 0, len(cal), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 3 {
		t.Fatalf("snaps %d", len(snaps))
	}
	if !backtest.NearlyEqualAbs(snaps[1].NAV, 1_050_000, backtest.SampleAbsTol) {
		t.Fatalf("record nav %v", snaps[1].NAV)
	}
	if !backtest.NearlyEqualAbs(snaps[2].NAV, 1_050_000, backtest.SampleAbsTol) {
		t.Fatalf("ex nav %v (must not use adj price as a substitute for cash/share credits)", snaps[2].NAV)
	}
	if state.Shares(code) != 1400 {
		t.Fatalf("shares %v", state.Shares(code))
	}
	if !backtest.NearlyEqualAbs(state.Cash, 1_000_600, backtest.SampleAbsTol) {
		t.Fatalf("cash %v", state.Cash)
	}
}

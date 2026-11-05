package backtest_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"quantitative-trading/internal/backtest"
	"quantitative-trading/internal/config"
	"quantitative-trading/internal/types"
	"quantitative-trading/internal/universe"
)

func TestReviewStrictQuotesFailsUnknownMissingBar(t *testing.T) {
	cfg := config.Default()
	cfg.Data.StrictQuotes = true
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
	_, err := eng.MarkNAV(state, d1)
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("formal/strict quotes must fail-close unknown missing bar, got %v", err)
	}
}

func TestReviewDiagnosticCarryPersistsMarks(t *testing.T) {
	cfg := config.Default()
	cfg.Data.StrictQuotes = false
	code := types.SecurityID("600000.SH")
	d0 := types.NewTradeDate(2025, 1, 10)
	d1 := types.NewTradeDate(2025, 1, 13)
	bars := map[types.SecurityID]map[string]types.Bar{
		code: {d0.String(): {TradeDate: d0, TSCode: code, Open: 10, Close: 10, HighLimit: 11, LowLimit: 9, AdjFactor: 1}},
	}
	cal := []types.TradeDate{d0, d1}
	eng := &backtest.Engine{Cfg: cfg, Calendar: cal, CalIndex: universe.BuildIndex(cal), Bars: bars}
	state := &backtest.PortfolioState{
		Cash: 100, Receivable: 3,
		Lots: []types.Lot{{TSCode: code, Shares: 10, AvailableFrom: d0, CostBasis: 10}},
	}
	s1, err := eng.MarkNAV(state, d1)
	if err != nil {
		t.Fatal(err)
	}
	if s1.MissingValued[code] != 10 || s1.Receivable != 3 {
		t.Fatalf("diagnostic carry %+v", s1)
	}
	dir := t.TempDir()
	if _, err := backtest.WriteSnapshots(dir, []types.DailySnapshot{s1}); err != nil {
		t.Fatal(err)
	}
	nav, err := os.ReadFile(filepath.Join(dir, "nav.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(nav), "receivable") {
		t.Fatalf("nav.csv must persist receivable: %s", nav)
	}
	marks, err := os.ReadFile(filepath.Join(dir, "valuation_marks.csv"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(marks)
	if !strings.Contains(text, "missing_bar") || !strings.Contains(text, d0.String()) {
		t.Fatalf("valuation_marks.csv must export as-of/reason: %s", text)
	}
}

func TestReviewMultiNameReceivableNotStolen(t *testing.T) {
	cfg := config.Default()
	cfg.Data.StrictPIT = true
	cfg.Costs.CommissionBps, cfg.Costs.StampTaxBps, cfg.Costs.SlippageBps = 0, 0, 0
	a := types.SecurityID("AAA.SZ")
	b := types.SecurityID("BBB.SZ")
	d0 := types.NewTradeDate(2026, 7, 14)
	rec := types.NewTradeDate(2026, 7, 15)
	ex := types.NewTradeDate(2026, 7, 16)
	payA := types.NewTradeDate(2026, 7, 20)
	cal := []types.TradeDate{d0, rec, ex, payA}
	mk := func(code types.SecurityID, d types.TradeDate, px float64) types.Bar {
		return types.Bar{TradeDate: d, TSCode: code, Open: px, High: px, Low: px, Close: px, PreClose: px, HighLimit: px * 1.1, LowLimit: px * 0.9, AdjFactor: 1}
	}
	bars := map[types.SecurityID]map[string]types.Bar{
		a: {d0.String(): mk(a, d0, 10), rec.String(): mk(a, rec, 10), ex.String(): mk(a, ex, 9), payA.String(): mk(a, payA, 9)},
		b: {d0.String(): mk(b, d0, 20), rec.String(): mk(b, rec, 20), ex.String(): mk(b, ex, 19.8), payA.String(): mk(b, payA, 19.8)},
	}
	eng := &backtest.Engine{
		Cfg: cfg, Calendar: cal, CalIndex: universe.BuildIndex(cal), Bars: bars,
		CorpActions: []types.CorpAction{
			{TSCode: a, Kind: backtest.CorpKindCashDividend, RecordDate: rec, ExDate: ex, CashPayDate: payA, CashPerShare: 0.10},
			{TSCode: b, Kind: backtest.CorpKindCashDividend, RecordDate: rec, ExDate: ex, CashPayDate: ex, CashPerShare: 0.20},
		},
		CorpActionsLoaded: true,
	}
	state := &backtest.PortfolioState{
		Cash: 1_000_000,
		Lots: []types.Lot{
			{TSCode: a, Shares: 1000, AvailableFrom: d0, CostBasis: 10},
			{TSCode: b, Shares: 100, AvailableFrom: d0, CostBasis: 20},
		},
	}
	snaps, err := backtest.RunCalendar(eng, state, 0, len(cal), nil)
	if err != nil {
		t.Fatal(err)
	}
	var exSnap, paySnap types.DailySnapshot
	for _, s := range snaps {
		if s.TradeDate.Equal(ex) {
			exSnap = s
		}
		if s.TradeDate.Equal(payA) {
			paySnap = s
		}
	}
	// A: 1000*0.10=100 receivable until 7/20; B: 100*0.20=20 paid on ex.
	if !backtest.NearlyEqualAbs(exSnap.Receivable, 100, backtest.SampleAbsTol) {
		t.Fatalf("ex-day receivable want 100 (A only), got %v — B same-day pay must not steal A's receivable", exSnap.Receivable)
	}
	if !backtest.NearlyEqualAbs(paySnap.Receivable, 0, backtest.SampleAbsTol) {
		t.Fatalf("pay-day receivable want 0, got %v", paySnap.Receivable)
	}
}

func TestReviewBaselineCostNotPolluted(t *testing.T) {
	d0, _ := types.ParseTradeDate("2026-06-30")
	d1, _ := types.ParseTradeDate("2026-07-01")
	d2, _ := types.ParseTradeDate("2026-07-02")
	snaps := []types.DailySnapshot{
		{TradeDate: d0, NAV: 100, CostTotal: 7, Turnover: 0.5},
		{TradeDate: d1, NAV: 90, CostTotal: 2, Turnover: 0.1},
		{TradeDate: d2, NAV: 99, CostTotal: 0, Turnover: 0},
	}
	cfg := testCfgWithSplit("2026-06-30", "2026-07-02", "2026-06-30", "2026-07-01")
	r, err := backtest.BuildReport(cfg, snaps, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	val := r.Segments["validate"]
	// validate=(6/30, 7/1] → period cost 2 / baseline NAV 100 = 2%, not (7+2)/100=9%
	if !val.Metrics.CostRatio.Available || !backtest.NearlyEqualAbs(val.Metrics.CostRatio.Value, 0.02, 1e-12) {
		t.Fatalf("validate cost ratio %v want 0.02 (period only)", val.Metrics.CostRatio)
	}
	if val.Metrics.NDays != 1 {
		t.Fatalf("NDays must be period observations, got %d", val.Metrics.NDays)
	}
	if !val.Metrics.TotalReturn.Available || !backtest.NearlyEqualAbs(val.Metrics.TotalReturn.Value, -0.10, 1e-12) {
		t.Fatalf("validate return still needs baseline: %v", val.Metrics.TotalReturn)
	}
}

func TestReviewBareArrayRejectedForFormalMeta(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "corp_actions.json")
	if err := os.WriteFile(p, []byte(`[]`), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := backtest.LoadCorpActionsSnapshot(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := backtest.RequireFormalCorpActionsMeta("akshare", snap); err == nil {
		t.Fatal("akshare/formal must reject bare [] without source/coverage/complete")
	}
}

func TestReviewSnapshotMetaAccepted(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "corp_actions.json")
	body := `{
		"source": "unit-test",
		"provider": "akshare",
		"coverage_start": "2026-01-01",
		"coverage_end": "2026-12-31",
		"complete": true,
		"events": []
	}`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := backtest.LoadCorpActionsSnapshot(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := backtest.RequireFormalCorpActionsMeta("akshare", snap); err != nil {
		t.Fatal(err)
	}
	if !snap.Complete || snap.Source != "unit-test" {
		t.Fatalf("meta %+v", snap)
	}
}

// Doc §11 E: cash 1000; A receivable 100; B pay 20 same-day → cash 1020/recv 100; then A pay → cash 1120/recv 0.
func TestReviewDocCashReceivableExact(t *testing.T) {
	cfg := config.Default()
	cfg.Data.StrictPIT = true
	cfg.Costs.CommissionBps, cfg.Costs.StampTaxBps, cfg.Costs.SlippageBps = 0, 0, 0
	a := types.SecurityID("AAA.SZ")
	b := types.SecurityID("BBB.SZ")
	d0 := types.NewTradeDate(2026, 7, 14)
	rec := types.NewTradeDate(2026, 7, 15)
	ex := types.NewTradeDate(2026, 7, 16)
	payA := types.NewTradeDate(2026, 7, 20)
	cal := []types.TradeDate{d0, rec, ex, payA}
	mk := func(code types.SecurityID, d types.TradeDate, px float64) types.Bar {
		return types.Bar{TradeDate: d, TSCode: code, Open: px, High: px, Low: px, Close: px, PreClose: px, HighLimit: px * 1.1, LowLimit: px * 0.9, AdjFactor: 1}
	}
	bars := map[types.SecurityID]map[string]types.Bar{
		a: {d0.String(): mk(a, d0, 10), rec.String(): mk(a, rec, 10), ex.String(): mk(a, ex, 9), payA.String(): mk(a, payA, 9)},
		b: {d0.String(): mk(b, d0, 20), rec.String(): mk(b, rec, 20), ex.String(): mk(b, ex, 19.8), payA.String(): mk(b, payA, 19.8)},
	}
	eng := &backtest.Engine{
		Cfg: cfg, Calendar: cal, CalIndex: universe.BuildIndex(cal), Bars: bars,
		CorpActions: []types.CorpAction{
			{TSCode: a, Kind: backtest.CorpKindCashDividend, RecordDate: rec, ExDate: ex, CashPayDate: payA, CashPerShare: 0.10},
			{TSCode: b, Kind: backtest.CorpKindCashDividend, RecordDate: rec, ExDate: ex, CashPayDate: ex, CashPerShare: 0.20},
		},
		CorpActionsLoaded: true,
	}
	state := &backtest.PortfolioState{
		Cash: 1000,
		Lots: []types.Lot{
			{TSCode: a, Shares: 1000, AvailableFrom: d0, CostBasis: 10},
			{TSCode: b, Shares: 100, AvailableFrom: d0, CostBasis: 20},
		},
	}
	snaps, err := backtest.RunCalendar(eng, state, 0, len(cal), nil)
	if err != nil {
		t.Fatal(err)
	}
	var exSnap, paySnap types.DailySnapshot
	for _, s := range snaps {
		if s.TradeDate.Equal(ex) {
			exSnap = s
		}
		if s.TradeDate.Equal(payA) {
			paySnap = s
		}
	}
	if !backtest.NearlyEqualAbs(exSnap.Cash, 1020, backtest.SampleAbsTol) || !backtest.NearlyEqualAbs(exSnap.Receivable, 100, backtest.SampleAbsTol) {
		t.Fatalf("after B pay: cash/recv want 1020/100, got %v/%v", exSnap.Cash, exSnap.Receivable)
	}
	if !backtest.NearlyEqualAbs(paySnap.Cash, 1120, backtest.SampleAbsTol) || !backtest.NearlyEqualAbs(paySnap.Receivable, 0, backtest.SampleAbsTol) {
		t.Fatalf("after A pay: cash/recv want 1120/0, got %v/%v", paySnap.Cash, paySnap.Receivable)
	}
}

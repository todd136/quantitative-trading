package backtest_test

import (
	"strings"
	"testing"

	"quantitative-trading/internal/backtest"
	"quantitative-trading/internal/config"
	"quantitative-trading/internal/types"
	"quantitative-trading/internal/universe"
)

func corpCal() []types.TradeDate {
	return []types.TradeDate{
		types.NewTradeDate(2026, 7, 14),
		types.NewTradeDate(2026, 7, 15),
		types.NewTradeDate(2026, 7, 16),
		types.NewTradeDate(2026, 7, 17),
	}
}

func corpBars(code types.SecurityID, cal []types.TradeDate, pxEx float64) map[types.SecurityID]map[string]types.Bar {
	out := map[types.SecurityID]map[string]types.Bar{code: {}}
	ex := cal[len(cal)-1]
	for _, d := range cal {
		px := 50.0
		pre := 50.0
		if d.Equal(ex) {
			px = pxEx
		}
		out[code][d.String()] = types.Bar{
			TradeDate: d, TSCode: code, Open: px, High: px, Low: px, Close: px,
			PreClose: pre, HighLimit: 55, LowLimit: 45, AdjFactor: 1, Volume: 1, Amount: 1,
		}
	}
	return out
}

func cashTransferAction(code types.SecurityID, cal []types.TradeDate) types.CorpAction {
	return types.CorpAction{
		TSCode: code, Kind: backtest.CorpKindProfitDistribution,
		RecordDate: cal[2], ExDate: cal[3], CashPayDate: cal[3], SharesTradableDate: cal[3],
		CashPerShare: 0.60, TransferRatio: 0.40,
	}
}

func zeroCostStrict() config.Config {
	cfg := config.Default()
	cfg.Data.StrictPIT = true
	cfg.Costs.CommissionBps, cfg.Costs.StampTaxBps, cfg.Costs.SlippageBps = 0, 0, 0
	cfg.Portfolio.RebalanceFreq = "weekly"
	return cfg
}

func TestSellBeforeRecordGetsNoEntitlement(t *testing.T) {
	code := types.SecurityID("SYNTH.SZ")
	cal := corpCal()
	pxEx := (50.0 - 0.60) / 1.40
	cfg := zeroCostStrict()
	cfg.Portfolio.RebalanceFreq = "daily"
	eng := &backtest.Engine{
		Cfg: cfg, Calendar: cal, CalIndex: universe.BuildIndex(cal),
		Bars:              corpBars(code, cal, pxEx),
		CorpActions:       []types.CorpAction{cashTransferAction(code, cal)},
		CorpActionsLoaded: true,
	}
	state := &backtest.PortfolioState{
		Cash: 1_000_000,
		Lots: []types.Lot{{TSCode: code, Shares: 1000, AvailableFrom: cal[0], CostBasis: 50}},
	}
	_, err := backtest.RunCalendar(eng, state, 0, len(cal), func(sig, exec types.TradeDate, _ types.DailySnapshot) ([]types.TargetHolding, error) {
		if exec.Equal(cal[1]) { // sell Wed 7/15, before record Thu 7/16
			return []types.TargetHolding{}, nil
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if state.Shares(code) != 0 {
		t.Fatalf("sold before record, shares %v", state.Shares(code))
	}
	if len(state.Entitlements) != 0 {
		t.Fatalf("no entitlement after pre-record sale: %+v", state.Entitlements)
	}
	if state.Cash < 1_049_000 { // 1e6 + 1000*50 minus tiny if any
		t.Fatalf("cash after sale %v", state.Cash)
	}
}

func TestBuyOnExDateGetsNoEntitlement(t *testing.T) {
	code := types.SecurityID("SYNTH.SZ")
	cal := corpCal()
	pxEx := (50.0 - 0.60) / 1.40
	cfg := zeroCostStrict()
	cfg.Portfolio.RebalanceFreq = "daily"
	eng := &backtest.Engine{
		Cfg: cfg, Calendar: cal, CalIndex: universe.BuildIndex(cal),
		Bars:              corpBars(code, cal, pxEx),
		CorpActions:       []types.CorpAction{cashTransferAction(code, cal)},
		CorpActionsLoaded: true,
	}
	state := &backtest.PortfolioState{Cash: 1_000_000}
	_, err := backtest.RunCalendar(eng, state, 0, len(cal), func(sig, exec types.TradeDate, _ types.DailySnapshot) ([]types.TargetHolding, error) {
		if exec.Equal(cal[3]) { // buy on ex date
			return []types.TargetHolding{{TSCode: code, Weight: 0.05}}, nil
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if state.Shares(code) <= 0 {
		t.Fatal("expected ex-date buy")
	}
	if len(state.Entitlements) != 0 {
		t.Fatalf("ex-date buy must not capture record-date entitlement: %+v", state.Entitlements)
	}
}

func TestSellAfterRecordKeepsEntitlement(t *testing.T) {
	code := types.SecurityID("SYNTH.SZ")
	cal := corpCal()
	pxEx := (50.0 - 0.60) / 1.40
	cfg := zeroCostStrict()
	cfg.Portfolio.RebalanceFreq = "daily"
	eng := &backtest.Engine{
		Cfg: cfg, Calendar: cal, CalIndex: universe.BuildIndex(cal),
		Bars:              corpBars(code, cal, pxEx),
		CorpActions:       []types.CorpAction{cashTransferAction(code, cal)},
		CorpActionsLoaded: true,
	}
	state := &backtest.PortfolioState{
		Cash: 1_000_000,
		Lots: []types.Lot{{TSCode: code, Shares: 1000, AvailableFrom: cal[0], CostBasis: 50}},
	}
	holdW := 1000.0 * 50.0 / 1_050_000.0
	snaps, err := backtest.RunCalendar(eng, state, 0, len(cal), func(sig, exec types.TradeDate, _ types.DailySnapshot) ([]types.TargetHolding, error) {
		if exec.Equal(cal[3]) { // sell on ex, after record close capture
			return []types.TargetHolding{}, nil
		}
		return []types.TargetHolding{{TSCode: code, Weight: holdW}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = snaps
	if len(state.Entitlements) != 1 {
		t.Fatalf("record-date capture must survive later sale: %+v", state.Entitlements)
	}
	// cash: sale proceeds of remaining original shares + dividend 600; transfer shares also sold if sellable
	if state.Cash < 1_000_600 {
		t.Fatalf("dividend cash must remain after post-record sale, cash=%v", state.Cash)
	}
}

func TestDuplicateCorpActionRejected(t *testing.T) {
	code := types.SecurityID("SYNTH.SZ")
	cal := corpCal()
	a := cashTransferAction(code, cal)
	cfg := zeroCostStrict()
	eng := &backtest.Engine{
		Cfg: cfg, Calendar: cal, CalIndex: universe.BuildIndex(cal),
		Bars:              corpBars(code, cal, 35.285714285714285),
		CorpActions:       []types.CorpAction{a, a},
		CorpActionsLoaded: true,
	}
	state := &backtest.PortfolioState{Cash: 1}
	_, err := backtest.RunCalendar(eng, state, 0, len(cal), nil)
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("want duplicate error, got %v", err)
	}
}

func TestUnsupportedCorpActionFailClosed(t *testing.T) {
	code := types.SecurityID("SYNTH.SZ")
	cal := corpCal()
	cfg := zeroCostStrict()
	eng := &backtest.Engine{
		Cfg: cfg, Calendar: cal, CalIndex: universe.BuildIndex(cal),
		Bars: corpBars(code, cal, 50),
		CorpActions: []types.CorpAction{{
			TSCode: code, Kind: "rights_issue",
			RecordDate: cal[2], ExDate: cal[3],
			UnsupportedReason: "rights issue not implemented",
		}},
		CorpActionsLoaded: true,
	}
	_, err := backtest.RunCalendar(eng, &backtest.PortfolioState{Cash: 1}, 0, len(cal), nil)
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("want unsupported, got %v", err)
	}
}

func TestStrictMissingCorpActionsFile(t *testing.T) {
	cfg := zeroCostStrict()
	cal := corpCal()
	eng := &backtest.Engine{
		Cfg: cfg, Calendar: cal, CalIndex: universe.BuildIndex(cal),
		Bars:              map[types.SecurityID]map[string]types.Bar{},
		CorpActionsLoaded: false,
	}
	_, err := backtest.RunCalendar(eng, &backtest.PortfolioState{Cash: 1}, 0, len(cal), nil)
	if err == nil || !strings.Contains(err.Error(), "corp-action") {
		t.Fatalf("strict missing corp actions: %v", err)
	}
}

func TestAdjFactorOneStillAppliesEvent(t *testing.T) {
	code := types.SecurityID("SYNTH.SZ")
	cal := corpCal()
	pxEx := (50.0 - 0.60) / 1.40
	cfg := zeroCostStrict()
	bars := corpBars(code, cal, pxEx)
	for d, b := range bars[code] {
		b.AdjFactor = 1
		bars[code][d] = b
	}
	eng := &backtest.Engine{
		Cfg: cfg, Calendar: cal, CalIndex: universe.BuildIndex(cal), Bars: bars,
		CorpActions:       []types.CorpAction{cashTransferAction(code, cal)},
		CorpActionsLoaded: true,
	}
	state := &backtest.PortfolioState{
		Cash: 1_000_000,
		Lots: []types.Lot{{TSCode: code, Shares: 1000, AvailableFrom: cal[0], CostBasis: 50}},
	}
	snaps, err := backtest.RunCalendar(eng, state, 0, len(cal), nil)
	if err != nil {
		t.Fatal(err)
	}
	if state.Shares(code) != 1400 {
		t.Fatalf("adj=1 must not suppress transfer, shares=%v", state.Shares(code))
	}
	if !backtest.NearlyEqualAbs(snaps[len(snaps)-1].NAV, 1_050_000, backtest.SampleAbsTol) {
		t.Fatalf("nav %v", snaps[len(snaps)-1].NAV)
	}
}

func TestStrictAdjJumpWithoutAction(t *testing.T) {
	code := types.SecurityID("SYNTH.SZ")
	cal := corpCal()
	cfg := zeroCostStrict()
	bars := corpBars(code, cal, 50)
	b := bars[code][cal[3].String()]
	b.AdjFactor = 1.4
	bars[code][cal[3].String()] = b
	eng := &backtest.Engine{
		Cfg: cfg, Calendar: cal, CalIndex: universe.BuildIndex(cal), Bars: bars,
		CorpActions: []types.CorpAction{}, CorpActionsLoaded: true,
	}
	_, err := backtest.RunCalendar(eng, &backtest.PortfolioState{Cash: 1}, 0, len(cal), nil)
	if err == nil || !strings.Contains(err.Error(), "adj_factor jump") {
		t.Fatalf("want adj jump error, got %v", err)
	}
}

func TestNAVIncludesLastDayAndDailyUniqueness(t *testing.T) {
	code := types.SecurityID("SYNTH.SZ")
	cal := corpCal()
	cfg := zeroCostStrict()
	eng := &backtest.Engine{
		Cfg: cfg, Calendar: cal, CalIndex: universe.BuildIndex(cal),
		Bars:              corpBars(code, cal, (50-0.60)/1.40),
		CorpActions:       []types.CorpAction{cashTransferAction(code, cal)},
		CorpActionsLoaded: true,
	}
	state := &backtest.PortfolioState{
		Cash: 1_000_000,
		Lots: []types.Lot{{TSCode: code, Shares: 1000, AvailableFrom: cal[0], CostBasis: 50}},
	}
	snaps, err := backtest.RunCalendar(eng, state, 0, len(cal), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != len(cal) {
		t.Fatalf("want daily nav including last day, got %d", len(snaps))
	}
	if !snaps[len(snaps)-1].TradeDate.Equal(cal[len(cal)-1]) {
		t.Fatal("last nav date")
	}
}

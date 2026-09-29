package universe_test

import (
	"path/filepath"
	"testing"

	"quantitative-trading/internal/config"
	"quantitative-trading/internal/data/fixture"
	"quantitative-trading/internal/types"
	"quantitative-trading/internal/universe"
)

func fixtureDir(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "testdata", "fixtures")
}

func TestBoardFilterSTARInBSEOut(t *testing.T) {
	cfg := config.Default()
	prov, err := fixture.New(fixtureDir(t))
	if err != nil {
		t.Fatal(err)
	}
	cal, _ := prov.LoadCalendar()
	calIndex := universe.BuildIndex(cal)
	secs, _ := prov.Securities()
	secMap := map[types.SecurityID]types.SecurityMaster{}
	for _, s := range secs {
		secMap[s.TSCode] = s
	}
	st, _ := prov.ST()
	inds, _ := prov.Industry()
	barsByCode := map[types.SecurityID]map[string]types.Bar{}
	zero := types.TradeDate{}
	for code := range secMap {
		bs, _ := prov.Bars(code, zero, zero)
		m := map[string]types.Bar{}
		for _, b := range bs {
			m[b.TradeDate.String()] = b
		}
		barsByCode[code] = m
	}
	// Pick a mid-sample date with enough list days: 2025-01-10
	td, err := types.ParseTradeDate("2025-01-10")
	if err != nil {
		t.Fatal(err)
	}
	uctx := &universe.Context{
		Cfg: cfg, Calendar: cal, CalIndex: calIndex,
		Securities: secMap, Bars: barsByCode, ST: st, Industry: inds,
	}
	univ := universe.BuildUniverse(uctx, td, nil)
	seen := map[types.SecurityID]bool{}
	for _, s := range univ {
		seen[s.Security.TSCode] = true
		if s.Security.Board == types.BoardBSE {
			t.Fatalf("BSE %s must be excluded", s.Security.TSCode)
		}
	}
	if !seen["688001.SH"] {
		t.Fatal("STAR 688001.SH should be included when otherwise eligible")
	}
	if seen["830001.BJ"] {
		t.Fatal("BSE 830001.BJ must be excluded")
	}
}

func TestOpenIsLimitUpOneWord(t *testing.T) {
	prov, err := fixture.New(fixtureDir(t))
	if err != nil {
		t.Fatal(err)
	}
	td, _ := types.ParseTradeDate("2025-01-16")
	bs, _ := prov.Bars("600004.SH", td, td)
	if len(bs) != 1 {
		t.Fatal("expected limit-up bar")
	}
	if !universe.OpenIsLimitUpOneWord(bs[0]) {
		t.Fatalf("expected limit-up open, got open=%v high_limit=%v status=%s", bs[0].Open, bs[0].HighLimit, bs[0].LimitStatus)
	}
	if !universe.CannotBuyAtOpen(bs[0]) {
		t.Fatal("CannotBuyAtOpen should be true")
	}
}

func TestMissingListDateSkipsMinListFilter(t *testing.T) {
	cfg := config.Default()
	cfg.Universe.MinListTradingDays = 60
	cfg.Universe.MinADV20 = 0
	cfg.Universe.MinADVValidDays = 0
	cfg.Market.ExcludeST = false

	td := types.NewTradeDate(2024, 1, 10)
	bar := types.Bar{
		TradeDate: td, TSCode: "600000.SH",
		Open: 10, High: 10, Low: 10, Close: 10,
		Volume: 1e6, Amount: 1e8, AdjFactor: 1,
	}
	smMissing := types.SecurityMaster{
		TSCode: "600000.SH", Name: "X", Board: types.BoardSSEMain,
		// ListDate zero → missing
	}
	ok, _ := universe.IsEligible(cfg, smMissing, bar, false, 0, 1e8, 20, true)
	if !ok {
		t.Fatal("missing list_date should degrade: skip min_list and remain eligible")
	}
	smNew := types.SecurityMaster{
		TSCode: "600001.SH", Name: "Y", Board: types.BoardSSEMain,
		ListDate: types.NewTradeDate(2024, 1, 2),
	}
	ok, _ = universe.IsEligible(cfg, smNew, bar, false, 5, 1e8, 20, false)
	if ok {
		t.Fatal("known short list_date with listDays<min must be rejected")
	}
	if universe.HasListDate(smMissing) {
		t.Fatal("zero ListDate must report !HasListDate")
	}
	if !universe.HasListDate(smNew) {
		t.Fatal("set ListDate must report HasListDate")
	}
}

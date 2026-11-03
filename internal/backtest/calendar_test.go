package backtest_test

import (
	"testing"
	"time"

	"quantitative-trading/internal/backtest"
	"quantitative-trading/internal/config"
	"quantitative-trading/internal/types"
	"quantitative-trading/internal/universe"
)

func weekdayCalendar(start types.TradeDate, n int, skip map[string]bool) []types.TradeDate {
	out := make([]types.TradeDate, 0, n)
	t := start.Time()
	for len(out) < n {
		if t.Weekday() != time.Saturday && t.Weekday() != time.Sunday && !skip[types.TradeDate(t).String()] {
			out = append(out, types.TradeDate(t))
		}
		t = t.AddDate(0, 0, 1)
	}
	return out
}

func TestResolveWindowIncludesLastTradingDay(t *testing.T) {
	cal := weekdayCalendar(types.NewTradeDate(2025, 1, 6), 10, nil)
	cfg := config.Default()
	cfg.Backtest.StartDate = cal[0].String()
	cfg.Backtest.EndDate = cal[len(cal)-1].String()
	start, end, err := backtest.ResolveWindow(cal, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if start != 0 || end != len(cal) {
		t.Fatalf("window [%d,%d) want [0,%d)", start, end, len(cal))
	}
}

func TestNAVCalendarUniqueWeeklyHolidayAndLastDay(t *testing.T) {
	// Two weeks of sessions; Wednesday 2025-01-08 omitted (holiday).
	// Weekly rebalance weekday=2 → Wednesday. Signal only on 2025-01-15.
	skip := map[string]bool{"2025-01-08": true}
	cal := weekdayCalendar(types.NewTradeDate(2025, 1, 6), 9, skip) // through Fri 17th, no Jan 8
	cfg := config.Default()
	cfg.Portfolio.RebalanceFreq = "weekly"
	cfg.Portfolio.RebalanceWeekday = 2
	cfg.Backtest.InitialCash = 1_000_000
	cfg.Execution.LotSize = 100

	code := types.SecurityID("600000.SH")
	bars := map[types.SecurityID]map[string]types.Bar{}
	bars[code] = map[string]types.Bar{}
	for _, d := range cal {
		bars[code][d.String()] = types.Bar{
			TradeDate: d, TSCode: code,
			Open: 10, High: 10.1, Low: 9.9, Close: 10,
			HighLimit: 11, LowLimit: 9, AdjFactor: 1, Volume: 1e6, Amount: 1e8,
		}
	}
	eng := &backtest.Engine{
		Cfg: cfg, Calendar: cal, CalIndex: universe.BuildIndex(cal), Bars: bars,
	}
	state := &backtest.PortfolioState{Cash: cfg.Backtest.InitialCash}

	var signalDates []string
	var execDates []string
	snaps, err := backtest.RunCalendar(eng, state, 0, len(cal), func(sig, exec types.TradeDate, _ types.DailySnapshot) ([]types.TargetHolding, error) {
		signalDates = append(signalDates, sig.String())
		execDates = append(execDates, exec.String())
		return []types.TargetHolding{{TSCode: code, Weight: 0.5}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := backtest.AssertUniqueIncreasingDates(snaps); err != nil {
		t.Fatal(err)
	}
	if len(snaps) != len(cal) {
		t.Fatalf("nav rows=%d want one per trading day %d", len(snaps), len(cal))
	}
	for i, s := range snaps {
		if !s.TradeDate.Equal(cal[i]) {
			t.Fatalf("snap[%d]=%s want %s", i, s.TradeDate, cal[i])
		}
	}
	// Holiday Wednesday is not a session, so no signal that week. Only 2025-01-15.
	if len(signalDates) != 1 || signalDates[0] != "2025-01-15" {
		t.Fatalf("signals=%v want [2025-01-15]", signalDates)
	}
	if execDates[0] != "2025-01-16" {
		t.Fatalf("exec=%v", execDates)
	}
	last := snaps[len(snaps)-1].TradeDate.String()
	if last != cal[len(cal)-1].String() {
		t.Fatalf("last nav %s want %s", last, cal[len(cal)-1])
	}
}

func TestNAVCalendarDailyNoDoubleWrite(t *testing.T) {
	cal := weekdayCalendar(types.NewTradeDate(2025, 1, 6), 5, nil)
	cfg := config.Default()
	cfg.Portfolio.RebalanceFreq = "daily"
	cfg.Backtest.InitialCash = 1_000_000
	eng := &backtest.Engine{
		Cfg: cfg, Calendar: cal, CalIndex: universe.BuildIndex(cal),
		Bars: map[types.SecurityID]map[string]types.Bar{},
	}
	state := &backtest.PortfolioState{Cash: cfg.Backtest.InitialCash}
	snaps, err := backtest.RunCalendar(eng, state, 0, len(cal), func(sig, exec types.TradeDate, _ types.DailySnapshot) ([]types.TargetHolding, error) {
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := backtest.AssertUniqueIncreasingDates(snaps); err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 5 {
		t.Fatalf("len=%d", len(snaps))
	}
}

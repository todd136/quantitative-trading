package backtest_test

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"quantitative-trading/internal/backtest"
	"quantitative-trading/internal/types"
)

func snap(date string, nav float64) types.DailySnapshot {
	d, err := types.ParseTradeDate(date)
	if err != nil {
		panic(err)
	}
	return types.DailySnapshot{TradeDate: d, NAV: nav}
}

func snapFull(date string, nav float64, holdings map[types.SecurityID]float64, weights map[types.SecurityID]float64, to, cost float64) types.DailySnapshot {
	s := snap(date, nav)
	s.Holdings = holdings
	s.Weights = weights
	s.Turnover = to
	s.CostTotal = cost
	return s
}

func TestMetricsEmptyNoCurve(t *testing.T) {
	m := backtest.ComputeMetrics(nil, 0)
	if m.HasCurve {
		t.Fatal("empty curve must not claim metrics")
	}
	if m.TotalReturn.Available || m.Sharpe.Available || m.Calmar.Available {
		t.Fatal("optional metrics must be unavailable without curve")
	}
}

func TestMetricsKnownNAVPath(t *testing.T) {
	// NAV: 100 → 110 → 105 → 120
	// total return = 120/100 - 1 = 0.20
	// max DD: peak 110, then 105 → dd = 105/110-1 = -0.04545...
	snaps := []types.DailySnapshot{
		snap("2024-01-02", 100),
		snap("2024-01-03", 110),
		snap("2024-01-04", 105),
		snap("2024-01-05", 120),
	}
	m := backtest.ComputeMetrics(snaps, 0)
	if !m.HasCurve {
		t.Fatal("expected curve")
	}
	if !backtest.NearlyEqualDefault(m.TotalReturn.Value, 0.20) {
		t.Fatalf("total return %v want 0.20", m.TotalReturn.Value)
	}
	wantDD := 105.0/110.0 - 1
	if !backtest.NearlyEqualDefault(m.MaxDrawdown.Value, wantDD) {
		t.Fatalf("maxDD %v want %v", m.MaxDrawdown.Value, wantDD)
	}
	// Ann return = (1.2)^(252/3) - 1
	wantAnn := math.Pow(1.2, 252.0/3.0) - 1
	if !m.AnnReturn.Available || !backtest.NearlyEqualDefault(m.AnnReturn.Value, wantAnn) {
		t.Fatalf("ann return %v want %v", m.AnnReturn.Value, wantAnn)
	}
	if !m.Calmar.Available {
		t.Fatal("calmar should be available")
	}
	wantCalmar := wantAnn / math.Abs(wantDD)
	if !backtest.NearlyEqualDefault(m.Calmar.Value, wantCalmar) {
		t.Fatalf("calmar %v want %v", m.Calmar.Value, wantCalmar)
	}
}

func TestCalmarNAWhenNoDrawdown(t *testing.T) {
	snaps := []types.DailySnapshot{
		snap("2024-01-02", 100),
		snap("2024-01-03", 101),
		snap("2024-01-04", 102),
	}
	m := backtest.ComputeMetrics(snaps, 0)
	if m.MaxDrawdown.Value != 0 {
		t.Fatalf("expected 0 DD, got %v", m.MaxDrawdown.Value)
	}
	if m.Calmar.Available {
		t.Fatal("Calmar must be N/A when maxDD==0")
	}
}

func TestDailyWinRate(t *testing.T) {
	// rets: +10%, -5%, +20% → 2/3 win
	snaps := []types.DailySnapshot{
		snap("2024-01-02", 100),
		snap("2024-01-03", 110),
		snap("2024-01-04", 104.5),
		snap("2024-01-05", 125.4),
	}
	m := backtest.ComputeMetrics(snaps, 0)
	if !m.DailyWinRate.Available || !backtest.NearlyEqualDefault(m.DailyWinRate.Value, 2.0/3.0) {
		t.Fatalf("daily win rate %v", m.DailyWinRate)
	}
}

func TestMonthlyWinRateNeedsTwoMonths(t *testing.T) {
	snaps := []types.DailySnapshot{
		snap("2024-01-02", 100),
		snap("2024-01-03", 110),
		snap("2024-01-31", 105),
	}
	m := backtest.ComputeMetrics(snaps, 0)
	if m.MonthlyWinRate.Available {
		t.Fatal("single calendar month → monthly win rate N/A")
	}

	snaps2 := []types.DailySnapshot{
		snap("2024-01-02", 100),
		snap("2024-01-31", 110),
		snap("2024-02-29", 121), // +10% month
		snap("2024-03-29", 100), // down month
	}
	m2 := backtest.ComputeMetrics(snaps2, 0)
	if !m2.MonthlyWinRate.Available {
		t.Fatal("expected monthly win rate")
	}
	// months: Jan=110, Feb=121 (+), Mar=100 (-) → 1/2
	if !backtest.NearlyEqualDefault(m2.MonthlyWinRate.Value, 0.5) {
		t.Fatalf("monthly win %v", m2.MonthlyWinRate.Value)
	}
}

func TestTurnoverAndCostFromSnaps(t *testing.T) {
	snaps := []types.DailySnapshot{
		snapFull("2024-01-02", 100, nil, nil, 0.10, 1.0),
		snapFull("2024-01-03", 100, nil, nil, 0.20, 2.0),
		snapFull("2024-01-04", 100, nil, nil, 0.00, 0.0),
	}
	m := backtest.ComputeMetricsFull(backtest.MetricsInput{Snaps: snaps, RiskFree: 0})
	avg := (0.10 + 0.20 + 0.0) / 3
	if !m.TurnoverDailyAvgTwoWay.Available || !backtest.NearlyEqualDefault(m.TurnoverDailyAvgTwoWay.Value, avg) {
		t.Fatalf("turnover avg %v want %v", m.TurnoverDailyAvgTwoWay, avg)
	}
	if !backtest.NearlyEqualDefault(m.TurnoverDailyAvgOneWay.Value, avg/2) {
		t.Fatal("one-way should be half two-way")
	}
	if !backtest.NearlyEqualDefault(m.CostRatio.Value, 3.0/100.0) {
		t.Fatalf("cost ratio %v", m.CostRatio.Value)
	}
}

func TestBenchmarkRelative(t *testing.T) {
	snaps := []types.DailySnapshot{
		snap("2024-01-02", 100),
		snap("2024-01-03", 110), // +10%
		snap("2024-01-04", 115.5), // +5%
		snap("2024-01-05", 121.275), // +5%
	}
	d0, _ := types.ParseTradeDate("2024-01-02")
	d1, _ := types.ParseTradeDate("2024-01-03")
	d2, _ := types.ParseTradeDate("2024-01-04")
	d3, _ := types.ParseTradeDate("2024-01-05")
	bench := []types.IndexBar{
		{TradeDate: d0, Close: 1000},
		{TradeDate: d1, Close: 1005},   // +0.5%
		{TradeDate: d2, Close: 1015.05}, // +1.0%
		{TradeDate: d3, Close: 1020.125}, // +0.5%
	}
	m := backtest.ComputeMetricsFull(backtest.MetricsInput{Snaps: snaps, Benchmark: bench})
	if !m.ExcessReturn.Available {
		t.Fatalf("excess should be available: %+v", m.ExcessReturn)
	}
	if !m.TrackingError.Available {
		t.Fatal("tracking error expected")
	}
	if !m.InformationRatio.Available {
		t.Fatalf("IR expected: %+v", m.InformationRatio)
	}
	if !m.ExcessMaxDD.Available {
		t.Fatal("excess max DD expected")
	}
}

func TestBenchmarkMissingIsNA(t *testing.T) {
	snaps := []types.DailySnapshot{
		snap("2024-01-02", 100),
		snap("2024-01-03", 110),
	}
	m := backtest.ComputeMetricsFull(backtest.MetricsInput{Snaps: snaps})
	if m.ExcessReturn.Available || m.TrackingError.Available || m.InformationRatio.Available || m.ExcessMaxDD.Available {
		t.Fatal("benchmark-relative must be N/A without series")
	}
}

func TestPortfolioStructure(t *testing.T) {
	snaps := []types.DailySnapshot{
		snapFull("2024-01-02", 100,
			map[types.SecurityID]float64{"A": 1, "B": 1},
			map[types.SecurityID]float64{"A": 0.4, "B": 0.6}, 0, 0),
		snapFull("2024-01-03", 100,
			map[types.SecurityID]float64{"A": 1},
			map[types.SecurityID]float64{"A": 1.0}, 0, 0),
	}
	m := backtest.ComputeMetricsFull(backtest.MetricsInput{Snaps: snaps})
	if !m.AvgHoldingsCount.Available || !backtest.NearlyEqualDefault(m.AvgHoldingsCount.Value, 1.5) {
		t.Fatalf("avg holdings %v", m.AvgHoldingsCount)
	}
	if !m.MaxSingleNameWeight.Available || !backtest.NearlyEqualDefault(m.MaxSingleNameWeight.Value, 1.0) {
		t.Fatalf("max weight %v", m.MaxSingleNameWeight)
	}
	if m.UnfilledSkipRate.Available || m.BrinsonAttribution.Available {
		t.Fatal("stubs must remain N/A")
	}
}

func TestAnnotateDailyActivity(t *testing.T) {
	d, _ := types.ParseTradeDate("2024-01-03")
	snaps := []types.DailySnapshot{
		snap("2024-01-02", 100),
		snap("2024-01-03", 200),
	}
	fills := []types.Fill{
		{TradeDate: d, Amount: 40, Cost: 1.5},
		{TradeDate: d, Amount: 20, Cost: 0.5},
	}
	backtest.AnnotateDailyActivity(snaps, fills)
	if snaps[1].CostTotal != 2.0 {
		t.Fatalf("cost %v", snaps[1].CostTotal)
	}
	if !backtest.NearlyEqualDefault(snaps[1].Turnover, 60.0/200.0) {
		t.Fatalf("turnover %v", snaps[1].Turnover)
	}
}

func TestWriteReports(t *testing.T) {
	dir := t.TempDir()
	snaps := []types.DailySnapshot{
		snap("2024-01-02", 100),
		snap("2024-01-03", 110),
		snap("2024-02-01", 115),
		snap("2024-02-02", 120),
	}
	cfg := testCfgWithSplit("2024-01-02", "2024-02-28", "2024-01-10", "2024-01-31")
	r, err := backtest.BuildReport(cfg, snaps, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.SpecVersion != backtest.SpecVersion {
		t.Fatalf("spec %s", r.SpecVersion)
	}
	if r.FormalSegment != backtest.SegmentOOS {
		t.Fatalf("formal %s", r.FormalSegment)
	}
	jp, cp, mp, err := backtest.WriteAllReports(dir, r)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{jp, cp, mp} {
		if _, err := os.Stat(p); err != nil {
			t.Fatal(err)
		}
	}
	md, _ := os.ReadFile(mp)
	if !strings.Contains(string(md), backtest.SpecVersion) {
		t.Fatal("report.md missing spec version")
	}
	if !strings.Contains(string(md), "Parameter freeze") {
		t.Fatal("report.md missing freeze note")
	}
}

func TestNoMagicPerformanceConstantsInMetricsPackage(t *testing.T) {
	// Optional sanity: metrics source must not embed claimed live performance numbers.
	src, err := os.ReadFile(filepath.Join("metrics.go"))
	if err != nil {
		// when tests run as backtest_test external package, file is sibling via module root
		src, err = os.ReadFile(filepath.Join("..", "..", "internal", "backtest", "metrics.go"))
		if err != nil {
			t.Skip("cannot locate metrics.go")
		}
	}
	text := string(src)
	forbidden := []string{"0.15 sharpe", "sharpe = 1.", "total_return: 0.2", "win_rate: 0.55"}
	lower := strings.ToLower(text)
	for _, f := range forbidden {
		if strings.Contains(lower, f) {
			t.Fatalf("suspicious magic constant %q in metrics.go", f)
		}
	}
}

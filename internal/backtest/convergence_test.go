package backtest_test

import (
	"fmt"
	"math"
	"quantitative-trading/internal/backtest"
	"quantitative-trading/internal/types"
	"strings"
	"testing"
)

func TestConvergenceLayerTiesHaveStableCodeOrder(t *testing.T) {
	p := backtest.DailyFactorPanel{Quality: map[types.SecurityID]float64{}, ForwardRet: map[types.SecurityID]float64{}}
	for i := 0; i < 10; i++ {
		c := types.SecurityID(fmt.Sprintf("%02d", i))
		p.Quality[c] = 0
		p.ForwardRet[c] = float64(i) / 100
	}
	for attempt := 0; attempt < 50; attempt++ {
		r := backtest.ComputeFactorLayers([]backtest.DailyFactorPanel{p}, 5)
		for i := 0; i < 5; i++ {
			want := float64(4*i+1) / 200
			got := r[0].Layers[i].MeanReturn.Value
			if math.Abs(got-want) > 1e-12 {
				t.Fatalf("Q%d got %v want %v; tied scores must sort by code", i+1, got, want)
			}
		}
	}
}

func TestConvergenceObservedZeroActivity(t *testing.T) {
	s := []types.DailySnapshot{snap("2026-01-05", 100), snap("2026-01-06", 100)}
	missing := backtest.ComputeMetrics(s, 0)
	if missing.CostRatio.Available {
		t.Fatal("unobserved cost should remain NA")
	}
	backtest.AnnotateDailyActivity(s, nil)
	m := backtest.ComputeMetrics(s, 0)
	if !m.CostRatio.Available || m.CostRatio.Value != 0 || !m.TurnoverDailyAvgTwoWay.Available || m.TurnoverDailyAvgTwoWay.Value != 0 {
		t.Fatalf("observed no-trade days must report zero: cost=%+v turnover=%+v", m.CostRatio, m.TurnoverDailyAvgTwoWay)
	}
}

func TestConvergenceYearBoundaryAndMissingBaseline(t *testing.T) {
	base := snap("2025-12-31", 100)
	base.CostTotal = 7
	s := []types.DailySnapshot{snap("2026-01-05", 90), snap("2026-01-06", 99)}
	s[0].CostTotal = 2
	ys := backtest.ComputeYearlyMetrics(s, backtest.MetricsInput{Baseline: &base})
	if len(ys) != 1 || ys[0].Metrics.NDays != 2 || math.Abs(ys[0].Metrics.TotalReturn.Value+0.01) > 1e-12 || math.Abs(ys[0].Metrics.CostRatio.Value-.02) > 1e-12 {
		t.Fatalf("year boundary: %+v", ys)
	}
	m := backtest.ComputeMetricsFull(backtest.MetricsInput{Snaps: s, ExpectBaseline: true})
	if !strings.Contains(m.TotalReturn.Note, "no valid pre-period baseline") {
		t.Fatalf("missing baseline must be explicit: %+v", m.TotalReturn)
	}
}

func TestConvergenceDuplicateCorpActionRejected(t *testing.T) {
	_, eng, state, _ := loadSamplePackage(t, samplePath(t, "synthetic", "cash_div_transfer"))
	eng.CorpActions = append(eng.CorpActions, eng.CorpActions[0])
	cash := state.Cash
	_, err := backtest.RunCalendar(eng, state, 0, len(eng.Calendar), nil)
	if err == nil || !strings.Contains(err.Error(), "duplicate") || state.Cash != cash {
		t.Fatalf("duplicate must reject before any credit: %v", err)
	}
}

func TestConvergenceCorpMetadataCoverage(t *testing.T) {
	s := backtest.CorpActionsSnapshot{Source: "controlled", Provider: "akshare", Complete: true, CoverageStart: "2026-01-01", CoverageEnd: "2026-12-31"}
	from, to := types.NewTradeDate(2026, 7, 1), types.NewTradeDate(2026, 10, 30)
	if err := backtest.RequireFormalCorpActionsMeta("akshare", s, from, to); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*backtest.CorpActionsSnapshot){func(s *backtest.CorpActionsSnapshot) { s.Provider = "fixture" }, func(s *backtest.CorpActionsSnapshot) { s.CoverageStart = "bad" }, func(s *backtest.CorpActionsSnapshot) { s.CoverageStart = "2026-08-01" }, func(s *backtest.CorpActionsSnapshot) { s.CoverageEnd = "2026-10-28" }} {
		bad := s
		change(&bad)
		if err := backtest.RequireFormalCorpActionsMeta("akshare", bad, from, to); err == nil {
			t.Fatalf("invalid scope accepted: %+v", bad)
		}
	}
}

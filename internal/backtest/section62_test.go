package backtest_test

import (
	"testing"

	"quantitative-trading/internal/backtest"
	"quantitative-trading/internal/types"
)

func TestYearlyMetricsSplit(t *testing.T) {
	snaps := []types.DailySnapshot{
		snap("2023-06-01", 100),
		snap("2023-12-29", 110), // +10% in 2023
		snap("2024-01-02", 110),
		snap("2024-06-28", 121), // +10% in 2024
	}
	ym := backtest.ComputeYearlyMetrics(snaps, backtest.MetricsInput{Snaps: snaps})
	if len(ym) != 2 {
		t.Fatalf("want 2 years, got %d", len(ym))
	}
	if ym[0].Year != 2023 || ym[1].Year != 2024 {
		t.Fatalf("years %+v", ym)
	}
	if !ym[0].Metrics.TotalReturn.Available || !backtest.NearlyEqualDefault(ym[0].Metrics.TotalReturn.Value, 0.10) {
		t.Fatalf("2023 total %v", ym[0].Metrics.TotalReturn)
	}
	if !ym[1].Metrics.TotalReturn.Available || !backtest.NearlyEqualDefault(ym[1].Metrics.TotalReturn.Value, 0.10) {
		t.Fatalf("2024 total %v", ym[1].Metrics.TotalReturn)
	}
}

func TestIndustryDistributionRequiresLookup(t *testing.T) {
	snaps := []types.DailySnapshot{
		snapFull("2024-01-02", 100,
			map[types.SecurityID]float64{"A": 1, "B": 1},
			map[types.SecurityID]float64{"A": 0.4, "B": 0.6}, 0, 0),
	}
	pts, st := backtest.ComputeIndustryDistribution(snaps, nil)
	if st.Available || pts != nil {
		t.Fatal("must be N/A without lookup")
	}
	lookup := map[types.SecurityID]string{"A": "IND1", "B": "IND2"}
	pts, st = backtest.ComputeIndustryDistribution(snaps, lookup)
	if !st.Available || len(pts) != 1 {
		t.Fatalf("status %+v pts %d", st, len(pts))
	}
	if !backtest.NearlyEqualDefault(pts[0].Weights["IND1"], 0.4) {
		t.Fatalf("IND1 weight %v", pts[0].Weights)
	}
	if !backtest.NearlyEqualDefault(pts[0].Weights["IND2"], 0.6) {
		t.Fatalf("IND2 weight %v", pts[0].Weights)
	}
}

func TestFactorExposureAndLayers(t *testing.T) {
	d0, _ := types.ParseTradeDate("2024-01-02")
	d1, _ := types.ParseTradeDate("2024-01-03")
	snaps := []types.DailySnapshot{
		snapFull("2024-01-02", 100,
			map[types.SecurityID]float64{"A": 1, "B": 1, "C": 1, "D": 1, "E": 1},
			map[types.SecurityID]float64{"A": 0.2, "B": 0.2, "C": 0.2, "D": 0.2, "E": 0.2}, 0, 0),
		snapFull("2024-01-03", 100,
			map[types.SecurityID]float64{"A": 1, "B": 1, "C": 1, "D": 1, "E": 1},
			map[types.SecurityID]float64{"A": 0.2, "B": 0.2, "C": 0.2, "D": 0.2, "E": 0.2}, 0, 0),
	}
	// No panels → N/A
	_, st := backtest.ComputeFactorExposure(snaps, nil)
	if st.Available {
		t.Fatal("exposure N/A without panels")
	}
	layers := backtest.ComputeFactorLayers(nil, 5)
	if layers[0].Available {
		t.Fatal("layers N/A without panels")
	}

	mkScores := func(qv map[types.SecurityID]float64) map[types.SecurityID]float64 {
		out := map[types.SecurityID]float64{}
		for k, v := range qv {
			out[k] = v
		}
		return out
	}
	scores := map[types.SecurityID]float64{"A": 1, "B": 2, "C": 3, "D": 4, "E": 5}
	fwd := map[types.SecurityID]float64{"A": -0.02, "B": -0.01, "C": 0.0, "D": 0.01, "E": 0.03}
	panels := []backtest.DailyFactorPanel{
		{TradeDate: d0, Quality: mkScores(scores), Composite: mkScores(scores), ForwardRet: fwd},
		{TradeDate: d1, Quality: mkScores(scores), Composite: mkScores(scores), ForwardRet: fwd},
	}
	pts, st := backtest.ComputeFactorExposure(snaps, panels)
	if !st.Available || len(pts) != 2 {
		t.Fatalf("exposure %+v len=%d", st, len(pts))
	}
	if pts[0].Composite == nil || !backtest.NearlyEqualDefault(*pts[0].Composite, 3.0) {
		t.Fatalf("equal-weight composite exposure want 3, got %v", pts[0].Composite)
	}
	fl := backtest.ComputeFactorLayers(panels, 5)
	var quality *backtest.FactorLayerResult
	for i := range fl {
		if fl[i].Factor == "quality" && fl[i].Available {
			quality = &fl[i]
		}
	}
	if quality == nil {
		t.Fatal("quality layers expected")
	}
	var ls *backtest.LayerReturn
	for i := range quality.Layers {
		if quality.Layers[i].Layer == "long_short" {
			ls = &quality.Layers[i]
		}
	}
	if ls == nil || !ls.MeanReturn.Available {
		t.Fatal("long_short expected")
	}
	// Q5 mean ≈ 0.03, Q1 mean ≈ -0.02 → LS ≈ 0.05
	if !backtest.NearlyEqualDefault(ls.MeanReturn.Value, 0.05) {
		t.Fatalf("long_short %v want 0.05", ls.MeanReturn.Value)
	}
}

func TestBrinsonSkeleton(t *testing.T) {
	empty := backtest.ComputeBrinsonAttribution(nil)
	if empty.Available {
		t.Fatal("empty periods → N/A")
	}
	if empty.Method == "" {
		t.Fatal("method must be documented even when N/A")
	}
	periods := [][]backtest.IndustryPeriodReturn{
		{
			{Industry: "X", PortWeight: 0.6, BenchWeight: 0.5, PortReturn: 0.02, BenchReturn: 0.01},
			{Industry: "Y", PortWeight: 0.4, BenchWeight: 0.5, PortReturn: -0.01, BenchReturn: 0.00},
		},
	}
	got := backtest.ComputeBrinsonAttribution(periods)
	if !got.Available {
		t.Fatalf("expected available: %+v", got)
	}
	// Alloc: (0.6-0.5)*0.01 + (0.4-0.5)*0 = 0.001
	// Sel: 0.5*(0.02-0.01) + 0.5*(-0.01-0) = 0.005 - 0.005 = 0
	// Inter: (0.1)*(0.01) + (-0.1)*(-0.01) = 0.001 + 0.001 = 0.002
	if !backtest.NearlyEqualDefault(got.Allocation.Value, 0.001) {
		t.Fatalf("alloc %v", got.Allocation.Value)
	}
	if !backtest.NearlyEqualDefault(got.Selection.Value, 0.0) {
		t.Fatalf("sel %v", got.Selection.Value)
	}
	if !backtest.NearlyEqualDefault(got.Interaction.Value, 0.002) {
		t.Fatalf("inter %v", got.Interaction.Value)
	}
}

func TestSkipRateAndLlmCounters(t *testing.T) {
	na := backtest.ComputeUnfilledSkipRate(nil)
	if na.Available {
		t.Fatal("skip N/A without counts")
	}
	rate := backtest.ComputeUnfilledSkipRate(&backtest.ExecutionSkipCounts{
		IntendedBuyOrders: 8, IntendedSellOrders: 2,
		SkippedLimitBuy: 1, SkippedLimitSell: 1, SkippedSuspend: 0,
	})
	if !rate.Available || !backtest.NearlyEqualDefault(rate.Value, 0.2) {
		t.Fatalf("skip rate %v", rate)
	}
	llmNA := backtest.ComputeLlmCoverage(nil)
	if llmNA.Available {
		t.Fatal("llm N/A without counters")
	}
	llm := backtest.ComputeLlmCoverage(&backtest.LlmRunCounters{
		AnnotateCalls: 2, FeaturesEmitted: 5, DegradeCount: 3, CandidateTotal: 10,
	})
	if !llm.Available || !backtest.NearlyEqualDefault(llm.CoverageRate.Value, 0.5) {
		t.Fatalf("llm %+v", llm)
	}
}

func TestBuildReportWithExtrasYearlyAndIndustry(t *testing.T) {
	snaps := []types.DailySnapshot{
		snapFull("2023-06-01", 100,
			map[types.SecurityID]float64{"A": 1},
			map[types.SecurityID]float64{"A": 1.0}, 0, 0),
		snapFull("2023-12-29", 110,
			map[types.SecurityID]float64{"A": 1},
			map[types.SecurityID]float64{"A": 1.0}, 0, 0),
		snapFull("2024-01-02", 110,
			map[types.SecurityID]float64{"A": 1, "B": 1},
			map[types.SecurityID]float64{"A": 0.5, "B": 0.5}, 0, 0),
		snapFull("2024-06-28", 120,
			map[types.SecurityID]float64{"A": 1, "B": 1},
			map[types.SecurityID]float64{"A": 0.5, "B": 0.5}, 0, 0),
	}
	cfg := testCfgWithSplit("2023-01-01", "2024-12-31", "2023-12-31", "2024-03-31")
	extras := backtest.ReportExtras{
		IndustryLookup: map[types.SecurityID]string{"A": "BANK", "B": "TECH"},
	}
	r, err := backtest.BuildReportWithExtras(cfg, snaps, nil, nil, extras)
	if err != nil {
		t.Fatal(err)
	}
	full := r.Segments["full"]
	if len(full.Yearly) != 2 {
		t.Fatalf("full yearly %d", len(full.Yearly))
	}
	if !full.Metrics.IndustryDistTS.Available || len(full.IndustryDistribution) == 0 {
		t.Fatalf("industry TS expected: %+v", full.Metrics.IndustryDistTS)
	}
	if full.Attribution.Available {
		t.Fatal("attribution must stay N/A without periods")
	}
	if full.LlmStats.Available {
		t.Fatal("llm must stay N/A without counters")
	}
	// train segment should also have yearly
	train := r.Segments["train"]
	if len(train.Yearly) == 0 {
		t.Fatal("train yearly expected")
	}
}

func TestBuildIndustryLookupFromRecords(t *testing.T) {
	d0, _ := types.ParseTradeDate("2020-01-01")
	d1, _ := types.ParseTradeDate("2023-01-01")
	asof, _ := types.ParseTradeDate("2024-06-01")
	recs := []types.IndustryRecord{
		{TSCode: "A", IndustryCode: "OLD", EffectiveDate: d0, Source: "SW_L1"},
		{TSCode: "A", IndustryCode: "NEW", EffectiveDate: d1, Source: "SW_L1"},
		{TSCode: "B", IndustryCode: "X", EffectiveDate: d0, Source: "CITIC_L1"},
	}
	m := backtest.BuildIndustryLookupFromRecords(recs, "SW_L1", asof)
	if m["A"] != "NEW" {
		t.Fatalf("want NEW got %v", m["A"])
	}
	if _, ok := m["B"]; ok {
		t.Fatal("CITIC filtered out")
	}
}

func TestLayersRefuseWithoutForwardRet(t *testing.T) {
	d0, _ := types.ParseTradeDate("2024-01-02")
	panels := []backtest.DailyFactorPanel{
		{TradeDate: d0, Quality: map[types.SecurityID]float64{"A": 1, "B": 2}},
	}
	fl := backtest.ComputeFactorLayers(panels, 5)
	if fl[0].Available {
		t.Fatal("must refuse without ForwardRet")
	}
}

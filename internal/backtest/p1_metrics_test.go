package backtest_test

import (
	"testing"

	"quantitative-trading/internal/backtest"
	"quantitative-trading/internal/types"
)

func TestFilterSnapsWithBaselineIncludesInitialReturn(t *testing.T) {
	// Spec example: 6/30=100, 7/1=90, 7/2=99 → ~-1%, not +10%.
	snaps := []types.DailySnapshot{
		snap("2026-06-30", 100),
		snap("2026-07-01", 90),
		snap("2026-07-02", 99),
	}
	ex, _ := types.ParseTradeDate("2026-06-30")
	end, _ := types.ParseTradeDate("2026-07-02")
	b := backtest.SegmentBounds{Name: backtest.SegmentOOS, Exclusive: ex, End: end, IsFormal: true}
	seg := backtest.FilterSnaps(snaps, b)
	if len(seg) != 2 || seg[0].NAV != 90 {
		t.Fatalf("exclusive filter %+v", seg)
	}
	withBase := backtest.FilterSnapsWithBaseline(snaps, b)
	if len(withBase) != 3 || withBase[0].NAV != 100 {
		t.Fatalf("baseline %+v", withBase)
	}
	mBad := backtest.ComputeMetricsFull(backtest.MetricsInput{Snaps: seg})
	mGood := backtest.ComputeMetricsFull(backtest.MetricsInput{Snaps: withBase})
	if !mBad.TotalReturn.Available || !backtest.NearlyEqualAbs(mBad.TotalReturn.Value, 99.0/90.0-1, 1e-12) {
		t.Fatalf("exclusive-only return %v (the bug)", mBad.TotalReturn)
	}
	want := 99.0/100.0 - 1 // -1%
	if !mGood.TotalReturn.Available || !backtest.NearlyEqualAbs(mGood.TotalReturn.Value, want, 1e-12) {
		t.Fatalf("with baseline %v want %v", mGood.TotalReturn, want)
	}
}

func TestBuildReportOOSUsesBaselineReturn(t *testing.T) {
	snaps := []types.DailySnapshot{
		snap("2026-06-30", 100),
		snap("2026-07-01", 90),
		snap("2026-07-02", 99),
	}
	cfg := testCfgWithSplit("2026-06-30", "2026-07-02", "2026-06-30", "2026-07-01")
	r, err := backtest.BuildReport(cfg, snaps, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	oos := r.Segments["oos"]
	if oos.NSnapshots != 1 {
		t.Fatalf("oos n_snapshots (exclusive days) %d", oos.NSnapshots)
	}
	want := 99.0/90.0 - 1 // OOS exclusive val_end=7/1 so baseline is 7/1=90 → 99/90-1
	// val_end is 7/1, OOS=(7/1, 7/2] → snaps 7/2 plus baseline 7/1
	if !oos.Metrics.TotalReturn.Available || !backtest.NearlyEqualAbs(oos.Metrics.TotalReturn.Value, want, 1e-12) {
		t.Fatalf("oos total %v want %v", oos.Metrics.TotalReturn, want)
	}
	val := r.Segments["validate"]
	// validate = (6/30, 7/1] → 7/1 plus baseline 6/30 → 90/100-1 = -10%
	if !val.Metrics.TotalReturn.Available || !backtest.NearlyEqualAbs(val.Metrics.TotalReturn.Value, -0.10, 1e-12) {
		t.Fatalf("val total %v want -0.10", val.Metrics.TotalReturn)
	}
}

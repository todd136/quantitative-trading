package backtest_test

import (
	"testing"

	"quantitative-trading/internal/backtest"
	"quantitative-trading/internal/config"
	"quantitative-trading/internal/types"
)

func testCfgWithSplit(start, end, trainEnd, valEnd string) config.Config {
	cfg := config.Default()
	cfg.Backtest.StartDate = start
	cfg.Backtest.EndDate = end
	cfg.Backtest.SampleSplit = config.SampleSplitConfig{
		TrainEnd: trainEnd,
		ValEnd:   valEnd,
	}
	return cfg
}

func TestParseSampleSplitRejectsBadOrder(t *testing.T) {
	cfg := config.Default()
	cfg.Backtest.SampleSplit = config.SampleSplitConfig{
		TrainEnd: "2022-12-31",
		ValEnd:   "2020-12-31",
	}
	if _, err := backtest.ParseSampleSplit(cfg.Backtest); err == nil {
		t.Fatal("expected error for train_end >= val_end")
	}

	cfg.Backtest.SampleSplit = config.SampleSplitConfig{TrainEnd: "2020-12-31"}
	if _, err := backtest.ParseSampleSplit(cfg.Backtest); err == nil {
		t.Fatal("expected error when val_end missing")
	}

	cfg = testCfgWithSplit("2018-01-01", "2024-12-31", "2020-12-31", "2024-12-31")
	if _, err := backtest.ParseSampleSplit(cfg.Backtest); err == nil {
		t.Fatal("expected error when val_end >= end_date")
	}
}

func TestParseSampleSplitOK(t *testing.T) {
	cfg := testCfgWithSplit("2018-01-01", "2024-12-31", "2020-12-31", "2022-12-31")
	rs, err := backtest.ParseSampleSplit(cfg.Backtest)
	if err != nil {
		t.Fatal(err)
	}
	if !rs.Enabled {
		t.Fatal("expected enabled")
	}
	formal := 0
	for _, s := range rs.Segments {
		if s.IsFormal {
			formal++
			if s.Name != backtest.SegmentOOS {
				t.Fatalf("formal should be oos, got %s", s.Name)
			}
		}
	}
	if formal != 1 {
		t.Fatalf("want 1 formal segment, got %d", formal)
	}
}

func TestSegmentMetricsIndependent(t *testing.T) {
	// Distinct NAV paths per segment → different total returns
	snaps := []types.DailySnapshot{
		snap("2020-06-01", 100),
		snap("2020-12-31", 110), // train ends +10%
		snap("2021-06-01", 110),
		snap("2022-12-31", 99), // val ends down
		snap("2023-06-01", 99),
		snap("2024-06-01", 150), // oos up big
	}
	cfg := testCfgWithSplit("2020-01-01", "2024-12-31", "2020-12-31", "2022-12-31")
	r, err := backtest.BuildReport(cfg, snaps, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	train := r.Segments["train"].Metrics
	oos := r.Segments["oos"].Metrics
	if !train.HasCurve || !oos.HasCurve {
		t.Fatalf("both need curves: train=%v oos=%v", train.HasCurve, oos.HasCurve)
	}
	if !train.TotalReturn.Available || !oos.TotalReturn.Available {
		t.Fatal("returns should be available")
	}
	if backtest.NearlyEqualDefault(train.TotalReturn.Value, oos.TotalReturn.Value) {
		t.Fatalf("train curve must differ from oos: %v vs %v", train.TotalReturn.Value, oos.TotalReturn.Value)
	}
	if r.FormalSegment != backtest.SegmentOOS {
		t.Fatal("formal must be oos")
	}
}

func TestFilterSnapsBounds(t *testing.T) {
	snaps := []types.DailySnapshot{
		snap("2020-01-01", 1),
		snap("2020-12-31", 2),
		snap("2021-01-04", 3),
		snap("2022-12-31", 4),
		snap("2023-01-03", 5),
	}
	trainEnd, _ := types.ParseTradeDate("2020-12-31")
	valEnd, _ := types.ParseTradeDate("2022-12-31")
	train := backtest.FilterSnaps(snaps, backtest.SegmentBounds{
		Name: backtest.SegmentTrain, End: trainEnd,
	})
	if len(train) != 2 {
		t.Fatalf("train len %d", len(train))
	}
	val := backtest.FilterSnaps(snaps, backtest.SegmentBounds{
		Name: backtest.SegmentValidate, Exclusive: trainEnd, End: valEnd,
	})
	if len(val) != 2 {
		t.Fatalf("val len %d want 2 got dates", len(val))
	}
	oos := backtest.FilterSnaps(snaps, backtest.SegmentBounds{
		Name: backtest.SegmentOOS, Exclusive: valEnd,
	})
	if len(oos) != 1 || oos[0].NAV != 5 {
		t.Fatalf("oos %+v", oos)
	}
}

func TestConfigValidateSampleSplit(t *testing.T) {
	c := config.Default()
	c.Backtest.SampleSplit = config.SampleSplitConfig{TrainEnd: "2021-01-01", ValEnd: "2020-01-01"}
	if err := c.Validate(); err == nil {
		t.Fatal("expected validate error")
	}
}

func TestWalkForwardSkeletonNote(t *testing.T) {
	note := backtest.WalkForwardSkeleton(config.WalkForwardConfig{TrainYears: 3, TestYears: 1})
	if note == "" {
		t.Fatal("expected skeleton note")
	}
}

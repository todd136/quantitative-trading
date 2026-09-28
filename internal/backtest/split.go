package backtest

import (
	"fmt"

	"quantitative-trading/internal/config"
	"quantitative-trading/internal/types"
)

// SegmentName identifies a sample-split segment.
type SegmentName string

const (
	SegmentFull     SegmentName = "full"
	SegmentTrain    SegmentName = "train"
	SegmentValidate SegmentName = "validate"
	SegmentOOS      SegmentName = "oos"
)

// SegmentBounds are inclusive/exclusive date bounds for a segment.
// Train:  [Start, End]  (End = train_end inclusive)
// Val:    (train_end, val_end]  → Start exclusive of train_end conceptually;
//         we filter snaps with date > train_end && date <= val_end
// OOS:    (val_end, end] → date > val_end && date <= end
type SegmentBounds struct {
	Name      SegmentName
	Start     types.TradeDate // inclusive lower bound (may be zero = open)
	End       types.TradeDate // inclusive upper bound
	Exclusive types.TradeDate // if set, snaps must be After(Exclusive)
	IsFormal  bool            // true for OOS when split configured
}

// ResolvedSplit is the parsed train/val/OOS layout.
type ResolvedSplit struct {
	Enabled  bool
	TrainEnd types.TradeDate
	ValEnd   types.TradeDate
	Start    types.TradeDate // backtest start (may be zero)
	End      types.TradeDate // backtest end (may be zero)
	Segments []SegmentBounds
}

// ParseSampleSplit validates config.Backtest.SampleSplit time order.
// Requires train_end < val_end; if start/end set, start <= train_end and val_end < end
// (OOS = (val_end, end], so val_end must be strictly before end when end is set).
func ParseSampleSplit(bc config.BacktestConfig) (ResolvedSplit, error) {
	ss := bc.SampleSplit
	out := ResolvedSplit{}
	if ss.TrainEnd == "" && ss.ValEnd == "" {
		out.Segments = []SegmentBounds{{Name: SegmentFull, IsFormal: true}}
		return out, nil
	}
	if ss.TrainEnd == "" || ss.ValEnd == "" {
		return out, fmt.Errorf("sample_split: both train_end and val_end required")
	}
	trainEnd, err := types.ParseTradeDate(ss.TrainEnd)
	if err != nil {
		return out, fmt.Errorf("sample_split.train_end: %w", err)
	}
	valEnd, err := types.ParseTradeDate(ss.ValEnd)
	if err != nil {
		return out, fmt.Errorf("sample_split.val_end: %w", err)
	}
	if !trainEnd.Before(valEnd) {
		return out, fmt.Errorf("sample_split: train_end (%s) must be < val_end (%s)", ss.TrainEnd, ss.ValEnd)
	}

	var start, end types.TradeDate
	if bc.StartDate != "" {
		start, err = types.ParseTradeDate(bc.StartDate)
		if err != nil {
			return out, fmt.Errorf("start_date: %w", err)
		}
		if trainEnd.Before(start) {
			return out, fmt.Errorf("sample_split: train_end (%s) must be >= start_date (%s)", ss.TrainEnd, bc.StartDate)
		}
	}
	if bc.EndDate != "" {
		end, err = types.ParseTradeDate(bc.EndDate)
		if err != nil {
			return out, fmt.Errorf("end_date: %w", err)
		}
		if !valEnd.Before(end) {
			return out, fmt.Errorf("sample_split: val_end (%s) must be < end_date (%s) so OOS is non-empty", ss.ValEnd, bc.EndDate)
		}
	}

	out.Enabled = true
	out.TrainEnd = trainEnd
	out.ValEnd = valEnd
	out.Start = start
	out.End = end
	out.Segments = []SegmentBounds{
		{Name: SegmentTrain, Start: start, End: trainEnd, IsFormal: false},
		{Name: SegmentValidate, Exclusive: trainEnd, End: valEnd, IsFormal: false},
		{Name: SegmentOOS, Exclusive: valEnd, End: end, IsFormal: true},
		{Name: SegmentFull, Start: start, End: end, IsFormal: false},
	}
	return out, nil
}

// FilterSnaps returns snapshots belonging to bounds (date-ordered input assumed).
func FilterSnaps(snaps []types.DailySnapshot, b SegmentBounds) []types.DailySnapshot {
	out := make([]types.DailySnapshot, 0, len(snaps))
	for _, s := range snaps {
		d := s.TradeDate
		if !b.Start.Time().IsZero() && d.Before(b.Start) {
			continue
		}
		if !b.Exclusive.Time().IsZero() && !d.After(b.Exclusive) {
			continue
		}
		if !b.End.Time().IsZero() && d.After(b.End) {
			continue
		}
		out = append(out, s)
	}
	return out
}

// FilterFills returns fills whose TradeDate falls in the same bounds as FilterSnaps.
func FilterFills(fills []types.Fill, b SegmentBounds) []types.Fill {
	out := make([]types.Fill, 0, len(fills))
	for _, f := range fills {
		d := f.TradeDate
		if !b.Start.Time().IsZero() && d.Before(b.Start) {
			continue
		}
		if !b.Exclusive.Time().IsZero() && !d.After(b.Exclusive) {
			continue
		}
		if !b.End.Time().IsZero() && d.After(b.End) {
			continue
		}
		out = append(out, f)
	}
	return out
}

// WalkForwardSkeleton documents an optional walk-forward config without running it.
// Returns a human-readable note; full multi-window execution is deferred.
func WalkForwardSkeleton(wf config.WalkForwardConfig) string {
	if wf.TrainYears <= 0 && wf.TestYears <= 0 {
		return ""
	}
	return fmt.Sprintf(
		"walk_forward skeleton: train_years=%d test_years=%d (multi-window execution not yet implemented; use sample_split for fixed Train/Val/OOS)",
		wf.TrainYears, wf.TestYears,
	)
}

// FreezeParamsNotice is the OOS parameter-freeze policy text for reports/README.
const FreezeParamsNotice = `Parameter freeze (rule book §6.3): OOS / test_oos must NOT be used for tuning.
Any parameter change after freeze requires bumping config version. Formal report segment is OOS.`

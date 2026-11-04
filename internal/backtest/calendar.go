package backtest

import (
	"fmt"

	"quantitative-trading/internal/config"
	"quantitative-trading/internal/portfolio"
	"quantitative-trading/internal/types"
)

// ResolveWindow maps config start/end onto calendar indices.
// endIdx is exclusive. The window includes the last trading day in range
// (NAV is marked that day even when no T+1 exec exists).
func ResolveWindow(cal []types.TradeDate, cfg config.Config) (startIdx, endIdx int, err error) {
	if len(cal) < 1 {
		return 0, 0, fmt.Errorf("calendar too short")
	}
	startIdx, endIdx = 0, len(cal)

	if cfg.Backtest.StartDate != "" {
		sd, e := types.ParseTradeDate(cfg.Backtest.StartDate)
		if e != nil {
			return 0, 0, fmt.Errorf("start_date: %w", e)
		}
		found := false
		for i, d := range cal {
			if !d.Before(sd) {
				startIdx = i
				found = true
				break
			}
		}
		if !found {
			return 0, 0, fmt.Errorf("start_date %s after calendar end", cfg.Backtest.StartDate)
		}
	} else {
		// Legacy short fixture window: need momentum lookback headroom.
		startIdx = len(cal) - 8
		if startIdx < 260 {
			startIdx = 260
		}
		if startIdx >= len(cal) {
			startIdx = 0
		}
	}

	if cfg.Backtest.EndDate != "" {
		ed, e := types.ParseTradeDate(cfg.Backtest.EndDate)
		if e != nil {
			return 0, 0, fmt.Errorf("end_date: %w", e)
		}
		found := false
		for i := len(cal) - 1; i >= 0; i-- {
			if !cal[i].After(ed) {
				endIdx = i + 1 // exclusive
				found = true
				break
			}
		}
		if !found {
			return 0, 0, fmt.Errorf("end_date %s before calendar start", cfg.Backtest.EndDate)
		}
	}

	if endIdx > len(cal) {
		endIdx = len(cal)
	}
	if endIdx <= startIdx {
		return 0, 0, fmt.Errorf("date window too short: startIdx=%d endIdx=%d", startIdx, endIdx)
	}
	return startIdx, endIdx, nil
}

// SignalFunc generates freeze-on-T target holdings. It must not consult T+1 exec bars.
// closeSnap is the signal-day EOD mark (after any morning fills, before next-day exec).
type SignalFunc func(signalDate, execDate types.TradeDate, closeSnap types.DailySnapshot) ([]types.TargetHolding, error)

type pendingExec struct {
	execDate types.TradeDate
	targets  []types.TargetHolding
}

// RunCalendar walks each trading day in [startIdx, endIdx):
//  1. Open: execute yesterday's frozen targets (if this is their exec date)
//  2. Close: mark NAV (exactly one row per trading day)
//  3. Signal: on rebalance days, freeze new targets for the next session in-window
func RunCalendar(
	e *Engine,
	state *PortfolioState,
	startIdx, endIdx int,
	onSignal SignalFunc,
) ([]types.DailySnapshot, error) {
	if e == nil {
		return nil, fmt.Errorf("nil engine")
	}
	if state == nil {
		return nil, fmt.Errorf("nil portfolio state")
	}
	if startIdx < 0 || endIdx > len(e.Calendar) || endIdx <= startIdx {
		return nil, fmt.Errorf("bad window startIdx=%d endIdx=%d cal=%d", startIdx, endIdx, len(e.Calendar))
	}
	cfg := e.Cfg
	if err := e.ValidateCorpActions(); err != nil {
		return nil, err
	}
	var pending *pendingExec
	snaps := make([]types.DailySnapshot, 0, endIdx-startIdx)

	for i := startIdx; i < endIdx; i++ {
		t := e.Calendar[i]

		e.applyOpenCredits(state, t)

		if pending != nil && pending.execDate.Equal(t) {
			nextAvail, _ := e.NextTradeDate(t)
			if err := e.ApplyTargets(state, t, pending.targets, nextAvail); err != nil {
				return nil, fmt.Errorf("exec %s: %w", t, err)
			}
			pending = nil
		}

		snap, err := e.MarkNAV(state, t)
		if err != nil {
			return nil, fmt.Errorf("mark nav %s: %w", t, err)
		}
		snaps = append(snaps, snap)
		e.captureRecordEntitlements(state, t)

		if !portfolio.IsRebalanceDay(t, cfg.Portfolio.RebalanceFreq, cfg.Portfolio.RebalanceWeekday, e.Calendar, e.CalIndex) {
			continue
		}
		execDate, ok := e.NextTradeDate(t)
		if !ok {
			continue // last calendar day: still marked NAV, no T+1 exec
		}
		execIdx, ok := e.CalIndex[execDate.String()]
		if !ok || execIdx >= endIdx {
			continue // exec would fall outside the backtest window
		}
		if onSignal == nil {
			continue
		}
		targets, err := onSignal(t, execDate, snap)
		if err != nil {
			return nil, fmt.Errorf("signal %s: %w", t, err)
		}
		pending = &pendingExec{execDate: execDate, targets: targets}
	}

	if err := AssertUniqueIncreasingDates(snaps); err != nil {
		return snaps, err
	}
	return snaps, nil
}

// AssertUniqueIncreasingDates requires one NAV mark per date, strictly increasing.
func AssertUniqueIncreasingDates(snaps []types.DailySnapshot) error {
	seen := map[string]int{}
	for i, s := range snaps {
		key := s.TradeDate.String()
		if key == "" || s.TradeDate.Time().IsZero() {
			return fmt.Errorf("nav[%d]: empty trade_date", i)
		}
		if prev, ok := seen[key]; ok {
			return fmt.Errorf("nav date %s duplicated at indices %d and %d", key, prev, i)
		}
		seen[key] = i
		if i > 0 && !snaps[i-1].TradeDate.Before(s.TradeDate) {
			return fmt.Errorf("nav dates not strictly increasing: %s then %s",
				snaps[i-1].TradeDate, s.TradeDate)
		}
	}
	return nil
}

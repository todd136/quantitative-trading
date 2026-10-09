package factors

import (
	"time"

	"quantitative-trading/internal/types"
)

// MarketValueAsOf returns the latest TotalMV with trade_date ≤ asof within
// lookbackCalendarDays (inclusive window [asof-lookback, asof]).
// Missing / non-positive MV → ok=false (never treat 0 as valid).
func MarketValueAsOf(rows []types.MarketValue, asof types.TradeDate, lookbackCalendarDays int) (float64, bool) {
	if lookbackCalendarDays < 0 {
		lookbackCalendarDays = 0
	}
	var best types.MarketValue
	found := false
	earliest := types.TradeDate(asof.Time().AddDate(0, 0, -lookbackCalendarDays))
	for _, r := range rows {
		if r.TotalMV <= 0 {
			continue
		}
		if r.TradeDate.After(asof) {
			continue
		}
		if r.TradeDate.Before(earliest) {
			continue
		}
		if !found || r.TradeDate.After(best.TradeDate) {
			best = r
			found = true
		}
	}
	if !found {
		return 0, false
	}
	return best.TotalMV, true
}

// ForwardCloseReturn computes close-to-close return from fromDate to toDate
// using the provided bar map. Returns ok=false when either bar is missing,
// suspended, or close≤0 — never invents a return.
func ForwardCloseReturn(bars map[string]types.Bar, fromDate, toDate types.TradeDate) (float64, bool) {
	if bars == nil {
		return 0, false
	}
	a, okA := bars[fromDate.String()]
	b, okB := bars[toDate.String()]
	if !okA || !okB {
		return 0, false
	}
	if a.Suspended || b.Suspended || a.Close <= 0 || b.Close <= 0 {
		return 0, false
	}
	return b.Close/a.Close - 1.0, true
}

// DaysBetween is a tiny helper for tests / diagnostics (calendar days).
func DaysBetween(a, b types.TradeDate) int {
	return int(b.Time().Sub(a.Time()) / (24 * time.Hour))
}

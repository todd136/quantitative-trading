package factors

import (
	"time"

	"quantitative-trading/internal/types"
)

// MarketValueAsOf returns the latest TotalMV with trade_date ≤ asof within
// lookbackCalendarDays (inclusive window [asof-lookback, asof]).
// Missing / non-positive / snapshot / undated MV → ok=false (never treat 0 as valid).
func MarketValueAsOf(rows []types.MarketValue, asof types.TradeDate, lookbackCalendarDays int) (float64, bool) {
	mv, err := MarketValueAsOfPIT(rows, asof, lookbackCalendarDays, false)
	if err != nil {
		return 0, false
	}
	return mv, true
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

package universe

import (
	"quantitative-trading/internal/types"
)

// LimitUpDownPct returns the conventional A-share daily limit ratio for board / ST.
// Main boards 10%; ChiNext / STAR 20%; ST/*ST 5%. Used only when vendor high/low
// limits are missing — never invents fills or returns.
func LimitUpDownPct(board types.Board, isST bool) float64 {
	if isST {
		return 0.05
	}
	switch board {
	case types.BoardChiNext, types.BoardSTAR:
		return 0.20
	default:
		return 0.10
	}
}

// EnsureLimitPrices fills HighLimit / LowLimit from PreClose×(1±pct) when absent.
// Uses PreClose only — never same-day Close (that would leak T close into T open limits).
// No-op when both limits already set or PreClose is unusable.
func EnsureLimitPrices(b *types.Bar, board types.Board, isST bool) {
	if b == nil {
		return
	}
	if b.HighLimit > 0 && b.LowLimit > 0 {
		return
	}
	ref := b.PreClose
	if ref <= 0 {
		return
	}
	pct := LimitUpDownPct(board, isST)
	if b.HighLimit <= 0 {
		b.HighLimit = ref * (1 + pct)
	}
	if b.LowLimit <= 0 {
		b.LowLimit = ref * (1 - pct)
	}
}

// EnrichBarsLimits walks barsByCode and fills missing limit prices using
// SecurityMaster.Board and IsSTOn(st, code, bar.TradeDate).
func EnrichBarsLimits(
	barsByCode map[types.SecurityID]map[string]types.Bar,
	secs map[types.SecurityID]types.SecurityMaster,
	st []types.STRecord,
) {
	for code, byDate := range barsByCode {
		sm, ok := secs[code]
		board := types.Board("")
		if ok {
			board = sm.Board
		}
		for d, b := range byDate {
			isST := IsSTOn(st, code, b.TradeDate)
			EnsureLimitPrices(&b, board, isST)
			byDate[d] = b
		}
	}
}

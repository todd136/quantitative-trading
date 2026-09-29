// Package universe implements §2.2 hard filters and §2.3 limit helpers.
package universe

import (
	"math"

	"quantitative-trading/internal/config"
	"quantitative-trading/internal/types"
)

// Snapshot holds eligibility context for one stock on signal day T.
type Snapshot struct {
	Security   types.SecurityMaster
	BarT       types.Bar
	IsST       bool
	Industry   string
	ListDays   int
	ADV20      float64
	ADVValid   int
	Unbuyable  bool // CannotBuyAtOpen on T+1 — does not remove from Universe; marks new opens
}

// Context aggregates calendars and lookup helpers for BuildUniverse.
type Context struct {
	Cfg       config.Config
	Calendar  []types.TradeDate
	CalIndex  map[string]int // TradeDate.String -> idx
	Securities map[types.SecurityID]types.SecurityMaster
	Bars      map[types.SecurityID]map[string]types.Bar // ts_code -> date -> bar
	ST        []types.STRecord
	Industry  []types.IndustryRecord
}

// BuildIndex indexes calendar dates for O(1) lookup.
func BuildIndex(cal []types.TradeDate) map[string]int {
	m := make(map[string]int, len(cal))
	for i, d := range cal {
		m[d.String()] = i
	}
	return m
}

// IsSTOn returns true if stock is ST / *ST on T (entry <= T < remove).
func IsSTOn(recs []types.STRecord, code types.SecurityID, t types.TradeDate) bool {
	for _, r := range recs {
		if r.TSCode != code {
			continue
		}
		if r.EntryDate.After(t) {
			continue
		}
		if r.RemoveDate != nil && !t.Before(*r.RemoveDate) {
			// T >= remove_date → not ST
			continue
		}
		// remove nil or T < remove
		if r.RemoveDate == nil || t.Before(*r.RemoveDate) {
			return true
		}
	}
	return false
}

// IndustryOn returns PIT industry code for T, or "" if missing.
func IndustryOn(recs []types.IndustryRecord, code types.SecurityID, t types.TradeDate, source string) string {
	var best string
	var bestEff types.TradeDate
	found := false
	for _, r := range recs {
		if r.TSCode != code {
			continue
		}
		if source != "" && r.Source != source {
			continue
		}
		if r.EffectiveDate.After(t) {
			continue
		}
		if r.ExpireDate != nil && !t.Before(*r.ExpireDate) {
			continue
		}
		if !found || r.EffectiveDate.After(bestEff) {
			best = r.IndustryCode
			bestEff = r.EffectiveDate
			found = true
		}
	}
	return best
}

// ListTradingDays counts trading days from list_date through T inclusive.
func ListTradingDays(calIndex map[string]int, listDate, t types.TradeDate) int {
	li, okL := calIndex[listDate.String()]
	ti, okT := calIndex[t.String()]
	if !okT {
		return 0
	}
	if !okL {
		// list date before calendar start — count from 0
		return ti + 1
	}
	if ti < li {
		return 0
	}
	return ti - li + 1
}

// HasListDate reports whether SecurityMaster.ListDate is a real (non-zero) IPO/list date.
// Zero ListDate means upstream did not supply one — callers should degrade min_list filter.
func HasListDate(sm types.SecurityMaster) bool {
	return !sm.ListDate.Time().IsZero()
}

// MeanADV20 computes mean amount over up to 20 sessions ending at T.
// Returns (mean, validDays). If validDays < minValid, caller should reject.
func MeanADV20(bars []types.Bar, t types.TradeDate, window int) (float64, int) {
	if window <= 0 {
		window = 20
	}
	// bars assumed sorted ascending
	idx := -1
	for i := range bars {
		if bars[i].TradeDate.Equal(t) {
			idx = i
			break
		}
	}
	if idx < 0 {
		return 0, 0
	}
	start := idx - window + 1
	if start < 0 {
		start = 0
	}
	sum := 0.0
	n := 0
	for i := start; i <= idx; i++ {
		if bars[i].Suspended {
			continue
		}
		sum += bars[i].Amount
		n++
	}
	if n == 0 {
		return 0, 0
	}
	return sum / float64(n), n
}

// OpenIsLimitUpOneWord: abs(open - high_limit)/high_limit < 1e-4.
func OpenIsLimitUpOneWord(b types.Bar) bool {
	if b.HighLimit <= 0 {
		return false
	}
	if b.LimitStatus == "一字涨停" {
		return true
	}
	return math.Abs(b.Open-b.HighLimit)/b.HighLimit < 1e-4
}

// OpenIsLimitDownOneWord: symmetric for low_limit.
func OpenIsLimitDownOneWord(b types.Bar) bool {
	if b.LowLimit <= 0 {
		return false
	}
	if b.LimitStatus == "一字跌停" {
		return true
	}
	return math.Abs(b.Open-b.LowLimit)/b.LowLimit < 1e-4
}

// CannotBuyAtOpen implements §2.3.
func CannotBuyAtOpen(b types.Bar) bool {
	if b.Suspended {
		return true
	}
	return OpenIsLimitUpOneWord(b)
}

// CannotSellAtOpen implements §2.3.
func CannotSellAtOpen(b types.Bar) bool {
	if b.Suspended {
		return true
	}
	return OpenIsLimitDownOneWord(b)
}

// IsEligible evaluates §2.2 hard filters (AND). Unbuyable is returned separately.
// When listDateMissing is true (upstream empty list_date), min_list_trading_days is skipped
// (explicit smoke/degrade path). Production should populate real list dates so this is rare.
func IsEligible(cfg config.Config, sm types.SecurityMaster, barT types.Bar, isST bool, listDays int, adv float64, advValid int, listDateMissing bool) (eligible bool, unbuyable bool) {
	boards := cfg.BoardSet()
	if cfg.Market.ExcludeST && isST {
		return false, false
	}
	if barT.Suspended {
		return false, false
	}
	if !listDateMissing && listDays < cfg.Universe.MinListTradingDays {
		return false, false
	}
	if sm.DelistDate != nil && !barT.TradeDate.Before(*sm.DelistDate) {
		return false, false
	}
	if advValid < cfg.Universe.MinADVValidDays || adv < cfg.Universe.MinADV20 {
		return false, false
	}
	if !boards[string(sm.Board)] {
		return false, false
	}
	if cfg.Market.RequireStarEligibility && sm.Board == types.BoardSTAR {
		return false, false
	}
	if barT.Close <= 0 {
		return false, false
	}
	if barT.AdjFactor == 0 {
		return false, false
	}
	return true, false
}

// BuildUniverse returns eligible securities on signal day T.
// execBar (T+1) is optional: if provided, sets Unbuyable for CannotBuyAtOpen.
func BuildUniverse(ctx *Context, t types.TradeDate, execBars map[types.SecurityID]types.Bar) []Snapshot {
	out := make([]Snapshot, 0)
	for code, sm := range ctx.Securities {
		barMap := ctx.Bars[code]
		if barMap == nil {
			continue
		}
		barT, ok := barMap[t.String()]
		if !ok {
			continue
		}
		isST := IsSTOn(ctx.ST, code, t)
		listMissing := !HasListDate(sm)
		listDays := 0
		if !listMissing {
			listDays = ListTradingDays(ctx.CalIndex, sm.ListDate, t)
		}

		// flatten bars for ADV
		cal := ctx.Calendar
		ti, okT := ctx.CalIndex[t.String()]
		if !okT {
			continue
		}
		flat := make([]types.Bar, 0, 20)
		start := ti - 19
		if start < 0 {
			start = 0
		}
		for i := start; i <= ti; i++ {
			if b, ok := barMap[cal[i].String()]; ok {
				flat = append(flat, b)
			}
		}
		adv, advValid := MeanADV20(flat, t, 20)

		okElig, _ := IsEligible(ctx.Cfg, sm, barT, isST, listDays, adv, advValid, listMissing)
		if !okElig {
			continue
		}
		ind := IndustryOn(ctx.Industry, code, t, ctx.Cfg.Processing.IndustrySource)
		snap := Snapshot{
			Security: sm,
			BarT:     barT,
			IsST:     isST,
			Industry: ind,
			ListDays: listDays,
			ADV20:    adv,
			ADVValid: advValid,
		}
		if execBars != nil {
			if eb, ok := execBars[code]; ok {
				snap.Unbuyable = CannotBuyAtOpen(eb)
			}
		}
		out = append(out, snap)
	}
	return out
}

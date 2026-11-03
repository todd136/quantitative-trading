package factors

import (
	"fmt"

	"quantitative-trading/internal/types"
)

// Sentinel dates used by AKShare helpers when historical intervals are missing.
var (
	STSnapshotEpoch       = types.NewTradeDate(1990, 1, 1)
	IndustryBackfillEpoch = types.NewTradeDate(2000, 1, 1)
)

// AnnouncementUsable reports whether a financial row may be used at signal date t.
// Always requires a real announcement_date ≤ t (never year-1 zero dates).
// Strict mode additionally rejects announce≈period proxies (no NOTICE_DATE).
func AnnouncementUsable(row types.FinancialRow, t types.TradeDate, strict bool) error {
	if row.AnnouncementDate.Time().IsZero() {
		return fmt.Errorf("financials %s period %s: missing announcement_date", row.TSCode, row.ReportPeriod)
	}
	if row.AnnouncementDate.After(t) {
		return fmt.Errorf("financials %s period %s: announcement %s after signal %s",
			row.TSCode, row.ReportPeriod, row.AnnouncementDate, t)
	}
	proxy := row.AnnouncementIsProxy || row.AnnouncementDate.Equal(row.ReportPeriod)
	if strict && proxy {
		return fmt.Errorf("strict PIT: financials %s period %s announcement_date≈report_period (no NOTICE_DATE)",
			row.TSCode, row.ReportPeriod)
	}
	return nil
}

// GetLatestFinancialPIT is GetLatestFinancial with an explicit strict-PIT switch.
func GetLatestFinancialPIT(rows []types.FinancialRow, t types.TradeDate, strict bool) (types.FinancialRow, bool) {
	var best types.FinancialRow
	found := false
	for _, r := range rows {
		if err := AnnouncementUsable(r, t, strict); err != nil {
			continue
		}
		if !found {
			best = r
			found = true
			continue
		}
		if r.ReportPeriod.After(best.ReportPeriod) ||
			(r.ReportPeriod.Equal(best.ReportPeriod) && r.AnnouncementDate.After(best.AnnouncementDate)) {
			best = r
		}
	}
	return best, found
}

// MarketValueUsable rejects snapshot / undated MV rows. Strict mode errors instead of skipping.
func MarketValueUsable(row types.MarketValue, asof types.TradeDate, strict bool) error {
	if row.IsSnapshot {
		return fmt.Errorf("strict PIT: market value %s is a current snapshot (not a historical series)", row.TSCode)
	}
	if row.TradeDate.Time().IsZero() {
		return fmt.Errorf("market value %s: missing trade_date", row.TSCode)
	}
	if row.TradeDate.After(asof) {
		return fmt.Errorf("market value %s: trade_date %s after asof %s", row.TSCode, row.TradeDate, asof)
	}
	if row.TotalMV <= 0 {
		return fmt.Errorf("market value %s %s: non-positive total_mv", row.TSCode, row.TradeDate)
	}
	if strict {
		return nil
	}
	return nil
}

// MarketValueAsOfPIT is MarketValueAsOf plus snapshot rejection.
// In strict mode, presence of snapshot rows (even if unused) is a fail-closed error
// when they are the only candidates; dated history still wins when present.
func MarketValueAsOfPIT(rows []types.MarketValue, asof types.TradeDate, lookbackCalendarDays int, strict bool) (float64, error) {
	if lookbackCalendarDays < 0 {
		lookbackCalendarDays = 0
	}
	var best types.MarketValue
	found := false
	sawSnapshot := false
	earliest := types.TradeDate(asof.Time().AddDate(0, 0, -lookbackCalendarDays))
	for _, r := range rows {
		if r.IsSnapshot || r.TradeDate.Time().IsZero() {
			sawSnapshot = true
			if strict {
				continue
			}
			continue
		}
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
		if strict && sawSnapshot {
			return 0, fmt.Errorf("strict PIT: market value asof %s has only snapshot/undated rows", asof)
		}
		return 0, fmt.Errorf("no market value on or before %s within %d calendar days", asof, lookbackCalendarDays)
	}
	return best.TotalMV, nil
}

// ValidateSTForStrict rejects snapshot / backfilled ST intervals under strict PIT.
func ValidateSTForStrict(recs []types.STRecord) error {
	for _, r := range recs {
		if r.PITBackfill || r.EntryDate.Equal(STSnapshotEpoch) {
			return fmt.Errorf("strict PIT: ST %s has no historical interval (entry_date=%s backfill=%v)",
				r.TSCode, r.EntryDate, r.PITBackfill)
		}
		if r.EntryDate.Time().IsZero() {
			return fmt.Errorf("strict PIT: ST %s missing entry_date", r.TSCode)
		}
	}
	return nil
}

// ValidateIndustryForStrict rejects membership rows without historical effective dates.
func ValidateIndustryForStrict(recs []types.IndustryRecord) error {
	for _, r := range recs {
		if r.PITBackfill || r.EffectiveDate.Equal(IndustryBackfillEpoch) {
			return fmt.Errorf("strict PIT: industry %s has no historical membership (effective=%s source=%s backfill=%v)",
				r.TSCode, r.EffectiveDate, r.Source, r.PITBackfill)
		}
		if r.EffectiveDate.Time().IsZero() {
			return fmt.Errorf("strict PIT: industry %s missing effective_date", r.TSCode)
		}
	}
	return nil
}

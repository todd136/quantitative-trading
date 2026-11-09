package backtest

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"quantitative-trading/internal/types"
)

func snapshotEntitlements(state *PortfolioState) []types.CorpActionMark {
	marks := make([]types.CorpActionMark, 0, len(state.Entitlements))
	for _, e := range state.Entitlements {
		m := types.CorpActionMark{TSCode: e.TSCode, Kind: e.Kind, RecordDate: e.RecordDate, ExDate: e.ExDate,
			CashPayDate: e.CashPayDate, SharesTradableDate: e.SharesTradableDate, SharesAtRecord: e.SharesAtRecord,
			CashEntitled: RoundMoney(e.SharesAtRecord * e.CashPerShare), ReceivableOutstanding: e.ReceivableOutstanding,
			SharesEntitled: e.SharesAtRecord * (e.BonusRatio + e.TransferRatio)}
		if e.CashCredited {
			m.CashPaid = m.CashEntitled
		}
		if e.SharesCredited {
			m.SharesCredited = m.SharesEntitled
		}
		marks = append(marks, m)
	}
	sort.Slice(marks, func(i, j int) bool { return claimKey(marks[i]) < claimKey(marks[j]) })
	return marks
}

func claimKey(m types.CorpActionMark) string {
	return fmt.Sprintf("%s|%s|%s|%s", m.TSCode, m.RecordDate, m.ExDate, m.Kind)
}

func auditCSV(path string, header []string, rows [][]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := csv.NewWriter(f)
	if err = w.Write(header); err == nil {
		err = w.WriteAll(rows)
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return err
}

func auditFloat(v float64) string { return strconv.FormatFloat(v, 'g', 17, 64) }

// Keep legacy valuation_marks.csv as carry-only; add complete, full-precision audit exports.
func writeAuditSnapshots(outDir string, snaps []types.DailySnapshot) error {
	var values, claims [][]string
	for _, s := range snaps {
		codes := make([]types.SecurityID, 0, len(s.Holdings))
		for c := range s.Holdings {
			codes = append(codes, c)
		}
		sort.Slice(codes, func(i, j int) bool { return codes[i] < codes[j] })
		for _, c := range codes {
			m, ok := s.Valuations[c]
			if !ok {
				return fmt.Errorf("audit: missing valuation %s %s", s.TradeDate, c)
			}
			values = append(values, []string{s.TradeDate.String(), string(c), auditFloat(s.Holdings[c]), auditFloat(m.Price),
				m.AsOf.String(), m.Source, m.Reason, auditFloat(s.Holdings[c] * m.Price)})
		}
		for _, m := range s.CorpActionMarks {
			claims = append(claims, []string{s.TradeDate.String(), claimKey(m), string(m.TSCode), m.Kind, m.RecordDate.String(),
				m.ExDate.String(), m.CashPayDate.String(), m.SharesTradableDate.String(), auditFloat(m.SharesAtRecord),
				auditFloat(m.CashEntitled), auditFloat(m.ReceivableOutstanding), auditFloat(m.CashPaid),
				auditFloat(m.SharesEntitled), auditFloat(m.SharesCredited)})
		}
	}
	if err := auditCSV(filepath.Join(outDir, "holdings_valuation.csv"), []string{"trade_date", "ts_code", "shares", "price", "as_of", "source", "reason", "market_value"}, values); err != nil {
		return err
	}
	return auditCSV(filepath.Join(outDir, "corp_action_ledger.csv"), []string{"trade_date", "event_id", "ts_code", "kind", "record_date", "ex_date", "cash_pay_date", "shares_tradable_date", "shares_at_record", "cash_entitled", "receivable_outstanding", "cash_paid_cumulative", "shares_entitled", "shares_credited_cumulative"}, claims)
}

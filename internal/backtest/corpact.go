package backtest

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"

	"quantitative-trading/internal/types"
)

// Supported corporate-action kinds. Anything else fail-closes (cannot be ignored).
const (
	CorpKindProfitDistribution = "profit_distribution"
	CorpKindCashDividend       = "cash_dividend"
	CorpKindBonusShare         = "bonus_share"
	CorpKindCapitalTransfer    = "capital_transfer"
)

// Carry reasons recorded on DailySnapshot.CarryMarks.
const (
	CarryReasonHalt       = "confirmed_halt"
	CarryReasonMissingBar = "missing_bar"
	CarrySourceSession    = "session_close"
	CarrySourceLastClose  = "last_close"
)

// Entitlement is a captured record-date claim waiting to be credited.
type Entitlement struct {
	TSCode                types.SecurityID
	RecordDate            types.TradeDate
	ExDate                types.TradeDate
	CashPayDate           types.TradeDate
	SharesTradableDate    types.TradeDate
	SharesAtRecord        float64
	CashPerShare          float64
	BonusRatio            float64
	TransferRatio         float64
	ReceivableAccrued     bool
	ReceivableOutstanding float64 // this event's accrued cash, not a global pool
	CashCredited          bool
	SharesCredited        bool
}

// CorpActionSupported reports whether kind is implemented (cash + bonus/transfer).
func CorpActionSupported(kind string) bool {
	switch kind {
	case CorpKindProfitDistribution, CorpKindCashDividend, CorpKindBonusShare, CorpKindCapitalTransfer:
		return true
	default:
		return false
	}
}

type corpActionJSON struct {
	TSCode             string  `json:"ts_code"`
	Kind               string  `json:"kind"`
	AnnouncementDate   string  `json:"announcement_date"`
	RecordDate         string  `json:"record_date"`
	ExDate             string  `json:"ex_date"`
	CashPayDate        string  `json:"cash_pay_date"`
	SharesTradableDate string  `json:"shares_tradable_date"`
	CashPerShare       float64 `json:"cash_per_share"`
	BonusRatio         float64 `json:"bonus_ratio"`
	TransferRatio      float64 `json:"transfer_ratio"`
	UnsupportedReason  string  `json:"unsupported_reason"`
}

func parseOptionalDate(s string) (types.TradeDate, error) {
	if s == "" {
		return types.TradeDate{}, nil
	}
	return types.ParseTradeDate(s)
}

// CorpActionsSnapshot is a loaded corporate-action file plus provenance.
type CorpActionsSnapshot struct {
	Path          string
	Source        string
	Provider      string
	CoverageStart string
	CoverageEnd   string
	Complete      bool
	Events        []types.CorpAction
	BareArray     bool
}

type corpActionsFileJSON struct {
	Source        string           `json:"source"`
	Provider      string           `json:"provider"`
	CoverageStart string           `json:"coverage_start"`
	CoverageEnd   string           `json:"coverage_end"`
	Complete      bool             `json:"complete"`
	Events        []corpActionJSON `json:"events"`
}

func convertCorpActionRows(path string, rows []corpActionJSON) ([]types.CorpAction, error) {
	out := make([]types.CorpAction, 0, len(rows))
	for i, r := range rows {
		a := types.CorpAction{
			TSCode:            types.SecurityID(r.TSCode),
			Kind:              r.Kind,
			CashPerShare:      r.CashPerShare,
			BonusRatio:        r.BonusRatio,
			TransferRatio:     r.TransferRatio,
			UnsupportedReason: r.UnsupportedReason,
		}
		var e error
		if a.AnnouncementDate, e = parseOptionalDate(r.AnnouncementDate); e != nil {
			return nil, fmt.Errorf("corp actions %s[%d] announcement_date: %w", path, i, e)
		}
		if a.RecordDate, e = parseOptionalDate(r.RecordDate); e != nil {
			return nil, fmt.Errorf("corp actions %s[%d] record_date: %w", path, i, e)
		}
		if a.ExDate, e = parseOptionalDate(r.ExDate); e != nil {
			return nil, fmt.Errorf("corp actions %s[%d] ex_date: %w", path, i, e)
		}
		if a.CashPayDate, e = parseOptionalDate(r.CashPayDate); e != nil {
			return nil, fmt.Errorf("corp actions %s[%d] cash_pay_date: %w", path, i, e)
		}
		if a.SharesTradableDate, e = parseOptionalDate(r.SharesTradableDate); e != nil {
			return nil, fmt.Errorf("corp actions %s[%d] shares_tradable_date: %w", path, i, e)
		}
		out = append(out, a)
	}
	return out, nil
}

// LoadCorpActionsSnapshot reads a JSON array (legacy) or an object with provenance.
func LoadCorpActionsSnapshot(path string) (CorpActionsSnapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return CorpActionsSnapshot{}, fmt.Errorf("corp actions %s: %w", path, err)
	}
	var rows []corpActionJSON
	if err := json.Unmarshal(data, &rows); err == nil {
		ev, e := convertCorpActionRows(path, rows)
		if e != nil {
			return CorpActionsSnapshot{}, e
		}
		return CorpActionsSnapshot{Path: path, Events: ev, BareArray: true}, nil
	}
	var wrap corpActionsFileJSON
	if err := json.Unmarshal(data, &wrap); err != nil {
		return CorpActionsSnapshot{}, fmt.Errorf("corp actions %s: %w", path, err)
	}
	ev, e := convertCorpActionRows(path, wrap.Events)
	if e != nil {
		return CorpActionsSnapshot{}, e
	}
	return CorpActionsSnapshot{
		Path:          path,
		Source:        wrap.Source,
		Provider:      wrap.Provider,
		CoverageStart: wrap.CoverageStart,
		CoverageEnd:   wrap.CoverageEnd,
		Complete:      wrap.Complete,
		Events:        ev,
	}, nil
}

// LoadCorpActionsFile reads events from a corporate-action snapshot (array or object).
func LoadCorpActionsFile(path string) ([]types.CorpAction, error) {
	snap, err := LoadCorpActionsSnapshot(path)
	if err != nil {
		return nil, err
	}
	return snap.Events, nil
}

// RequireFormalCorpActionsMeta rejects bare arrays / incomplete provenance for akshare runs.
func RequireFormalCorpActionsMeta(provider string, snap CorpActionsSnapshot) error {
	p := strings.ToLower(strings.TrimSpace(provider))
	if p != "akshare" {
		return nil
	}
	if snap.BareArray {
		return fmt.Errorf("corp actions: akshare snapshot must be an object with source, coverage_start, coverage_end, complete (bare array not allowed)")
	}
	if strings.TrimSpace(snap.Source) == "" || !snap.Complete || snap.CoverageStart == "" || snap.CoverageEnd == "" {
		return fmt.Errorf("corp actions: akshare snapshot missing source/coverage/complete declaration")
	}
	return nil
}

func normalizeActionDates(a types.CorpAction) types.CorpAction {
	if a.CashPayDate.Time().IsZero() {
		a.CashPayDate = a.ExDate
	}
	if a.SharesTradableDate.Time().IsZero() {
		a.SharesTradableDate = a.ExDate
	}
	return a
}

// ValidateCorpActions fail-closes on unsupported/duplicate events, and in strict
// mode on missing corp-action files or adj-factor jumps without a matching event.
// adj_factor=1 is never treated as evidence of no event.
func (e *Engine) ValidateCorpActions() error {
	if e == nil {
		return fmt.Errorf("nil engine")
	}
	seen := map[string]struct{}{}
	for _, raw := range e.CorpActions {
		if !CorpActionSupported(raw.Kind) {
			reason := raw.UnsupportedReason
			if reason == "" {
				reason = "kind not implemented"
			}
			return fmt.Errorf("unsupported corp action %s kind=%s record=%s: %s",
				raw.TSCode, raw.Kind, raw.RecordDate, reason)
		}
		if raw.RecordDate.Time().IsZero() || raw.ExDate.Time().IsZero() {
			return fmt.Errorf("corp action %s kind=%s missing record_date or ex_date", raw.TSCode, raw.Kind)
		}
		if !raw.ExDate.After(raw.RecordDate) {
			return fmt.Errorf("corp action %s ex_date %s must be after record_date %s",
				raw.TSCode, raw.ExDate, raw.RecordDate)
		}
		key := fmt.Sprintf("%s|%s|%s", raw.TSCode, raw.RecordDate, raw.Kind)
		if _, ok := seen[key]; ok {
			return fmt.Errorf("duplicate corp action %s", key)
		}
		seen[key] = struct{}{}
	}
	if e.Cfg.Data.StrictPIT {
		if !e.CorpActionsLoaded {
			return fmt.Errorf("strict PIT: corp-action records not loaded (missing records cannot default to no events; adj_factor=1 is not evidence of no event)")
		}
		if err := e.validateAdjJumpsHaveActions(); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) actionOnEx(code types.SecurityID, ex types.TradeDate) bool {
	for _, a := range e.CorpActions {
		if a.TSCode == code && a.ExDate.Equal(ex) {
			return true
		}
	}
	return false
}

func (e *Engine) validateAdjJumpsHaveActions() error {
	const rel = 1e-6
	for code, byDate := range e.Bars {
		var prev *types.Bar
		for _, d := range e.Calendar {
			b, ok := byDate[d.String()]
			if !ok || b.AdjFactor <= 0 {
				continue
			}
			if prev != nil && prev.AdjFactor > 0 {
				den := prev.AdjFactor
				if math.Abs(b.AdjFactor-prev.AdjFactor)/den > rel {
					if !e.actionOnEx(code, b.TradeDate) {
						return fmt.Errorf("strict PIT: adj_factor jump %s %s %g → %s %g without matching corp action (adj_factor=1 is not evidence of no event)",
							code, prev.TradeDate, prev.AdjFactor, b.TradeDate, b.AdjFactor)
					}
				}
			}
			bb := b
			prev = &bb
		}
	}
	return nil
}

func (e *Engine) applyOpenCredits(state *PortfolioState, t types.TradeDate) {
	if state == nil {
		return
	}
	for i := range state.Entitlements {
		ent := &state.Entitlements[i]
		if ent.ExDate.Time().IsZero() || t.Before(ent.ExDate) {
			continue
		}
		pay := ent.CashPayDate
		if pay.Time().IsZero() {
			pay = ent.ExDate
		}
		tradable := ent.SharesTradableDate
		if tradable.Time().IsZero() {
			tradable = ent.ExDate
		}
		cashAmt := RoundMoney(ent.SharesAtRecord * ent.CashPerShare)
		if !ent.ReceivableAccrued {
			if t.Before(pay) && cashAmt > 0 {
				state.Receivable = RoundMoney(state.Receivable + cashAmt)
				ent.ReceivableOutstanding = cashAmt
			}
			ent.ReceivableAccrued = true
		}
		if !ent.CashCredited && !t.Before(pay) {
			if ent.ReceivableOutstanding > 0 {
				state.Receivable = RoundMoney(state.Receivable - ent.ReceivableOutstanding)
				if state.Receivable < 0 {
					state.Receivable = 0
				}
				ent.ReceivableOutstanding = 0
			}
			if cashAmt > 0 {
				state.Cash = RoundMoney(state.Cash + cashAmt)
			}
			ent.CashCredited = true
		}
		if !ent.SharesCredited {
			newSh := ent.SharesAtRecord * (ent.BonusRatio + ent.TransferRatio)
			if newSh > 0 {
				state.Lots = append(state.Lots, types.Lot{
					TSCode:        ent.TSCode,
					Shares:        newSh,
					AvailableFrom: tradable,
					CostBasis:     0,
				})
			}
			ent.SharesCredited = true
		}
	}
}

func (e *Engine) captureRecordEntitlements(state *PortfolioState, t types.TradeDate) {
	if state == nil || e == nil {
		return
	}
	for _, raw := range e.CorpActions {
		a := normalizeActionDates(raw)
		if !a.RecordDate.Equal(t) {
			continue
		}
		sh := state.Shares(a.TSCode)
		if sh <= 0 {
			continue
		}
		dup := false
		for _, ent := range state.Entitlements {
			if ent.TSCode == a.TSCode && ent.RecordDate.Equal(a.RecordDate) &&
				ent.CashPerShare == a.CashPerShare &&
				ent.BonusRatio == a.BonusRatio && ent.TransferRatio == a.TransferRatio {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		state.Entitlements = append(state.Entitlements, Entitlement{
			TSCode:             a.TSCode,
			RecordDate:         a.RecordDate,
			ExDate:             a.ExDate,
			CashPayDate:        a.CashPayDate,
			SharesTradableDate: a.SharesTradableDate,
			SharesAtRecord:     sh,
			CashPerShare:       a.CashPerShare,
			BonusRatio:         a.BonusRatio,
			TransferRatio:      a.TransferRatio,
		})
	}
}

// Package backtest implements the T+1 execution engine skeleton (§5).
package backtest

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"

	"quantitative-trading/internal/config"
	"quantitative-trading/internal/types"
	"quantitative-trading/internal/universe"
)

// ErrMissingPrice is returned when a holding must be valued or filled on a
// normal trading day that has no usable price (not a halt carry).
var ErrMissingPrice = errors.New("missing price on trading day")

// PortfolioState is mutable book state during the backtest.
type PortfolioState struct {
	Cash         float64
	Receivable   float64     // confirmed cash dividend not yet paid
	Lots         []types.Lot // per-lot for T+1 availability
	Fills        []types.Fill
	Entitlements []Entitlement
}

// Shares returns total shares for code.
func (p *PortfolioState) Shares(code types.SecurityID) float64 {
	s := 0.0
	for _, l := range p.Lots {
		if l.TSCode == code {
			s += l.Shares
		}
	}
	return s
}

// SellableShares returns shares with AvailableFrom <= execDate.
func (p *PortfolioState) SellableShares(code types.SecurityID, execDate types.TradeDate) float64 {
	s := 0.0
	for _, l := range p.Lots {
		if l.TSCode == code && !l.AvailableFrom.After(execDate) {
			s += l.Shares
		}
	}
	return s
}

// Engine runs signal@T close → exec@T+1 open with costs / limits / T+1.
type Engine struct {
	Cfg      config.Config
	Calendar []types.TradeDate
	CalIndex map[string]int
	// Bars[code][date] = bar
	Bars map[types.SecurityID]map[string]types.Bar
	// Suspended[code][date] confirms an exchange halt independent of bar.Suspended.
	Suspended map[types.SecurityID]map[string]bool
	// CorpActions are loaded events. Empty slice with CorpActionsLoaded=true means
	// "snapshot present, no events in window". Unloaded (false) cannot default to none.
	CorpActions       []types.CorpAction
	CorpActionsLoaded bool
	// SkipCounts accumulates intended vs limit/suspend/missing skips across ApplyTargets calls.
	SkipCounts ExecutionSkipCounts
}

func (e *Engine) isConfirmedHalt(code types.SecurityID, t types.TradeDate) bool {
	if e == nil {
		return false
	}
	if e.Suspended != nil {
		if m := e.Suspended[code]; m != nil && m[t.String()] {
			return true
		}
	}
	if e.Bars == nil {
		return false
	}
	bar, ok := e.Bars[code][t.String()]
	return ok && bar.Suspended
}

// NextTradeDate returns the next calendar day after t, or false if none.
func (e *Engine) NextTradeDate(t types.TradeDate) (types.TradeDate, bool) {
	i, ok := e.CalIndex[t.String()]
	if !ok || i+1 >= len(e.Calendar) {
		return types.TradeDate{}, false
	}
	return e.Calendar[i+1], true
}

// Result is the backtest output (paths + snapshots). Metrics computed only from curve.
type Result struct {
	Snapshots []types.DailySnapshot
	Fills     []types.Fill
	OutDir    string
}

// ApplyTargets executes buys/sells at execDate open toward target weights.
// T+1: cannot sell lots bought on execDate. Limit/suspend skip per §5.3.
// Tallies ExecutionSkipCounts on the engine for §6.2 unfilled skip rate.
func (e *Engine) ApplyTargets(state *PortfolioState, execDate types.TradeDate, targets []types.TargetHolding, nextAvail types.TradeDate) error {
	cfg := e.Cfg
	lotSize := float64(cfg.Execution.LotSize)
	if lotSize <= 0 {
		lotSize = 100
	}

	// mark-to-market NAV at open for sizing (halt/missing: last valid price; never drop holdings)
	nav := state.Cash + state.Receivable
	for _, l := range state.Lots {
		q, err := e.quoteOpen(l.TSCode, execDate)
		if err != nil {
			return fmt.Errorf("open nav %s %s: %w", l.TSCode, execDate, err)
		}
		if q.Missing && e.Cfg.Data.StrictQuotes {
			return fmt.Errorf("%w: unknown missing quote %s on %s (strict_quotes; will not carry last close as a formal result)", ErrMissingPrice, l.TSCode, execDate)
		}
		nav += l.Shares * q.Price
	}
	if nav <= 0 {
		return fmt.Errorf("nav non-positive on %s", execDate)
	}

	targetW := map[types.SecurityID]float64{}
	for _, t := range targets {
		targetW[t.TSCode] = t.Weight
	}

	// gather all codes
	codes := map[types.SecurityID]struct{}{}
	for c := range targetW {
		codes[c] = struct{}{}
	}
	for _, l := range state.Lots {
		codes[l.TSCode] = struct{}{}
	}
	sorted := make([]types.SecurityID, 0, len(codes))
	for c := range codes {
		sorted = append(sorted, c)
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	// Sells first
	for _, code := range sorted {
		tw := targetW[code]
		curShares := state.Shares(code)
		halt := e.isConfirmedHalt(code, execDate)
		bar, ok := e.Bars[code][execDate.String()]
		if halt || !ok || bar.Open <= 0 {
			// Confirmed halt or missing K-line: never fill; keep the lot.
			// Live bar with open<=0: alarm — forbid fill on a normal day without price.
			if ok && !halt && bar.Open <= 0 && curShares > 0 {
				return fmt.Errorf("%w: sell %s on %s (bar present, open<=0, not suspended)", ErrMissingPrice, code, execDate)
			}
			if curShares > 0 && tw == 0 && state.SellableShares(code, execDate) > 0 {
				e.SkipCounts.IntendedSellOrders++
				if halt {
					e.SkipCounts.SkippedSuspend++
				} else {
					e.SkipCounts.SkippedMissingPrice++
				}
			}
			continue
		}
		px := bar.Open
		targetShares := math.Floor((tw*nav/px)/lotSize) * lotSize
		if tw == 0 {
			targetShares = 0
		}
		delta := targetShares - curShares
		if delta >= 0 {
			continue // buy later
		}
		needSell := -delta
		sellable := state.SellableShares(code, execDate)
		if sellable <= 0 {
			continue // T+1 block — not a limit/suspend skip
		}
		// Intended sell order (had sellable shares to reduce toward target).
		e.SkipCounts.IntendedSellOrders++
		if bar.HighLimit <= 0 || bar.LowLimit <= 0 {
			if e.Cfg.Data.StrictPIT {
				return fmt.Errorf("strict: missing reliable open limit prices for sell %s on %s (will not infer from same-day close)", code, execDate)
			}
			e.SkipCounts.SkippedMissingLimit++
			continue
		}
		if universe.CannotSellAtOpen(bar) {
			e.SkipCounts.SkippedLimitSell++
			continue
		}
		qty := math.Min(needSell, sellable)
		qty = math.Floor(qty/lotSize) * lotSize
		if tw == 0 && sellable < needSell {
			// allow odd-lot clear of sellable
			qty = sellable
		}
		if qty <= 0 {
			continue
		}
		notional := qty * px
		cost := SellCost(cfg, notional)
		state.Cash += notional - cost
		state.removeShares(code, qty)
		fill := types.Fill{
			TradeDate: execDate,
			TSCode:    code,
			Side:      "SELL",
			Price:     px,
			Shares:    qty,
			Amount:    notional,
			Cost:      cost,
		}
		state.Fills = append(state.Fills, fill)
	}

	// Buys
	for _, code := range sorted {
		tw := targetW[code]
		if tw <= 0 {
			continue
		}
		halt := e.isConfirmedHalt(code, execDate)
		bar, ok := e.Bars[code][execDate.String()]
		if halt || !ok || bar.Open <= 0 {
			e.SkipCounts.IntendedBuyOrders++
			if ok && !halt && bar.Open <= 0 {
				return fmt.Errorf("%w: buy %s on %s (bar present, open<=0, not suspended)", ErrMissingPrice, code, execDate)
			}
			if halt {
				e.SkipCounts.SkippedSuspend++
			} else {
				e.SkipCounts.SkippedMissingPrice++
			}
			continue
		}
		px := bar.Open
		curShares := state.Shares(code)
		targetShares := math.Floor((tw*nav/px)/lotSize) * lotSize
		delta := targetShares - curShares
		if delta <= 0 {
			continue
		}
		// Intended buy order (target weight above current).
		e.SkipCounts.IntendedBuyOrders++
		if bar.HighLimit <= 0 || bar.LowLimit <= 0 {
			if e.Cfg.Data.StrictPIT {
				return fmt.Errorf("strict: missing reliable open limit prices for buy %s on %s (will not infer from same-day close)", code, execDate)
			}
			e.SkipCounts.SkippedMissingLimit++
			continue
		}
		if universe.CannotBuyAtOpen(bar) {
			e.SkipCounts.SkippedLimitBuy++
			continue
		}
		qty := math.Floor(delta/lotSize) * lotSize
		if qty <= 0 {
			continue
		}
		notional := qty * px
		cost := BuyCost(cfg, notional)
		total := notional + cost
		if total > state.Cash {
			affordable := math.Floor((state.Cash/(px*(1+cfg.BuyCostRate())))/lotSize) * lotSize
			qty = affordable
			if qty <= 0 {
				continue
			}
			notional = qty * px
			cost = BuyCost(cfg, notional)
			total = notional + cost
		}
		state.Cash -= total
		avail := nextAvail
		if !nextAvail.Time().IsZero() {
			// ok
		} else {
			avail = execDate // fallback; caller should set next trading day
		}
		state.Lots = append(state.Lots, types.Lot{
			TSCode:        code,
			Shares:        qty,
			AvailableFrom: avail,
			CostBasis:     px,
		})
		state.Fills = append(state.Fills, types.Fill{
			TradeDate: execDate,
			TSCode:    code,
			Side:      "BUY",
			Price:     px,
			Shares:    qty,
			Amount:    notional,
			Cost:      cost,
		})
	}
	return nil
}

func (p *PortfolioState) removeShares(code types.SecurityID, qty float64) {
	remain := qty
	newLots := make([]types.Lot, 0, len(p.Lots))
	for _, l := range p.Lots {
		if l.TSCode != code || remain <= 0 {
			newLots = append(newLots, l)
			continue
		}
		// prefer sellable lots first — caller already capped by sellable; take FIFO
		take := math.Min(l.Shares, remain)
		l.Shares -= take
		remain -= take
		if l.Shares > 0 {
			newLots = append(newLots, l)
		}
	}
	p.Lots = newLots
}

// priceQuote is a valuation/exec price plus halt vs missing-bar provenance.
type priceQuote struct {
	Price   float64
	Halt    bool // confirmed halt only
	Missing bool // session bar missing, halt not confirmed
	AsOf    types.TradeDate
	Source  string
	Reason  string
}

// lastValidBar walks the engine calendar backward for the latest bar with Close>0.
func (e *Engine) lastValidBar(code types.SecurityID, asof types.TradeDate, inclusive bool) (types.Bar, bool) {
	if e == nil || len(e.Calendar) == 0 {
		return types.Bar{}, false
	}
	idx, ok := e.CalIndex[asof.String()]
	if !ok {
		idx = -1
		for i := len(e.Calendar) - 1; i >= 0; i-- {
			if !e.Calendar[i].After(asof) {
				idx = i
				ok = true
				break
			}
		}
		if !ok {
			return types.Bar{}, false
		}
	}
	if !inclusive {
		idx--
	}
	byDate := e.Bars[code]
	if byDate == nil {
		return types.Bar{}, false
	}
	for i := idx; i >= 0; i-- {
		if b, ok := byDate[e.Calendar[i].String()]; ok && b.Close > 0 {
			return b, true
		}
	}
	return types.Bar{}, false
}

func (e *Engine) quoteClose(code types.SecurityID, t types.TradeDate) (priceQuote, error) {
	halt := e.isConfirmedHalt(code, t)
	bar, ok := e.Bars[code][t.String()]
	if halt {
		if ok && bar.Close > 0 {
			return priceQuote{
				Price: bar.Close, Halt: true, AsOf: t,
				Source: CarrySourceSession, Reason: CarryReasonHalt,
			}, nil
		}
		if prev, okp := e.lastValidBar(code, t, false); okp {
			return priceQuote{
				Price: prev.Close, Halt: true, AsOf: prev.TradeDate,
				Source: CarrySourceLastClose, Reason: CarryReasonHalt,
			}, nil
		}
		return priceQuote{}, fmt.Errorf("%w: halt %s on %s has no last valid close", ErrMissingPrice, code, t)
	}
	if ok {
		if bar.Close > 0 {
			return priceQuote{
				Price: bar.Close, Halt: false, AsOf: t,
				Source: CarrySourceSession,
			}, nil
		}
		return priceQuote{}, fmt.Errorf("%w: %s on %s close<=0 (not suspended)", ErrMissingPrice, code, t)
	}
	// Missing session bar, halt not confirmed: still carry last close so NAV does
	// not drop the holding, but do not classify the gap as a halt.
	if prev, okp := e.lastValidBar(code, t, false); okp {
		return priceQuote{
			Price: prev.Close, Missing: true, AsOf: prev.TradeDate,
			Source: CarrySourceLastClose, Reason: CarryReasonMissingBar,
		}, nil
	}
	return priceQuote{}, fmt.Errorf("%w: %s on %s missing bar and no last valid close", ErrMissingPrice, code, t)
}

func (e *Engine) quoteOpen(code types.SecurityID, t types.TradeDate) (priceQuote, error) {
	halt := e.isConfirmedHalt(code, t)
	bar, ok := e.Bars[code][t.String()]
	if halt {
		if ok && bar.Open > 0 {
			return priceQuote{
				Price: bar.Open, Halt: true, AsOf: t,
				Source: CarrySourceSession, Reason: CarryReasonHalt,
			}, nil
		}
		if ok && bar.Close > 0 {
			return priceQuote{
				Price: bar.Close, Halt: true, AsOf: t,
				Source: CarrySourceSession, Reason: CarryReasonHalt,
			}, nil
		}
		if prev, okp := e.lastValidBar(code, t, false); okp {
			return priceQuote{
				Price: prev.Close, Halt: true, AsOf: prev.TradeDate,
				Source: CarrySourceLastClose, Reason: CarryReasonHalt,
			}, nil
		}
		return priceQuote{}, fmt.Errorf("%w: halt %s on %s has no last valid open", ErrMissingPrice, code, t)
	}
	if ok {
		if bar.Open > 0 {
			return priceQuote{
				Price: bar.Open, Halt: false, AsOf: t,
				Source: CarrySourceSession,
			}, nil
		}
		return priceQuote{}, fmt.Errorf("%w: %s on %s open<=0 (not suspended)", ErrMissingPrice, code, t)
	}
	if prev, okp := e.lastValidBar(code, t, false); okp {
		return priceQuote{
			Price: prev.Close, Missing: true, AsOf: prev.TradeDate,
			Source: CarrySourceLastClose, Reason: CarryReasonMissingBar,
		}, nil
	}
	return priceQuote{}, fmt.Errorf("%w: %s on %s missing bar and no last valid open", ErrMissingPrice, code, t)
}

// MarkNAV builds end-of-day snapshot using close prices.
// Confirmed halt: last valid unadjusted close, flagged on HaltValued.
// Missing bar (halt unconfirmed): last valid close on MissingValued — never drop the holding.
// Normal session with close<=0: error.
// NAV uses raw Close, not CloseAdj (corporate actions are booked as cash/shares).
func (e *Engine) MarkNAV(state *PortfolioState, t types.TradeDate) (types.DailySnapshot, error) {
	holdings := map[types.SecurityID]float64{}
	equity := state.Cash + state.Receivable
	haltValued := map[types.SecurityID]float64{}
	missingValued := map[types.SecurityID]float64{}
	carryMarks := map[types.SecurityID]types.ValuationMark{}
	valuations := map[types.SecurityID]types.ValuationMark{}
	for _, l := range state.Lots {
		holdings[l.TSCode] += l.Shares
	}
	pxByCode := map[types.SecurityID]float64{}
	codes := make([]types.SecurityID, 0, len(holdings))
	for code := range holdings {
		codes = append(codes, code)
	}
	sort.Slice(codes, func(i, j int) bool { return codes[i] < codes[j] })
	for _, code := range codes {
		sh := holdings[code]
		q, err := e.quoteClose(code, t)
		if err != nil {
			return types.DailySnapshot{}, fmt.Errorf("mark nav %s: %w", t, err)
		}
		if q.Missing && e.Cfg.Data.StrictQuotes {
			return types.DailySnapshot{}, fmt.Errorf("%w: unknown missing quote %s on %s (strict_quotes; will not carry last close as a formal result)", ErrMissingPrice, code, t)
		}
		valuations[code] = types.ValuationMark{Price: q.Price, AsOf: q.AsOf, Source: q.Source, Reason: q.Reason}
		pxByCode[code] = q.Price
		equity += sh * q.Price
		if q.Halt {
			haltValued[code] = q.Price
		}
		if q.Missing {
			missingValued[code] = q.Price
		}
		if q.Halt || q.Missing {
			carryMarks[code] = types.ValuationMark{
				Price:  q.Price,
				AsOf:   q.AsOf,
				Source: q.Source,
				Reason: q.Reason,
			}
		}
	}
	weights := map[types.SecurityID]float64{}
	if equity > 0 {
		for code, sh := range holdings {
			weights[code] = sh * pxByCode[code] / equity
		}
	}
	if len(haltValued) == 0 {
		haltValued = nil
	}
	if len(missingValued) == 0 {
		missingValued = nil
	}
	if len(carryMarks) == 0 {
		carryMarks = nil
	}
	return types.DailySnapshot{
		TradeDate:       t,
		NAV:             equity,
		Cash:            state.Cash,
		Receivable:      state.Receivable,
		Holdings:        holdings,
		Weights:         weights,
		HaltValued:      haltValued,
		MissingValued:   missingValued,
		CarryMarks:      carryMarks,
		Valuations:      valuations,
		CorpActionMarks: snapshotEntitlements(state),
	}, nil
}

// WriteSnapshots dumps NAV CSV under outDir (skeleton persistence).
func WriteSnapshots(outDir string, snaps []types.DailySnapshot) (string, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(outDir, "nav.csv")
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	fmt.Fprintln(f, "trade_date,nav,cash,receivable,halt_n,missing_n")
	for _, s := range snaps {
		fmt.Fprintf(f, "%s,%.6f,%.6f,%.6f,%d,%d\n",
			s.TradeDate.String(), s.NAV, s.Cash, s.Receivable, len(s.HaltValued), len(s.MissingValued))
	}
	if err := writeValuationMarks(outDir, snaps); err != nil {
		return path, err
	}
	if err := writeAuditSnapshots(outDir, snaps); err != nil {
		return path, err
	}
	return path, nil
}

func writeValuationMarks(outDir string, snaps []types.DailySnapshot) error {
	path := filepath.Join(outDir, "valuation_marks.csv")
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	fmt.Fprintln(f, "trade_date,ts_code,price,as_of,source,reason,market_value,nav_weight")
	for _, s := range snaps {
		codes := make([]types.SecurityID, 0, len(s.CarryMarks))
		for c := range s.CarryMarks {
			codes = append(codes, c)
		}
		sort.Slice(codes, func(i, j int) bool { return codes[i] < codes[j] })
		for _, c := range codes {
			m := s.CarryMarks[c]
			sh := s.Holdings[c]
			mv := sh * m.Price
			w := 0.0
			if s.NAV > 0 {
				w = mv / s.NAV
			}
			fmt.Fprintf(f, "%s,%s,%.6f,%s,%s,%s,%.6f,%.8f\n",
				s.TradeDate.String(), c, m.Price, m.AsOf.String(), m.Source, m.Reason, mv, w)
		}
	}
	return nil
}

// WriteFills dumps fills CSV.
func WriteFills(outDir string, fills []types.Fill) (string, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(outDir, "fills.csv")
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	fmt.Fprintln(f, "trade_date,ts_code,side,price,shares,amount,cost")
	for _, x := range fills {
		fmt.Fprintf(f, "%s,%s,%s,%.4f,%.2f,%.4f,%.4f\n",
			x.TradeDate.String(), x.TSCode, x.Side, x.Price, x.Shares, x.Amount, x.Cost)
	}
	return path, nil
}

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
	Cash  float64
	Lots  []types.Lot // per-lot for T+1 availability
	Fills []types.Fill
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
	// SkipCounts accumulates intended vs limit/suspend skips across ApplyTargets calls.
	SkipCounts ExecutionSkipCounts
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

	// mark-to-market NAV at open for sizing (halt: last valid price; never drop holdings)
	nav := state.Cash
	for _, l := range state.Lots {
		q, err := e.quoteOpen(l.TSCode, execDate)
		if err != nil {
			return fmt.Errorf("open nav %s %s: %w", l.TSCode, execDate, err)
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
		bar, ok := e.Bars[code][execDate.String()]
		if !ok || bar.Open <= 0 {
			// Halt / missing K-line: never fill without a session open; keep the lot.
			// Live bar with open<=0: alarm — forbid fill on a normal day without price.
			if ok && !bar.Suspended && bar.Open <= 0 && curShares > 0 {
				return fmt.Errorf("%w: sell %s on %s (bar present, open<=0, not suspended)", ErrMissingPrice, code, execDate)
			}
			if curShares > 0 && tw == 0 && state.SellableShares(code, execDate) > 0 {
				e.SkipCounts.IntendedSellOrders++
				e.SkipCounts.SkippedSuspend++
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
		if bar.Suspended {
			e.SkipCounts.SkippedSuspend++
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
		bar, ok := e.Bars[code][execDate.String()]
		if !ok || bar.Open <= 0 {
			e.SkipCounts.IntendedBuyOrders++
			if ok && !bar.Suspended && bar.Open <= 0 {
				return fmt.Errorf("%w: buy %s on %s (bar present, open<=0, not suspended)", ErrMissingPrice, code, execDate)
			}
			e.SkipCounts.SkippedSuspend++
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
		if bar.Suspended {
			e.SkipCounts.SkippedSuspend++
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

// priceQuote is a valuation/exec price plus whether it was carried through a halt gap.
type priceQuote struct {
	Price float64
	Halt  bool
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
	bar, ok := e.Bars[code][t.String()]
	if ok && bar.Suspended {
		if bar.Close > 0 {
			return priceQuote{Price: bar.Close, Halt: true}, nil
		}
		if prev, okp := e.lastValidBar(code, t, false); okp {
			return priceQuote{Price: prev.Close, Halt: true}, nil
		}
		return priceQuote{}, fmt.Errorf("%w: halt %s on %s has no last valid close", ErrMissingPrice, code, t)
	}
	if ok && !bar.Suspended {
		if bar.Close > 0 {
			return priceQuote{Price: bar.Close, Halt: false}, nil
		}
		return priceQuote{}, fmt.Errorf("%w: %s on %s close<=0 (not suspended)", ErrMissingPrice, code, t)
	}
	// Missing session bar on a trading day: A-share halt typically has no K-line.
	// Carry last valid close so NAV does not drop the holding (fake PnL).
	if prev, okp := e.lastValidBar(code, t, false); okp {
		return priceQuote{Price: prev.Close, Halt: true}, nil
	}
	return priceQuote{}, fmt.Errorf("%w: %s on %s missing bar and no last valid close", ErrMissingPrice, code, t)
}

func (e *Engine) quoteOpen(code types.SecurityID, t types.TradeDate) (priceQuote, error) {
	bar, ok := e.Bars[code][t.String()]
	if ok && bar.Suspended {
		if bar.Open > 0 {
			return priceQuote{Price: bar.Open, Halt: true}, nil
		}
		if bar.Close > 0 {
			return priceQuote{Price: bar.Close, Halt: true}, nil
		}
		if prev, okp := e.lastValidBar(code, t, false); okp {
			px := prev.Close
			if prev.Open > 0 {
				px = prev.Open
			}
			return priceQuote{Price: px, Halt: true}, nil
		}
		return priceQuote{}, fmt.Errorf("%w: halt %s on %s has no last valid open", ErrMissingPrice, code, t)
	}
	if ok && !bar.Suspended {
		if bar.Open > 0 {
			return priceQuote{Price: bar.Open, Halt: false}, nil
		}
		return priceQuote{}, fmt.Errorf("%w: %s on %s open<=0 (not suspended)", ErrMissingPrice, code, t)
	}
	if prev, okp := e.lastValidBar(code, t, false); okp {
		px := prev.Close
		if prev.Open > 0 {
			px = prev.Open
		}
		return priceQuote{Price: px, Halt: true}, nil
	}
	return priceQuote{}, fmt.Errorf("%w: %s on %s missing bar and no last valid open", ErrMissingPrice, code, t)
}

// MarkNAV builds end-of-day snapshot using close prices.
// Halt / missing K-line: last valid unadjusted close, flagged on HaltValued.
// Normal session with close<=0: error (never silently drop the holding).
// NAV uses raw Close, not CloseAdj (ex-div/split is in the vendor close).
func (e *Engine) MarkNAV(state *PortfolioState, t types.TradeDate) (types.DailySnapshot, error) {
	holdings := map[types.SecurityID]float64{}
	equity := state.Cash
	haltValued := map[types.SecurityID]float64{}
	for _, l := range state.Lots {
		holdings[l.TSCode] += l.Shares
	}
	pxByCode := map[types.SecurityID]float64{}
	for code, sh := range holdings {
		q, err := e.quoteClose(code, t)
		if err != nil {
			return types.DailySnapshot{}, fmt.Errorf("mark nav %s: %w", t, err)
		}
		pxByCode[code] = q.Price
		equity += sh * q.Price
		if q.Halt {
			haltValued[code] = q.Price
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
	return types.DailySnapshot{
		TradeDate:  t,
		NAV:        equity,
		Cash:       state.Cash,
		Holdings:   holdings,
		Weights:    weights,
		HaltValued: haltValued,
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
	fmt.Fprintln(f, "trade_date,nav,cash")
	for _, s := range snaps {
		fmt.Fprintf(f, "%s,%.6f,%.6f\n", s.TradeDate.String(), s.NAV, s.Cash)
	}
	return path, nil
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

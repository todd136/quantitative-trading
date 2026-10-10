// Paper-trading / simulation holdings export: next-day target list only (no auto orders).
package backtest

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"quantitative-trading/internal/types"
)

// PaperAction labels weight change vs prior target (not fill outcomes).
const (
	PaperActionEnter    = "ENTER"
	PaperActionExit     = "EXIT"
	PaperActionIncrease = "INCREASE"
	PaperActionDecrease = "DECREASE"
	PaperActionHold     = "HOLD"
)

// PaperTargetDay is one rebalance signal day's intended target book.
// Weights come from SelectHoldings (equal-weight targets), not post-fill snaps.
type PaperTargetDay struct {
	SignalDate types.TradeDate
	ExecDate   types.TradeDate
	// Weights: security → target weight (sum ≈ 1 over equity sleeve).
	Weights map[types.SecurityID]float64
	// NAV optional mark used to estimate target_shares (e.g. cash+equity before rebalance).
	NAV float64
	// Prices optional closes (signal-day or last available) for share estimate.
	Prices map[types.SecurityID]float64
}

// PaperTargetRow is one CSV/MD line for paper following.
type PaperTargetRow struct {
	SignalDate   string
	ExecDate     string
	Symbol       string
	TargetWeight float64
	PrevWeight   float64
	NewWeight    float64
	Action       string
	// TargetShares set when NAV+price known; otherwise nil (honest blank).
	TargetShares *float64
	PriceUsed    *float64
}

// PaperExportResult holds rows plus notes for the MD header.
type PaperExportResult struct {
	Rows            []PaperTargetRow
	SharesAvailable bool
	Notes           []string
}

// BuildPaperTargets diffs consecutive PaperTargetDay books into ENTER/EXIT/… rows.
// Never invents returns; share counts only when NAV>0 and price>0.
// lotSize>0 floors shares to lot multiples; lotSize<=0 leaves raw shares.
func BuildPaperTargets(days []PaperTargetDay, lotSize int) PaperExportResult {
	res := PaperExportResult{
		Notes: []string{
			"Paper / simulation export only — next-day target holdings, not auto orders.",
			"Not investment advice. Weights are intended SelectHoldings targets (pre-fill).",
			"Actions compare consecutive rebalance targets; non-rebalance days are omitted (use portfolio.rebalance_freq).",
		},
	}
	if len(days) == 0 {
		res.Notes = append(res.Notes, "No rebalance target days collected.")
		return res
	}

	var prev map[types.SecurityID]float64
	sharesOK := false
	anyShareMissing := false

	for _, d := range days {
		cur := map[types.SecurityID]float64{}
		for code, w := range d.Weights {
			if w == 0 || math.IsNaN(w) || math.IsInf(w, 0) {
				continue
			}
			cur[code] = w
		}
		syms := map[types.SecurityID]struct{}{}
		for c := range cur {
			syms[c] = struct{}{}
		}
		for c := range prev {
			syms[c] = struct{}{}
		}
		codes := make([]string, 0, len(syms))
		for c := range syms {
			codes = append(codes, string(c))
		}
		sort.Strings(codes)

		for _, cs := range codes {
			code := types.SecurityID(cs)
			newW := cur[code]
			oldW := 0.0
			if prev != nil {
				oldW = prev[code]
			}
			action := paperAction(oldW, newW)
			row := PaperTargetRow{
				SignalDate:   d.SignalDate.String(),
				ExecDate:     d.ExecDate.String(),
				Symbol:       cs,
				TargetWeight: newW,
				PrevWeight:   oldW,
				NewWeight:    newW,
				Action:       action,
			}
			if d.NAV > 0 && d.Prices != nil {
				if px, ok := d.Prices[code]; ok && px > 0 && newW > 0 {
					raw := newW * d.NAV / px
					if lotSize > 0 {
						raw = math.Floor(raw/float64(lotSize)) * float64(lotSize)
					}
					row.TargetShares = &raw
					pxc := px
					row.PriceUsed = &pxc
					sharesOK = true
				} else if newW > 0 {
					anyShareMissing = true
				}
			} else if newW > 0 {
				anyShareMissing = true
			}
			res.Rows = append(res.Rows, row)
		}
		prev = cur
	}

	res.SharesAvailable = sharesOK
	if !sharesOK {
		res.Notes = append(res.Notes, "target_shares left blank: NAV and/or close prices unavailable — weights only.")
	} else if anyShareMissing {
		res.Notes = append(res.Notes, "Some rows lack target_shares (missing close on that name); weights still exported.")
	}
	if lotSize > 0 && sharesOK {
		res.Notes = append(res.Notes, fmt.Sprintf("target_shares floored to lot_size=%d when estimated.", lotSize))
	}
	return res
}

func paperAction(prev, next float64) string {
	prevZ := NearlyEqualDefault(prev, 0)
	nextZ := NearlyEqualDefault(next, 0)
	switch {
	case prevZ && !nextZ:
		return PaperActionEnter
	case !prevZ && nextZ:
		return PaperActionExit
	case NearlyEqualDefault(prev, next):
		return PaperActionHold
	case next > prev:
		return PaperActionIncrease
	default:
		return PaperActionDecrease
	}
}

// WritePaperTargets writes paper_targets.csv and paper_targets.md under outDir.
func WritePaperTargets(outDir string, res PaperExportResult) (csvPath, mdPath string, err error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", "", err
	}
	csvPath = filepath.Join(outDir, "paper_targets.csv")
	mdPath = filepath.Join(outDir, "paper_targets.md")

	cf, err := os.Create(csvPath)
	if err != nil {
		return "", "", err
	}
	defer cf.Close()
	fmt.Fprintln(cf, "signal_date,exec_date,symbol,target_weight,prev_weight,new_weight,action,target_shares,price_used")
	for _, r := range res.Rows {
		shares := ""
		price := ""
		if r.TargetShares != nil {
			shares = fmt.Sprintf("%.2f", *r.TargetShares)
		}
		if r.PriceUsed != nil {
			price = fmt.Sprintf("%.4f", *r.PriceUsed)
		}
		fmt.Fprintf(cf, "%s,%s,%s,%.8f,%.8f,%.8f,%s,%s,%s\n",
			r.SignalDate, r.ExecDate, r.Symbol,
			r.TargetWeight, r.PrevWeight, r.NewWeight, r.Action, shares, price)
	}

	mf, err := os.Create(mdPath)
	if err != nil {
		return csvPath, "", err
	}
	defer mf.Close()
	fmt.Fprintln(mf, "# Paper / simulation target holdings")
	fmt.Fprintln(mf)
	fmt.Fprintln(mf, "**Not investment advice. Export of next-day intended targets only — no auto orders, no fabricated returns.**")
	fmt.Fprintln(mf)
	for _, n := range res.Notes {
		fmt.Fprintf(mf, "- %s\n", n)
	}
	fmt.Fprintln(mf)
	fmt.Fprintf(mf, "Rows: %d\n\n", len(res.Rows))
	fmt.Fprintln(mf, "| signal_date | exec_date | symbol | target_weight | prev_weight | new_weight | action | target_shares | price_used |")
	fmt.Fprintln(mf, "|---|---|---|---:|---:|---:|---|---:|---:|")
	for _, r := range res.Rows {
		shares := "—"
		price := "—"
		if r.TargetShares != nil {
			shares = fmt.Sprintf("%.2f", *r.TargetShares)
		}
		if r.PriceUsed != nil {
			price = fmt.Sprintf("%.4f", *r.PriceUsed)
		}
		fmt.Fprintf(mf, "| %s | %s | %s | %.6f | %.6f | %.6f | %s | %s | %s |\n",
			r.SignalDate, r.ExecDate, r.Symbol,
			r.TargetWeight, r.PrevWeight, r.NewWeight, r.Action, shares, price)
	}
	return csvPath, mdPath, nil
}

// TargetsToWeightMap converts SelectHoldings output to a weight map.
func TargetsToWeightMap(targets []types.TargetHolding) map[types.SecurityID]float64 {
	m := make(map[types.SecurityID]float64, len(targets))
	for _, t := range targets {
		if t.Weight == 0 || math.IsNaN(t.Weight) || math.IsInf(t.Weight, 0) {
			continue
		}
		m[t.TSCode] = t.Weight
	}
	return m
}

// CollectSignalCloses picks close on signalDate when present (for share estimate).
func CollectSignalCloses(codes []types.SecurityID, signalDate types.TradeDate, bars map[types.SecurityID]map[string]types.Bar) map[types.SecurityID]float64 {
	out := map[types.SecurityID]float64{}
	ds := signalDate.String()
	for _, code := range codes {
		byDate, ok := bars[code]
		if !ok {
			continue
		}
		if b, ok := byDate[ds]; ok && b.Close > 0 {
			out[code] = b.Close
		}
	}
	return out
}

// SummarizePaperActions returns a short count string for CLI/logs.
func SummarizePaperActions(rows []PaperTargetRow) string {
	counts := map[string]int{}
	for _, r := range rows {
		counts[r.Action]++
	}
	order := []string{PaperActionEnter, PaperActionExit, PaperActionIncrease, PaperActionDecrease, PaperActionHold}
	parts := make([]string, 0, len(order))
	for _, a := range order {
		if n := counts[a]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", a, n))
		}
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, " ")
}

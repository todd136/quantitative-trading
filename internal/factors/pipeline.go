// Package factors implements raw Quality / Value / Momentum and the processing pipeline (§3).
package factors

import (
	"math"
	"sort"

	"quantitative-trading/internal/config"
	"quantitative-trading/internal/types"
)

// Point is one name's raw + processed factor values on day T.
type Point struct {
	TSCode   types.SecurityID
	Industry string
	RawQ     *float64
	RawV     *float64
	RawM     *float64
	ZQ       *float64
	ZV       *float64
	ZM       *float64
}

// GetLatestFinancial returns the latest row with announcement_date ≤ T (PIT hard rule §3.7).
// Among those, prefers the newest report_period; ties broken by later announcement.
func GetLatestFinancial(rows []types.FinancialRow, t types.TradeDate) (types.FinancialRow, bool) {
	var best types.FinancialRow
	found := false
	for _, r := range rows {
		if r.AnnouncementDate.After(t) {
			continue // future announcement invisible
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

// RawQuality = (1/3)*(ROE + GrossMargin + (-DebtToAsset)) before cross-section pipeline.
func RawQuality(fin types.FinancialRow) (float64, bool) {
	if fin.Equity == 0 || fin.Revenue == 0 || fin.TotalAssets == 0 {
		return 0, false
	}
	roe := fin.NetProfit / fin.Equity
	gm := fin.GrossProfit / fin.Revenue
	dta := fin.TotalLiabilities / fin.TotalAssets
	return (roe + gm + (-dta)) / 3.0, true
}

// RawValue = 0.5*EP + 0.5*BP using T-day total MV.
func RawValue(fin types.FinancialRow, totalMV float64) (float64, bool) {
	if totalMV <= 0 || fin.Equity == 0 {
		return 0, false
	}
	ep := fin.NetProfit / totalMV
	bp := fin.Equity / totalMV
	return 0.5*ep + 0.5*bp, true
}

// RawMomentum = close_adj[T-21]/close_adj[T-252] - 1.
// bars must be sorted ascending and include T. Returns false if window incomplete.
func RawMomentum(bars []types.Bar, t types.TradeDate, lookback, skip int) (float64, bool) {
	if lookback <= 0 {
		lookback = 252
	}
	if skip <= 0 {
		skip = 21
	}
	idx := -1
	for i := range bars {
		if bars[i].TradeDate.Equal(t) {
			idx = i
			break
		}
	}
	if idx < 0 {
		return 0, false
	}
	iNear := idx - skip
	iFar := idx - lookback
	if iNear < 0 || iFar < 0 {
		return 0, false
	}
	cNear := bars[iNear].CloseAdj()
	cFar := bars[iFar].CloseAdj()
	if cFar == 0 {
		return 0, false
	}
	return cNear/cFar - 1.0, true
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	cp := append([]float64(nil), xs...)
	sort.Float64s(cp)
	n := len(cp)
	if n%2 == 1 {
		return cp[n/2]
	}
	return 0.5 * (cp[n/2-1] + cp[n/2])
}

// WinsorizeMAD clips to med ± n * 1.4826 * MAD (§3.3).
func WinsorizeMAD(x []float64, n float64) []float64 {
	out := append([]float64(nil), x...)
	if len(out) == 0 {
		return out
	}
	med := median(out)
	devs := make([]float64, len(out))
	for i, v := range out {
		devs[i] = math.Abs(v - med)
	}
	mad := median(devs)
	if mad == 0 {
		return out
	}
	sigma := 1.4826 * mad
	lo := med - n*sigma
	hi := med + n*sigma
	for i, v := range out {
		if v < lo {
			out[i] = lo
		} else if v > hi {
			out[i] = hi
		}
	}
	return out
}

// IndustryNeutralize subtracts within-industry mean (§3.5). Missing industry → NaN marker via ok=false handled by caller.
func IndustryNeutralize(x []float64, industry []string) []float64 {
	out := append([]float64(nil), x...)
	groups := map[string][]int{}
	for i, ind := range industry {
		groups[ind] = append(groups[ind], i)
	}
	for _, idxs := range groups {
		sum := 0.0
		for _, i := range idxs {
			sum += out[i]
		}
		mu := sum / float64(len(idxs))
		for _, i := range idxs {
			out[i] -= mu
		}
	}
	return out
}

// CrossSectionalZScore uses sample std (ddof=1); sd==0 → all zeros (§3.4).
func CrossSectionalZScore(x []float64) []float64 {
	out := make([]float64, len(x))
	if len(x) == 0 {
		return out
	}
	sum := 0.0
	for _, v := range x {
		sum += v
	}
	mu := sum / float64(len(x))
	if len(x) == 1 {
		return out // all 0
	}
	var ss float64
	for _, v := range x {
		d := v - mu
		ss += d * d
	}
	sd := math.Sqrt(ss / float64(len(x)-1))
	if sd == 0 {
		return out
	}
	for i, v := range x {
		out[i] = (v - mu) / sd
	}
	return out
}

// ProcessCrossSection runs locked order: MAD winsorize → industry neutralize → z-score.
// Only finite raw values; names with missing industry are dropped from neutralization set
// (caller should mark missing per §3.6).
func ProcessCrossSection(raw []float64, industry []string, cfg config.ProcessingConfig) []float64 {
	x := raw
	if cfg.Winsorize.Method == "MAD" || cfg.Winsorize.Method == "" {
		n := cfg.Winsorize.N
		if n == 0 {
			n = 5
		}
		x = WinsorizeMAD(x, n)
	}
	if cfg.IndustryNeutral {
		x = IndustryNeutralize(x, industry)
	}
	if cfg.ZScore {
		x = CrossSectionalZScore(x)
	}
	return x
}

// PipelineResult holds processed scores keyed by ts_code.
type PipelineResult struct {
	Scores map[types.SecurityID]types.FactorScores
}

func fptr(v float64) *float64 { return &v }

// BuildRawAndProcess computes raw factors for universe names then runs the pipeline per factor.
// require_all drops names missing any of the three processed Z scores.
func BuildRawAndProcess(
	cfg config.Config,
	universe []types.SecurityID,
	industry map[types.SecurityID]string,
	rawQ, rawV, rawM map[types.SecurityID]float64,
) PipelineResult {
	res := PipelineResult{Scores: make(map[types.SecurityID]types.FactorScores)}

	processOne := func(rawMap map[types.SecurityID]float64) map[types.SecurityID]float64 {
		codes := make([]types.SecurityID, 0)
		vals := make([]float64, 0)
		inds := make([]string, 0)
		for _, c := range universe {
			v, ok := rawMap[c]
			if !ok {
				continue
			}
			ind := industry[c]
			if ind == "" && cfg.Processing.IndustryNeutral {
				continue // industry missing → factor missing (§3.6)
			}
			codes = append(codes, c)
			vals = append(vals, v)
			inds = append(inds, ind)
		}
		if len(vals) == 0 {
			return nil
		}
		z := ProcessCrossSection(vals, inds, cfg.Processing)
		out := make(map[types.SecurityID]float64, len(codes))
		for i, c := range codes {
			out[c] = z[i]
		}
		return out
	}

	zq := processOne(rawQ)
	zv := processOne(rawV)
	zm := processOne(rawM)

	for _, c := range universe {
		fs := types.FactorScores{TSCode: c}
		if zq != nil {
			if v, ok := zq[c]; ok {
				fs.Quality = fptr(v)
			}
		}
		if zv != nil {
			if v, ok := zv[c]; ok {
				fs.Value = fptr(v)
			}
		}
		if zm != nil {
			if v, ok := zm[c]; ok {
				fs.Momentum = fptr(v)
			}
		}
		if cfg.Factors.RequireAll {
			if fs.Quality == nil || fs.Value == nil || fs.Momentum == nil {
				continue
			}
		}
		res.Scores[c] = fs
	}
	return res
}

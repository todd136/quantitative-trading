package backtest

import (
	"fmt"
	"math"
	"sort"
	"strconv"

	"quantitative-trading/internal/types"
)

// BrinsonMethodNote documents the simple Brinson-style decomposition used when inputs exist.
const BrinsonMethodNote = `Simple Brinson (BF) skeleton: period excess ≈ Allocation + Selection + Interaction.
Allocation_i = (w_p,i - w_b,i) * r_b,i; Selection_i = w_b,i * (r_p,i - r_b,i);
Interaction_i = (w_p,i - w_b,i) * (r_p,i - r_b,i). Summed over industries.
Requires portfolio industry weights, benchmark industry weights, and industry returns for the same period.
Not investment advice; research diagnostic only.`

// FactorLayerMethodNote documents layered / long-short factor return construction.
const FactorLayerMethodNote = `Cross-sectional factor layers: each day rank names by factor score into Q quantiles (default 5);
layer return = equal-weight mean of that day's forward returns in the quantile.
Long-short = top quantile mean − bottom quantile mean. Only computed when real scores AND forward returns are provided — never invented.`

// YearlyMetrics is calendar-year performance derived from a real NAV segment (never fabricated).
type YearlyMetrics struct {
	Year       int     `json:"year"`
	NSnapshots int     `json:"n_snapshots"`
	StartDate  string  `json:"start_date,omitempty"`
	EndDate    string  `json:"end_date,omitempty"`
	Metrics    Metrics `json:"metrics"`
}

// IndustryWeightPoint is one day's portfolio weight by industry.
type IndustryWeightPoint struct {
	TradeDate string             `json:"trade_date"`
	Weights   map[string]float64 `json:"weights"` // industry code → weight
}

// FactorExposurePoint is portfolio-weighted factor exposure for one day.
type FactorExposurePoint struct {
	TradeDate string             `json:"trade_date"`
	Composite *float64           `json:"composite,omitempty"`
	Factors   map[string]float64 `json:"factors,omitempty"` // quality / value / momentum / …
}

// AttributionResult is Brinson / simple attribution skeleton output.
type AttributionResult struct {
	Available   bool          `json:"available"`
	Method      string        `json:"method"`
	Note        string        `json:"note,omitempty"`
	Allocation  OptionalFloat `json:"allocation"`
	Selection   OptionalFloat `json:"selection"`
	Interaction OptionalFloat `json:"interaction"`
	NIndustries int           `json:"n_industries,omitempty"`
}

// LayerReturn is one quantile (or long/short) mean forward return.
type LayerReturn struct {
	Layer      string        `json:"layer"` // Q1..Qn or "long_short"
	MeanReturn OptionalFloat `json:"mean_return"`
	NDays      int           `json:"n_days"`
}

// FactorLayerResult is per-factor layered / long-short research diagnostic.
type FactorLayerResult struct {
	Available bool          `json:"available"`
	Method    string        `json:"method"`
	Note      string        `json:"note,omitempty"`
	Factor    string        `json:"factor,omitempty"`
	NQuantile int           `json:"n_quantile,omitempty"`
	Layers    []LayerReturn `json:"layers,omitempty"`
}

// LlmCoverageStats is §6.2 optional LLM coverage / degrade diagnostics.
type LlmCoverageStats struct {
	Available      bool          `json:"available"`
	Note           string        `json:"note,omitempty"`
	CoverageRate   OptionalFloat `json:"coverage_rate"`
	DegradeCount   OptionalFloat `json:"degrade_count"`
	AnnotateCalls  OptionalFloat `json:"annotate_calls"`
}

// ExecutionSkipCounts are limit/suspend unfilled skips from the engine (when tracked).
type ExecutionSkipCounts struct {
	IntendedBuyOrders  int `json:"intended_buy_orders"`
	IntendedSellOrders int `json:"intended_sell_orders"`
	SkippedLimitBuy    int `json:"skipped_limit_buy"`
	SkippedLimitSell   int `json:"skipped_limit_sell"`
	SkippedSuspend     int `json:"skipped_suspend"`
}

// DailyFactorPanel is one cross-section of processed factor scores + optional forward returns.
// ForwardRet must be real next-period returns when present; never invent.
type DailyFactorPanel struct {
	TradeDate types.TradeDate
	Quality   map[types.SecurityID]float64
	Value     map[types.SecurityID]float64
	Momentum  map[types.SecurityID]float64
	Composite map[types.SecurityID]float64
	ForwardRet map[types.SecurityID]float64 // optional; required for layer / long-short
}

// IndustryPeriodReturn is one industry's portfolio/benchmark weight and return for attribution.
type IndustryPeriodReturn struct {
	Industry   string
	PortWeight float64
	BenchWeight float64
	PortReturn float64
	BenchReturn float64
}

// ReportExtras carries optional §6.2 inputs. Missing fields → Available=false + honest notes.
type ReportExtras struct {
	// IndustryLookup maps security → industry code used to build industry weight TS from snap Weights.
	IndustryLookup map[types.SecurityID]string
	// FactorPanels enable factor exposure TS and (with ForwardRet) layered / long-short returns.
	FactorPanels []DailyFactorPanel
	// AttributionPeriods: when non-empty, Brinson skeleton is computed; else N/A with method note.
	AttributionPeriods [][]IndustryPeriodReturn
	// SkipCounts: when non-nil with intended orders > 0, unfilled skip rate is computed.
	SkipCounts *ExecutionSkipCounts
	// LlmCounters: when non-nil, coverage/degrade stats are filled; else N/A.
	LlmCounters *LlmRunCounters
	// LayerQuantiles defaults to 5 when <=0.
	LayerQuantiles int
}

// LlmRunCounters are optional counters from an enabled LLM path.
type LlmRunCounters struct {
	AnnotateCalls   int
	FeaturesEmitted int // names with non-nil sentiment (or equivalent)
	DegradeCount    int
	CandidateTotal  int // sum of candidate universe sizes across annotate calls
}

// ComputeYearlyMetrics splits snaps by calendar year and computes Metrics per year.
func ComputeYearlyMetrics(snaps []types.DailySnapshot, in MetricsInput) []YearlyMetrics {
	if len(snaps) == 0 {
		return nil
	}
	type yearBucket struct {
		year  int
		snaps []types.DailySnapshot
		fills []types.Fill
	}
	order := make([]int, 0)
	byYear := map[int]*yearBucket{}
	for _, s := range snaps {
		y := s.TradeDate.Time().Year()
		b, ok := byYear[y]
		if !ok {
			b = &yearBucket{year: y}
			byYear[y] = b
			order = append(order, y)
		}
		b.snaps = append(b.snaps, s)
	}
	sort.Ints(order)

	fillByYear := map[int][]types.Fill{}
	for _, f := range in.Fills {
		y := f.TradeDate.Time().Year()
		fillByYear[y] = append(fillByYear[y], f)
	}
	benchByYear := map[int][]types.IndexBar{}
	for _, b := range in.Benchmark {
		y := b.TradeDate.Time().Year()
		benchByYear[y] = append(benchByYear[y], b)
	}

	out := make([]YearlyMetrics, 0, len(order))
	for _, y := range order {
		b := byYear[y]
		ym := YearlyMetrics{
			Year:       y,
			NSnapshots: len(b.snaps),
			Metrics: ComputeMetricsFull(MetricsInput{
				Snaps:     b.snaps,
				Fills:     fillByYear[y],
				Benchmark: benchByYear[y],
				RiskFree:  in.RiskFree,
			}),
		}
		if len(b.snaps) > 0 {
			ym.StartDate = b.snaps[0].TradeDate.String()
			ym.EndDate = b.snaps[len(b.snaps)-1].TradeDate.String()
		}
		out = append(out, ym)
	}
	return out
}

// ComputeIndustryDistribution builds daily industry weight points from snap Weights + lookup.
func ComputeIndustryDistribution(snaps []types.DailySnapshot, lookup map[types.SecurityID]string) (points []IndustryWeightPoint, status OptionalFloat) {
	if len(lookup) == 0 {
		return nil, na("industry distribution TS requires IndustryLookup (security→industry); not provided")
	}
	hasW := false
	for _, s := range snaps {
		if len(s.Weights) > 0 {
			hasW = true
			break
		}
	}
	if !hasW {
		return nil, na("industry distribution TS requires Weights on DailySnapshot; none present")
	}
	points = make([]IndustryWeightPoint, 0, len(snaps))
	mappedDays := 0
	for _, s := range snaps {
		if len(s.Weights) == 0 {
			continue
		}
		w := map[string]float64{}
		any := false
		for code, wt := range s.Weights {
			if wt == 0 {
				continue
			}
			ind := lookup[code]
			if ind == "" {
				ind = "UNKNOWN"
			} else {
				any = true
			}
			w[ind] += wt
		}
		if len(w) == 0 {
			continue
		}
		if any {
			mappedDays++
		}
		points = append(points, IndustryWeightPoint{
			TradeDate: s.TradeDate.String(),
			Weights:   w,
		})
	}
	if len(points) == 0 {
		return nil, na("no days with Weights to build industry distribution")
	}
	return points, avail(float64(mappedDays))
}

// ComputeFactorExposure builds portfolio-weighted factor exposure TS from panels + snap weights.
func ComputeFactorExposure(snaps []types.DailySnapshot, panels []DailyFactorPanel) (points []FactorExposurePoint, status OptionalFloat) {
	if len(panels) == 0 {
		return nil, na("factor exposure TS requires DailyFactorPanel cross-sections; not provided")
	}
	panelByDate := map[string]DailyFactorPanel{}
	for _, p := range panels {
		panelByDate[p.TradeDate.String()] = p
	}
	weightByDate := map[string]map[types.SecurityID]float64{}
	for _, s := range snaps {
		if len(s.Weights) > 0 {
			weightByDate[s.TradeDate.String()] = s.Weights
		}
	}
	if len(weightByDate) == 0 {
		return nil, na("factor exposure TS requires Weights on DailySnapshot; none present")
	}
	dates := make([]string, 0)
	for d := range panelByDate {
		if _, ok := weightByDate[d]; ok {
			dates = append(dates, d)
		}
	}
	sort.Strings(dates)
	if len(dates) == 0 {
		return nil, na("no overlapping dates between factor panels and snapshot Weights")
	}
	points = make([]FactorExposurePoint, 0, len(dates))
	for _, d := range dates {
		p := panelByDate[d]
		w := weightByDate[d]
		pt := FactorExposurePoint{
			TradeDate: d,
			Factors:   map[string]float64{},
		}
		if exp, ok := weightedExposure(w, p.Composite); ok {
			v := exp
			pt.Composite = &v
		}
		if exp, ok := weightedExposure(w, p.Quality); ok {
			pt.Factors["quality"] = exp
		}
		if exp, ok := weightedExposure(w, p.Value); ok {
			pt.Factors["value"] = exp
		}
		if exp, ok := weightedExposure(w, p.Momentum); ok {
			pt.Factors["momentum"] = exp
		}
		points = append(points, pt)
	}
	return points, avail(float64(len(points)))
}

func weightedExposure(weights map[types.SecurityID]float64, scores map[types.SecurityID]float64) (float64, bool) {
	if len(scores) == 0 || len(weights) == 0 {
		return 0, false
	}
	sumW, sum := 0.0, 0.0
	for code, wt := range weights {
		if wt == 0 {
			continue
		}
		sc, ok := scores[code]
		if !ok {
			continue
		}
		sumW += wt
		sum += wt * sc
	}
	if sumW <= 0 {
		return 0, false
	}
	return sum / sumW, true
}

// ComputeBrinsonAttribution sums Allocation/Selection/Interaction over industries for each period, then averages.
func ComputeBrinsonAttribution(periods [][]IndustryPeriodReturn) AttributionResult {
	out := AttributionResult{Method: BrinsonMethodNote}
	if len(periods) == 0 {
		out.Note = "Brinson attribution N/A: no industry weight/return periods provided"
		out.Allocation = na(out.Note)
		out.Selection = na(out.Note)
		out.Interaction = na(out.Note)
		return out
	}
	var sumA, sumS, sumI float64
	nInd := 0
	nPer := 0
	for _, per := range periods {
		if len(per) == 0 {
			continue
		}
		var a, s, inter float64
		for _, row := range per {
			dw := row.PortWeight - row.BenchWeight
			dr := row.PortReturn - row.BenchReturn
			a += dw * row.BenchReturn
			s += row.BenchWeight * dr
			inter += dw * dr
			nInd++
		}
		sumA += a
		sumS += s
		sumI += inter
		nPer++
	}
	if nPer == 0 {
		out.Note = "Brinson attribution N/A: empty industry periods"
		out.Allocation = na(out.Note)
		out.Selection = na(out.Note)
		out.Interaction = na(out.Note)
		return out
	}
	out.Available = true
	out.NIndustries = nInd / nPer
	out.Allocation = avail(sumA / float64(nPer))
	out.Selection = avail(sumS / float64(nPer))
	out.Interaction = avail(sumI / float64(nPer))
	out.Note = fmt.Sprintf("averaged over %d period(s); method documented in method field", nPer)
	return out
}

// ComputeFactorLayers builds quantile and long-short mean forward returns per factor.
func ComputeFactorLayers(panels []DailyFactorPanel, nQ int) []FactorLayerResult {
	if nQ <= 0 {
		nQ = 5
	}
	method := FactorLayerMethodNote
	if len(panels) == 0 {
		return []FactorLayerResult{{
			Available: false,
			Method:    method,
			Note:      "factor long-short / layer returns N/A: no DailyFactorPanel provided",
		}}
	}
	hasFwd := false
	for _, p := range panels {
		if len(p.ForwardRet) > 0 {
			hasFwd = true
			break
		}
	}
	if !hasFwd {
		return []FactorLayerResult{{
			Available: false,
			Method:    method,
			Note:      "factor long-short / layer returns N/A: panels lack ForwardRet (refuse to invent forward returns)",
		}}
	}

	type factorSpec struct {
		name string
		get  func(DailyFactorPanel) map[types.SecurityID]float64
	}
	specs := []factorSpec{
		{"quality", func(p DailyFactorPanel) map[types.SecurityID]float64 { return p.Quality }},
		{"value", func(p DailyFactorPanel) map[types.SecurityID]float64 { return p.Value }},
		{"momentum", func(p DailyFactorPanel) map[types.SecurityID]float64 { return p.Momentum }},
		{"composite", func(p DailyFactorPanel) map[types.SecurityID]float64 { return p.Composite }},
	}
	out := make([]FactorLayerResult, 0, len(specs))
	for _, sp := range specs {
		layerRets := make([][]float64, nQ) // per quantile, daily means
		for _, p := range panels {
			scores := sp.get(p)
			if len(scores) == 0 || len(p.ForwardRet) == 0 {
				continue
			}
			type pair struct {
				code types.SecurityID
				sc   float64
				fr   float64
			}
			pairs := make([]pair, 0, len(scores))
			for code, sc := range scores {
				fr, ok := p.ForwardRet[code]
				if !ok {
					continue
				}
				pairs = append(pairs, pair{code, sc, fr})
			}
			if len(pairs) < nQ {
				continue
			}
			sort.Slice(pairs, func(i, j int) bool { return pairs[i].sc < pairs[j].sc })
			// Assign to quantiles by rank index.
			for qi := 0; qi < nQ; qi++ {
				lo := qi * len(pairs) / nQ
				hi := (qi + 1) * len(pairs) / nQ
				if hi <= lo {
					continue
				}
				sum := 0.0
				for _, x := range pairs[lo:hi] {
					sum += x.fr
				}
				layerRets[qi] = append(layerRets[qi], sum/float64(hi-lo))
			}
		}
		layers := make([]LayerReturn, 0, nQ+1)
		any := false
		for qi := 0; qi < nQ; qi++ {
			lr := LayerReturn{Layer: "Q" + strconv.Itoa(qi+1), NDays: len(layerRets[qi])}
			if len(layerRets[qi]) > 0 {
				lr.MeanReturn = avail(mean(layerRets[qi]))
				any = true
			} else {
				lr.MeanReturn = na("no days with scores+forward returns for this quantile")
			}
			layers = append(layers, lr)
		}
		// Long-short = top (Qn) − bottom (Q1)
		ls := LayerReturn{Layer: "long_short"}
		if len(layerRets[0]) > 0 && len(layerRets[nQ-1]) > 0 {
			// Align by using overall means (not day-matched) — documented limitation of skeleton.
			ls.MeanReturn = avail(mean(layerRets[nQ-1]) - mean(layerRets[0]))
			ls.NDays = int(math.Min(float64(len(layerRets[0])), float64(len(layerRets[nQ-1]))))
			any = true
		} else {
			ls.MeanReturn = na("need both bottom and top quantile days")
		}
		layers = append(layers, ls)

		fr := FactorLayerResult{
			Available: any,
			Method:    method,
			Factor:    sp.name,
			NQuantile: nQ,
			Layers:    layers,
		}
		if !any {
			fr.Note = "no overlapping score+forward-return days for factor " + sp.name
		}
		out = append(out, fr)
	}
	return out
}

// ComputeUnfilledSkipRate derives skip rate from engine counters when present.
func ComputeUnfilledSkipRate(c *ExecutionSkipCounts) OptionalFloat {
	if c == nil {
		return na("涨跌停/停牌未成交 skip rate N/A: ExecutionSkipCounts not provided to report (CLI should pass Engine.SkipCounts after ApplyTargets)")
	}
	intended := c.IntendedBuyOrders + c.IntendedSellOrders
	if intended <= 0 {
		return na("skip counts present but intended order total is 0")
	}
	skipped := c.SkippedLimitBuy + c.SkippedLimitSell + c.SkippedSuspend
	return avail(float64(skipped) / float64(intended))
}

// ComputeLlmCoverage fills LLM coverage/degrade stats only when counters exist.
func ComputeLlmCoverage(c *LlmRunCounters) LlmCoverageStats {
	if c == nil {
		return LlmCoverageStats{
			Available:    false,
			Note:         "LLM coverage/degrade N/A: LLM path disabled or counters not wired",
			CoverageRate: na("no LLM counters"),
			DegradeCount: na("no LLM counters"),
			AnnotateCalls: na("no LLM counters"),
		}
	}
	out := LlmCoverageStats{
		Available:     true,
		AnnotateCalls: avail(float64(c.AnnotateCalls)),
		DegradeCount:  avail(float64(c.DegradeCount)),
	}
	if c.CandidateTotal > 0 {
		out.CoverageRate = avail(float64(c.FeaturesEmitted) / float64(c.CandidateTotal))
	} else {
		out.CoverageRate = na("candidate_total is 0")
	}
	return out
}

// emptyAttribution is the default N/A Brinson skeleton.
func emptyAttribution() AttributionResult {
	return ComputeBrinsonAttribution(nil)
}

// emptyLlmStats is the default N/A LLM stats.
func emptyLlmStats() LlmCoverageStats {
	return ComputeLlmCoverage(nil)
}

// emptyFactorLayers is a single N/A stub entry.
func emptyFactorLayers() []FactorLayerResult {
	return ComputeFactorLayers(nil, 5)
}

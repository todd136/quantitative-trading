package backtest

import (
	"math"
	"sort"
	"time"

	"quantitative-trading/internal/types"
)

// SpecVersion is the frozen rule-book version stamped into reports.
const SpecVersion = "v1.2-20260928"

// TradingDaysPerYear is the annualization convention (A-share approx).
const TradingDaysPerYear = 252

// OptionalFloat is a metric that may be unavailable. When Available=false the
// Value must be ignored — never treat a zero as a real result.
type OptionalFloat struct {
	Available bool    `json:"available"`
	Value     float64 `json:"value,omitempty"`
	Note      string  `json:"note,omitempty"`
}

func avail(v float64) OptionalFloat { return OptionalFloat{Available: true, Value: v} }

func na(note string) OptionalFloat { return OptionalFloat{Available: false, Note: note} }

// Metrics holds §6.2 performance stats computed from a real equity curve.
// Never hardcode fake return/Sharpe/drawdown — only derive from snapshots/fills.
type Metrics struct {
	HasCurve bool `json:"has_curve"`
	NDays    int  `json:"n_days"`
	NReturns int  `json:"n_returns"`

	// Absolute performance (from NAV when HasCurve)
	TotalReturn       OptionalFloat `json:"total_return"`
	AnnReturn         OptionalFloat `json:"ann_return"` // (1+R)^(252/n)-1
	AnnVol            OptionalFloat `json:"ann_vol"`
	Sharpe            OptionalFloat `json:"sharpe"` // risk_free annual; default 0
	MaxDrawdown       OptionalFloat `json:"max_drawdown"`
	Calmar            OptionalFloat `json:"calmar"` // ann_return / |maxDD|; N/A if maxDD==0
	DailyWinRate      OptionalFloat `json:"daily_win_rate"`
	MonthlyWinRate    OptionalFloat `json:"monthly_win_rate"` // N/A if <2 calendar months

	// Turnover: DailySnapshot.Turnover treated as two-way (buy+sell notional / NAV).
	// OneWay = TwoWay / 2. Annualized = daily avg × 252.
	TurnoverDailyAvgTwoWay   OptionalFloat `json:"turnover_daily_avg_two_way"`
	TurnoverDailyAvgOneWay   OptionalFloat `json:"turnover_daily_avg_one_way"`
	TurnoverAnnTwoWay        OptionalFloat `json:"turnover_ann_two_way"`
	TurnoverAnnOneWay        OptionalFloat `json:"turnover_ann_one_way"`
	CostRatio                OptionalFloat `json:"cost_ratio"` // sum CostTotal / starting NAV

	// Relative to benchmark (optional IndexBars)
	ExcessReturn    OptionalFloat `json:"excess_return"`
	TrackingError   OptionalFloat `json:"tracking_error"`
	InformationRatio OptionalFloat `json:"information_ratio"`
	ExcessMaxDD     OptionalFloat `json:"excess_max_drawdown"`

	// Portfolio structure (from snaps when Weights/Holdings present)
	AvgHoldingsCount   OptionalFloat `json:"avg_holdings_count"`
	MaxSingleNameWeight OptionalFloat `json:"max_single_name_weight"`
	UnfilledSkipRate   OptionalFloat `json:"unfilled_skip_rate"` // N/A until engine tracks skips

	// Deferred §6.2 stubs (always N/A until implemented)
	BrinsonAttribution OptionalFloat `json:"brinson_attribution"`
	FactorLongShort    OptionalFloat `json:"factor_long_short"`
	IndustryDistTS     OptionalFloat `json:"industry_distribution_ts"`

	RiskFreeUsed float64 `json:"risk_free_used"`
	AnnConvention string `json:"ann_convention"` // "252"
}

// MetricsInput bundles optional inputs for ComputeMetricsFull.
type MetricsInput struct {
	Snaps     []types.DailySnapshot
	Fills     []types.Fill // optional; used only if snap Turnover/CostTotal empty
	Benchmark []types.IndexBar
	RiskFree  float64 // annual risk-free rate; default 0
}

// ComputeMetrics is the legacy thin wrapper (absolute curve only).
func ComputeMetrics(snaps []types.DailySnapshot, riskFree float64) Metrics {
	return ComputeMetricsFull(MetricsInput{Snaps: snaps, RiskFree: riskFree})
}

// ComputeMetricsFull derives §6.2 metrics. Missing inputs → Available=false, never fake zeros-as-results.
func ComputeMetricsFull(in MetricsInput) Metrics {
	m := Metrics{
		RiskFreeUsed:  in.RiskFree,
		AnnConvention: "252",
		BrinsonAttribution: na("TODO §6.2: Brinson attribution not implemented"),
		FactorLongShort:    na("TODO §6.2: factor long-short / layer returns not implemented"),
		IndustryDistTS:     na("TODO §6.2: industry distribution time series not implemented"),
		UnfilledSkipRate:   na("engine does not yet expose limit/suspend skip counts"),
	}
	snaps := in.Snaps
	if len(snaps) < 2 {
		return m
	}
	m.HasCurve = true
	m.NDays = len(snaps)
	m.NReturns = len(snaps) - 1

	first := snaps[0].NAV
	last := snaps[len(snaps)-1].NAV
	var totalRet float64
	if first > 0 {
		totalRet = last/first - 1
		m.TotalReturn = avail(totalRet)
		// Annualize with 252 convention over number of daily return periods.
		n := float64(m.NReturns)
		if n > 0 && first > 0 && last > 0 {
			m.AnnReturn = avail(math.Pow(last/first, TradingDaysPerYear/n) - 1)
		} else {
			m.AnnReturn = na("cannot annualize")
		}
	} else {
		m.TotalReturn = na("starting NAV <= 0")
		m.AnnReturn = na("starting NAV <= 0")
	}

	rets := make([]float64, 0, len(snaps)-1)
	peak := snaps[0].NAV
	maxDD := 0.0
	posDays := 0
	for i := 1; i < len(snaps); i++ {
		prev, cur := snaps[i-1].NAV, snaps[i].NAV
		if prev > 0 {
			r := cur/prev - 1
			rets = append(rets, r)
			if r > 0 {
				posDays++
			}
		}
		if cur > peak {
			peak = cur
		}
		if peak > 0 {
			dd := cur/peak - 1
			if dd < maxDD {
				maxDD = dd
			}
		}
	}
	m.MaxDrawdown = avail(maxDD)
	if len(rets) > 0 {
		m.DailyWinRate = avail(float64(posDays) / float64(len(rets)))
	}

	if len(rets) > 1 {
		mu := mean(rets)
		sd := sampleStd(rets)
		m.AnnVol = avail(sd * math.Sqrt(TradingDaysPerYear))
		if sd > 0 {
			dailyRF := in.RiskFree / TradingDaysPerYear
			m.Sharpe = avail(((mu - dailyRF) / sd) * math.Sqrt(TradingDaysPerYear))
		} else {
			m.Sharpe = na("zero return volatility")
		}
	} else {
		m.AnnVol = na("need >=2 daily returns for vol/Sharpe")
		m.Sharpe = na("need >=2 daily returns for vol/Sharpe")
	}

	// Calmar = ann_return / |maxDD|
	if m.AnnReturn.Available && maxDD != 0 {
		m.Calmar = avail(m.AnnReturn.Value / math.Abs(maxDD))
	} else if maxDD == 0 {
		m.Calmar = na("max drawdown is 0")
	} else {
		m.Calmar = na("ann_return unavailable")
	}

	m.MonthlyWinRate = monthlyWinRate(snaps)

	fillTurnoverCost(snaps, in.Fills, &m, first)
	fillStructure(snaps, &m)
	fillBenchmark(snaps, in.Benchmark, in.RiskFree, &m)

	return m
}

func monthlyWinRate(snaps []types.DailySnapshot) OptionalFloat {
	// Aggregate NAV to calendar month-end (last snap in each YYYY-MM).
	type monthKey struct{ y int; m time.Month }
	order := make([]monthKey, 0)
	lastNAV := map[monthKey]float64{}
	seen := map[monthKey]bool{}
	for _, s := range snaps {
		tm := s.TradeDate.Time()
		k := monthKey{tm.Year(), tm.Month()}
		if !seen[k] {
			order = append(order, k)
			seen[k] = true
		}
		lastNAV[k] = s.NAV
	}
	if len(order) < 2 {
		return na("need >=2 calendar months of NAV")
	}
	wins := 0
	n := 0
	for i := 1; i < len(order); i++ {
		prev, cur := lastNAV[order[i-1]], lastNAV[order[i]]
		if prev <= 0 {
			continue
		}
		n++
		if cur/prev-1 > 0 {
			wins++
		}
	}
	if n == 0 {
		return na("no valid monthly returns")
	}
	return avail(float64(wins) / float64(n))
}

func fillTurnoverCost(snaps []types.DailySnapshot, fills []types.Fill, m *Metrics, startNAV float64) {
	// Prefer snapshot Turnover/CostTotal when any day is populated.
	hasTO := false
	hasCost := false
	sumTO := 0.0
	sumCost := 0.0
	for _, s := range snaps {
		if s.Turnover != 0 {
			hasTO = true
		}
		sumTO += s.Turnover
		if s.CostTotal != 0 {
			hasCost = true
		}
		sumCost += s.CostTotal
	}
	// If snaps lack activity fields, derive from fills (two-way = sum amount / day NAV).
	if !hasTO && len(fills) > 0 {
		byDate := map[string]float64{}
		for _, f := range fills {
			byDate[f.TradeDate.String()] += f.Amount // buy+sell notionals = two-way base
		}
		navByDate := map[string]float64{}
		for _, s := range snaps {
			navByDate[s.TradeDate.String()] = s.NAV
		}
		sumTO = 0
		nWith := 0
		for d, amt := range byDate {
			nav := navByDate[d]
			if nav > 0 {
				sumTO += amt / nav
				nWith++
			}
		}
		if nWith > 0 {
			hasTO = true
		}
	}
	if !hasCost && len(fills) > 0 {
		sumCost = 0
		for _, f := range fills {
			sumCost += f.Cost
		}
		hasCost = sumCost != 0 || len(fills) > 0
	}

	if hasTO && len(snaps) > 0 {
		avg := sumTO / float64(len(snaps))
		m.TurnoverDailyAvgTwoWay = avail(avg)
		m.TurnoverDailyAvgOneWay = avail(avg / 2)
		m.TurnoverAnnTwoWay = avail(avg * TradingDaysPerYear)
		m.TurnoverAnnOneWay = avail(avg / 2 * TradingDaysPerYear)
	} else {
		note := "Turnover not populated on snapshots and no fills provided"
		m.TurnoverDailyAvgTwoWay = na(note)
		m.TurnoverDailyAvgOneWay = na(note)
		m.TurnoverAnnTwoWay = na(note)
		m.TurnoverAnnOneWay = na(note)
	}

	if hasCost && startNAV > 0 {
		m.CostRatio = avail(sumCost / startNAV)
	} else if !hasCost {
		m.CostRatio = na("CostTotal not on snapshots and no fills")
	} else {
		m.CostRatio = na("starting NAV <= 0")
	}
}

func fillStructure(snaps []types.DailySnapshot, m *Metrics) {
	var sumCount float64
	nCount := 0
	maxW := 0.0
	hasW := false
	for _, s := range snaps {
		if len(s.Holdings) > 0 {
			c := 0
			for _, sh := range s.Holdings {
				if sh > 0 {
					c++
				}
			}
			sumCount += float64(c)
			nCount++
		}
		for _, w := range s.Weights {
			hasW = true
			if w > maxW {
				maxW = w
			}
		}
	}
	if nCount > 0 {
		m.AvgHoldingsCount = avail(sumCount / float64(nCount))
	} else {
		m.AvgHoldingsCount = na("Holdings not present on snapshots")
	}
	if hasW {
		m.MaxSingleNameWeight = avail(maxW)
	} else {
		m.MaxSingleNameWeight = na("Weights not present on snapshots")
	}
}

func fillBenchmark(snaps []types.DailySnapshot, bench []types.IndexBar, riskFree float64, m *Metrics) {
	_ = riskFree
	if len(bench) < 2 {
		note := "benchmark series not provided or too short"
		m.ExcessReturn = na(note)
		m.TrackingError = na(note)
		m.InformationRatio = na(note)
		m.ExcessMaxDD = na(note)
		return
	}
	bClose := map[string]float64{}
	for _, b := range bench {
		bClose[b.TradeDate.String()] = b.Close
	}
	// Align: for each consecutive snap pair, need both dates in benchmark.
	var excess []float64
	var alignedPort []float64 // cumulative for excess DD: start 1.0
	cumEx := 1.0
	alignedPort = append(alignedPort, 1.0)
	for i := 1; i < len(snaps); i++ {
		d0, d1 := snaps[i-1].TradeDate.String(), snaps[i].TradeDate.String()
		b0, ok0 := bClose[d0]
		b1, ok1 := bClose[d1]
		p0, p1 := snaps[i-1].NAV, snaps[i].NAV
		if !ok0 || !ok1 || b0 <= 0 || p0 <= 0 {
			continue
		}
		pr := p1/p0 - 1
		br := b1/b0 - 1
		ex := pr - br
		excess = append(excess, ex)
		cumEx *= 1 + ex
		alignedPort = append(alignedPort, cumEx)
	}
	if len(excess) == 0 {
		note := "no overlapping dates between portfolio and benchmark"
		m.ExcessReturn = na(note)
		m.TrackingError = na(note)
		m.InformationRatio = na(note)
		m.ExcessMaxDD = na(note)
		return
	}
	// Total excess over aligned window: final cum - 1
	m.ExcessReturn = avail(alignedPort[len(alignedPort)-1] - 1)

	peak := alignedPort[0]
	maxDD := 0.0
	for _, v := range alignedPort {
		if v > peak {
			peak = v
		}
		if peak > 0 {
			dd := v/peak - 1
			if dd < maxDD {
				maxDD = dd
			}
		}
	}
	m.ExcessMaxDD = avail(maxDD)

	if len(excess) > 1 {
		sd := sampleStd(excess)
		m.TrackingError = avail(sd * math.Sqrt(TradingDaysPerYear))
		mu := mean(excess)
		if sd > 0 {
			m.InformationRatio = avail((mu / sd) * math.Sqrt(TradingDaysPerYear))
		} else {
			m.InformationRatio = na("zero excess volatility")
		}
	} else {
		m.TrackingError = na("need >=2 aligned excess returns")
		m.InformationRatio = na("need >=2 aligned excess returns")
	}
}

// AnnotateDailyActivity sets Turnover (two-way = fill notionals / NAV) and CostTotal
// from fills matching each snapshot date. Safe to call in-place.
func AnnotateDailyActivity(snaps []types.DailySnapshot, fills []types.Fill) {
	type agg struct{ amt, cost float64 }
	byDate := map[string]agg{}
	for _, f := range fills {
		a := byDate[f.TradeDate.String()]
		a.amt += f.Amount
		a.cost += f.Cost
		byDate[f.TradeDate.String()] = a
	}
	for i := range snaps {
		a := byDate[snaps[i].TradeDate.String()]
		snaps[i].CostTotal = a.cost
		if snaps[i].NAV > 0 {
			snaps[i].Turnover = a.amt / snaps[i].NAV
		}
	}
}

func mean(xs []float64) float64 {
	s := 0.0
	for _, v := range xs {
		s += v
	}
	return s / float64(len(xs))
}

func sampleStd(xs []float64) float64 {
	if len(xs) < 2 {
		return 0
	}
	mu := mean(xs)
	ss := 0.0
	for _, v := range xs {
		d := v - mu
		ss += d * d
	}
	return math.Sqrt(ss / float64(len(xs)-1))
}

// SortIndexBars sorts by trade date ascending (helper for callers).
func SortIndexBars(bars []types.IndexBar) {
	sort.Slice(bars, func(i, j int) bool {
		return bars[i].TradeDate.Before(bars[j].TradeDate)
	})
}

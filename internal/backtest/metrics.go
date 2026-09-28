package backtest

import (
	"math"

	"quantitative-trading/internal/types"
)

// Metrics holds performance stats computed from a real equity curve.
// Never hardcode fake return/Sharpe/drawdown — only derive from snapshots.
type Metrics struct {
	HasCurve       bool
	NDays          int
	TotalReturn    float64 // NAV_last/NAV_first - 1; 0 if insufficient
	AnnVol         float64
	Sharpe         float64 // risk_free from cfg applied by caller
	MaxDrawdown    float64
	// TODO(§6.2): Calmar, win rates, turnover, benchmark-relative, Brinson attribution
}

// ComputeMetrics derives metrics from daily NAV snapshots. Returns zero-value with
// HasCurve=false if fewer than 2 points — does not invent numbers.
func ComputeMetrics(snaps []types.DailySnapshot, riskFree float64) Metrics {
	m := Metrics{}
	if len(snaps) < 2 {
		return m
	}
	m.HasCurve = true
	m.NDays = len(snaps)
	first := snaps[0].NAV
	last := snaps[len(snaps)-1].NAV
	if first > 0 {
		m.TotalReturn = last/first - 1
	}
	rets := make([]float64, 0, len(snaps)-1)
	peak := snaps[0].NAV
	maxDD := 0.0
	for i := 1; i < len(snaps); i++ {
		prev, cur := snaps[i-1].NAV, snaps[i].NAV
		if prev > 0 {
			rets = append(rets, cur/prev-1)
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
	m.MaxDrawdown = maxDD
	if len(rets) > 1 {
		mu := mean(rets)
		sd := sampleStd(rets)
		m.AnnVol = sd * math.Sqrt(252)
		if sd > 0 {
			dailyRF := riskFree / 252
			m.Sharpe = ((mu - dailyRF) / sd) * math.Sqrt(252)
		}
	}
	return m
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

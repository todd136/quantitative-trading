package backtest

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"quantitative-trading/internal/config"
	"quantitative-trading/internal/types"
)

// SegmentReport is metrics for one sample-split segment.
type SegmentReport struct {
	Name       SegmentName `json:"name"`
	IsFormal   bool        `json:"is_formal"`
	StartDate  string      `json:"start_date,omitempty"`
	EndDate    string      `json:"end_date,omitempty"`
	NSnapshots int         `json:"n_snapshots"`
	Metrics    Metrics     `json:"metrics"`
}

// Report is the full backtest metrics export (§6.1 / §6.2).
type Report struct {
	SpecVersion       string                    `json:"spec_version"`
	GeneratedAt       string                    `json:"generated_at"` // Asia/Shanghai
	FormalSegment     SegmentName               `json:"formal_segment"`
	FreezeNote        string                    `json:"freeze_note"`
	WalkForwardNote   string                    `json:"walk_forward_note,omitempty"`
	ConfigFreezeYAML  string                    `json:"config_freeze_yaml"`
	ConfigVersion     string                    `json:"config_version"`
	Benchmark         string                    `json:"benchmark,omitempty"`
	RiskFree          float64                   `json:"risk_free"`
	AnnConvention     string                    `json:"ann_convention"`
	TurnoverAssumption string                   `json:"turnover_assumption"`
	NAPolicy          string                    `json:"na_policy"`
	Segments          map[string]SegmentReport  `json:"segments"`
	SegmentOrder      []string                  `json:"segment_order"`
}

const turnoverAssumption = `DailySnapshot.Turnover is two-way (sum of buy+sell fill notionals / day NAV). One-way = two-way/2. Annualized = daily average × 252. When Turnover is unset on snaps, metrics derive the same definition from fills if provided.`

const naPolicy = `Metrics that cannot be computed (missing benchmark, <2 months, no fills/turnover, maxDD=0 for Calmar, etc.) are emitted with available=false and an explanatory note — never fabricated zeros presented as results.`

// BuildReport assembles a Report from snaps/fills/optional benchmark and config.
func BuildReport(cfg config.Config, snaps []types.DailySnapshot, fills []types.Fill, bench []types.IndexBar) (Report, error) {
	split, err := ParseSampleSplit(cfg.Backtest)
	if err != nil {
		return Report{}, err
	}
	loc := time.FixedZone("CST", 8*3600)
	freezeYAML, _ := yaml.Marshal(cfg)

	r := Report{
		SpecVersion:        SpecVersion,
		GeneratedAt:        time.Now().In(loc).Format(time.RFC3339),
		FreezeNote:         FreezeParamsNotice,
		WalkForwardNote:    WalkForwardSkeleton(cfg.Backtest.WalkForward),
		ConfigFreezeYAML:   string(freezeYAML),
		ConfigVersion:      cfg.Version,
		Benchmark:          cfg.Backtest.Benchmark,
		RiskFree:           cfg.Backtest.RiskFree,
		AnnConvention:      "252",
		TurnoverAssumption: turnoverAssumption,
		NAPolicy:           naPolicy,
		Segments:           map[string]SegmentReport{},
	}

	formal := SegmentFull
	order := make([]string, 0, len(split.Segments))
	for _, b := range split.Segments {
		segSnaps := FilterSnaps(snaps, b)
		segFills := FilterFills(fills, b)
		// Restrict benchmark to segment date span when possible
		segBench := filterBench(bench, segSnaps)
		m := ComputeMetricsFull(MetricsInput{
			Snaps:     segSnaps,
			Fills:     segFills,
			Benchmark: segBench,
			RiskFree:  cfg.Backtest.RiskFree,
		})
		sr := SegmentReport{
			Name:       b.Name,
			IsFormal:   b.IsFormal,
			NSnapshots: len(segSnaps),
			Metrics:    m,
		}
		if len(segSnaps) > 0 {
			sr.StartDate = segSnaps[0].TradeDate.String()
			sr.EndDate = segSnaps[len(segSnaps)-1].TradeDate.String()
		}
		key := string(b.Name)
		r.Segments[key] = sr
		order = append(order, key)
		if b.IsFormal {
			formal = b.Name
		}
	}
	r.FormalSegment = formal
	r.SegmentOrder = order
	return r, nil
}

func filterBench(bench []types.IndexBar, snaps []types.DailySnapshot) []types.IndexBar {
	if len(bench) == 0 || len(snaps) == 0 {
		return bench
	}
	lo := snaps[0].TradeDate
	hi := snaps[len(snaps)-1].TradeDate
	out := make([]types.IndexBar, 0, len(bench))
	for _, b := range bench {
		if b.TradeDate.Before(lo) || b.TradeDate.After(hi) {
			continue
		}
		out = append(out, b)
	}
	return out
}

// WriteReportJSON writes report.json.
func WriteReportJSON(outDir string, r Report) (string, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(outDir, "report.json")
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, data, 0o644)
}

// WriteMetricsJSON is an alias writing metrics-oriented JSON (same report).
func WriteMetricsJSON(path string, r Report) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// WriteMetricsCSV flattens per-segment optional floats into metrics.csv.
func WriteMetricsCSV(outDir string, r Report) (string, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(outDir, "metrics.csv")
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	_ = w.Write([]string{"segment", "is_formal", "metric", "available", "value", "note"})

	keys := append([]string{}, r.SegmentOrder...)
	if len(keys) == 0 {
		for k := range r.Segments {
			keys = append(keys, k)
		}
		sort.Strings(keys)
	}
	for _, seg := range keys {
		sr := r.Segments[seg]
		rows := metricRows(sr.Metrics)
		for _, row := range rows {
			_ = w.Write([]string{
				seg,
				strconv.FormatBool(sr.IsFormal),
				row.name,
				strconv.FormatBool(row.f.Available),
				formatOpt(row.f),
				row.f.Note,
			})
		}
	}
	w.Flush()
	return path, w.Error()
}

type namedOpt struct {
	name string
	f    OptionalFloat
}

func metricRows(m Metrics) []namedOpt {
	return []namedOpt{
		{"total_return", m.TotalReturn},
		{"ann_return", m.AnnReturn},
		{"ann_vol", m.AnnVol},
		{"sharpe", m.Sharpe},
		{"max_drawdown", m.MaxDrawdown},
		{"calmar", m.Calmar},
		{"daily_win_rate", m.DailyWinRate},
		{"monthly_win_rate", m.MonthlyWinRate},
		{"turnover_daily_avg_two_way", m.TurnoverDailyAvgTwoWay},
		{"turnover_daily_avg_one_way", m.TurnoverDailyAvgOneWay},
		{"turnover_ann_two_way", m.TurnoverAnnTwoWay},
		{"turnover_ann_one_way", m.TurnoverAnnOneWay},
		{"cost_ratio", m.CostRatio},
		{"excess_return", m.ExcessReturn},
		{"tracking_error", m.TrackingError},
		{"information_ratio", m.InformationRatio},
		{"excess_max_drawdown", m.ExcessMaxDD},
		{"avg_holdings_count", m.AvgHoldingsCount},
		{"max_single_name_weight", m.MaxSingleNameWeight},
		{"unfilled_skip_rate", m.UnfilledSkipRate},
		{"brinson_attribution", m.BrinsonAttribution},
		{"factor_long_short", m.FactorLongShort},
		{"industry_distribution_ts", m.IndustryDistTS},
	}
}

func formatOpt(f OptionalFloat) string {
	if !f.Available {
		return ""
	}
	return strconv.FormatFloat(f.Value, 'f', 8, 64)
}

// WriteReportMD writes a human-readable report.md with spec version + param freeze.
// Does not invent performance numbers — only prints computed Available metrics.
func WriteReportMD(outDir string, r Report) (string, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(outDir, "report.md")
	var b strings.Builder
	b.WriteString("# Backtest report\n\n")
	b.WriteString(fmt.Sprintf("- **Spec version**: `%s`\n", r.SpecVersion))
	b.WriteString(fmt.Sprintf("- **Config version**: `%s`\n", r.ConfigVersion))
	b.WriteString(fmt.Sprintf("- **Generated at**: %s (Asia/Shanghai)\n", r.GeneratedAt))
	b.WriteString(fmt.Sprintf("- **Formal segment**: `%s`\n", r.FormalSegment))
	b.WriteString(fmt.Sprintf("- **Benchmark**: `%s`\n", r.Benchmark))
	b.WriteString(fmt.Sprintf("- **Risk-free (annual)**: %g\n", r.RiskFree))
	b.WriteString(fmt.Sprintf("- **Annualization**: %s trading days/year\n", r.AnnConvention))
	b.WriteString("\n## Parameter freeze\n\n")
	b.WriteString(r.FreezeNote + "\n")
	if r.WalkForwardNote != "" {
		b.WriteString("\n")
		b.WriteString(r.WalkForwardNote + "\n")
	}
	b.WriteString("\n## N/A policy\n\n")
	b.WriteString(r.NAPolicy + "\n")
	b.WriteString("\n## Turnover assumption\n\n")
	b.WriteString(r.TurnoverAssumption + "\n")
	b.WriteString("\n## Frozen config dump\n\n```yaml\n")
	b.WriteString(r.ConfigFreezeYAML)
	b.WriteString("```\n")

	keys := append([]string{}, r.SegmentOrder...)
	for _, seg := range keys {
		sr := r.Segments[seg]
		formal := ""
		if sr.IsFormal {
			formal = " **(formal / OOS)**"
		}
		b.WriteString(fmt.Sprintf("\n## Segment: `%s`%s\n\n", seg, formal))
		b.WriteString(fmt.Sprintf("- Snapshots: %d\n", sr.NSnapshots))
		if sr.StartDate != "" {
			b.WriteString(fmt.Sprintf("- Date range: %s → %s\n", sr.StartDate, sr.EndDate))
		}
		b.WriteString(fmt.Sprintf("- HasCurve: %v\n\n", sr.Metrics.HasCurve))
		b.WriteString("| Metric | Available | Value | Note |\n|--------|-----------|-------|------|\n")
		for _, row := range metricRows(sr.Metrics) {
			val := "N/A"
			if row.f.Available {
				val = strconv.FormatFloat(row.f.Value, 'f', 6, 64)
			}
			note := strings.ReplaceAll(row.f.Note, "|", "/")
			b.WriteString(fmt.Sprintf("| %s | %v | %s | %s |\n", row.name, row.f.Available, val, note))
		}
	}
	b.WriteString("\n---\n*Not investment advice. Values above are computed from the run's equity/turnover curves only.*\n")
	return path, os.WriteFile(path, []byte(b.String()), 0o644)
}

// WriteAllReports writes report.json, metrics.csv, and report.md under outDir.
func WriteAllReports(outDir string, r Report) (jsonPath, csvPath, mdPath string, err error) {
	jsonPath, err = WriteReportJSON(outDir, r)
	if err != nil {
		return
	}
	csvPath, err = WriteMetricsCSV(outDir, r)
	if err != nil {
		return
	}
	mdPath, err = WriteReportMD(outDir, r)
	return
}

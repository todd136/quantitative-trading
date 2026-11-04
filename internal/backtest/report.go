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

	// §6.2 extended skeletons (honest N/A when inputs missing)
	Yearly               []YearlyMetrics       `json:"yearly,omitempty"`
	IndustryDistribution []IndustryWeightPoint `json:"industry_distribution,omitempty"`
	FactorExposure       []FactorExposurePoint `json:"factor_exposure,omitempty"`
	Attribution          AttributionResult     `json:"attribution"`
	FactorLayers         []FactorLayerResult   `json:"factor_layers,omitempty"`
	LlmStats             LlmCoverageStats      `json:"llm_stats"`
}

// Report is the full backtest metrics export (§6.1 / §6.2).
type Report struct {
	SpecVersion        string                   `json:"spec_version"`
	GeneratedAt        string                   `json:"generated_at"` // Asia/Shanghai
	FormalSegment      SegmentName              `json:"formal_segment"`
	FreezeNote         string                   `json:"freeze_note"`
	WalkForwardNote    string                   `json:"walk_forward_note,omitempty"`
	ConfigFreezeYAML   string                   `json:"config_freeze_yaml"`
	ConfigVersion      string                   `json:"config_version"`
	Benchmark          string                   `json:"benchmark,omitempty"`
	RiskFree           float64                  `json:"risk_free"`
	AnnConvention      string                   `json:"ann_convention"`
	TurnoverAssumption string                   `json:"turnover_assumption"`
	NAPolicy           string                   `json:"na_policy"`
	BrinsonMethod      string                   `json:"brinson_method"`
	FactorLayerMethod  string                   `json:"factor_layer_method"`
	Segments           map[string]SegmentReport `json:"segments"`
	SegmentOrder       []string                 `json:"segment_order"`
}

const turnoverAssumption = `DailySnapshot.Turnover is two-way (sum of buy+sell fill notionals / day NAV). One-way = two-way/2. Annualized = daily average × 252. When Turnover is unset on snaps, metrics derive the same definition from fills if provided.`

const naPolicy = `Metrics that cannot be computed (missing benchmark, <2 months, no fills/turnover, maxDD=0 for Calmar, missing industry/factor panels, etc.) are emitted with available=false and an explanatory note — never fabricated zeros presented as results.`

// BuildReport assembles a Report from snaps/fills/optional benchmark and config (no extras).
func BuildReport(cfg config.Config, snaps []types.DailySnapshot, fills []types.Fill, bench []types.IndexBar) (Report, error) {
	return BuildReportWithExtras(cfg, snaps, fills, bench, ReportExtras{})
}

// BuildReportWithExtras is BuildReport plus optional §6.2 inputs (industry lookup, factor panels, …).
func BuildReportWithExtras(cfg config.Config, snaps []types.DailySnapshot, fills []types.Fill, bench []types.IndexBar, extras ReportExtras) (Report, error) {
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
		BrinsonMethod:      BrinsonMethodNote,
		FactorLayerMethod:  FactorLayerMethodNote,
		Segments:           map[string]SegmentReport{},
	}

	formal := SegmentFull
	order := make([]string, 0, len(split.Segments))
	for _, b := range split.Segments {
		segSnaps := FilterSnaps(snaps, b)
		metricSnaps := FilterSnapsWithBaseline(snaps, b)
		segFills := FilterFills(fills, b)
		segBench := filterBench(bench, metricSnaps)
		min := MetricsInput{
			Snaps:     metricSnaps,
			Fills:     segFills,
			Benchmark: segBench,
			RiskFree:  cfg.Backtest.RiskFree,
		}
		m := ComputeMetricsFull(min)
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
		enrichSegment(&sr, min, extras, metricSnaps)
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

func enrichSegment(sr *SegmentReport, min MetricsInput, extras ReportExtras, segSnaps []types.DailySnapshot) {
	// 1) Per-year performance (always attempt when snaps exist)
	sr.Yearly = ComputeYearlyMetrics(segSnaps, min)

	// 2) Industry distribution TS
	indPts, indStatus := ComputeIndustryDistribution(segSnaps, extras.IndustryLookup)
	sr.IndustryDistribution = indPts
	sr.Metrics.IndustryDistTS = indStatus

	// 3) Factor exposure TS
	facPts, facStatus := ComputeFactorExposure(segSnaps, extras.FactorPanels)
	sr.FactorExposure = facPts
	// Reflect exposure availability on a dedicated OptionalFloat via FactorLongShort sibling:
	// IndustryDistTS already mirrors industry; keep FactorLongShort for layers below.
	_ = facStatus

	// 4) Skip rate
	sr.Metrics.UnfilledSkipRate = ComputeUnfilledSkipRate(extras.SkipCounts)

	// 5) Brinson (filter periods to this segment's date span)
	sr.Attribution = ComputeBrinsonAttribution(FilterAttributionPeriods(extras.AttributionPeriods, segSnaps))
	if sr.Attribution.Available {
		// Scalar summary = allocation+selection+interaction (total excess approx)
		total := sr.Attribution.Allocation.Value + sr.Attribution.Selection.Value + sr.Attribution.Interaction.Value
		sr.Metrics.BrinsonAttribution = avail(total)
		sr.Metrics.BrinsonAttribution.Note = "sum of allocation+selection+interaction; see attribution object"
	} else {
		sr.Metrics.BrinsonAttribution = na(sr.Attribution.Note)
	}

	// 6) Factor layers / long-short
	sr.FactorLayers = ComputeFactorLayers(extras.FactorPanels, extras.LayerQuantiles)
	anyLayer := false
	var lsVal float64
	lsNote := ""
	for _, fl := range sr.FactorLayers {
		if !fl.Available {
			continue
		}
		for _, lr := range fl.Layers {
			if lr.Layer == "long_short" && lr.MeanReturn.Available {
				anyLayer = true
				lsVal = lr.MeanReturn.Value
				lsNote = "long_short mean for factor " + fl.Factor + "; see factor_layers"
			}
		}
	}
	if anyLayer {
		sr.Metrics.FactorLongShort = avail(lsVal)
		sr.Metrics.FactorLongShort.Note = lsNote
	} else if len(sr.FactorLayers) > 0 {
		sr.Metrics.FactorLongShort = na(sr.FactorLayers[0].Note)
	}

	// Mark factor exposure status on IndustryDistTS sibling is awkward; stash note on FactorLongShort
	// when layers N/A but exposure exists — prefer a clear Metrics flag via IndustryDistTS pattern:
	if facStatus.Available && !anyLayer {
		// Keep FactorLongShort as layer note; exposure series is in FactorExposure field.
		if !sr.Metrics.FactorLongShort.Available {
			sr.Metrics.FactorLongShort = na("factor exposure series available (" + strconv.Itoa(len(facPts)) + " days); long-short/layers still N/A without ForwardRet")
		}
	} else if !facStatus.Available && !sr.Metrics.FactorLongShort.Available {
		// leave note from layers
	}

	// 8) LLM
	sr.LlmStats = ComputeLlmCoverage(extras.LlmCounters)
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
		// Yearly total_return rows
		for _, ym := range sr.Yearly {
			_ = w.Write([]string{
				seg,
				strconv.FormatBool(sr.IsFormal),
				fmt.Sprintf("yearly_%d_total_return", ym.Year),
				strconv.FormatBool(ym.Metrics.TotalReturn.Available),
				formatOpt(ym.Metrics.TotalReturn),
				ym.Metrics.TotalReturn.Note,
			})
			_ = w.Write([]string{
				seg,
				strconv.FormatBool(sr.IsFormal),
				fmt.Sprintf("yearly_%d_ann_return", ym.Year),
				strconv.FormatBool(ym.Metrics.AnnReturn.Available),
				formatOpt(ym.Metrics.AnnReturn),
				ym.Metrics.AnnReturn.Note,
			})
			_ = w.Write([]string{
				seg,
				strconv.FormatBool(sr.IsFormal),
				fmt.Sprintf("yearly_%d_sharpe", ym.Year),
				strconv.FormatBool(ym.Metrics.Sharpe.Available),
				formatOpt(ym.Metrics.Sharpe),
				ym.Metrics.Sharpe.Note,
			})
			_ = w.Write([]string{
				seg,
				strconv.FormatBool(sr.IsFormal),
				fmt.Sprintf("yearly_%d_max_drawdown", ym.Year),
				strconv.FormatBool(ym.Metrics.MaxDrawdown.Available),
				formatOpt(ym.Metrics.MaxDrawdown),
				ym.Metrics.MaxDrawdown.Note,
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
	b.WriteString("\n## Attribution method (Brinson skeleton)\n\n")
	b.WriteString(r.BrinsonMethod + "\n")
	b.WriteString("\n## Factor layer method\n\n")
	b.WriteString(r.FactorLayerMethod + "\n")
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

		if len(sr.Yearly) > 0 {
			b.WriteString("\n### Per-year performance\n\n")
			b.WriteString("| Year | N | TotalReturn | AnnReturn | Sharpe | MaxDD | Note |\n|------|---|-------------|-----------|--------|-------|------|\n")
			for _, ym := range sr.Yearly {
				b.WriteString(fmt.Sprintf("| %d | %d | %s | %s | %s | %s | |\n",
					ym.Year, ym.NSnapshots,
					optCell(ym.Metrics.TotalReturn),
					optCell(ym.Metrics.AnnReturn),
					optCell(ym.Metrics.Sharpe),
					optCell(ym.Metrics.MaxDrawdown),
				))
			}
		}

		b.WriteString("\n### Industry distribution TS\n\n")
		if len(sr.IndustryDistribution) == 0 {
			b.WriteString(fmt.Sprintf("- N/A: %s\n", sr.Metrics.IndustryDistTS.Note))
		} else {
			b.WriteString(fmt.Sprintf("- Points: %d (see report.json `industry_distribution`)\n", len(sr.IndustryDistribution)))
			// Show last point summary
			last := sr.IndustryDistribution[len(sr.IndustryDistribution)-1]
			inds := make([]string, 0, len(last.Weights))
			for k := range last.Weights {
				inds = append(inds, k)
			}
			sort.Strings(inds)
			parts := make([]string, 0, len(inds))
			for _, k := range inds {
				parts = append(parts, fmt.Sprintf("%s=%.3f", k, last.Weights[k]))
			}
			b.WriteString(fmt.Sprintf("- Last day `%s`: %s\n", last.TradeDate, strings.Join(parts, ", ")))
		}

		b.WriteString("\n### Factor exposure TS\n\n")
		if len(sr.FactorExposure) == 0 {
			b.WriteString("- N/A: DailyFactorPanel not provided (composite + per-factor portfolio-weighted exposure)\n")
		} else {
			b.WriteString(fmt.Sprintf("- Points: %d (see report.json `factor_exposure`)\n", len(sr.FactorExposure)))
		}

		b.WriteString("\n### Brinson attribution\n\n")
		if !sr.Attribution.Available {
			b.WriteString(fmt.Sprintf("- N/A: %s\n", sr.Attribution.Note))
		} else {
			b.WriteString(fmt.Sprintf("- Allocation: %s\n", optCell(sr.Attribution.Allocation)))
			b.WriteString(fmt.Sprintf("- Selection: %s\n", optCell(sr.Attribution.Selection)))
			b.WriteString(fmt.Sprintf("- Interaction: %s\n", optCell(sr.Attribution.Interaction)))
			b.WriteString(fmt.Sprintf("- Note: %s\n", sr.Attribution.Note))
		}

		b.WriteString("\n### Factor layers / long-short\n\n")
		if len(sr.FactorLayers) == 0 || !anyFactorLayerAvailable(sr.FactorLayers) {
			note := "N/A"
			if len(sr.FactorLayers) > 0 {
				note = sr.FactorLayers[0].Note
			}
			b.WriteString(fmt.Sprintf("- %s\n", note))
		} else {
			for _, fl := range sr.FactorLayers {
				if !fl.Available {
					continue
				}
				b.WriteString(fmt.Sprintf("- **%s** (Q=%d):\n", fl.Factor, fl.NQuantile))
				for _, lr := range fl.Layers {
					b.WriteString(fmt.Sprintf("  - %s: %s (n_days=%d)\n", lr.Layer, optCell(lr.MeanReturn), lr.NDays))
				}
			}
		}

		b.WriteString("\n### LLM coverage / degrade\n\n")
		if !sr.LlmStats.Available {
			b.WriteString(fmt.Sprintf("- N/A: %s\n", sr.LlmStats.Note))
		} else {
			b.WriteString(fmt.Sprintf("- Coverage: %s\n", optCell(sr.LlmStats.CoverageRate)))
			b.WriteString(fmt.Sprintf("- Degrade count: %s\n", optCell(sr.LlmStats.DegradeCount)))
			b.WriteString(fmt.Sprintf("- Annotate calls: %s\n", optCell(sr.LlmStats.AnnotateCalls)))
		}
	}
	b.WriteString("\n---\n*Not investment advice. Values above are computed from the run's equity/turnover curves only.*\n")
	return path, os.WriteFile(path, []byte(b.String()), 0o644)
}

func optCell(f OptionalFloat) string {
	if !f.Available {
		return "N/A"
	}
	return strconv.FormatFloat(f.Value, 'f', 6, 64)
}

func anyFactorLayerAvailable(layers []FactorLayerResult) bool {
	for _, fl := range layers {
		if fl.Available {
			return true
		}
	}
	return false
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

// BuildIndustryLookupFromRecords maps each security to its latest industry code
// (EffectiveDate ≤ asof when asof is set; otherwise latest by EffectiveDate).
// Used by CLI to feed ReportExtras without importing universe into every caller.
func BuildIndustryLookupFromRecords(recs []types.IndustryRecord, source string, asof types.TradeDate) map[types.SecurityID]string {
	type best struct {
		code string
		eff  types.TradeDate
	}
	cur := map[types.SecurityID]best{}
	asofSet := !asof.Time().IsZero()
	for _, r := range recs {
		if source != "" && r.Source != "" && r.Source != source {
			continue
		}
		if asofSet && r.EffectiveDate.After(asof) {
			continue
		}
		if asofSet && r.ExpireDate != nil && !asof.Before(*r.ExpireDate) {
			continue
		}
		prev, ok := cur[r.TSCode]
		if !ok || r.EffectiveDate.After(prev.eff) {
			cur[r.TSCode] = best{code: r.IndustryCode, eff: r.EffectiveDate}
		}
	}
	out := make(map[types.SecurityID]string, len(cur))
	for k, v := range cur {
		out[k] = v.code
	}
	return out
}

// Command backtest runs a fixture/live-window backtest and writes nav/fills + §6 metrics reports.
// Metrics are computed from real equity/turnover curves only — never invented.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"quantitative-trading/internal/backtest"
	"quantitative-trading/internal/config"
	"quantitative-trading/internal/data"
	"quantitative-trading/internal/data/akshare"
	"quantitative-trading/internal/data/fixture"
	"quantitative-trading/internal/factors"
	"quantitative-trading/internal/llm"
	"quantitative-trading/internal/portfolio"
	"quantitative-trading/internal/types"
	"quantitative-trading/internal/universe"
)

func openProvider(cfg config.Config) (data.Provider, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Data.Provider)) {
	case "", "fixture":
		fixDir := cfg.Data.FixtureDir
		if !filepath.IsAbs(fixDir) {
			fixDir = filepath.Clean(fixDir)
		}
		return fixture.New(fixDir)
	case "akshare":
		helper := cfg.Data.AKShareHelper
		if helper == "" {
			helper = "scripts/akshare_fetch.py"
		}
		if !filepath.IsAbs(helper) {
			helper = filepath.Clean(helper)
		}
		py := cfg.Data.AKSharePython
		if py == "" {
			py = "python3"
		}
		cache := cfg.Data.AKShareCacheDir
		return akshare.New(akshare.Config{
			PythonBin:   py,
			HelperPath:  helper,
			CacheDir:    cache,
			SkipNetwork: false,
			StrictPIT:   cfg.Data.StrictPIT,
		}), nil
	default:
		return nil, fmt.Errorf("unknown data.provider %q (want fixture|akshare)", cfg.Data.Provider)
	}
}

// lookbackCalendarDays covers momentum(~252 trading days), ADV20, and min_list_trading_days(60).
const lookbackCalendarDays = 400

// resolveDataWindow returns [dataStart, dataEnd] for Bars/Adj preload covering the
// backtest window plus factor lookback. Never uses zero/zero full history.
func resolveDataWindow(cfg config.Config, cal []types.TradeDate) (from, to types.TradeDate, err error) {
	if len(cal) == 0 {
		return types.TradeDate{}, types.TradeDate{}, fmt.Errorf("empty calendar")
	}
	to = cal[len(cal)-1]
	if cfg.Backtest.EndDate != "" {
		ed, e := types.ParseTradeDate(cfg.Backtest.EndDate)
		if e != nil {
			return types.TradeDate{}, types.TradeDate{}, fmt.Errorf("end_date: %w", e)
		}
		to = ed
	}
	from = cal[0]
	if cfg.Backtest.StartDate != "" {
		sd, e := types.ParseTradeDate(cfg.Backtest.StartDate)
		if e != nil {
			return types.TradeDate{}, types.TradeDate{}, fmt.Errorf("start_date: %w", e)
		}
		from = types.TradeDate(sd.Time().AddDate(0, 0, -lookbackCalendarDays))
	} else {
		// Legacy fixture path: keep enough headroom before the signal window.
		startIdx := len(cal) - 8
		if startIdx < 260 {
			startIdx = 260
		}
		if startIdx >= len(cal) {
			startIdx = 0
		}
		from = types.TradeDate(cal[startIdx].Time().AddDate(0, 0, -lookbackCalendarDays))
	}
	if from.Before(cal[0]) {
		from = cal[0]
	}
	if to.After(cal[len(cal)-1]) {
		to = cal[len(cal)-1]
	}
	if !from.Before(to) && !from.Equal(to) {
		return types.TradeDate{}, types.TradeDate{}, fmt.Errorf("data window invalid: %s .. %s", from, to)
	}
	return from, to, nil
}

func main() {
	cfgPath := flag.String("config", "configs/example.yaml", "path to YAML config")
	outDir := flag.String("out", "output/backtest_run", "output directory for nav/fills/reports")
	paperFlag := flag.Bool("paper", false, "force paper/simulation target export (paper_targets.csv/.md); overrides paper_export.enabled")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}
	if _, err := backtest.ParseSampleSplit(cfg.Backtest); err != nil {
		fmt.Fprintf(os.Stderr, "sample_split: %v\n", err)
		os.Exit(1)
	}

	prov, err := openProvider(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "provider: %v\n", err)
		os.Exit(1)
	}

	cal, err := prov.LoadCalendar()
	if err != nil {
		fmt.Fprintf(os.Stderr, "calendar: %v\n", err)
		os.Exit(1)
	}
	calIndex := universe.BuildIndex(cal)

	secs, _ := prov.Securities()
	secMap := map[types.SecurityID]types.SecurityMaster{}
	missingListDate := 0
	for _, s := range secs {
		secMap[s.TSCode] = s
		if !universe.HasListDate(s) {
			missingListDate++
		}
	}
	if missingListDate > 0 {
		fmt.Fprintf(os.Stderr,
			"warning: %d/%d securities missing list_date — skipping min_list_trading_days for those (degrade); prefer real IPO dates in production\n",
			missingListDate, len(secs))
	} else if len(secs) > 0 {
		fmt.Fprintf(os.Stderr, "securities list_date: populated for all %d names\n", len(secs))
	}

	// Optional debug-only universe sampling (smoke). Prefer date-window truncation for production.
	if n := cfg.Data.MaxSymbols; n > 0 && len(secs) > n {
		sort.Slice(secs, func(i, j int) bool { return secs[i].TSCode < secs[j].TSCode })
		secs = secs[:n]
		secMap = map[types.SecurityID]types.SecurityMaster{}
		missingListDate = 0
		for _, s := range secs {
			secMap[s.TSCode] = s
			if !universe.HasListDate(s) {
				missingListDate++
			}
		}
		fmt.Fprintf(os.Stderr, "warning: data.max_symbols=%d applied (debug smoke only; not for production)\n", n)
		if missingListDate > 0 {
			fmt.Fprintf(os.Stderr,
				"warning: after max_symbols, %d/%d securities still missing list_date (min_list degraded)\n",
				missingListDate, len(secs))
		}
	}

	st, err := prov.ST()
	if err != nil {
		if cfg.Data.StrictPIT {
			fmt.Fprintf(os.Stderr, "st (strict PIT): %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "warning: st: %v\n", err)
	}
	if cfg.Data.StrictPIT {
		if err := factors.ValidateSTForStrict(st); err != nil {
			fmt.Fprintf(os.Stderr, "st (strict PIT): %v\n", err)
			os.Exit(1)
		}
	}
	inds, err := prov.Industry()
	if err != nil {
		if cfg.Data.StrictPIT {
			fmt.Fprintf(os.Stderr, "industry (strict PIT): %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "warning: industry: %v\n", err)
	}
	if cfg.Data.StrictPIT {
		if err := factors.ValidateIndustryForStrict(inds); err != nil {
			fmt.Fprintf(os.Stderr, "industry (strict PIT): %v\n", err)
			os.Exit(1)
		}
	}

	dataFrom, dataTo, err := resolveDataWindow(cfg, cal)
	if err != nil {
		fmt.Fprintf(os.Stderr, "data window: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "bars preload window: %s .. %s (%d symbols)\n", dataFrom, dataTo, len(secMap))

	barsByCode := map[types.SecurityID]map[string]types.Bar{}
	barLists := map[types.SecurityID][]types.Bar{}
	codes := make([]types.SecurityID, 0, len(secMap))
	for code := range secMap {
		codes = append(codes, code)
	}
	sort.Slice(codes, func(i, j int) bool { return codes[i] < codes[j] })
	for i, code := range codes {
		bs, _ := prov.Bars(code, dataFrom, dataTo)
		barLists[code] = bs
		m := map[string]types.Bar{}
		for _, b := range bs {
			m[b.TradeDate.String()] = b
		}
		barsByCode[code] = m
		if (i+1)%50 == 0 || i+1 == len(codes) {
			fmt.Fprintf(os.Stderr, "bars preload progress: %d/%d\n", i+1, len(codes))
		}
	}
	// Fill conventional high/low limits when vendor bars omit them (needed for Unbuyable / skip).
	universe.EnrichBarsLimits(barsByCode, secMap, st)

	uctx := &universe.Context{
		Cfg:        cfg,
		Calendar:   cal,
		CalIndex:   calIndex,
		Securities: secMap,
		Bars:       barsByCode,
		ST:         st,
		Industry:   inds,
	}

	plugin := llm.NewPlugin(cfg.LLM) // Noop when disabled

	eng := &backtest.Engine{
		Cfg:      cfg,
		Calendar: cal,
		CalIndex: calIndex,
		Bars:     barsByCode,
	}
	state := &backtest.PortfolioState{Cash: cfg.Backtest.InitialCash}

	startIdx, endIdx, err := backtest.ResolveWindow(cal, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "window: %v\n", err)
		os.Exit(1)
	}

	var factorPanels []backtest.DailyFactorPanel
	var paperDays []backtest.PaperTargetDay
	benchUnivByDate := map[string][]types.SecurityID{} // eligible universe per exec snap date (EW Brinson bench)
	const mvLookbackCalendarDays = 10                  // nearest TotalMV on or before T within this window

	snaps, err := backtest.RunCalendar(eng, state, startIdx, endIdx, func(t, execDate types.TradeDate, closeSnap types.DailySnapshot) ([]types.TargetHolding, error) {
		// Signal-day selection uses T bars only — never T+1 exec bars for Unbuyable/limit.
		univ := universe.BuildUniverse(uctx, t, nil)
		uCodes := make([]types.SecurityID, 0, len(univ))
		industry := map[types.SecurityID]string{}
		for _, s := range univ {
			uCodes = append(uCodes, s.Security.TSCode)
			industry[s.Security.TSCode] = s.Industry
		}
		fmt.Fprintf(os.Stderr, "universe %s: eligible=%d of %d securities (exec=%s)\n", t, len(univ), len(secMap), execDate)
		if len(uCodes) > 0 {
			benchUnivByDate[execDate.String()] = append([]types.SecurityID{}, uCodes...)
		}

		rawQ := map[types.SecurityID]float64{}
		rawV := map[types.SecurityID]float64{}
		rawM := map[types.SecurityID]float64{}
		for _, code := range uCodes {
			fins, ferr := prov.Financials(code)
			if ferr != nil {
				if cfg.Data.StrictPIT {
					return nil, fmt.Errorf("financials %s: %w", code, ferr)
				}
			}
			fin, ok := factors.GetLatestFinancialPIT(fins, t, cfg.Data.StrictPIT)
			if ok {
				if q, ok := factors.RawQuality(fin); ok {
					rawQ[code] = q
				}
				// Require real TotalMV on/before T; never invent MV from Close*constant.
				mvFrom := types.TradeDate(t.Time().AddDate(0, 0, -mvLookbackCalendarDays))
				mvList, merr := prov.MarketValues(code, mvFrom, t)
				if merr != nil {
					if cfg.Data.StrictPIT {
						return nil, fmt.Errorf("market value %s: %w", code, merr)
					}
				}
				if cfg.Data.StrictPIT {
					mv, err := factors.MarketValueAsOfPIT(mvList, t, mvLookbackCalendarDays, true)
					if err == nil {
						if v, ok := factors.RawValue(fin, mv); ok {
							rawV[code] = v
						}
					}
				} else if mv, ok := factors.MarketValueAsOf(mvList, t, mvLookbackCalendarDays); ok {
					if v, ok := factors.RawValue(fin, mv); ok {
						rawV[code] = v
					}
				}
			}
			if m, ok := factors.RawMomentum(barLists[code], t, 252, 21); ok {
				rawM[code] = m
			}
		}
		pipe := factors.BuildRawAndProcess(cfg, uCodes, industry, rawQ, rawV, rawM)

		_, _ = plugin.Annotate(nil, t, uCodes)

		comp := portfolio.CompositeEqualWeight(cfg, pipe.Scores, industry)
		// Freeze Top-N on signal day. Limit/halt/unbuyable are applied at T+1 open only.
		targets := portfolio.SelectHoldings(comp, cfg.Portfolio.N, cfg.Portfolio.IndustryCapRatio)

		paperNav := closeSnap.NAV
		if paperNav <= 0 {
			paperNav = cfg.Backtest.InitialCash
		}
		paperCodes := make([]types.SecurityID, 0, len(targets))
		for _, th := range targets {
			paperCodes = append(paperCodes, th.TSCode)
		}
		paperDays = append(paperDays, backtest.PaperTargetDay{
			SignalDate: t,
			ExecDate:   execDate,
			Weights:    backtest.TargetsToWeightMap(targets),
			NAV:        paperNav,
			Prices:     backtest.CollectSignalCloses(paperCodes, t, barsByCode),
		})

		panel := backtest.DailyFactorPanel{
			TradeDate:  execDate,
			Quality:    map[types.SecurityID]float64{},
			Value:      map[types.SecurityID]float64{},
			Momentum:   map[types.SecurityID]float64{},
			Composite:  map[types.SecurityID]float64{},
			ForwardRet: map[types.SecurityID]float64{},
		}
		for code, fs := range pipe.Scores {
			if fs.Quality != nil {
				panel.Quality[code] = *fs.Quality
			}
			if fs.Value != nil {
				panel.Value[code] = *fs.Value
			}
			if fs.Momentum != nil {
				panel.Momentum[code] = *fs.Momentum
			}
		}
		for _, c := range comp {
			panel.Composite[c.TSCode] = c.Composite
		}
		for _, code := range uCodes {
			if fr, ok := factors.ForwardCloseReturn(barsByCode[code], t, execDate); ok {
				panel.ForwardRet[code] = fr
			}
		}
		if len(panel.ForwardRet) == 0 {
			panel.ForwardRet = nil
		}
		factorPanels = append(factorPanels, panel)
		return targets, nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "backtest: %v\n", err)
		os.Exit(1)
	}

	backtest.AnnotateDailyActivity(snaps, state.Fills)

	navPath, err := backtest.WriteSnapshots(*outDir, snaps)
	if err != nil {
		fmt.Fprintf(os.Stderr, "write nav: %v\n", err)
		os.Exit(1)
	}
	fillsPath, err := backtest.WriteFills(*outDir, state.Fills)
	if err != nil {
		fmt.Fprintf(os.Stderr, "write fills: %v\n", err)
		os.Exit(1)
	}

	var bench []types.IndexBar
	if cfg.Backtest.Benchmark != "" {
		from, to := types.TradeDate{}, types.TradeDate{}
		if len(snaps) > 0 {
			from, to = snaps[0].TradeDate, snaps[len(snaps)-1].TradeDate
		}
		bench, _ = prov.IndexBars(cfg.Backtest.Benchmark, from, to)
		backtest.SortIndexBars(bench)
	}

	extras := backtest.ReportExtras{}
	if len(inds) > 0 {
		asof := types.TradeDate{}
		if len(snaps) > 0 {
			asof = snaps[len(snaps)-1].TradeDate
		}
		extras.IndustryLookup = backtest.BuildIndustryLookupFromRecords(inds, cfg.Processing.IndustrySource, asof)
	}
	if eng.SkipCounts.IntendedBuyOrders+eng.SkipCounts.IntendedSellOrders > 0 {
		sc := eng.SkipCounts
		extras.SkipCounts = &sc
	}
	if len(factorPanels) > 0 {
		extras.FactorPanels = factorPanels
	}
	if len(extras.IndustryLookup) > 0 && len(barsByCode) > 0 {
		extras.AttributionPeriods = backtest.BuildAttributionPeriodsFromSnaps(
			snaps, extras.IndustryLookup, barsByCode, benchUnivByDate)
		if len(extras.AttributionPeriods) == 0 {
			fmt.Fprintf(os.Stderr, "brinson: no AttributionPeriods built (need consecutive Weights snaps + real close-to-close returns)\n")
		} else {
			fmt.Fprintf(os.Stderr, "brinson: built %d AttributionPeriod(s) (EW-universe industry proxy)\n", len(extras.AttributionPeriods))
		}
	}
	// LlmCounters remain unset until LLM path wires counters — honest N/A.
	report, err := backtest.BuildReportWithExtras(cfg, snaps, state.Fills, bench, extras)
	if err != nil {
		fmt.Fprintf(os.Stderr, "report: %v\n", err)
		os.Exit(1)
	}
	jsonPath, csvPath, mdPath, err := backtest.WriteAllReports(*outDir, report)
	if err != nil {
		fmt.Fprintf(os.Stderr, "write reports: %v\n", err)
		os.Exit(1)
	}

	paperCSV, paperMD := "", ""
	exportPaper := cfg.PaperExport.Enabled || *paperFlag
	if exportPaper {
		res := backtest.BuildPaperTargets(paperDays, cfg.Execution.LotSize)
		paperCSV, paperMD, err = backtest.WritePaperTargets(*outDir, res)
		if err != nil {
			fmt.Fprintf(os.Stderr, "paper export: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "paper export: %d rows (%s) rebalance_freq=%s\n",
			len(res.Rows), backtest.SummarizePaperActions(res.Rows), cfg.Portfolio.RebalanceFreq)
	}

	fmt.Println("spec_version:", cfg.Version)
	fmt.Println("provider:", cfg.Data.Provider)
	fmt.Println("llm.enabled:", cfg.LLM.Enabled)
	fmt.Println("nav:", navPath)
	fmt.Println("fills:", fillsPath)
	fmt.Println("report_json:", jsonPath)
	fmt.Println("metrics_csv:", csvPath)
	fmt.Println("report_md:", mdPath)
	if exportPaper {
		fmt.Println("paper_csv:", paperCSV)
		fmt.Println("paper_md:", paperMD)
	}
	fmt.Println("snapshots:", len(snaps), "fills:", len(state.Fills))
	fmt.Println("formal_segment:", report.FormalSegment)
	if formal, ok := report.Segments[string(report.FormalSegment)]; ok && formal.Metrics.HasCurve {
		fmt.Printf("fixture_demo formal total_return: %.6f (computed from this run's NAV; not live market results)\n",
			formal.Metrics.TotalReturn.Value)
	} else {
		fmt.Println("note: formal segment has no equity curve yet (too few days or empty OOS)")
	}
}

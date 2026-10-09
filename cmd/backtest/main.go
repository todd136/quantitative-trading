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
		}), nil
	default:
		return nil, fmt.Errorf("unknown data.provider %q (want fixture|akshare)", cfg.Data.Provider)
	}
}

func resolveWindow(cal []types.TradeDate, cfg config.Config) (startIdx, endIdx int, err error) {
	// endIdx is exclusive upper bound into cal for the signal/exec loop.
	if len(cal) < 3 {
		return 0, 0, fmt.Errorf("calendar too short")
	}
	startIdx, endIdx = 0, len(cal)-1

	if cfg.Backtest.StartDate != "" {
		sd, e := types.ParseTradeDate(cfg.Backtest.StartDate)
		if e != nil {
			return 0, 0, fmt.Errorf("start_date: %w", e)
		}
		found := false
		for i, d := range cal {
			if !d.Before(sd) {
				startIdx = i
				found = true
				break
			}
		}
		if !found {
			return 0, 0, fmt.Errorf("start_date %s after calendar end", cfg.Backtest.StartDate)
		}
	} else {
		// Legacy short fixture window: need momentum lookback headroom.
		startIdx = len(cal) - 8
		if startIdx < 260 {
			startIdx = 260
		}
	}

	if cfg.Backtest.EndDate != "" {
		ed, e := types.ParseTradeDate(cfg.Backtest.EndDate)
		if e != nil {
			return 0, 0, fmt.Errorf("end_date: %w", e)
		}
		found := false
		for i := len(cal) - 1; i >= 0; i-- {
			if !cal[i].After(ed) {
				endIdx = i + 1 // exclusive
				found = true
				break
			}
		}
		if !found {
			return 0, 0, fmt.Errorf("end_date %s before calendar start", cfg.Backtest.EndDate)
		}
	}

	// Leave one day for T+1 exec beyond last signal day.
	if endIdx > len(cal)-1 {
		endIdx = len(cal) - 1
	}
	if endIdx <= startIdx+1 {
		return 0, 0, fmt.Errorf("date window too short: startIdx=%d endIdx=%d", startIdx, endIdx)
	}
	return startIdx, endIdx, nil
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

	st, _ := prov.ST()
	inds, _ := prov.Industry()

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

	startIdx, endIdx, err := resolveWindow(cal, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "window: %v\n", err)
		os.Exit(1)
	}

	var snaps []types.DailySnapshot
	var factorPanels []backtest.DailyFactorPanel
	const mvLookbackCalendarDays = 10 // nearest TotalMV on or before T within this window
	for i := startIdx; i < endIdx-1; i++ { // -1: need next day for T+1
		t := cal[i]
		if !portfolio.IsRebalanceDay(t, cfg.Portfolio.RebalanceFreq, cfg.Portfolio.RebalanceWeekday, cal, calIndex) {
			snaps = append(snaps, eng.MarkNAV(state, t))
			continue
		}
		execDate, ok := eng.NextTradeDate(t)
		if !ok {
			break
		}
		// Respect end_date: do not exec past window
		if cfg.Backtest.EndDate != "" {
			ed, _ := types.ParseTradeDate(cfg.Backtest.EndDate)
			if execDate.After(ed) {
				snaps = append(snaps, eng.MarkNAV(state, t))
				continue
			}
		}
		// Pass T+1 bars so Unbuyable / CannotBuyAtOpen can use real open+limits when present.
		execBars := map[types.SecurityID]types.Bar{}
		for code, byDate := range barsByCode {
			if b, ok := byDate[execDate.String()]; ok {
				execBars[code] = b
			}
		}
		univ := universe.BuildUniverse(uctx, t, execBars)
		uCodes := make([]types.SecurityID, 0, len(univ))
		industry := map[types.SecurityID]string{}
		unbuyable := map[types.SecurityID]bool{}
		for _, s := range univ {
			uCodes = append(uCodes, s.Security.TSCode)
			industry[s.Security.TSCode] = s.Industry
			unbuyable[s.Security.TSCode] = s.Unbuyable
		}
		fmt.Fprintf(os.Stderr, "universe %s: eligible=%d of %d securities (exec=%s)\n", t, len(univ), len(secMap), execDate)

		rawQ := map[types.SecurityID]float64{}
		rawV := map[types.SecurityID]float64{}
		rawM := map[types.SecurityID]float64{}
		for _, code := range uCodes {
			fins, _ := prov.Financials(code)
			fin, ok := factors.GetLatestFinancial(fins, t)
			if ok {
				if q, ok := factors.RawQuality(fin); ok {
					rawQ[code] = q
				}
				// Require real TotalMV on/before T; never invent MV from Close*constant.
				mvFrom := types.TradeDate(t.Time().AddDate(0, 0, -mvLookbackCalendarDays))
				mvList, _ := prov.MarketValues(code, mvFrom, t)
				if mv, ok := factors.MarketValueAsOf(mvList, t, mvLookbackCalendarDays); ok {
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
		// Drop Unbuyable names from *new* opens (still in universe for research scores).
		compTradable := make([]types.CompositeResult, 0, len(comp))
		for _, c := range comp {
			if unbuyable[c.TSCode] {
				continue
			}
			compTradable = append(compTradable, c)
		}
		targets := portfolio.SelectHoldings(compTradable, cfg.Portfolio.N, cfg.Portfolio.IndustryCapRatio)

		nextAvail, _ := eng.NextTradeDate(execDate)
		if err := eng.ApplyTargets(state, execDate, targets, nextAvail); err != nil {
			fmt.Fprintf(os.Stderr, "exec %s: %v\n", execDate, err)
			os.Exit(1)
		}
		snaps = append(snaps, eng.MarkNAV(state, execDate))

		// Persist daily factor panel aligned to execDate (Weights snap date) for §6.2 exposure.
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
		// Optional real 1-day forward return: signal close T → exec close T+1 (never invented).
		for _, code := range uCodes {
			if fr, ok := factors.ForwardCloseReturn(barsByCode[code], t, execDate); ok {
				panel.ForwardRet[code] = fr
			}
		}
		if len(panel.ForwardRet) == 0 {
			panel.ForwardRet = nil // keep layered N/A honest when no real forwards
		}
		factorPanels = append(factorPanels, panel)
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
	// AttributionPeriods / LlmCounters remain unset until those inputs exist — honest N/A.
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

	fmt.Println("spec_version:", cfg.Version)
	fmt.Println("provider:", cfg.Data.Provider)
	fmt.Println("llm.enabled:", cfg.LLM.Enabled)
	fmt.Println("nav:", navPath)
	fmt.Println("fills:", fillsPath)
	fmt.Println("report_json:", jsonPath)
	fmt.Println("metrics_csv:", csvPath)
	fmt.Println("report_md:", mdPath)
	fmt.Println("snapshots:", len(snaps), "fills:", len(state.Fills))
	fmt.Println("formal_segment:", report.FormalSegment)
	if formal, ok := report.Segments[string(report.FormalSegment)]; ok && formal.Metrics.HasCurve {
		fmt.Printf("fixture_demo formal total_return: %.6f (computed from this run's NAV; not live market results)\n",
			formal.Metrics.TotalReturn.Value)
	} else {
		fmt.Println("note: formal segment has no equity curve yet (too few days or empty OOS)")
	}
}

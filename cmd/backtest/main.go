// Command backtest runs a short fixture-window skeleton backtest and prints output paths.
// Does not claim or invent performance metrics.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"quantitative-trading/internal/backtest"
	"quantitative-trading/internal/config"
	"quantitative-trading/internal/data/fixture"
	"quantitative-trading/internal/factors"
	"quantitative-trading/internal/llm"
	"quantitative-trading/internal/portfolio"
	"quantitative-trading/internal/types"
	"quantitative-trading/internal/universe"
)

func main() {
	cfgPath := flag.String("config", "configs/example.yaml", "path to YAML config")
	outDir := flag.String("out", "output/backtest_run", "output directory for nav/fills")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}

	fixDir := cfg.Data.FixtureDir
	if !filepath.IsAbs(fixDir) {
		fixDir = filepath.Clean(fixDir)
	}
	prov, err := fixture.New(fixDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fixture: %v\n", err)
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
	for _, s := range secs {
		secMap[s.TSCode] = s
	}
	st, _ := prov.ST()
	inds, _ := prov.Industry()

	barsByCode := map[types.SecurityID]map[string]types.Bar{}
	barLists := map[types.SecurityID][]types.Bar{}
	zero := types.TradeDate{}
	for code := range secMap {
		bs, _ := prov.Bars(code, zero, zero)
		barLists[code] = bs
		m := map[string]types.Bar{}
		for _, b := range bs {
			m[b.TradeDate.String()] = b
		}
		barsByCode[code] = m
	}

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

	// Short window: last ~5 trading days before end (skip final day for T+1)
	startIdx := len(cal) - 8
	if startIdx < 260 {
		startIdx = 260 // need momentum lookback headroom in fixtures
	}
	endIdx := len(cal) - 2
	if endIdx <= startIdx {
		fmt.Fprintf(os.Stderr, "calendar too short\n")
		os.Exit(1)
	}

	var snaps []types.DailySnapshot
	for i := startIdx; i < endIdx; i++ {
		t := cal[i]
		if !portfolio.IsRebalanceDay(t, cfg.Portfolio.RebalanceFreq, cfg.Portfolio.RebalanceWeekday, cal, calIndex) {
			snaps = append(snaps, eng.MarkNAV(state, t))
			continue
		}
		univ := universe.BuildUniverse(uctx, t, nil)
		uCodes := make([]types.SecurityID, 0, len(univ))
		industry := map[types.SecurityID]string{}
		for _, s := range univ {
			uCodes = append(uCodes, s.Security.TSCode)
			industry[s.Security.TSCode] = s.Industry
		}

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
				mvList, _ := prov.MarketValues(code, t, t)
				tmv := 0.0
				if len(mvList) > 0 {
					tmv = mvList[0].TotalMV
				} else if b, ok := barsByCode[code][t.String()]; ok {
					tmv = b.Close * 1e9
				}
				if v, ok := factors.RawValue(fin, tmv); ok {
					rawV[code] = v
				}
			}
			if m, ok := factors.RawMomentum(barLists[code], t, 252, 21); ok {
				rawM[code] = m
			}
		}
		pipe := factors.BuildRawAndProcess(cfg, uCodes, industry, rawQ, rawV, rawM)

		// LLM annotate (Noop when disabled — must not change composite path)
		_, _ = plugin.Annotate(nil, t, uCodes)

		comp := portfolio.CompositeEqualWeight(cfg, pipe.Scores, industry)
		targets := portfolio.SelectHoldings(comp, cfg.Portfolio.N, cfg.Portfolio.IndustryCapRatio)

		execDate, ok := eng.NextTradeDate(t)
		if !ok {
			break
		}
		nextAvail, _ := eng.NextTradeDate(execDate)
		if err := eng.ApplyTargets(state, execDate, targets, nextAvail); err != nil {
			fmt.Fprintf(os.Stderr, "exec %s: %v\n", execDate, err)
			os.Exit(1)
		}
		snaps = append(snaps, eng.MarkNAV(state, execDate))
	}

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

	fmt.Println("spec_version:", cfg.Version)
	fmt.Println("provider:", cfg.Data.Provider)
	fmt.Println("llm.enabled:", cfg.LLM.Enabled)
	fmt.Println("nav:", navPath)
	fmt.Println("fills:", fillsPath)
	fmt.Println("snapshots:", len(snaps), "fills:", len(state.Fills))
	fmt.Println("note: metrics not claimed — compute from nav.csv if needed")
}

// Package config loads YAML configuration matching spec appendix A (v1.2).
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the root configuration for the A-share multifactor backtest.
type Config struct {
	Version    string           `yaml:"version"`
	Market     MarketConfig     `yaml:"market"`
	Universe   UniverseConfig   `yaml:"universe"`
	Factors    FactorsConfig    `yaml:"factors"`
	Processing ProcessingConfig `yaml:"processing"`
	Portfolio  PortfolioConfig  `yaml:"portfolio"`
	Signal     SignalConfig     `yaml:"signal"`
	Costs      CostsConfig      `yaml:"costs"`
	Execution  ExecutionConfig  `yaml:"execution"`
	Backtest   BacktestConfig   `yaml:"backtest"`
	LLM        LLMConfig        `yaml:"llm"`
	Data       DataConfig       `yaml:"data"`
}

type MarketConfig struct {
	Boards                 []string `yaml:"boards"`
	ExcludeST              bool     `yaml:"exclude_st"`
	RequireStarEligibility bool     `yaml:"require_star_eligibility"`
}

type UniverseConfig struct {
	MinListTradingDays int     `yaml:"min_list_trading_days"`
	MinADV20           float64 `yaml:"min_adv_20"`
	MinADVValidDays    int     `yaml:"min_adv_valid_days"`
}

type FactorWeight struct {
	Enabled bool    `yaml:"enabled"`
	Weight  float64 `yaml:"weight"`
}

type FactorsConfig struct {
	Quality    FactorWeight `yaml:"quality"`
	Value      FactorWeight `yaml:"value"`
	Momentum   FactorWeight `yaml:"momentum"`
	RequireAll bool         `yaml:"require_all"`
}

type WinsorizeConfig struct {
	Method string  `yaml:"method"` // MAD | percentile
	N      float64 `yaml:"n"`
	PLow   float64 `yaml:"p_low"`
	PHigh  float64 `yaml:"p_high"`
}

type ProcessingConfig struct {
	Winsorize       WinsorizeConfig `yaml:"winsorize"`
	IndustryNeutral bool            `yaml:"industry_neutral"`
	IndustrySource  string          `yaml:"industry_source"`
	ZScore          bool            `yaml:"zscore"`
}

type PortfolioConfig struct {
	N                int     `yaml:"N"`
	Weight           string  `yaml:"weight"` // equal | mv
	IndustryCapRatio float64 `yaml:"industry_cap_ratio"`
	RebalanceFreq    string  `yaml:"rebalance_freq"`
	RebalanceWeekday int     `yaml:"rebalance_weekday"`
}

type SignalConfig struct {
	SignalPrice string `yaml:"signal_price"`
	ExecPrice   string `yaml:"exec_price"`
}

type CostsConfig struct {
	CommissionBps float64 `yaml:"commission_bps"`
	StampTaxBps   float64 `yaml:"stamp_tax_bps"`
	SlippageBps   float64 `yaml:"slippage_bps"`
}

type ExecutionConfig struct {
	RollingUnfilled         bool   `yaml:"rolling_unfilled"`
	VolumeConstraintEnabled bool   `yaml:"volume_constraint_enabled"`
	BuyLimitHandler         string `yaml:"buy_limit_handler"`
	LotSize                 int    `yaml:"lot_size"` // default 100
}

type FloatTolConfig struct {
	Rel float64 `yaml:"rel"`
	Abs float64 `yaml:"abs"`
}

// SampleSplitConfig is a fixed time-forward Train / Validate / OOS split (§6.3).
// train: [start, train_end]; validate: (train_end, val_end]; oos: (val_end, end].
type SampleSplitConfig struct {
	TrainEnd string `yaml:"train_end"`
	ValEnd   string `yaml:"val_end"`
}

// WalkForwardConfig is an optional skeleton for rolling windows (not fully executed yet).
type WalkForwardConfig struct {
	TrainYears int `yaml:"train_years"`
	TestYears  int `yaml:"test_years"`
}

type BacktestConfig struct {
	Benchmark    string            `yaml:"benchmark"`
	RiskFree     float64           `yaml:"risk_free"`
	InitialCash  float64           `yaml:"initial_cash"`
	FloatTol     FloatTolConfig    `yaml:"float_tol"`
	StartDate    string            `yaml:"start_date"`
	EndDate      string            `yaml:"end_date"`
	SampleSplit  SampleSplitConfig `yaml:"sample_split"`
	WalkForward  WalkForwardConfig `yaml:"walk_forward"`
}

type LLMFilterConfig struct {
	Enabled       bool     `yaml:"enabled"`
	MinConfidence float64  `yaml:"min_confidence"`
	BlockTags     []string `yaml:"block_tags"`
}

type LLMModelConfig struct {
	ModelID       string  `yaml:"model_id"`
	PromptVersion string  `yaml:"prompt_version"`
	Temperature   float64 `yaml:"temperature"`
	Seed          int     `yaml:"seed"`
	TimeoutMs     int     `yaml:"timeout_ms"`
	MaxRetries    int     `yaml:"max_retries"`
}

type LLMCacheConfig struct {
	Enabled   bool     `yaml:"enabled"`
	KeyFields []string `yaml:"key_fields"`
}

type LLMTextConfig struct {
	Timezone             string `yaml:"timezone"`
	RequireAsofLeqSignal bool   `yaml:"require_asof_leq_signal"`
	OnMissing            string `yaml:"on_missing"`
}

type LLMAuditConfig struct {
	LogRawText           bool   `yaml:"log_raw_text"`
	LogPromptFull        bool   `yaml:"log_prompt_full"`
	LogStructuredSummary bool   `yaml:"log_structured_summary"`
	ComplianceNotice     string `yaml:"compliance_notice"`
}

type LLMConfig struct {
	Enabled         bool            `yaml:"enabled"`
	Weight          float64         `yaml:"weight"`
	ScorePipeline   string          `yaml:"score_pipeline"`
	AttachPoint     string          `yaml:"attach_point"`
	ApplyToHoldings bool            `yaml:"apply_to_holdings"`
	Filter          LLMFilterConfig `yaml:"filter"`
	Model           LLMModelConfig  `yaml:"model"`
	Cache           LLMCacheConfig  `yaml:"cache"`
	Text            LLMTextConfig   `yaml:"text"`
	Audit           LLMAuditConfig  `yaml:"audit"`
}

type DataConfig struct {
	Provider        string `yaml:"provider"` // fixture | akshare
	FixtureDir      string `yaml:"fixture_dir"`
	AKSharePython   string `yaml:"akshare_python"`    // default python3
	AKShareHelper   string `yaml:"akshare_helper"`    // path to scripts/akshare_fetch.py
	AKShareCacheDir string `yaml:"akshare_cache_dir"` // JSON cache from prior fetches
	// MaxSymbols caps securities loaded during Bars preload (0 = all).
	// Debug-only smoke sampling — production runs must use date-window truncation, not permanent sampling.
	MaxSymbols int `yaml:"max_symbols"`
}

// Default returns v1.2 locked defaults from the spec appendix A.
func Default() Config {
	return Config{
		Version: "v1.2-20260928",
		Market: MarketConfig{
			Boards:                 []string{"SSE_MAIN", "SZSE_MAIN", "CHINEXT", "STAR"},
			ExcludeST:              true,
			RequireStarEligibility: false,
		},
		Universe: UniverseConfig{
			MinListTradingDays: 60,
			MinADV20:           20_000_000,
			MinADVValidDays:    15,
		},
		Factors: FactorsConfig{
			Quality:    FactorWeight{Enabled: true, Weight: 1.0},
			Value:      FactorWeight{Enabled: true, Weight: 1.0},
			Momentum:   FactorWeight{Enabled: true, Weight: 1.0},
			RequireAll: true,
		},
		Processing: ProcessingConfig{
			Winsorize:       WinsorizeConfig{Method: "MAD", N: 5, PLow: 0.02, PHigh: 0.98},
			IndustryNeutral: true,
			IndustrySource:  "SW_L1",
			ZScore:          true,
		},
		Portfolio: PortfolioConfig{
			N:                50,
			Weight:           "equal",
			IndustryCapRatio: 0.15,
			RebalanceFreq:    "daily",
			RebalanceWeekday: 2,
		},
		Signal: SignalConfig{
			SignalPrice: "close",
			ExecPrice:   "open",
		},
		Costs: CostsConfig{
			CommissionBps: 3,
			StampTaxBps:   5,
			SlippageBps:   5,
		},
		Execution: ExecutionConfig{
			RollingUnfilled:         true,
			VolumeConstraintEnabled: false,
			BuyLimitHandler:         "renormalize",
			LotSize:                 100,
		},
		Backtest: BacktestConfig{
			Benchmark:   "000300.SH",
			RiskFree:    0.0,
			InitialCash: 100_000_000,
			FloatTol:    FloatTolConfig{Rel: 1e-9, Abs: 1e-12},
		},
		LLM: LLMConfig{
			Enabled:       false,
			Weight:        0.0,
			ScorePipeline: "zscore_only",
			AttachPoint:   "post_universe",
			Filter: LLMFilterConfig{
				Enabled:       false,
				MinConfidence: 0.5,
				BlockTags:     []string{},
			},
			Model: LLMModelConfig{
				ModelID:       "",
				PromptVersion: "llm_prompt_v1",
				Temperature:   0.0,
				Seed:          0,
				TimeoutMs:     8000,
				MaxRetries:    0,
			},
			Cache: LLMCacheConfig{
				Enabled:   true,
				KeyFields: []string{"model_id", "prompt_version", "signal_date", "ts_code", "source_ids_hash"},
			},
			Text: LLMTextConfig{
				Timezone:             "Asia/Shanghai",
				RequireAsofLeqSignal: true,
				OnMissing:            "degrade_to_three_factor",
			},
			Audit: LLMAuditConfig{
				LogRawText:           false,
				LogPromptFull:        false,
				LogStructuredSummary: true,
				ComplianceNotice:     "non_investment_advice",
			},
		},
		Data: DataConfig{
			Provider:        "fixture",
			FixtureDir:      "testdata/fixtures",
			AKSharePython:   "python3",
			AKShareHelper:   "scripts/akshare_fetch.py",
			AKShareCacheDir: "testdata/akshare_cache",
		},
	}
}

// Load reads a YAML file and merges onto Default().
func Load(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("read config: %w", err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// Validate checks critical invariants.
func (c Config) Validate() error {
	if c.Portfolio.N <= 0 {
		return fmt.Errorf("portfolio.N must be > 0")
	}
	if c.Universe.MinADV20 < 0 {
		return fmt.Errorf("universe.min_adv_20 must be >= 0")
	}
	if c.LLM.Enabled && c.LLM.Model.ModelID == "" {
		return fmt.Errorf("llm.model.model_id required when llm.enabled=true")
	}
	if c.LLM.Enabled && c.LLM.Model.ModelID == "latest" {
		return fmt.Errorf("llm.model.model_id must not be 'latest'")
	}
	ss := c.Backtest.SampleSplit
	if ss.TrainEnd != "" || ss.ValEnd != "" {
		if ss.TrainEnd == "" || ss.ValEnd == "" {
			return fmt.Errorf("backtest.sample_split: both train_end and val_end required")
		}
		if ss.TrainEnd >= ss.ValEnd {
			return fmt.Errorf("backtest.sample_split: train_end must be < val_end (got %s >= %s)", ss.TrainEnd, ss.ValEnd)
		}
		if c.Backtest.EndDate != "" && ss.ValEnd >= c.Backtest.EndDate {
			return fmt.Errorf("backtest.sample_split: val_end must be < end_date")
		}
		if c.Backtest.StartDate != "" && ss.TrainEnd < c.Backtest.StartDate {
			return fmt.Errorf("backtest.sample_split: train_end must be >= start_date")
		}
	}
	return nil
}

// BoardSet returns allowed boards as a set.
func (c Config) BoardSet() map[string]bool {
	m := make(map[string]bool, len(c.Market.Boards))
	for _, b := range c.Market.Boards {
		m[b] = true
	}
	return m
}

// BuyCostRate returns commission + slippage as a fraction.
func (c Config) BuyCostRate() float64 {
	return (c.Costs.CommissionBps + c.Costs.SlippageBps) / 1e4
}

// SellCostRate returns commission + stamp + slippage as a fraction.
func (c Config) SellCostRate() float64 {
	return (c.Costs.CommissionBps + c.Costs.StampTaxBps + c.Costs.SlippageBps) / 1e4
}

// MaxPerIndustry returns max(3, ceil(N * industry_cap_ratio)).
func (c Config) MaxPerIndustry() int {
	n := float64(c.Portfolio.N) * c.Portfolio.IndustryCapRatio
	ceil := int(n)
	if float64(ceil) < n {
		ceil++
	}
	if ceil < 3 {
		return 3
	}
	return ceil
}

package config

import (
	"path/filepath"
	"runtime"
	"testing"
)

// Ensures research H1 YAML stays loadable (weekly + paper_export) without network.
func TestLoadResearchStrategyIter1YAML(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "configs", "research_strategy_iter_1.yaml")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Portfolio.RebalanceFreq != "weekly" {
		t.Fatalf("rebalance_freq=%q want weekly", cfg.Portfolio.RebalanceFreq)
	}
	if cfg.Portfolio.RebalanceWeekday != 2 {
		t.Fatalf("rebalance_weekday=%d want 2", cfg.Portfolio.RebalanceWeekday)
	}
	if !cfg.PaperExport.Enabled {
		t.Fatal("paper_export.enabled want true")
	}
	if !cfg.Factors.RequireAll || !cfg.Processing.IndustryNeutral {
		t.Fatal("formal flags require_all/industry_neutral should stay true")
	}
}

// Ensures research H2 YAML stays loadable (weekly + momentum/value tilt) without network.
func TestLoadResearchStrategyIter1H2YAML(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "configs", "research_strategy_iter_1_h2.yaml")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Portfolio.RebalanceFreq != "weekly" {
		t.Fatalf("rebalance_freq=%q want weekly", cfg.Portfolio.RebalanceFreq)
	}
	if cfg.Factors.Value.Weight != 0.8 {
		t.Fatalf("value.weight=%v want 0.8", cfg.Factors.Value.Weight)
	}
	if cfg.Factors.Momentum.Weight != 1.2 {
		t.Fatalf("momentum.weight=%v want 1.2", cfg.Factors.Momentum.Weight)
	}
	if cfg.Factors.Quality.Weight != 1.0 {
		t.Fatalf("quality.weight=%v want 1.0", cfg.Factors.Quality.Weight)
	}
	if !cfg.PaperExport.Enabled {
		t.Fatal("paper_export.enabled want true")
	}
	if !cfg.Factors.RequireAll || !cfg.Processing.IndustryNeutral {
		t.Fatal("formal flags require_all/industry_neutral should stay true")
	}
}


// Ensures research H3 YAML stays loadable (weekly H1 factors + tighter industry_cap_ratio) without network.
func TestLoadResearchStrategyIter1H3YAML(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "configs", "research_strategy_iter_1_h3.yaml")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Portfolio.RebalanceFreq != "weekly" {
		t.Fatalf("rebalance_freq=%q want weekly", cfg.Portfolio.RebalanceFreq)
	}
	if cfg.Portfolio.IndustryCapRatio != 0.12 {
		t.Fatalf("industry_cap_ratio=%v want 0.12", cfg.Portfolio.IndustryCapRatio)
	}
	if cfg.MaxPerIndustry() != 6 {
		t.Fatalf("MaxPerIndustry=%d want 6 (N=50, ratio=0.12)", cfg.MaxPerIndustry())
	}
	// H3 is on H1 only: equal factor weights (not H2 tilt).
	if cfg.Factors.Value.Weight != 1.0 || cfg.Factors.Momentum.Weight != 1.0 || cfg.Factors.Quality.Weight != 1.0 {
		t.Fatalf("factor weights q/v/m=%v/%v/%v want 1/1/1",
			cfg.Factors.Quality.Weight, cfg.Factors.Value.Weight, cfg.Factors.Momentum.Weight)
	}
	if !cfg.PaperExport.Enabled {
		t.Fatal("paper_export.enabled want true")
	}
	if !cfg.Factors.RequireAll || !cfg.Processing.IndustryNeutral {
		t.Fatal("formal flags require_all/industry_neutral should stay true")
	}
}

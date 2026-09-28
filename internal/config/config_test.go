package config_test

import (
	"path/filepath"
	"testing"

	"quantitative-trading/internal/config"
)

func TestDefaultMatchesV12(t *testing.T) {
	c := config.Default()
	if c.Version != "v1.2-20260928" {
		t.Fatalf("version %s", c.Version)
	}
	if c.LLM.Enabled {
		t.Fatal("llm.enabled must default false")
	}
	if c.LLM.Weight != 0 {
		t.Fatal("llm.weight must default 0")
	}
	boards := c.BoardSet()
	if !boards["STAR"] {
		t.Fatal("STAR must be included")
	}
	if boards["BSE"] {
		t.Fatal("BSE must not be in default boards")
	}
	if c.Universe.MinADV20 != 20_000_000 {
		t.Fatalf("min_adv_20=%v", c.Universe.MinADV20)
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadExampleYAML(t *testing.T) {
	path := filepath.Join("..", "..", "configs", "example.yaml")
	c, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.LLM.Enabled {
		t.Fatal("example yaml must keep llm.enabled=false")
	}
	if c.Portfolio.N != 50 {
		t.Fatalf("N=%d", c.Portfolio.N)
	}
}

func TestValidateLLMRequiresModelID(t *testing.T) {
	c := config.Default()
	c.LLM.Enabled = true
	c.LLM.Model.ModelID = ""
	if err := c.Validate(); err == nil {
		t.Fatal("expected error when enabled without model_id")
	}
	c.LLM.Model.ModelID = "latest"
	if err := c.Validate(); err == nil {
		t.Fatal("expected error for model_id=latest")
	}
}

func TestCostRates(t *testing.T) {
	c := config.Default()
	// buy: 3+5 = 8 bps; sell: 3+5+5 = 13 bps
	if c.BuyCostRate() != 8e-4 {
		t.Fatalf("buy=%v", c.BuyCostRate())
	}
	if c.SellCostRate() != 13e-4 {
		t.Fatalf("sell=%v", c.SellCostRate())
	}
}

func TestMaxPerIndustry(t *testing.T) {
	c := config.Default()
	// max(3, ceil(50*0.15)=8) = 8
	if c.MaxPerIndustry() != 8 {
		t.Fatalf("got %d", c.MaxPerIndustry())
	}
}

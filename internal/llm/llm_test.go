package llm_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"quantitative-trading/internal/backtest"
	"quantitative-trading/internal/config"
	"quantitative-trading/internal/data/fixture"
	"quantitative-trading/internal/llm"
	"quantitative-trading/internal/portfolio"
	"quantitative-trading/internal/types"
)

func TestNoopWhenDisabled(t *testing.T) {
	cfg := config.Default()
	if cfg.LLM.Enabled {
		t.Fatal("default enabled?")
	}
	p := llm.NewPlugin(cfg.LLM)
	if p.Enabled() {
		t.Fatal("Noop must report Enabled=false")
	}
	out, err := p.Annotate(context.Background(), types.NewTradeDate(2025, 1, 10), []types.SecurityID{"600000.SH"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Fatalf("Annotate must return empty when disabled, got %d", len(out))
	}
}

func TestTextAsofAfterTInvisible(t *testing.T) {
	prov, err := fixture.New(filepath.Join("..", "..", "testdata", "fixtures"))
	if err != nil {
		t.Fatal(err)
	}
	signal, _ := types.ParseTradeDate("2025-01-10")
	// Provider already filters; also test FilterTextsByAsof on raw-ish set
	all, err := loadAllTexts(prov)
	if err != nil {
		t.Fatal(err)
	}
	filtered := llm.FilterTextsByAsof(all, signal)
	for _, tx := range filtered {
		end := llm.EndOfDayShanghai(signal)
		if tx.AsofTS.After(end) {
			t.Fatalf("leaked future text %s asof %v", tx.SourceID, tx.AsofTS)
		}
	}
	// Explicitly: news_20250111_999 must be absent
	for _, tx := range filtered {
		if tx.SourceID == "news_20250111_999" {
			t.Fatal("future asof_ts text must be invisible")
		}
	}
	found := false
	for _, tx := range filtered {
		if tx.SourceID == "ann_20250109_001" {
			found = true
		}
	}
	if !found {
		t.Fatal("past text should remain visible")
	}
}

// loadAllTexts bypasses provider filter by reading via a wide asof (year 2099).
func loadAllTexts(prov *fixture.Provider) ([]types.PublicText, error) {
	far, _ := types.ParseTradeDate("2099-01-01")
	return prov.PublicTexts("", far)
}

func TestDisabledCompositeParityWithOrWithoutPlugin(t *testing.T) {
	cfg := config.Default()
	cfg.LLM.Enabled = false
	cfg.LLM.Weight = 0

	q, v, m := 0.5, -0.2, 1.0
	scores := map[types.SecurityID]types.FactorScores{
		"600000.SH": {TSCode: "600000.SH", Quality: &q, Value: &v, Momentum: &m},
		"000001.SZ": {TSCode: "000001.SZ", Quality: &m, Value: &q, Momentum: &v},
	}
	industry := map[types.SecurityID]string{
		"600000.SH": "801780",
		"000001.SZ": "801780",
	}

	// Path A: no plugin involvement
	compA := portfolio.CompositeEqualWeight(cfg, scores, industry)

	// Path B: Noop plugin present but disabled — annotate then composite (LLM field unused)
	plugin := llm.NewPlugin(cfg.LLM)
	feats, _ := plugin.Annotate(context.Background(), types.NewTradeDate(2025, 1, 10), []types.SecurityID{"600000.SH", "000001.SZ"})
	scoresB := cloneScores(scores)
	for _, f := range feats {
		if f.SentimentScore != nil {
			fs := scoresB[f.TSCode]
			fs.LLM = f.SentimentScore
			scoresB[f.TSCode] = fs
		}
	}
	compB := portfolio.CompositeEqualWeight(cfg, scoresB, industry)

	tol := cfg.Backtest.FloatTol
	if len(compA) != len(compB) {
		t.Fatalf("len %d vs %d", len(compA), len(compB))
	}
	mapA := map[types.SecurityID]float64{}
	for _, c := range compA {
		mapA[c.TSCode] = c.Composite
	}
	for _, c := range compB {
		if !backtest.NearlyEqual(mapA[c.TSCode], c.Composite, tol) {
			t.Fatalf("parity fail %s: %v vs %v", c.TSCode, mapA[c.TSCode], c.Composite)
		}
	}
}

func cloneScores(in map[types.SecurityID]types.FactorScores) map[types.SecurityID]types.FactorScores {
	out := make(map[types.SecurityID]types.FactorScores, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func TestEndOfDayShanghai(t *testing.T) {
	td := types.NewTradeDate(2025, 1, 10)
	end := llm.EndOfDayShanghai(td)
	loc := time.FixedZone("CST", 8*3600)
	if end.Location().String() != loc.String() && end.UTC().Hour() != 15 {
		// CST = UTC+8; 23:59:59 CST = 15:59:59 UTC
	}
	if end.In(loc).Hour() != 23 {
		t.Fatalf("expected hour 23 CST, got %v", end.In(loc))
	}
}

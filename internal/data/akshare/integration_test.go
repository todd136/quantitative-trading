//go:build integration

package akshare_test

import (
	"os"
	"path/filepath"
	"testing"

	"quantitative-trading/internal/data/akshare"
	"quantitative-trading/internal/types"
)

// Live AKShare smoke — only when AKSHARE_LIVE=1 and akshare is installed.
func TestLiveAKShareSmoke(t *testing.T) {
	if os.Getenv("AKSHARE_LIVE") != "1" {
		t.Skip("set AKSHARE_LIVE=1 to run live AKShare integration test")
	}
	helper := filepath.Join("..", "..", "..", "scripts", "akshare_fetch.py")
	helper, _ = filepath.Abs(helper)
	p := akshare.New(akshare.Config{
		PythonBin:   "python3",
		HelperPath:  helper,
		SkipNetwork: false,
		CacheDir:    t.TempDir(),
	})
	from, _ := types.ParseTradeDate("2024-01-02")
	to, _ := types.ParseTradeDate("2024-01-10")
	bars, err := p.Bars("600000.SH", from, to)
	if err != nil {
		t.Fatalf("live bars: %v", err)
	}
	if len(bars) == 0 {
		t.Fatal("live bars empty")
	}
	t.Logf("live bars=%d first=%s close=%v", len(bars), bars[0].TradeDate, bars[0].Close)
}

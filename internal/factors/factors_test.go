package factors_test

import (
	"math"
	"path/filepath"
	"testing"

	"quantitative-trading/internal/config"
	"quantitative-trading/internal/data/fixture"
	"quantitative-trading/internal/factors"
	"quantitative-trading/internal/types"
)

func TestFinancialPITFutureAnnouncementInvisible(t *testing.T) {
	prov, err := fixture.New(filepath.Join("..", "..", "testdata", "fixtures"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := prov.Financials("600000.SH")
	if err != nil {
		t.Fatal(err)
	}
	// Signal before 2025-03-15 announcement of 2024 annual
	tSignal, _ := types.ParseTradeDate("2025-02-01")
	fin, ok := factors.GetLatestFinancial(rows, tSignal)
	if !ok {
		t.Fatal("expected some financial")
	}
	ann2024, _ := types.ParseTradeDate("2025-03-15")
	if fin.AnnouncementDate.Equal(ann2024) {
		t.Fatal("must not see announcement_date > T")
	}
	rp2024, _ := types.ParseTradeDate("2024-12-31")
	if fin.ReportPeriod.Equal(rp2024) {
		t.Fatal("must not use report_period with future announcement")
	}
	// After announcement, visible
	tAfter, _ := types.ParseTradeDate("2025-03-17")
	fin2, ok := factors.GetLatestFinancial(rows, tAfter)
	if !ok {
		t.Fatal("expected financial after announce")
	}
	if !fin2.AnnouncementDate.Equal(ann2024) {
		t.Fatalf("expected 2025-03-15 ann, got %s", fin2.AnnouncementDate)
	}
	// Net profit of future row is higher (1.2x) — ensure pre-announce value differs
	if fin.NetProfit == fin2.NetProfit {
		t.Fatal("PIT should yield different net_profit before vs after announce")
	}
}

func TestPipelineOrderMADNeutralZScore(t *testing.T) {
	cfg := config.Default()
	// Synthetic cross-section with outlier + two industries
	raw := []float64{1, 2, 3, 4, 100} // 100 is outlier
	inds := []string{"A", "A", "B", "B", "B"}
	// Step-by-step
	w := factors.WinsorizeMAD(raw, 5)
	if w[4] >= 100 {
		t.Fatalf("MAD should clip outlier, got %v", w[4])
	}
	n := factors.IndustryNeutralize(w, inds)
	// Within each industry mean ~0
	sumA, nA := 0.0, 0
	sumB, nB := 0.0, 0
	for i, ind := range inds {
		if ind == "A" {
			sumA += n[i]
			nA++
		} else {
			sumB += n[i]
			nB++
		}
	}
	if math.Abs(sumA/float64(nA)) > 1e-9 || math.Abs(sumB/float64(nB)) > 1e-9 {
		t.Fatalf("industry means not ~0: A=%v B=%v", sumA/float64(nA), sumB/float64(nB))
	}
	z := factors.CrossSectionalZScore(n)
	mu := 0.0
	for _, v := range z {
		mu += v
	}
	mu /= float64(len(z))
	if math.Abs(mu) > 1e-9 {
		t.Fatalf("z-score mean %v", mu)
	}
	// Full pipeline should match stepwise
	full := factors.ProcessCrossSection(raw, inds, cfg.Processing)
	for i := range z {
		if math.Abs(full[i]-z[i]) > 1e-9 {
			t.Fatalf("pipeline mismatch at %d: %v vs %v", i, full[i], z[i])
		}
	}
}

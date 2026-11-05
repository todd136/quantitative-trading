package akshare

import (
	"testing"

	"quantitative-trading/internal/types"
)

func TestReviewAdjAsOfSparseNoFuture(t *testing.T) {
	rows := []adjRowJSON{
		{TradeDate: "2024-10-18", AdjFactor: 25.39},
		{TradeDate: "2024-10-23", AdjFactor: 25.69},
	}
	// 10/22 should carry 10/18, not default 1 and not the future 10/23 factor.
	got, ok := adjFactorAsOf(rows, "2024-10-22")
	if !ok || got != 25.39 {
		t.Fatalf("10/22 as-of got %v ok=%v want 25.39", got, ok)
	}
	got, ok = adjFactorAsOf(rows, "2024-10-23")
	if !ok || got != 25.69 {
		t.Fatalf("10/23 got %v ok=%v", got, ok)
	}
	got, ok = adjFactorAsOf(rows, "2024-10-26")
	if !ok || got != 25.69 {
		t.Fatalf("10/26 must carry last factor, got %v ok=%v (not 1)", got, ok)
	}
	_, ok = adjFactorAsOf(rows, "2024-01-01")
	if ok {
		t.Fatal("no historical factor before first adj row")
	}
}

func TestReviewFillLimitsNoCloseFallback(t *testing.T) {
	b := types.Bar{PreClose: 0, Close: 10.5, HighLimit: 0, LowLimit: 0}
	fillConventionalLimits(&b, types.BoardSSEMain, false)
	if b.HighLimit != 0 || b.LowLimit != 0 {
		t.Fatalf("must not fill open limits from same-day close: %+v", b)
	}
}

// Doc §11 C: 000623.SZ sparse adj exact values (must as-of, never future, never default 1).
func TestReviewAdjAsOf000623Exact(t *testing.T) {
	rows := []adjRowJSON{
		{TradeDate: "2024-10-18", AdjFactor: 25.392813287206},
		{TradeDate: "2024-10-23", AdjFactor: 25.692302747539102},
	}
	got, ok := adjFactorAsOf(rows, "2024-10-22")
	if !ok || got != 25.392813287206 {
		t.Fatalf("000623 10/22 as-of got %v ok=%v want 25.392813287206", got, ok)
	}
	got, ok = adjFactorAsOf(rows, "2024-10-23")
	if !ok || got != 25.692302747539102 {
		t.Fatalf("000623 10/23 got %v ok=%v", got, ok)
	}
	got, ok = adjFactorAsOf(rows, "2024-10-26")
	if !ok || got != 25.692302747539102 {
		t.Fatalf("000623 10/26 must carry last factor, got %v ok=%v", got, ok)
	}
}

func TestReviewFillLimitsCloseChangeDoesNotAffectOpen(t *testing.T) {
	b1 := types.Bar{PreClose: 0, Close: 10.0, HighLimit: 0, LowLimit: 0}
	b2 := types.Bar{PreClose: 0, Close: 99.0, HighLimit: 0, LowLimit: 0}
	fillConventionalLimits(&b1, types.BoardSSEMain, false)
	fillConventionalLimits(&b2, types.BoardSSEMain, false)
	if b1.HighLimit != 0 || b2.HighLimit != 0 {
		t.Fatalf("both must stay unset; close must not seed open limits: b1=%+v b2=%+v", b1, b2)
	}
}

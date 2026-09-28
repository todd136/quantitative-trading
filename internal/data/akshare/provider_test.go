package akshare_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"quantitative-trading/internal/data/akshare"
	"quantitative-trading/internal/types"
)

func TestSkipNetwork(t *testing.T) {
	p := akshare.New(akshare.Config{SkipNetwork: true})
	_, err := p.LoadCalendar()
	if !errors.Is(err, akshare.ErrSkipped) {
		t.Fatalf("want ErrSkipped, got %v", err)
	}
	_, err = p.Bars("600000.SH", types.TradeDate{}, types.TradeDate{})
	if !errors.Is(err, akshare.ErrSkipped) {
		t.Fatalf("want ErrSkipped, got %v", err)
	}
	_, err = p.Securities()
	if !errors.Is(err, akshare.ErrSkipped) {
		t.Fatalf("want ErrSkipped, got %v", err)
	}
	_, err = p.PublicTexts("600000.SH", types.TradeDate{})
	if !errors.Is(err, akshare.ErrSkipped) {
		t.Fatalf("want ErrSkipped, got %v", err)
	}
}

func mockCfg(t *testing.T) akshare.Config {
	t.Helper()
	helper, err := filepath.Abs(filepath.Join("testdata", "mock_helper.py"))
	if err != nil {
		t.Fatal(err)
	}
	return akshare.Config{
		PythonBin:   "python3",
		HelperPath:  helper,
		SkipNetwork: false,
	}
}

func TestMockHelperParses(t *testing.T) {
	p := akshare.New(mockCfg(t))

	cal, err := p.LoadCalendar()
	if err != nil {
		t.Fatal(err)
	}
	if len(cal) != 3 {
		t.Fatalf("calendar len=%d", len(cal))
	}
	if cal[0].String() != "2024-01-02" {
		t.Fatalf("first date %s", cal[0])
	}

	secs, err := p.Securities()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range secs {
		if s.Board == types.BoardBSE || akshare.IsBSE(string(s.TSCode)) {
			t.Fatalf("BSE leaked: %+v", s)
		}
	}
	if len(secs) != 4 {
		t.Fatalf("want 4 securities after BSE filter, got %d", len(secs))
	}

	from, _ := types.ParseTradeDate("2024-01-02")
	to, _ := types.ParseTradeDate("2024-01-03")
	bars, err := p.Bars("600000.SH", from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(bars) != 2 {
		t.Fatalf("bars=%d", len(bars))
	}
	if bars[0].AdjFactor != 1.1 {
		t.Fatalf("adj merge want 1.1 got %v", bars[0].AdjFactor)
	}
	if !bars[1].Suspended {
		t.Fatal("expected suspended on zero volume bar")
	}
	if bars[0].Amount != 1e7 {
		t.Fatalf("amount=%v", bars[0].Amount)
	}

	st, err := p.ST()
	if err != nil {
		t.Fatal(err)
	}
	if len(st) != 1 || st[0].STType != "ST" {
		t.Fatalf("st=%+v", st)
	}

	inds, err := p.Industry()
	if err != nil {
		t.Fatal(err)
	}
	if len(inds) != 1 {
		t.Fatalf("industry=%d", len(inds))
	}

	fins, err := p.Financials("600000.SH")
	if err != nil {
		t.Fatal(err)
	}
	if len(fins) != 1 || fins[0].NetProfit != 1e9 {
		t.Fatalf("fins=%+v", fins)
	}

	mvs, err := p.MarketValues("600000.SH", from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(mvs) != 1 {
		t.Fatalf("mv=%d", len(mvs))
	}

	idx, err := p.IndexBars("000300.SH", from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx) != 1 {
		t.Fatalf("index=%d", len(idx))
	}
}

func TestBoardFilterHelpers(t *testing.T) {
	cases := []struct {
		code  string
		board types.Board
		ok    bool
		ts    string
		isBSE bool
	}{
		{"600000", types.BoardSSEMain, true, "600000.SH", false},
		{"000001", types.BoardSZSEMain, true, "000001.SZ", false},
		{"300001", types.BoardChiNext, true, "300001.SZ", false},
		{"301001", types.BoardChiNext, true, "301001.SZ", false},
		{"688001", types.BoardSTAR, true, "688001.SH", false},
		{"830799", types.BoardBSE, false, "", true},
		{"430047", types.BoardBSE, false, "", true},
		{"920001", types.BoardBSE, false, "", true},
	}
	for _, tc := range cases {
		b, ok := akshare.MapBoard(tc.code)
		if ok != tc.ok || (ok && b != tc.board) {
			t.Fatalf("%s: board=%s ok=%v want %s/%v", tc.code, b, ok, tc.board, tc.ok)
		}
		if akshare.IsBSE(tc.code) != tc.isBSE {
			t.Fatalf("%s IsBSE", tc.code)
		}
		if got := akshare.ToTSCode(tc.code); got != tc.ts {
			t.Fatalf("%s ts=%s want %s", tc.code, got, tc.ts)
		}
	}
}

func TestCacheOffline(t *testing.T) {
	dir := t.TempDir()
	// Seed calendar cache
	calJSON := []byte(`{"dates":["2024-06-03","2024-06-04"],"source":"seed"}`)
	if err := os.WriteFile(filepath.Join(dir, "calendar.json"), calJSON, 0o644); err != nil {
		t.Fatal(err)
	}
	p := akshare.New(akshare.Config{
		SkipNetwork: true,
		CacheDir:    dir,
	})
	cal, err := p.LoadCalendar()
	if err != nil {
		t.Fatal(err)
	}
	if len(cal) != 2 {
		t.Fatalf("cache calendar len=%d", len(cal))
	}
	// Uncached resource still skips
	_, err = p.ST()
	if !errors.Is(err, akshare.ErrSkipped) {
		t.Fatalf("want ErrSkipped for uncached, got %v", err)
	}
}

func TestFetchAndCache(t *testing.T) {
	dir := t.TempDir()
	cfg := mockCfg(t)
	cfg.CacheDir = dir
	p := akshare.New(cfg)
	raw, err := p.FetchAndCache(t.Context(), "calendar", "calendar")
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 10 {
		t.Fatalf("raw=%s", raw)
	}
	if _, err := os.Stat(filepath.Join(dir, "calendar.json")); err != nil {
		t.Fatal(err)
	}
	// Now SkipNetwork + cache should work
	p2 := akshare.New(akshare.Config{SkipNetwork: true, CacheDir: dir})
	cal, err := p2.LoadCalendar()
	if err != nil || len(cal) != 3 {
		t.Fatalf("cached load: %v len=%d", err, len(cal))
	}
}

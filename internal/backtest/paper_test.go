package backtest_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"quantitative-trading/internal/backtest"
	"quantitative-trading/internal/types"
)

func mustDate(t *testing.T, s string) types.TradeDate {
	t.Helper()
	d, err := types.ParseTradeDate(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestBuildPaperTargetsDeterministic(t *testing.T) {
	d1 := mustDate(t, "2024-01-02")
	d1e := mustDate(t, "2024-01-03")
	d2 := mustDate(t, "2024-01-09")
	d2e := mustDate(t, "2024-01-10")

	days := []backtest.PaperTargetDay{
		{
			SignalDate: d1,
			ExecDate:   d1e,
			Weights: map[types.SecurityID]float64{
				"AAA.SH": 0.5,
				"BBB.SZ": 0.5,
			},
			NAV: 1_000_000,
			Prices: map[types.SecurityID]float64{
				"AAA.SH": 10.0,
				"BBB.SZ": 20.0,
			},
		},
		{
			SignalDate: d2,
			ExecDate:   d2e,
			Weights: map[types.SecurityID]float64{
				"AAA.SH": 0.5,
				"CCC.SH": 0.5, // BBB exits, CCC enters
			},
			NAV: 1_000_000,
			Prices: map[types.SecurityID]float64{
				"AAA.SH": 10.0,
				"CCC.SH": 25.0,
			},
		},
	}

	res := backtest.BuildPaperTargets(days, 100)
	if len(res.Rows) != 5 {
		// day1: AAA ENTER, BBB ENTER; day2: AAA HOLD, BBB EXIT, CCC ENTER
		t.Fatalf("rows=%d want 5; %+v", len(res.Rows), res.Rows)
	}
	if !res.SharesAvailable {
		t.Fatal("expected shares available")
	}

	// Deterministic symbol order within each day.
	want := []struct {
		sig, sym, action string
		shares           float64
	}{
		{"2024-01-02", "AAA.SH", backtest.PaperActionEnter, 50000},    // 0.5*1e6/10 floored to 100
		{"2024-01-02", "BBB.SZ", backtest.PaperActionEnter, 25000},    // 0.5*1e6/20
		{"2024-01-09", "AAA.SH", backtest.PaperActionHold, 50000},
		{"2024-01-09", "BBB.SZ", backtest.PaperActionExit, 0}, // EXIT has newW=0 → no shares
		{"2024-01-09", "CCC.SH", backtest.PaperActionEnter, 20000}, // 0.5*1e6/25
	}
	for i, w := range want {
		r := res.Rows[i]
		if r.SignalDate != w.sig || r.Symbol != w.sym || r.Action != w.action {
			t.Fatalf("row %d: got %s %s %s want %s %s %s", i, r.SignalDate, r.Symbol, r.Action, w.sig, w.sym, w.action)
		}
		if w.action == backtest.PaperActionExit {
			if r.TargetShares != nil {
				t.Fatalf("EXIT should not set shares, got %v", *r.TargetShares)
			}
			continue
		}
		if r.TargetShares == nil || *r.TargetShares != w.shares {
			got := interface{}(nil)
			if r.TargetShares != nil {
				got = *r.TargetShares
			}
			t.Fatalf("row %d shares=%v want %v", i, got, w.shares)
		}
	}
}

func TestBuildPaperTargetsWeightsOnlyWhenNoPrices(t *testing.T) {
	d1 := mustDate(t, "2024-01-02")
	days := []backtest.PaperTargetDay{{
		SignalDate: d1,
		ExecDate:   mustDate(t, "2024-01-03"),
		Weights:    map[types.SecurityID]float64{"AAA.SH": 1.0},
		NAV:        0,
	}}
	res := backtest.BuildPaperTargets(days, 100)
	if res.SharesAvailable {
		t.Fatal("shares should be unavailable")
	}
	if len(res.Rows) != 1 || res.Rows[0].TargetShares != nil {
		t.Fatalf("expected blank shares: %+v", res.Rows)
	}
	joined := strings.Join(res.Notes, " ")
	if !strings.Contains(joined, "weights only") {
		t.Fatalf("note missing: %v", res.Notes)
	}
}

func TestWritePaperTargets(t *testing.T) {
	dir := t.TempDir()
	d1 := mustDate(t, "2024-01-02")
	res := backtest.BuildPaperTargets([]backtest.PaperTargetDay{{
		SignalDate: d1,
		ExecDate:   mustDate(t, "2024-01-03"),
		Weights: map[types.SecurityID]float64{
			"BBB.SZ": 0.4,
			"AAA.SH": 0.6,
		},
	}}, 0)
	csvPath, mdPath, err := backtest.WritePaperTargets(dir, res)
	if err != nil {
		t.Fatal(err)
	}
	csvB, err := os.ReadFile(csvPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(csvB)), "\n")
	if len(lines) != 3 { // header + 2
		t.Fatalf("csv lines=%d\n%s", len(lines), csvB)
	}
	if !strings.HasPrefix(lines[0], "signal_date,exec_date,symbol") {
		t.Fatalf("bad header %s", lines[0])
	}
	// AAA before BBB
	if !strings.Contains(lines[1], "AAA.SH") || !strings.Contains(lines[2], "BBB.SZ") {
		t.Fatalf("order:\n%s", csvB)
	}
	mdB, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mdB), "Not investment advice") {
		t.Fatal("md missing compliance notice")
	}
	if filepath.Base(csvPath) != "paper_targets.csv" || filepath.Base(mdPath) != "paper_targets.md" {
		t.Fatalf("names %s %s", csvPath, mdPath)
	}
}

func TestTargetsToWeightMap(t *testing.T) {
	m := backtest.TargetsToWeightMap([]types.TargetHolding{
		{TSCode: "A", Weight: 0.5},
		{TSCode: "B", Weight: 0.5},
		{TSCode: "C", Weight: 0},
	})
	if len(m) != 2 || m["A"] != 0.5 {
		t.Fatalf("%v", m)
	}
}

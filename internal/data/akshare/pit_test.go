package akshare_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"quantitative-trading/internal/data/akshare"
	"quantitative-trading/internal/types"
)

func writeCache(t *testing.T, dir, key string, obj any) {
	t.Helper()
	b, err := json.Marshal(obj)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, key+".json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestStrictPITRejectsProxyFinancialsFromCache(t *testing.T) {
	dir := t.TempDir()
	writeCache(t, dir, "financials_600000.SH", map[string]any{
		"symbol": "600000.SH",
		"source": "stock_financial_abstract",
		"note":   "announcement_date≈report_period",
		"rows": []map[string]any{{
			"ts_code":               "600000.SH",
			"report_period":         "2023-12-31",
			"announcement_date":     "2023-12-31",
			"announcement_is_proxy": true,
			"net_profit":            1e9,
			"revenue":               5e9,
			"gross_profit":          2e9,
			"total_assets":          5e10,
			"total_liabilities":     4e10,
			"equity":                1e10,
			"operating_cf":          8e8,
			"statement_type":        "consolidated",
		}},
	})
	p := akshare.New(akshare.Config{SkipNetwork: true, CacheDir: dir, StrictPIT: true})
	_, err := p.Financials("600000.SH")
	if !errors.Is(err, akshare.ErrStrictPIT) {
		t.Fatalf("want ErrStrictPIT, got %v", err)
	}

	pLoose := akshare.New(akshare.Config{SkipNetwork: true, CacheDir: dir, StrictPIT: false})
	rows, err := pLoose.Financials("600000.SH")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || !rows[0].AnnouncementIsProxy {
		t.Fatalf("non-strict should keep flagged proxy row, got %+v", rows)
	}
}

func TestStrictPITRejectsSnapshotMV(t *testing.T) {
	dir := t.TempDir()
	from, _ := types.ParseTradeDate("2024-01-02")
	to, _ := types.ParseTradeDate("2024-01-31")
	writeCache(t, dir, "mv_600000.SH", map[string]any{
		"symbol": "600000.SH",
		"source": "stock_individual_info_em",
		"note":   "snapshot only",
		"values": []map[string]any{{
			"trade_date":  "2024-01-02",
			"ts_code":     "600000.SH",
			"total_share": 1e9,
			"float_share": 8e8,
			"total_mv":    1e10,
			"float_mv":    8e9,
			"is_snapshot": true,
		}},
	})
	p := akshare.New(akshare.Config{SkipNetwork: true, CacheDir: dir, StrictPIT: true})
	_, err := p.MarketValues("600000.SH", from, to)
	if !errors.Is(err, akshare.ErrStrictPIT) {
		t.Fatalf("want ErrStrictPIT, got %v", err)
	}
	pLoose := akshare.New(akshare.Config{SkipNetwork: true, CacheDir: dir, StrictPIT: false})
	vals, err := pLoose.MarketValues("600000.SH", from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(vals) != 0 {
		t.Fatalf("snapshot MV must not backfill history, got %d", len(vals))
	}
}

func TestStrictPITRejectsBackfilledST(t *testing.T) {
	dir := t.TempDir()
	writeCache(t, dir, "st", map[string]any{
		"source": "stock_zh_a_st_em",
		"note":   "ST is a current snapshot (no historical entry/remove intervals)",
		"records": []map[string]any{{
			"ts_code":      "600002.SH",
			"entry_date":   "1990-01-01",
			"remove_date":  nil,
			"st_type":      "ST",
			"is_snapshot":  true,
			"pit_backfill": true,
		}},
	})
	p := akshare.New(akshare.Config{SkipNetwork: true, CacheDir: dir, StrictPIT: true})
	_, err := p.ST()
	if !errors.Is(err, akshare.ErrStrictPIT) {
		t.Fatalf("want ErrStrictPIT, got %v", err)
	}
}

func TestStrictPITRejectsBackfilledIndustry(t *testing.T) {
	dir := t.TempDir()
	writeCache(t, dir, "industry", map[string]any{
		"source": "stock_board_industry_cons_em",
		"note":   "effective_date stamped 2000-01-01 (no historical membership API)",
		"records": []map[string]any{{
			"ts_code":        "600000.SH",
			"industry_code":  "银行",
			"industry_name":  "银行",
			"effective_date": "2000-01-01",
			"expire_date":    nil,
			"source":         "EM_INDUSTRY",
			"pit_backfill":   true,
		}},
	})
	p := akshare.New(akshare.Config{SkipNetwork: true, CacheDir: dir, StrictPIT: true})
	_, err := p.Industry()
	if !errors.Is(err, akshare.ErrStrictPIT) {
		t.Fatalf("want ErrStrictPIT, got %v", err)
	}
}

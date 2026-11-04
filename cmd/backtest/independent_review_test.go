package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"quantitative-trading/internal/config"
)

func TestReviewAkshareDoesNotLoadFixtureCorpActions(t *testing.T) {
	fix := t.TempDir()
	cache := t.TempDir()
	if err := os.WriteFile(filepath.Join(fix, "corp_actions.json"), []byte(`[]`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Data.Provider = "akshare"
	cfg.Data.FixtureDir = fix
	cfg.Data.AKShareCacheDir = cache
	cfg.Data.StrictPIT = true
	path, loaded, err := resolveCorpActionsPath(cfg)
	if err == nil && loaded && strings.Contains(filepath.ToSlash(path), filepath.ToSlash(fix)) {
		t.Fatalf("akshare must not pick fixture empty file, got path=%s loaded=%v", path, loaded)
	}
	if loaded {
		t.Fatalf("no akshare corp_actions.json in cache; loaded should be false, path=%s", path)
	}
	if err == nil {
		t.Fatal("strict akshare without a provider-local snapshot must error (cannot default to no events)")
	}
}

func TestReviewExplicitCorpActionsPathMissingFails(t *testing.T) {
	cfg := config.Default()
	cfg.Data.Provider = "akshare"
	cfg.Data.CorpActionsPath = filepath.Join(t.TempDir(), "missing.json")
	cfg.Data.StrictPIT = false
	_, _, err := resolveCorpActionsPath(cfg)
	if err == nil {
		t.Fatal("explicit corp_actions_path must fail immediately when missing")
	}
}

func TestReviewFixtureProviderMayUseFixtureDir(t *testing.T) {
	fix := t.TempDir()
	p := filepath.Join(fix, "corp_actions.json")
	if err := os.WriteFile(p, []byte(`{"source":"fixture","provider":"fixture","coverage_start":"2023-01-01","coverage_end":"2025-12-31","complete":true,"events":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Data.Provider = "fixture"
	cfg.Data.FixtureDir = fix
	path, loaded, err := resolveCorpActionsPath(cfg)
	if err != nil || !loaded || path != p {
		t.Fatalf("fixture provider path=%s loaded=%v err=%v", path, loaded, err)
	}
}

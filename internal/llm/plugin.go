// Package llm defines the optional LlmAssistPlugin (§9). Default is Noop (enabled=false).
package llm

import (
	"context"
	"time"

	"quantitative-trading/internal/config"
	"quantitative-trading/internal/types"
)

// LlmFeature is re-exported alias for schema clarity at the plugin boundary.
type LlmFeature = types.LlmFeature

// LlmAssistPlugin annotates already-filtered candidates. Must not mutate universe/exec/cost.
type LlmAssistPlugin interface {
	Enabled() bool
	// Annotate returns auditable features; on failure/missing text return empty features (degrade).
	Annotate(ctx context.Context, signalDate types.TradeDate, candidates []types.SecurityID) ([]LlmFeature, error)
}

// NoopPlugin is used when llm.enabled=false. Annotate returns empty; no model I/O.
type NoopPlugin struct{}

func (NoopPlugin) Enabled() bool { return false }

func (NoopPlugin) Annotate(ctx context.Context, signalDate types.TradeDate, candidates []types.SecurityID) ([]LlmFeature, error) {
	return nil, nil
}

// NewPlugin returns Noop when disabled; otherwise a SchemaOnly stub (no network).
// Real model wiring is out of skeleton scope — TODO: attach vendor client when model_id locked.
func NewPlugin(cfg config.LLMConfig) LlmAssistPlugin {
	if !cfg.Enabled {
		return NoopPlugin{}
	}
	return &SchemaStubPlugin{Cfg: cfg}
}

// SchemaStubPlugin validates schema / PIT filtering without calling a real model.
// Annotate returns empty features (degrade) — sufficient for skeleton parity tests.
type SchemaStubPlugin struct {
	Cfg   config.LLMConfig
	Texts []types.PublicText // optional injected corpus for PIT unit tests
}

func (p *SchemaStubPlugin) Enabled() bool { return p.Cfg.Enabled }

func (p *SchemaStubPlugin) Annotate(ctx context.Context, signalDate types.TradeDate, candidates []types.SecurityID) ([]LlmFeature, error) {
	// Skeleton: do not invent scores. Return empty → degrade to three-factor.
	// PIT visibility is tested via FilterTextsByAsof.
	_ = ctx
	_ = candidates
	_ = signalDate
	return nil, nil
}

// EndOfDayShanghai returns end of calendar day T in Asia/Shanghai.
func EndOfDayShanghai(t types.TradeDate) time.Time {
	base := t.Time()
	loc := time.FixedZone("CST", 8*3600)
	y, m, d := base.In(loc).Date()
	return time.Date(y, m, d, 23, 59, 59, int(time.Second-time.Nanosecond), loc)
}

// FilterTextsByAsof keeps texts with asof_ts ≤ end of signal day T (Asia/Shanghai).
func FilterTextsByAsof(texts []types.PublicText, signalDate types.TradeDate) []types.PublicText {
	end := EndOfDayShanghai(signalDate)
	out := make([]types.PublicText, 0, len(texts))
	for _, tx := range texts {
		if tx.AsofTS.After(end) {
			continue
		}
		out = append(out, tx)
	}
	return out
}

// EmptyFeature is the degrade path when text is missing (§9.4).
func EmptyFeature(code types.SecurityID, signalDate types.TradeDate) LlmFeature {
	return LlmFeature{
		TSCode:         code,
		SignalDate:     signalDate,
		SentimentScore: nil,
		EventTags:      nil,
		Confidence:     0,
		SourceIDs:      nil,
	}
}

# Research: strategy iter-1

Branch: `research/strategy-iter-1` (local research only; do not treat as production freeze).

Not investment advice. Never invent Sharpe / returns / drawdowns — quote only from run artifacts (`report.json`, `metrics.csv`, `report.md`). Mark `TEMP DEGRADE` if any.

## Baseline

- Formal short reference (daily): `configs/akshare_short_formal.yaml`
- Research H1 config (weekly): `configs/research_strategy_iter_1.yaml`
- Production / example defaults stay **daily** per v1.2 (`configs/example.yaml` unchanged by this iter)

## H1 — weekly rebalance lowers turnover

**Hypothesis:** Switching `portfolio.rebalance_freq` from `daily` to `weekly` (`rebalance_weekday: 2`) reduces average two-way turnover and cost ratio on the same short calendar, without inventing new alpha (factor weights unchanged).

**Acceptance (same window A/B):**

1. Run daily control (existing formal short):
   ```bash
   go run ./cmd/backtest -config configs/akshare_short_formal.yaml \
     -out output/research_iter1_daily_control
   ```
2. Run weekly research baseline (distinct out dir; do not overwrite paper oneshot dirs):
   ```bash
   go run ./cmd/backtest -config configs/research_strategy_iter_1.yaml \
     -out output/research_iter1_weekly_h1 -paper
   ```
3. Compare **only** artifact fields (OOS / formal segment when present):
   - `turnover_daily_avg_two_way` (and ann if reported)
   - `cost_ratio`
   - OOS total / ann return, Sharpe, max DD — descriptive only, not a tuning target
4. Pass if weekly two-way turnover and cost_ratio are materially lower than daily on the same calendar **and** both runs finish without invented metrics (N/A stays N/A). Fail / revisit if weekly path errors or TEMP DEGRADE differs in a way that invalidates comparison.

**Paper list:** `paper_export.enabled: true` in the research config; CLI `-paper` also works. Weekly cadence emits targets only on rebalance signal days.

## Next hypotheses (not implemented yet)

- **H2:** Modest momentum vs value weight tilt (e.g. 1.2 / 0.8) — freeze H1 first; accept only on train/val, never tune on OOS.
- **H3:** Tighter or looser `industry_cap_ratio` vs current 0.15 — measure industry concentration + turnover, not invented alpha.

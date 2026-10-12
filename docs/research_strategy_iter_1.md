# Research: strategy iter-1

Branch: `research/strategy-iter-1` (local research only; do not treat as production freeze).

Not investment advice. Never invent Sharpe / returns / drawdowns — quote only from run artifacts (`report.json`, `metrics.csv`, `report.md`). Mark `TEMP DEGRADE` if any.

## Baseline

- Formal short reference (daily): `configs/akshare_short_formal.yaml`
- Research H1 config (weekly, equal factor weights 1/1/1): `configs/research_strategy_iter_1.yaml`
- Research H2 config (weekly + momentum/value tilt): `configs/research_strategy_iter_1_h2.yaml`
- Research H3 config (weekly + tighter industry cap; factors = H1): `configs/research_strategy_iter_1_h3.yaml`
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

## H2 — modest momentum vs value weight tilt

**Hypothesis:** On the H1 weekly baseline, tilting composite weights toward momentum and away from value (`momentum 1.0→1.2`, `value 1.0→0.8`, `quality` stays `1.0`) changes ranking enough to move holdings / turnover modestly, without rewriting factors. Accept only on train/val descriptive comparison; **never tune on OOS**.

**What changed (vs H1 only):**

| Knob | H1 | H2 |
|------|----|----|
| `factors.quality.weight` | 1.0 | 1.0 |
| `factors.value.weight` | 1.0 | **0.8** |
| `factors.momentum.weight` | 1.0 | **1.2** |
| `portfolio.rebalance_freq` | weekly | weekly (unchanged) |
| `paper_export.enabled` | true | true (unchanged) |

Config: `configs/research_strategy_iter_1_h2.yaml` (H1 YAML left intact for A/B).

**Acceptance (same window A/B vs H1):**

1. H1 control (if not already run):
   ```bash
   go run ./cmd/backtest -config configs/research_strategy_iter_1.yaml \
     -out output/research_iter1_weekly_h1 -paper
   ```
2. H2 tilt:
   ```bash
   go run ./cmd/backtest -config configs/research_strategy_iter_1_h2.yaml \
     -out output/research_iter1_weekly_h2 -paper
   ```
3. Compare **only** artifact fields — prefer train/val segments for any “did the tilt do something?” check; OOS is descriptive only:
   - holdings overlap / fill count / turnover / cost_ratio
   - train & val total return, Sharpe, max DD from report (quote, do not invent)
4. Pass if both runs complete cleanly (no invented metrics; N/A stays N/A) and H2 artifacts differ from H1 in a measurable way (scores/holdings/turnover). Fail / revisit if load fails, TEMP DEGRADE invalidates comparison, or changes look like noise-only with identical portfolios.

Do **not** retune weights using OOS numbers from this short calendar.

## H3 — tighter industry_cap_ratio (on H1 only)

**Hypothesis:** On the H1 weekly baseline (factor weights stay 1/1/1), tightening `portfolio.industry_cap_ratio` from **0.15 → 0.12** lowers peak SW_L1 concentration when top scores cluster in few industries. With `N=50`, `MaxPerIndustry = max(3, ceil(N * ratio))` moves **8 → 6**, so the portfolio must spread across more industries. That should show up in industry-distribution artifacts and may change turnover when forced substitutes replace capped names — without inventing new alpha.

**Why 0.12 (not 0.18):** One modest step **tighter** keeps the experiment directional and measurable (max/industry drops by 2 names). Looser 0.18 is deferred; stacking on H2 would confound factor tilt with cap — H3 is intentionally **H1-only** (single variable).

**What changed (vs H1 only):**

| Knob | H1 | H3 |
|------|----|----|
| `portfolio.industry_cap_ratio` | 0.15 | **0.12** |
| implied MaxPerIndustry (N=50) | 8 | **6** |
| factor weights | 1/1/1 | 1/1/1 (unchanged) |
| `portfolio.rebalance_freq` | weekly | weekly (unchanged) |
| `paper_export.enabled` | true | true (unchanged) |

Config: `configs/research_strategy_iter_1_h3.yaml` (H1 / H2 YAMLs left intact).

**Acceptance (same window A/B vs H1):**

1. H1 control (if not already run):
   ```bash
   go run ./cmd/backtest -config configs/research_strategy_iter_1.yaml \
     -out output/research_iter1_weekly_h1 -paper
   ```
2. H3 tighter cap:
   ```bash
   go run ./cmd/backtest -config configs/research_strategy_iter_1_h3.yaml \
     -out output/research_iter1_weekly_h3 -paper
   ```
3. Compare **only** artifact fields — prefer industry distribution / concentration and train/val descriptive metrics; **do not tune on OOS**:
   - industry time series / max industry weight or count-per-industry from report (when Available)
   - turnover / cost_ratio / fill count / holdings overlap vs H1
   - train & val total return, Sharpe, max DD from report (quote, do not invent)
4. Pass if both runs complete cleanly (N/A stays N/A; no invented numbers) and H3 shows **lower** peak industry concentration than H1 when industry mapping is present. Fail / revisit if load fails, TEMP DEGRADE invalidates comparison, or industry series is N/A for both (then only turnover/holdings diffs are descriptive, not acceptance of the cap hypothesis).

Do **not** retune `industry_cap_ratio` using OOS numbers from this short calendar.

## Next hypotheses (not implemented yet)

- **H4:** Looser `industry_cap_ratio` (e.g. 0.18) as the opposite one-variable A/B vs H1 — or modest quality-weight tilt on H1 — still quote artifacts only; no OOS tuning.

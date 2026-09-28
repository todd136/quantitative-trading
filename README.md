# A-share daily multifactor backtest skeleton (v1.2)

Go skeleton implementing [规则说明书 v1.2](../output/A股日频多因子筛选_规则说明书_v1.2.md): universe hard filters (STAR in, BSE out), Quality/Value/Momentum + MAD→industry-neutral→z-score, Composite/SelectHoldings, T+1 exec/costs/limits, fixture + AKShare adapters, LLM plugin schema **default OFF** (Noop), **§6 metrics export + Train/Val/OOS sample split**.

**Not investment advice. No fabricated backtest returns / Sharpe / drawdown.** Metrics are computed only from real equity/turnover curves; unavailable metrics are marked `available: false` (N/A) — never fake zeros presented as results.

## Test (offline — default CI)

```bash
cd /workspace/quantitative-trading
go test ./...
```

Live AKShare integration (optional; needs network + `pip install akshare`):

```bash
AKSHARE_LIVE=1 go test -tags=integration ./internal/data/akshare/...
```

## Config

- Defaults: `internal/config.Default()` (appendix A)
- Example YAML: `configs/example.yaml`
- Spec / report stamp: `v1.2-20260928`
- `llm.enabled: false` by default; when false, Composite matches pure three-factor path

### Sample split (Train / Validate / OOS)

Per rule book §6.3, time-forward split (dates are inclusive/exclusive as below):

```yaml
backtest:
  start_date: "2018-01-01"
  end_date: "2024-12-31"
  sample_split:
    train_end: "2020-12-31"   # train: [start, train_end]
    val_end: "2022-12-31"     # validate: (train_end, val_end]
    # OOS / test_oos: (val_end, end_date]  — formal report segment
  # walk_forward:             # optional skeleton only (not fully executed yet)
  #   train_years: 3
  #   test_years: 1
```

Validation: `train_end < val_end < end_date` (when end is set). Fixture demo in `configs/example.yaml` uses a short 2025-03 window because fixture calendars are short — unit tests use synthetic NAV sequences.

### Parameter freeze policy (OOS)

- **OOS must not be used for tuning.** Train/validate only for research; any param change after freeze **bumps `version`** in YAML.
- Formal report segment is `oos` when `sample_split` is set; otherwise `full`.
- Reports embed a frozen YAML config dump + this freeze note.

### Switch data provider to AKShare

```yaml
data:
  provider: "akshare"
  akshare_python: "python3"
  akshare_helper: "scripts/akshare_fetch.py"
  akshare_cache_dir: "testdata/akshare_cache"
```

With a populated `akshare_cache_dir`, Load* can run offline (cache hit). Cache miss invokes the Python helper unless you set `SkipNetwork` in code.

## AKShare install & fetch helper

```bash
python3 -m pip install akshare pandas
python3 scripts/akshare_fetch.py ping
```

Example fetches (JSON to stdout or `--out`):

```bash
python3 scripts/akshare_fetch.py calendar --out testdata/akshare_cache/calendar.json
python3 scripts/akshare_fetch.py securities --out testdata/akshare_cache/securities.json
python3 scripts/akshare_fetch.py bars --symbol 600000.SH --start 2024-01-01 --end 2024-01-31 \
  --out testdata/akshare_cache/bars_600000.SH.json
python3 scripts/akshare_fetch.py adj --symbol 600000.SH --out testdata/akshare_cache/adj_600000.SH.json
python3 scripts/akshare_fetch.py st --out testdata/akshare_cache/st.json
python3 scripts/akshare_fetch.py industry --limit 10 --out testdata/akshare_cache/industry.json
python3 scripts/akshare_fetch.py financials --symbol 600000.SH \
  --out testdata/akshare_cache/financials_600000.SH.json
python3 scripts/akshare_fetch.py mv --symbol 600000.SH --out testdata/akshare_cache/mv_600000.SH.json
python3 scripts/akshare_fetch.py index --symbol 000300.SH --start 2024-01-01 --end 2024-01-31 \
  --out testdata/akshare_cache/index_000300.SH.json
```

**Notes**

- Live China market data quality/availability varies by AKShare version and upstream sites.
- Fixture remains the CI default (`data.provider: fixture`).
- ST / suspend / daily MV / full financials are **best-effort**; helper returns empty lists + `note` when APIs are missing or unstable (no crash).
- BSE (`8xxxxx` / `4xxxxx` / `BJ`) is filtered out in helper and Go `MapBoard`.

## Data providers

| Provider | Use |
|----------|-----|
| `fixture` (default) | CSV/JSON under `testdata/fixtures` — used by unit tests |
| `akshare` | Python helper `scripts/akshare_fetch.py` + optional JSON `akshare_cache_dir`. Offline unit tests use mock helper / `SkipNetwork`. |

## Run fixture backtest

```bash
cd /workspace/quantitative-trading
go run ./cmd/backtest -config configs/example.yaml -out output/backtest_run
```

Writes under `-out` (default `output/backtest_run/`):

| File | Contents |
|------|----------|
| `nav.csv` | Daily NAV / cash |
| `fills.csv` | Executed trades |
| `report.json` | Full §6 report: spec version, frozen config, per-segment metrics |
| `metrics.csv` | Flattened metric rows (`available` / value / note) |
| `report.md` | Human-readable header + tables (no invented numbers) |

CLI may print a **fixture demo** total return for the formal segment — that value is computed from the short fixture run's NAV, labeled as fixture demo, **not** live market results.

### Metric conventions

- **Annualization**: 252 trading days/year for return, vol, Sharpe, turnover.
- **Sharpe**: uses `backtest.risk_free` (annual; default 0).
- **Calmar**: `ann_return / |max_drawdown|`; **N/A** if max DD is 0.
- **Turnover**: `DailySnapshot.Turnover` = **two-way** (buy+sell notional / NAV). One-way = two-way / 2. Ann = daily avg × 252. Derived from fills when snaps lack Turnover.
- **Cost ratio**: sum of daily `CostTotal` / starting NAV.
- **Benchmark-relative**: computed only when aligned `IndexBars` are provided; otherwise `available: false`.
- **Monthly win rate**: N/A if fewer than 2 calendar months of NAV.

### §6.2 coverage vs stubs (N/A)

| Item | Status |
|------|--------|
| Cumulative / ann return, ann vol, Sharpe, max DD, Calmar | Implemented |
| Daily / monthly win rate | Implemented |
| Turnover one-way & two-way (daily avg + ann) | Implemented (two-way from snaps/fills) |
| Cost ratio | Implemented |
| Excess return, tracking error, IR, excess max DD | Implemented when benchmark series present |
| Avg holdings count, max single-name weight | Implemented when Holdings/Weights on snaps |
| Limit/suspend unfilled skip rate | **N/A stub** (engine does not yet count skips) |
| Industry distribution time series | **N/A stub** |
| Factor exposure time series | **N/A stub** |
| Brinson attribution | **N/A stub** |
| Factor long-short / layer returns | **N/A stub** |
| Per-year breakdown tables | Not yet (overall / per-segment only) |
| Walk-forward multi-window execution | Skeleton config only |
| LLM coverage / degrade stats | N/A until LLM path enabled with counters |

## Layout

```
cmd/backtest/          CLI → nav/fills + report.json/metrics.csv/report.md
configs/example.yaml
internal/
  types/ config/ data/{fixture,akshare}/
  universe/ factors/ portfolio/ backtest/ llm/
testdata/fixtures/
scripts/akshare_fetch.py
```

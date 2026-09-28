# A-share daily multifactor backtest skeleton (v1.2)

Go skeleton implementing [规则说明书 v1.2](../output/A股日频多因子筛选_规则说明书_v1.2.md): universe hard filters (STAR in, BSE out), Quality/Value/Momentum + MAD→industry-neutral→z-score, Composite/SelectHoldings, T+1 exec/costs/limits, fixture + AKShare adapters, LLM plugin schema **default OFF** (Noop).

**Not investment advice. No fabricated backtest returns / Sharpe / drawdown.**

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
- `llm.enabled: false` by default; when false, Composite matches pure three-factor path

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

Prints paths to `nav.csv` / `fills.csv`. Does **not** print claimed performance numbers.

## Layout

```
cmd/backtest/          minimal CLI (selects fixture|akshare from YAML)
configs/example.yaml
internal/
  types/ config/ data/{fixture,akshare}/
  universe/ factors/ portfolio/ backtest/ llm/
testdata/fixtures/
scripts/akshare_fetch.py
```

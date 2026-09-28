# A-share daily multifactor backtest skeleton (v1.2)

Go skeleton implementing [规则说明书 v1.2](../output/A股日频多因子筛选_规则说明书_v1.2.md): universe hard filters (STAR in, BSE out), Quality/Value/Momentum + MAD→industry-neutral→z-score, Composite/SelectHoldings, T+1 exec/costs/limits, fixture + AKShare stub adapters, LLM plugin schema **default OFF** (Noop).

**Not investment advice. No fabricated backtest returns / Sharpe / drawdown.**

## Test

```bash
cd /workspace/quantitative-trading
go test ./...
```

## Config

- Defaults: `internal/config.Default()` (appendix A)
- Example YAML: `configs/example.yaml`
- `llm.enabled: false` by default; when false, Composite matches pure three-factor path

## Data providers

| Provider | Use |
|----------|-----|
| `fixture` (default) | CSV/JSON under `testdata/fixtures` — used by unit tests |
| `akshare` | Compile-time stub; optional Python helper `scripts/akshare_fetch.py` or HTTP sidecar. Set `SkipNetwork: true` offline. |

## Run fixture backtest

```bash
cd /workspace/quantitative-trading
go run ./cmd/backtest -config configs/example.yaml -out output/backtest_run
```

Prints paths to `nav.csv` / `fills.csv`. Does **not** print claimed performance numbers.

## Layout

```
cmd/backtest/          minimal CLI
configs/example.yaml
internal/
  types/ config/ data/{fixture,akshare}/
  universe/ factors/ portfolio/ backtest/ llm/
testdata/fixtures/
scripts/akshare_fetch.py
```

# P1 第十节独立复核判定（相对 ac8ce72）

## 结论摘要

第十节 A–F **全部为真问题**，且均已在同一分支修复。整体「通过」的验收笔记不可采信：修复前 **不宜做正式排名**。优先项 A/B/C/D（实盘空文件、正式缺价放行、稀疏复权、收盘推开盘限制价）会直接污染正式结果；E/F 会错账与错费用率。

修复后仍须：正式 AKShare 跑批提供带 `source` / `coverage_*` / `complete` 的公司行动快照；显式打开 `data.strict_quotes: true`；旧行情缓存应重拉（限制价来源字段）。KEEP_DEFAULT 未改。未编造收益数字。

## 逐条判定

| ID | 判定 | 复现要点 | 处理 |
|---|---|---|---|
| **A** | **真问题，已修** | `resolveCorpActionsPath` 不按 provider 分流；Default `FixtureDir=testdata/fixtures`，该目录 `corp_actions.json` 为 `[]`。`akshare_short_formal.yaml` 仍带 `fixture_dir`。显式路径缺失时被静默跳过。 | 按 provider 选路径；显式路径缺失立即失败；akshare 禁止 fixture 兜底；akshare 快照必须含来源/覆盖/完整性 |
| **B** | **真问题，已修** | `quoteClose` 缺 bar 且 `StrictPIT=true` 仍沿用旧收盘并返回；`WriteSnapshots` 只写 `trade_date,nav,cash` | 新增独立开关 `data.strict_quotes`；正式失败未知缺价；诊断模式写 `valuation_marks.csv`（价格日期、原因、市值、占比）及 nav 的 receivable |
| **C** | **真问题，已修** | `provider.go` `adjMap[b.TradeDate]` 精确匹配，未命中则 bar 上的 1.0。稀疏 adj 会在无行日掉回 1 | `adjFactorAsOf`：取 `trade_date<=bar` 的最后因子，禁止未来因子；无历史则保留 bar 因子 |
| **D** | **真问题，已修** | P1 只改了 `universe.EnsureLimitPrices`；`fillConventionalLimits` 与 `akshare_fetch.py` 仍 `pre else close` | 全链路去掉收盘回退；`limit_source`；非 official 的旧正数限制价按 PreClose 重算，PreClose 缺失则清零 |
| **E** | **真问题，已修** | `applyOpenCredits` 从全局 `state.Receivable` 扣款。A 应收 100、B 除息即派 20 会扣走 A 的 20 | 每笔 `ReceivableOutstanding` 只冲本事件 |
| **F** | **真问题，已修** | `metricSnaps`（含基准日）交给 `ComputeMetricsFull` 全部统计。基准费用 7 + 本期 2 → 费用率 9% | `MetricsInput.Baseline` 只用于收益路径；费用/换手/NDays/行业分布用区间观测 |

仓库原先没有 `independent_review_test.go` / `TestReview*`；已按第十节意图补回归。

## 修复

- 分支：`research/fix-backtest-p0-nav-pit-exec`（不合 main）
- tip SHA：`e58d4d1dee21367ba6972ac469fcd4f431709878`（相对基线 `ac8ce72`）
- 改动路径：`cmd/backtest/main.go`，`internal/backtest/{engine,corpact,metrics,report,section62}.go`，`internal/config/config.go`，`internal/types/types.go`，`internal/data/akshare/{provider,schema}.go`，`scripts/akshare_fetch.py`，`testdata/fixtures/corp_actions.json`，`configs/example.yaml`，`configs/akshare_short_formal.yaml`，`internal/backtest/independent_review_test.go`，`cmd/backtest/independent_review_test.go`，`internal/data/akshare/asof_internal_test.go`

### 测试

```
go test ./internal/backtest ./cmd/backtest -run TestReview -count=1
go test ./... -count=1
```

结果：`TestReview` 9 项通过；`go test ./... -count=1` 全部通过。

未修项：无（A–F 均已改）。采集/校验/回测只读快照分离与 AI 成分表仍不在第十节范围。

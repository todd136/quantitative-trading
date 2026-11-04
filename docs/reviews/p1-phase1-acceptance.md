# P1 第一阶段验收报告

分支：`research/fix-backtest-p0-nav-pit-exec`（P0 引擎核心仍为 `e98bbc9`）。本阶段不合并 `main`。KEEP_DEFAULT 未改（Q/V/M=1/1/1、N=50、industry_cap_ratio=0.15、周频口径保持原配置）。

## 1. 变更说明

行为变化（相对 `6626e1c`）：

- **停牌 vs 缺行情**：`quoteClose` / `quoteOpen` 仅在 `bar.Suspended` 或显式停牌日历时记 `HaltValued`；缺 bar 且未确认停牌时沿用最近收盘估值，记 `MissingValued` / `CarryMarks.reason=missing_bar`，持仓不从净值消失。
- **成交跳过**：缺 bar 记 `SkippedMissingPrice`，不再记 `SkippedSuspend`。`TestMissingBarSkipsFillAsHalt` 已按此调整。
- **公司行动**：支持 `profit_distribution` / `cash_dividend` / `bonus_share` / `capital_transfer`。区分登记日 / 除权除息日 / 现金到账日 / 新增股份可交易日。成交价用原始 Open，不用 `CloseAdj`。现金按税前每股派现、金额舍入 0.01 元。
- **严格模式**：未加载公司行动文件不得默认为无事件；`adj_factor=1` 不能当作无事件；复权因子跳动但无匹配除权日事件则失败；未实现公司行动（配股等）失败；缺可靠开盘涨跌停（且禁止用当日收盘反推）则拦截。
- **区间收益**：`FilterSnapsWithBaseline` 把 Exclusive 当日净值作为期初基准，再算段内收益（6/30=100, 7/1=90, 7/2=99 → 约 -1%）。
- **执行选项**：引擎实际为跳过并留现金。`rolling_unfilled=true`、`volume_constraint_enabled=true`、`buy_limit_handler≠skip` 在 `config.Validate` 报错。Default / 仓库 YAML 改为 `false` / `skip`（与真实行为一致）。

支持的公司行动与限制：见上。不支持配股、合并、分拆、可转债等；出现则阻止正式结果。

## 2. 小样本包（真实 vs 构造分开）

| 目录 | 性质 | 命令 |
|---|---|---|
| `testdata/samples/synthetic/cash_div_transfer/` | 构造；独立账本 `expected_ledger.json`（非引擎生成） | `go test ./internal/backtest -count=1 -run TestSyntheticCashDivTransferLedger` |
| `testdata/samples/synthetic/staggered_cash_shares/` | 构造；现金与转增可交易日错开 | `go test ./internal/backtest -count=1 -run TestStaggeredCashAndSharesLedger` |
| `testdata/samples/real/688059_202607/` | 真实公告事实（无构造 K 线） | `go test ./internal/backtest -count=1 -run 'TestReal688059EventFacts\|TestReal688059EventAccounting'` |

舍入 0.01 元，样本容差 0.01。说明见 `testdata/samples/README.md`。

全量本阶段测试：

```
go test ./internal/backtest ./internal/config ./internal/universe ./internal/factors -count=1
go test ./... -count=1
```

## 3. 验收分项

| 项 | 结果 | 说明 |
|---|---|---|
| 任务一：冻结小样本 + 独立预期账本 | 通过 | JSON 标记 `independent=true`、`generated_by_engine=false`；测试拒绝引擎生成账本 |
| 任务二：分红送转记账（688059 事实 + 构造边界） | 通过 | 登记日前卖出无权益；除权日买入无权益；登记日后卖出保留已确认权益；错开到账；重复事件失败 |
| 任务三：开盘成交约束 | 通过 | T+1、涨跌停、无有效开盘价、缺 bar 不填单；PreClose-only 限制价；严格模式缺涨跌停拦截 |
| 任务四：财报时点 / 缺公司行动失败 | 通过 | `ListFinancialGaps` 列出 announce≈period；严格模式未加载公司行动失败；adj=1 仍记账 |
| 任务五：逐日净值 / 费用一次 / 缺价估值 | 通过 | 含末日；`TestCostsCountedOnce`；缺价不丢持仓 |
| 缺陷1：缺 bar ≠ 确认停牌 | 通过 | `HaltValued` vs `MissingValued` |
| 缺陷2：FilterSnaps 期初基准 | 通过 | 约 -1% 而非 +10% |
| 缺陷3：`TestMissingBarSkipsFillAsHalt` | 通过 | `SkippedMissingPrice`，`SkippedSuspend=0` |
| review：未实现执行选项配置时报错 | 通过 | Validate + Default/YAML 对齐 skip |
| 采集/校验/回测只读快照分离 | 未执行 | 本阶段小样本不依赖全市场采集拆分 |
| AI 历史成分表 | 未执行 | 与小样本验收无关，按规格延后 |
| 全市场第二阶段数据 | 未执行 | 按任务书不做 |

未编造回测收益/Sharpe；未荐股。

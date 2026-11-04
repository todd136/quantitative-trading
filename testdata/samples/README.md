# P1 小样本包（真实 vs 构造分开）

预期账本为手工独立计算，**不得**用被测回测引擎生成。金额舍入 0.01 元（分），样本绝对值容差 0.01。

不做荐股；本目录数字只用于核对记账与约束，不是收益宣称。

## 构造样本 `synthetic/`

现金分红 + 资本公积转增，复权因子全程为 1（**adj_factor=1 不能当作无事件**）。

```
go test ./internal/backtest -count=1 -run 'TestSyntheticCashDivTransferLedger|TestStaggeredCashAndSharesLedger'
```

- `synthetic/cash_div_transfer/`：登记日与除权/到账/可交易日相邻；理论除权价使净值连续。
- `synthetic/staggered_cash_shares/`：现金到账日与新增股份可交易日晚于除权日。

## 真实事件 `real/688059_202607/`

华锐精密 688059 2026-07-17 利润分配（登记日 2026-07-16）。本目录只冻结公告事实，不含构造行情。

```
go test ./internal/backtest -count=1 -run 'TestReal688059EventFacts|TestReal688059EventAccounting'
```

## 其它本阶段单测

```
go test ./internal/backtest ./internal/config ./internal/universe ./internal/factors -count=1
```

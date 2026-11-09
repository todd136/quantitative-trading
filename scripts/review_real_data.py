#!/usr/bin/env python3
"""Offline cross-source audit of the sourced 000623 sample; never repairs raw data."""
import datetime
import json
import pathlib
import sys

root = pathlib.Path(__file__).resolve().parents[1]
sample = root / 'testdata/samples/real/000623_202610'
sina = json.loads((sample/'bars_raw.json').read_text())['bars']
eastmoney = json.loads((sample/'eastmoney_raw.json').read_text())
em = {datetime.datetime.fromtimestamp(r['日期']/1000, datetime.timezone.utc).date().isoformat(): r for r in eastmoney}
adj = json.loads((sample/'adj.json').read_text())['adj']
rows = []
for r in sina:
    day = r['trade_date']
    other = em[day]
    diff = {k: r[k]-other[c] for k,c in [('open','开盘'),('high','最高'),('low','最低'),('close','收盘')]}
    eligible = [f for f in adj if f['trade_date']<=day and f['adj_factor']>0]
    factor = max(eligible,key=lambda f:f['trade_date']) if eligible else None
    rows.append(dict(date=day, ohlc_diff=diff, factor_asof=factor,
                     volume_difference_shares=r['volume']-other['成交量']*100,
                     amount_difference=r['amount']-other['成交额'],
                     reliable_limits_present=r['high_limit']>0 and r['low_limit']>0 and r['limit_source']!='none'))
assert len(rows)==7 and all(all(abs(v)<1e-9 for v in r['ohlc_diff'].values()) for r in rows)
assert all(r['factor_asof'] is not None for r in rows)
ex=em['2026-10-23']
result=dict(ohlc_crosscheck='PASS', historical_adj_coverage='PASS', rows=rows,
            ex_date_close_minus_change=ex['收盘']-ex['涨跌额'],
            formal_ready=False,
            blockers=['raw limits and ex-date reference not verified',
                      'PIT financials/market values and historical universe statuses not yet accepted',
                      'full real-data selection/execution CLI not yet accepted'])
print(json.dumps(result,ensure_ascii=False,indent=2))
# Passing these checks is explicitly not formal readiness.
sys.exit(0)

#!/usr/bin/env python3
"""Controlled upstream rows; exercise the real cmd_bars without network/pandas."""
import argparse
import importlib.util
import json
import os
import pathlib
import sys
from types import SimpleNamespace

path=pathlib.Path(__file__).resolve().parents[4]/'scripts/akshare_fetch.py'
spec=importlib.util.spec_from_file_location('actual_fetch',path)
mod=importlib.util.module_from_spec(spec);spec.loader.exec_module(mod)
class Frame:
    empty=False
    columns=['date','open','high','low','close','volume','amount']
    def iterrows(self):
        yield 0, dict(date='2026-10-22',open=10,high=11,low=9,close=float(os.environ.get('REVIEW_CLOSE','10')),volume=10000,amount=100000)
if sys.argv[1]=='adj':
    print(json.dumps({'adj':[{'trade_date':'2026-10-01','adj_factor':1}]}))
else:
    mod._import_ak=lambda: SimpleNamespace(stock_zh_a_daily=lambda **kw:Frame())
    p=argparse.ArgumentParser();p.add_argument('command');p.add_argument('--symbol');p.add_argument('--start');p.add_argument('--end');p.add_argument('--out',default=None)
    raise SystemExit(mod.cmd_bars(p.parse_args()))

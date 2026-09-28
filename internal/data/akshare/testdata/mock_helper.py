#!/usr/bin/env python3
"""Offline mock of scripts/akshare_fetch.py for Go unit tests."""
from __future__ import annotations

import argparse
import json
import sys


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--out")
    sub = ap.add_subparsers(dest="cmd")

    sub.add_parser("ping")
    sub.add_parser("calendar")
    sub.add_parser("securities")
    sub.add_parser("st")
    sub.add_parser("suspend")
    p = sub.add_parser("industry")
    p.add_argument("--limit", type=int, default=0)
    p = sub.add_parser("bars")
    p.add_argument("--symbol", required=True)
    p.add_argument("--start", required=True)
    p.add_argument("--end", required=True)
    p = sub.add_parser("adj")
    p.add_argument("--symbol", required=True)
    p = sub.add_parser("financials")
    p.add_argument("--symbol", required=True)
    p = sub.add_parser("mv")
    p.add_argument("--symbol", required=True)
    p.add_argument("--start", default="")
    p.add_argument("--end", default="")
    p = sub.add_parser("index")
    p.add_argument("--symbol", required=True)
    p.add_argument("--start", required=True)
    p.add_argument("--end", required=True)

    args = ap.parse_args()
    cmd = args.cmd
    if cmd == "ping":
        obj = {"ok": True, "akshare_version": "mock", "note": "mock helper"}
    elif cmd == "calendar":
        obj = {"dates": ["2024-01-02", "2024-01-03", "2024-01-04"], "source": "mock"}
    elif cmd == "securities":
        # Include a BSE code that Go must filter out
        obj = {
            "securities": [
                {"ts_code": "600000.SH", "name": "PFYH", "board": "SSE_MAIN", "list_date": "1999-11-10", "delist_date": None},
                {"ts_code": "000001.SZ", "name": "PAYH", "board": "SZSE_MAIN", "list_date": "1991-04-03", "delist_date": None},
                {"ts_code": "300001.SZ", "name": "TRD", "board": "CHINEXT", "list_date": "2009-10-30", "delist_date": None},
                {"ts_code": "688001.SH", "name": "HRWJ", "board": "STAR", "list_date": "2019-07-22", "delist_date": None},
                {"ts_code": "830799.BJ", "name": "BSE_X", "board": "BSE", "list_date": "2020-01-01", "delist_date": None},
                {"ts_code": "430047.BJ", "name": "BSE_Y", "board": "BSE", "list_date": "2020-01-01", "delist_date": None},
            ],
            "excluded_bse_or_unknown": 0,
            "source": "mock",
        }
    elif cmd == "bars":
        sym = args.symbol
        obj = {
            "symbol": sym,
            "bars": [
                {
                    "trade_date": "2024-01-02",
                    "ts_code": sym,
                    "open": 10.0,
                    "high": 10.5,
                    "low": 9.8,
                    "close": 10.2,
                    "volume": 1e6,
                    "amount": 1e7,
                    "pre_close": 10.0,
                    "adj_factor": 1.0,
                    "suspended": False,
                },
                {
                    "trade_date": "2024-01-03",
                    "ts_code": sym,
                    "open": 10.2,
                    "high": 10.6,
                    "low": 10.0,
                    "close": 10.4,
                    "volume": 0.0,
                    "amount": 0.0,
                    "pre_close": 10.2,
                    "adj_factor": 1.0,
                    "suspended": True,
                },
            ],
            "source": "mock",
        }
    elif cmd == "adj":
        obj = {
            "symbol": args.symbol,
            "adj": [
                {"trade_date": "2024-01-02", "adj_factor": 1.1},
                {"trade_date": "2024-01-03", "adj_factor": 1.1},
            ],
            "source": "mock",
        }
    elif cmd == "st":
        obj = {
            "records": [
                {
                    "ts_code": "600002.SH",
                    "entry_date": "2024-01-01",
                    "remove_date": None,
                    "st_type": "ST",
                }
            ],
            "note": "mock",
            "source": "mock",
        }
    elif cmd == "suspend":
        obj = {"records": [], "note": "mock empty", "source": "mock"}
    elif cmd == "industry":
        obj = {
            "records": [
                {
                    "ts_code": "600000.SH",
                    "industry_code": "801780",
                    "industry_name": "银行",
                    "effective_date": "2020-01-01",
                    "expire_date": None,
                    "source": "SW_L1",
                }
            ],
            "note": "",
            "source": "mock",
        }
    elif cmd == "financials":
        obj = {
            "symbol": args.symbol,
            "rows": [
                {
                    "ts_code": args.symbol,
                    "report_period": "2023-12-31",
                    "announcement_date": "2024-03-30",
                    "net_profit": 1e9,
                    "revenue": 5e9,
                    "gross_profit": 2e9,
                    "total_assets": 5e10,
                    "total_liabilities": 4e10,
                    "equity": 1e10,
                    "operating_cf": 8e8,
                    "statement_type": "consolidated",
                }
            ],
            "note": "",
            "source": "mock",
        }
    elif cmd == "mv":
        obj = {
            "symbol": args.symbol,
            "values": [
                {
                    "trade_date": "2024-01-02",
                    "ts_code": args.symbol,
                    "total_share": 1e9,
                    "float_share": 8e8,
                    "total_mv": 1e10,
                    "float_mv": 8e9,
                }
            ],
            "note": "",
            "source": "mock",
        }
    elif cmd == "index":
        obj = {
            "index_code": args.symbol,
            "bars": [
                {"trade_date": "2024-01-02", "index_code": args.symbol, "open": 4000.0, "close": 4010.0}
            ],
            "source": "mock",
        }
    else:
        print(json.dumps({"error": f"unknown cmd {cmd}"}), file=sys.stderr)
        return 1

    text = json.dumps(obj, ensure_ascii=False)
    if args.out:
        open(args.out, "w", encoding="utf-8").write(text + "\n")
    else:
        print(text)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

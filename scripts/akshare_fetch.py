#!/usr/bin/env python3
"""AKShare fetch helper for the Go quantitative-trading data adapter.

Emits stable JSON on stdout (or --out). Network/API logic lives here so Go
stays free of scraping. Offline unit tests use a mock helper instead.

Commands:
  ping
  calendar
  securities
  bars --symbol XXXXXX[.SH|.SZ] --start YYYY-MM-DD --end YYYY-MM-DD
  adj --symbol ...
  st
  suspend
  industry
  financials --symbol ...
  mv --symbol ... [--start ...] [--end ...]
  index --symbol 000300.SH --start ... --end ...

Install:
  python3 -m pip install akshare pandas

Exit codes:
  0 success
  1 runtime / network / API error (JSON {"error":"..."} on stderr)
  2 usage / missing akshare import (clear message)
"""
from __future__ import annotations

import argparse
import json
import sys
import traceback
import warnings
from datetime import datetime
from typing import Any, Optional

warnings.filterwarnings("ignore")


def _emit(obj: Any, out: Optional[str], *, stream=sys.stdout) -> None:
    text = json.dumps(obj, ensure_ascii=False, default=str)
    if out:
        with open(out, "w", encoding="utf-8") as f:
            f.write(text)
            if not text.endswith("\n"):
                f.write("\n")
    else:
        print(text, file=stream)


def _fail(msg: str, code: int = 1, out: Optional[str] = None) -> int:
    _emit({"error": msg}, None, stream=sys.stderr)
    if out:
        _emit({"error": msg}, out)
    return code


def _import_ak() -> Any:
    try:
        import akshare as ak  # type: ignore
    except ImportError:
        print(
            json.dumps({"error": "pip install akshare (and pandas) required"}),
            file=sys.stderr,
        )
        raise SystemExit(2)
    return ak


def _norm_code(symbol: str) -> str:
    s = symbol.strip().upper()
    if "." in s:
        return s.split(".", 1)[0]
    return s


def _to_ts_code(code: str) -> Optional[str]:
    """Map 6-digit code → ts_code; return None for BSE / unknown."""
    c = _norm_code(code)
    if not c.isdigit() or len(c) != 6:
        return None
    # Beijing Stock Exchange — excluded by rule book
    if c.startswith(("8", "4", "92")):
        return None
    if c.startswith(("688", "689")):
        return f"{c}.SH"
    if c.startswith(("60", "68")):
        return f"{c}.SH"
    if c.startswith(("000", "001", "002", "003", "300", "301")):
        return f"{c}.SZ"
    return None


def _board_of(ts_code: str) -> Optional[str]:
    code = ts_code.split(".", 1)[0]
    if code.startswith(("688", "689")):
        return "STAR"
    if code.startswith(("300", "301")):
        return "CHINEXT"
    if code.startswith("60"):
        return "SSE_MAIN"
    if code.startswith(("000", "001", "002", "003")):
        return "SZSE_MAIN"
    return None


def _ymd(s: str) -> str:
    s = s.strip().replace("-", "")
    if len(s) != 8 or not s.isdigit():
        raise ValueError(f"bad date: {s!r}")
    return s


def _ymd_dash(s: Any) -> str:
    if s is None:
        return ""
    if hasattr(s, "strftime"):
        return s.strftime("%Y-%m-%d")
    t = str(s).strip()
    if len(t) == 8 and t.isdigit():
        return f"{t[:4]}-{t[4:6]}-{t[6:8]}"
    if len(t) >= 10 and t[4] == "-" and t[7] == "-":
        return t[:10]
    return t


def _f(v: Any, default: float = 0.0) -> float:
    if v is None:
        return default
    try:
        if isinstance(v, float) and v != v:  # NaN
            return default
        return float(v)
    except (TypeError, ValueError):
        return default


def _col(df: Any, *names: str) -> Optional[Any]:
    cols = {str(c): c for c in df.columns}
    lower = {str(c).lower(): c for c in df.columns}
    for n in names:
        if n in cols:
            return cols[n]
        if n.lower() in lower:
            return lower[n.lower()]
    return None


def cmd_ping(args: argparse.Namespace) -> int:
    note = ""
    ver = None
    try:
        import akshare as ak  # type: ignore

        ver = getattr(ak, "__version__", "unknown")
    except ImportError:
        note = "akshare not installed; run: python3 -m pip install akshare pandas"
    _emit({"ok": True, "akshare_version": ver, "note": note}, args.out)
    return 0


def cmd_calendar(args: argparse.Namespace) -> int:
    ak = _import_ak()
    try:
        if hasattr(ak, "tool_trade_date_hist_sina"):
            df = ak.tool_trade_date_hist_sina()
            source = "tool_trade_date_hist_sina"
        elif hasattr(ak, "tool_trade_date_hist"):
            df = ak.tool_trade_date_hist()
            source = "tool_trade_date_hist"
        else:
            return _fail("no trade calendar API found in this akshare version", out=args.out)
        col = _col(df, "trade_date", "日期")
        if not col:
            return _fail(f"calendar columns unexpected: {list(df.columns)}", out=args.out)
        dates = sorted({_ymd_dash(v) for v in df[col].tolist() if _ymd_dash(v)})
        _emit({"dates": dates, "source": source}, args.out)
        return 0
    except SystemExit:
        raise
    except Exception as e:
        return _fail(f"calendar: {e}", out=args.out)


def cmd_securities(args: argparse.Namespace) -> int:
    ak = _import_ak()
    try:
        if not hasattr(ak, "stock_info_a_code_name"):
            return _fail("stock_info_a_code_name missing", out=args.out)
        df = ak.stock_info_a_code_name()
        code_c = _col(df, "code", "证券代码", "A股代码")
        name_c = _col(df, "name", "证券简称", "A股简称")
        if not code_c or not name_c:
            return _fail(f"securities columns unexpected: {list(df.columns)}", out=args.out)
        out = []
        excluded = 0
        for _, row in df.iterrows():
            raw = str(row[code_c]).strip().zfill(6)
            ts = _to_ts_code(raw)
            if ts is None:
                excluded += 1
                continue
            board = _board_of(ts)
            if board is None:
                excluded += 1
                continue
            out.append(
                {
                    "ts_code": ts,
                    "name": str(row[name_c]).strip(),
                    "board": board,
                    "list_date": "",
                    "delist_date": None,
                }
            )
        _emit(
            {
                "securities": out,
                "excluded_bse_or_unknown": excluded,
                "source": "stock_info_a_code_name",
            },
            args.out,
        )
        return 0
    except SystemExit:
        raise
    except Exception as e:
        return _fail(f"securities: {e}", out=args.out)


def cmd_bars(args: argparse.Namespace) -> int:
    ak = _import_ak()
    code = _norm_code(args.symbol)
    ts = _to_ts_code(code)
    if ts is None:
        return _fail(f"symbol excluded or invalid (BSE?): {args.symbol}", out=args.out)
    start = _ymd(args.start)
    end = _ymd(args.end)
    try:
        df = None
        source = ""
        if hasattr(ak, "stock_zh_a_hist"):
            df = ak.stock_zh_a_hist(
                symbol=code,
                period="daily",
                start_date=start,
                end_date=end,
                adjust="",
            )
            source = "stock_zh_a_hist"
        if (df is None or getattr(df, "empty", True)) and hasattr(ak, "stock_zh_a_daily"):
            prefix = "sh" if ts.endswith(".SH") else "sz"
            df = ak.stock_zh_a_daily(
                symbol=f"{prefix}{code}",
                start_date=start,
                end_date=end,
                adjust="",
            )
            source = "stock_zh_a_daily"
        if df is None or getattr(df, "empty", True):
            _emit({"symbol": ts, "bars": [], "source": source or "none", "note": "empty"}, args.out)
            return 0

        date_c = _col(df, "日期", "date", "trade_date")
        open_c = _col(df, "开盘", "open")
        high_c = _col(df, "最高", "high")
        low_c = _col(df, "最低", "low")
        close_c = _col(df, "收盘", "close")
        vol_c = _col(df, "成交量", "volume")
        amt_c = _col(df, "成交额", "amount")
        pre_c = _col(df, "前收盘", "前收盘价", "preclose", "pre_close")
        if not all([date_c, open_c, high_c, low_c, close_c]):
            return _fail(f"bars columns unexpected: {list(df.columns)}", out=args.out)

        bars = []
        for _, row in df.iterrows():
            vol = _f(row[vol_c]) if vol_c else 0.0
            bars.append(
                {
                    "trade_date": _ymd_dash(row[date_c]),
                    "ts_code": ts,
                    "open": _f(row[open_c]),
                    "high": _f(row[high_c]),
                    "low": _f(row[low_c]),
                    "close": _f(row[close_c]),
                    "volume": vol,
                    "amount": _f(row[amt_c]) if amt_c else 0.0,
                    "pre_close": _f(row[pre_c]) if pre_c else 0.0,
                    "adj_factor": 1.0,
                    "suspended": vol == 0.0,
                }
            )
        _emit({"symbol": ts, "bars": bars, "source": source}, args.out)
        return 0
    except SystemExit:
        raise
    except Exception as e:
        return _fail(f"bars({ts}): {e}", out=args.out)


def cmd_adj(args: argparse.Namespace) -> int:
    ak = _import_ak()
    code = _norm_code(args.symbol)
    ts = _to_ts_code(code)
    if ts is None:
        return _fail(f"symbol excluded or invalid: {args.symbol}", out=args.out)
    try:
        rows: list[dict] = []
        source = ""
        if hasattr(ak, "stock_zh_a_daily"):
            prefix = "sh" if ts.endswith(".SH") else "sz"
            try:
                df = ak.stock_zh_a_daily(symbol=f"{prefix}{code}", adjust="hfq-factor")
                source = "stock_zh_a_daily(hfq-factor)"
                date_c = _col(df, "date", "日期", "trade_date")
                fac_c = _col(df, "hfq_factor", "factor", "adj", "权息因子")
                if date_c and fac_c is None:
                    for c in df.columns:
                        if c != date_c:
                            fac_c = c
                            break
                if date_c and fac_c is not None:
                    for _, row in df.iterrows():
                        rows.append(
                            {
                                "trade_date": _ymd_dash(row[date_c]),
                                "adj_factor": _f(row[fac_c], 1.0),
                            }
                        )
            except Exception as inner:
                print(f"adj warn: {inner}", file=sys.stderr)

        if not rows:
            source = source or "none"
            print(
                "adj: hfq-factor unavailable; returning empty (caller may use adj_factor=1)",
                file=sys.stderr,
            )

        _emit({"symbol": ts, "adj": rows, "source": source or "none"}, args.out)
        return 0
    except SystemExit:
        raise
    except Exception as e:
        return _fail(f"adj({ts}): {e}", out=args.out)


def cmd_st(args: argparse.Namespace) -> int:
    ak = _import_ak()
    note = ""
    records: list[dict] = []
    source = ""
    try:
        df = None
        fn = getattr(ak, "stock_zh_a_st_em", None)
        if callable(fn):
            try:
                df = fn()
                source = "stock_zh_a_st_em"
            except Exception as e:
                note = f"stock_zh_a_st_em failed: {e}"

        if df is None and hasattr(ak, "stock_zh_a_spot_em"):
            try:
                spot = ak.stock_zh_a_spot_em()
                name_c = _col(spot, "名称", "name")
                code_c = _col(spot, "代码", "code")
                if name_c and code_c:
                    source = "stock_zh_a_spot_em(name contains ST)"
                    today = datetime.now().strftime("%Y-%m-%d")
                    for _, row in spot.iterrows():
                        nm = str(row[name_c])
                        if "ST" not in nm.upper():
                            continue
                        ts = _to_ts_code(str(row[code_c]))
                        if not ts:
                            continue
                        st_type = "*ST" if ("*ST" in nm.upper() or "＊ST" in nm) else "ST"
                        records.append(
                            {
                                "ts_code": ts,
                                "entry_date": today,
                                "remove_date": None,
                                "st_type": st_type,
                            }
                        )
            except Exception as e:
                note = (note + "; " if note else "") + f"spot ST fallback failed: {e}"

        if df is not None and not records:
            code_c = _col(df, "代码", "code", "证券代码")
            name_c = _col(df, "名称", "name", "证券简称")
            today = datetime.now().strftime("%Y-%m-%d")
            if code_c:
                for _, row in df.iterrows():
                    ts = _to_ts_code(str(row[code_c]))
                    if not ts:
                        continue
                    nm = str(row[name_c]) if name_c else "ST"
                    st_type = "*ST" if "*ST" in nm.upper() else "ST"
                    records.append(
                        {
                            "ts_code": ts,
                            "entry_date": today,
                            "remove_date": None,
                            "st_type": st_type,
                        }
                    )

        if not records and not note:
            note = (
                "AKShare has no stable historical ST interval API; "
                "returned current snapshot if available, else empty"
            )
        _emit({"records": records, "note": note, "source": source or "none"}, args.out)
        return 0
    except SystemExit:
        raise
    except Exception as e:
        print(f"st warning: {e}", file=sys.stderr)
        _emit({"records": [], "note": f"st unavailable: {e}", "source": "none"}, args.out)
        return 0


def cmd_suspend(args: argparse.Namespace) -> int:
    note = (
        "No stable AKShare historical suspend-interval API wired; "
        "bars.suspended may be inferred (volume==0) by Go consumer. Returning empty."
    )
    print(note, file=sys.stderr)
    _emit({"records": [], "note": note, "source": "none"}, args.out)
    return 0


def cmd_industry(args: argparse.Namespace) -> int:
    ak = _import_ak()
    records: list[dict] = []
    note = ""
    source = ""
    try:
        fn = getattr(ak, "stock_board_industry_name_em", None)
        detail = getattr(ak, "stock_board_industry_cons_em", None)
        if callable(fn) and callable(detail):
            source = "stock_board_industry_cons_em"
            boards = fn()
            name_c = _col(boards, "板块名称", "name")
            if name_c:
                limit = int(getattr(args, "limit", 0) or 0)
                names = list(boards[name_c].astype(str))
                if limit > 0:
                    names = names[:limit]
                for bname in names:
                    try:
                        cons = detail(symbol=bname)
                    except Exception:
                        continue
                    code_c = _col(cons, "代码", "code")
                    if not code_c:
                        continue
                    for _, row in cons.iterrows():
                        ts = _to_ts_code(str(row[code_c]))
                        if not ts:
                            continue
                        records.append(
                            {
                                "ts_code": ts,
                                "industry_code": bname,
                                "industry_name": bname,
                                "effective_date": "2000-01-01",
                                "expire_date": None,
                                "source": "EM_INDUSTRY",
                            }
                        )
        else:
            note = "industry board APIs not found in this akshare version"
        if not records and not note:
            note = "industry fetch returned empty"
        _emit({"records": records, "note": note, "source": source or "none"}, args.out)
        return 0
    except SystemExit:
        raise
    except Exception as e:
        print(f"industry warning: {e}", file=sys.stderr)
        _emit({"records": [], "note": f"industry unavailable: {e}", "source": "none"}, args.out)
        return 0


def cmd_financials(args: argparse.Namespace) -> int:
    ak = _import_ak()
    code = _norm_code(args.symbol)
    ts = _to_ts_code(code)
    if ts is None:
        return _fail(f"symbol excluded or invalid: {args.symbol}", out=args.out)
    rows: list[dict] = []
    note = ""
    source = ""
    tried: list[str] = []
    try:
        candidates = (
            ("stock_financial_analysis_indicator", {"symbol": code}),
            ("stock_financial_abstract", {"symbol": code}),
        )
        for name, kwargs in candidates:
            fn = getattr(ak, name, None)
            if not callable(fn):
                continue
            tried.append(name)
            try:
                try:
                    df = fn(**kwargs)
                except TypeError:
                    df = fn(symbol=code)
            except Exception as e:
                note = f"{name}: {e}"
                continue
            source = name
            date_c = _col(df, "日期", "date", "报告日", "报告期", "trade_date")
            if date_c is None and len(df.columns):
                date_c = df.columns[0]
            np_c = _col(df, "净利润", "net_profit")
            rev_c = _col(df, "营业收入", "revenue", "营业总收入")
            ta_c = _col(df, "总资产", "total_assets")
            for _, row in df.head(40).iterrows():
                d = _ymd_dash(row[date_c]) if date_c is not None else ""
                rows.append(
                    {
                        "ts_code": ts,
                        "report_period": d,
                        "announcement_date": d,
                        "net_profit": _f(row[np_c]) if np_c else 0.0,
                        "revenue": _f(row[rev_c]) if rev_c else 0.0,
                        "gross_profit": 0.0,
                        "total_assets": _f(row[ta_c]) if ta_c else 0.0,
                        "total_liabilities": 0.0,
                        "equity": 0.0,
                        "operating_cf": 0.0,
                        "statement_type": "consolidated",
                    }
                )
            break
        if not rows:
            note = note or (
                "financials APIs unavailable or schema unmatched; "
                f"tried={tried}. Returning empty."
            )
            print(note, file=sys.stderr)
        _emit({"symbol": ts, "rows": rows, "note": note, "source": source or "none"}, args.out)
        return 0
    except SystemExit:
        raise
    except Exception as e:
        print(f"financials warning: {e}", file=sys.stderr)
        _emit(
            {"symbol": ts, "rows": [], "note": f"financials unavailable: {e}", "source": "none"},
            args.out,
        )
        return 0


def cmd_mv(args: argparse.Namespace) -> int:
    ak = _import_ak()
    code = _norm_code(args.symbol)
    ts = _to_ts_code(code)
    if ts is None:
        return _fail(f"symbol excluded or invalid: {args.symbol}", out=args.out)
    values: list[dict] = []
    note = ""
    source = ""
    try:
        fn = getattr(ak, "stock_individual_info_em", None)
        if callable(fn):
            try:
                info = fn(symbol=code)
                source = "stock_individual_info_em"
                item_c = _col(info, "item", "项目")
                val_c = _col(info, "value", "值")
                kv: dict[str, Any] = {}
                if item_c and val_c:
                    for _, row in info.iterrows():
                        kv[str(row[item_c])] = row[val_c]
                total_mv = _f(kv.get("总市值", kv.get("总市值(元)")))
                float_mv = _f(kv.get("流通市值", kv.get("流通市值(元)")))
                total_share = _f(kv.get("总股本", kv.get("总股本(股)")))
                float_share = _f(kv.get("流通股", kv.get("流通股(股)")))
                day = args.start or datetime.now().strftime("%Y-%m-%d")
                if total_mv or float_mv or total_share:
                    values.append(
                        {
                            "trade_date": day,
                            "ts_code": ts,
                            "total_share": total_share,
                            "float_share": float_share,
                            "total_mv": total_mv,
                            "float_mv": float_mv,
                        }
                    )
            except Exception as e:
                note = f"stock_individual_info_em: {e}"
        if not values:
            note = note or (
                "No stable daily MV history API wired; snapshot only when available."
            )
            print(note, file=sys.stderr)
        _emit({"symbol": ts, "values": values, "note": note, "source": source or "none"}, args.out)
        return 0
    except SystemExit:
        raise
    except Exception as e:
        print(f"mv warning: {e}", file=sys.stderr)
        _emit(
            {"symbol": ts, "values": [], "note": f"mv unavailable: {e}", "source": "none"},
            args.out,
        )
        return 0


def cmd_index(args: argparse.Namespace) -> int:
    ak = _import_ak()
    raw = args.symbol.strip().upper()
    code = _norm_code(raw)
    index_code = raw if "." in raw else f"{code}.SH"
    start = _ymd(args.start)
    end = _ymd(args.end)
    try:
        df = None
        source = ""
        if hasattr(ak, "index_zh_a_hist"):
            df = ak.index_zh_a_hist(
                symbol=code, period="daily", start_date=start, end_date=end
            )
            source = "index_zh_a_hist"
        elif hasattr(ak, "stock_zh_index_daily"):
            for prefix in ("sh", "sz"):
                try:
                    df = ak.stock_zh_index_daily(symbol=f"{prefix}{code}")
                    source = "stock_zh_index_daily"
                    break
                except Exception:
                    continue
        if df is None or getattr(df, "empty", True):
            _emit(
                {
                    "index_code": index_code,
                    "bars": [],
                    "source": source or "none",
                    "note": "empty",
                },
                args.out,
            )
            return 0
        date_c = _col(df, "日期", "date", "trade_date")
        open_c = _col(df, "开盘", "open")
        close_c = _col(df, "收盘", "close")
        bars = []
        for _, row in df.iterrows():
            d = _ymd_dash(row[date_c]) if date_c else ""
            if args.start and d and d < args.start:
                continue
            if args.end and d and d > args.end:
                continue
            bars.append(
                {
                    "trade_date": d,
                    "index_code": index_code,
                    "open": _f(row[open_c]) if open_c else 0.0,
                    "close": _f(row[close_c]) if close_c else 0.0,
                }
            )
        _emit({"index_code": index_code, "bars": bars, "source": source}, args.out)
        return 0
    except SystemExit:
        raise
    except Exception as e:
        return _fail(f"index({index_code}): {e}", out=args.out)


def build_parser() -> argparse.ArgumentParser:
    ap = argparse.ArgumentParser(description="AKShare fetch helper for quantitative-trading")
    ap.add_argument("--out", help="write JSON to path instead of stdout")
    sub = ap.add_subparsers(dest="cmd", required=False)

    p = sub.add_parser("ping", help="check helper / akshare import")
    p.set_defaults(func=cmd_ping)

    p = sub.add_parser("calendar", help="A-share trade calendar")
    p.set_defaults(func=cmd_calendar)

    p = sub.add_parser("securities", help="A-share security master (excl BSE)")
    p.set_defaults(func=cmd_securities)

    p = sub.add_parser("bars", help="daily OHLCV bars")
    p.add_argument("--symbol", required=True)
    p.add_argument("--start", required=True)
    p.add_argument("--end", required=True)
    p.set_defaults(func=cmd_bars)

    p = sub.add_parser("adj", help="adjustment factors")
    p.add_argument("--symbol", required=True)
    p.set_defaults(func=cmd_adj)

    p = sub.add_parser("st", help="ST / *ST snapshot (best-effort)")
    p.set_defaults(func=cmd_st)

    p = sub.add_parser("suspend", help="suspend intervals (may be empty)")
    p.set_defaults(func=cmd_suspend)

    p = sub.add_parser("industry", help="industry membership (best-effort)")
    p.add_argument("--limit", type=int, default=0, help="limit industry boards (0=all)")
    p.set_defaults(func=cmd_industry)

    p = sub.add_parser("financials", help="financial statements (best-effort)")
    p.add_argument("--symbol", required=True)
    p.set_defaults(func=cmd_financials)

    p = sub.add_parser("mv", help="market values (best-effort snapshot)")
    p.add_argument("--symbol", required=True)
    p.add_argument("--start", default="")
    p.add_argument("--end", default="")
    p.set_defaults(func=cmd_mv)

    p = sub.add_parser("index", help="index daily bars")
    p.add_argument("--symbol", required=True)
    p.add_argument("--start", required=True)
    p.add_argument("--end", required=True)
    p.set_defaults(func=cmd_index)

    ap.add_argument("--ping", action="store_true", help=argparse.SUPPRESS)
    ap.add_argument("--date", help=argparse.SUPPRESS)
    return ap


def main(argv: Optional[list[str]] = None) -> int:
    ap = build_parser()
    args = ap.parse_args(argv)
    if args.ping and not args.cmd:
        args.cmd = "ping"
        args.func = cmd_ping
    if not getattr(args, "func", None):
        ap.print_help()
        return 2
    try:
        return int(args.func(args))
    except SystemExit as e:
        if e.code is None:
            return 0
        if isinstance(e.code, int):
            return e.code
        return 1
    except Exception as e:
        traceback.print_exc(file=sys.stderr)
        return _fail(str(e), out=getattr(args, "out", None))


if __name__ == "__main__":
    raise SystemExit(main())

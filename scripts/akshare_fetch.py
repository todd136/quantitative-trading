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
import time
import traceback
import warnings
from datetime import datetime
from typing import Any, Callable, Optional

warnings.filterwarnings("ignore")

_RETRY_MARKERS = (
    "RemoteDisconnected",
    "Connection aborted",
    "ConnectionResetError",
    "Connection refused",
    "Read timed out",
    "timed out",
    "Timeout",
    "Temporarily unavailable",
    "Max retries exceeded",
    "EOF occurred",
    "Broken pipe",
)


def _with_retry(fn: Callable[[], Any], *, tries: int = 3, base_delay: float = 1.0, label: str = "") -> Any:
    """Retry transient Eastmoney / urllib disconnects a few times with backoff."""
    last: Optional[BaseException] = None
    for i in range(max(1, tries)):
        try:
            return fn()
        except SystemExit:
            raise
        except Exception as e:  # noqa: BLE001 — helper boundary
            last = e
            msg = f"{type(e).__name__}: {e}"
            transient = any(m in msg for m in _RETRY_MARKERS)
            if (not transient) or i + 1 >= tries:
                raise
            delay = base_delay * (2**i)
            print(
                f"retry {label or 'fetch'} attempt {i+1}/{tries} after {msg}; sleep {delay:.1f}s",
                file=sys.stderr,
            )
            time.sleep(delay)
    assert last is not None
    raise last


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


def _f_opt(v: Any) -> Optional[float]:
    """Parse a numeric field; return None when missing/NaN/unparseable (never invent 0)."""
    if v is None:
        return None
    try:
        if isinstance(v, float) and v != v:  # NaN
            return None
        s = str(v).strip()
        if s == "" or s.lower() in {"nan", "none", "null", "--", "-"}:
            return None
        return float(s)
    except (TypeError, ValueError):
        return None


def _period_dash(period: str) -> str:
    """Normalize YYYYMMDD / YYYY-MM-DD period labels to YYYY-MM-DD."""
    return _ymd_dash(period)


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


def _load_list_dates(ak: Any) -> tuple[dict[str, str], list[str], str]:
    """Map 6-digit code → YYYY-MM-DD from SSE/SZSE official lists (prefer real IPO dates).

    Returns (code→date, warnings, source_label). Empty dict when upstream unavailable.
    """
    dates: dict[str, str] = {}
    warnings: list[str] = []
    sources: list[str] = []

    # Shanghai: main board A + STAR (科创板)
    sh_fn = getattr(ak, "stock_info_sh_name_code", None)
    if callable(sh_fn):
        for symbol in ("主板A股", "科创板"):
            try:
                df = _with_retry(
                    lambda s=symbol: sh_fn(symbol=s),
                    tries=3,
                    label=f"list_date_sh({symbol})",
                )
            except Exception as e:  # noqa: BLE001
                warnings.append(f"stock_info_sh_name_code({symbol}): {e}")
                continue
            code_c = _col(df, "证券代码", "code", "A股代码")
            date_c = _col(df, "上市日期", "list_date", "上市日")
            if not code_c or not date_c:
                warnings.append(
                    f"stock_info_sh_name_code({symbol}) columns unexpected: {list(df.columns)}"
                )
                continue
            sources.append(f"stock_info_sh_name_code({symbol})")
            for _, row in df.iterrows():
                raw = str(row[code_c]).strip().zfill(6)
                ld = _ymd_dash(row[date_c])
                if raw.isdigit() and len(raw) == 6 and ld:
                    dates[raw] = ld
    else:
        warnings.append("stock_info_sh_name_code missing")

    # Shenzhen: A-share list (主板 + 创业板)
    sz_fn = getattr(ak, "stock_info_sz_name_code", None)
    if callable(sz_fn):
        try:
            df = _with_retry(
                lambda: sz_fn(symbol="A股列表"),
                tries=3,
                label="list_date_sz",
            )
            code_c = _col(df, "A股代码", "证券代码", "code")
            date_c = _col(df, "A股上市日期", "上市日期", "list_date")
            if not code_c or not date_c:
                warnings.append(
                    f"stock_info_sz_name_code columns unexpected: {list(df.columns)}"
                )
            else:
                sources.append("stock_info_sz_name_code(A股列表)")
                for _, row in df.iterrows():
                    raw = str(row[code_c]).strip().zfill(6)
                    ld = _ymd_dash(row[date_c])
                    if raw.isdigit() and len(raw) == 6 and ld:
                        dates[raw] = ld
        except Exception as e:  # noqa: BLE001
            warnings.append(f"stock_info_sz_name_code: {e}")
    else:
        warnings.append("stock_info_sz_name_code missing")

    src = "+".join(sources) if sources else "none"
    return dates, warnings, src


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

        list_dates, list_warns, list_src = _load_list_dates(ak)
        for w in list_warns:
            print(f"securities list_date warn: {w}", file=sys.stderr)

        out = []
        excluded = 0
        missing_list_date = 0
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
            ld = list_dates.get(raw, "")
            if not ld:
                missing_list_date += 1
            out.append(
                {
                    "ts_code": ts,
                    "name": str(row[name_c]).strip(),
                    "board": board,
                    "list_date": ld,
                    "delist_date": None,
                }
            )

        note = ""
        if missing_list_date:
            note = (
                f"{missing_list_date}/{len(out)} securities missing list_date; "
                "Go consumer should skip min_list_trading_days for those names "
                "(smoke degrade). Production prefers real SSE/SZSE IPO dates."
            )
            print(f"securities: {note}", file=sys.stderr)
        if not list_dates:
            note = (
                note + "; " if note else ""
            ) + "no list_date from upstream; all list_date empty"
            print(f"securities: {note}", file=sys.stderr)

        source = "stock_info_a_code_name"
        if list_src and list_src != "none":
            source = f"{source}+{list_src}"

        _emit(
            {
                "securities": out,
                "excluded_bse_or_unknown": excluded,
                "list_date_missing": missing_list_date,
                "list_date_source": list_src,
                "note": note,
                "source": source,
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
        # Prefer Sina daily when available — Eastmoney hist often RemoteDisconnects.
        if hasattr(ak, "stock_zh_a_daily"):
            prefix = "sh" if ts.endswith(".SH") else "sz"
            try:
                df = _with_retry(
                    lambda: ak.stock_zh_a_daily(
                        symbol=f"{prefix}{code}",
                        start_date=start,
                        end_date=end,
                        adjust="",
                    ),
                    tries=3,
                    label=f"bars_daily({ts})",
                )
                source = "stock_zh_a_daily"
            except Exception as e:
                print(f"bars daily failed: {e}", file=sys.stderr)
                df = None
        if (df is None or getattr(df, "empty", True)) and hasattr(ak, "stock_zh_a_hist"):
            try:
                df = _with_retry(
                    lambda: ak.stock_zh_a_hist(
                        symbol=code,
                        period="daily",
                        start_date=start,
                        end_date=end,
                        adjust="",
                    ),
                    tries=3,
                    label=f"bars({ts})",
                )
                source = "stock_zh_a_hist"
            except Exception as e:
                print(f"bars hist failed: {e}", file=sys.stderr)
                df = None
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
                df = _with_retry(fn, tries=3, label="st")
                source = "stock_zh_a_st_em"
            except Exception as e:
                note = f"stock_zh_a_st_em failed: {e}"

        if df is None and hasattr(ak, "stock_zh_a_spot_em"):
            try:
                spot = _with_retry(ak.stock_zh_a_spot_em, tries=3, label="st_spot")
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

        if not records and hasattr(ak, "stock_info_a_code_name"):
            try:
                info = _with_retry(ak.stock_info_a_code_name, tries=2, label="st_via_names")
                code_c = _col(info, "code", "证券代码", "A股代码")
                name_c = _col(info, "name", "证券简称", "A股简称")
                if code_c and name_c:
                    source = "stock_info_a_code_name(name contains ST)"
                    today = datetime.now().strftime("%Y-%m-%d")
                    for _, row in info.iterrows():
                        nm = str(row[name_c])
                        if "ST" not in nm.upper() and "＊ST" not in nm and "*ST" not in nm.upper():
                            continue
                        ts2 = _to_ts_code(str(row[code_c]))
                        if not ts2:
                            continue
                        st_type = "*ST" if ("*ST" in nm.upper() or "＊ST" in nm) else "ST"
                        records.append(
                            {
                                "ts_code": ts2,
                                "entry_date": today,
                                "remove_date": None,
                                "st_type": st_type,
                            }
                        )
            except Exception as e:
                note = (note + "; " if note else "") + f"name-ST fallback failed: {e}"

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
    """Best-effort EM industry membership. Never fabricates industries.

    Gaps: Eastmoney board APIs frequently RemoteDisconnect; historical
    effective/expire dates are unavailable (we stamp effective_date=2000-01-01).
    Source label is EM_INDUSTRY (not SW_L1).
    """
    ak = _import_ak()
    records: list[dict] = []
    note_parts: list[str] = []
    source = ""
    failed_boards = 0
    ok_boards = 0
    try:
        fn = getattr(ak, "stock_board_industry_name_em", None)
        detail = getattr(ak, "stock_board_industry_cons_em", None)
        if not (callable(fn) and callable(detail)):
            note_parts.append("industry board APIs not found in this akshare version")
            _emit({"records": [], "note": "; ".join(note_parts), "source": "none"}, args.out)
            return 0

        source = "stock_board_industry_cons_em"
        try:
            boards = _with_retry(fn, tries=4, base_delay=1.5, label="industry_boards")
        except Exception as e:  # noqa: BLE001
            note = f"industry board list unavailable: {e}"
            print(note, file=sys.stderr)
            _emit({"records": [], "note": note, "source": "none"}, args.out)
            return 0

        name_c = _col(boards, "板块名称", "name")
        if not name_c:
            note = f"industry board columns unexpected: {list(boards.columns)}"
            print(note, file=sys.stderr)
            _emit({"records": [], "note": note, "source": "none"}, args.out)
            return 0

        limit = int(getattr(args, "limit", 0) or 0)
        names = [str(x) for x in boards[name_c].tolist() if str(x).strip()]
        if limit > 0:
            names = names[:limit]

        seen: dict[str, dict] = {}
        for i, bname in enumerate(names):
            try:
                cons = _with_retry(
                    lambda b=bname: detail(symbol=b),
                    tries=4,
                    base_delay=1.0,
                    label=f"industry_cons({bname})",
                )
            except Exception as e:  # noqa: BLE001
                failed_boards += 1
                print(f"industry cons fail {bname}: {e}", file=sys.stderr)
                time.sleep(0.4)
                continue
            code_c = _col(cons, "代码", "code")
            if not code_c:
                failed_boards += 1
                continue
            ok_boards += 1
            for _, row in cons.iterrows():
                ts = _to_ts_code(str(row[code_c]))
                if not ts:
                    continue
                # Last board wins if a name appears in multiple EM boards.
                seen[ts] = {
                    "ts_code": ts,
                    "industry_code": bname,
                    "industry_name": bname,
                    "effective_date": "2000-01-01",
                    "expire_date": None,
                    "source": "EM_INDUSTRY",
                }
            # gentle pacing — Eastmoney disconnects under burst traffic
            if (i + 1) % 5 == 0:
                time.sleep(0.35)

        records = list(seen.values())
        if failed_boards:
            note_parts.append(f"boards_ok={ok_boards} boards_failed={failed_boards}")
        if not records:
            note_parts.append("industry fetch returned empty")
        else:
            note_parts.append(
                f"mapped {len(records)} names from {ok_boards} boards; "
                "effective_date stamped 2000-01-01 (no historical membership API)"
            )
        note = "; ".join(note_parts)
        print(f"industry: {note}", file=sys.stderr)
        _emit({"records": records, "note": note, "source": source}, args.out)
        return 0
    except SystemExit:
        raise
    except Exception as e:
        print(f"industry warning: {e}", file=sys.stderr)
        _emit({"records": [], "note": f"industry unavailable: {e}", "source": "none"}, args.out)
        return 0


def cmd_financials(args: argparse.Namespace) -> int:
    """Map real equity/revenue/assets; never invent zeros as valid fundamentals.

    Preferred: stock_financial_abstract (fast) for equity/revenue/profit/cost/debt-ratio,
    deriving total_assets = equity / (1 - debt_ratio/100) when ratio in (0,100).
    Fallback: stock_financial_analysis_indicator absolute 总资产(元).
    Best PIT (slow): EM balance+profit sheets with NOTICE_DATE — used when abstract empty.

    Gaps logged in note: announcement_date often approximated as report_period when
    abstract path is used (strict PIT prefers EM NOTICE_DATE).
    """
    ak = _import_ak()
    code = _norm_code(args.symbol)
    ts = _to_ts_code(code)
    if ts is None:
        return _fail(f"symbol excluded or invalid: {args.symbol}", out=args.out)

    rows: list[dict] = []
    note_parts: list[str] = []
    source = ""
    tried: list[str] = []

    def _emit_rows(rows_in: list[dict], src: str, notes: list[str]) -> int:
        note = "; ".join([n for n in notes if n])
        if not rows_in:
            note = note or (
                "financials APIs unavailable or schema unmatched; "
                f"tried={tried}. Returning empty (no fabricated zeros)."
            )
            print(note, file=sys.stderr)
        _emit(
            {"symbol": ts, "rows": rows_in, "note": note, "source": src or "none"},
            args.out,
        )
        return 0

    def _row_from_parts(
        *,
        period: str,
        announce: str,
        net_profit: Optional[float],
        revenue: Optional[float],
        gross_profit: Optional[float],
        total_assets: Optional[float],
        total_liabilities: Optional[float],
        equity: Optional[float],
        operating_cf: Optional[float],
    ) -> Optional[dict]:
        # Required for downstream Quality/Value: equity + revenue + total_assets must be real.
        if equity is None or equity == 0:
            return None
        if revenue is None or revenue == 0:
            return None
        if total_assets is None or total_assets == 0:
            return None
        # Liabilities: derive if missing
        liab = total_liabilities
        if liab is None:
            liab = total_assets - equity
        gp = gross_profit
        if gp is None:
            gp = None  # leave 0 only when truly unknown — mark via note; store 0 but Quality uses it
            # GrossProfit==0 with Revenue>0 is a valid edge (zero margin); allow 0.
            gp = 0.0
        return {
            "ts_code": ts,
            "report_period": period,
            "announcement_date": announce or period,
            "net_profit": float(net_profit) if net_profit is not None else 0.0,
            "revenue": float(revenue),
            "gross_profit": float(gp),
            "total_assets": float(total_assets),
            "total_liabilities": float(liab),
            "equity": float(equity),
            "operating_cf": float(operating_cf) if operating_cf is not None else 0.0,
            "statement_type": "consolidated",
        }

    try:
        # --- Path A: stock_financial_abstract (wide indicator × period) ---
        abs_fn = getattr(ak, "stock_financial_abstract", None)
        if callable(abs_fn):
            tried.append("stock_financial_abstract")
            try:
                df = _with_retry(lambda: abs_fn(symbol=code), tries=3, label=f"fin_abstract({ts})")
                ind_c = _col(df, "指标", "indicator")
                if ind_c is not None:
                    by_ind: dict[str, Any] = {}
                    for _, row in df.iterrows():
                        # Prefer 常用指标 section when present
                        opt = str(row[_col(df, "选项", "option") or ind_c]) if _col(df, "选项", "option") else ""
                        key = str(row[ind_c]).strip()
                        if key in by_ind and opt and "常用" not in opt:
                            continue
                        by_ind[key] = row

                    opt_c = _col(df, "选项", "option")
                    period_cols = []
                    for c in df.columns:
                        if c == ind_c or (opt_c is not None and c == opt_c):
                            continue
                        s = str(c).strip().replace("-", "")
                        if s.isdigit() and len(s) == 8:
                            period_cols.append(c)
                    # keep chronological recent first is fine; take up to 40 periods
                    built: list[dict] = []
                    skipped_incomplete = 0
                    for pcol in period_cols[:40]:
                        def g(name: str) -> Optional[float]:
                            r = by_ind.get(name)
                            if r is None:
                                return None
                            return _f_opt(r[pcol])

                        equity = g("股东权益合计(净资产)")
                        revenue = g("营业总收入")
                        if revenue is None:
                            revenue = g("营业收入")
                        net_profit = g("归母净利润")
                        if net_profit is None:
                            net_profit = g("净利润")
                        cost = g("营业成本")
                        ocf = g("经营现金流量净额")
                        dta_pct = g("资产负债率")
                        gm_pct = g("毛利率")

                        total_assets = None
                        total_liab = None
                        if equity is not None and dta_pct is not None and 0.0 < dta_pct < 100.0:
                            denom = 1.0 - dta_pct / 100.0
                            if denom > 1e-9:
                                total_assets = equity / denom
                                total_liab = total_assets - equity

                        gross = None
                        if revenue is not None and cost is not None:
                            gross = revenue - cost
                        elif revenue is not None and gm_pct is not None:
                            gross = revenue * (gm_pct / 100.0)

                        period = _period_dash(str(pcol))
                        rec = _row_from_parts(
                            period=period,
                            announce=period,  # abstract lacks NOTICE_DATE
                            net_profit=net_profit,
                            revenue=revenue,
                            gross_profit=gross,
                            total_assets=total_assets,
                            total_liabilities=total_liab,
                            equity=equity,
                            operating_cf=ocf,
                        )
                        if rec is None:
                            skipped_incomplete += 1
                            continue
                        built.append(rec)
                    if built:
                        source = "stock_financial_abstract"
                        note_parts.append(
                            "announcement_date≈report_period (abstract has no NOTICE_DATE; "
                            "strict PIT prefers EM sheets)"
                        )
                        if skipped_incomplete:
                            note_parts.append(
                                f"skipped_incomplete_periods={skipped_incomplete} "
                                "(missing equity/revenue/assets — not written as zero)"
                            )
                        rows = built
            except Exception as e:  # noqa: BLE001
                note_parts.append(f"stock_financial_abstract: {e}")

        # --- Path B: analysis_indicator absolute 总资产 — fill holes / fallback ---
        if not rows:
            ind_fn = getattr(ak, "stock_financial_analysis_indicator", None)
            if callable(ind_fn):
                tried.append("stock_financial_analysis_indicator")
                try:
                    df = _with_retry(
                        lambda: ind_fn(symbol=code),
                        tries=3,
                        label=f"fin_indicator({ts})",
                    )
                    date_c = _col(df, "日期", "date", "报告日", "报告期")
                    ta_c = _col(df, "总资产(元)", "总资产")
                    dta_c = _col(df, "资产负债率(%)", "资产负债率")
                    er_c = _col(df, "股东权益比率(%)", "股东权益比率")
                    gp_c = _col(df, "主营业务利润(元)", "主营业务利润")
                    np_c = _col(df, "扣除非经常性损益后的净利润(元)", "净利润")
                    # This path often lacks revenue absolute — skip unless we can derive.
                    # Equity from assets * equity_ratio when both present.
                    built = []
                    skipped = 0
                    for _, row in df.head(40).iterrows():
                        d = _ymd_dash(row[date_c]) if date_c is not None else ""
                        ta = _f_opt(row[ta_c]) if ta_c else None
                        er = _f_opt(row[er_c]) if er_c else None
                        dta = _f_opt(row[dta_c]) if dta_c else None
                        equity = None
                        if ta is not None and er is not None and 0.0 < er <= 100.0:
                            equity = ta * er / 100.0
                        elif ta is not None and dta is not None and 0.0 <= dta < 100.0:
                            equity = ta * (1.0 - dta / 100.0)
                        liab = (ta - equity) if (ta is not None and equity is not None) else None
                        # Revenue absent on this schema → cannot emit Quality-valid row.
                        skipped += 1
                        _ = (d, equity, liab, gp_c, np_c)  # keep linters quiet if unused
                    note_parts.append(
                        "stock_financial_analysis_indicator lacks absolute revenue; "
                        f"not emitting zero-revenue rows (looked_at={min(40, len(df))})"
                    )
                except Exception as e:  # noqa: BLE001
                    note_parts.append(f"stock_financial_analysis_indicator: {e}")

        # --- Path C: EM balance + profit (NOTICE_DATE, absolute fields) ---
        if not rows:
            prefix = "SH" if ts.endswith(".SH") else "SZ"
            em_sym = f"{prefix}{code}"
            bal_fn = getattr(ak, "stock_balance_sheet_by_report_em", None)
            pft_fn = getattr(ak, "stock_profit_sheet_by_report_em", None)
            if callable(bal_fn) and callable(pft_fn):
                tried.append("stock_balance_sheet_by_report_em+stock_profit_sheet_by_report_em")
                try:
                    bal = _with_retry(
                        lambda: bal_fn(symbol=em_sym),
                        tries=2,
                        base_delay=2.0,
                        label=f"fin_em_bal({ts})",
                    )
                    pft = _with_retry(
                        lambda: pft_fn(symbol=em_sym),
                        tries=2,
                        base_delay=2.0,
                        label=f"fin_em_pft({ts})",
                    )
                    # index profit by REPORT_DATE
                    pft_by: dict[str, Any] = {}
                    rd_c = _col(pft, "REPORT_DATE", "报告日")
                    for _, row in pft.iterrows():
                        k = _ymd_dash(row[rd_c]) if rd_c is not None else ""
                        if k:
                            pft_by[k] = row
                    built = []
                    skipped = 0
                    bal_rd = _col(bal, "REPORT_DATE", "报告日")
                    bal_nd = _col(bal, "NOTICE_DATE", "公告日期")
                    for _, brow in bal.head(40).iterrows():
                        period = _ymd_dash(brow[bal_rd]) if bal_rd is not None else ""
                        if not period:
                            continue
                        announce = _ymd_dash(brow[bal_nd]) if bal_nd is not None else period
                        equity = _f_opt(brow["TOTAL_EQUITY"]) if "TOTAL_EQUITY" in bal.columns else None
                        if equity is None and "TOTAL_PARENT_EQUITY" in bal.columns:
                            equity = _f_opt(brow["TOTAL_PARENT_EQUITY"])
                        ta = _f_opt(brow["TOTAL_ASSETS"]) if "TOTAL_ASSETS" in bal.columns else None
                        liab = (
                            _f_opt(brow["TOTAL_LIABILITIES"])
                            if "TOTAL_LIABILITIES" in bal.columns
                            else None
                        )
                        prow = pft_by.get(period)
                        revenue = None
                        net_profit = None
                        gross = None
                        if prow is not None:
                            if "OPERATE_INCOME" in pft.columns:
                                revenue = _f_opt(prow["OPERATE_INCOME"])
                            if "PARENT_NETPROFIT" in pft.columns:
                                net_profit = _f_opt(prow["PARENT_NETPROFIT"])
                            if net_profit is None and "NETPROFIT" in pft.columns:
                                net_profit = _f_opt(prow["NETPROFIT"])
                            if "OPERATE_COST" in pft.columns and revenue is not None:
                                cost = _f_opt(prow["OPERATE_COST"])
                                if cost is not None:
                                    gross = revenue - cost
                        rec = _row_from_parts(
                            period=period,
                            announce=announce or period,
                            net_profit=net_profit,
                            revenue=revenue,
                            gross_profit=gross,
                            total_assets=ta,
                            total_liabilities=liab,
                            equity=equity,
                            operating_cf=None,
                        )
                        if rec is None:
                            skipped += 1
                            continue
                        built.append(rec)
                    if built:
                        source = "stock_balance_sheet_by_report_em+stock_profit_sheet_by_report_em"
                        note_parts.append("PIT announcement_date from NOTICE_DATE")
                        if skipped:
                            note_parts.append(f"skipped_incomplete_em_rows={skipped}")
                        rows = built
                    else:
                        note_parts.append(
                            f"EM sheets returned no complete rows (skipped={skipped})"
                        )
                except Exception as e:  # noqa: BLE001
                    note_parts.append(f"EM financial sheets: {e}")

        return _emit_rows(rows, source, note_parts)
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
    """Daily market values. Prefer stock_value_em history; never invent MV=0 as valid.

    Fallback: stock_zh_a_daily outstanding_share * close (windowed).
    Snapshot stock_individual_info_em is last resort (often blocked).
    """
    ak = _import_ak()
    code = _norm_code(args.symbol)
    ts = _to_ts_code(code)
    if ts is None:
        return _fail(f"symbol excluded or invalid: {args.symbol}", out=args.out)
    values: list[dict] = []
    note_parts: list[str] = []
    source = ""
    start = (args.start or "").strip()
    end = (args.end or "").strip()
    try:
        # --- Path A: stock_value_em daily history ---
        fn = getattr(ak, "stock_value_em", None)
        if callable(fn):
            try:
                df = _with_retry(lambda: fn(symbol=code), tries=3, label=f"mv_value_em({ts})")
                date_c = _col(df, "数据日期", "date", "trade_date")
                tmv_c = _col(df, "总市值", "total_mv")
                fmv_c = _col(df, "流通市值", "float_mv")
                tsh_c = _col(df, "总股本", "total_share")
                fsh_c = _col(df, "流通股本", "float_share")
                if date_c and tmv_c:
                    source = "stock_value_em"
                    for _, row in df.iterrows():
                        d = _ymd_dash(row[date_c])
                        if start and d and d < start:
                            continue
                        if end and d and d > end:
                            continue
                        tmv = _f_opt(row[tmv_c])
                        if tmv is None or tmv <= 0:
                            continue  # never emit invalid zero MV
                        values.append(
                            {
                                "trade_date": d,
                                "ts_code": ts,
                                "total_share": _f(row[tsh_c]) if tsh_c else 0.0,
                                "float_share": _f(row[fsh_c]) if fsh_c else 0.0,
                                "total_mv": float(tmv),
                                "float_mv": float(_f_opt(row[fmv_c]) or 0.0) if fmv_c else 0.0,
                            }
                        )
                else:
                    note_parts.append(f"stock_value_em columns unexpected: {list(df.columns)}")
            except Exception as e:  # noqa: BLE001
                note_parts.append(f"stock_value_em: {e}")

        # --- Path B: daily bars outstanding_share * close ---
        if not values and hasattr(ak, "stock_zh_a_daily"):
            prefix = "sh" if ts.endswith(".SH") else "sz"
            try:
                kwargs = {"symbol": f"{prefix}{code}", "adjust": ""}
                if start:
                    kwargs["start_date"] = _ymd(start)
                if end:
                    kwargs["end_date"] = _ymd(end)
                df = _with_retry(
                    lambda: ak.stock_zh_a_daily(**kwargs),
                    tries=3,
                    label=f"mv_daily({ts})",
                )
                date_c = _col(df, "date", "日期", "trade_date")
                close_c = _col(df, "close", "收盘")
                shr_c = _col(df, "outstanding_share", "总股本")
                if date_c and close_c and shr_c:
                    source = "stock_zh_a_daily(close*outstanding_share)"
                    for _, row in df.iterrows():
                        d = _ymd_dash(row[date_c])
                        if start and d and d < start:
                            continue
                        if end and d and d > end:
                            continue
                        close = _f_opt(row[close_c])
                        shr = _f_opt(row[shr_c])
                        if close is None or shr is None or close <= 0 or shr <= 0:
                            continue
                        tmv = close * shr
                        values.append(
                            {
                                "trade_date": d,
                                "ts_code": ts,
                                "total_share": float(shr),
                                "float_share": float(shr),
                                "total_mv": float(tmv),
                                "float_mv": float(tmv),
                            }
                        )
                else:
                    note_parts.append(
                        f"stock_zh_a_daily missing share/close cols: {list(df.columns)}"
                    )
            except Exception as e:  # noqa: BLE001
                note_parts.append(f"stock_zh_a_daily MV: {e}")

        # --- Path C: snapshot (often blocked) ---
        if not values:
            snap = getattr(ak, "stock_individual_info_em", None)
            if callable(snap):
                try:
                    info = snap(symbol=code)
                    source = "stock_individual_info_em"
                    item_c = _col(info, "item", "项目")
                    val_c = _col(info, "value", "值")
                    kv: dict[str, Any] = {}
                    if item_c and val_c:
                        for _, row in info.iterrows():
                            kv[str(row[item_c])] = row[val_c]
                    total_mv = _f_opt(kv.get("总市值", kv.get("总市值(元)")))
                    float_mv = _f_opt(kv.get("流通市值", kv.get("流通市值(元)")))
                    total_share = _f_opt(kv.get("总股本", kv.get("总股本(股)")))
                    float_share = _f_opt(kv.get("流通股", kv.get("流通股(股)")))
                    day = start or end or datetime.now().strftime("%Y-%m-%d")
                    if total_mv is not None and total_mv > 0:
                        values.append(
                            {
                                "trade_date": day,
                                "ts_code": ts,
                                "total_share": float(total_share or 0.0),
                                "float_share": float(float_share or 0.0),
                                "total_mv": float(total_mv),
                                "float_mv": float(float_mv or 0.0),
                            }
                        )
                        note_parts.append("snapshot only (no daily history from this path)")
                    else:
                        note_parts.append("stock_individual_info_em missing/invalid 总市值")
                except Exception as e:  # noqa: BLE001
                    note_parts.append(f"stock_individual_info_em: {e}")

        if not values:
            note_parts.append(
                "No usable daily MV; returning empty (not fabricating total_mv=0)."
            )
            print("; ".join(note_parts), file=sys.stderr)
        note = "; ".join(note_parts)
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
        if hasattr(ak, "stock_zh_index_daily"):
            for prefix in ("sh", "sz"):
                try:
                    df = _with_retry(
                        lambda p=prefix: ak.stock_zh_index_daily(symbol=f"{p}{code}"),
                        tries=3,
                        label=f"index_daily({prefix}{code})",
                    )
                    source = "stock_zh_index_daily"
                    break
                except Exception as e:
                    print(f"index daily {prefix} failed: {e}", file=sys.stderr)
                    continue
        if (df is None or getattr(df, "empty", True)) and hasattr(ak, "index_zh_a_hist"):
            try:
                df = _with_retry(
                    lambda: ak.index_zh_a_hist(
                        symbol=code, period="daily", start_date=start, end_date=end
                    ),
                    tries=3,
                    label=f"index({index_code})",
                )
                source = "index_zh_a_hist"
            except Exception as e:
                print(f"index hist failed: {e}", file=sys.stderr)
                df = None
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

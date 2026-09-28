#!/usr/bin/env python3
"""Thin AKShare fetch helper (optional; not required for go test).

Usage:
  python3 scripts/akshare_fetch.py --help

Boundary: Go akshare.Provider may exec this CLI or call an HTTP sidecar.
Network calls live here — keep Go free of scraping logic.
"""
from __future__ import annotations

import argparse
import json
import sys


def main() -> int:
    ap = argparse.ArgumentParser(description="AKShare fetch helper stub")
    ap.add_argument("--date", help="trade date YYYY-MM-DD")
    ap.add_argument("--out", help="output path")
    ap.add_argument("--ping", action="store_true", help="print ok and exit")
    args = ap.parse_args()
    if args.ping:
        print(json.dumps({"ok": True, "note": "install akshare to enable real fetches"}))
        return 0
    print(
        "akshare_fetch: stub only — wire akshare package calls here when integrating",
        file=sys.stderr,
    )
    return 2


if __name__ == "__main__":
    raise SystemExit(main())

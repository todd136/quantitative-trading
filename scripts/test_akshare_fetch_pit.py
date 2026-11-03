#!/usr/bin/env python3
"""Offline PIT helpers for scripts/akshare_fetch.py (no network)."""
from __future__ import annotations

import importlib.util
import pathlib
import unittest


def _load():
    path = pathlib.Path(__file__).resolve().parent / "akshare_fetch.py"
    spec = importlib.util.spec_from_file_location("akshare_fetch", path)
    mod = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    spec.loader.exec_module(mod)
    return mod


class TestAnnouncementFields(unittest.TestCase):
    def test_missing_announce_is_proxy(self):
        mod = _load()
        date, proxy = mod.announcement_fields("", "2023-12-31")
        self.assertEqual(date, "")
        self.assertTrue(proxy)

    def test_announce_equals_period_is_proxy(self):
        mod = _load()
        date, proxy = mod.announcement_fields("2023-12-31", "2023-12-31")
        self.assertEqual(date, "2023-12-31")
        self.assertTrue(proxy)

    def test_notice_date_is_not_proxy(self):
        mod = _load()
        date, proxy = mod.announcement_fields("2024-03-30", "2023-12-31")
        self.assertEqual(date, "2024-03-30")
        self.assertFalse(proxy)


if __name__ == "__main__":
    raise SystemExit(unittest.main())

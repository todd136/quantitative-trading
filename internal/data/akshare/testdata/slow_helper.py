#!/usr/bin/env python3
"""Mock helper that sleeps forever — used to test CLI timeout."""
import sys
import time
time.sleep(3600)
print("{}")
sys.exit(0)

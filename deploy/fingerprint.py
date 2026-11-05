#!/usr/bin/env python3
from pathlib import Path
from broker import fingerprint

print(fingerprint(Path(__file__).resolve().parent.parent))

#!/usr/bin/env python3
# Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
"""Host convenience wrapper; the maintained recorder lives in the site source."""
from pathlib import Path
import subprocess
import sys

if __name__ == '__main__':
    repo = Path(__file__).resolve().parents[1]
    raise SystemExit(subprocess.call([sys.executable,
        '/data/open-ontologies-site/main/scripts/record_export.py', '--repo', str(repo), *sys.argv[1:]]))

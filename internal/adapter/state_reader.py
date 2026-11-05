# Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
"""Fixed read-only registry reader. No client paths, SQL, or script evaluation."""
import json
import pathlib
import sqlite3
import sys

db = sqlite3.connect(pathlib.Path(sys.argv[1]).as_uri() + '?mode=ro', uri=True, timeout=2)
db.execute('PRAGMA query_only=ON')
db.execute('BEGIN')
tables = {row[0] for row in db.execute("SELECT name FROM sqlite_master WHERE type='table'")}
out = {'policies': [], 'actions': []}
if 'policy_rules' in tables:
    for row in db.execute('SELECT name,effect,condition,description FROM policy_rules ORDER BY name LIMIT 257'):
        out['policies'].append(dict(zip(('name', 'effect', 'condition', 'description'), row)))
if 'dynamics_action_schemas' in tables:
    for row in db.execute('SELECT schema_json FROM dynamics_action_schemas ORDER BY name LIMIT 257'):
        out['actions'].append(json.loads(row[0]))
if any(len(v) > 256 for v in out.values()):
    raise ValueError('registry exceeds adapter inspection limit')
db.close()
json.dump(out, sys.stdout, ensure_ascii=True)

#!/bin/sh
set -eu

if [ ! -f .env ]; then
    echo 'Create .env in the repository root before starting MemberOS.' >&2
    exit 2
fi

exec python3 - "$@" <<'PY'
import os
import re
import sys
from pathlib import Path

environment = os.environ.copy()
for line in Path('.env').read_text().splitlines():
    line = line.strip()
    if not line or line.startswith('#'):
        continue
    key, separator, value = line.partition('=')
    if not separator or not re.fullmatch(r'[A-Za-z_][A-Za-z0-9_]*', key):
        raise SystemExit('Invalid .env entry; expected KEY=value')
    if len(value) >= 2 and value[0] == value[-1] and value[0] in ("'", '"'):
        value = value[1:-1]
    environment[key] = value

os.execvpe('go', ['go', 'run', './cmd/memberos', *sys.argv[1:]], environment)
PY

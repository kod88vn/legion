#!/bin/bash
# fieldcheck.sh [args] - kept for old callers: runs tools/system/fieldcheck.py (every field file and portal line of the
# repo and of every OpenKore container, stopped ones too, against the server's map cache and loaded warps). Exit 0 ok,
# 1 mismatches, 2 the server data could not be read. Read-only (docker cp, nothing started).
HERE=$(cd "$(dirname "$0")" && pwd)
if command -v python3 >/dev/null 2>&1; then PY=python3; else PY=python; fi
exec "$PY" "$HERE/fieldcheck.py" "$@"

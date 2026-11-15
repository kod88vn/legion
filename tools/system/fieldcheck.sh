#!/bin/bash
# fieldcheck.sh [container ...] - compare every openkore/fields-re/*.fld2.gz (server-exact field files) with the
# /openkore/fields copy inside running OpenKore containers (md5, read-only: docker exec md5sum / ls only).
# Also flags a leftover uncompressed .fld2 (it would shadow the .gz) and the classic izlude portal lines.
# Default container: ro-openkore-ayla (plus ro-openkore-qa when it runs). Exit 1 on any mismatch.
# Run from WSL (or Git Bash: it falls back to `wsl docker`). For the deploy agent after an OpenKore recreate.
# 2026-11-15: the docker-cp'd field fix was lost on a recreate and Ayla was stranded on izlude 24,98.
set -u
HERE=$(cd "$(dirname "$0")/../.." && pwd)
DIR="$HERE/openkore/fields-re"
DOCKER="docker"
command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1 || DOCKER="wsl docker"
if [ $# -eq 0 ]; then
  set -- ro-openkore-ayla
  $DOCKER ps --format '{{.Names}}' | grep -qx ro-openkore-qa && set -- "$@" ro-openkore-qa
fi
bad=0
for c in "$@"; do
  if ! $DOCKER ps --format '{{.Names}}' | grep -qx "$c"; then echo "$c: not running"; bad=1; continue; fi
  remote=$($DOCKER exec "$c" sh -c 'cd /openkore/fields && md5sum *.fld2.gz 2>/dev/null; ls *.fld2 2>/dev/null | sed "s/^/RAW /"')
  n=0; m=0
  for f in "$DIR"/*.fld2.gz; do
    b=$(basename "$f"); n=$((n + 1))
    want=$(md5sum "$f" | cut -d' ' -f1)
    got=$(printf '%s\n' "$remote" | awk -v f="$b" '$2 == f || $2 == "*" f {print $1}')
    if [ "$want" != "$got" ]; then
      echo "$c: MISMATCH ${b%.fld2.gz}  repo $want  container ${got:-missing}"; m=$((m + 1))
    fi
    if printf '%s\n' "$remote" | grep -qx "RAW ${b%.gz}"; then
      echo "$c: MISMATCH ${b%.fld2.gz}  an uncompressed ${b%.gz} shadows the .gz"; m=$((m + 1))
    fi
  done
  if $DOCKER exec "$c" grep -q '^izlude 30 78 ' /openkore/tables/portals.txt; then
    echo "$c: MISMATCH portals.txt still has the classic izlude portals (fix_re_portals.sh not applied)"; m=$((m + 1))
  fi
  if [ $m -eq 0 ]; then echo "$c: ok ($n fields-re files match, Renewal izlude portals)"; else bad=1; fi
done
exit $bad

#!/bin/sh
# overlay.sh - start step for OpenKore containers (openkore-ayla): make the server-matching field files and portal
# patches part of every container start, so they survive any recreate even when the ro-openkore image is stale.
# 2026-11-14/15: the fields-re + portals fixes were in openkore/Dockerfile but the image (built 2026-11-12) was never
# rebuilt; the docker-cp hotfix died with the next recreate and Ayla was stranded on izlude 24,98 again
# ("Cannot calculate a route from izlude (24,98) to yuno").
# Mounted read-only by docker-compose: ./openkore -> /ok-src, rathena/npc-custom/functions_kafras.txt -> /ok-kafras.txt.
# Idempotent: files are only rewritten when their content changes (an unchanged portals.txt keeps the portal LOS
# cache, no recompile on a plain restart). Then execs the normal entrypoint with the given arguments.
SRC=${OK_SRC:-/ok-src}
OK=/openkore
LOG=$OK/logs/overlay.log
ts() { date -u '+%Y-%m-%dT%H:%M:%SZ'; }
say() { echo "[overlay] $*"; { echo "$(ts) $*" >> "$LOG"; } 2>/dev/null; return 0; }
alert() { say "!!!!! $* !!!!!"; { echo "$(ts) overlay: $*" >> "$OK/logs/alerts.log"; } 2>/dev/null; return 0; }
cd "$OK" || exit 1
mkdir -p "$OK/logs" 2>/dev/null

if [ -d "$SRC/fields-re" ]; then
  n=0
  for f in "$SRC"/fields-re/*.fld2.gz; do
    [ -e "$f" ] || continue
    b=$(basename "$f" .fld2.gz)
    if ! cmp -s "$f" "fields/$b.fld2.gz"; then
      cp "$f" "fields/$b.fld2.gz" && rm -f "fields/$b.dist" "fields/$b.weight" "fields/$b.fld2" && n=$((n + 1))
    fi
  done
  say "fields-re: $n field file(s) replaced"
  # self-check: every server-matching field must now be the one OpenKore loads
  bad=""
  for f in "$SRC"/fields-re/*.fld2.gz; do
    [ -e "$f" ] || continue
    b=$(basename "$f" .fld2.gz)
    cmp -s "$f" "fields/$b.fld2.gz" || bad="$bad $b"
    [ -e "fields/$b.fld2" ] && bad="$bad $b(an uncompressed .fld2 shadows it)"
  done
  if [ -n "$bad" ]; then
    alert "FIELD MISMATCH: /openkore/fields differs from openkore/fields-re for:$bad - routes on these maps WILL fail"
  else
    say "self-check ok: all fields-re files match /openkore/fields"
  fi
else
  alert "FIELD MISMATCH: $SRC/fields-re not mounted - classic izlude/morocc/alberta fields in use, routes there WILL fail"
fi

# portals: Renewal Izlude first, then the Kafra lines (fix_kafra_portals.py must stay the last patch)
if [ -f "$SRC/fix_re_portals.sh" ]; then
  sh "$SRC/fix_re_portals.sh" tables/portals.txt 2>&1 | while read -r l; do say "$l"; done
fi
if [ -f "$SRC/fix_kafra_portals.py" ]; then
  K=""; [ -f /ok-kafras.txt ] && K=/ok-kafras.txt
  cp tables/portals.txt /tmp/portals.kafra
  python3 -I "$SRC/fix_kafra_portals.py" /tmp/portals.kafra $K 2>&1 | while read -r l; do say "$l"; done
  if [ -s /tmp/portals.kafra ] && ! cmp -s /tmp/portals.kafra tables/portals.txt; then
    cp /tmp/portals.kafra tables/portals.txt && say "portals.txt: Kafra lines rewritten"
  fi
fi
if grep -q '^izlude 20 98 prt_fild08 ' tables/portals.txt && ! grep -q '^izlude 30 78 ' tables/portals.txt; then
  say "self-check ok: Renewal Izlude portals present"
else
  alert "PORTAL MISMATCH: classic izlude portals still in tables/portals.txt - izlude routes WILL fail"
fi

exec /entrypoint.sh "$@"

#!/bin/sh
# fix_re_portals.sh - OpenKore's tables/portals.txt has the classic Izlude layout; the server runs Renewal Izlude
# (rathena npc/re/warps/cities/izlude.txt, map 268x300). Run inside the OpenKore tree (Dockerfile / docker exec).
# 2026-11-14 (Ayla stuck in izlude: every getRoute failed).
set -e
P=${1:-tables/portals.txt}
cp "$P" "$P.bak-re" 2>/dev/null || true
# drop the classic izlude portals (and the ship / dungeon lines whose cells moved; Kafra lines: fix_kafra_portals.py)
grep -vE '^(izlude (30 78|52 140|109 151|148 148|216 129|201 181) |izlude_in (65 84|74 158|116 46|148 127) izlude |prt_fild08 371 212 izlude |alberta 189 151 izlude )' "$P" > "$P.tmp"
# (Kafra "c r2 c rN" lines belong to fix_kafra_portals.py, which runs after this script: none are touched here)
cat >> "$P.tmp" <<'EOT'
# Renewal Izlude (rathena npc/re/warps/cities/izlude.txt), 2026-11-14
izlude 20 98 prt_fild08 367 212
prt_fild08 371 212 izlude 24 98
izlude 110 182 izlude_in 65 87
izlude_in 65 84 izlude 112 179
izlude 52 172 izlude_in 74 161
izlude_in 74 158 izlude 52 168
izlude_in 116 46 izlude 128 98
izlude_in 148 127 izlude 210 161
izlude 213 161 arena_room 100 30
alberta 189 151 izlude 195 212 500 c r1
EOT
mv "$P.tmp" "$P"
echo "portals.txt patched for Renewal Izlude"

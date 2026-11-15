#!/bin/sh
# fix_re_portals.sh - OpenKore's tables/portals.txt has the classic Izlude layout; the server runs Renewal Izlude
# (rathena npc/re/warps/cities/izlude.txt, map 268x300). Run inside the OpenKore tree (Dockerfile, or the
# openkore-ayla start step openkore/overlay.sh on every container start).
# 2026-11-14 (Ayla stuck in izlude: every getRoute failed). Idempotent: it drops every classic/stale line and every line
# it wrote before, then appends one marked block. The file is only rewritten when the result differs (an unchanged
# portals.txt keeps OpenKore's portal LOS cache valid, no recompile on a plain restart).
# Sources (server, rAthena 1ca3e77): npc/re/warps/cities/izlude.txt, npc/re/warps/other/arena.txt, npc/cities/izlude.txt
# (Sailor_izlude at izlude 197,205 in npc/re/cities/izlude.txt; Sailor#2izlude izlu2dun 108,27 -> izlude 197,210),
# npc/cities/alberta.txt (Fisk -> izlude 195,212), npc/re/airports/izlude.txt (Airship Staff izlude 202,75).
set -e
P=${1:-tables/portals.txt}
T="$P.tmp-re"
# drop: the classic izlude portals (old cells), the old ship/arena/airship/izlude_in lines, the lines an earlier
# (non-idempotent) version of this script appended, and prontera 98,120 -> yuno (no such NPC on this server: Ayla
# "Failed to teleport using NPC at prontera (98,120) after 5 tries", 2026-11-15 02:32).
# Kafra "c r2 c rN" lines belong to fix_kafra_portals.py (runs after this script): none are touched here.
B="$P.block-re"
cat > "$B" <<'EOT'
# Renewal Izlude (fix_re_portals.sh, from the server's rathena npc files)
izlude 20 98 prt_fild08 367 212
prt_fild08 371 212 izlude 24 98
izlude 110 182 izlude_in 65 87
izlude_in 65 84 izlude 112 179
izlude 52 172 izlude_in 74 161
izlude_in 74 158 izlude 52 168
izlude_in 116 46 izlude 128 98
izlude_in 148 127 izlude 210 161
izlude_in 43 169 izlude_in 63 169
izlude_in 87 169 izlude_in 111 169
izlude_in 108 169 izlude_in 84 169
izlude_in 172 139 izlude_in 172 161
izlude_in 172 158 izlude_in 172 136
izlude_in 172 116 izlude_in 172 94
izlude_in 171 97 izlude_in 172 119
izlude 213 161 arena_room 100 30
arena_room 99 24 izlude 209 161
izlude 197 205 izlu2dun 107 50 150 c r0
izlude 197 205 alberta 188 169 500 c r1
izlu2dun 108 27 izlude 197 210 0 c r0
alberta 189 151 izlude 195 212 500 c r1
izlude 202 75 airplane_01 244 58 1200 c r0 c r0
# end Renewal Izlude
EOT
# the block goes where it was before (so a re-run gives a byte-identical file even after fix_kafra_portals.py appended
# its own block behind it), else at the end
awk -v blockfile="$B" '
  function block(   l) { if (!done) { while ((getline l < blockfile) > 0) print l; done = 1 } }
  END { block() }
  /^# Renewal Izlude \(fix_re_portals.sh/ { block(); skip = 1; next }
  skip && /^# end Renewal Izlude/ { skip = 0; next }
  skip { next }
  /^# Renewal Izlude \(rathena npc\/re\/warps\/cities\/izlude.txt\), 2026-11-14$/ { next }
  /^izlude (30 78|52 140|109 151|148 148|216 129|201 181|149 39|176 56|128 226|206 55|20 98|110 182|52 172|213 161|197 205|202 75) / { next }
  /^izlude_in (65 84|74 158|116 46|148 127|43 169|87 169|108 169|171 97|172 116|172 139|172 158) / { next }
  /^(prt_fild08 371 212|alberta 189 151|izlu2dun 108 27|arena_room 99 24) izlude / { next }
  /^prontera 98 120 yuno / { next }
  { print }
' "$P" > "$T"
rm -f "$B"
if cmp -s "$T" "$P"; then
  rm -f "$T"; echo "portals.txt already patched for Renewal Izlude"
else
  cp "$P" "$P.bak-re" 2>/dev/null || true
  mv "$T" "$P"; echo "portals.txt patched for Renewal Izlude"
fi

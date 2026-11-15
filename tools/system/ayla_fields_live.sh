#!/bin/bash
# ayla_fields_live.sh [container] - put the server-matching fields and portal patches into a RUNNING OpenKore
# container without recreating it (file copies only; sends nothing to OpenKore). Afterwards OpenKore needs
# `reload portals` + `portals recompile` (main decides when) and picks up a changed field on its next map change.
# The bind-mount + overlay.sh in docker-compose make the same change permanent on the next recreate.
set -e
C=${1:-ro-openkore-ayla}
W=$(cd "$(dirname "$0")/../.." && pwd)
docker exec "$C" sh -c 'rm -rf /tmp/ok-src && mkdir -p /tmp/ok-src'
docker cp "$W/openkore/fields-re" "$C":/tmp/ok-src/fields-re
for f in fix_re_portals.sh fix_srv_portals.py portals-srv.txt fix_kafra_portals.py; do docker cp "$W/openkore/$f" "$C":/tmp/ok-src/$f; done
docker cp "$W/rathena/npc-custom/functions_kafras.txt" "$C":/tmp/ok-src/functions_kafras.txt
docker exec "$C" sh -c '
cd /openkore
n=0; for f in /tmp/ok-src/fields-re/*.fld2.gz; do b=$(basename "$f" .fld2.gz)
  cmp -s "$f" "fields/$b.fld2.gz" || { cp "$f" fields/ && rm -f "fields/$b.dist" "fields/$b.weight" "fields/$b.fld2"; n=$((n+1)); }; done
echo "fields replaced: $n"
cp tables/portals.txt /tmp/portals.before
sh /tmp/ok-src/fix_re_portals.sh tables/portals.txt
python3 -I /tmp/ok-src/fix_srv_portals.py tables/portals.txt /tmp/ok-src/portals-srv.txt
python3 -I /tmp/ok-src/fix_kafra_portals.py tables/portals.txt /tmp/ok-src/functions_kafras.txt
if cmp -s /tmp/portals.before tables/portals.txt; then echo "portals.txt unchanged: no reload needed"
else cp /tmp/portals.before tables/portals.txt.bak-live; echo "portals.txt CHANGED: run reload portals, then portals recompile"; fi'

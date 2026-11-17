# rathena_recreate.sh - DELIBERATE, owner-approved recreate of ro-rathena (and ro-db if its config changed), then
# every container that shares rathena's network namespace, then wait until Ayla is back in game.
# Owner rule (RULES.md 0, 2026-11-14): "Deploys never recreate rathena by accident; Ayla follows rathena".
#
# Usage (WSL):  bash /mnt/g/RO/legion/tools/deploy/rathena_recreate.sh            -> plan only (dry run), changes nothing
#               bash /mnt/g/RO/legion/tools/deploy/rathena_recreate.sh --yes      -> do it (kicks EVERYONE, Ayla too)
#               add --build to rebuild the ro-rathena image first (rathena/ source or build args changed)
#               followers keep their RUNNING image (pinned); --use-compose-image if a running image is gone
#
# Environment variables:
#   COMPOSE_FILE - docker compose manifest to use (default: docker-compose.yml)
#   RATHENA_WAIT - seconds to wait for rathena map-server startup (default: 240)
#   AYLA_WAIT - seconds to wait for Ayla to log back in (default: 300)
#
# Services with network_mode "service:rathena" (from COMPOSE_FILE): openkore-ayla, openkore, legion-core, legion
# (+ mastermind, profile "fallback", only if its container exists). When rathena is recreated they keep the dead
# namespace of the old container (offline, no error), so they are force-recreated right after it, Ayla first.
# Never sends anything to Ayla's OpenKore (in particular never ai / ai off / ai manual): only container recreates.
set -eo pipefail
cd /mnt/g/RO/legion
G="$(dirname "$0")/guard.sh"; [ -f "$G" ] || G=/mnt/g/RO/legion/tools/deploy/guard.sh
# shellcheck source=guard.sh
. "$G"

YES=0; BUILD=0; USE_COMPOSE=0
for a in "$@"; do
  case "$a" in
    --yes) YES=1;;
    --build) BUILD=1;;
    --use-compose-image) USE_COMPOSE=1;;
    -h|--help) sed -n '2,14p' "$0"; exit 0;;
    *) echo "unknown option $a"; exit 1;;
  esac
done
[ "$YES" = 1 ] || DRY_RUN=1
rathena_gate   # at most one recreate per hour (DRY_RUN only reports)

AYLA_STATUS=/mnt/g/RO/legion/openkore-ayla/logs/status.json
RATHENA_WAIT=${RATHENA_WAIT:-240}   # s for "Map-server 0 loading complete."
AYLA_WAIT=${AYLA_WAIT:-300}         # s for Ayla back in game

# Followers in recreate order (Ayla first). Taken from the compose file, so a new service:rathena user is not missed.
followers() {
  docker compose -f "$COMPOSE_FILE" config --format json | python3 -c '
import json,sys
svcs=json.load(sys.stdin)["services"]
f=[n for n,s in svcs.items() if s.get("network_mode","") == "service:rathena"]
order={"openkore-ayla":0,"legion-core":1,"openkore":2,"legion":3}
print(" ".join(sorted(f,key=lambda n:(order.get(n,9),n))))'
}
FOLLOW=$(followers)
# mastermind is profile-gated: recreate it only if a container exists.
F2=""
for s in $FOLLOW; do
  if [ "$s" = mastermind ] && ! docker inspect ro-mastermind >/dev/null 2>&1; then continue; fi
  F2="$F2 $s"
done
FOLLOW=${F2# }

# Pin every follower to the image ID it runs now: tag it ro-pin/<svc>:<id12> and write a compose override, so the
# recreate never picks up a newer :latest (10:20 2026-11-14: legion/legion-core came back on an untested build).
# If a running image no longer exists (removed while the container ran), stop BEFORE touching rathena unless
# --use-compose-image accepts the compose tag for those services.
PIN=/tmp/rathena_recreate_pin.yml
echo "services:" > "$PIN"
MISSING=""
for s in $FOLLOW; do
  c=$(svc_field "$s" container_name); [ -n "$c" ] || c="ro-$s"
  img=$(docker inspect -f '{{.Image}}' "$c" 2>/dev/null || true)
  if [ -z "$img" ]; then echo "pin:     $s has no container; it starts on its compose image"; continue; fi
  if ! docker image inspect "$img" >/dev/null 2>&1; then
    MISSING="$MISSING $s"
    echo "pin:     $s runs image ${img:7:12}, which no longer exists - cannot keep it on that image"
    continue
  fi
  tag="ro-pin/$s:${img:7:12}"
  run docker tag "$img" "$tag"
  printf '  %s:
    image: %s
' "$s" "$tag" >> "$PIN"
  drift=""; r=$(svc_image_drift "$s") && drift=" (compose tag is newer: kept off it)"
  echo "pin:     $s stays on its running image ${img:7:12} as $tag$drift"
done
[ "$(wc -l < "$PIN")" -gt 1 ] || echo "services: {}" > "$PIN"   # no pins: keep the override valid YAML
if [ -n "$MISSING" ]; then
  if [ "$USE_COMPOSE" = 1 ]; then
    for s in $MISSING; do echo "WARNING: $s comes back on its compose image $(svc_field "$s" image) (--use-compose-image)"; done
  else
    echo "STOP: running image gone for:$MISSING. Rebuild/choose a tested image first, or rerun with"
    echo "      --use-compose-image to accept the compose tag for them. Nothing was changed."
    exit 4
  fi
fi

DBCHG=""
if r=$(svc_changed db); then DBCHG="$r"; fi
RCHG=""
if r=$(svc_changed rathena); then RCHG="$r"; fi

echo "rathena: ${RCHG:-config unchanged (recreating anyway, as asked)}"
for s in db rathena; do if r=$(svc_image_drift "$s"); then echo "image:   $r -> the recreate picks up the new image"; fi; done
[ -n "$DBCHG" ] && echo "db:      $DBCHG -> db is recreated first"
echo "followers (service:rathena), recreated right after rathena, in order: $FOLLOW"
[ "$DRY_RUN" = 1 ] && echo "PLAN ONLY (no --yes): nothing will be changed."

T0=$(date +%s)
T0ISO=$(date -u +%Y-%m-%dT%H:%M:%SZ)

[ "$BUILD" = 1 ] && run docker compose build rathena
if [ -n "$DBCHG" ]; then
  run docker compose up -d --no-deps db
  if [ "$DRY_RUN" != 1 ]; then
    echo -n "waiting for ro-db healthy"
    for _ in $(seq 1 60); do
      [ "$(docker inspect -f '{{.State.Health.Status}}' ro-db 2>/dev/null)" = healthy ] && break
      echo -n .; sleep 3
    done; echo
  fi
fi

[ "$DRY_RUN" = 1 ] || "$PYTHON" "$SYSDIR/deploylog.py" rathena-record ${RATHENA_HOUR_OVERRIDE:+--override "$RATHENA_HOUR_OVERRIDE"} || true
run docker compose up -d --no-deps --force-recreate rathena

if [ "$DRY_RUN" != 1 ]; then
  echo -n "waiting for rathena map-server"
  ok=0
  for _ in $(seq 1 $((RATHENA_WAIT / 3))); do
    if docker logs --since "$T0ISO" ro-rathena 2>&1 | grep -q "Map-server 0 loading complete"; then ok=1; break; fi
    echo -n .; sleep 3
  done; echo
  [ "$ok" = 1 ] && echo "rathena is up" || echo "WARNING: no 'Map-server 0 loading complete' after ${RATHENA_WAIT}s; recreating followers anyway"
fi

# Followers: must be recreated even if the map-server is slow, else they stay on the dead namespace.
# Each keeps the image it is RUNNING (recorded in $PIN before anything was touched), never the current :latest tag
# (10:20 2026-11-14: legion/legion-core came back on an untested ro-legion:latest).
# shellcheck disable=SC2086
run docker compose -f docker-compose.yml -f "$PIN" up -d --no-deps --no-build --pull never --force-recreate $FOLLOW

[ "$DRY_RUN" = 1 ] && { echo "DRY_RUN: nothing changed"; exit 0; }

# Verify every follower sits in the NEW rathena namespace.
RID=$(docker inspect -f '{{.Id}}' ro-rathena)
bad=0
for s in $FOLLOW; do
  c=$(docker compose ps -a --format '{{.Name}}' "$s" | head -1); [ -n "$c" ] || c="ro-$s"
  ns=$(docker inspect -f '{{.HostConfig.NetworkMode}} {{.State.Running}}' "$c" 2>/dev/null || echo "missing")
  if [ "$ns" = "container:$RID true" ]; then echo "ok   $c in rathena's namespace, running"
  else echo "BAD  $c: $ns (want container:${RID:0:12} true)"; bad=1; fi
done

# Wait for Ayla back in game: status.json (statusout.pl) online with a fresh timestamp, or the OpenKore log line.
echo -n "waiting for Ayla to log in"
in=0
for _ in $(seq 1 $((AYLA_WAIT / 5))); do
  if python3 - "$AYLA_STATUS" "$T0" <<'PY' 2>/dev/null; then in=1; break; fi
import json,sys
s=json.load(open(sys.argv[1]))
sys.exit(0 if s.get("online") and int(s.get("ts",0)) > int(sys.argv[2]) + 5 and s.get("map") else 1)
PY
  if docker logs --since "$T0ISO" ro-openkore-ayla 2>&1 | grep -q "You are now in the game"; then in=1; break; fi
  echo -n .; sleep 5
done; echo
if [ "$in" = 1 ]; then
  echo "Ayla is back in game: $(cat "$AYLA_STATUS" 2>/dev/null)"
  echo "  (check: wsl -d Ubuntu-24.04 -- bash /mnt/g/RO/legion/tools/rogui/ayla.sh status)"
else
  echo "ERROR: Ayla not back in game after ${AYLA_WAIT}s. Last log lines:"
  docker logs --tail 15 ro-openkore-ayla 2>&1 | sed -e 's/\x1b\[[0-9;?]*[a-zA-Z]//g'
  echo "Try: docker compose up -d --no-deps --force-recreate openkore-ayla   (never send ai/ai off/ai manual)"
  exit 1
fi
[ "$bad" = 0 ] || { echo "some followers are not in rathena's namespace (see BAD above)"; exit 1; }
echo "rathena recreate done; all service:rathena containers follow it"

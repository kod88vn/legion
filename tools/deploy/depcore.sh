# depcore.sh - redeploy legion-core (Ayla's party bots), pinned to one Legion deploy image.
# Builds a fresh image from a frozen snapshot of legion/ first (same as dep3.sh: tested copy = shipped copy).
# Never touches rathena/db/agency/openkore*: `up --no-deps legion-core`, after guard_core (tools/deploy/guard.sh)
# confirmed db/rathena would not change. DRY_RUN=1 = checks only, nothing built or deployed.
set -eo pipefail
cd /mnt/g/RO/legion
G="$(dirname "$0")/guard.sh"; [ -f "$G" ] || G=/mnt/g/RO/legion/tools/deploy/guard.sh
# shellcheck source=guard.sh
. "$G"
LOG=/tmp/legion-core-build.log
NAME=ro-legion-core

guard_core
snapshot_build "$LOG" || { echo "BUILD FAILED - nothing deployed"; exit 1; }
[ "$DRY_RUN" = 1 ] || guard_core   # again after the build
if [ "$DRY_RUN" = 1 ]; then deploy_up_img "${DEPLOY_TAG:-ro-legion:deploy-DRYRUN}" legion-core; echo "DRY_RUN: nothing built or deployed"; exit 0; fi

WANT=$(docker image inspect -f '{{.Id}}' "$DEPLOY_TAG")
deploy_up_img "$DEPLOY_TAG" legion-core >"$LOG" 2>&1 || { tail -30 "$LOG"; echo "UP FAILED for legion-core"; exit 1; }
tail -1 "$LOG"

# Recover from a recreate that left "<hash>_ro-legion-core" behind in Created state.
if ! docker inspect "$NAME" >/dev/null 2>&1; then
  ODD=$(docker ps -a --format '{{.Names}}' | grep -E "^[0-9a-f]+_${NAME}\$" | head -1 || true)
  [ -n "$ODD" ] || { echo "ERROR: no $NAME container found"; exit 1; }
  echo "fixing stray container $ODD -> $NAME"
  docker start "$ODD" >/dev/null
  docker rename "$ODD" "$NAME"
fi
[ "$(docker inspect -f '{{.State.Running}}' "$NAME")" = true ] || docker start "$NAME" >/dev/null
[ "$(docker inspect -f '{{.State.Running}}' "$NAME")" = true ] || { echo "ERROR: $NAME is not running"; exit 1; }

RUNIMG=$(docker inspect -f '{{.Image}}' "$NAME")
if [ "$RUNIMG" != "$WANT" ]; then
  echo "ERROR: $NAME runs $RUNIMG but deploy image $DEPLOY_TAG is $WANT"; exit 1
fi
echo "image:   $DEPLOY_TAG ($WANT)"
echo "started: $(docker inspect -f '{{.State.StartedAt}}' "$NAME")"
echo "core redeployed (--no-deps; rathena/db/openkore untouched)"

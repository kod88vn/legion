# dep3.sh - build the Legion image from a FROZEN SNAPSHOT of legion/ and redeploy the MAIN legion (ro-legion) only.
# gofmt/vet/go test run on the same snapshot the image is built from (guard.sh snapshot_build), tagged
# ro-legion:deploy-<time> and ro-legion:latest; the tag is saved in /tmp/legion-deploy.tag for depcore_nobuild.sh.
# Never touches rathena/db/agency/openkore*: `up --no-deps legion` pinned to that tag, after guard_core confirmed
# db/rathena would not change. DRY_RUN=1 = checks only, nothing built or deployed.
set -eo pipefail
cd /mnt/g/RO/legion
G="$(dirname "$0")/guard.sh"; [ -f "$G" ] || G=/mnt/g/RO/legion/tools/deploy/guard.sh
# shellcheck source=guard.sh
. "$G"
LOG=/tmp/legion-build.log

parse_deploy_args "$@"
deploy_gate
guard_core
snapshot_build "$LOG" || { echo "BUILD FAILED - nothing deployed"; exit 1; }
[ "$DRY_RUN" = 1 ] || guard_core   # again: compose may have changed during the build
deploy_up_img "$DEPLOY_TAG" legion >"$LOG" 2>&1 || { tail -30 "$LOG"; echo "UP FAILED for legion"; exit 1; }
[ "$DRY_RUN" = 1 ] && { cat "$LOG"; echo "DRY_RUN: nothing built or deployed"; exit 0; }
tail -1 "$LOG"
RUNIMG=$(docker inspect -f '{{.Image}}' ro-legion)
WANT=$(docker image inspect -f '{{.Id}}' "$DEPLOY_TAG")
[ "$RUNIMG" = "$WANT" ] || { echo "ERROR: ro-legion runs ${RUNIMG:7:12}, deploy image $DEPLOY_TAG is ${WANT:7:12}"; exit 1; }
record_deploy "$DEPLOY_TAG" legion
echo "legion redeployed on $DEPLOY_TAG (${WANT:7:12}; --no-deps; rathena/db/openkore untouched)"
echo "core: bash /mnt/g/RO/legion/tools/deploy/depcore_nobuild.sh   (uses $DEPLOY_TAG)"

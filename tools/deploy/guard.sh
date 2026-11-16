# guard.sh - helpers sourced by tools/deploy/rathena_recreate.sh, the only script that recreates rathena (G15, G16).
# Owner rule (RULES.md 0, 2026-11-14): "Deploys never recreate rathena by accident; Ayla follows rathena".
# The Legion batch deploy is tools/deploy/deploy.py (PR47, tested); it checks db and rathena itself and never recreates them.
#
# DRY_RUN=1  -> run every check, print the recreate commands instead of running them (nothing is changed).

DRY_RUN=${DRY_RUN:-0}

# ---- rathena gate (tools/system/deploylog.py) -----------------------------------------------------------------------
GUARD_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
SYSDIR=$GUARD_DIR/../system; [ -f "$SYSDIR/deploylog.py" ] || SYSDIR=/mnt/g/RO/legion/tools/system
PYTHON=${PYTHON:-python3}

# rathena_gate: one recreate per hour at most (owner rule). RATHENA_HOUR_OVERRIDE="<reason>" for a broken rathena.
rathena_gate() {
  local out
  if out=$("$PYTHON" "$SYSDIR/deploylog.py" rathena-check ${RATHENA_HOUR_OVERRIDE:+--override "$RATHENA_HOUR_OVERRIDE"} 2>&1); then
    echo "gate: $out"; return 0
  fi
  echo "$out"
  if [ "$DRY_RUN" = 1 ]; then echo "PLAN: a real run would be refused now"; return 0; fi
  exit 5
}

# run CMD... : run it, or only print it in dry-run mode.
run() {
  if [ "$DRY_RUN" = 1 ]; then echo "DRY_RUN would run: $*"; return 0; fi
  "$@"
}

# Is the service's container out of date vs docker-compose.yml (config hash or image)? Prints the reason.
svc_changed() {
  local svc=$1 cname want have
  cname=$(svc_field "$svc" container_name); [ -n "$cname" ] || cname="ro-$svc"
  if ! docker inspect "$cname" >/dev/null 2>&1; then echo "$svc: container $cname does not exist"; return 0; fi
  want=$(docker compose config --hash "$svc" 2>/dev/null | awk '{print $2}')
  have=$(docker inspect -f '{{index .Config.Labels "com.docker.compose.config-hash"}}' "$cname")
  if [ -z "$want" ]; then echo "$svc: could not compute compose config hash"; return 0; fi
  if [ "$want" != "$have" ]; then echo "$svc: compose config hash changed (running ${have:0:12}, compose ${want:0:12})"; return 0; fi
  return 1
}

# Was the service's image tag rebuilt since its container started? Warning only: compose itself does not recreate
# for that on a plain `up` (its dry-run, checked in guard_core, is the authority); it takes effect at rathena_recreate.sh.
svc_image_drift() {
  local svc=$1 cname tag img_want img_have
  cname=$(svc_field "$svc" container_name); [ -n "$cname" ] || cname="ro-$svc"
  tag=$(svc_field "$svc" image); [ -n "$tag" ] || return 1
  img_want=$(docker image inspect -f '{{.Id}}' "$tag" 2>/dev/null || true)
  img_have=$(docker inspect -f '{{.Image}}' "$cname" 2>/dev/null || true)
  if [ -n "$img_want" ] && [ -n "$img_have" ] && [ "$img_want" != "$img_have" ]; then
    echo "$svc: image $tag rebuilt (running ${img_have:7:12}, tag now ${img_want:7:12})"; return 0
  fi
  return 1
}

svc_field() {  # svc_field SERVICE KEY -> value of a top-level key of the service in the resolved compose config
  docker compose config --format json 2>/dev/null \
    | python3 -c 'import json,sys; print(json.load(sys.stdin)["services"].get(sys.argv[1],{}).get(sys.argv[2],""))' "$1" "$2" 2>/dev/null || true
}


# guard.sh - sourced by the Legion deploy scripts (dep3.sh, depcore.sh, depcore_nobuild.sh).
# Owner rule (RULES.md 0, 2026-11-14): "Deploys never recreate rathena by accident; Ayla follows rathena".
# Legion deploys touch ONLY legion / legion-core, always with --no-deps. Before deploying, check that the compose
# config (and image) of db and rathena still match the running containers; if not, a plain `docker compose up` would
# recreate them, so STOP and leave it to tools/deploy/rathena_recreate.sh (run deliberately, owner-approved).
#
# DRY_RUN=1  -> run every check, print the build/up commands instead of running them (nothing is deployed).

DEPLOY_ALLOWED="legion legion-core"
DRY_RUN=${DRY_RUN:-0}

# run CMD... : run it, or only print it in dry-run mode.
run() {
  if [ "$DRY_RUN" = 1 ]; then echo "DRY_RUN would run: $*"; return 0; fi
  "$@"
}

# The only allowed `up`: listed services must be legion/legion-core, always --no-deps.
deploy_up() {
  local s
  for s in "$@"; do
    case " $DEPLOY_ALLOWED " in *" $s "*) ;; *) echo "REFUSED: deploy scripts may only recreate: $DEPLOY_ALLOWED (got '$s')"; exit 3;; esac
  done
  run docker compose up -d --no-deps --no-build "$@"
}

# deploy_up_img TAG SVC... : same, but the services run exactly image TAG (compose override), so a :latest moved by
# another agent's build between our build and our `up` is never picked up.
deploy_up_img() {
  local tag=$1 s ovr=/tmp/legion-deploy-override.yml; shift
  for s in "$@"; do
    case " $DEPLOY_ALLOWED " in *" $s "*) ;; *) echo "REFUSED: deploy scripts may only recreate: $DEPLOY_ALLOWED (got '$s')"; exit 3;; esac
  done
  echo "services:" > "$ovr"
  for s in "$@"; do printf '  %s:\n    image: %s\n' "$s" "$tag" >> "$ovr"; done
  run docker compose -f docker-compose.yml -f "$ovr" up -d --no-deps --no-build --pull never "$@"
}

# Frozen-snapshot build (2026-11-14): other agents edit legion/*.go all the time, so copy what the Dockerfile needs
# to a temp dir, gofmt/vet/test THAT copy, and build the image from the same copy: what is tested is what ships.
# Sets DEPLOY_TAG=ro-legion:deploy-<time>, also tags ro-legion:latest, records it in $DEPLOY_TAG_FILE.
DEPLOY_TAG_FILE=/tmp/legion-deploy.tag
SRC=/mnt/g/RO/legion/legion
snapshot_build() {
  local log=$1 ts snap try
  ts=$(date +%Y%m%d-%H%M%S)
  DEPLOY_TAG="ro-legion:deploy-$ts"
  for try in 1 2; do
    snap=$(mktemp -d /tmp/legion-snap-XXXXXX)
    # exactly the Dockerfile's inputs (COPY go.mod *.go *.html recvpackets.txt) + the Dockerfile itself
    cp "$SRC"/Dockerfile "$SRC"/go.mod "$SRC"/*.go "$SRC"/*.html "$SRC"/recvpackets.txt "$snap"/
    echo "snapshot $snap ($(find "$snap" -name '*.go' | wc -l) .go files, try $try)"
    if [ "$DRY_RUN" = 1 ]; then
      run docker run --rm -v "$snap":/src -w /src golang:1.22 sh -c 'gofmt -l -w . ; go vet ./... && go test ./...'
      run docker build -t "$DEPLOY_TAG" -t ro-legion:latest "$snap"
      rm -rf "$snap"; return 0
    fi
    if docker run --rm -v "$snap":/src -w /src golang:1.22 sh -c \
         'f=$(gofmt -l .); [ -n "$f" ] && echo "gofmt fixed (snapshot only): $f"; gofmt -w . && go vet ./... && go test -count=1 ./...' \
         >"$log" 2>&1; then
      grep "gofmt fixed" "$log" || true
      if docker build -t "$DEPLOY_TAG" -t ro-legion:latest "$snap" >>"$log" 2>&1; then
        echo "$DEPLOY_TAG" > "$DEPLOY_TAG_FILE"
        echo "built $DEPLOY_TAG (= ro-legion:latest) from $snap"
        rm -rf "$snap"; return 0
      fi
      tail -30 "$log"; rm -rf "$snap"; echo "docker build failed"; return 1
    fi
    rm -rf "$snap"
    if [ "$try" = 1 ]; then
      echo "vet/test failed on the snapshot (maybe a half-saved file); new snapshot in 60 s:"; tail -15 "$log"; sleep 60
    fi
  done
  tail -30 "$log"; echo "vet/test failed twice on fresh snapshots"; return 1
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

# Main guard: call before any build/up. Exits 2 (nothing deployed) if db or rathena would change.
guard_core() {
  local why="" r svc line bad=0
  for svc in db rathena; do
    if r=$(svc_changed "$svc"); then why="$why\n  - $r"; fi
  done
  # What a plain `up legion legion-core` WITH deps (the 10:12 accident) would do to db/rathena, per compose itself.
  while IFS= read -r line; do
    case "$line" in
      *Container*ro-rathena*Recreat*|*Container*ro-db*Recreat*|*Container*ro-rathena*Creat*|*Container*ro-db*Creat*)
        why="$why\n  - compose dry-run (with deps):$line";;
    esac
  done < <(docker compose up -d --no-build --dry-run legion legion-core 2>&1 || true)
  if [ -n "$why" ]; then
    echo "================================================================================"
    echo "rathena config changed — needs an owner-approved rathena recreate; not deploying"
    printf "%b\n" "$why"
    echo "Run deliberately (kicks everyone, Ayla included, until she logs back in):"
    echo "  wsl -d Ubuntu-24.04 -- bash /mnt/g/RO/legion/tools/deploy/rathena_recreate.sh --yes"
    echo "================================================================================"
    exit 2
  fi
  # Belt and braces: ask compose itself what `up --no-deps legion legion-core` would touch.
  while IFS= read -r line; do
    case "$line" in
      *Container*) case "$line" in *ro-legion-core*|*"ro-legion "*|*ro-legion) ;; *) echo "  dry-run touches: $line"; bad=1;; esac;;
    esac
  done < <(docker compose up -d --no-deps --no-build --dry-run legion legion-core 2>&1 || true)
  if [ "$bad" = 1 ]; then echo "compose dry-run would touch containers other than legion/legion-core; not deploying"; exit 2; fi
  for svc in db rathena; do
    if r=$(svc_image_drift "$svc"); then echo "NOTE: $r - not deployed now; takes effect at an owner-approved rathena_recreate.sh"; fi
  done
  # Followers whose compose tag moved past the image they run (e.g. ro-legion:latest rebuilt by another agent).
  # Only legion/legion-core deploys move them on purpose; rathena_recreate.sh keeps each on its running image.
  for svc in openkore-ayla openkore legion legion-core; do
    if r=$(svc_image_drift "$svc"); then echo "NOTE: $r - a rathena_recreate.sh keeps it on the running image"; fi
  done
  # Warn (don't block) if Ayla's OpenKore is not in rathena's current network namespace = she is offline.
  local rid ns c
  rid=$(docker inspect -f '{{.Id}}' ro-rathena 2>/dev/null || true)
  for c in ro-openkore-ayla ro-openkore; do
    ns=$(docker inspect -f '{{.HostConfig.NetworkMode}}' "$c" 2>/dev/null || true)
    if [ -n "$rid" ] && [ -n "$ns" ] && [ "$ns" != "container:$rid" ]; then
      echo "WARNING: $c is on a dead network namespace ($ns), not ro-rathena's. Tell main; fix with:"
      echo "  docker compose up -d --no-deps --force-recreate ${c#ro-}"
    fi
  done
  echo "guard: db/rathena unchanged; deploy touches only legion/legion-core (--no-deps)"
}

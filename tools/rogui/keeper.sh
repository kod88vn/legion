#!/bin/bash
# keeper.sh - keeps Ayla online. Ayla is played by OpenKore in XKore 0 (direct login, own reconnect logic),
# so the watch client (ai-client.sh) is only a camera and is never killed from here.
# Offline (char.online=0 in the DB) for ~5 min -> restart the container once; if that does not help, only log
# (investigate instead of looping restarts).
# HOLD marker (2026-11-14): while legion/data/stack.hold exists the owner has the stack stopped and this loop does
# nothing at all - it never starts, restarts or queries a container (a leftover keeper once restarted the stack
# during the migration). `python tools/system/stackctl.py down` creates it, `... up` removes it.
HOLD=/mnt/g/RO/legion/legion/data/stack.hold
LOG=/mnt/g/RO/legion/legion/data/watchdog.log
HB=/mnt/g/RO/legion/legion/data/keeper.heartbeat   # epoch each loop, read by the watchdog (tools/system/watchdog.py)
. "$(dirname "$0")/keeper-lock.sh"   # single instance: KEEPER_LOCK (see keeper-lock.sh)
cd /mnt/g/RO/legion
strikes=0; restarts=0
log() { echo "$(date '+%F %T') keeper: $*" | tee -a "$LOG"; }
# every loop (and after each long docker step) refreshes the heartbeat and the lock; a keeper whose lock was taken exits
beat() {
  keeper_refresh_lock || { log "keeper lock now held by another keeper; this copy exits"; exit 3; }
  echo "$(date +%s) $*" > "$HB" 2>/dev/null
}
keeper_take_lock || { log "another keeper holds $KEEPER_LOCK (fresh); this copy exits"; exit 3; }
while true; do
  if [ -e "$HOLD" ]; then
    beat "idle: stack.hold present"
    [ "${held:-0}" = 1 ] || { log "stack.hold present: keeper idle until it is removed"; held=1; }
    strikes=0; restarts=0; sleep 20 & wait $!; continue
  fi
  held=0
  beat "running"
  if ! docker ps --format '{{.Names}}' | grep -qx ro-openkore-ayla; then
    log "ro-openkore-ayla not running -> starting it"
    docker compose up -d --no-deps openkore-ayla >/dev/null 2>&1   # --no-deps (G15): never recreate rathena or the db
    beat "started ayla"
    sleep 60 & wait $!; continue
  fi
  # the server's own online flag (OpenKore's status title is not printed while she sits idle -> false alarms)
  if [ "$(docker exec ro-db mariadb -N -uragnarok -pragnarok ragnarok -e 'SELECT online FROM `char` WHERE char_id=150010' 2>/dev/null)" = "1" ]; then
    strikes=0; restarts=0
  else
    strikes=$((strikes+1))
  fi
  if [ $strikes -ge 15 ]; then
    if [ $restarts -lt 1 ]; then
      log "Ayla not in game for ~5 min -> restarting ro-openkore-ayla"
      docker compose restart openkore-ayla >/dev/null 2>&1; restarts=$((restarts+1)); beat "restarted ayla"
    else
      log "Ayla still not in game after a restart - needs investigation (last log: $(docker logs --tail 3 ro-openkore-ayla 2>&1 | tr '\n' ' ' | cut -c1-200))"
    fi
    strikes=0
  fi
  sleep 20 & wait $!
done

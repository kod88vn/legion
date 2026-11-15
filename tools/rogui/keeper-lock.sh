# keeper-lock.sh - single instance for keeper.sh (sourced by it; tests source it too).
# The lock file holds "<pid> <epoch>". The holder refreshes its mtime on every loop (keeper_refresh_lock). A lock
# refreshed within KEEPER_LOCK_STALE seconds belongs to a live keeper: a second keeper is refused (keeper.sh exits 3).
# A stale lock is moved aside with one atomic rename, so only one starter can move it; the moved file must really be
# stale, else it is put back and the start is refused. The holder removes the lock when its shell exits (EXIT and TERM
# traps), and only while the lock still names its pid.
KEEPER_LOCK=${KEEPER_LOCK:-/mnt/g/RO/legion/legion/data/keeper.lock}
KEEPER_LOCK_STALE=${KEEPER_LOCK_STALE:-150}

keeper_lock_age() {
  echo $(( $(date +%s) - $(stat -c %Y "$1") ))
}

keeper_lock_fresh() {
  [ -e "$KEEPER_LOCK" ] && [ "$(keeper_lock_age "$KEEPER_LOCK")" -le "$KEEPER_LOCK_STALE" ]
}

keeper_lock_owner() {
  cut -d' ' -f1 "$KEEPER_LOCK" 2>/dev/null
}

# on exit: remove the lock only while it still names this shell's pid
keeper_release_lock() {
  [ "$(keeper_lock_owner)" = "$$" ] && rm -f "$KEEPER_LOCK"
  return 0
}

# 0 = this shell now holds the lock; 1 = a live keeper holds it (or another starter won the takeover)
keeper_take_lock() {
  if [ -e "$KEEPER_LOCK" ]; then
    keeper_lock_fresh && return 1
    aside="$KEEPER_LOCK.stale.$$"
    mv "$KEEPER_LOCK" "$aside" 2>/dev/null || return 1      # atomic: only one starter moves the stale lock
    if [ "$(keeper_lock_age "$aside")" -le "$KEEPER_LOCK_STALE" ]; then
      # it was fresh after all (a starter created it between our look and the move): put it back, refuse
      mv -n "$aside" "$KEEPER_LOCK" 2>/dev/null
      [ -e "$aside" ] && rm -f "$aside"
      return 1
    fi
    rm -f "$aside"
  fi
  ( set -C; echo "$$ $(date +%s)" > "$KEEPER_LOCK" ) 2>/dev/null || return 1
  trap keeper_release_lock EXIT
  trap 'exit 143' TERM
  return 0
}

# 0 = still ours (time refreshed); 1 = the lock belongs to someone else now: the caller exits
keeper_refresh_lock() {
  [ "$(keeper_lock_owner)" = "$$" ] || return 1
  touch "$KEEPER_LOCK"
}

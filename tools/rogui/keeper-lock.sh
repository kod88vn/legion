# keeper-lock.sh - single instance for keeper.sh (sourced by it; tests source it too).
# The lock file holds "<pid> <epoch>". The holder refreshes its mtime on every loop (keeper_refresh_lock). A lock
# refreshed within KEEPER_LOCK_STALE seconds belongs to a live keeper: a second keeper exits at once. A lock older
# than that is replaced, and the old keeper exits on its next refresh (its pid is no longer in the lock).
KEEPER_LOCK=${KEEPER_LOCK:-/mnt/g/RO/legion/legion/data/keeper.lock}
KEEPER_LOCK_STALE=${KEEPER_LOCK_STALE:-150}

keeper_lock_fresh() {
  [ -e "$KEEPER_LOCK" ] && [ $(( $(date +%s) - $(stat -c %Y "$KEEPER_LOCK") )) -le "$KEEPER_LOCK_STALE" ]
}

keeper_lock_owner() {
  cut -d' ' -f1 "$KEEPER_LOCK" 2>/dev/null
}

# 0 = this shell now holds the lock; 1 = a live keeper holds it
keeper_take_lock() {
  keeper_lock_fresh && return 1
  rm -f "$KEEPER_LOCK"
  ( set -C; echo "$$ $(date +%s)" > "$KEEPER_LOCK" ) 2>/dev/null
}

# 0 = still ours (time refreshed); 1 = the lock belongs to someone else now: the caller exits
keeper_refresh_lock() {
  [ "$(keeper_lock_owner)" = "$$" ] || return 1
  touch "$KEEPER_LOCK"
}

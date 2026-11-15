#!/bin/bash
# ayla_safe_reload.sh [--anywhere] PLUGIN [PLUGIN...] - reload Ayla's OpenKore plugins one at a time, 30 s apart, only
# at a calm moment (2026-11-14: a reload batch while she fought ended in "Timeout on Map Server").
#   default     : she is in a town and not shopping / fighting / talking to an NPC
#   --anywhere  : (safety-critical fixes) any map, when no monster attacks her, HP > 80%, not attacking / routing
#                 through a portal / talking to an NPC, and no rally / departure / hold running
# For partylead: never while a rally or departure runs. Waits up to 2 h per plugin.
# Usage (WSL): bash /mnt/g/RO/tools/rogui/ayla_safe_reload.sh --anywhere partylead aylagear
CMDS=/mnt/g/RO/server/openkore-ayla/control/commands.txt
ST=/mnt/g/RO/server/openkore-ayla/logs/status.json
TOWNS='"map":"(prontera|payon|geffen|morocc|alberta|aldebaran|izlude|yuno|comodo|umbala)"'
ANY=0; [ "$1" = "--anywhere" ] && { ANY=1; shift; }
calm() {
  local tail; tail=$(docker logs --tail 40 ro-openkore-ayla 2>&1 | sed -e 's/\x1b\[[0-9;?]*[a-zA-Z]//g')
  echo "$tail" | tail -3 | grep -qE '\[(sellAuto|buyAuto|storageAuto|NPC|attack|teleport|dead)' && return 1
  echo "$tail" | grep -qE 'attacks you|is casting .* on you' && return 1
  if [ $ANY = 0 ]; then grep -qE "$TOWNS" "$ST" || return 1; fi
  local hp mx; hp=$(grep -o '"hp":[0-9]*' "$ST" | cut -d: -f2); mx=$(grep -o '"max_hp":[0-9]*' "$ST" | cut -d: -f2)
  [ -n "$hp" ] && [ -n "$mx" ] && [ $((hp * 100)) -gt $((mx * 80)) ] || return 1
  printf 'partylead status\n' >> "$CMDS"; sleep 2
  docker logs --tail 40 ro-openkore-ayla 2>&1 | grep -a 'holding=' | tail -1 | grep -q 'holding=0 rally=- depart=-' || return 1
  return 0
}
for p in "$@"; do
  for i in $(seq 1 1440); do
    if calm; then
      echo "$(date -u +%H:%M:%S) reloading $p"; printf 'plugin reload %s\n' "$p" >> "$CMDS"; sleep 30; break
    fi
    sleep 5
  done
done
echo "$(date -u +%H:%M:%S) done"
